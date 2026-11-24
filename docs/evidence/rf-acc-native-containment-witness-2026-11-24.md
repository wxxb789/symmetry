# rf-acc-native-containment-witness — evidence record (2026-11-24/25, implement + apply + fix rounds 1-3)

Contract: `goal-rf-acc-native-containment-witness.md` (GDD state root, external to the repository).
Base `477e4c62` (#277, the rf-acc-native-burndown merge). Branch
`refactor/rf-acc-native-containment-witness`. Scratch branch
`scratch/rf-acc-native-containment-witness` (deleted at stage close, §9). Every hash below is a
working-tree byte hash; the §1 rows cite the final (post-fix) bytes.

Two clauses are proven green by the binding run `45465331434` on its bytes: the containment rows
name their post-seed App refusal (bounded, redacted, in the job log) and end at a recorded
`DECISION_NEEDED` product cause; `synthetic_goal_cleanup` is past its admission refusal and stops
at a later named check. Fix rounds 1-2 (§13, §14) then changed five code/script rows; the round-2
binding run `45486208881` re-proves both clauses on the delivered bytes (§14.8) — identical stop
signatures, ledger counts and failing-test sets against `45465331434`, no new stop class.

## 1. Binding — final bytes, runs, and scratch-only bytes

| File (working tree) | sha256 |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `71980151ffebbb39142e5d4a8620f1f43de1256723d3f17cd3e04a9d06d495a4` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `29366dd82495027dcbaa23bdb1b8270a5ee67d4255a6bee94b3e4b57710a546c` |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `d120677d8718d0ad346d0fdfc9ef6136234d1b48071d77e3506aac4c844f01e6` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `0903c8e25de5b16a1936a586261b3f4c7c7b32a0224b3eee5b4c1a1da066173c` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `e3c2c1554d046d234dcd29a9ac1c79f0d526f1e27f4f61ce82eb97451bba9466` |
| `scripts/ci/test-linux-v2-evidence.py` | `00342bc23cbbb08c7569b95049ddb68e7d61a640ba0c481eda0db59847a1f234` |
| `docs/plans/2026-11-24-rf-acc-native-containment-witness-plan.md` | `ac84fd3b91746199e9f700edea5ca84ebf1de3047dfcaab4b17374b6455531df` |

Run `45465331434` ran on scratch head `65bfeaf2`, whose six code/script rows were byte-identical
to the pre-apply copies of the rows above (`cmp` clean for each) plus the two scratch-only files
of §10; fix rounds 1-2 then changed five code/script rows (§13: the containment capture helper,
the acceptance script, the exporter and its suite; §14: the acceptance script again and
`production_linux_v2_owner_cli_test.go`). The round-2 binding run `45486208881` ran on scratch
head `5e2c6dfd`, whose copies of all six code/script rows are byte-identical to the table as it
stood at round 2 (§14.8). Round 3 then changed two rows — the script (`machine_run` root fix,
§15.1) and the plan — and the round-3 binding run `45495704270` ran on scratch head `a9abd537`,
whose copies of all six code/script rows and the plan are byte-identical to the final table
(§15.3); only this record (plus the out-of-tree PR body) postdates the round-3 run because it
cites it. Each hosted proof was possible because the conductor authorized one dispatch for its
round and the stage could push only the scratch branch.

| Run | Head | Sets | Result |
| --- | --- | --- | --- |
| `45388258526` | `99277aed` (before-state, burndown) | all 14 | containment rows unnamed; `synthetic_goal_cleanup` at the admission refusal (§3) |
| `45431551609` | `7072891f` (burndown binding run) | all 14 | pre-change baseline for "no previously green row reddens" (§6) |
| `45452357027` | `05ec6ec8` | `codex_containment`, `claude_containment`, `synthetic_goal_cleanup`, `migration` | names the fixed-B HOME refusal (§7.1) |
| `45455325059` | `305a5d0f` | same four | attempt 1 died in the `contracts` job (known frontend flake, §11); `gh run rerun` passed it; the acceptance job (8m10s) names the clone-admission refusal (§7.2) |
| `45458181149` | `1ff5ad44` | all 14 | every row named; job 23m04s; its failure blocks are byte-identical to run 4's, but one doc comment was trimmed to the two-line comment policy afterwards, so it is not the binding run |
| `45465331434` | `65bfeaf2` | all 14 (final literal, §6) | every row named; job 23m23s |
| `45486208881` | `5e2c6dfd` (round-2 final bytes, §14) | all 14 (final literal) | identical 91-line stop signature, failing-test set and ledger against `45465331434`; job 21m44s; no new stop class (§14.8) |
| `45495704270` | `a9abd537` (round-3 final bytes, §15) | all 14 (final literal) | identical 91-line stop signature, failing-test set and ledger against `45486208881`; job 22m36s; no new stop class (§15.3) |

## 2. Change 1 — the captured App stderr, its redaction, and the failure report

- Sink: `linuxV2OwnerWitnessStderrPath` (`owner-witness.stderr` beside the response file) replaces
  `os.DevNull` as the command's stderr (`daemon/e2e/production_linux_v2_owner_cli_test.go:52-63`),
  opened `O_WRONLY|O_CREAT|O_TRUNC|O_CLOEXEC|O_NOFOLLOW` 0600 (round 2; §14), one per command
  start, in the A-private `artifactDir`.
- Helpers (`production_linux_v2_native_witness_test.go:2001-2081`):
  `linuxV2KnownCredentialValues` (every `os.Environ` name matching
  `linuxV2IsCredentialEnvironmentName` — which covers the writer's `SYMMETRY_OPERATOR_TOKEN` /
  `SYMMETRY_ENROLLMENT_TOKEN` cases — the request's lease token, and the provider token; round 2
  narrows the effective set under the harness to the request tokens plus the printer's
  `evidence_secret_values_file`, because the capture-bearing cases' `--setenv` lists carry no
  `*_TOKEN`/`*_API_KEY` entry, so the env-derived leg is inert on every capture path (the
  `legacy-v1` arm sets two dev dummy `*_TOKEN` entries,
  `linux-v2-native-acceptance.sh:1897-1898`, but never starts the owner-witness command; round-3
  qualifier, §15.2) — residual §11/§14),
  `linuxV2OwnerWitnessStderrTail` (`:2041-2066`: replaces every known value in the whole capture
  with `<redacted>`, then refuses — error, no text — if a value survives in the marker-stripped
  view, and only then returns the last ≤40 lines / ≤4 KiB window; §13 lists the round-1 change),
  `linuxV2OwnerWitnessStderrBoundedLines` (≤5 lines, each ≤300 content bytes).
- Report: `linuxV2ReportOwnerWitnessFailure` (`owner_cli_test.go:1580-1598`) waits ≤5 s for the
  pidfd, logs `owner-witness command exit status: <n>`, then the tail as separate `t.Logf` lines
  before each `t.Fatalf`. Line budget: 1 status line, then last ≤5 tail lines (`:2008`); the
  fixture printer keeps the last 10 lines of a test and four of them surround the report
  (`=== RUN`, the `t.Fatalf` body, the fixture's `:1838` StateDir cleanup line, `--- FAIL`), so
  the maximum report stream fills the window exactly and keeps the status line and every tail
  line. Call sites: eight `waitLinuxV2OwnerWitnessStage` waits report
  (`:1751`, `:2384`, `:3394`, `:3573`, `:3804`, `:3970`, `:4129`, `:4451`, the barrier wait at
  `:4451` included; round 2's sink change shifts every ref after `:57` by seven lines, §14).
  Census and exemptions (round 1, §13): nine
  `waitLinuxV2OwnerWitnessStage` call sites exist — the ninth,
  `production_linux_v2_helper_exit_witness_test.go:188` (reached by the `shared` set at
  `linux-v2-native-acceptance.sh:2955`), is outside this goal's owned set and stays unnamed; the
  recovery/replay exit waits on a live command (`owner_cli_test.go:1848-1850`, `:3472-3473`,
  `:3707-3708`, `:4522-4523`) stay unnamed too, and their rows stop at their own named stage
  waits first.
- Local probes, before any dispatch:
  - `print_witness_output_lines` extracted verbatim from the script and fed a real non-verbose
    `go test -json` failing test (throwaway module): the `exit status` and 279-byte diagnostic lines
    printed; a 420-byte content line and a line carrying `development-operator-token` were dropped;
    last-10/400-byte semantics unchanged.
  - Unit tests (`:2083`, `:2147`, `:2167`, `:2180`):
    `TestLinuxV2OwnerWitnessStderrCaptureRedactsAndBounds` (44 lines → last 40, four credential
    values replaced in the window, the out-of-window carrier line dropped, diagnostic kept, one
    over-long line truncated, ≤5 logged lines),
    `TestLinuxV2OwnerWitnessStderrCaptureRedactsValuesInsideTheMarker` (a value that is a
    substring of the marker does not veto the tail),
    `TestLinuxV2OwnerWitnessStderrCaptureRefusesFragmentedValue` (a value redaction split is
    refused), and `TestLinuxV2OwnerWitnessStderrCaptureRedactsAcrossTheByteWindow` (a credential
    straddling the 4 KiB cut is redacted before the window). All four PASS on a linux/amd64 test
    binary (§8).
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
  `print_filtered_output_lines` (`:1567`), like the `run_go_test` twin.
- P3-2: both Change-1 `systemctl show` blocks and the `journalctl --output=cat -n 80 --grep=…` dump
  (`:1637-1653`) capture to `$fixture_root/daemon-service-failure.log` and print through
  `print_filtered_output_lines`; the narrow `--grep` and `-n 80` bound are unchanged. The branch had
  0 firings in run `45458181149` (0 `systemctl show --property=Result` lines).
- P3-3: the two probe pipelines (`:1721-1723`, `:1727-1730`) became capture-then-filter, so
  `machine_run_as` runs in the parent shell. Probe (throwaway stub): pipeline shape → `count after
  pipelines=0` and the unit `probe-step-1` reused twice; capture shape → `count after captures=2`,
  units `step-1`, `step-2`. Filter probe on the extracted function: a 420-byte line and a
  secret-bearing line were dropped, the last 40 of 48 lines printed, exit 0. Round-3 note
  (§15.2/§15.4): round 2's script edits shifted these ranges by another four lines; the refs
  above are the final tree's and re-verified, and the `machine_run_as` passwd read keeps its
  `:950-953`.

## 5. Exporter exclusion (conductor ruling Q3)

`export-linux-v2-redacted-evidence.py` gains `EXCLUDED_WITNESS_NAMES = ("owner-witness.stderr",)`
and `require_no_excluded_witness_file` (all three modes, before any read): an input root that
carries the capture refuses publication, and — added in round 1 (§13) — any computed export source
path that carries the name as a component refuses too, so a future allowlist entry that named a
subpath such as `stage-replay/owner-witness.stderr` cannot leak. The guard calls moved after each
mode's allowlist walk with the computed sources passed in (`:1545`, `:1552`, `:1560`).
`test-linux-v2-evidence.py` gains
`test_manual_containment_refuses_the_owner_witness_stderr_capture` (the apply stage removed the
successful-export test's added `assertNotIn`, §12). Round-1 tests: a computed subpath source
refuses, and compose/legacy witness roots carrying the capture refuse (`:1619`, `:1626`, `:1636`).
Mutant row: with the guard stashed, the new test fails (`AssertionError: 0 == 0` — the export
succeeded); with the guard, the full suite is `Ran 48 tests … OK` (§8; 43 at the apply stage,
§12).

## 6. The final literal all-14 run (`45465331434`, head `65bfeaf2`, job 23m23s; pre-fix bytes, §13)

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
(`:612-613`). Every B-path fence and A-view ACL was already `$B_HOME`-relative, with one
round-1 exception (§13, STD-07): `machine_run_as` passed a hardcoded per-account `--setenv=HOME=`
value that was false for B after this move; it now derives HOME from the guest passwd entry
(`:950-953`). No other byte moved in the apply stage. Before/after rows: `45452357027` row 1 → `45455325059` and
`45458181149` (rows 1-2 proceed to the refusal below). Probe of the new fence: synthetic passwd
rows → positive passes, a row carrying the old outside-root HOME and a missing user both fire.

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

## 8. Gates (implement stage; exact commands, exit codes)

| Command (cwd) | Exit | Result |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`) | 0 | 0 lines |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` (`daemon/`) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` (`daemon/`) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o <tmp> ./e2e` then the binary `-test.run 'TestLinuxV2OwnerWitnessStderrCapture'` (WSL Ubuntu-24.04, linux/amd64) | 0 | all four capture tests PASS (two at the apply stage; §13 adds two) |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL Ubuntu-24.04, python 3.12.3) | 0 | `Ran 48 tests … OK` (43 at the apply stage; §13 adds five) |
| `python3 scripts/ci/test-linux-v2-go-events.py` (same) | 0 | clean |
| `python3 -m py_compile scripts/ci/export-linux-v2-redacted-evidence.py` (same) | 0 | clean |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | clean |
| `shellcheck --severity=warning -f gcc scripts/ci/linux-v2-native-acceptance.sh` (Windows 0.11.0) | 1 | 16 findings on the shipped bytes and 16 on the base script; the line:col-stripped finding multiset is identical at base (delta 0); SC histogram `SC1078 ×1, SC2024, SC2043, SC2154 ×8, SC2174, SC2276 ×2, SC2288, SC2289` (this row is the round-1 correction of the apply-stage count; §12/§13) |
| the same command under WSL 0.9.0 | 0 | 16 findings, same histogram, delta 0 versus the base script; this distro build prints findings but exits 0 even for parse errors (a redirection parse probe also exits 0) |
| `actionlint .github/workflows/scratch-native-containment-witness.yml` (throwaway, 1.7.12) | 0 | clean; the real workflow is untouched, so no row for it |

Changed-document gates, run in the scratch worktree over a temporary, never-pushed local commit of
this tree plus `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this record (each amendment was
re-committed and the gates re-run; the last run is the row below, so the temporary head is not a
delivered artifact):

| Command (`scripts/ci`, `--base 477e4c62 --head <temporary commit>`) | Exit | Result |
| --- | --- | --- |
| `python3 check-record-cleanliness.py` | 0 | `1 changed record(s) pass the root-only check` (the first attempt refused the fixed-B legacy home path quoted in §7.1; the sentence was rewritten to a placeholder) |
| `python3 check-citation-existence.py` | 0 | `1 changed document(s) pass the path and symbol check (600 path citation(s))` on the delivered bytes (§14.8, base `33ad88a9`; the apply-stage measurement was 593); three pre-existing `CLOSED-TODO.md` notes name gitignored paths |
| `python3 jscpd-changed.py` | not run | the pinned `node_modules/jscpd` is not installed on this host and the registry is unreachable (the burndown's round-1 constraint); replacement proxy: every 10-line whitespace-normalized window containing an added line was searched against the whole head tree (1,798 scanned-format files, 647,423 distinct windows) → 0 clone candidates |

The Go unit tests, `gofmt`, `go vet` and the fixture-contract scripts run in the `daemon` CI job
(`.github/workflows/ci.yml`); that job is triggered by `pull_request`, not by a scratch-branch push
(`ci.yml` pushes to `main` only), so this stage's rows for them are the local runs above. The e2e
witnesses themselves need the installed A/B fixture and were exercised only by the hosted scratch
runs, as the contract allows. Git-diff hygiene: `git diff --check` clean; the profile-name and
host-path scans used by the burndown apply (no Windows/WSL host prefixes in added lines).
Fix rounds 1-3 re-ran every row above on their bytes (§13 carries round 1's mutant rows, §14.6 and
§14.8 the round-2 gates, §15.2 the round-3 gates); §12 keeps the apply-stage counts and §13 the
round-1 document gates.

## 9. Scope and scratch hygiene

Owned files touched: the three `daemon/e2e` files (§2, §3), the acceptance script (§4, §7.1), the
exporter and its fixture suite (§5), the plan, this record, `docs/TODO.md` and
`docs/archive/CLOSED-TODO.md`. Not touched: `daemon/internal/**` (no byte), `control/**`, the real
workflow (no byte), the frozen OpenCode pin/render tokens, any environment/secrets/vars/trigger.
The fix stage re-touched the capture helper's file, the acceptance script, the exporter and its
suite, the plan and this record; no new file was added and no other file moved (§13).

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
`45388258526` as the contract allows. The fix stage spent no dispatch: its four changed
code/script files postdate the binding run, and the hosted re-proof on the final bytes (or a
formal acceptance of the amendment) remains conductor-owned.

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

- The round-1/round-2 code/script deltas after `45465331434` (the report budget, the capture
  helper's redaction order and refusal view, `machine_run_as`'s HOME source and unit counter, the
  round-2 filter captures and the sink's `O_NOFOLLOW`) are re-covered by the round-2 binding run
  `45486208881`, which reproduces the same stop signatures, ledger and failing-test sets (§14.8).
  Round 3's script delta (the `machine_run` BASHPID suffix) is re-covered by the round-3 binding
  run (`45495704270`, §15.3).
  What that run cannot discriminate is the deltas' mutation behavior — the local probes and mutant
  rows (§13, §14.7) are that evidence — and the watcher capture path, which no run reached.
- `machine_run_as`'s passwd-derived HOME is exercised by the extraction probe only (positive and
  negative rows, §13); the guest transport itself needs the installed A/B fixture, so its mutant
  is an oracle gap: removing the derivation reddens no CI job, only that probe.
- Capture redaction's env-derived leg is inert under the harness (§14.3, qualified in §15.2): the
  capture-bearing cases' `--setenv` lists carry no `*_TOKEN`/`*_API_KEY` entry (the `legacy-v1`
  arm sets two dev dummy tokens, `linux-v2-native-acceptance.sh:1897-1898`, but never starts the
  owner-witness command), so the effective known-value set is the request's lease/provider tokens,
  and the fixture printer's `evidence_secret_values_file` filters the echoed lines independently.
  The guest env is deliberately not widened.
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
- The apply-stage §8 shellcheck count (11) was wrong; both installed binaries report 16 findings
  on the shipped bytes with the histogram §8 now carries, and the delta versus the base script is
  the binding number and is 0 (§12, §13).
- `docs/TODO.md`'s `linux-native-v2-acceptance` dispatch entry and the burndown's other follow-ups
  are untouched: this goal narrows the containment follow-up to §7.2 and closes the capture/cleanup
  half.
- The hosted all-14 run was produced on the pre-apply copies of the §1 rows; §12 records the four
  behavior-neutral apply deltas and §13 the four fix-stage deltas; both leave the hosted re-proof
  (or a formal acceptance of the amendment) to the conductor.

## 12. Apply stage (2026-11-24) — accepted simplifications and the binding amendment

Source: this goal's simplify-stage report (run `symmetry-rf-acc-native-containment-witness-simpl`,
findings F1-F4). Applied: F1, F2, F3's in-scope half, F4. All four are behavior-neutral; the
paragraphs below name the evidence and each probe re-runs with one command.

- **F1 (Delete)** — `linuxV2ReportOwnerWitnessFailure`'s `len(lines) == 0` branch
  (`owner_cli_test.go:1589-1592`) was unreachable: `linuxV2OwnerWitnessStderrTail` returns either
  an error or `strings.Split`'s result, whose length is ≥ 1 for every input (probe P1), so the
  `capture is empty` line could never print. Deleted; the function now ends at `:1592` and the
  eight `linuxV2ReportOwnerWitnessFailure` call sites shifted to `:1744`, `:2377`, `:3387`,
  `:3566`, `:3797`, `:3963`, `:4122`, `:4444` at the apply bytes; round 2's sink change shifted
  them seven lines further to `:1751`, `:2384`, `:3394`, `:3573`, `:3804`, `:3970`, `:4129`,
  `:4451` (§2 and plan §2 carry the final refs; §15.4 re-verified).
- **F2 (Delete)** — the `assertNotIn("owner-witness.stderr", …)` line the implement stage added
  to `test_manual_containment_export_records_pinned_native_binary` could not fail: the output
  directory receives only the allowlisted names plus `manifest.json`, and a root without the
  capture never yields it. Mutant
  P2 (guard call lines removed from a disposable copy) reddens only the dedicated refusal test
  and leaves the export test `ok`, i.e. the deleted line detected nothing about the guard. Q3's
  coverage stays with `test_manual_containment_refuses_the_owner_witness_stderr_capture` (§5).
- **F3 (Refactor, in-scope half)** — `linuxV2KnownCredentialValues`
  (`native_witness_test.go:2025`) now checks `!linuxV2IsCredentialEnvironmentName(key)` alone;
  the dropped `key != "SYMMETRY_OPERATOR_TOKEN" && key != "SYMMETRY_ENROLLMENT_TOKEN"` disjuncts
  were provable no-ops: the predicate is case-insensitive and both names end in `_TOKEN` (probe
  P1: 0 mismatches over 10 keys; exhaustively, a matching name always already skips and a
  non-matching name is neither literal). The pre-existing writer's copy
  (`native_witness_test.go:3407`) is outside this goal's owned region ("only the shared
  redaction/capture helper") and stays untouched; only the increment's own copy is gone, so the
  helper no longer carries the duplicated literals.
- **F4 (Refactor)** — the comment above `print_witness_failure_context`
  (`linux-v2-native-acceptance.sh:1684-1685` at the final bytes; `:1669-1670` was the apply-stage
  ref) was rewritten to drop the falsified premise that
  the command's stderr is discarded by its own harness (§2, §6, §7.1 show the captured, redacted
  tail in the job log). Same line count; the fixture-state clause stays true.

Binding amendment: run `45465331434` was produced on scratch head `65bfeaf2`, byte-identical to
the pre-apply copies of the §1 rows; the four deltas above were behavior-neutral by P1/P2 and by
inspection (F4 touches comment text only). Round 1 then changed four code/script rows again
(§13), so the final byte binding and the hosted re-proof — or a formal acceptance of the
amendment — are conductor-owned (one dispatch of the contract's budget remains).

Census for the "every" clauses this stage touches:

- "redacts every credential value the harness already knows": `linuxV2IsCredentialEnvironmentName`
  call sites, 6 total — definition `native_witness_test.go:1991`; helper `:2025` (converted by
  F3); writer `:3407` (exempt: pre-existing, outside the owned region); `owner_cli_test.go:124`,
  `:140` and `production_linux_v2_rootless_compose_witness_test.go:1115` (pre-existing env
  filtering, untouched — the rootless witness already uses the plain-predicate form F3 adopts).
- Change 1's call-site census (corrected in round 1, §13): `linuxV2ReportOwnerWitnessFailure(t,
  fixture, command)` appears 8 times, one per `production_linux_v2_owner_cli_test.go` wait site;
  the ninth `waitLinuxV2OwnerWitnessStage` call site
  (`production_linux_v2_helper_exit_witness_test.go:188`) and the recovery/replay exit waits
  (`:1848-1850`, `:3472-3473`, `:3707-3708`, `:4522-4523`) stay unnamed as explicit exemptions
  (§2 carries the full census).
- Q3's "all three modes": `require_no_excluded_witness_file` definition
  (`export-linux-v2-redacted-evidence.py:707`) plus call sites `:1545`, `:1552`, `:1560` — all
  three present; round 1 passes each mode's computed sources into the same guard (§5, §13).

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

The apply-stage §8 count (11 findings) was not reproducible with either installed binary (both
report 16 on the shipped bytes); §8 now carries the measured row, and the binding number, the
delta versus the base script, stays 0 on both binaries. `actionlint` has no row: the real
workflow is untouched. The apply-stage note that the §1 code hashes were re-checked after this
record's last edit held for the apply tree; round 1 recomputed them (§13).

## 13. Fix stage (2026-11-24) — review round 1, findings STD-01–STD-10

Source: this goal's review-stage report (`MUST_FIX (10)`, all P3, two mechanisms described as
unreachable with the repository's job env). Every finding is FIXed or explicitly exempted below;
no settled design or contract clause was weakened. The four changed code/script files and the
plan carry the §1 hashes.

| id | disposition | what changed, and the observed before/after proof |
| --- | --- | --- |
| STD-01 | docs; ninth site exempt (outside the owned set) | The census is now 9 `waitLinuxV2OwnerWitnessStage` sites: 8 report (`owner_cli_test.go:1751,2384,3394,3573,3804,3970,4129,4451`; round 2 shifted the apply bytes' `:1744,…` by seven lines, §15.2), the ninth (`production_linux_v2_helper_exit_witness_test.go:188`, reached by the `shared` set at `linux-v2-native-acceptance.sh:2955`) and the recovery/replay exit waits (`:1848-1850`, `:3472-3473`, `:3707-3708`, `:4522-4523`) are explicit exemptions — the first because the file is outside the goal's owned set. Rewritten: §2, §12's census bullet, plan §2. Census probe: `grep -n 'if err := waitLinuxV2OwnerWitnessStage'` over the two `e2e` files returns 9 call sites (8 in `production_linux_v2_owner_cli_test.go`, 1 in the helper-exit file) and `grep -c 'linuxV2ReportOwnerWitnessFailure(t, fixture, command)'` returns 8. |
| STD-02 | code + test + docs | `linuxV2OwnerWitnessStderrLogLines` 9 → 5 (`native_witness_test.go:2008`). Before: `WitnessOutputWindowTest.test_report_budget_keeps_its_status_line_and_every_tail_line` fails (the 9-line budget evicts `owner-witness command exit status: 1` and tail lines 0-1 from the printer's 10-line window — mutant M3 reproduces it). After: 10 of 10 lines print, the status line and every tail line included. §2/plan §1 carry the window arithmetic. |
| STD-03 | code + test (one pinned expectation replaced) | The refusal check now runs on the marker-stripped view (`:2051`): a value that is a substring of `<redacted>` no longer vetoes the tail, and a value a redaction split is refused. `TestLinuxV2OwnerWitnessStderrCaptureRedactsValuesInsideTheMarker` (marker output, no refusal) and `…RefusesFragmentedValue` replace the old `…RefusesMarkerCarriedValue` expectation, which pinned the veto this finding names as the regression. Before: both fail (refusal where the marker is the carrier; no refusal where redaction split the value). |
| STD-04 | code + test | The whole capture is redacted before the ≤40-line/≤4 KiB window (`:2041-2066`), so a credential value straddling the old raw-byte cut can no longer reach the tail as a fragment. `…RedactsAcrossTheByteWindow` fails on the old order (leaked `90fe`; mutant M2 reproduces it) and passes now. |
| STD-05 | test + docs | The `operatorToken` carrier moved inside the retained window (`:2106`), the out-of-window line's drop is asserted separately (`:2126-2128`), and §2 no longer miscounts the replaced values (all four are in the window). |
| STD-06 | docs (plan §1) | The false invariant "commands never overlap within one fixture" is replaced with the actual semantics: the unresolved-journal witness starts its fresh command (`native_witness_test.go:495`) while the setup command's owner processes stay live (`:449-453`), so that start truncates a capture the setup tree may still hold open; no report reads the capture in that window, so the interleave stays latent. |
| STD-07 | code + probe (oracle gap) | `machine_run_as` derives `HOME` from the guest passwd entry (`linux-v2-native-acceptance.sh:950-953`) and refuses a missing/relative home. Probe (verbatim in the appendix) against the branch-head bytes first shows the defect: B ran with an A-shaped per-account home and an unknown account succeeded with a synthesized home; the final bytes give B its passwd home and refuse the unknown / relative / transport-failure rows (exit 3). The guest transport needs the installed A/B fixture, so no CI job reddens without the derivation: recorded oracle gap (§11). |
| STD-08 | code + tests | `require_no_excluded_witness_file` also refuses any computed export source path carrying the excluded name as a component (`export-linux-v2-redacted-evidence.py:715-718`), and all three modes pass their computed sources in after the allowlist walk (`:1545`, `:1552`, `:1560`). New tests: subpath source refuses (fails pre-change: no such parameter), compose refusal, legacy refusal (both cover the previously untested call sites; mutants M4/M5a/M5b). Plan §5's claim now matches the guard. |
| STD-09 | docs | §8's shellcheck row carries the measured 16 findings and histogram; §11/§12 note the correction. Measured: Windows 0.11.0 exit 1, WSL 0.9.0 exit 0 (that build prints findings without signaling), identical line:col-stripped multiset at base (delta 0) on both. |
| STD-10 | docs | §4's ranges refreshed to the final tree (`:1567`, `:1637-1653`, `:1721-1723`/`:1727-1730`; round 2 shifted them, round 3 re-verified §15.2/§15.4) and §2's double-counted "barrier wait and all eight waits" sentence rewritten (the barrier wait `:4451` is one of the eight). |

Round-1 deltas (compared with the pre-fix copies of the §1 rows):

- `daemon/e2e/production_linux_v2_native_witness_test.go`: helper redaction order and refusal
  view, budget 5, test updates and two new tests (`:2041-2066`, `:2108`, `:2128-2130`, `:2147`,
  `:2167`, `:2180`).
- `scripts/ci/linux-v2-native-acceptance.sh`: `machine_run_as` passwd-derived `HOME`.
- `scripts/ci/export-linux-v2-redacted-evidence.py` + `scripts/ci/test-linux-v2-evidence.py`:
  source-component guard, moved call sites, three guard tests and the printer-window pair.
- `docs/plans/2026-11-24-rf-acc-native-containment-witness-plan.md` and this record: §1/§2/§4/§5
  and §13.

Counts at the final tree: the e2e capture tests run 2 → 4; `test-linux-v2-evidence.py` 43 → 48
`OK`; `test-linux-v2-go-events.py` 6 `OK` unchanged.

CI coverage of this round's guards (no new suite was added, so nothing needed wiring): the
`daemon` job (`.github/workflows/ci.yml`, `ubuntu-latest`, non-root runner) runs
`go test -race -count=1 -timeout 600s -json ./...` — the four capture tests included — and
`python3 ../scripts/ci/test-linux-v2-evidence.py`, so M1-M5 redden there; the `machine_run_as`
mutant reddens no CI job and stays the oracle gap (§11).

Round-1 probes and mutants (verbatim mutation or overlay, then the full sha256 of the mutant
artifact; every artifact lives outside the repository and every restoration was checked against
the §1 hash):

M1 — drop the marker strip (mutant of the helper check line):

```
-	stripped := strings.ReplaceAll(redacted, linuxV2OwnerWitnessStderrRedacted, "")
+	stripped := redacted
```

Mutant `daemon/e2e/production_linux_v2_native_witness_test.go` sha256
`fd1169e92b246fda070f267ef9fdeaee9adfc7f317e2fae79fa4dcc9c76583cf`. Result: 2 FAIL
(`…RedactsValuesInsideTheMarker`, `…RefusesFragmentedValue`), other two PASS; restored hash
matches §1.

M2 — re-introduce the pre-redaction byte cut (mutant inserts after `redacted := string(contents)`):

```go
	if len(redacted) > linuxV2OwnerWitnessStderrTailBytes {
		redacted = redacted[len(redacted)-linuxV2OwnerWitnessStderrTailBytes:]
	}
```

Mutant file sha256 `cbb1d66f1c6b0c6fa23dbbd0276c189a95a23cd43f657fee78274085c9a77fa7`. Result: 1
FAIL (`…RedactsAcrossTheByteWindow`); restored hash matches §1.

M3 — budget back to 9:

```
-	linuxV2OwnerWitnessStderrLogLines  = 5
+	linuxV2OwnerWitnessStderrLogLines  = 9
```

Mutant file sha256 `b7a44f88568c5b5bfa17a8e75f8a7dddc2c2251a6099c2b0f63838f789be8e3f`. Result: 1
FAIL (`WitnessOutputWindowTest.test_report_budget_keeps_its_status_line_and_every_tail_line`);
restored hash matches §1.

M4/M5 — exporter guard mutants, built in a disposable copy of `scripts/ci` outside the repository:
restore the shipped exporter, then run

```python
import pathlib
p = pathlib.Path("export-linux-v2-redacted-evidence.py")
text = p.read_text(encoding="utf-8")
line = '        require_no_excluded_witness_file((witness_root,), tuple(source for source, _ in expected))\n'
assert text.count(line) == 2, text.count(line)
compose = text.replace(line, '', 1)
legacy = compose.replace(line, '', 1)
p.with_name("export-m5-compose.py").write_text(compose, encoding="utf-8")
p.with_name("export-m5-legacy.py").write_text(legacy, encoding="utf-8")
```

M4 (drop the computed-source loop from the exporter): mutant sha256
`0be15eb2af032c243c666ac3e7dd994e42a0fd20f36650986975529c2809709e`. Result: 1 FAIL
(`test_excluded_capture_in_a_computed_source_path_refuses`, `SystemExit not raised`); the
compose/legacy tests stay green. M5-compose (compose guard call removed): mutant sha256
`746d6e344a1b538c0201b7a89c07e23096974f4997421761b207f8d4c1cb9682`, 1 FAIL (the compose refusal
test's `owner-witness.stderr` assertion). M5-legacy (both call sites removed): mutant sha256
`17578407ccd95bc65e9efe1eb4a52d75a624dbc9f1c6f6431f38ba7fcc6c43d3`, 2 FAIL (compose + legacy).

Machine-run-as extraction probe (STD-07 replacement proof; sha256
`f1ec7bc3896d5d13b2b9b90a16b00f7aad3b32d0966e21165537efcc115b822f`), run against the shipped
script with the guest transport stubbed:

```bash
#!/usr/bin/env bash
# Local probe for the machine_run_as HOME derivation; stubs the guest transport.
set -u
machine=symmetry-test-machine
machine_run_count=0
fail() { printf 'PROBE-FAIL: %s\n' "$1" >&2; exit 3; }
declare -A probe_passwd=(
  [symmetry-a]="symmetry-a:x:52000:52000::<A_HOME>:/bin/bash"
  [symmetry-b]="symmetry-b:x:52001:52001::<B_HOME>:/bin/bash"
)
machine_run() { printf '%s\n' "${probe_passwd[$3]:-}"; }
sudo() { printf 'sudo %s\n' "$*"; }
eval "$(sed -n '/^machine_run_as() {/,/^}/p' "$1")"
echo "--- positive A run"
machine_run_as symmetry-a symmetry-a /usr/bin/true
echo "--- positive B run"
machine_run_as symmetry-b symmetry-b /usr/bin/true
echo "--- negative unknown user"
if (machine_run_as symmetry-c symmetry-c /usr/bin/true); then echo 'unexpected success'; else echo "refused exit=$?"; fi
echo "--- negative relative home"
probe_passwd[symmetry-c]="symmetry-c:x:53000:53000::relative-home:/bin/bash"
if (machine_run_as symmetry-c symmetry-c /usr/bin/true); then echo 'unexpected success'; else echo "refused exit=$?"; fi
echo "--- negative transport failure"
machine_run() { return 1; }
if (machine_run_as symmetry-b symmetry-b /usr/bin/true); then echo 'unexpected success'; else echo "refused exit=$?"; fi
```

The block above is the run artifact with the two guest passwd homes replaced by `<A_HOME>` /
`<B_HOME>` (the run artifact outside the repository keeps the literal homes and has the sha256
given above). Observed: A run `--setenv=HOME=<A_HOME>` (unchanged), B run
`--setenv=HOME=<B_HOME>` (the fix; the pre-fix form set the false A-shaped home for B), and all
three negative cases refuse with exit 3. The shipped script's `set -Eeuo pipefail` makes a failing
`getent` take the `|| fail` arm (`set -o pipefail` is not restored in the stub, so the
transport-failure row is caught by the absolute-home check instead — same refusal).

Final-tree gates this stage ran (exact commands, WSL Ubuntu-24.04 unless noted):

| Command | Exit | Result |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`) | 0 | 0 lines |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` (`daemon/`) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` (`daemon/`) | 0 | clean |
| `go test -c -o <tmp> ./e2e` + `-test.run TestLinuxV2OwnerWitnessStderrCapture -test.v` | 0 | 4/4 PASS |
| `python3 scripts/ci/test-linux-v2-evidence.py` (python 3.12.3) | 0 | `Ran 48 tests … OK` |
| `python3 scripts/ci/test-linux-v2-go-events.py` | 0 | `Ran 6 tests … OK` |
| `python3 -m py_compile scripts/ci/export-linux-v2-redacted-evidence.py` | 0 | clean |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | clean |
| `shellcheck --severity=warning -f gcc` on the script (Windows 0.11.0 / WSL 0.9.0) | 1 / 0 | 16 findings each, delta 0 versus base (0.9.0 does not signal findings on exit) |
| `python3 scripts/ci/check-record-cleanliness.py --base 33ad88a9790186e656b76bd66c157b3bd51c9d2c --head <git stash create commit>` (run from the repository root) | 0 | `1 changed record(s) pass the root-only check` |
| `python3 scripts/ci/check-citation-existence.py --base <same> --head <same>` | 0 | `1 changed document(s) pass the path and symbol check (600 path citation(s))`; the three pre-existing `CLOSED-TODO.md` ignore notes unchanged |
| `git diff --check` | 0 | clean |
| host-path scans: the sanitize tool's root set (`/home /Users /root /tmp /var /mnt /private`, drive letters) over the two changed Markdown files → 0 hits; the program rule's case-insensitive profile-directory scan over `git diff origin/main...HEAD` and over a `git stash create` commit of the final bytes → 0 and 0 matches | 0 | no host absolute path in the changed documents and no profile-name path in the branch diff |

## 14. Fix round 2 (2026-11-25) — the PR #280 round-1 conductor ruling, items 1-6

Source: the contract's binding ruling "PR #280 round 1, triple audit reconciled", reconciling Sol
`findings/280-opus-review-1.md` (P1-1, P2-1, P3-1, P3-2), DS `findings/280-ds-audit-1.md`
(P2-1, P3-1) and the security review's F1-F3. Lessons 74-79/82 apply: every added function runs in
isolation locally before any dispatch, every "every" clause gets a census, artifacts go in
verbatim with full sha256, and records cite bytes, not commits.

Changed rows (§1 table): `scripts/ci/linux-v2-native-acceptance.sh` and
`daemon/e2e/production_linux_v2_owner_cli_test.go`; the plan gains §10; this record gains §14.
`production_linux_v2_native_witness_test.go`, the cleanup witness, the exporter and its suite are
byte-unchanged from round 1.

### 14.1 Item 1 — transient unit names are unique (`machine_run_as` passwd read)

Round 2 moved `machine_run_as` (`linux-v2-native-acceptance.sh:946-962`) to read the passwd entry
in the parent
shell: `machine_run /usr/bin/getent passwd "$user" >"$fixture_root/machine-run-as-passwd.log"
2>&1 || fail …` (`:950-952`), then `home=$(cut -d: -f6 <"$passwd_log")` (`:954`), and only then
reserves the outer unit (`:956-957`). The previous `home=$(machine_run … | cut …)` ran
`machine_run` in a command substitution: its `step-(n+1)` increment died with the subshell while
the name stayed burned, so the outer call reused `step-(n+1)` one call later. Round 3 keeps that
parent-shell capture (conductor ruling: harmless) and closes the class at its source — the
`machine_run` unit name carries counter + BASHPID (`:941`) — see §15.1.

Probe (verbatim in §14.7; sha256 `05f207dc…`), extracting `machine_run` and `machine_run_as`
verbatim from the shipped bytes with the guest transport stubbed, five calls, unit names printed
in creation order:

- round-2 bytes: `step-1 … step-9`, `units=9 duplicates=0 monotonic=1`, outer `--setenv=HOME`
  values (the synthetic guest home of A ×3 and of B ×1) → `PROBE-PASS`, exit 0.
- pre-fix bytes (`git show 5e109813:scripts/ci/linux-v2-native-acceptance.sh`, sha256
  `01550959…`): `step-2, step-1, step-3, step-2, …`, `units=9 duplicates=3 monotonic=0` →
  `PROBE-FAIL`, exit 3 (mutant row).

Round 3's extended probe (substitution call forms interleaved with parent calls) and its two runs
are in §15.1; the §14.7 probe bytes above stay the round-2 artifact.

Unit-name census for the same "transient unit names stay unique" clause, round 3 (§15.1 closes the
substitution gap): `machine_run`'s `step-n` names carry `machine_run_count` + `BASHPID` (`:941`);
the `machine_run_as` outer unit (`:957`) reserves the counter in the parent shell at every call
site; `-go-test-n` (`:1540`, `:1768`) reserves from the same counter in the parent shell;
`-ready` (`:967`) is one fixed name retried by one loop with `--collect` retiring each attempt;
`-poweroff` (`:289`) is a single-shot cleanup; `-helper-exit-control-n` (`:2656-2658`) counts in
its own parent-shell callers; `$PID_REUSE_WATCHER_UNIT` (`:501`) is stopped between sets.

### 14.2 Item 2 — round-2 echo sites routed, and the full echo census

| Site (final tree) | Change |
| --- | --- |
| `:1625-1630` daemon `-verify-linux-owner-install -config /etc/symmetry/daemon.json` | captured to `$fixture_root/daemon-verify-linux-owner-install.log` (`:1626`), printed at `:1628`; a failure prints the filtered tail and then `fail`s |
| `:1977` and `:1995` guest `journalctl … >&2` in `start_literal_pid_reuse_watcher` | the whole guest bash run is captured (`:1999`) and printed at `:2000` through the filter (both journalctl dumps and the guest's own "did not become ready" line) |
| `:1603-1614` the guest `install-linux-owner.sh` invocation | captured (`:1611`), printed at `:1612`; a fresh guest runs the installer's legacy-drain branch, which executes the staged daemon (`-verify-legacy-owner-drained`) whose fixed status line would otherwise bypass the filter |

Census of every `>&2` / `2>&1` site in the script (`grep -n '>&2\|2>&1'`, 52 sites, final tree):

| Classification | Sites | Reason |
| --- | --- | --- |
| filtered — the printers | `:169`, `:220` | the witness printer and `print_filtered_output_lines` are the filtered path |
| filtered — round-2 captures | `:1611`→`:1612`, `:1626`→`:1628`, `:1977`, `:1995`+`:1996` (guest lines, +`:1999` capture)→`:2000` | captured to a host file, printed through `print_filtered_output_lines` |
| filtered — earlier rounds | `:1641`→`:1642`, `:1647`→`:1648`, `:1651`→`:1652` (P3-2 service dumps), `:1722`→`:1723`, `:1729`→`:1730` (config probes); the file-only event captures below print through `:1567` / `:1914` + the printer | daemon failure context, config probes and go-test event lines |
| exempt — host-authored fixture text | `:6`, `:298`, `:306`, `:326`, `:330`, `:338`, `:344`, `:1565`, `:1698`, `:1704`, `:1707`, `:1720`, `:1726`, `:1912`, `:1920`, `:2242`, `:2734` | the script's own labels, statuses and retention messages; no guest text |
| exempt — discarded or file-only | `:289`, `:292`, `:294`, `:308`, `:315`, `:318`, `:337`, `:350`, `:369`, `:370`, `:951`, `:967`, `:1465`, `:1470`, `:1561`, `:1910`, `:2233` | sent to `/dev/null`, to a private log file, or consumed by `cut`; never echoed |
| exempt — tool metadata only | `:1700`, `:1702`, `:1706`, `:1709` | as-A `ls -ld` / `stat` path-mode-owner listings and the `find -printf '%y %P %s' \| sort \| head` name-and-size inventories |
| exempt — structured witness-document projection | `:162` | fixed `jq` field list (case, harness, version, outcome, category, state, binary flags); no free-form process output |

With the round-2 captures the invariant at `:204-206` holds on this script's paths: every echoed
byte of guest daemon/journal/App text passes `print_filtered_output_lines` (or the witness
printer's identical 400-byte/secret rule); the remaining sites are the script's own text,
discarded or consumed output, tool metadata listings, or the bounded document projection.

### 14.3 Item 3 — the env-derived redaction leg is narrowed, not widened

§2 and plan §1 state the effective set under the harness: the capture-bearing cases' `--setenv`
lists carry no `*_TOKEN`/`*_API_KEY` entry, so `linuxV2KnownCredentialValues`' env-derived
leg adds nothing on any capture path and the effective known-value set is the request's
`Claim.LeaseToken`/`Claim.ProviderAccess.Token`; the fixture printer's
`evidence_secret_values_file` filters the echoed lines independently (§11 residual). The guest env
is deliberately not widened. Round-3 qualifier (§15.2): the `legacy-v1` arm does pass
`--setenv=SYMMETRY_ENROLLMENT_TOKEN=…` / `--setenv=SYMMETRY_OPERATOR_TOKEN=…` (`:1897-1898`), but
that case never starts the owner-witness command or its capture, so the residual's conclusion is
unchanged.

### 14.4 Item 4 — the capture sink opens with `O_NOFOLLOW`

`startLinuxV2OwnerWitnessCommandWithRequestPath` (`owner_cli_test.go:52-63`) opens the capture via
`unix.Open(path, O_WRONLY|O_CREAT|O_TRUNC|O_CLOEXEC|O_NOFOLLOW, 0o600)` +
`os.NewFile`, matching `writeLinuxV2NativeWitness` (`native_witness_test.go:3419`); a symlink at
the sink path now refuses instead of being followed. The response file keeps its round-1
`os.OpenFile` shape; the sink stays 0600, one per command start.

### 14.5 Item 5 — §8's citation count

§8's `check-citation-existence` row is refreshed to the round-2 measurement over the final bytes
(§14.8); the round-1 row had carried the apply-stage 593 against the later 600.

### 14.6 Round-2 gates (final bytes)

| Command | Exit | Result |
| --- | --- | --- |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | clean |
| `shellcheck --severity=warning -f gcc` on the script, Windows 0.11.0 / WSL 0.9.0 | 1 / 1 | 16 findings on each, line:col-stripped multiset identical to the base `33ad88a9` and `477e4c62` scripts (delta 0 on both binaries; both installed builds signal findings on this invocation) |
| `gofmt -l .` (`daemon/`, Windows and WSL) | 0 | 0 lines |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./e2e/...` (`daemon/`, WSL) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` (`daemon/`, WSL) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` (`daemon/`, WSL) | 0 | clean |
| `go test -c -o <tmp> ./e2e` + `-test.run TestLinuxV2OwnerWitnessStderrCapture -test.v` (WSL linux/amd64) | 0 | 4/4 PASS |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL, python 3.12.3) | 0 | `Ran 48 tests in 31.457s … OK` |
| `python3 scripts/ci/test-linux-v2-go-events.py` (same) | 0 | `Ran 6 tests … OK` |
| `python3 -m py_compile scripts/ci/export-linux-v2-redacted-evidence.py` (same) | 0 | clean |
| `git diff --check` | 0 | clean |
| unit-name probe, filter probe (§14.7) | 0, 0 | final bytes pass; the unit probe reddens (exit 3) on the pre-fix script (mutant row) |

Changed-document gates over the final record/plan (round-2 measurements; run against a
`git stash create` commit of the final working tree) are in §14.8 with the binding run.

### 14.7 Round-2 probe appendix (verbatim; run under WSL Ubuntu-24.04, bash 5.2.21, python 3.12.3)

`unit-name-probe.sh`, sha256 `05f207dc1a58e1ee50d6aacb0273d58ca5f3991dc5880d3d6e8da623b1b86b50`:

```bash
#!/usr/bin/env bash
# Local probe (conductor lesson 74): unit-name sequence of machine_run/machine_run_as after the
# round-2 counter fix. Usage: unit-name-probe.sh <script>; exit 0 only when every transient unit
# name over several calls is unique and strictly monotonic in creation order.
set -Eeuo pipefail

script=$1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fixture_root=$work
machine=symmetry-test-machine
machine_run_count=0
units_log="$work/units"
sudo_log="$work/sudo-lines"
: > "$units_log"
: > "$sudo_log"

fail() { printf 'PROBE-FAIL: %s\n' "$1" >&2; exit 3; }

sudo() {
  printf '%s\n' "$*" >> "$sudo_log"
  local arg unit='' user='' i
  local -a args=("$@")
  for ((i = 0; i < ${#args[@]}; i++)); do
    arg=${args[i]}
    [[ "$arg" == --unit=* ]] && unit=${arg#--unit=}
    if [[ "$arg" == /usr/bin/getent ]]; then
      user=${args[i + 2]}
      echo "$user:x:52000:52000::/synthetic-guest-home/$user:/bin/bash"
    fi
  done
  printf '%s\n' "$unit" >> "$units_log"
}

eval "$(sed -n '/^machine_run() {/,/^}/p' "$script")"
eval "$(sed -n '/^machine_run_as() {/,/^}/p' "$script")"

printf 'machine_run     A A first\n'
machine_run_as symmetry-a symmetry-a /usr/bin/true
printf 'machine_run_as  A A second\n'
machine_run_as symmetry-a symmetry-a /usr/bin/true
printf 'machine_run_as  B B third\n'
machine_run_as symmetry-b symmetry-b /usr/bin/true
printf 'machine_run     A plain\n'
machine_run /usr/bin/true
printf 'machine_run_as  A A fourth\n'
machine_run_as symmetry-a symmetry-a /usr/bin/true

echo '--- unit sequence (creation order)'
cat "$units_log"
echo '--- outer HOME values'
grep -o -- '--setenv=HOME=[^ ]*' "$sudo_log" || true

mapfile -t units < "$units_log"
duplicates=$(printf '%s\n' "${units[@]}" | sort | uniq -d | wc -l)
monotonic=1
prev=0
for u in "${units[@]}"; do
  n=${u##*-step-}
  (( n > prev )) || monotonic=0
  prev=$n
done
printf 'units=%d duplicates=%d monotonic=%d last=%s\n' \
  "${#units[@]}" "$duplicates" "$monotonic" "$prev"

grep -q -- '--setenv=HOME=/synthetic-guest-home/symmetry-b' "$sudo_log" ||
  { echo 'PROBE-FAIL: the B run did not receive the passwd home' >&2; exit 3; }
(( duplicates == 0 && monotonic == 1 )) ||
  { echo 'PROBE-FAIL: unit names are not unique and monotonic' >&2; exit 3; }
echo 'PROBE-PASS: all transient unit names unique and monotonic'
```

`filter-probe.sh`, sha256 `fe8be99dbf877b09cc65678c25f7daa872b57d267f0f6b9ba766a9de201d59b6`:

```bash
#!/usr/bin/env bash
# Local probe for print_filtered_output_lines on the round-2 capture-then-filter sites: bounded
# guest/journal lines print, secret-bearing or over-400-byte lines are dropped, exit stays 0, and
# the last-40-line window holds. Usage: filter-probe.sh <script>
set -Eeuo pipefail

script=$1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
evidence_secret_values_file="$work/secrets"
printf 'development-operator-token\0scratch-dummy-secret\0' > "$evidence_secret_values_file"
eval "$(sed -n '/^print_filtered_output_lines() {/,/^}/p' "$script")"

{
  printf 'watcher ready: unit=symmetry-test-machine-step-9 MainPID=4242 cgroup=/system.slice/kept\n'
  printf 'journal line carrying scratch-dummy-secret\n'
  python3 -c 'print("o" * 420)'
  printf 'literal-pid-reuse-helper: bounded tail line\n'
} > "$work/capture.log"

status=0
out=$(print_filtered_output_lines < "$work/capture.log" 2>&1) || status=$?
printf '%s\n' "$out"
(( status == 0 )) || { echo "PROBE-FAIL: filter exited $status" >&2; exit 3; }
grep -q 'watcher ready' <<<"$out" || { echo 'PROBE-FAIL: bounded watcher line dropped' >&2; exit 3; }
grep -q 'bounded tail line' <<<"$out" || { echo 'PROBE-FAIL: bounded journal line dropped' >&2; exit 3; }
! grep -q 'scratch-dummy-secret' <<<"$out" || { echo 'PROBE-FAIL: secret value printed' >&2; exit 3; }
! grep -q 'ooooo' <<<"$out" || { echo 'PROBE-FAIL: oversized line printed' >&2; exit 3; }

for i in $(seq 1 45); do printf 'window line %s\n' "$i"; done > "$work/window.log"
window_out=$(print_filtered_output_lines < "$work/window.log" 2>&1)
window_count=$(grep -c 'window line' <<<"$window_out")
printf 'window lines=%s\n' "$window_count"
(( window_count == 40 )) || { echo 'PROBE-FAIL: the last-40-line window did not hold' >&2; exit 3; }
! grep -q 'window line 5$' <<<"$window_out" || { echo 'PROBE-FAIL: evicted line printed' >&2; exit 3; }
grep -q 'window line 45$' <<<"$window_out" || { echo 'PROBE-FAIL: last line dropped' >&2; exit 3; }

echo 'PROBE-PASS: bounded lines through, secret/oversized dropped, exit 0, last 40 kept'
```

### 14.8 Item 6 — the binding all-14 run on the final bytes (`45486208881`, head `5e2c6dfd`)

One fresh literal all-14 run, waited for in the foreground (`gh run watch 45486208881 --repo
wxxb789/symmetry --interval 60`): `contracts` passed in 1m23s, `linux-native-v2-acceptance` (job
138772996317) in 21m44s, all 14 case sets armed, the throwaway workflow and the ratified gh shim
unchanged (§10). The scratch head `5e2c6dfd127e9d49b7dbbcb882d0a90df2504d9a` is the delivered
working tree (rsync, then per-file sha256 equality against the goal worktree) plus the two
scratch-only files; the branch was deleted after the run, `git push origin --delete` printed
`- [deleted]`, and `git ls-remote origin scratch/rf-acc-native-containment-witness` prints nothing
(0 lines, exit 0; the unrelated `scratch/rf-acc-pi-durability-matrices` ref seen here at round 2
has since been deleted by its own stage — §15.3).

sha256 equality, scratch head vs the delivered rows of §1 (`git cat-file blob HEAD:<path>` piped
to `sha256sum`): `production_linux_v2_owner_cli_test.go` `71980151…`; `native_witness_test.go`
`29366dd8…`; `cleanup_witness_test.go` `d120677d…`; `linux-v2-native-acceptance.sh` `25137250…`;
`export-linux-v2-redacted-evidence.py` `e3c2c155…`; `test-linux-v2-evidence.py` `00342bc2…` — all
six equal the §1 table as it stood at round 2 (round 3 then changed the script row and the plan,
§15.1/§15.3). The round-2 plan (`9f332ec4…`) was at the scratch head byte-identically; only
this record and the out-of-tree PR body postdate the run because they cite it, and every other
delivered file equals the scratch head.

Per-set comparison with the binding run `45465331434` (both logs downloaded; timestamps,
durations and scratch temp-dir names normalized away): the 91-line stop signature
(`--- FAIL:`, `_test.go:<line>:`, and the captured `owner-witness` status/stderr lines) is
byte-identical, the failing-test-name multiset is identical, and the ledger counts match:

| Pattern | `45465331434` | `45486208881` |
| --- | --- | --- |
| case sets exited 1 | 14 (same names, same order) | 14 |
| `owner-witness command exit status: 1` | 12 | 12 |
| `stayed live` / `requires an App admission check` / `fixed-B native HOME is outside` | 0 / 0 / 0 | 0 / 0 / 0 |
| `native harness version differs from the tested release` | 220 | 220 |
| `Linux clone intent does not match the current admitted Subject` | 4 | 4 |
| clones / daemon smokes / `retired` histogram | 14 / 14 / `0`×1 `5`×13 | same |
| verified legs (`…RejectsUnexpectedCgroupProcsFD`, `…CleanupRejectsBCallerScratch`) | 2 | 2 |

Rows: 1-2 containment carry the named post-seed refusal with exit status 1 (`:101`/`:109`); 3 the
three `shared` scratch failures (`clone_cleanup_linux_test.go:259`, `clone_integration_linux_test.go:240`
+ `:113`, `launcher_integration_linux_test.go:439`); 4 `synthetic_goal_cleanup` past `:499` at the
helper's `release_outcome_unknown`; 5-14 the F2-excluded version gate. No new stop class: the
round-2 failure strings never fired (`did not install the staged owner artifacts`,
`verify-linux-owner-install refused the staged daemon config`, `literal PID-reuse watcher did not
become ready`, the passwd refusals: 0 each), and no previously green row reddened.

What the run exercised of round 2: the installer capture→filter prints its success lines in all 14
sets (14 `Verified fixed Linux owner assets`, 14 `one-time legacy drain proof passed`; the same
counts appear in the binding run) and the daemon `-verify-linux-owner-install` boundary line
prints 220 times (identical count, now through the filter); the `machine_run_as` passwd read ran
on every set's `--setenv=HOME` path with no refusal. The watcher capture path (`:1956-2002`) was
not reached by any set in either run (0 `literal PID-reuse watcher` lines: the sets stop before
`run_literal_pid_reuse_witness` at `:2959`), so its shape rests on the filter probe (§14.7) and
the census, not on the hosted run.

Changed-document gates on the delivered record and plan, run against a `git stash create` commit of
the final working tree (`--base 33ad88a9…`):

| Command | Exit | Result |
| --- | --- | --- |
| `python3 scripts/ci/check-record-cleanliness.py` | 0 | `1 changed record(s) pass the root-only check` |
| `python3 scripts/ci/check-citation-existence.py` | 0 | `1 changed document(s) pass the path and symbol check (600 path citation(s))` |
| `git diff --check` | 0 | clean |
| host-path and profile-directory scans: the sanitize tool's root set over the record and plan → 0 hits; the case-insensitive profile-directory scan over the branch-diff added lines → 0 hits, over every changed file at a `git stash create` commit → 1 hit that is the substring inside the pre-existing identifier `ensureLocalHandleAvailableLocked` in `docs/archive/CLOSED-TODO.md` (not a path) | 0 | no host absolute path and no profile-directory path in any changed document |

## 15. Fix round 3 (2026-11-25) — the PR #280 round-2 ruling, items 1-3

Source: the contract's binding ruling "PR #280 round 2", reconciling Sol
`findings/280-opus-review-2.md` (P2-1, P3-1, P3-2) and DS `findings/280-ds-audit-2.md` (P3-1,
P3-2); Sec280 r2 found none. Changed rows (§1): `scripts/ci/linux-v2-native-acceptance.sh` — the
`machine_run` root fix, 3 insertions / 3 deletions, line count unchanged (3001), every other byte
unchanged — plus the plan and this record. No Go file moved, so every Go line reference of §12/§13
survives; the script's line stability keeps §4/§14's refs valid, and §15.2 lists the refs that
still needed refreshing. Lessons 74-79/82 apply: the probe ran before the dispatch, the "every"
clause got a census, artifacts go in verbatim with full sha256, and records cite bytes, not
commits.

### 15.1 Item 1 — the root fix, the extended probe, the call-form census

Root fix (`linux-v2-native-acceptance.sh:937-944`), diff against the round-2 bytes verbatim:

```diff
@@ -936,7 +936,9 @@ pin_guest_cgroup_while_active || fail 'could not pin the live disposable guest u

 machine_run() {
   machine_run_count=$((machine_run_count + 1))
-  local unit="${machine}-step-${machine_run_count}"
+  # A command substitution runs this in a subshell: its increment dies there, so the counter alone
+  # would repeat a name; the subshell's own BASHPID keeps every call form's unit unique.
+  local unit="${machine}-step-${machine_run_count}-${BASHPID}"
   sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect \
     --unit="$unit" -- "$@"
 }
@@ -945,8 +947,6 @@ machine_run_as() {
   local user=$1
   local group=$2
   shift 2
-  # The passwd read runs here, not in a command substitution: its machine_run only advances the
-  # counter in this shell, while a subshell would lose the increment and still burn the unit name.
   local passwd_log="$fixture_root/machine-run-as-passwd.log"
```

The seven `$(machine_run …)` sites carry the counter into their subshell where the increment dies,
so the counter alone names a unit a later parent call reuses; the subshell's own BASHPID makes the
name unique whatever the call form, which is why the round-2 `machine_run_as` parent-shell capture
becomes a readability choice rather than the only protection.

Probe: `unit-name-probe.sh`, sha256
`9eb89fbf474586cc5d7f984a19c152250a57b588de6fc9eab6c44ba4938f9a31` (verbatim §15.5), extends the
§14.7 probe: it extracts `machine_run`/`machine_run_as` verbatim, stubs the guest transport, and
calls both parent forms and two `$(machine_run …)` forms (an assignment and a
`[[ -n "$(…)" ]]` condition) interleaved. Run under WSL Ubuntu-24.04 (bash 5.2.21) against the
final script (sha256 `0903c8e25de5b16a1936a586261b3f4c7c7b32a0224b3eee5b4c1a1da066173c`) and the
BASHPID-removed mutant (sha256
`523cd3cbabd3e5f5bad59dddc859d70eb707e24c880cd9efd495beebc79035d0`, produced by §15.5's python
from the final bytes, one substring removed).

Final bytes, exit 0:

```text
machine_run_as  A A first
machine_run     parent plain
machine_run     substitution assignment (interleaved)
machine_run_as  A A second
machine_run     substitution condition (interleaved)
machine_run_as  B B third
machine_run     parent plain
--- unit sequence (creation order)
symmetry-test-machine-step-1-51296
symmetry-test-machine-step-2
symmetry-test-machine-step-3-51296
symmetry-test-machine-step-4-51302
symmetry-test-machine-step-4-51296
symmetry-test-machine-step-5
symmetry-test-machine-step-6-51305
symmetry-test-machine-step-6-51296
symmetry-test-machine-step-7
symmetry-test-machine-step-8-51296
--- outer HOME values
--setenv=HOME=/synthetic-guest-home/symmetry-a
--setenv=HOME=/synthetic-guest-home/symmetry-a
--setenv=HOME=/synthetic-guest-home/symmetry-b
units=10 duplicates=0 parent_steps=5 parent_monotonic=1
PROBE-PASS: all transient unit names unique; parent step names strictly increasing
```

BASHPID-removed mutant, exit 3:

```text
machine_run_as  A A first
machine_run     parent plain
machine_run     substitution assignment (interleaved)
machine_run_as  A A second
machine_run     substitution condition (interleaved)
machine_run_as  B B third
machine_run     parent plain
--- unit sequence (creation order)
symmetry-test-machine-step-1
symmetry-test-machine-step-2
symmetry-test-machine-step-3
symmetry-test-machine-step-4
symmetry-test-machine-step-4
symmetry-test-machine-step-5
symmetry-test-machine-step-6
symmetry-test-machine-step-6
symmetry-test-machine-step-7
symmetry-test-machine-step-8
--- outer HOME values
--setenv=HOME=/synthetic-guest-home/symmetry-a
--setenv=HOME=/synthetic-guest-home/symmetry-a
--setenv=HOME=/synthetic-guest-home/symmetry-b
units=10 duplicates=2 parent_steps=0 parent_monotonic=1
PROBE-FAIL: unit names are not unique and parent-monotonic
```

The two interleaved substitution names (`step-4-51302`, `step-6-51305`) share a counter value with
the parent's next name and stay distinct through BASHPID; without the suffix the same probe
reproduces `step-4`/`step-6` duplicates (mutant row).

Census of every `machine_run`/`machine_run_as` call form (`grep` + classifier, both verbatim in
§15.5; cwd the worktree root): 105 token lines = 2 definitions (`:937`, `:946`) + 103 call sites
(64 `machine_run`, 39 `machine_run_as`):

| call form | sites (final bytes) |
| --- | --- |
| plain parent statement (bare with errexit; a multi-line call counts at its `machine_run` line) | 83: `:974`, `:975`, `:976`, `:977`, `:978`, `:982`, `:1001`, `:1003`, `:1004`, `:1006`, `:1008`, `:1010`, `:1011`, `:1351`, `:1352`, `:1399`, `:1533`, `:1539`, `:1574`, `:1603`, `:1615`, `:1616`, `:1617`, `:1618`, `:1625`, `:1639`, `:1645`, `:1649`, `:1656`, `:1662`, `:1667`, `:1673`, `:1674`, `:1677`, `:1678`, `:1679`, `:1680`, `:1699`, `:1701`, `:1721`, `:1727`, `:1836`, `:1864`, `:1874`, `:1884`, `:1930`, `:1931`, `:1932`, `:1938`, `:1939`, `:1961`, `:1967`, `:2014`, `:2135`, `:2212`, `:2214`, `:2250`, `:2265`, `:2267`, `:2304`, `:2374`, `:2449`, `:2465`, `:2489`, `:2506`, `:2509`, `:2528`, `:2545`, `:2565`, `:2574`, `:2613`, `:2777`, `:2819`, `:2820`, `:2849`, `:2850`, `:2851`, `:2852`, `:2855`, `:2859`, `:2860`, `:2861`, `:2998` |
| `if` condition | 9: `:983`, `:986`, `:1353`, `:1356`, `:1400`, `:1534`, `:1632`, `:2015`, `:2136` |
| `||` chain | 4: `:951`, `:993`, `:995`, `:997` |
| command substitution `$(machine_run …)` | 7: `:1657`, `:1658`, `:1659`, `:1664`, `:1668`, `:1669`, `:1670` |
| pipeline | 0 — `grep -n 'machine_run.*|'` has no match; round 1 converted the only two (`machine_run_as … \| print_filtered_output_lines`) to capture-then-filter (§4 P3-3) |

Every substitution site is `machine_run`; no `machine_run_as` call site is a substitution (all 39
are parent-shell plain/condition/`||` forms), and the round-1 parent-shell capture in
`machine_run_as` stays per the ruling. Transient-name census for the "every transient unit name
is unique" clause: `machine_run` names carry counter + BASHPID (`:941`); the `machine_run_as`
outer unit (`:957`) reserves the counter in the parent shell at every call site; `-go-test-n`
reserves the same counter in the parent shell at `:1540`/`:1768` (both callers are parent-shell
forms); `-ready` (`:967`) is one fixed name retried by one `--collect` loop; `-poweroff` (`:289`)
is a single-shot cleanup; `-helper-exit-control-n` (`:2656-2658`) counts in its own parent-shell
callers (7 call sites, none a substitution); `$PID_REUSE_WATCHER_UNIT` (`:501`) is started once
per set and stopped at `:2859`.

### 15.2 Item 2 — records refreshed

- DS P3-1 stale refs fixed: §4 P3-1 `:1563`→`:1567`, P3-2 `:1623-1637`→`:1637-1653`, P3-3
  `:1706-1708`/`:1713-1715`→`:1721-1723`/`:1727-1730`; §2's and §13 STD-01's
  `linux-v2-native-acceptance.sh:2936`→`:2955`; plan §2's eight report refs → `:1751`, `:2384`,
  `:3394`, `:3573`, `:3804`, `:3970`, `:4129`, `:4451` (the barrier wait `:4451`) and its four
  recovery/replay refs → `:1848-1850`, `:3472-3473`, `:3707-3708`, `:4522-4523`.
- The re-verification pass (§15.4) found four more ref sets stale at the final bytes and fixed
  them: §12's F1 bullet and §13's STD-01 carried the apply-bytes report/exemption refs (`:1744,…`;
  §13 STD-01 now also names the round-2 shift); §12's F4 comment ref `:1669-1670`→`:1684-1685`;
  §12's predicate-census refs `owner_cli_test.go:117`/`:133`→`:124`/`:140`; §14.8's watcher range
  `:1956-2001`→`:1956-2002`.
- Env qualifier: §2, §11, §14.3 and plan §1 now say "the capture-bearing cases' `--setenv` lists
  carry no `*_TOKEN`/`*_API_KEY` entry" and name the `legacy-v1` counterexample (`:1897-1898`,
  two dev dummy tokens); that case never starts the owner-witness command or writes a capture, so
  the residual's conclusion is unchanged and the guest env stays deliberately unwidened.
- §14.2's `:1996` moved out of the host-authored exemption row into the round-2 captures row,
  labelled guest-side, captured at `:1999` and filtered at `:2000` (the same section's round-2
  row already said so; the census had double-counted the site).
- §8's implement-stage gate table retitled `(implement stage)`; §11's round-2 delta bullet names
  the round-3 run; §14.1 retitled and its census paragraph refreshed to the root fix; §14.8's
  "all six equal the §1 table" qualified to the round-2 values.

### 15.3 Item 3 — the binding all-14 run on the final bytes (`45495704270`, head `a9abd537`)

One fresh literal all-14 run, waited for in the foreground (`gh run watch 45495704270 --repo
wxxb789/symmetry --interval 60 --exit-status`): `contracts` passed in 1m4s and
`linux-native-v2-acceptance` (job 138806105421) ran 22m36s with all 14 case sets armed, on the
throwaway workflow and the ratified gh shim whose §10 pins recompute at the head (`a3aa2b1c…`,
`507340af…`). The scratch head `a9abd537d8e0073a8d91e7d8e841490f251a3221` (parent `5e2c6dfd`) is
the final working tree (rsync, per-file sha256 equality below) plus the two scratch-only files;
the branch was deleted after the run (`git push origin --delete
scratch/rf-acc-native-containment-witness` →
`- [deleted] scratch/rf-acc-native-containment-witness`), `git ls-remote origin
scratch/rf-acc-native-containment-witness` prints nothing (0 lines, exit 0), and `git ls-remote
origin | grep -c scratch` = 0 (the unrelated `scratch/rf-acc-pi-durability-matrices` ref §14.8
mentions has since been deleted by its own stage).

sha256 equality, scratch head vs the delivered rows of §1 (`git cat-file blob HEAD:<path>` piped
to `sha256sum` on both sides): `production_linux_v2_owner_cli_test.go` `71980151…`;
`native_witness_test.go` `29366dd8…`; `cleanup_witness_test.go` `d120677d…`;
`linux-v2-native-acceptance.sh` `0903c8e2…`; `export-linux-v2-redacted-evidence.py` `e3c2c155…`;
`test-linux-v2-evidence.py` `00342bc2…`; plan `ac84fd3b…` — 7/7 EQ. Only this record postdates the
run (its head blob is `caee9029…`; the final bytes add this section) plus the out-of-tree PR body;
every other delivered file equals the scratch head.

Per-set comparison with `45486208881` (both logs downloaded, 19709/19712 lines): the 91-line stop
signature — 68 `_test.go:<line>:` lines + 23 `--- FAIL:` lines (19 distinct failing test names),
the captured `owner-witness` status/stderr lines included — normalizes for timestamps, durations
and scratch temp-dir names to a **0-line diff**; the per-set order and the first failing lines
match row for row:

| # | Case set | First failing line (both runs) |
| --- | --- | --- |
| 1-2 | `codex_containment`, `claude_containment` | `production_linux_v2_native_witness_test.go:101`/`:109` `owner-witness command exit status: 1` + captured `-linux-v2-native-owner-witness: reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject` (§7.2 `DECISION_NEEDED`) |
| 3 | `shared` | `clone_cleanup_linux_test.go:259`, `clone_integration_linux_test.go:240` + `:113`, `launcher_integration_linux_test.go:439` |
| 4 | `synthetic_goal_cleanup` | `production_linux_v2_cleanup_witness_test.go:499` past the old admission refusal, at `… one-use Linux v2 RELEASE result is unresolved: … release_outcome_unknown`, + its `:488` cleanup line |
| 5-14 | the ten OpenCode-vehicle sets | their own stage waits (`:178`, `:217`, `:630`, `:616`, `:534`, `:269`) + the captured version-gate line |

Ledger counts, `45486208881` → `45495704270`:

| Pattern | `45486208881` | `45495704270` |
| --- | --- | --- |
| case sets exited 1 (same names, same order) | 14 | 14 |
| `owner-witness command exit status: 1` | 12 | 12 |
| `stayed live` / `requires an App admission check` / `fixed-B native HOME is outside` | 0 / 0 / 0 | 0 / 0 / 0 |
| `native harness version differs from the tested release` | 220 | 220 |
| `Linux clone intent does not match the current admitted Subject` | 4 | 4 |
| clones / daemon smokes / `retired` histogram | 14 / 14 / `0`×1 `5`×13 | 14 / 14 / `0`×1 `5`×13 |
| verified legs (`…RejectsUnexpectedCgroupProcsFD`, `…CleanupRejectsBCallerScratch`) | 1 + 1 | 1 + 1 |
| installer `Verified fixed Linux owner assets` / `one-time legacy drain proof passed` | 14 / 14 | 14 / 14 |
| daemon `-verify-linux-owner-install` boundary / `read-only preflight does not launch B…` lines | 220 / 220 | 220 / 220 |
| watcher (`literal PID-reuse watcher`) lines | 0 | 0 |
| round-2 failure strings (`did not install the staged owner artifacts`, `refused the staged daemon config`, `watcher did not become ready`, the passwd refusals) | 0 each | 0 each |
| `Failed to start transient unit` / `already exists|already loaded|already active` | 0 / 0 | 0 / 0 |

What the run exercised of round 3: the `machine_run` BASHPID name reached every set's step calls
with no unit-name failure (0 `Failed to start transient unit`, 0 `already exists`), the
`machine_run_as` passwd read ran on every set's `--setenv=HOME` path with no refusal, and the
watcher capture path (`:1956-2002`) was again not reached (0 lines; the sets stop before
`run_literal_pid_reuse_witness` at `:2959`). No new stop class; nothing previously green
reddened.

### 15.4 Re-verification of every cited line at the final bytes

A resolver extracted every `:N`/`:N-M` citation from this record and the plan, resolved each to
its target file (full path on the line, else the enclosing bullet/paragraph/table row, else the
script in the script-specific sections) and printed the cited line from the final bytes; every
one was checked against its claim:

- the script's 52 `>&2`/`2>&1` sites: §14.2 cites all 52 (recomputed membership on the final
  bytes: sites minus cited = none; the extra refs are the paired capture/print sites plus
  `:204-206`); §4's refreshed ranges, §7.1's `:570`/`:612-613`/`:950-953`, §2's `:1751…`/`:1848…`,
  §12's F4 `:1684-1685` and §14.1/§15.1's `:941`/`:946-962`/`:950-957` resolve to the claimed
  lines;
- `owner_cli_test.go`: the eight report call sites, the four recovery/replay waits, the sink
  `:52-63`, the report `:1580-1598`, `linuxV2IsCredentialEnvironmentName` call sites `:124`/`:140`
  (fixed), and `native_witness_test.go:3407`/`:3419`;
- `native_witness_test.go`: `:101`, `:109`, `:178`, `:217`, `:269`, `:449-453`, `:495`, `:534`,
  `:616`, `:630`, `:948`, `:1565-1567`, `:1838`, `:1991-1999`, `:2001-2081`, `:2008`, `:2025`,
  `:2041-2066`, `:2051`, `:2083`, `:2106`, `:2126-2128`, `:2147`, `:2167`, `:2180`, `:3407`,
  `:3419`;
- `cleanup_witness_test.go` `:1050-1077`, `:1078-1081`, `:499`, `:488`, and the other e2e files
  the §6 rows cite (`clone_cleanup_linux_test.go:259`, `clone_integration_linux_test.go:240` and
  `:113`, `launcher_integration_linux_test.go:439`);
- `scripts/ci/*`: exporter `:707`, `:715-718`, `:1545`, `:1552`, `:1560`; suite tests `:1619`,
  `:1626`, `:1636`; `provision-linux-v2-control-claims.exs:415-421`; the fixture's
  `omo-control-native-fixture.exs:190,262-284,297-303,376`; the script's `:289`, `:501`, `:570`,
  `:612-613`, `:941`, `:950-957`, `:1540-1541`, `:1567`, `:1603-1614`, `:1611-1612`,
  `:1625-1630`, `:1626-1628`, `:1637-1653`, `:1641-1652`, `:1684-1685`, `:1721-1730`,
  `:1897-1898`, `:1956-2002`, `:1977-2000`, `:2656-2658`, `:2955`, `:2959`;
- `daemon/internal/**`: `linux_owner_v2_linux.go:339-341`, `:1054`, `:1066`, `:1107-1108`,
  `:1118`; `native_run.go:210-214`; `linux_cgroup_journal.go:520-537`; `helper_v2_server.go:1188`;
  `linux_owner_witness_linux.go:503-505`, `:1166-1174`, `:2625-2633`, `:2902-2904`, `:2981`;
  `linux_clone_journal.go:208-216`, `:864-878`; `goal_admission.go:486-490`, `:540-546`.

The §1 hashes, §10 pins, §15.5 probe/mutant/census pins and the §14.2 census membership were
recomputed from the final bytes. The round-3 fix kept the script's line count, so §4/§14's line
refs needed no renumbering beyond the DS list; the four extra stale sets are the ones §15.2
lists.

### 15.5 Appendix — round-3 probe, mutant and census artifacts, verbatim

`unit-name-probe.sh`, sha256
`9eb89fbf474586cc5d7f984a19c152250a57b588de6fc9eab6c44ba4938f9a31`:

```bash
#!/usr/bin/env bash
# Local probe (conductor lesson 74): unit-name sequence of machine_run/machine_run_as after the
# round-3 root fix, including command-substitution call sites interleaved with parent calls.
# Usage: unit-name-probe.sh <script>; exit 0 only when every transient unit name is unique and
# the parent-shell step names stay strictly increasing.
set -Eeuo pipefail

script=$1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fixture_root=$work
machine=symmetry-test-machine
machine_run_count=0
units_log="$work/units"
sudo_log="$work/sudo-lines"
: > "$units_log"
: > "$sudo_log"

fail() { printf 'PROBE-FAIL: %s\n' "$1" >&2; exit 3; }

# Emulates systemd-run: records the unit name, answers a getent passwd read, and relays one
# stdout line for every other command (--pipe does).
sudo() {
  printf '%s\n' "$*" >> "$sudo_log"
  local arg unit='' user='' i saw_getent=0
  local -a args=("$@")
  for ((i = 0; i < ${#args[@]}; i++)); do
    arg=${args[i]}
    [[ "$arg" == --unit=* ]] && unit=${arg#--unit=}
    if [[ "$arg" == /usr/bin/getent ]]; then
      saw_getent=1
      user=${args[i + 2]}
      echo "$user:x:52000:52000::/synthetic-guest-home/$user:/bin/bash"
    fi
  done
  printf '%s\n' "$unit" >> "$units_log"
  (( saw_getent )) || printf 'relayed stdout for %s\n' "$unit"
}

eval "$(sed -n '/^machine_run() {/,/^}/p' "$script")"
eval "$(sed -n '/^machine_run_as() {/,/^}/p' "$script")"

printf 'machine_run_as  A A first\n'
machine_run_as symmetry-a symmetry-a /usr/bin/true >/dev/null
printf 'machine_run     parent plain\n'
machine_run /usr/bin/true >/dev/null
printf 'machine_run     substitution assignment (interleaved)\n'
substituted=$(machine_run /usr/bin/true)
[[ -n "$substituted" ]] || fail 'substitution returned nothing'
printf 'machine_run_as  A A second\n'
machine_run_as symmetry-a symmetry-a /usr/bin/true >/dev/null
printf 'machine_run     substitution condition (interleaved)\n'
[[ -n "$(machine_run /usr/bin/true)" ]]
printf 'machine_run_as  B B third\n'
machine_run_as symmetry-b symmetry-b /usr/bin/true >/dev/null
printf 'machine_run     parent plain\n'
machine_run /usr/bin/true >/dev/null

echo '--- unit sequence (creation order)'
cat "$units_log"
echo '--- outer HOME values'
grep -o -- '--setenv=HOME=[^ ]*' "$sudo_log" || true

mapfile -t units < "$units_log"
duplicates=$(printf '%s\n' "${units[@]}" | sort | uniq -d | wc -l)
parent_counters=()
for u in "${units[@]}"; do
  rest=${u#"$machine"-step-}
  [[ "${rest##*-}" == "$BASHPID" ]] && parent_counters+=("${rest%%-*}")
done
monotonic=1
prev=0
for n in "${parent_counters[@]}"; do
  (( n > prev )) || monotonic=0
  prev=$n
done
printf 'units=%d duplicates=%d parent_steps=%d parent_monotonic=%d\n' \
  "${#units[@]}" "$duplicates" "${#parent_counters[@]}" "$monotonic"

grep -q -- '--setenv=HOME=/synthetic-guest-home/symmetry-b' "$sudo_log" ||
  { echo 'PROBE-FAIL: the B run did not receive the passwd home' >&2; exit 3; }
(( duplicates == 0 && monotonic == 1 )) ||
  { echo 'PROBE-FAIL: unit names are not unique and parent-monotonic' >&2; exit 3; }
echo 'PROBE-PASS: all transient unit names unique; parent step names strictly increasing'
```

Run driver `run-probes.sh` (sha256
`3e73a54a432f67a00551a6c3d99ed08c405855be657c09cac6dbd7068aff5e95`; built the mutant, then ran
the probe twice; the recorded final-bytes output sha256 is
`5029c535da4d4cbf8384635ed064f7fb989bf3994a562ca6eddd1189b1dea431`, the mutant output sha256
`e01b22a67c2fd675f2eee1b09fb8c4bb90e5d207ba185586677c78283e3dd4f5`):

```bash
#!/usr/bin/env bash
set -Eeuo pipefail
work=$(cd "$(dirname "$0")" && pwd)
repo=<REPO>
script="$repo/scripts/ci/linux-v2-native-acceptance.sh"
probe="$work/unit-name-probe.sh"
mutant="$work/mutant-bashpid-removed.sh"

python3 - "$script" "$mutant" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src, encoding='utf-8').read()
needle = '-${BASHPID}'
assert text.count(needle) == 1, text.count(needle)
open(dst, 'w', encoding='utf-8', newline='').write(text.replace(needle, ''))
PY

echo "probe sha256:  $(sha256sum "$probe" | cut -d' ' -f1)"
echo "script sha256: $(sha256sum "$script" | cut -d' ' -f1)"
echo "mutant sha256: $(sha256sum "$mutant" | cut -d' ' -f1)"

set +e
bash "$probe" "$script" > "$work/final.out" 2>&1
final_status=$?
bash "$probe" "$mutant" > "$work/mutant.out" 2>&1
mutant_status=$?
set -e

echo "=== final-bytes probe output (exit $final_status) ==="
cat "$work/final.out"
echo "=== BASHPID-removed mutant probe output (exit $mutant_status) ==="
cat "$work/mutant.out"
```

The block above is the run artifact with its one `repo=` line replaced by `<REPO>` (the artifact
outside the repository keeps the literal path and has the sha256 given above).

The mutant is the final script's bytes with the single `-${BASHPID}` substring removed; sha256
`523cd3cbabd3e5f5bad59dddc859d70eb707e24c880cd9efd495beebc79035d0`; the driver asserts the
needle count is exactly one.

Census artifact `machine-run-census.py`, sha256
`3b761f0c81db2bb9fb464eaf4f2a8e14de38206a259b39e4b6c15e986ac78687`, run under WSL Ubuntu-24.04
(python 3.12.3) with cwd the worktree root; output sha256
`b426a14f7055185d5052dcff84218692d145a7c21c56dab9638b745bd1e62133`:

```python
import re, pathlib
script = pathlib.Path('scripts/ci/linux-v2-native-acceptance.sh').read_text(encoding='utf-8').splitlines()
call = re.compile(r'(?<![A-Za-z0-9_])machine_run(_as)?(?![A-Za-z0-9_])')
forms = {'plain': [], 'condition': [], 'or-chain': [], 'substitution': [], 'pipeline': []}
definitions = []
for i, line in enumerate(script, 1):
    stripped = line.strip()
    if stripped.startswith('machine_run()') or stripped.startswith('machine_run_as()'):
        definitions.append(i)
        continue
    m = call.search(line)
    if not m:
        continue
    pre, post = line[:m.start()], line[m.end():]
    if '$(' in pre:
        form = 'substitution'
    elif re.match(r'^\s*(if|elif|while)\b', pre):
        form = 'condition'
    elif post.strip().startswith('||') or re.search(r'\|\|\s*$', post):
        form = 'or-chain'
    elif '| print_filtered_output_lines' in post or post.strip().startswith('| '):
        form = 'pipeline'
    else:
        form = 'plain'
    forms[form].append(i)
for form in ('plain', 'condition', 'or-chain', 'substitution', 'pipeline'):
    sites = forms[form]
    print(f'{form}: {len(sites)}')
    if sites:
        print('  ' + ', '.join(f':{s}' for s in sites))
print('definitions:', ', '.join(f':{d}' for d in definitions))
print('call sites total:', sum(len(v) for v in forms.values()))
```

```text
plain: 83
  :974, :975, :976, :977, :978, :982, :1001, :1003, :1004, :1006, :1008, :1010, :1011, :1351, :1352, :1399, :1533, :1539, :1574, :1603, :1615, :1616, :1617, :1618, :1625, :1639, :1645, :1649, :1656, :1662, :1667, :1673, :1674, :1677, :1678, :1679, :1680, :1699, :1701, :1721, :1727, :1836, :1864, :1874, :1884, :1930, :1931, :1932, :1938, :1939, :1961, :1967, :2014, :2135, :2212, :2214, :2250, :2265, :2267, :2304, :2374, :2449, :2465, :2489, :2506, :2509, :2528, :2545, :2565, :2574, :2613, :2777, :2819, :2820, :2849, :2850, :2851, :2852, :2855, :2859, :2860, :2861, :2998
condition: 9
  :983, :986, :1353, :1356, :1400, :1534, :1632, :2015, :2136
or-chain: 4
  :951, :993, :995, :997
substitution: 7
  :1657, :1658, :1659, :1664, :1668, :1669, :1670
pipeline: 0
definitions: :937, :946
call sites total: 103
```

### 15.6 Round-3 gates (final bytes)

| Command | Exit | Result |
| --- | --- | --- |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | clean |
| `shellcheck --severity=warning -f gcc` on the script (Windows 0.11.0 / WSL 0.9.0) | 1 / 1 | 16 findings on each; line:col- and path-stripped finding multiset delta 0 versus the base script (`33ad88a9` and `477e4c62` are the same bytes, sha256 `f9399352…`) on both binaries; histogram `SC1078 ×1, SC2024, SC2043, SC2154 ×8, SC2174, SC2276 ×2, SC2288, SC2289` (the WSL build exits 1 this round, as in §14.6; §8/§13's exit-0 note is historical) |
| `gofmt -l .` (`daemon/`, WSL) | 0 | 0 lines |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./e2e/...` (`daemon/`, WSL) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o <tmp>e2e-capture.test ./e2e` then that binary `-test.run TestLinuxV2OwnerWitnessStderrCapture -test.v` (WSL linux/amd64) | 0 | 4/4 PASS |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL, python 3.12.3) | 0 | `Ran 48 tests in 39.656s` … `OK` |
| `git diff --check` (final tree) | 0 | clean |
| record sanitize: `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record and the plan | 0 / 0 | no host absolute path in either document |
| host-path root-set scan (`/home /Users /root /tmp /var /mnt /private`, drive letters) over this record and the plan | 0 hits | no host path |
| case-insensitive profile-directory scan over the changed lines of a `git stash create` commit | 1 hit | the substring inside the pre-existing identifier `ensureLocalHandleAvailableLocked` quoted by this record's own §14.8 row (not a path); the same documented false positive as round 2 |
| `python3 scripts/ci/check-record-cleanliness.py --base 33ad88a9… --head <stash-create commit>` | 0 | `1 changed record(s) pass the root-only check` |
| `python3 scripts/ci/check-citation-existence.py --base 33ad88a9… --head <stash-create commit>` | 0 | `1 changed document(s) pass the path and symbol check (600 path citation(s))`; the three pre-existing `CLOSED-TODO.md` ignore notes unchanged |
| unit-name probe / census (§15.5) | 0 / 0 | final bytes `PROBE-PASS`; the BASHPID-removed mutant exits 3; the census lists 103 call sites |
