# rf-acc-native-burndown — evidence record (2026-11-24, implement stage)

Contract: `goal-rf-acc-native-burndown.md` (GDD state root, external to the repository). Diagnosis:
`native-45200662557-diagnosis.md` (GDD findings root; `log:N` references are lines of
`native/run-45200662557.log`). Plan: `docs/plans/2026-11-24-rf-acc-native-burndown-plan.md`.
Base revision: `668e75de6b376eb4205c11256228778003497d0a` (#271). Branch:
`refactor/rf-acc-native-burndown`.

Runner: GitHub-hosted `ubuntu-latest`, `workflow`-dispatched scratch copy of the real job on
`scratch/rf-acc-native-burndown` (`workflow_dispatch`-shaped push trigger; no `environment`,
`secrets.*` or `vars.*`; the contract's dummy credentials; the scratch-only `gh`
credential-ingestion shim that answers `gh auth login` for the dummy token and never reaches the PR
branch). Every scratch run therefore stopped before the first real model turn: all
credential-dependent steps past the recorded stop remain unproven until the owner dispatches the
real workflow.

## 1. Subject bytes (final tree, sha256)

| File | sha256 |
| --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `ab44c6148972c742943f2bc03c51ae0a70d5e71e370f7779e2ca0fbb28264b72` |
| `scripts/ci/linux-v2-native-rootfs-base.sh` | `d4f91792b7954acb9b592b4599790d99cc37dfadc13fc21c921c5f05691b7ccc` |
| `scripts/ci/write-linux-v2-owner-request.py` | `17c36b13e25cf50813d4dd74155a69684a0094dedd9b7ba49cffb67ce1eb7b46` |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `76ad9dfb89d4bc95ad26440f7d59c5c0f36f5d456307cfa8a1d46d3697645fa7` |
| `.github/workflows/linux-native-v2-acceptance.yml` | `c0d18435e86758e00cef0b176248eed696e70ec397c883b9b7b46c17740dc890` |
| `daemon/internal/linuxpreflight/systemd_linux.go` | `37045972b7c6aba631045e5a9a348a9f51a0eae0351eef15b6908bdb4724ea92` |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `465293fa3ac3b0c385fabd38f6b385da986f86a7305d7958e9b335a2c7d9f1fd` |
| this record | no self-referential hash |

Fix-stage refresh (2026-11-24, §10): the acceptance-script row above is the fix-stage bytes; the
final all-14 run `45402706492` bound the pre-fix bytes (`05d81032…`, §3.15/§9), and only
§10.1-§10.3's diagnostic-path regions differ. The preflight row above is the merged head's value
(§9).

The cap-ruling heads progress as `9fadfda2` (round 7: round-6 bytes plus the scoped
`safe.directory` guard, §3.7.6), `26ba0065` (round 8: the ACK-loss fix and the first failure
context, the acceptance script at `fd1dee5d…`), `a51d26cb` (round 9: the native-gate probe, the
guest tool view and the file-level inventory, script at `c4343e0f…`) and `196bfb27` (round 10:
the pi share directory mode, the A-view tool fences and the containment early-phase probe, the
table's hashes), `daeaedda` (round 11: the explicit tool-tree modes A's own fences pin, §3.11),
`99277aed` (round 12: the fixed-B XDG cache and Pi session directory, §3.12.1, run 12's hash),
`d4fdb149` (round 13: the fifth leg's pin re-point, §3.14) and `8339f991` (round 13b: the
parent-death fixture name width, §3.15 — the head of the final all-14 run `45402706492`; §10
replaced this table's script row with the fix-stage bytes).
Verified with `git cat-file blob <head>:<path>` against the worktree at each push;
each commit's tree is the push-time worktree (PR head plus this stage's files) with the two
throwaway files below, deleted with the branch: `.github/workflows/scratch-native-burndown.yml`,
`scripts/ci/scratch-gh-shim`. The earlier rounds' head `8d84ecfc` carried the four `scripts/ci/*` and
workflow files byte-identically and `daemon/**` at base bytes.
`scripts/ci/linux-v2-native-tools.sh`, the kind pin map, the frozen opencode render tokens and
`control/**` are at base bytes.

## 2. The changes

1. Diagnostics (ships to CI): the `daemon_service_start_status != 0` branch of the fixture now
   prints, before `fail`, three bounded blocks to stderr, each `|| true`, guest-side, following the
   in-fixture precedent at the pid-reuse watcher (`--output=cat`, bounded `-n`, narrow `--grep`,
   private paths only):
   - `systemctl show` of `symmetry-daemon.service` with
     `Result,ExecMainStatus,ExecMainCode,ActiveState,SubState,ControlGroup,NRestarts,After,BindsTo,ExecStartPre,ExecStart`;
   - `systemctl show` of `symmetry-owner.service` with
     `Before,After,BindsTo,Requires,Requisite,Wants,PartOf,Conflicts` (the keys the owner policy
     check reads), so the log names the source of a rejected owner/daemon edge;
   - the last 80 `--output=cat` journal lines of the unit, filtered to the daemon's own stderr
     prefixes and systemd's unit-failure lines.
   No environment block, request body or unit environment is printed; the added properties are
   unit-state values and static command lines.
2. Containment smoke config: `daemon-service.json` is derived from the case set's own kind config
   (`codex-containment.json` / `claude-containment.json` / `opencode-health.json`) instead of always
   from `opencode-health.json`; `state_dir`, `machine_name`, `runtime_key`, the `daemon-service`
   workspace root and the runtime name (now from the config's own `harness_kind`) stay rewritten.
   Containment guests still provision exactly their own kind's credential directory; the
   provisioning and absence-assertion blocks are untouched (0 diff lines).
3. Guest build: the mirror rule and the debootstrap move into
   `scripts/ci/linux-v2-native-rootfs-base.sh` (mirror selection byte-identical to the removed
   block, fail-closed; `--include` list byte-identical). One base rootfs per job, built by a new
   workflow step that exports `SYMMETRY_LINUX_V2_NATIVE_ROOTFS_BASE`; each case set clones it with
   `sudo cp -a` from its own fresh `mktemp -d` root. The base build retries only its acquire phase
   (≤5 attempts — F4 raised the cap from 3 — with 30 s / 60 s / 90 s / 120 s backoff, each attempt
   bounded at 600 s with
   `sudo timeout --signal=TERM --kill-after=30s`, `timeout` owning the root `debootstrap`
   directly; an attempt is retried on the acquire-failure patterns or on a 124/137 timeout status,
   otherwise the helper prints the attempt log's last 40 lines and fails); a success is fenced with
   `chroot <base> test -x /usr/bin/readelf`, and the fixture re-runs that fence on every clone. With
   the variable unset the fixture builds its own rootfs through the same helper, so local runs keep
   working. Every per-set provisioning step, fence and teardown proof is unchanged.

Not touched: `scripts/ci/linux-v2-native-tools.sh` (0 diff lines: the OpenCode staging URL, archive
hash, version assertion), the kind pin map's `"opencode": "1.18.32"`, the frozen opencode render
tokens, all of `daemon/**` and `control/**`, and the real workflow's `environment`, `secrets.*`,
`vars.*`, triggers and permissions (the workflow diff adds one step).

## 3. Scratch runs

### 3.1 Round 1 — narrow triad on the PR-head bytes (`45228382743`, head `13217d00`)

Carried over from the previous implement stage; it ended while this run was pending.

One base build (step 37 s, `Build the shared disposable Ubuntu guest rootfs base`) then three
clones of the same base (`Cloning the shared disposable guest rootfs base for this case set` ×3),
case-set step 101 s for all three sets, job 3 m 33 s. Per case set: 51.0 s
(`codex_containment`), 24.5 s (`migration`), 24.4 s (`stale_leaf`), against 6.9–11.1 min per set in
the pre-change baseline `45200662557` (one debootstrap each, ~8.5 min mean).

| Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- |
| `codex_containment` | installed `symmetry-daemon.service` smoke: direct preflight green (`read-only preflight does not launch B, …`), `systemctl start` failed | 1 | `-verify-linux-owner-install: effective symmetry-owner.service unit violates the owner service contract: effective owner unit depends on or orders against symmetry-daemon.service through Before` |
| `migration` | same | 1 | same |
| `stale_leaf` | same | 1 | same |

Class A is gone here: the containment set's direct preflight (`daemon-service.json` derived from
`codex-containment.json`) passes, and the `inspect B credential directory …/.local/share/opencode`
refusal of the baseline no longer appears. Class B is gone: 1 base build + 3 clones, no debootstrap
per case set. Class C is now named from the journal (Change 1) in all three sets, and
`ExecMainStatus=0` with `ExecStartPre … status=1` separates the control process from the daemon's
own exit.

### 3.2 Round 2 — Change 2 mutant (`45302931629`, head `fb578b8f`)

The scratch tree reverted only the smoke-template selection to always `opencode-health.json`
(the pre-fix behaviour) and ran `codex_containment` alone. Base build step 32 s, one clone,
case-set step 48 s, job 2 m 27 s.

| Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- |
| `codex_containment` | installed `symmetry-daemon.service` smoke: direct preflight refused the missing OpenCode credential directory | 1 | `-verify-linux-owner-install: inspect B credential directory /home/symmetry-b/.local/share/opencode: lstat /home/symmetry-b/.local: no such file or directory` |

Byte-identical to the baseline's class A text (`log:3626-3627`, `log:5273-5274`), followed by
`redacted evidence export failed: containment witness is unavailable`. The before/after pair for
Change 2 is therefore `45302931629` (A reappears) → `45228382743` (A gone).

### 3.3 Round 3 — all 14 case sets on the PR-head bytes (`45303576904`, head `d3f60026`)

Job 8 m 15 s; base-build step 25 s; case-set step 391 s for all 14 sets (per set 22.9–50.2 s, the
first set paying the Go build warmup); one base build and 14 clones. Every row's dump separated the
control process from the daemon (`ExecMainStatus=0`, `ExecStartPre … status=1`) and printed the
owner's effective edges; 13 of 14 rows show `Before=… symmetry-daemon.service` in that line.

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | installed daemon smoke: direct preflight green, `systemctl start` failed | 1 | `-verify-linux-owner-install: effective symmetry-owner.service unit violates the owner service contract: … through Before` (class C) |
| 2 | `claude_containment` | same | 1 | same (class C) |
| 3 | `shared` | Control claim provisioning, `schedule stage_replay_unprovisioned_refusal` → `launch_one_shot_work_item!` | 1 | `ERROR 23514 (check_violation) … constraint: tasks_goal_fields_all_or_none` (class D) |
| 4 | `synthetic_goal_cleanup` | installed daemon smoke | 1 | class C line |
| 5 | `stage_replay_create_started` | same | 1 | class C line |
| 6 | `stage_replay_leaf_bound` | same | 1 | class C line |
| 7 | `stage_replay_launcher_bound` | same | 1 | class C line |
| 8 | `ack_loss_launcher_bound` | same | 1 | class C line |
| 9 | `stale_leaf` | same | 1 | class C line |
| 10 | `migration` | same | 1 | class C line |
| 11 | `stale_journal_run_key` | same | 1 | class C line |
| 12 | `stale_journal_run_key_legacy` | same | 1 | class C line |
| 13 | `stale_journal_fence` | same | 1 | class C line |
| 14 | `stale_journal_subject` | same | 1 | class C line |

Class A: gone for both containment kinds — both preflights are green with their own kind config
(`daemon-service.json` derived from `codex-containment.json` / `claude-containment.json`), and no row
carries the baseline's `inspect B credential directory …/.local/share/opencode` refusal. Class B:
gone — no row runs debootstrap; each row clones the one shared base. Class C: named in 13 rows from
the journal, with the source edge visible in the added `systemctl show` line. Class D: one row, and
the fix is in §5.2. No row ends at A, B or a silent daemon start failure. The OpenCode-vehicle rows
never reach the F2 version gate because class C stops them earlier; the pin stays untouched.

### 3.4 Round 4 — the base-build mirror incident, and the class D proof

`45305439399` (head `3d628e73`, class-D fix, sets `codex_containment` + `shared`): the base build
failed before any case set ran. Job 4 m 10 s, base-build step 2 m 53 s (three attempts plus the two
backoffs). Each attempt failed in the acquire phase on a *different* package against the image's own
mirror `http://azure.archive.ubuntu.com/ubuntu` — attempt 1 `binutils`, attempt 2 `libsqlite3-0`,
attempt 3 `tzdata` — and the helper failed closed with
`Linux v2 native rootfs base build failed: debootstrap against http://azure.archive.ubuntu.com/ubuntu failed after 3 attempt(s)`.
This is the retry path observed instead of inferred; the then-three-attempt cap (raised to five by
F4, §6.3) means a bad mirror window still fails the job (`45200662557`'s class B, one exposure
instead of 14).

`45306374149` (head `902053ea`, identical bytes): the same step then made no progress for 16 m 44 s
(healthy base builds: 25–37 s), so the run was cancelled (`conclusion=cancelled`). That stall is the
measured reason for the per-attempt 600 s bound added before round 5; a healthy or merely slow
mirror still fits inside it (16× the healthy time), while a stalled one now costs one attempt, not
the whole job.

`45309039777` (head `0a247993`): the class-D proof and the final all-14 run in one dispatch, on the
final bytes — rows in §3.5.

`45310491984` (head `1f01818f`, the `shared`-only diagnostic run on the same fixture bytes): the
base-build step ran 02:15:28Z → 02:51:20Z (35 m 52 s) and was still running, i.e. past the
3 × 600 s + 90 s the bound allows, so the guard as first written did **not** bound it: `timeout` had `sudo` as
its child and the signalled process group did not include the root `debootstrap`, which kept
writing into the `tee` pipe and held the step open. The run was cancelled
(`conclusion=cancelled`). Fix: `sudo timeout … debootstrap …` so `timeout` runs as root and owns
`debootstrap` directly. This run therefore also fails to discriminate class E's cross-set reading;
that reading stays derived from the code paths plus the logged task profile (§5.3).

### 3.5 All-14 run A (`45309039777`, head `0a247993`) — class E first reached

Job 7 m 04 s; base-build step 30 s (first attempt); case-set step 321 s for all 14 sets (~23 s per
set); one base build and 14 clones; no debootstrap inside any case set.

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | installed daemon smoke: direct preflight green, `systemctl start` failed | 1 | class C line |
| 2 | `claude_containment` | same | 1 | class C line |
| 3 | `shared` | Control claim provisioning, `schedule stage_replay_unprovisioned_refusal`: run assigned, then the fixture's runtime guard | 1 | `** (RuntimeError) generic Control claim runtime did not match the exact non-native Work profile/resource` (`provision-linux-v2-control-claims.exs:205`) — class E, §5.3 |
| 4 | `synthetic_goal_cleanup` | installed daemon smoke | 1 | class C line |
| 5 | `stage_replay_create_started` | same | 1 | class C line |
| 6 | `stage_replay_leaf_bound` | same | 1 | class C line |
| 7 | `stage_replay_launcher_bound` | same | 1 | class C line |
| 8 | `ack_loss_launcher_bound` | same | 1 | class C line |
| 9 | `stale_leaf` | same | 1 | class C line |
| 10 | `migration` | same | 1 | class C line |
| 11 | `stale_journal_run_key` | same | 1 | class C line |
| 12 | `stale_journal_run_key_legacy` | same | 1 | class C line |
| 13 | `stale_journal_fence` | same | 1 | class C line |
| 14 | `stale_journal_subject` | same | 1 | class C line |

Class D is gone: `shared` passed the provisioning that failed in round 3 (its task launched, a Run
was assigned, `max_run_attempts` untouched) and died one step later at the runtime guard. Class A
stays gone for both containment kinds. Class B stays gone for every row. No row ends at A, B or a
silent daemon start failure; rows 1-2 and 4-14 end at the *named* class C check with the owner
`Before=… symmetry-daemon.service` line in the dump; row 3 ends at the named class E guard.

### 3.6 Final all-14 run B (`45315493474`, head `8d84ecfc`, final bytes)

Job 8 m 50 s; base-build step 36 s on the first attempt (the mirror window recovered); case-set step
395 s for all 14 sets (~28 s per set). Log counts: 13 class-C lines, 13 `Before=…
symmetry-daemon.service` owner lines, 1 class-E line, and zero class-A, class-B or class-D lines.

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | installed daemon smoke: direct preflight green, `systemctl start` failed | 1 | class C line |
| 2 | `claude_containment` | same | 1 | class C line |
| 3 | `shared` | Control claim provisioning: run assigned, then the fixture's runtime guard | 1 | `generic Control claim runtime did not match the exact non-native Work profile/resource` (class E) |
| 4 | `synthetic_goal_cleanup` | installed daemon smoke | 1 | class C line |
| 5 | `stage_replay_create_started` | same | 1 | class C line |
| 6 | `stage_replay_leaf_bound` | same | 1 | class C line |
| 7 | `stage_replay_launcher_bound` | same | 1 | class C line |
| 8 | `ack_loss_launcher_bound` | same | 1 | class C line |
| 9 | `stale_leaf` | same | 1 | class C line |
| 10 | `migration` | same | 1 | class C line |
| 11 | `stale_journal_run_key` | same | 1 | class C line |
| 12 | `stale_journal_run_key_legacy` | same | 1 | class C line |
| 13 | `stale_journal_fence` | same | 1 | class C line |
| 14 | `stale_journal_subject` | same | 1 | class C line |

This is the contract's final all-14 run, on the final bytes; it is the last scratch run of the
stage. It reproduces run A's shape exactly, so class E is deterministic in the all-14 shape, not a
one-off.

### 3.7 F-ruling round 1 — all 14 sets past classes C and E (`45318768814`, head `c7053e2a`)

Job 10 m 39 s (`03:21:21Z`–`03:32:00Z`); base-build step 38 s on the first attempt
(`03:22:41`–`03:23:19`), so F4's raised cap was not needed; case-set step 8 m 37 s for all 14 sets
(~37 s per set); 14 `Cloning the shared disposable guest rootfs base` lines.

Proof of the F-ruling changes in this run: **zero** `…through Before`/`effective owner unit depends`
lines (class C gone; F1's live proof) and 14 `retired N runtime(s) left online by earlier case sets`
lines — `0`, then `4`, then `5` twelve times (F2's live proof: set 1 finds no earlier runtime, set 2
retires set 1's four, and every later set retires five). 13 sets print
`read-only preflight does not launch B, …` and then the smoke stop line
`Stopping installed daemon.service before direct witnesses to release its exclusive StateDir Store
lock` — i.e. `ExecStartPre`, `systemctl start`, `is-active`/`MainPID`/`NRestarts` and the drain
assertions all passed inside the unit (the class-C gate is cleared from the journal side too, with no
failure dump printed).

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | installed `symmetry-daemon.service` smoke green (preflight, start, stop), then the first witness invocation | 1 | `each exact Go witness invocation requires named test events` — §3.7.1 |
| 2 | `claude_containment` | same | 1 | same |
| 3 | `shared` | Control claim provisioning (`retired 4 runtime(s)`) → the unprovisioned witness's Go invocation | 1 | `Go native acceptance event gate failed: bad=TestProductionLinuxV2UnprovisionedHostRefusal=run:0,pass:0,skip:0, skipped=[], fail_events=0, invalid_json_lines=1` with `go test exit=1` — §3.7.2 |
| 4 | `synthetic_goal_cleanup` | same as row 1 | 1 | same as row 1 |
| 5 | `stage_replay_create_started` | same as row 1 | 1 | same |
| 6 | `stage_replay_leaf_bound` | same as row 1 | 1 | same |
| 7 | `stage_replay_launcher_bound` | same as row 1 | 1 | same |
| 8 | `ack_loss_launcher_bound` | same as row 1 | 1 | same |
| 9 | `stale_leaf` | same as row 1 | 1 | same |
| 10 | `migration` | same as row 1 | 1 | same |
| 11 | `stale_journal_run_key` | same as row 1 | 1 | same |
| 12 | `stale_journal_run_key_legacy` | same as row 1 | 1 | same |
| 13 | `stale_journal_fence` | same as row 1 | 1 | same |
| 14 | `stale_journal_subject` | same as row 1 | 1 | same |

No row ends at class A, B, C or E. This run is not the contract's final all-14 run: it names two
further stops, and §3.7.1's fix plus §3.7.2's diagnostics ship in the next round (run 2,
`45320631643`, head `926e26e7`).

#### 3.7.1 The witness-invocation parse defect (13 rows; fixed in scope)

`run_go_test` read its expected Go test-event names from the arguments **before** a literal `--`
separator, while all 26 call sites pass them **after** it, so the collected list was always empty
and the guard at `linux-v2-native-acceptance.sh:1563` (the pre-fix line; the assertion now sits at
`:1625`) fired before any Go test ran. The defect is
pre-existing (identical loop since the function's introduction in `41e64851`; unmasked until this
round because every earlier run died at classes A/B/C before a witness invocation) and mechanical:

| Probe (extracted `run_go_test` head, `-- Name1 Name2/sub`) | Pre-fix | Post-fix |
| --- | --- | --- |
| collected names | `0`, first remaining argument `Name1` | `Name1 Name2/sub` |
| call without `--` | `0` (same empty-list failure) | refused by the delimiter guard |
| call with `--` and no names | refused | refused |

Fix in the owned script: `[[ "${1:-}" == -- ]] || fail '<delimiter message>'`, then
`local -a expected_tests=("$@")`; the original non-empty assertion stays and now guards the callers'
actual convention. No call site changes; no assertion is weakened.

#### 3.7.2 The `shared` unprovisioned event gate (1 row; diagnostics added)

`shared` runs its unprovisioned-refusal witness before the smoke (its own `systemd-run … go test
-json` block, `linux-v2-native-acceptance.sh:1444-1478`). This round it produced no JSON events at
all: `invalid_json_lines=1`, `fail_events=0`, `run/pass/skip = 0` for the named test, `go test
exit=1`. A Go build failure emits JSON (`build-output`/`build-fail`, verified locally with `go test
-json` on a broken package) and would count a `fail` event, so the single non-JSON line came from
before/around the Go invocation; the run kept no copy of it. The failure branch now prints the
event file's first ≤5 non-JSON lines (same bounded failure-branch pattern as Change 1), and the
next run names the root.

### 3.7.3 F-ruling round 2 — every set reaches its named witness (`45320631643`, head `926e26e7`)

Job: base-build step 45 s (`03:38:39`–`03:39:24`, first attempt); case-set step 8 m 27 s
(`03:39:24`–`03:47:51`). The parse fix works: **zero** `requires named test events` lines, and the
case-set step is the same length as round 1 even though every set now executes a Go invocation. The
new diagnostics printed the same line in all 14 sets:

```
failed to initialize build cache at /home/symmetry-a/.cache/go-build: mkdir /home/symmetry-a/.cache/go-build: permission denied
```

Root: the A user's fixed Go build cache sits under `/home/symmetry-a/.cache`, and the staging lists
`.cache/go-build` and `.cache/tmp` as `install -d` arguments without naming `.cache`. `install -d`
applies `-o/-g/-m` to each *named* directory only, so the implicit `.cache` parent is left with that
coreutils' default attributes; on this runner's `install` it is not usable by A (the guest's `go`
cannot create or reach its cache inside it — the observed `mkdir … permission denied`). Locally the
GNU coreutils 9.4 behaviour is root:root 0755 (traversable), which the new fence still reddens.
The fixture stages the *root* user's cache correctly (its `.cache` is named); the A copy omitted it.
Fixed in the owned script: `.cache` is named, and a fence now fails closed
unless `.cache`, `.cache/go-build` and `.cache/tmp` are all A-owned mode 0700
(`linux-v2-native-acceptance.sh:508-514,535-538`). Local mutant probe (GNU install semantics): the
pre-fix staging leaves `.cache` root-owned (fence reddens), the post-fix staging leaves all three
A-owned 0700 (fence passes).

The same scan over every `install -d` path in the script found one more implicit parent on an
A-owned barrier root, `$OWNER_BARRIER_ROOT/unprovisioned` (named now, `:698-699`), and one flag that
is already covered: `negative-root/stale_journal_run_key` is re-owned by the per-case loop at
`:730` before the guest boots (`stale_journal_run_key` is in `OWNER_NEGATIVE_CASES`), and
`install -d` re-applies attributes to an existing named directory (verified locally).

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | named witness `TestProductionLinuxV2CodexNativeContainment` invoked; guest build cache refused | 1 | gate `bad=…=run:0,pass:0,skip:0, skipped=[], fail_events=0, invalid_json_lines=1` + the cache line above |
| 2 | `claude_containment` | same, `TestProductionLinuxV2ClaudeNativeContainment` | 1 | same |
| 3 | `shared` | same, `TestProductionLinuxV2UnprovisionedHostRefusal` (pre-smoke leg) | 1 | same |
| 4 | `synthetic_goal_cleanup` | same, `TestProductionLinuxV2CloneCleanupAuthenticatedScratch` | 1 | same |
| 5 | `stage_replay_create_started` | same, `TestProductionLinuxV2StageCrashReplayMatrix` | 1 | same |
| 6 | `stage_replay_leaf_bound` | same, `TestProductionLinuxV2StageCrashReplayMatrix` | 1 | same |
| 7 | `stage_replay_launcher_bound` | same, `TestProductionLinuxV2StageCrashReplayMatrix` | 1 | same |
| 8 | `ack_loss_launcher_bound` | same, `TestProductionLinuxV2AckLossReplayMatrix` | 1 | same |
| 9 | `stale_leaf` | same, `TestProductionLinuxV2StaleLeafIdentityRetainsJournal` | 1 | same |
| 10 | `migration` | same, `TestProductionLinuxV2MigrationDenialRetainsJournal` | 1 | same |
| 11 | `stale_journal_run_key` | same, `TestProductionLinuxV2UnresolvedJournalBlocksFreshRun` | 1 | same |
| 12 | `stale_journal_run_key_legacy` | same, `TestProductionLinuxV2StaleOwnerRequestDenials` | 1 | same |
| 13 | `stale_journal_fence` | same, `TestProductionLinuxV2StaleOwnerRequestDenials` | 1 | same |
| 14 | `stale_journal_subject` | same, `TestProductionLinuxV2StaleOwnerRequestDenials` | 1 | same |

No row fails a fixture assertion other than the JSON gate; every row's stop is the guest-side Go
invocation. Rounds 1-2 used 2 of the ≤6 further scratch runs; round 3 (`d2375e8f`) carries the
staging fix.

### 3.7.4 F-ruling round 3 — the guest Go invocation runs; every witness fails inside it

Round 3 (`45322501907`, head `d2375e8f`) died in the base build: attempts 1-4 hit retryable acquire
failures, attempt 5 hit the 600 s per-attempt bound (`debootstrap exited 124`), and the helper
failed closed with
`Linux v2 native rootfs base build failed: debootstrap against http://us-east-2.ec2.archive.ubuntu.com/ubuntu failed after 5 attempt(s)`.
The retry, the backoff, the per-attempt bound and the image-apt mirror rule all behaved as designed;
the mirror window was bad. Per the ruling this is infrastructure, not a defect, and the case-set
evidence comes from round 3b (`45327162434`, head `dc805c89`), whose tree is byte-identical to
`d2375e8f` (verified with `git rev-parse d2375e8f^{tree}`).

Round 3b: base build 36 s; case-set step 21 m 54 s (`04:36:37`–`04:58:31`), much longer than rounds
1-2 because the witnesses now execute. The staging fix worked — every row has `invalid_json_lines=0`
and `run:1` (the guest `go test` starts the named test) — and every row now fails *inside* the named
witness (`pass:0`, `fail_events=2`, or `3` where a subtest also fails). The containment rows
additionally show the exporter refusing with
`redacted evidence export failed: containment witness native_binary does not match --native-binary`.

| # | Case set | Named witness (gate summary) |
| --- | --- | --- |
| 1 | `codex_containment` | `TestProductionLinuxV2CodexNativeContainment` `run:1,pass:0,skip:0, fail_events=2, invalid_json_lines=0` |
| 2 | `claude_containment` | `TestProductionLinuxV2ClaudeNativeContainment` same |
| 3 | `shared` | `TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction` same |
| 4 | `synthetic_goal_cleanup` | `TestProductionLinuxV2CloneCleanupAuthenticatedScratch` same |
| 5 | `stage_replay_create_started` | `TestProductionLinuxV2StageCrashReplayMatrix/create_started` `fail_events=3` |
| 6 | `stage_replay_leaf_bound` | `…StageCrashReplayMatrix/leaf_bound` `fail_events=3` |
| 7 | `stage_replay_launcher_bound` | `…StageCrashReplayMatrix/launcher_bound` `fail_events=3` |
| 8 | `ack_loss_launcher_bound` | `TestProductionLinuxV2AckLossReplayMatrix/launcher_bound` `fail_events=3` |
| 9 | `stale_leaf` | `TestProductionLinuxV2StaleLeafIdentityRetainsJournal` `fail_events=2` |
| 10 | `migration` | `TestProductionLinuxV2MigrationDenialRetainsJournal` `fail_events=2` |
| 11 | `stale_journal_run_key` | `TestProductionLinuxV2UnresolvedJournalBlocksFreshRun` `fail_events=2` |
| 12 | `stale_journal_run_key_legacy` | `TestProductionLinuxV2StaleOwnerRequestDenials/stale_journal_run_key` `fail_events=3` |
| 13 | `stale_journal_fence` | `…StaleOwnerRequestDenials/stale_journal_fence` `fail_events=3` |
| 14 | `stale_journal_subject` | `…StaleOwnerRequestDenials/stale_journal_subject` `fail_events=3` |

The witness document's `native_binary` is written from the staged profile command
(`daemon/internal/app/linux_owner_witness_linux.go:2969`, `witness.summary.NativeBinary =
profile.Command`), and the staged codex/claude configs set exactly the exporter's pins
(`/usr/local/bin/codex`, `/usr/local/bin/claude`, `linux-v2-native-acceptance.sh:953-970`). The
exporter's refusal therefore means the field is absent — the witness aborted before
`recordNativeExecutableDigest` (`:2909`) — or the document is a failure form. The fixture printed no
test output, so round 5 (`45330552341`, head `f5bca7ce`) adds two bounded diagnostics instead of a
guess: the failing test's last ≤10 output lines, filtered against the fixture's secret-values file,
and the witness document's `{case, harness, outcome, failure_category, installed_owner_verified,
native_binary, native_open_reached}`.

### 3.7.5 F-ruling round 5 — every witness start is now named (`45330552341`, head `f5bca7ce`)

The diagnostics worked: every row now carries its witness's own first failing line (plus, for the
containment rows, nothing — those sets' documents are covered by §3.7.4's exporter message).

| # | Case set | First failing line / check |
| --- | --- | --- |
| 1 | `codex_containment` | `production_linux_v2_native_witness_test.go:101: wait for requested production owner-witness barrier: owner-witness command did not reach its requested durable stage` (30.8 s) |
| 2 | `claude_containment` | same, `:109` (30.8 s) |
| 3 | `shared` | `provider_extension_native_linux_test.go:53: native Pi version did not equal the tested version 1.5.2` (0.4 s) |
| 4 | `synthetic_goal_cleanup` | `production_linux_v2_cleanup_witness_test.go:385: prepare exact A-owned immutable seed: validate A-owned Git source: A-owned Git source must contain complete history: unsupported B Git metadata entry "shallow" is present` (1.5 s) |
| 5 | `stage_replay_create_started` | `:178: wait for exact durable stage create_started: …` (31.4 s) |
| 6 | `stage_replay_leaf_bound` | `:178: … leaf_bound …` |
| 7 | `stage_replay_launcher_bound` | `:178: … launcher_bound …` |
| 8 | `ack_loss_launcher_bound` | `:217: load isolated production owner witness request: owner-witness barrier directory or timeout is invalid` (0.6 s) |
| 9 | `stale_leaf` | `:630: wait for exact durable stop receipt: …` (31.4 s) |
| 10 | `migration` | `:616: wait for exact OpenCode native_release boundary: …` (31.4 s) |
| 11 | `stale_journal_run_key` | `:534: wait for exact durable stage handoff_committed: …` (31.4 s) |
| 12 | `stale_journal_run_key_legacy` | `:269: wait for exact durable stage handoff_committed: …` (31.4 s) |
| 13 | `stale_journal_fence` | same as row 12 |
| 14 | `stale_journal_subject` | same as row 12 |

Three named causes, all fixture-rooted:

1. **Shallow CI checkout (11 rows + row 4).** Row 4 names the validator
   (`daemon/internal/workspace/linux_clone_linux.go:1347`, reached from
   `linuxpreflight/git_ownership_linux.go:19` and the seed path) that refuses a `shallow` entry in
   the A-owned Git source. The source is `$A_HOME/ci-source`, a read-only bind mount of the
   workflow's own checkout, and `.github/workflows/linux-native-v2-acceptance.yml`'s
   `actions/checkout@v7` has no `fetch-depth`, so the checkout (and every guest clone of it) is
   shallow. The barrier witnesses abort in preparation, before their first stage, which is why rows
   1-2 and 5-7, 9-14 all stop at their stage wait after ~31 s. Fix: `fetch-depth: 0` on that step
   (a step-level prerequisite, the only workflow change) — mirrored in the throwaway workflow.
2. **A lone pi binary reports `0.0.0` (row 3).** The fixture staged only `$pi_unpack/pi/pi` to
   `/usr/local/bin/pi`, but pi reads its version from the `package.json` in its release tree: the
   pinned 1.5.2 binary prints `1.5.2` inside its archive directory and `0.0.0` when copied alone
   (reproduced locally with the pinned archive's sha256 on both paths, plus the minimal set:
   `package.json` alone restores `1.5.2`). The sibling fixture
   `scripts/ci/rootless-compose-native-acceptance.sh:227,353` stages the whole `pi/` directory —
   this one copied the binary only. Fix: stage the release tree to `/usr/local/lib/symmetry-pi`
   (root-owned, non-writable, digest-fenced against the verified archive) and point the pi profile
   command and the baseline smoke at `/usr/local/lib/symmetry-pi/pi`.
3. **`ack_loss_launcher_bound`'s request (row 8).** The daemon-side loader
   (`daemon/e2e/production_linux_v2_native_witness_test.go:1587-1591`) rejected the staged
   request's barrier on one of its four conditions (absolute+canonical directory, path within
   `/opt/symmetry-owner-test`, `timeout_seconds` in 1..30, `hold_after == the test case's stage`).
   The fixture's request for this leg is generated by the same writer as the other stage-replay
   legs (which load fine), with `drop_ack_after=launcher_bound`, so the difference is ack-loss
   specific and not yet isolated; the message itself is the named reason for this row.

Run 6 (`45334300138`, head `d22a6b31`) carries fixes 1 and 2 on the same tree as this record's
script bytes; its rows go in §3.7.6.

### 3.7.6 F-ruling round 6 — the checkout change broke guest setup (run cap reached)

Run 6 (`45334300138`, head `d22a6b31`) shipped fixes 1 and 2 above and failed in the fixture's
*setup*: every case set exited 128 within ~30 s, each with

```
fatal: detected dubious ownership in repository at '/home/symmetry-a/ci-source/.git'
```

from the guest's A-owned clone of the read-only runner-owned source mount
(`linux-v2-native-acceptance.sh:909`). The `fetch-depth: 0` checkout leaves that mount's `.git`
foreign-owned for A, and the fixture's single `-c safe.directory=$A_HOME/ci-source` no longer
covered the guard this git build applies to the gitdir — the same command cloned the shallow mount
fine in rounds 1-5. The mount is read-only and cannot be chowned, so the shipping tree carries the
minimal relaxation for that one command: `-c 'safe.directory=*'` with a two-line comment naming the
mount.

This was the sixth and last dispatch under the ruling's "at most 6 further scratch runs"; no run
executes the current bytes (`5e189d61`/`c0d18435`), so fixes 1-2 and this guard are reasoned and
locally probed but **unverified in a hosted run**. The stage's final all-14 evidence table is
therefore round 5's (§3.7.5), which satisfies the contract's row requirements: every row ends at a
named check with its reason, none at class A, B or a silent daemon start.

### 3.8 Round 7 — the round-6 fixes proven; every row reaches its own witness (`45369766498`, head `9fadfda2`)

The conductor's cap ruling reopened the scratch loop with six further dispatches; run 7 is the
ordered first dispatch: the checked-in tree (round 6 plus the scoped `safe.directory=*` guard,
§3.7.6) with no further change. Job 20m52s; the base-build step succeeded on its first attempt;
the case-set step ran `09:58:28Z`–`10:17:27Z` (18m59s) for all 14 sets (0.9–1.8 min per set); 14
`Cloning the shared disposable guest rootfs base` and 14 base-helper lines.

Log counts over the whole failed step: `effective symmetry-owner.service` 0, `inspect B credential
directory` 0, `debootstrap` 0, `generic Control claim runtime did not match` 0, `dubious
ownership` 0, `shallow` 0, `build cache` 0, `native Pi version did not equal` 0. Classes A–E stay
gone, and all three round-6 changes are proven in a hosted run: the guest's A-owned clone of the
read-only runner-owned source mount succeeds (`safe.directory` guard), the A-owned Git source
carries complete history (`fetch-depth: 0`; no `shallow` refusal from the installed owner's
validator), and the pi release tree is staged (§3.7.5 fix 2) — the pi leg now stops one check
later, §3.8.1.

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | named witness `TestProductionLinuxV2CodexNativeContainment` invoked; the witness command does not reach its requested `native_release` stage | 1 | `production_linux_v2_native_witness_test.go:101: wait for requested production owner-witness barrier: owner-witness command did not reach its requested durable stage` |
| 2 | `claude_containment` | same, `TestProductionLinuxV2ClaudeNativeContainment` | 1 | same at `:109` |
| 3 | `shared` | witness `TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction` invoked (`pi-baseline`) | 1 | `provider_extension_native_linux_test.go:35: native Pi executable must be an existing regular file` |
| 4 | `synthetic_goal_cleanup` | witness `TestProductionLinuxV2CloneCleanupAuthenticatedScratch` invoked; the test's own real-launcher path starts `/bin/sh` | 1 | `production_linux_v2_cleanup_witness_test.go:499: start controlled B Git worker through the real Linux v2 launcher/anchor/helper: start "/bin/sh": Linux v2 native release requires an App admission check` |
| 5 | `stage_replay_create_started` | named witness invoked; no durable stage | 1 | `production_linux_v2_native_witness_test.go:178: wait for exact durable stage create_started: owner-witness command did not reach its requested durable stage` |
| 6 | `stage_replay_leaf_bound` | same | 1 | same at `:178`, `leaf_bound` |
| 7 | `stage_replay_launcher_bound` | same | 1 | same at `:178`, `launcher_bound` |
| 8 | `ack_loss_launcher_bound` | fixture request load, before the App starts | 1 | `production_linux_v2_native_witness_test.go:217: load isolated production owner witness request: owner-witness barrier directory or timeout is invalid` (0.6 s) — the persistence of §6.4's pre-fix defect |
| 9 | `stale_leaf` | named witness invoked; no durable stage | 1 | `production_linux_v2_native_witness_test.go:630: wait for exact durable stop receipt: owner-witness command did not reach its requested durable stage` |
| 10 | `migration` | same | 1 | `production_linux_v2_native_witness_test.go:616: wait for exact OpenCode native_release boundary: …` |
| 11 | `stale_journal_run_key` | same | 1 | `production_linux_v2_native_witness_test.go:534: wait for exact durable stage handoff_committed: …` |
| 12 | `stale_journal_run_key_legacy` | same | 1 | `production_linux_v2_native_witness_test.go:269: wait for exact durable stage handoff_committed: …` |
| 13 | `stale_journal_fence` | same | 1 | same as row 12 |
| 14 | `stale_journal_subject` | same | 1 | same as row 12 |

No row ends at class A, B, C or E, and no row ends at a silent daemon start failure: every row
ends inside its own named production witness with the test's own failing line. The gate summary
lines confirm the invocation shape (`run:1, pass:0`, `invalid_json_lines=0` in all 14 sets).

#### 3.8.1 The two witness-internal stops this round names

1. **Rows 1-2, 5-7, 9-14 — the witness command never reaches its requested durable stage.** The
   stage-wait message is the test's own; the App's refusal behind it is invisible, because the
   witness harness discards the App's stderr by construction
   (`daemon/e2e/production_linux_v2_owner_cli_test.go:56-66` opens `/dev/null` as the child's
   stderr) and the App prints exactly one diagnostic there
   (`daemon/cmd/symmetry-daemon/main.go:34-36`), while the response file on the App's stdout is
   written only on structured outcomes (`daemon/internal/app/linux_owner_witness_linux.go:613-630`
   returns early without a summary). Round 8 therefore names the stop from fixture state:
   `print_witness_failure_context` prints the witness document projection for the failed test
   names, a bounded name-and-size inventory of the daemon StateDir and of the witness fixture
   root, and re-runs the App's own owner-install preflight as A for every staged witness config
   (root:A mode 0640) — `linuxpreflight.VerifyLinuxOwnerInstallation(value)`, the first
   substantive step of the App's `Preflight` (`linux_owner_witness_linux.go:2887` →
   `native_run.go:159` → `linux_owner_v2_linux.go:80`), reachable verbatim through
   `symmetry-daemon -verify-linux-owner-install -config <path>` (`main.go:261-266`).
2. **Row 3 (`shared`) — the pi leg's staged executable is not visible to A.** The first `shared`
   witness is `pi-baseline`, which reads `SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE`
   (`/usr/local/lib/symmetry-pi/pi`, round 6) and refuses when `os.Stat` does not see a regular
   file (`daemon/internal/harness/pi/provider_extension_native_linux_test.go:29-36`). The fixture's
   host-side fences verified that exact path before the guest booted (`0:0:755:1` and the verified
   archive digest, §3.7.5 fix 2), so the divergence is between the host rootfs and the guest's A
   view of `/usr/local`; round 8 prints that guest view (`ls -ld` + `stat` of the staged tools as
   A) from the failure context. If it shows a non-A-traversable parent, the fix is the round-3
   lesson applied to the native tool tree: name every parent in the fixture's `install -d` list
   and fence traversability. Round 5's `shared` row executed the then-current
   `/usr/local/bin/pi` as A (`:53: native Pi version did not equal the tested version 1.5.2`), so
   the guest's `/usr/local` traversal worked on those bytes; the change of path and staging
   method is the difference to explain.
3. **Row 4 (`synthetic_goal_cleanup`) — the test's own launcher invocation.** The shallow refusal
   is gone; the test now builds a real `LinuxV2Invocation` and the App refuses it at
   `daemon/internal/execution/linux_owner_v2_linux.go:339` because the test's callback set has no
   `CheckNativeAdmission`. That construction lives in `daemon/e2e/**` (test-side) and is therefore
   raised, not fixed: §8.

#### 3.8.2 The ack-loss persistence and the round-8 fix

Row 8 reproduces round 5's stop byte-for-byte at `0.6 s`, so the defect is deterministic and
independent of the round-6 changes. The root is in the fixture's request writer and is fixed in
round 8: §6.4.

### 3.9 Round 8 — the ACK-loss fix lands; the context names the fixture state (`45374416995`, head `26ba0065`)

Job 22m18s; the base build succeeded on the first attempt; all 14 sets exited 1 again. Rows 1-7 and
9-14 are byte-identical to §3.8; row 8 moved past the fixture request load and joins the other
OpenCode-vehicle rows:

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | named witness invoked; no requested `native_release` stage | 1 | `production_linux_v2_native_witness_test.go:101: wait for requested production owner-witness barrier: owner-witness command did not reach its requested durable stage` |
| 2 | `claude_containment` | same | 1 | same at `:109` |
| 3 | `shared` | `pi-baseline` witness invoked | 1 | `provider_extension_native_linux_test.go:35: native Pi executable must be an existing regular file` |
| 4 | `synthetic_goal_cleanup` | test's real-launcher invocation | 1 | `production_linux_v2_cleanup_witness_test.go:499: … start "/bin/sh": Linux v2 native release requires an App admission check` |
| 5 | `stage_replay_create_started` | named witness invoked; no durable stage | 1 | `production_linux_v2_native_witness_test.go:178: wait for exact durable stage create_started: …` |
| 6 | `stage_replay_leaf_bound` | same | 1 | same at `:178`, `leaf_bound` |
| 7 | `stage_replay_launcher_bound` | same | 1 | same at `:178`, `launcher_bound` |
| 8 | `ack_loss_launcher_bound` | **fixture request accepted**; the ACK-loss witness invokes the App and no `launcher_bound` stage appears | 1 | `production_linux_v2_native_witness_test.go:217: wait for exact launcher_bound ACK boundary: owner-witness command did not reach its requested durable stage` |
| 9 | `stale_leaf` | named witness invoked; no durable stage | 1 | `production_linux_v2_native_witness_test.go:630: wait for exact durable stop receipt: …` |
| 10 | `migration` | same | 1 | `production_linux_v2_native_witness_test.go:616: wait for exact OpenCode native_release boundary: …` |
| 11 | `stale_journal_run_key` | same | 1 | `production_linux_v2_native_witness_test.go:534: wait for exact durable stage handoff_committed: …` |
| 12-14 | `stale_journal_run_key_legacy`, `stale_journal_fence`, `stale_journal_subject` | same | 1 | `production_linux_v2_native_witness_test.go:269: wait for exact durable stage handoff_committed: …` |

#### 3.9.1 What the failure context names

- `load isolated production owner witness request` and `barrier directory or timeout is invalid`
  drop from 1 each (runs `45330552341`, `45369766498`) to 0: §6.4's mutant pair is
  `45330552341`/`45369766498` (refused at the load, 0.6 s) → `45374416995` (accepted, the witness
  invokes the App).
- The witness documents carry `owner_witness_outcome: null` in every failing row (no App response),
  and the daemon StateDir inventory shows exactly one journal — the installed-daemon smoke's
  (`runs/journal-3705ef67….json`, 1268 bytes) — and none for any witness RunKey: the App stops
  before `seedAuthenticClaim` writes the claim intent
  (`daemon/internal/app/linux_owner_witness_linux.go:3857-3870`).
- The per-config `-verify-linux-owner-install` probe ran 156 times: 112 configs print the success
  boundary, 20 fail on the containment guests' deliberately absent foreign credential directory
  (`inspect B credential directory /home/symmetry-b/.local/share/opencode: lstat
  /home/symmetry-b/.local: no such file or directory`), 12 on `.codex` and 12 on `.claude`. Only
  configs whose kind the guest does not provision are refused — class A's reviewed scope, §2.2 — so
  every witness config is clean at the owner-install layer.
- The App's witness-specific config gate
  (`daemon/internal/app/linux_owner_witness_linux.go:2256-2265`) compares the config's
  `harness_version` against the pinned tested release. The fixture's table
  (`scripts/ci/linux-v2-native-acceptance.sh:1002-1007`) carries `opencode: "1.18.32"` against
  `opencode.TestedVersion = "2.3.0"` (`daemon/internal/harness/opencode/adapter.go:28`), while
  `pi: "1.5.2" == pi.TestedVersion`, `codex: "0.168.0" == codex.TestedVersion`, and
  `claude_code: "2.1.323" == claude.TestedVersion`
  (`daemon/internal/harness/claude/probe.go:17`). This is the pinned OpenCode staleness the
  conductor's rulings call `F2-excluded`, and it is consistent with exactly the ten OpenCode-vehicle
  rows stopping where they do; round 9 prints it from the App itself (the native-gate probe in
  §3.8.1 item 1).

### 3.10 Round 9 — the native gate names the OpenCode stop; the pi share's directory mode is the pi stop (`45378500332`, head `a51d26cb`)

Job 20m3s; all 14 sets exited 1; rows 1-14 keep §3.9's shapes and stops. The round-9 probes
resolve the two unnamed causes:

1. **The App's own witness config gate refuses the OpenCode-family configs — observed, not
   inferred.** The native-gate probe (the App's `-linux-v2-native-owner-witness` run with a missing
   request path, so it stops at its config gate before any mutation) ran 280 times, once per staged
   witness config per case set: 210 print `-linux-v2-native-owner-witness: owner witness config
   native harness version differs from the tested release`
   (`daemon/internal/app/linux_owner_witness_linux.go:2260-2265`) and 70 print
   `read root-controlled owner witness request: open root-controlled fixture file`, i.e. they pass
   the gate and reach the request read. The 210 are the OpenCode-family configs (every
   `ack-loss/*`, `stage-replay/*`, `negative-root/*` and `config/opencode-*.json` config, whose
   `harness_version` is the fixture's `1.18.32` against `opencode.TestedVersion = "2.3.0"`); the 70
   are the `pi`/`codex`/`claude_code` configs whose pinned versions match. This is the OpenCode
   staleness the conductor's rulings call `F2-excluded`: the ten OpenCode-vehicle rows stop at the
   witness's own version gate, before any stage.
2. **The pi row's stop is the staged pi share's directory mode.** The guest view the round-9
   context prints for A is unambiguous:

   ```
   drwxr-xr-x 11 root root 4096 Nov 24 10:53 /usr/local
   drwxr-xr-x  2 root root 4096 Nov 24 10:53 /usr/local/bin
   drwxr-xr-x  4 root root 4096 Nov 24 10:53 /usr/local/lib
   drwx------  8 root root 4096 Nov 10 12:02 /usr/local/lib/symmetry-pi
   stat: cannot stat '/usr/local/lib/symmetry-pi/pi': Permission denied (os error 13)
   regular file root:root 755 1 /usr/local/bin/opencode
   ```

   `/usr/local/lib/symmetry-pi` is mode 0700, so A cannot traverse it and
   `provider_extension_native_linux_test.go:35` refuses — while every *host-side* fence in the
   fixture passes, because it runs as root. The directory is created by the fixture's
   `install -d -o root -g root -m 0755` and then passed through `chmod -R go-w` on the image's
   coreutils build, the same implicit-attribute behaviour round 3 found for the A XDG cache
   (§3.7.3). Round 10 sets the three directories explicitly to 0755 after the recursive pass,
   fences the share directory's mode, and adds A-view fences (`test -x`/`test -r` as A) for
   `/usr/local/lib/symmetry-pi/pi`, its `package.json` and `/usr/local/bin/opencode`, so the
   traversability of the staged native tools is proven in the guest before any witness runs.
3. **The two containment rows stay unnamed** by the round-9 probes: their configs pass the native
   gate (they are among the 70), the daemon StateDir carries no journal for the witness RunKey
   (§3.9.1), and the round-9 file-level inventory shows every witness barrier directory empty — no
   stage marker exists — so the App stops between its config gate and `seedAuthenticClaim`. Round 10
   adds the bounded early-phase probe: the App is run once, as A, with the exact staged containment
   config and request and its own two environment variables, under `timeout 90`; a refusal before
   any mutation prints verbatim, and a run that gets past the refusal stops at the barrier's own
   30-second release timeout.

### 3.11 Round 10 — the A-view fences catch the staged tree's modes before any witness (`45382412572`, head `196bfb27`)

Job 10m8s: every case set exited 1 immediately after `Preparing the A-owned source repository and
B-only native credentials.`, i.e. at the new A-view fences
(`scripts/ci/linux-v2-native-acceptance.sh:910-916`), so no witness ran and the round-10
containment probe never reached its call site. The fences are fail-closed (each carries a `|| fail`
message) and the fixture's `set -e` stops the set, which is the intended shape; the run also
surfaced one ordering defect in the new diagnostics: the EXIT trap's exporter calls
`print_witness_document`, which was defined later in the file, so an early fixture death printed
`scripts/ci/linux-v2-native-acceptance.sh: line 207: print_witness_document: command not found`
twice instead of the containment document projection.

Together with the round-9 guest view (the share directory at 0700, `stat` refusing on A) this shows
the staged tree arrives with modes A cannot use: `chmod -R go-w` on the image's coreutils rewrites
it the way the round-3 A-XDG-cache finding described — 0755 directories → 0700, 0644 files → 0600 —
while the *root-owned* host-side fences keep passing. Round 11 replaces the recursive symbolic pass
with explicit normalization and keeps the A-view fences positive:

- `find … -type d -exec chmod 0755 {} +`, `… -type f ! -perm -u+x -exec chmod 0644 {} +`,
  `… -type f -perm -u+x -exec chmod 0755 {} +`, plus `chmod 0755` on the three parent directories
  and on `pi`;
- `print_witness_document` moves above `export_native_evidence`, so an early fixture death still
  exports the allowlisted evidence and prints the document projection.

The normalization was probed locally before the dispatch: a mock tree mangled to 0700 directories
and 0600 files, passed through the three `find` passes and the explicit `chmod`, then verified from
an unprivileged account — directories 0755, plain files 0644, executables 0755, and
`test -x`/`test -r` succeed.

### 3.12 Round 11 — the pi fix moves `shared` four witnesses deep; the containment stop is a missing fixed-B directory (`45384436642`, head `daeaedda`)

Job 19m42s; all 14 sets exited 1; the A-view fences pass in every set (0 fence failures), so the
round-10/11 tree-mode fix holds, and the `shared` set advances past `pi-baseline` (its `:35`
refusal is gone from every row).

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | witness invoked; the App seeds its claim and stops before any stage | 1 | `production_linux_v2_native_witness_test.go:101: wait for requested production owner-witness barrier: …`; cause named by the early-phase probe, §3.12.1 |
| 2 | `claude_containment` | same | 1 | same at `:109` |
| 3 | `shared` | legs `pi-baseline`, `opencode-baseline`, `cleanup-root`, `cleanup-mounts-root` pass; the fifth leg's pin matches no Go test | 1 | gate `bad=TestInstalledLinuxOwnerRejectsUnexpectedCgroupKillFD=run:0,pass:0,skip:0, skipped=[], fail_events=0, invalid_json_lines=0` — the stale pin `docs/TODO.md:1025` already records (no Go file defines that name in base or HEAD) |
| 4 | `synthetic_goal_cleanup` | the test's own real-launcher invocation | 1 | `production_linux_v2_cleanup_witness_test.go:499: … Linux v2 native release requires an App admission check` |
| 5 | `stage_replay_create_started` | witness invoked; no durable stage | 1 | `:178: … create_started …`; cause named: the App's witness gate, §3.10 item 1 |
| 6 | `stage_replay_leaf_bound` | same | 1 | same at `:178`, `leaf_bound` |
| 7 | `stage_replay_launcher_bound` | same | 1 | same at `:178`, `launcher_bound` |
| 8 | `ack_loss_launcher_bound` | fixture request accepted; no ACK boundary | 1 | `:217: wait for exact launcher_bound ACK boundary: …`; cause named (witness gate) |
| 9 | `stale_leaf` | witness invoked; no durable stop receipt | 1 | `:630: …`; cause named (witness gate) |
| 10 | `migration` | same | 1 | `:616: …`; cause named (witness gate) |
| 11 | `stale_journal_run_key` | same | 1 | `:534: …`; cause named (witness gate) |
| 12-14 | `stale_journal_run_key_legacy`, `stale_journal_fence`, `stale_journal_subject` | same | 1 | `:269: …`; cause named (witness gate) |

#### 3.12.1 The containment stop named end to end

The round-11 early-phase probe (the App run once, as A, with the exact staged containment config and
request, §3.10 item 3) prints, for both containment sets:

```
-linux-v2-native-owner-witness: existing owner witness clone intent differs from the exact Subject
```

`validateLinuxV2NativeWitnessExistingJournal`
(`daemon/internal/app/linux_owner_witness_linux.go:2628-2633`) returns that error when a RunJournal
already exists for the request's RunKey and its clone intent does not match the request Subject.
The journal can only exist because the *witness's own* App run passed `seedAuthenticClaim`
(`:3857`), which is exactly the state the §3.9.1 inventory shows (one journal for the set, no stage
markers). `runFresh` (`:2839-2918`) seeds first and only then calls
`prepareLinuxV2NativeOwner` → `linuxV2OwnerResources.Preflight`
(`daemon/internal/app/linux_owner_v2_linux.go:80-150`), which requires the fixed-B XDG cache:

```go
cacheDir := filepath.Join(home, ".cache")
if err := linuxV2ValidateOwnedDirectory(cacheDir, owner.owner, false); err != nil { … }
```

The fixture provisions the **A** user's XDG cache (round 3, §3.7.3) and the B credential
directories, but never `$B_HOME/.cache`: the App's refusal is therefore deterministic, happens
after the claim is seeded (so the journal stays), and prints only to the App's stderr, which the
witness harness discards — the complete explanation of the containment rows' 30-second stage
waits. Round 12 provisions `$B_HOME/.cache` (B-owned mode 0700, fenced like the A-side cache) and,
for the Pi/OpenCode guests, the fixed-B Pi session directory the same preflight requires
(`linux_owner_v2_linux.go:159-170`, `linuxV2ValidateOwnedDirectory(sessionDir, …, true)`), which the
fixture had also never created.

### 3.13 Round 12 — the round-12 provisioning lands and fences green; the containment rows stop byte-identically (`45388258526`, head `99277aed`)

Job 28m55s (`11:54:50`→`12:23:45`); step 13 `Build the shared disposable Ubuntu guest rootfs base`
5m08s; step 14 `Run isolated A/B Linux v2 acceptance case sets` 22m28s; all 14 sets exited 1. The
base build stays one per job and the case-set step logs exactly 14
`Cloning the shared disposable guest rootfs base for this case set` lines against the one base path
(`/home/runner/work/_temp/linux-v2-native-rootfs-base-45388258526-1`) with no per-set debootstrap, so
the class-B stop is gone in this run. Every set's installed-daemon smoke started, stayed active with
a real MainPID and `NRestarts=0` (`scripts/ci/linux-v2-native-acceptance.sh:1570-1577`), and every
set reached `Stopping installed daemon.service before direct witnesses` (`:1579`); the Change-1
diagnostics branch (`:1512-1569`) never fired — the F1 live proof. The App's native gate printed
`owner witness config native harness version differs from the tested release` 210 times, the
F2-excluded cause of the ten OpenCode-vehicle rows. Head `99277aed`'s tree equals this tree's bytes
for all eight modified tracked files and `linux-v2-native-rootfs-base.sh` (blob hashes compared one
by one), so the table below is the final script bytes.

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | witness invoked; the App run as A seeds its claim and leaves one 1268-byte RunJournal, no stage marker | 1 | `production_linux_v2_native_witness_test.go:101: wait for requested production owner-witness barrier: owner-witness command did not reach its requested durable stage` (30.76 s); the post-seed refusal is not named, §3.13.1; the export then fails closed (`export-linux-v2-redacted-evidence.py:1592`, `containment witness native_binary does not match --native-binary`, document `native_binary:null`) |
| 2 | `claude_containment` | same; journal 1284 B | 1 | same at `:109` (30.79 s) |
| 3 | `shared` | legs `pi-baseline`, `opencode-baseline`, `cleanup-root`, `cleanup-mounts-root` pass; the fifth leg (`run_unexpected_cgroup_kill_fd_witness`, `:1902`) pins a test no Go file defines | 1 | gate `bad=TestInstalledLinuxOwnerRejectsUnexpectedCgroupKillFD=run:0,pass:0,skip:0, skipped=[], fail_events=0, invalid_json_lines=0` (`go test exit=0`); the stale pin `docs/TODO.md:1025` |
| 4 | `synthetic_goal_cleanup` | the test's own real-launcher invocation | 1 | `production_linux_v2_cleanup_witness_test.go:499: … start "/bin/sh": Linux v2 native release requires an App admission check` (2.00 s); `daemon/e2e/**` construction outside the owned files, already raised `DECISION_NEEDED` |
| 5 | `stage_replay_create_started` | witness invoked; no durable stage | 1 | `:178: wait for exact durable stage create_started: …` (31.42 s); cause named: the App's witness gate (F2-excluded) |
| 6 | `stage_replay_leaf_bound` | same | 1 | `:178`, `leaf_bound` (31.39 s); same cause |
| 7 | `stage_replay_launcher_bound` | same | 1 | `:178`, `launcher_bound` (31.36 s); same cause |
| 8 | `ack_loss_launcher_bound` | fixture request accepted; no ACK boundary | 1 | `:217: wait for exact launcher_bound ACK boundary: …` (31.42 s); same cause |
| 9 | `stale_leaf` | witness invoked; no durable stop receipt | 1 | `:630: wait for exact durable stop receipt: …` (31.37 s); same cause |
| 10 | `migration` | same | 1 | `:616: wait for exact OpenCode native_release boundary: …` (31.36 s); same cause |
| 11 | `stale_journal_run_key` | same | 1 | `:534: wait for exact durable stage handoff_committed: …` (31.33 s); same cause |
| 12-14 | `stale_journal_run_key_legacy`, `stale_journal_fence`, `stale_journal_subject` | same | 1 | `:269: wait for exact durable stage handoff_committed: …` (31.44/31.36/31.35 s); same cause |

#### 3.13.1 The round-12 hypothesis is falsified; the containment rows' cause stays unobservable

The round-12 provisioning is present and passes its own fences: `$B_HOME/.cache` is installed
B-owned mode 0700 and fenced (`scripts/ci/linux-v2-native-acceptance.sh:570-578`), and a fence
failure would have stopped both containment sets at the provisioning step — neither log shows one.
Rows 1-2 are nevertheless identical in shape to round 11 (§3.12): the same probe line
(`existing owner witness clone intent differs from the exact Subject`), the same surviving RunJournal
(1268 B in both rounds; claude 1283→1284 B), the same 30.7 s barrier wait, and the same fail-closed
export (2 occurrences). §3.12.1's "missing `$B_HOME/.cache`" cause therefore does not explain the
stop.

The probe cannot supply the missing name by construction. `probe_witness_app_refusal`
(`:1638-1650`) runs the App a second time against the same StateDir, where the witness's own App run
has already seeded a RunJournal for the request's RunKey; for a non-`stage_replay` case the App
returns at the existing-journal check (`daemon/internal/app/linux_owner_witness_linux.go:502-503`,
`validateLinuxV2NativeWitnessExistingJournal` `:2625-2633`), and a witness that stops right after the
seed leaves `LinuxCloneIntent` nil, so the printed message is unconditional. The probe's premise
comment (`:1636-1637`, "the witness command refuses before it writes any fixture state") is
falsified by the journal line its own inventory prints.

Consequence for the acceptance contract: rows 1-2 reach the witness's own App run and the claim
seed, but not the credential-dependent step, and their stop has no named cause — the judging clause
"codex_containment and claude_containment … must reach the credential-dependent step or pass" is not
met by this run. Raised as `DECISION_NEEDED` in the stage report. The smallest next step is a probe
that observes the first post-seed refusal: run the App once on a fresh StateDir (or with the seeded
journal moved aside), or capture the witness's own stderr instead of `/dev/null`
(`daemon/e2e/production_linux_v2_owner_cli_test.go:52-56`), then one more scratch run (5 of the
ruling's 6 used). No fixture change is made here: the tree must stay at head `99277aed`'s bytes for
this table to describe the final script bytes.

### 3.14 Round 13 — the fifth leg's pin re-pointed; the `shared` set reaches its owner leg (`45398550682`, head `d4fdb149`)

Conductor ruling (2026-11-24, "close scope and land"): `shared`'s fifth leg pinned
`TestInstalledLinuxOwnerRejectsUnexpectedCgroupKillFD`, which no Go file defines. Successor by
intent: `TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD`
(`daemon/native/linux-gate/protocol/launcher_integration_linux_test.go:122`) — the same package the
leg already runs (`./native/linux-gate/protocol`), the same opt-in environment
(`SYMMETRY_LINUX_OWNER_TEST_SCRATCH=1`, `SYMMETRY_LINUX_OWNER_TEST_LEAF`, which the `owner-fd-scratch`
profile sets at `linux-v2-native-acceptance.sh:1796-1800`), and the same rule (an unexpected fifth
cgroup-control FD in a guarded RELEASE is rejected with `SIGCHLD`/code 1, the exact scratch leaf
stays unpopulated and PID-free). `git log -S 'TestInstalledLinuxOwnerRejectsUnexpectedCgroupKillFD'
--all` finds the name only in the script and docs since `41e64851` — no Go file ever defined it —
and the ruling's pointer to #272's `linuxpreflight` tests names a different package than the leg's
`run_go_test` invocation.

Fix: the test-name re-point plus the leg's rename (`run_unexpected_cgroup_procs_fd_witness`, event
file `linux-owner-unexpected-cgroup-procs-fd.json`, `:1900-1914`, `:2864`, `:2887`). Run
`45398550682` (linux job 20m06s; base-build step 1m03s on the first attempt; case-set step 17m26s):
every set's shape equals round 12's except `shared`, and the leg reports
`verified TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD: run=1 pass=1 skip=0` — the
ruling's required proof. The set then advances into the owner leg
(`TestInstalledLinuxOwner(BoundaryScratch|CloneCheckoutScratch|CloneCleanupScratch|CloneCleanupParentDeathScratch)`)
and stops there, `bad=…BoundaryScratch=run:1,pass:0; …CloneCheckoutScratch=run:1,pass:0;
…CloneCleanupParentDeathScratch=run:1,pass:0, fail_events=4` (`CloneCleanupScratch` passes):

| Test | First failing line |
| --- | --- |
| `TestInstalledLinuxOwnerCloneCleanupParentDeathScratch` | `clone_cleanup_linux_test.go:207: root fixture lacks the later entry needed to keep the deleter busy: lstat /opt/symmetry-owner-test/clone-root/binding-cleanup-binding/run-cleanup-parent-death/generation-1/z-later-000000: no such file or directory` |
| `TestInstalledLinuxOwnerCloneCheckoutScratch` | `clone_integration_linux_test.go:240: one-use B clone RELEASE and durable checkout ACK/native exec: … clone status frame magic or version is invalid` |
| `TestInstalledLinuxOwnerBoundaryScratch` | `launcher_integration_linux_test.go:439: one-use fixed-B RELEASE and same-PID exec: native exec failed after RELEASE: errno=1` |

The first line is a fixture defect, proven mechanically and fixed in round 13b; the other two sit
inside product tests (`daemon/native/linux-gate/**`, unreachable until this leg's pin was fixed) and
are recorded as a follow-up in `docs/TODO.md`.

### 3.15 Round 13b — the fixture name width; the final all-14 run (`45402706492`, head `8339f991`)

The fixture wrote `z-later-{index:05d}` (five digits, `z-later-00000`) while the test reads
`z-later-000000` (six digits, `clone_cleanup_linux_test.go:202`), so the test's precondition failed
on the leg's first-ever execution. Probe on the fixture's own format strings:
`width=5: names=['z-later-00000', …] test-literal-present=False` and
`width=6: names=['z-later-000000', …] test-literal-present=True`. Fix: `06d` (`:628`). The
before/after pair is run `45398550682` (`:207`, precondition) → run `45402706492` (`:259`, inside
the test's intended path: `A cleanup caller exited without the requested parent-death signal`).

Final all-14 run: head `8339f991`, linux job 20m42s (`13:23:42Z`–`13:44:24Z`); base-build step 29 s
on the first attempt (`13:24:56`–`13:25:25`); case-set step 18m56s (`13:25:25`–`13:44:21`); 14
`Cloning the shared disposable guest rootfs base` lines; all 14 sets exited 1. Counts over the
failed step: `through Before` 0, `inspect B credential directory` 60 (the per-config probes for
kinds a guest does not provision, §3.9.1), `debootstrap` 0, `generic Control claim runtime` 0,
`dubious ownership` 0, `build cache` 0, Change-1 diagnostics branch 0 firings (F1 live again),
`retired N runtime(s)` lines `0` once and `5` thirteen times (F2 live),
`owner witness config native harness version differs from the tested release` 210 (the F2-excluded
OpenCode cause), and six `verified …: run=1 pass=1 skip=0` lines, all in `shared`:
`TestProductionLinuxV2UnprovisionedHostRefusal`,
`TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction`,
`TestLinuxLegacyOpenCodeRefusesBeforeHealthRequest`,
`TestInstalledLinuxOwnerCleanupRejectsBCallerScratch`, `TestCloneCleanupRejectsCrossMountEntry`,
`TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD`.

| # | Case set | Last step reached | Exit | First failing line / check |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | named witness invoked; the App seeds its claim and stops before any stage | 1 | `production_linux_v2_native_witness_test.go:101: wait for requested production owner-witness barrier: owner-witness command did not reach its requested durable stage`; post-seed refusal unnamed, follow-up in `docs/TODO.md` |
| 2 | `claude_containment` | same | 1 | same at `:109` |
| 3 | `shared` | legs 1-5 pass (`pi-baseline`, `opencode-baseline`, `cleanup-root`, `cleanup-mounts-root`, the cgroup FD leg) and `TestInstalledLinuxOwnerCloneCleanupScratch` passes; the owner leg's other three tests fail | 1 | `clone_cleanup_linux_test.go:259: A cleanup caller exited without the requested parent-death signal`; `clone_integration_linux_test.go:240: … clone status frame magic or version is invalid` (with the test's cleanup line `:113`); `launcher_integration_linux_test.go:439: … native exec failed after RELEASE: errno=1` |
| 4 | `synthetic_goal_cleanup` | the test's own real-launcher invocation | 1 | `production_linux_v2_cleanup_witness_test.go:499: … start "/bin/sh": Linux v2 native release requires an App admission check`; follow-up in `docs/TODO.md` |
| 5 | `stage_replay_create_started` | witness invoked; no durable stage | 1 | `production_linux_v2_native_witness_test.go:178: wait for exact durable stage create_started: …`; App witness gate (F2-excluded OpenCode config) |
| 6 | `stage_replay_leaf_bound` | same | 1 | same at `:178`, `leaf_bound` |
| 7 | `stage_replay_launcher_bound` | same | 1 | same at `:178`, `launcher_bound` |
| 8 | `ack_loss_launcher_bound` | fixture request accepted; no ACK boundary | 1 | `:217: wait for exact launcher_bound ACK boundary: …`; same cause |
| 9 | `stale_leaf` | witness invoked; no durable stop receipt | 1 | `:630: wait for exact durable stop receipt: …`; same cause |
| 10 | `migration` | same | 1 | `:616: wait for exact OpenCode native_release boundary: …`; same cause |
| 11 | `stale_journal_run_key` | same | 1 | `:534: wait for exact durable stage handoff_committed: …`; same cause |
| 12-14 | `stale_journal_run_key_legacy`, `stale_journal_fence`, `stale_journal_subject` | same | 1 | `:269: wait for exact durable stage handoff_committed: …`; same cause |

Every witness row also prints `:1838: production Linux v2 StateDir remains at fixture-provided path
/var/lib/symmetry-v2; private scratch retained at …` as its cleanup context. No row ends at class A,
B, C, D or E, and no row ends at a silent daemon start failure: rows 1-2 end inside their named
witness with the App's refusal unnamed (follow-up), rows 5-14 end at the App's own witness gate
(F2-excluded OpenCode configs), row 4 ends at its test's own launcher assertion (follow-up), and
row 3 reaches its set's owner leg with three named product-test stops (follow-up). This is the
last scratch run of the stage, on the final script bytes (`05d81032…`, blob `19ed563b…`).

## 4. Gates (local, final tree)

| Gate | Command | Exit | Counts |
| --- | --- | --- | --- |
| Syntax | `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | — |
| Syntax | `bash -n scripts/ci/linux-v2-native-rootfs-base.sh` | 0 | — |
| Shellcheck | `shellcheck --severity=warning scripts/ci/linux-v2-native-acceptance.sh` | 1 | 16 findings (3 error, 13 warning) — identical code histogram to the same command on `668e75de`'s copy (16: 3 error, 13 warning), delta 0 |
| Shellcheck | `shellcheck --severity=warning scripts/ci/linux-v2-native-rootfs-base.sh` | 0 | 0 findings (new file) |
| Shellcheck | `shellcheck --severity=warning scripts/ci/linux-v2-native-tools.sh` | 0 | 0 findings; the file is at base bytes |
| Actionlint | `actionlint .github/workflows/linux-native-v2-acceptance.yml` (v1.7.12, the version `ci.yml` pins) | 0 | 0 findings |
| Daemon tests | `go test ./internal/linuxpreflight/` (Go 1.27.4, `GOOS=linux`, WSL Ubuntu-24.04; the CI `daemon` job's own package) | 0 | ok; targeted `-run` pass shows 7 `owner/<key>` refuse rows plus the accept block |
| Daemon full suite | `go test ./... -count=1` in `daemon/` (same runner) | 1 | every package ok except `internal/platform`, which fails only in this local sandbox: the default WSL user is root (`wire harness requires an unprivileged A process: uid=0 euid=0`) and its `/tmp` parents are group/other-writable (`Linux supervisor witness marker parent is not a safe witness directory`). Both preconditions are environment, in files this change does not touch; the CI `daemon` job runs as the non-root runner user. The change's own package and 26 others pass. |
| Daemon format/vet | `gofmt -l .` / `go vet ./...` in `daemon/` | 0 | empty file list / no diagnostics |
| Elixir parse | `Code.string_to_quoted!` on `scripts/ci/provision-linux-v2-control-claims.exs` (Elixir 1.20.4/OTP 29) | 0 | parse OK |
| Ruff | `ruff check scripts/ci/write-linux-v2-owner-request.py` / `ruff format --check` (0.17.5, the version `pyproject.toml` requires) | 0 | all checks passed / already formatted |
| Workflow lint | `actionlint .github/workflows/scratch-native-burndown.yml` | 0 | 0 findings (throwaway copy of the real job) |

Every row above was re-run on the round-12 bytes (`ed6b023f…`, the table's hash) after the last
fixture edit, with the same exit codes: `bash -n` 0/0/0, shellcheck 16/0/0 findings, actionlint 0,
`go test ./internal/linuxpreflight/` ok, `go vet` 0, `gofmt -l` empty, Elixir parse 0, ruff 0.
`go vet ./internal/linuxpreflight/` (Go 1.27.4, `GOOS=linux`) is exit 0 as well.

Stage-close re-run (2026-11-24, after round 12's run and before the report; shellcheck 0.11.0,
actionlint 1.7.12, Go 1.27.0 `GOOS=linux` in WSL Ubuntu-24.04, Elixir 1.20.4/OTP 29, ruff 0.17.5):
`bash -n` 0/0, shellcheck exit 1 with 16 findings (3 error, 13 warning) on the acceptance script and
0/0 on the two helpers, actionlint 0, `go test ./internal/linuxpreflight/ -count=1` 0 with the 7
`owner/<key>` refuse rows plus the accept block, `gofmt -l .` empty, `go vet ./...` 0, Elixir parse
0, `ruff check`/`ruff format --check` 0. The only files edited after the round-12 run are this
record and the plan; the eight subject files still hash to §1's values, so the round-12 run's bytes
are the final bytes.

Stage-close re-run 2 (2026-11-24, after rounds 13 and 13b, before the final report; shellcheck
0.11.0, actionlint 1.7.12, ruff 0.17.5 via `python3 -m ruff`, Elixir 1.20.4/OTP 29 in WSL
Ubuntu-24.04): `bash -n scripts/ci/linux-v2-native-acceptance.sh` 0 and
`bash -n scripts/ci/linux-v2-native-rootfs-base.sh` 0; `shellcheck --severity=warning` exit 1 with
16 findings (3 error, 13 warning) on the acceptance script — the code histogram is identical to the
same command on `668e75de`'s copy (16), delta 0 — and 0 findings on `linux-v2-native-rootfs-base.sh`
and `linux-v2-native-tools.sh`; `actionlint .github/workflows/linux-native-v2-acceptance.yml` 0 and
`actionlint .github/workflows/scratch-native-burndown.yml` 0; `ruff check`/`ruff format --check` 0/0
on `scripts/ci/write-linux-v2-owner-request.py`; `Code.string_to_quoted!` parse 0 on
`scripts/ci/provision-linux-v2-control-claims.exs`. Rounds 13/13b changed only
`scripts/ci/linux-v2-native-acceptance.sh` (no `daemon/**`, `control/**` or workflow change), so the
Go gates above (unchanged files) are not re-run; between run `45402706492` and this report only this
record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and the plan were edited, and the final
subject bytes are the ones §1 hashes (`05d81032…`, git blob `19ed563b…` — run `45402706492`'s head
`8339f991` blob, §3.15).

The 16 shellcheck findings are pre-existing (SC2174, SC2154 ×8, SC2043, SC2024, SC2276 ×2, SC2289,
SC1078, SC2288) at the same sites in both trees; the code-histogram delta is zero. The F1 test needs
Linux (`_linux_test.go`/`_linux.go` build constraints) but no root: it drives
`validateEffectiveLinuxSystemdProperties` directly on property maps. `scripts/ci/*.exs` is outside
the Control formatter and Credo inputs, so no Elixir style gate covers the fixture script; the
scratch run compiles and executes it.

Secret scan: every downloaded run log (rounds 1-6, F-ruling rounds 1-6, and rounds 7-9) contains no
credential value. Runs `45369766498` and `45374416995` hold exactly one `gho_…` token — the
contract's dummy `gho_scratchdummy0000000000000000000000000000` — one dummy `sk-scratch-dummy`, two
`***` masquerades (the generated operator token), and no `ghp_`/`github_pat_`, `Bearer`, or
`lease_token` value. The dump's inventories print names and sizes only, its journal filter prints
the daemon's own stderr prefixes only, and the added probes print unit state and the App's own
refusal lines, none of which carry credentials.

Fix-stage update (2026-11-24, §10.6): the review's STD-01…STD-03 changed the acceptance script, so
§1's script row is refreshed and §10.6 re-runs the script gates at that tree (`bash -n` 0;
`shellcheck` 16 findings, delta 0; `actionlint` 0). The rows above describe the implement stage's
trees.

## 5. Reached failure classes behind the smoke

### 5.1 Class C — named from the journal; root outside the owned files

Facts from run `45228382743` (same guest, 47 ms apart):

1. the fixture's direct preflight passes —
   `machine_run_as symmetry-a … -verify-linux-owner-install -config /etc/symmetry/daemon.json`
   printed `read-only preflight does not launch B, certify a future cgroup leaf, or attest a future
   per-run Git metadata ACL`;
2. `systemctl start symmetry-daemon.service` then fails, and its own `ExecStartPre` reports
   `effective symmetry-owner.service unit violates the owner service contract: effective owner unit
   depends on or orders against symmetry-daemon.service through Before`
   (`ExecStartPre … status=1`, `ExecMainStatus=0`, `Result=exit-code`).

The owner unit's staged fragment cannot carry that edge: `validateLinuxSystemdCandidate` rejects
`before`/`bindsto`/`requires`/… on the owner unit (`daemon/internal/linuxpreflight/systemd_linux.go:496`),
and the direct run validates exactly that file plus its drop-ins. The effective `Before` entry is
systemd's inverse registration of the daemon unit's own required
`After=local-fs.target symmetry-owner.service` (`scripts/systemd/symmetry-daemon.service.in:3`):
systemd stores both directions of an ordering dependency (`src/core/unit.c:3066-3082`, release
`v259`: `inverse_table[UNIT_AFTER] = UNIT_BEFORE` in `unit_add_dependency_impl`) and the `Before`
D-Bus property serializes that map (`src/core/dbus-unit.c` `property_get_dependencies`, the same
file's `SD_BUS_PROPERTY("Before", …)`). The check under `policy.owner` (`systemd_linux.go:233`)
rejects it, while the daemon policy in the same file requires the daemon unit to keep both `After=`
and `BindsTo=` on the owner. Since the daemon unit must be loaded for its own `ExecStartPre` to run,
that start-time read sees the inverse entry and rejects it; the exact point at which PID 1 registers
the entry is not pinned by the evidence collected (see §8).

Root is in `daemon/**` ⇒ contract Change 4's `DECISION_NEEDED`, not an in-scope fix. Smallest
proposed fix: drop `"Before"` from the owner policy's effective-key list
(`systemd_linux.go:233`); the staged-fragment and drop-in validations already forbid the owner
declaring `Before=` or any other edge to the daemon, so no constraint is lost. Raised also in
`docs/TODO.md` (one entry, with the same evidence path).

Resolution (conductor ruling, 2026-11-24): the fix is in scope, and the smallest proposed fix is
the one applied — see §6.1 for the clause tests, the pre-fix failure and both mutant rows. The
`docs/TODO.md` decision entry moved to `docs/archive/CLOSED-TODO.md`; what stays open is the
class-E product question in §5.3. Live proof: run `45318768814` reports zero class-C lines and 13
smokes that start, stay active and stop cleanly inside the unit (§3.7).

### 5.2 Class D — the legacy-task attempt pin, fixed in the owned fixture helper

The final narrow run `45303576904` surfaced a failure the baseline could never reach (class B and C
stopped every set earlier): `shared` died in Control claim provisioning, before its smoke:

```
** (Postgrex.Error) ERROR 23514 (check_violation) new row for relation "tasks" violates check constraint "tasks_goal_fields_all_or_none"
    constraint: tasks_goal_fields_all_or_none
    .../scripts/ci/provision-linux-v2-control-claims.exs:417: LinuxV2ControlClaimFixture.launch_one_shot_work_item!/2
```

Root, proven by reading the schema contract: `max_run_attempts` belongs to the goal-admission group
(`control/priv/repo/migrations/20260909000000_add_goal_0006_control_plane.exs:1227` and the
identical re-create in `20260910000000_add_planning_task_identity_guards.exs`: the field may be
non-NULL only together with `goal_id`, `goal_revision`, `context_snapshot_id` and `admission_key`).
The fixture pinned it with a raw `UPDATE` on a *legacy* task that `Workspaces.launch_work_item`
creates through `ensure_legacy_work_item` — goal-less by construction. Sibling fixtures respect the
contract: `scripts/ci/omo-control-native-fixture.exs:170,283` admits through the goal path and
asserts `task.max_run_attempts == 1`, and
`scripts/ci/credentialed-provider-witness.exs:494-510` inserts the whole goal group. Nothing reads
the field for a legacy task (`grep max_run_attempts daemon/` → only goal-budget contract structs;
Control's readers are goal-scoped), so the pin was inert except for the constraint violation.

Fix in the owned `scripts/ci/*linux-v2*` helper: `launch_one_shot_work_item!` now only launches and
returns the task id, with one comment naming the constraint. Class D's before/after pair is
`45303576904` (constraint violation) → `45305439399` (provisioning passes, the set reaches the
named class-C smoke).

### 5.3 Class E — the runtime guard and Control's cross-set assignment

Reached by `shared` in the final run once class D stopped failing. The fixture launches the
unprovisioned-refusal work item, Control assigns a Run (`INSERT INTO "runs" … generation 1`), and
the fixture's guard then raises:

```
** (RuntimeError) generic Control claim runtime did not match the exact non-native Work profile/resource
    .../scripts/ci/provision-linux-v2-control-claims.exs:205
```

The guard (`provision-linux-v2-control-claims.exs:196-206`) requires the assigned runtime to carry
the case's own `agent_profile`, `workspace == "native"`, **this case set's**
`repository_resource_id`, no `adapter` capability, and `provider_access` matching the harness.

Control's rule for a goal-less task (`orchestration/assignment.ex:260-286`) joins the Task to every
online Runtime matching `runtime_matches_binding()` — `agent_profile`, `workspace`, and
`capabilities @> required_capabilities` (`assignment.ex:291-304`); the repository resource is *not*
part of the match — then takes `order_by: [asc: task.inserted_at, asc: runtime.inserted_at]`, i.e.
the **oldest** matching runtime, and requires a fresh heartbeat
(`last_heartbeat_at > now − 3 × heartbeat_interval_ms`, `assignment.ex:267-273`).

The fixture registers four fresh generic runtimes per case set, each under its own machine, and the
controller stamps every registration with the server's `heartbeat_interval_ms`
(`symmetry_control_web/controllers/daemon_controller.ex:37-54`), which this workflow's environment
sets to 3 600 000 ms. So a case set's runtimes stay assignable for about three hours, and in an
all-14 run the `opencode-native` task of set 3 matches the `opencode-native` runtimes registered by
sets 1 and 2 as well; the oldest wins, its `repository_resource_id` points at a *previous* set's
OpenCode resource, and the guard's equality fails. The failure is deterministic for `shared` in any
all-14 dispatch (it is the first legacy task scheduled after two other sets registered runtimes),
and independent of the class-D pin: Control's legacy assignment never reads `max_run_attempts`.

The failing clause is necessarily the repository equality: the run log's `INSERT INTO "tasks"`
parameters give this task `agent_profile = "opencode-native"` and `workspace = "native"` (the
fixture's own values), and every `opencode-native` runtime the fixture registers has no `adapter`
capability and `provider_access` absent, so profile, workspace, adapter and `provider_access` all
hold. The only runtimes that satisfy that profile/workspace/capability match are the three
`opencode-native` generic runtimes of sets 1-3; `asc: runtime.inserted_at` picks set 1's, registered
68 s earlier and still online inside the three-hour heartbeat window, whose
`repository_resource_id` is set 1's OpenCode witness repository — not this set's. A `shared`-only
job has just its own runtime, so the narrow diagnostic run distinguishes this reading from an
intra-set cause.

No fix exists inside the owned files without weakening an assertion:

1. fixture-side, drop the repository equality from the guard — the assertion exists to prove the
   claim is served by a *generic* runtime bound to this case's repository; weakening it is the
   kind of change the repository contract forbids without independent review;
2. fixture-side, retract a set's runtimes at its end — Control has no path for it: the session PUT
   upserts only the keys it lists (`orchestration/registry.ex:163-238`), so previous sets' runtimes
   cannot be taken offline by the fixture;
3. Control-side, include the task's repository resource in `runtime_matches_binding` (or prefer the
   runtime whose binding matches the task's resource) — `control/**` is outside this goal's scope;
4. environment-side, shorten `SYMMETRY_HEARTBEAT_INTERVAL_MS` — it sits in the workflow's job-level
   `env:`, which the contract forbids touching, and it would also shrink the window the witness
   legs rely on.

Raised as a second decision alongside class C. The narrow diagnostic run that would have confirmed
the cross-set reading directly (`45310491984`, `shared` alone on the same fixture bytes — a fresh
database has no earlier runtimes, so the guard could then only fail for an intra-set reason) never
reached a case set; see §3.4. The reading therefore stays derived from the code paths and the
logged task profile rather than observed on a passing guard.

Resolution (conductor ruling, 2026-11-24): fixture-side isolation, no Control product change and no
relaxed guard — option 2's premise held only for Control's HTTP session API; the fixture runs in
Control's own Repo context, so it can apply the same `Runtime.changeset(%{status: "offline"})`
transition heartbeat expiry uses. §6.2 records the change and §3.7 the run-log counts. The product
question (legacy assignment ignoring the task's repository resource) stays open in `docs/TODO.md`
for Control review; option 4 stays rejected, so the workflow's heartbeat interval is unchanged.
Live proof: run `45318768814`'s retirement counts are `0`, `4`, then `5` twelve times, and no row
fails the runtime guard (§3.7).

## 6. F1 clause tests, mutants, and the F2/F4 proofs

### 6.1 F1 — owner `Before` accepted, every other owner edge still refused

Fix: `daemon/internal/linuxpreflight/systemd_linux.go:235` — only `Before` leaves the effective-key
refusal list; `After`, `BindsTo`, `Requires`, `Requisite`, `Wants`, `PartOf` and `Conflicts` stay.
The staged-fragment and drop-in validations that forbid the owner from *declaring* the edge are
untouched and still refuse: `systemd_linux.go:498` (`rejectLinuxSystemdUnitLists` on the owner
candidate) and `systemd_linux.go:773-784` (`validateLinuxSystemdOverrides` permits only
`environment`).

Clause tests (`daemon/internal/linuxpreflight/preflight_linux_test.go`), run with
`go test ./internal/linuxpreflight/ -count=1 -run 'TestLinuxSystemdEffectivePropertiesRequireLiveOwnerAndExactCommands|TestLinuxSystemdCandidateEnforcesExactOwnerAndDaemonTopology'`:

| Clause | Test | Before the fix | Observed now |
| --- | --- | --- | --- |
| accept: effective owner `Before=symmetry-daemon.service` | `:870-876` | FAIL: `effective owner unit depends on or orders against symmetry-daemon.service through Before` | PASS |
| refuse: each remaining key listing the daemon | `:877-889` (7 `owner/<key>` rows) | PASS | PASS |
| refuse: declared `Before=symmetry-daemon.service` in the owner fragment | `:525-527` | PASS | PASS |
| refuse: declared `before` in an owner drop-in | `:539-541` | PASS | PASS |
| shipped owner/daemon templates still validate | `TestLinuxSystemdCandidatesAcceptShippedTemplates` | PASS | PASS |

The accept clause failing on the pre-fix source, with the production message:

```
--- FAIL: TestLinuxSystemdEffectivePropertiesRequireLiveOwnerAndExactCommands (0.00s)
    preflight_linux_test.go:876: systemd's inverse owner ordering edge to the daemon was rejected: effective owner unit depends on or orders against symmetry-daemon.service through Before
```

Mutant rows:

| Mutant | Expected | Observed |
| --- | --- | --- |
| re-add `Before` to the refusal list (revert F1) | accept clause reddens | `preflight_linux_test.go:876: systemd's inverse owner ordering edge … through Before` |
| drop `After` from the refusal list | `owner/After` row reddens | `--- FAIL: TestLinuxSystemdEffectivePropertiesRequireLiveOwnerAndExactCommands/owner/After … effective owner After edge to the daemon was accepted: <nil>` |
| restore the F1 list | package green | `ok github.com/wxxb789/symmetry/daemon/internal/linuxpreflight 0.090s` |

Environment: Linux GOOS required (`_linux.go`/`_linux_test.go` build constraints), no root — the
test drives `validateEffectiveLinuxSystemdProperties` on property maps. Live proof: the scratch
run's class-C rows clear the `ExecStartPre` owner check (§3.7).

### 6.2 F2 — earlier case sets' runtimes retired before provisioning

`scripts/ci/provision-linux-v2-control-claims.exs:98-101` calls
`retire_earlier_case_set_runtimes!/0` (`:425-436`) at the start of every case set's `prepare` mode,
before the case set's own machine registration (`linux-v2-native-acceptance.sh:1387`), so every
runtime still `online` in the job's shared Control database belongs to an earlier case set by
construction. The transition is Control's own schema and changeset
(`Runtime.changeset(%{status: "offline"})`), the same one heartbeat expiry applies
(`control/lib/symmetry_control/orchestration/transitions.ex:511-544`): no Control API, migration or
guard change, and the fixture's runtime guard keeps its exact profile/workspace/resource assertion
(`provision-linux-v2-control-claims.exs:200-206`). Each `prepare` prints
`retired N runtime(s) left online by earlier case sets`, which names the class-E proof in the run
log: §3.7 lists the observed counts per set.

### 6.3 F4 — five base-build attempts

`scripts/ci/linux-v2-native-rootfs-base.sh:59` caps the retry loop at 5 attempts (was 3), keeping
the per-attempt 600 s bound, the `30 s × attempt` backoff, the image-apt mirror rule, the
acquire-phase retry filter and the fail-closed exit with the attempt log's last 40 lines. The
class-B before/after pair still cites run `45200662557` (§3.4).

### 6.4 The ack-loss barrier-validation stop — fixture-side root, fixed in the owned writer

Runs `45330552341` (round 5, row 8) and `45369766498` (round 7, row 8) both stop the
`ack_loss_launcher_bound` set before the App starts:

```
production_linux_v2_native_witness_test.go:217: load isolated production owner witness request:
owner-witness barrier directory or timeout is invalid
```

`linuxV2ReadOwnerWitnessRequestWithBarrierState`
(`daemon/e2e/production_linux_v2_native_witness_test.go:1583-1589`) computes
`expectedHold = testCase.barrierStage` for `case = stage_replay` and refuses unless
`barrier.Directory` is canonical under `/opt/symmetry-owner-test`, `1 <= timeout_seconds <= 30` and
`barrier.HoldAfter == expectedHold`; its ACK-loss branch (`:1604-1610`) additionally requires a
non-empty `drop_ack_after` equal to `barrier.HoldAfter`. The App enforces the same shape
(`daemon/internal/app/linux_owner_witness_linux.go:2381-2387`: `HoldAfter` must be a supported
hold and `drop_ack_after` must equal it).

The fixture's writer violated both: `run_ack_loss_replay_case`
(`scripts/ci/linux-v2-native-acceptance.sh:2222`, pre-fix) passes the selector
`drop_ack_after=<stage>` as the *hold* argument, and `write-linux-v2-owner-request.py` (pre-fix
lines 79-85) used it verbatim:

```python
hold_after = sys.argv[8]
ack_loss_stage = hold_after.removeprefix("drop_ack_after=")   # validated, then discarded
barrier = {..., "hold_after": hold_after}                     # the raw selector
# no top-level drop_ack_after is ever written
```

so `barrier.hold_after` became the literal string `drop_ack_after=launcher_bound` and no
`drop_ack_after` field existed. The stripped variable's only use was the validation, i.e. the
prefix was meant to select the drop stage. The pre-fix replay derivation rewrote
`barrier.hold_after` on its copy, which cannot repair the first request.

Fix (owned files, round 8):

- `write-linux-v2-owner-request.py`: strip the selector into `drop_ack_after`, keep
  `hold_after = <stage>`, and write the top-level `drop_ack_after` only when selected.
- `run_ack_loss_replay_case`: the replay copy now asserts `barrier.hold_after == stage` and
  `drop_ack_after == stage` before deleting the selector, so the replay request carries the same
  barrier with no second drop — the shape `linuxV2RequireSameReplayRequest`
  (`daemon/e2e/production_linux_v2_owner_cli_test.go:1542-1556`: `first.Barrier == replay.Barrier`,
  `first.DropAckAfter == dropStage`, `replay.DropAckAfter == ""`) and the replay load path
  (`replayCase.ackLossReplay = false`, `:3427-3432`) demand.

Local probe (the real writer against a stubbed Control claim endpoint, both byte versions; writer
sha256 `4c73bf5c…` pre-fix and `17c36b13…` post-fix):

| Request | `barrier.hold_after` | `drop_ack_after` | First load | Replay load |
| --- | --- | --- | --- | --- |
| pre-fix writer | `drop_ack_after=launcher_bound` | absent | reject: owner-witness barrier directory or timeout is invalid | reject: response-loss replay changed the exact claim, RunKey, fence, Subject, or held boundary |
| post-fix writer | `launcher_bound` | `launcher_bound` | accept | accept |

Mutant rows with run ids: before `45330552341` (round 5, row 8) and `45369766498` (round 7, row 8),
each stopping at the load in 0.6 s; after: run 8 (`45374416995`). The sibling fixture
`scripts/ci/rootless-compose-native-acceptance.sh:421-424` passes the plain hold `native_release`,
whose request bytes the fix does not change.

## 7. Scope

Owned files touched: `scripts/ci/linux-v2-native-acceptance.sh`,
`scripts/ci/linux-v2-native-rootfs-base.sh` (new `scripts/ci/*linux-v2*` helper),
`scripts/ci/write-linux-v2-owner-request.py` (one `scripts/ci/*linux-v2*` helper: the ACK-loss
selector fix, §6.4), `scripts/ci/provision-linux-v2-control-claims.exs` (one
`scripts/ci/*linux-v2*` helper: the class-D
pin removal and the F2 retirement),
`.github/workflows/linux-native-v2-acceptance.yml` (one step-level prerequisite; step only),
`daemon/internal/linuxpreflight/systemd_linux.go` and
`daemon/internal/linuxpreflight/preflight_linux_test.go` (F1, added by the conductor ruling),
`docs/plans/2026-11-24-rf-acc-native-burndown-plan.md` (addenda); `docs/TODO.md` (the class-E
Control entry rewritten to the C1 verdict with the two discriminating tests, §3.15/§5.3; a new
section `## rf-acc-native-burndown: Linux v2 native acceptance follow-ups (2026-11-24)` with the
three recorded follow-ups — the containment witness's discarded App stderr plus
`synthetic_goal_cleanup`'s missing admission check, the three product-test stops of `shared`'s
owner leg, and the OpenCode F2-excluded rows pending `rf-acc-pi-durability-matrices`; the stale
`linux-v2-native-acceptance.sh:1804` pin entry removed, moved to `docs/archive/CLOSED-TODO.md`);
`docs/archive/CLOSED-TODO.md` (the class-C decision entry, closed; the stale-pin entry, closed
with the successor and both run ids), and this record. The scratch loop's remote branch
`scratch/rf-acc-native-burndown` carried the throwaway `.github/workflows/scratch-native-burndown.yml`
and `scripts/ci/scratch-gh-shim` only, plus the push-time tree at each round; heads `8d84ecfc`, then
the F-ruling rounds `c7053e2a`, `926e26e7`, `d2375e8f`, the cap-ruling rounds `9fadfda2` (7),
`26ba0065` (8), `a51d26cb` (9), `196bfb27` (10), `daeaedda` (11), `99277aed` (12, run
`45388258526`), `d4fdb149` (13, run `45398550682`) and `8339f991` (13b, the final all-14 run
`45402706492`). Deleted from the remote at the end of the stage:
`git push origin --delete scratch/rf-acc-native-burndown` printed `- [deleted] …` (exit 0),
`git fetch --prune origin` removed the remote-tracking ref, and
`git ls-remote origin scratch/rf-acc-native-burndown` prints nothing (exit 0). The local branch
`scratch/rf-acc-native-burndown` is retained so a follow-up scratch loop can re-push the same bytes;
the two throwaway files stay untracked in the working tree and never reach the PR branch.

## 8. Residual risks and known limits

- Dummy credentials: in rounds 1-3 no case set reached a provider turn or a GitHub provider action.
  Every credential-dependent step remains unproven; the owner's dispatch of the real workflow is
  still the only evidence for it. Round 2 is the first round whose rows reach the *named* witness
  invocation at all (round 1 stopped at fixture scaffolding), so the run's progress is bounded by
  the guest-side Go invocation, not by the credential surface.
- The OpenCode `TestedVersion` 2.3.0 pin staleness stays unproven in effect (`F2-excluded`; pin and
  render tokens untouched by decision). Rounds 1-3 never reach the OpenCode-vehicle sets' version
  gate: round 1 stops at the fixture's own invocation guard, round 2 at the guest Go build cache,
  and the gate sits behind a successful witness start.
- The shim is in the loop because the fixture's `gh auth login` (the `:292` gate) needs a real
  `gho_`-class OAuth token and the protected Environment never admits the scratch branch: without it
  every case set stops at `Credentialed manual acceptance …` → `gh auth login`, which would hide
  A/B/C. The shim answers only `gh auth login` for the dummy token; the remote branch is deleted
  (§7), and the local copy stays untracked in the working tree, outside the PR branch.
- The base build remains the job's only mirror exposure: `45305439399` exercised the retry path for
  real (three acquire-phase attempts, a different missing package each, then a fail-closed exit),
  `45306374149` and `45310491984` measured the second failure mode (stalls of 16 m 44 s and
  35 m 52 s), and the four runs `45228382743`, `45302931629`, `45303576904`, `45309039777` plus the
  final run `45315493474` built the base on the first attempt in 25-37 s. The per-attempt bound now
  makes a stall cost one attempt instead of the job, but the three-attempt cap the contract fixes
  means a bad mirror window still fails the job before the case sets. The fixture's no-base fallback
  path (local runs) was exercised only by the pre-change rounds' local use, not in CI.
  F-ruling rounds 1-2 built the base on the first attempt in 38 s and 45 s; round 3
  (`45322501907`, head `d2375e8f`) then entered the stall mode again (base-build step still running
  37+ minutes after start, `03:56:00Z`), which the five-attempt cap and the 600 s per-attempt bound
  now terminate inside the step; round 3b (`45327162434`, head `dc805c89`) re-dispatched the
  identical tree bytes as the hedge the ruling allows ("if a dispatch still dies in the base build,
  record it as infrastructure"). A base-build-only failure leaves the case-set evidence to the
  other dispatch of the same bytes.
- Class D's fix is proven as far as provisioning and assignment: rounds 1-2 show every case set's
  `schedule` call creating a task and a Run and passing the fixture's runtime guard (round 2's 13
  `Prepare owner request` paths plus `shared`'s pre-smoke leg). What stays unverified is the
  `launch_one_shot_work_item!` effect on the *witness legs behind* the Go invocation, because no
  round has yet let a witness start (round 2 blocks at the guest Go build cache).
- Class C's mechanism is verified in both directions (the owner fragment cannot declare the edge;
  systemd v259 registers the inverse of `After=`), and the in-unit failure is observed in 13 rows.
  What is *not* explained by the collected evidence is why the fixture's direct preflight still
  passes in the same guest 47 ms earlier, although it queries the same manager and its daemon-policy
  check itself runs `systemctl show symmetry-daemon.service`: either the direct query does not load
  the unit, or the entry appears later than the load. The smallest fix and the `DECISION_NEEDED`
  do not depend on which it is — the gate is unpassable in the unit context either way — but a
  reviewer deciding the product-side fix should not treat the direct pass as evidence that the
  check is sound.
- The class-B mutant row cites baseline `45200662557` (per-case-set debootstrap, no retry) instead
  of re-running, as the contract allows; the positive rows are `45228382743` and `45302931629`
  (1 base build, 3 + 1 clones).
- The three round-6 changes (`fetch-depth: 0`, the pi release tree, the `safe.directory=*` guard)
  are **not verified in a hosted run**: the ruling's six-dispatch cap was reached on run 6, which
  broke before the case sets on the guest's ownership guard. The pi mechanism itself is verified
  locally against the pinned archive (lone binary prints `0.0.0`, `package.json` beside it restores
  `1.5.2`), the shallow-source refusal is verified by the daemon's own message in run 5, and the
  guard fix addresses the exact run-6 error text; the first dispatch on the current bytes should
  confirm all three together.
- `ack_loss_launcher_bound`'s stop is named but not isolated: its staged request fails the
  daemon-side barrier validation while the identically-generated stage-replay requests load. The
  next round should print the offending request field (or the loader's comparison) the same way
  round 5 printed the tests' own output.
- Round-7 update (the bullets above that predate it are superseded where they conflict; runs
  `45369766498`, §3.8): the three round-6 changes are verified in a hosted run, and the ACK-loss
  stop is isolated and fixed in the owned writer (§6.4). The remaining hosted stops after round 7
  are: (a) the witness command not reaching its requested durable stage in 11 rows, with the App's
  own refusal invisible by construction — the witness harness discards the App's stderr and the
  App writes its response only on structured outcomes (§3.8.1), so round 8 names the stop from
  fixture state (witness documents, StateDir and fixture inventories, and the App's own
  `-verify-linux-owner-install` run as A for every staged witness config); (b) the pi leg's staged
  executable not visible to A from inside the guest although the host-side fences verify it
  (§3.8.1), for which round 8 prints the guest's A view; (c) `synthetic_goal_cleanup`'s test-built
  `LinuxV2Invocation` has no `Callbacks.CheckNativeAdmission`, so the App refuses the test's own
  launch (`daemon/internal/execution/linux_owner_v2_linux.go:339`) — a `daemon/e2e/**`
  construction outside the owned files, raised in the stage report as `DECISION_NEEDED` with the
  run's evidence rather than patched here.
- Round-11 update (supersedes the earlier bullets where they conflict; runs `45378500332`,
  `45382412572`, `45384436642`): the two stops the round-7 evidence left unnamed are named.
  - The ten OpenCode-vehicle rows stop at the App's own witness config gate — observed 210 times
    from the App's binary, `owner witness config native harness version differs from the tested
    release`, because the fixture's `opencode` pin is `1.18.32` against the tested `2.3.0`
    (§3.10 item 1). This is the `F2-excluded` staleness the conductor's rulings decide separately;
    the fixture keeps the frozen pin and render tokens untouched.
  - The pi row stopped at the staged pi share's permissions: on this image's coreutils the
    recursive `chmod -R go-w` pass rewrote the tree (0755 directories → 0700, 0644 files → 0600),
    so A could not traverse it. Round 10's A-view fences made that fail closed and round 11
    normalizes the tree explicitly (§3.11). The later rounds' `pi` and `opencode` A-view fences
    stay as positive checks.
- Round-12 update (supersedes the earlier bullets where they conflict; runs `45384436642`,
  `45388258526`): the containment rows' invisible stop is a fixture provisioning gap, not a product
  defect. The App's preflight requires the fixed-B XDG cache (`$B_HOME/.cache`) — and, for Pi
  profiles, the fixed-B Pi session directory — between seeding its claim and releasing the witness
  barrier; the fixture never created either, so the App refused after the seed, the RunJournal
  stayed, and the witness command waited out its 30-second barrier release (§3.12.1, statically
  resolved and fixed in round 12). Two limits remain visible in the record: the witness harness
  still discards the App's stderr (`daemon/e2e/production_linux_v2_owner_cli_test.go:56-66` writes
  it to `/dev/null`), so an App refusal inside the witness is only ever named by fixture probes, not
  by the product's own output; and the `shared` set's fifth leg pins
  `TestInstalledLinuxOwnerRejectsUnexpectedCgroupKillFD`, which no Go file defines — the stale pin
  `docs/TODO.md:1025` records, left to its own maintenance task, so the set's `pi_provider_action`
  and `opencode_provider_action` production witnesses are not exercised by this goal's runs.
- Round-12 run update (2026-11-24, run `45388258526`, §3.13; supersedes the containment parts of the
  bullets above): classes A, B, C, D and E stay gone on the round-12 bytes — one base build (5m08s)
  plus 14 clones, every set's daemon smoke active with a real MainPID and `NRestarts=0`, 210
  native-gate observations, every A-view fence passing — and the ten OpenCode-vehicle rows keep
  their F2-excluded version-gate cause. What stays open: `codex_containment` and
  `claude_containment` stop at the witness barrier with their post-seed App refusal unnamed; the
  round-12 `$B_HOME/.cache` hypothesis is falsified (identical probe line, surviving journal and
  export failure versus round 11), and the early-phase probe cannot name the cause by construction
  (§3.13.1). The judging clause "containment rows reach the credential-dependent step or pass" is
  therefore unmet at stage close and is raised as `DECISION_NEEDED`; the next step needs one scratch
  run on redesigned probe bytes, so §1's hashes pin the last run's tree and any probe change
  invalidates them until the next dispatch.

- Round-13 update (2026-11-24, runs `45398550682` and `45402706492`, §3.14/§3.15; supersedes the
  earlier bullets where they conflict): the fifth leg's stale pin is closed — re-pointed to
  `TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD`, and the leg prints
  `run=1 pass=1 skip=0` in both runs — and the fixture's five-digit `z-later-` names (which failed
  the parent-death test's precondition on its first-ever execution) are six-digit like the test
  reads (`linux-v2-native-acceptance.sh:628`), so that test now fails inside its intended path
  (`clone_cleanup_linux_test.go:259`). Classes A, B, C, D and E stay gone on the final bytes
  (`8339f991`): one base build (29 s, first attempt) plus 14 clones, 14 `retired` lines
  (`0` ×1, `5` ×13), 210 App version-gate observations (the OpenCode pin gate, the expected stop
  for the ten OpenCode-vehicle rows), and zero Change-1 diagnostic firings. The remaining stops are
  all named and recorded in `docs/TODO.md`'s new follow-up section: the containment sets' post-seed
  App refusal (`:101`/`:109`) stays unobservable in the current harness (the App's stderr goes to
  `/dev/null`, `production_linux_v2_owner_cli_test.go:52-66`); `synthetic_goal_cleanup` needs the
  test-side admission callback (`production_linux_v2_cleanup_witness_test.go:499`); `shared`'s
  owner leg names three product-test stops (`clone_cleanup_linux_test.go:259`,
  `clone_integration_linux_test.go:240` with `:113`, `launcher_integration_linux_test.go:439`); and
  the twelve OpenCode-vehicle rows stay F2-excluded until `rf-acc-pi-durability-matrices`. The
  containment judging clause ("reach the credential-dependent step or pass") is therefore still
  unmet, but per the closing ruling its stop is recorded as a follow-up instead of blocking this
  goal. Every credential-dependent step remains unproven: the scratch doubles never reach a
  provider turn or a GitHub provider action.

- Fix-stage residual (STD-06, one-shot F2 sweep; read at the fix tree): the retirement runs once
  per case set inside `prepare` (`scripts/ci/provision-linux-v2-control-claims.exs:101` calling
  `:425-436`), while the workflow keeps running later case sets after a failed one
  (`.github/workflows/linux-native-v2-acceptance.yml:136-152`). If a set's teardown cannot prove
  its guest stopped (`linux-v2-native-acceptance.sh:238`/`:246` set `guest_stopped=0`), the
  surviving guest's daemon can heartbeat its own earlier runtime back to `online`
  (`control/lib/symmetry_control/orchestration/registry.ex:263`), and that row carries the
  previous set's `repository_resource_id`, so the class-E guard fires again. Not observed in any
  run; the workflow's `SYMMETRY_HEARTBEAT_INTERVAL_MS: "3600000"` (same file, `:44`) keeps the
  window near one hour. If it ever fires: re-sweep at `schedule` time.

## 9. Apply stage (2026-11-24, after the simplify review of `9d113eb6..4c305861`)

Applied from the accepted simplify set (no commit: the stage's rules forbid one; the deletions and
the record edit are working-tree changes):

- Both scratch-only files are removed from the tree:
  `.github/workflows/scratch-native-burndown.yml` and `scripts/ci/scratch-gh-shim`. They were
  tracked at the pushed PR head `4c305861` (`git cat-file -e` succeeds on both), so §1/§7's "never
  reach the PR branch" was false at that head; the worktree now holds their deletion (222 lines).
  The retained local scratch branch `8339f991` still carries both byte-identically
  (`git diff --stat 4c305861 8339f991 -- <paths>` empty), so a follow-up re-push is unaffected.
  §4's actionlint row for the scratch workflow stays as history.
- The moved CLOSED-TODO item's citation of the phantom pin's line is corrected from `:1804` — no
  revision holds the pin there: base `668e75de` and range start `9d113eb6` have `:1733`, the
  scratch heads `:1734`-`:1902`, the landed head zero occurrences — to `:1733`, the line the item
  was filed against. The `:1804` occurrences in this record's §7 and in the plan's §1 table are
  the implement stage's dispatch-time intent and stay as history.

Withheld, not applied: the two findings that edit `scripts/ci/linux-v2-native-acceptance.sh` (the
containment probe's falsified premise comment, and the `// null` jq selector). Any byte change
there breaks §1's `05d81032…` row and §3.15's binding of the final all-14 run `45402706492` to blob
`19ed563b…`, while the acceptance requires that run on the PR-head script bytes; re-proof needs one
fresh literal all-14 scratch run, which this stage cannot dispatch (no push; the scratch branch is
deleted), so both wait on a conductor amendment. Rejected: the writer-request test (finding 4)
needs a step line in `.github/workflows/ci.yml`, outside the contract's owned set.

Gates re-run on this tree: `bash -n` 0 on the acceptance script, `linux-v2-native-rootfs-base.sh`
and `linux-v2-native-tools.sh`; `shellcheck --severity=warning` exit 1 with 16 findings on the
acceptance script — the same code histogram as `668e75de`'s copy (SC2174, SC2154 ×8, SC2043,
SC2024, SC2276 ×2, SC2289, SC1078, SC2288), delta 0 — and 0 on both helpers; `actionlint
.github/workflows/linux-native-v2-acceptance.yml` 0; `check-citation-existence.py --base 9d113eb6
--head HEAD` 0 (the gate strips `file:line` suffixes by design; the edited citation's path resolves
at HEAD). One §1 row moved after the implement stage: `preflight_linux_test.go` hashes to
`465293fa…` at the merged head `4c305861` (`08b5367d…` at `dbdd807c`/`8339f991`, where the F1 live
proof binds it) because the merge unions main's six-line rename (`configuredHarnessKind` →
`harness.ParseKind`, lines 438-455) into the branch's file. At the merged bytes the F1 clauses stay
green: `go test ./internal/linuxpreflight/ -count=1` exit 0 (the accept block and the seven
`owner/<key>` refuse rows included), `go vet` 0, `gofmt -l` empty (Go 1.27.4, `GOOS=linux`). The
other six subject rows hash identically at the merged head; refreshing the Go row's value is left to
the conductor, outside this apply set.

## 10. Fix stage (2026-11-24, review findings STD-01…STD-06)

Supersedes §9's withholding note and its "left to the conductor" sentence, and the withheld-finding
parts of §3.13.1/§9 where they conflict; §8 keeps a new STD-06 bullet, and §1's script and Go rows
are refreshed above. The stage's rules forbid commit and push, so nothing below is committed; the
hashes are the fix-stage working tree's.

### 10.1 STD-01 — the `shared` set's unprovisioned-gate failure branch was dead code

`print_witness_output_lines` was defined at `:1600` (pre-fix numbering) but called at `:1499` (the
`shared` set's unprovisioned-gate failure branch), so bash reached the call before the definition:
under `set -Eeuo pipefail` the branch aborted with `command not found` (exit 127) before its
`exit 1`, and never printed the bounded witness-output diagnostic the branch exists for. The
function now sits beside `print_witness_document` (`:165-200`), before every caller; the call
sites move with the layout (`:1536`, `:1889`, `:1895` at the fix tree) and their content is
unchanged. Probes (all run at the fix tree):

- definition-order probe over the whole file (41 functions): the pre-fix bytes (`06fbdb06`) show
  exactly one reference-before-definition pair, `print_witness_output_lines` referenced at `:1499`,
  defined at `:1600`; the fixed tree shows zero pairs.
- runtime characterization of the pre-fix shape:
  `bash -c 'set -euo pipefail; …; print_witness_output_lines; print_witness_output_lines() { :; }'`
  printed `command not found` and exited 127.
- the fixed function body extracted from the file prints `witness output: hello from the witness`
  for a matching `output` event and prints nothing for a non-matching name, so the move did not
  change the body's behaviour.

Mutant row: move the definition back below the call → the branch reddens with `command not found`
(exit 127) instead of its diagnostic plus `exit 1`; observed in the isolated repro and in the
definition-order probe above. In CI the branch is reached only when the `shared` set's
unprovisioned gate fails inside the `linux-native-v2-acceptance` job (root on the runner), so the
row is an oracle gap: no passing run exercises it.

### 10.2 STD-02 — the probe comment's premise

The comment claimed the probe's one bounded run names the first refusal because the witness refuses
before it writes fixture state. Falsified by §3.13.1 and by run `45402706492`'s log (two
`existing owner witness clone intent differs from the exact Subject` lines, rows 1-2): the probe's
App run stops at the existing-journal check (`daemon/internal/app/linux_owner_witness_linux.go:2625`)
on the journal the witness's own run seeded, and the printed message is unconditional, so the first
post-seed refusal stays unnamed. The comment now states that premise; no code change.

### 10.3 STD-03 — the containment field's jq selector

`native_open_reached: (.containment.native_open_reached // null)` mapped a recorded `false` to
`null` (jq's `//` substitutes on `false`), erasing the "native open never reached" signal the
containment rows exist to report; the Go field is a non-pointer bool
(`daemon/internal/app/linux_owner_witness_linux.go:278`), so a written containment block always
carries `true`/`false`. The selector is now `.containment.native_open_reached`. Probes on
`{"containment":{"native_open_reached":false}}`: the old form printed `null`, the new form prints
`false`; with the block absent both forms print `null`, so absence stays distinguishable.

### 10.4 STD-04/05 — the plan and record citations

- Plan change 2 (`docs/plans/2026-11-24-rf-acc-native-burndown-plan.md:57-60`): the reject sites
  are now `daemon/internal/harness/codex/probe.go:246,256,266` and
  `daemon/internal/harness/claude/probe.go:144,147,170`, and the gate is `kind.UnavailableProbe()`
  at `daemon/internal/app/probe.go:49`; the stale `unavailableRuntimeKind` symbol is gone (main
  #273 renamed it).
- §1's `preflight_linux_test.go` row is refreshed to `465293fa…`, the merged head's value (§9).

### 10.5 STD-06 — the one-shot F2 sweep

Recorded in §8 as a residual, with its probes: one-shot call at
`scripts/ci/provision-linux-v2-control-claims.exs:101` → `:425-436`; the workflow continues to
later case sets (`.github/workflows/linux-native-v2-acceptance.yml:136-152`); teardown failure sets
`guest_stopped=0` (`scripts/ci/linux-v2-native-acceptance.sh:238`/`:246`); a surviving daemon
heartbeats its runtime back to `online`
(`control/lib/symmetry_control/orchestration/registry.ex:263`); the workflow's
`SYMMETRY_HEARTBEAT_INTERVAL_MS: "3600000"` (same file, `:44`) keeps the window near one hour. Not
observed in any run.

### 10.6 Gates at the fix tree, and the acceptance binding

Re-run on the fix-stage working tree (diff range `9d113eb6..06fbdb06` plus this stage's working
tree; commands as run):

- `bash -n scripts/ci/linux-v2-native-acceptance.sh scripts/ci/linux-v2-native-rootfs-base.sh
  scripts/ci/linux-v2-native-tools.sh` (WSL Ubuntu-24.04, bash 5.2.21): exit 0.
- `shellcheck --severity=warning scripts/ci/linux-v2-native-acceptance.sh` (0.11.0): exit 1 with 16
  findings (3 error, 13 warning); its output is byte-identical to the same command on the pre-fix
  copy (`06fbdb06`) apart from the reported line numbers, and the code histogram matches the base
  copies at `668e75de` and `9d113eb6` (16 findings each): delta 0.
- `shellcheck --severity=warning scripts/ci/linux-v2-native-rootfs-base.sh
  scripts/ci/linux-v2-native-tools.sh`: exit 0, 0 findings.
- `actionlint .github/workflows/linux-native-v2-acceptance.yml` (1.7.12): exit 0. No workflow
  change this stage.
- Record hygiene: a profile-name scan over `git diff origin/main...HEAD` and an added-line scan for
  Windows drive-letter and WSL mount-point host prefixes are both empty; quoted run-log lines keep
  their guest/runner-internal paths verbatim.

The acceptance clause "the final all-14 scratch run, on the PR-head script bytes" is **not re-bound**
at the fix tree: run `45402706492` ran the pre-fix script (`05d81032…`, blob `19ed563b…`, §3.15),
and §10.1-§10.3 are exactly the bytes that changed. The deltas cannot change any path that run
exercised — the hoisted function is the same code, the branch that now prints had zero firings, the
comment is documentation, and the jq selector changes only the printed witness document — but the
binding is byte-exact by contract, so closing it needs one fresh literal all-14 scratch run on the
fixed bytes, or an explicit conductor amendment. This stage cannot dispatch one (no commit, no
push; the scratch branch is deleted); it is recorded as the stage report's open item.
