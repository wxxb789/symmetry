# rf-acc-native-containment-witness — plan

Contract: `goal-rf-acc-native-containment-witness.md` (GDD state root, external to the repository).
Shape reference: run `45388258526` / record `docs/evidence/rf-acc-native-burndown-2026-11-24.md`
§3.13, §3.13.1, §8; base `477e4c62` (#277). Branch: `refactor/rf-acc-native-containment-witness`.

This plan records only the implementation choices the accepted contract leaves unresolved.

## 1. Change 1 — capture sink, redaction, and failure reporting

- Sink: `fixture.artifactDir/owner-witness.stderr`, opened
  `O_CREATE|O_TRUNC|O_WRONLY` mode 0600 exactly like the sibling `owner-witness-response.json`,
  and kept as `command.Stderr` for the whole witness lifetime. Each command start truncates it,
  so a report reads the tail of the last-started command. Commands can share one fixture's
  capture: the unresolved-journal witness starts its fresh command while the setup command's
  owner processes are still live (`production_linux_v2_native_witness_test.go:495`, its live
  PIDFD fences at `:449-453`), so that start truncates a capture the setup command's live process
  tree may still hold open; no report reads the capture in that window, so the interleave stays
  latent. `artifactDir` is an A-private `os.MkdirTemp` root, so the file is A-owned and outside
  every exporter allowlist.
- Tail helper (shared redaction, owned file `production_linux_v2_native_witness_test.go`):
  `linuxV2OwnerWitnessStderrTail(path, request) ([]string, error)` reads the file, replaces every
  known credential value in the whole text with one constant marker (`<redacted>`), then refuses —
  returns an error and no text — if a known value survives in the marker-stripped view (so a value
  that is a substring of the marker cannot veto the tail, and a value redaction split is refused),
  and only then keeps the last ≤40 lines / ≤4 KiB as the returned window.
  Known values: every `os.Environ` entry whose name matches `linuxV2IsCredentialEnvironmentName`
  (which covers the writer's `SYMMETRY_OPERATOR_TOKEN` / `SYMMETRY_ENROLLMENT_TOKEN` cases,
  `writeLinuxV2NativeWitness`), plus the request's `Claim.LeaseToken` and
  `Claim.ProviderAccess.Token`. Empty values are skipped, like the writer skips them.
- Reporting (`owner_cli_test.go`): `linuxV2ReportOwnerWitnessFailure(t, fixture, command)` first
  waits ≤5 s for the pidfd to exit (`waitLinuxV2OwnerWitnessCommand`, `allowNonzero=true`) and
  logs `owner-witness command exit status: <n>` (`linuxV2OwnerCommandExitCode`) or the
  still-live error, then logs the tail as separate `t.Logf` lines. Line budget: 1 status line,
  then last ≤5 tail lines, each truncated to ≤300 content bytes so the test-log prefix keeps the
  whole `go test -json` output line under the fixture printer's 400-byte cap; the text after the
  `owner-witness stderr: ` prefix is the redacted App diagnostic verbatim. The fixture printer
  keeps the last 10 lines of a test, and four more lines (`=== RUN`, the `t.Fatalf` body, the
  fixture's StateDir cleanup line, `--- FAIL`) share that window, so 1 status + 5 tail + 4 fills
  it exactly; `scripts/ci/test-linux-v2-evidence.py` runs the printer on the maximum report stream
  and fails when the status line or the first tail line leaves the window. A refused or empty tail
  logs one line instead of the tail.
- Mutant-checkable semantics: removing the sink replacement restores `/dev/null` and the rows go
  back to an unnamed 30 s barrier wait; removing `CheckNativeAdmission` restores the `:499`
  refusal.

## 2. Change 1 — call sites instrumented

The contract names the barrier wait and the stage waits (its base-tree refs `:4414`, `:1721`,
`:3770`, `:3935`). Nine `waitLinuxV2OwnerWitnessStage` call sites exist; the eight in
`production_linux_v2_owner_cli_test.go` hold the live command and share the same failure shape,
so every one of them reports through the new helper before `t.Fatalf`: `:1744`, `:2377`, `:3387`,
`:3566`, `:3797`, `:3963`, `:4122`, `:4444` (the barrier wait at `:4444` included; four of the
eight are report-only and cannot change pass behaviour — leaving them silent would keep the
`ack_loss` and helper-response-loss rows unnamed in the same run). Two groups stay unnamed by
explicit exemption and outside the eight: the ninth call site in
`daemon/e2e/production_linux_v2_helper_exit_witness_test.go:188` (the helper-exit witness the
`shared` set reaches at `scripts/ci/linux-v2-native-acceptance.sh:2936`, a file this goal's owned
set does not cover) and the recovery/replay exit waits on a live command
(`production_linux_v2_owner_cli_test.go:1841-1843`, `:3465-3466`, `:3700-3701`, `:4515-4516`),
whose rows stop at their own named stage waits first.

## 3. Change 2 — cleanup witness admission callback

`linuxV2CleanupJournalCallbacks` gains
`CheckNativeAdmission: func() error { return store.CheckLinuxV2NativeAdmission(key, fence, time.Now().UTC(), nil) }`
— the production App's own Store CAS (`native_run.go:210-214`) with the fixture's nil live-owner
map. The launcher guard (`linux_owner_v2_linux.go:339-341`) and every `daemon/internal/**` byte
stay untouched. The callback runs pre-RELEASE (`:1054`), so the set still exercises the real
launcher path.

## 4. Carry-over P3s in `scripts/ci/linux-v2-native-acceptance.sh`

- P3-1: the `shared` set's unprovisioned-gate failure branch pipes its `grep | head -n 5`
  through `print_filtered_output_lines`, like the `run_go_test` twin.
- P3-2: both Change-1 `systemctl show` blocks and the `journalctl --output=cat -n 80 --grep=…`
  dump are captured to a host file under `$fixture_root` and then printed through
  `print_filtered_output_lines`; the narrow `--grep` and the `-n 80` bound are unchanged.
- P3-3: the two probe pipelines become capture-then-filter (`machine_run_as … >"$log" 2>&1 ||
  true`, then `print_filtered_output_lines <"$log"`), so `machine_run_as` runs in the parent
  shell and `machine_run_count` advances; transient unit names stay unique. A capture file in
  the parent shell is required because command substitution is itself a subshell.

## 5. Exporter exclusion (Q3 ruling)

`scripts/ci/export-linux-v2-redacted-evidence.py` gains `EXCLUDED_WITNESS_NAMES =
("owner-witness.stderr",)` and fails closed in all three modes when any input root carries one of
those names or when any computed export source path carries the name as a path component. A
successful export therefore can never publish the capture, and a future allowlist entry that
named it — top level or the subpath shape the manual allowlist already uses, such as
`stage-replay/` — refuses instead of leaking. `test-linux-v2-evidence.py` gains the
mutant-checkable rows: a containment source root carrying `owner-witness.stderr` refuses
publication and writes no manifest, the guard refuses a computed subpath source, and a compose or
legacy witness root carrying the capture refuses too.

## 6. Evidence plan (scratch loop)

Branch `scratch/rf-acc-native-containment-witness`, throwaway workflow
`.github/workflows/scratch-native-containment-witness.yml` (the real job minus
`environment`/`secrets`/`vars`, contract dummy values, `GITHUB_EVENT_NAME=workflow_dispatch`)
plus the ratified scratch-only `scripts/ci/scratch-gh-shim` (the fixture's `gh auth login`
refuses the dummy `gho_` token). Budget: the two named failure classes + any fixture class the
captured refusal names, plus 2 runs.

1. Narrow run: `codex_containment`, `claude_containment`, `synthetic_goal_cleanup`, `migration`.
   Discriminating checks: the containment rows print the redacted App stderr tail and the exit
   status; `synthetic_goal_cleanup` passes `:499`; `migration` keeps its F2-excluded version-gate
   stop.
2. Classify the named refusal; fix it only when it names a missing fixture precondition (ruling
   Q1); a `daemon/internal/**` cause stops the goal at `DECISION_NEEDED` with the captured line.
3. Final literal all-14 run on the PR-head bytes, waited for in the foreground
   (`gh run watch … --interval 60`), per-set rows read with `gh run view <id> --log-failed`.
4. Mutant rows: Change 1 before = `45388258526` (rows 1-2 unnamed, no App stderr in the log);
   Change 2 before = `45388258526` (`:499` `requires an App admission check`). If the budget
   allows, one revert run per change; otherwise the before run is the mutant.
5. Delete the scratch branch and prove it with `git ls-remote origin …` printing nothing.

Local checks before any dispatch (conductor lessons 74-79): a real `go test -json` probe that a
`t.Logf` of a failing test reaches the fixture's `print_witness_output_lines` filter (positive and
negative), the redaction helper's unit test (bounds, redaction, refusal), and the exporter test.

## 7. Out of scope with reason

- `daemon/internal/**`: not owned; a product-rooted refusal stops at `DECISION_NEEDED` (ruling
  Q1) and is recorded with the captured line.
- The real workflow `.github/workflows/linux-native-v2-acceptance.yml`: Change 1 needs no new
  step (the capture lives inside the guest's A-private artifact dir), so its bytes stay
  untouched, including environment/secrets/vars/triggers/permissions.
- The frozen OpenCode pin/render tokens and the ten OpenCode-vehicle rows stay F2-excluded.
- The `shared` set's three product-test stops and the stale-pin findings stay in
  `docs/TODO.md`; only the containment/cleanup entry closes if both halves land.

## 8. Addendum (2026-11-24, implement stage runs and the classification)

Runs: `45452357027` (head `05ec6ec8`, narrow four: `codex_containment`, `claude_containment`,
`synthetic_goal_cleanup`, `migration`), `45455325059` (head `305a5d0f`, same four; the first
attempt died in the unrelated `contracts` frontend flake and was rerun in-run), `45458181149`
(head `1ff5ad44`, the first literal all-14 run, job 23m04s), and `45465331434` (head `65bfeaf2`,
the binding literal all-14 run, job 23m23s) after one doc comment was trimmed to the two-line
comment policy — its 14 failure blocks are byte-identical to `45458181149`'s apart from durations.
The scratch branch was deleted at stage close with `git ls-remote` proof. The record's §6 carries
the per-set table.

What the runs changed in this plan:

- The capture (Change 1) named the first post-seed refusal in run 1:
  `fixed-B native HOME is outside the isolated owner witness root`. That is the fixture
  precondition §7.1 of the record fixes in the owned script — `B_HOME` moves under
  `/opt/symmetry-owner-test` with a passwd readback fence; the B-path fences and A-view ACLs were
  already `$B_HOME`-relative, and the one hardcoded exception (`machine_run_as`'s per-account
  `--setenv=HOME=` value) is fixed in round 1 below. Cleared in runs 2-3.
- The next named stop is a product-vs-harness contradiction, not a fixture gap: the witness's
  clone reservation requires an admitted Control Subject
  (`daemon/internal/state/linux_clone_journal.go:208-216,864-878`) while the witness request
  validation refuses a claim that carries an admission
  (`daemon/e2e/production_linux_v2_native_witness_test.go:1565-1567`), and Control's
  `tasks_goal_fields_all_or_none` forbids both at once. No fixture claim satisfies both, so the
  containment rows stop at `Linux clone intent does not match the current admitted Subject` and are
  recorded as `DECISION_NEEDED` per the contract's Change-1 branch 2 (record §7.2) with the
  smallest proposed fix (Goal-backed admitted claims plus the one-block harness relaxation).
  `daemon/internal/**` stays untouched.
- The carry-over P3 fixes landed in the owned script with local probes (record §4); the
  `synthetic_goal_cleanup` row is past its admission refusal and stops at the helper's later named
  `release_outcome_unknown` (record §3), which satisfies the contract's Change-2 clause without
  chasing the new class.
- Budget: 4 of the contract's 5 dispatches used (the fourth re-bound the bytes after the comment
  trim); the Change-1/Change-2 mutant rows cite the before run `45388258526` as the contract allows.

## 9. Fix round 1 (2026-11-24, review findings STD-01–STD-10)

Code: the tail helper now redacts the whole capture before its window and judges the
marker-stripped view (`production_linux_v2_native_witness_test.go:2041-2066`); the report budget
drops to 1 status + 5 tail lines against the printer's last-10 window; `machine_run_as` derives
`HOME` from the guest passwd entry; the exporter's exclusion guard also refuses a computed source
path component. Docs: §1/§2/§4/§5 above, and the record's §13 carries the per-finding deltas,
probes and mutant rows. No dispatch was spent in this round; the hosted re-proof of the changed
bytes stays conductor-owned.
