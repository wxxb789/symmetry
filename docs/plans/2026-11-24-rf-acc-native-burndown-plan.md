# rf-acc-native-burndown — plan

Contract: `goal-rf-acc-native-burndown.md` (GDD state root, external to the repository). Diagnosis:
`native-45200662557-diagnosis.md` (GDD findings root), run
`45200662557` / log `native/run-45200662557.log`. Base revision:
`668e75de6b376eb4205c11256228778003497d0a`. Branch: `refactor/rf-acc-native-burndown`.

Goal shape: burn down the host/fixture classes A (containment smoke config names the OpenCode kind
whose credential paths the containment fixture deliberately does not provision) and B (one
debootstrap per case set over a 14-set loop, ~8.5 min each) and make class C (installed
`symmetry-daemon.service` `ExecStart` failure) diagnosable from the journal; fix C at its root only
if that root is inside the owned files. The OpenCode pin stays frozen and untouched.

This plan records only the implementation choices the contract leaves unresolved.

## 1. Owned-file re-derivation at dispatch

| Contract entry | HEAD reality | Action |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh:1523-1544` daemon start branch | `if (( status != 0 )); then fail "installed daemon.service could not pass ExecStartPre/start"` with no unit or journal detail | add the bounded `systemctl show` + `journalctl` dump before `fail` (Change 1) |
| `:983-991` kind pins, `:1212-1229` smoke-config derivation | the smoke config is always derived from `opencode-health.json` while containment guests carry only their own kind's credential directory (`:534-540`, `:1254-1267`) | select the case set's own kind config as the derivation template (Change 2) |
| `:322-343` tool fetch + mirror rule + debootstrap | one rootfs per case set; mirror rule already fail-closed on the image's own apt configuration | move the mirror rule and the build into one shared base build with a bounded download-phase retry; clone per case set (Change 3) |
| `.github/workflows/linux-native-v2-acceptance.yml:126-131` case-set loop | the loop invokes the fixture once per case set | add one step before the loop that builds the shared base and exports its path through `$GITHUB_ENV`; no `environment`, `secrets`, `vars`, trigger or permission change |
| `docs/TODO.md:1010` citation `scripts/ci/linux-v2-native-acceptance.sh:1733` | a pre-existing stale pin (the Go test is gone) whose line number this change moves | re-point the line number only (now `:1804`); the staleness stays recorded |
| `docs/plans/**`, `docs/evidence/rf-acc-native-burndown-2026-11-24.md` | new | this plan and the goal's evidence record |
| one `docs/TODO.md` entry | new | what stays open after this increment |

Not owned and untouched: `daemon/**`, `control/**`, the frozen OpenCode pin and render tokens, the
real workflow's environment/secrets/vars/triggers/permissions.

## 2. Change 1 — failure-branch diagnostics

`machine_run /usr/bin/systemctl show --no-pager -p
Result,ExecMainStatus,ExecMainCode,ActiveState,SubState,ControlGroup,NRestarts,ExecStartPre,ExecStart
symmetry-daemon.service` then `machine_run /usr/bin/journalctl --no-pager --output=cat -n 80
--grep='<narrow alternation>' -u symmetry-daemon.service`, both `>&2 || true`, failure branch only.
Precedent: the in-fixture guest journal dump at `:1769-1799` (`--output=cat`, bounded `-n`, narrow
`--grep`, stderr, failure only). The grep alternation is the daemon's own stderr prefixes
(`-verify-linux-owner-install:`, `load config:`, `run daemon:`, `probe runtime harness:`,
`build runtime registration:`, `register daemon session:`, `registration did not return`,
`enroll machine:`, `load machine identity:`) plus systemd's unit lines (`Failed at step`,
`Main process exited`, `Failed to start`, `Start request repeated`). No environment block, request
body, or unit environment is printed; `ExecStartPre`/`ExecStart` are the unit's static command
lines, which carry no secret. `Result`/`ExecMainStatus`/`ExecMainCode` separate the pre-start
control process from the daemon's own exit, and the journal names the failing check.

## 3. Change 2 — per-kind smoke config

Template mapping: `codex_containment` → `codex-containment.json`, `claude_containment` →
`claude-containment.json`, every other case set → `opencode-health.json` (unchanged bytes). The
existing `state_dir` / `machine_name` / `runtime_key` / workspace (`daemon-service`) rewrites stay;
the `runtime.name` label is derived from the config's own `harness_kind` instead of the hardcoded
`OpenCode` prefix so no containment set advertises a foreign kind. No further rewrite is required:
the kind pin map `:984-991` already carries the guest's probed native version for codex (`0.168.0`)
and claude (`2.1.323`), and the blanket `adapter_version` is only compared for a *verified*
projection (`daemon/internal/app/probe.go:56-64`), which neither kind has on Linux
(`daemon/internal/harness/codex/probe.go:246,256,266` and
`daemon/internal/harness/claude/probe.go:144,147,170` both reject with `ErrNativeUnverified`, and
`kind.UnavailableProbe()` at `daemon/internal/app/probe.go:49` does not list codex). Containment
guests still provision exactly their own kind's credential directory; `:1254-1267` is untouched.

## 4. Change 3 — one base rootfs per job, cloned per case set

New helper `scripts/ci/linux-v2-native-rootfs-base.sh <dir>` owns the mirror rule (byte-identical
awk, fail-closed message) and the build:

- `sudo rm -rf` any partial base, then up to 3 debootstrap attempts; between retryable failures,
  `sleep 30` then `sleep 60`.
- A failure is retried only when its captured output names an acquire-phase
  network/mirror failure (`Couldn.t download package`, `Failed to fetch`,
  `Temporary failure resolving`, `Cannot initiate the connection`,
  `Connection (timed out|reset|refused)`, `Read error`, `Hash Sum mismatch`); anything else fails
  immediately after printing the attempt log's last 40 lines, so a deterministic error (wrong
  codename, unreachable mirror rule) is not hidden behind three slow attempts.
- On success the base is verified with `sudo chroot <base> /usr/bin/test -x /usr/bin/readelf`.

The fixture: when `SYMMETRY_LINUX_V2_NATIVE_ROOTFS_BASE` names an absolute directory, clone it
(`sudo cp -a -- "$base" "$rootfs"`); the path is a runner-image surface, so no `--reflink` (the
image's coreutils is the uutils Rust build). Otherwise it builds its own rootfs through the same
helper, so local runs keep working. The existing `sudo chroot "$rootfs" /usr/bin/test -x
/usr/bin/readelf` fence runs on both paths; every per-set provisioning step, fence and teardown
proof is unchanged, and the base is only read after the build.

The workflow gains one step after "Install disposable Linux fixture tools" and before the case-set
loop: build
`"$RUNNER_TEMP/linux-v2-native-rootfs-base-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"` with the helper,
then export `SYMMETRY_LINUX_V2_NATIVE_ROOTFS_BASE` through `$GITHUB_ENV`. A base-build failure fails
the job before the loop (three attempts bounded), instead of 14 independent exposures.

## 5. Evidence plan (scratch loop)

Branch `scratch/rf-acc-native-burndown`, throwaway workflow
`.github/workflows/scratch-native-burndown.yml` (copy of the real job; no `environment`, no
`secrets.*`/`vars.*`; dummy `GH_TOKEN`/`SYMMETRY_OPENAI_API_KEY`/models/base URL from the contract;
`GITHUB_EVENT_NAME=workflow_dispatch` in the loop env), plus the scratch-only `gh` ingestion shim
(ratified in `rf-acc-native-hosted-run`) because the fixture's `:292` `gh auth login` refuses the
dummy `gho_` token and the protected Environment never admits the scratch branch. Runs, ≤15:

1. Narrow triad `codex_containment migration stale_leaf` — Change 2 (A), Change 3 (B), and the
   Change 1 dump plus class C's real journal.
2. Post-fix narrow re-run at whatever checkpoint the journal names.
3. Change 2 mutant: revert the smoke template to `opencode-health.json` on the scratch branch only,
   re-run `codex_containment`; class A must reappear. Revert after.
4. Final literal all-14 run on the PR-head script bytes (scratch head = PR head + the throwaway
   workflow), with the shim and the contract's dummy values; every row must end past A/B and past a
   silent daemon start failure.
5. Change 3 mutant row cites run `45200662557` (per-case-set debootstrap, no retry) instead of
   re-running, as the contract allows.

Out of scope with reason: the OpenCode pin/staging (frozen D1 bytes; the conductor decides it
separately), any `daemon/**` or `control/**` root cause (class C then stops at `DECISION_NEEDED`
with the journal), and any host capability the runner lacks.

## 6. Addendum (2026-11-24, resumed implement stage)

Change 1 gains two bounded lines in the same failure branch: the daemon unit's `systemctl show`
also prints `After,BindsTo`, and the owner unit's effective edge list
(`Before,After,BindsTo,Requires,Requisite,Wants,PartOf,Conflicts`) is printed next to it. Reason:
the journal line names the failing check and the key, but not which unit's directive produced the
effective edge; those two properties make the source visible in the run log itself. Both stay
bounded, stderr-only, failure-branch-only, and carry no secret (unit names and static command
lines the same dump already prints for the daemon).

Class C's root cause is named from run `45228382743`: the unit's own `ExecStartPre` fails with
`effective symmetry-owner.service unit violates the owner service contract: effective owner unit
depends on or orders against symmetry-daemon.service through Before`, while the identical direct
preflight passed 47 ms earlier in the same guest. The owner's staged fragment cannot declare that
edge (`systemd_linux.go:496` rejects `Before=`/`BindsTo=`/... on the owner unit, and the direct run
validates it), and systemd registers the inverse of the daemon unit's own required
`After=symmetry-owner.service` on the owner unit (`src/core/unit.c:3066-3082` in `v259`
`inverse_table[UNIT_AFTER] = UNIT_BEFORE`; `Before` is serialized from that map). The same
preflight requires the daemon unit to keep that `After=`, so the check at
`daemon/internal/linuxpreflight/systemd_linux.go:233` can never pass inside its own
`ExecStartPre`. Root is in `daemon/**` ⇒ contract Change 4's `DECISION_NEEDED`, smallest proposed
fix: drop `Before` from that effective-key list (the staged fragment and drop-in validations
already forbid the owner declaring it).

Scratch runs of this stage: `45228382743` (round 1, carried over: narrow triad, first journal
evidence for class C), `45302931629` (round 2: Change 2 mutant, class A reappears), `45303576904`
(round 3: final literal all-14 on the PR-head bytes). Rows and exit codes:
`docs/evidence/rf-acc-native-burndown-2026-11-24.md`.

Class D (new, reached by round 3): `shared` died in Control claim provisioning with a
`tasks_goal_fields_all_or_none` violation from `scripts/ci/provision-linux-v2-control-claims.exs:417`
— a raw `max_run_attempts` pin on a legacy (goal-less) task, which Control's own constraint forbids
and its sibling fixtures never do. Fix in that owned `scripts/ci/*linux-v2*` helper: the pin is
removed (nothing reads the field for a legacy task). Proven by the narrow round-4 run and by the
final all-14 run; the witness legs behind it stay unverifiable while class C blocks the smoke.

Rounds 4-5 and the base build's second failure mode: run `45305439399` failed the *base build*
(three acquire-phase attempts, a different missing package each: `binutils`, `libsqlite3-0`,
`tzdata`), proving the retry path and its remaining single-point-of-failure; run `45306374149` on
the same bytes then stalled in the same step for 16 m 44 s against a healthy 25-37 s and was
cancelled. That stall is why round 4's helper adds one bound the accepted plan left open: each
debootstrap attempt runs under `timeout --signal=TERM --kill-after=30s 600` and a 124/137 status
counts as retryable. The contract's mirror rule, attempt cap (≤3), backoff, fences and per-set
clone path are unchanged. Round 5 (`45309039777`) is the class-D proof and the final all-14 run in
one dispatch on the final bytes.

Class E (new, reached by the final run once class D stopped failing): `shared`'s Control claim is
assigned to the *oldest* runtime matching profile/workspace/capabilities
(`control/lib/symmetry_control/orchestration/assignment.ex:260-304`), which in an all-14 run is a
previous case set's `opencode-native` runtime, and the fixture's guard
(`scripts/ci/provision-linux-v2-control-claims.exs:196-206`) then rejects it for carrying that
set's `repository_resource_id`. Independent of the class-D pin. No owned-file fix preserves the
assertion (Control has no runtime-deregistration path; the heartbeat interval that keeps the older
runtimes assignable lives in the workflow's job `env:`), so it is raised as a second decision with
its options in the evidence record §5.3. The narrow `shared`-only run (`45310491984`) is the
discriminating check; it never reached a case set — its base-build step hung 31 m 20 s past the
bound because `timeout` had wrapped `sudo` instead of owning the root `debootstrap`, and the run
was cancelled. The helper now runs `sudo timeout … debootstrap …` (timeout as root, owning the
download process directly), and the final all-14 run was re-dispatched on those bytes
(`45315493474`, head `8d84ecfc`).

## 8. Addendum (2026-11-24, implement stage resumed after the F-ruling)

The conductor's ruling replaces contract Change 4's `DECISION_NEEDED` with three in-scope fixes plus
one literal all-14 scratch run, waited for in the foreground.

- **F1 (owned files grow to `daemon/internal/linuxpreflight/systemd_linux.go` and its test).** Class
  C's root is the daemon check itself: systemd registers the inverse of the daemon's own required
  `After=symmetry-owner.service` on the owner as `Before`, and the owner policy refused every key
  listing the daemon. Fix as ruled: only `Before` leaves the effective-key list
  (`systemd_linux.go:235`); the staged-fragment and drop-in validations that forbid the owner
  declaring the edge stay (`systemd_linux.go:498`, `:773-784`). Clause tests added before the fix:
  accept `Before=symmetry-daemon.service` (`preflight_linux_test.go:870-876`), table-driven refuse
  for each remaining key (`:877-889`), declared `before` in the fragment (`:525-527`) and drop-in
  (`:539-541`) still refused. Observed failure before the fix and both mutant rows in the record §6.
  The test is pure property logic: Linux GOOS, no root.
- **F2 (fixture-side isolation, no Control product change, no relaxed guard).** The Control
  provisioning script now retires, at the start of each case set's `prepare` mode
  (`provision-linux-v2-control-claims.exs:98-101`), every runtime still online — by construction
  only earlier case sets', because registration of this set happens later in the fixture. The
  transition uses Control's own schema and changeset (`Runtime.changeset(%{status: "offline"})`,
  `:425-436`), the same one heartbeat expiry applies (`transitions.ex:511-544`); no new API,
  migration or guard change. The remaining product question (legacy assignment ignores the task's
  repository resource) moves to `docs/TODO.md` for Control review with runs `45309039777` and
  `45315493474` as evidence.
- **F4 (retry cap 3 → 5).** `linux-v2-native-rootfs-base.sh:59` retries up to 5 attempts, keeping the
  per-attempt 600 s bound, the 30 s × attempt backoff, the mirror rule and the fail-closed path.
- **Evidence plan.** One literal all-14 scratch run on the final bytes (scratch head = PR head +
  throwaway workflow + shim), waited for in the foreground with `gh run watch --interval 60`, then
  the per-set table from `gh run view <id> --log-failed`; the scratch branch is deleted and the
  deletion proven with `git ls-remote`. Expected rows: the containment sets and the pi legs reach
  the credential-dependent step; the OpenCode-vehicle sets stop at the version gate or
  `UnavailableAdapter.Start` as `F2-excluded`.

## 9. Addendum (2026-11-24, F-ruling round 1 findings)

Run 1 of the F-ruling round: `45318768814`, head `c7053e2a`, all 14 sets, job 10 m 39 s, base build
on the first attempt (38 s), 14 clones. F1's and F2's live proofs hold (record §3.7): zero class-C
lines, 13 smokes that start, stay active and stop cleanly, retirement counts `0`, `4`, then `5`
twelve times. No row ends at classes A, B, C or E. Two further stops, both rooted in the owned
script:

1. **Witness-invocation parse (13 rows).** `run_go_test` collected its expected test names from the
   arguments *before* a `--` separator; all 26 call sites pass them *after* it, so the list was
   always empty and `linux-v2-native-acceptance.sh:1563` (pre-fix; now `:1625`) fired before any Go
   test ran (pre-existing
   since the function's introduction, unmasked until classes A/B/C were fixed). Fix: require the
   `--` delimiter, then `local -a expected_tests=("$@")`; the non-empty assertion stays. Mechanical
   before/after probe in record §3.7.1; no call-site change, no assertion weakened.
2. **`shared` unprovisioned event gate (1 row).** The pre-smoke `systemd-run … go test -json` block
   produced no JSON events and exactly one non-JSON line, and a Go build failure would have emitted
   JSON (`build-output`/`build-fail`), so the line came from before/around the Go invocation; the
   run kept no copy of it. The failure branch now prints the event file's first ≤5 non-JSON lines
   (Change-1 pattern), and the next round's log names the root.

Round 2 (`45320631643`, head `926e26e7`) is the literal all-14 run on those bytes, waited for in the
foreground; its rows go in record §3.7.3.

## 10. Addendum (2026-11-24, F-ruling rounds 2-3)

Round 2 (`45320631643`, head `926e26e7`) proved the parse fix (zero invocation-guard lines; every set
executes its named `go test`) and named the next root with the non-JSON diagnostic: the A user's
`<A_HOME>/.cache` was not usable, so the guest `go` refused its build cache in all 14 sets.
Fix in the owned script: name `.cache` itself in the A-owned `install -d` list and fence the three A
XDG cache directories; the same scan over every `install -d` path named
`$OWNER_BARRIER_ROOT/unprovisioned` explicitly (the one other implicit parent on an A-owned barrier
root; `negative-root/stale_journal_run_key` is re-owned by its per-case loop before the guest boots).

Round 3 (`45322501907`, head `d2375e8f`) died in the base build after four retryable acquire
failures and one 600 s-bounded attempt — F4's cap, backoff and bound all fired and failed closed
against the image mirror; infrastructure, not a defect. Round 3b (`45327162434`, head `dc805c89`,
tree byte-identical to `d2375e8f`) ran the case sets: the staging fix works (`run:1`,
`invalid_json_lines=0` in all 14 rows), and every row now fails inside its named production witness
(`pass:0`, `fail_events=2`/`3`), with the containment rows' exporter refusing on the witness
document's `native_binary`.

Because the fixture prints no test output, round 5 (`45330552341`, head `f5bca7ce`) adds two
bounded diagnostics rather than a guess: `print_witness_output_lines` (last ≤10 output lines of the
expected tests, dropped if they contain a value from the fixture's secret-values file) and, on a
containment export failure, a `jq` projection of the witness document's
`{case, harness, outcome, failure_category, installed_owner_verified, native_binary,
native_open_reached}`. Its rows go in record §3.7.5. Dispatches used: 5 of the ruling's ≤6.

## 11. Addendum (2026-11-24, F-ruling rounds 4-6)

Round 4 (`45327162434`) showed the staging fix working (`run:1`, `invalid_json_lines=0`) and every
row failing inside its named production witness; round 5 (`45330552341`) added the bounded
witness-output and witness-document diagnostics and named each row's first failing line (record
§3.7.5). Three fixture-rooted causes came out:

1. the workflow's `actions/checkout@v7` has no `fetch-depth`, so the A-owned Git source is shallow
   and the installed owner's complete-history validator refuses it — this aborts every barrier
   witness in preparation (11 rows) and is what `synthetic_goal_cleanup` names verbatim
   (`unsupported B Git metadata entry "shallow" is present`);
2. the fixture staged only the pi *binary*, and pi reports `0.0.0` without the `package.json` in
   its release tree (reproduced locally against the pinned archive; the sibling
   `rootless-compose-native-acceptance.sh` stages the whole directory);
3. `ack_loss_launcher_bound`'s staged request fails the daemon-side barrier validation
   (directory/timeout/`hold_after`), named by the row and not yet isolated.

Fixes for 1 and 2 ship in round 6 (`45334300138`, head `d22a6b31`): `fetch-depth: 0` on the
workflow's checkout step (mirrored in the throwaway workflow), and the pi release tree staged to
`/usr/local/lib/symmetry-pi` with the profile command and baseline smoke repointed and digest-fenced
against the verified archive. Cause 3 stays named in the row; if it persists in round 6 it is
reported as the remaining fixture-side unknown. Dispatches used: 6 of the ruling's ≤6.

Round 6 failed in setup, not in a witness: with the full-history checkout, the guest's A-owned clone
of the read-only runner-owned mount hit git's ownership guard
(`fatal: detected dubious ownership in repository at '<A_HOME>/ci-source/.git'`) and every
case set exited 128. The shipping tree adds the minimal fix for that one command
(`-c 'safe.directory=*'`, read-only mount, commented). That was the sixth dispatch, so the tree
carries three changes — `fetch-depth: 0`, the pi release tree, and this guard — that no run has
executed; the stage's final all-14 evidence table is round 5's. The first dispatch on the current
bytes should confirm all three and then continue to the witness rows.

## 12. Addendum (2026-11-24, implement stage resumed after the cap ruling)

Run 7 (`45369766498`, head `9fadfda2`) is the ordered first dispatch on the checked-in round-6
bytes plus the scoped `safe.directory` guard: all three round-6 changes are proven in a hosted run,
classes A–E stay gone, and the per-case-set table is in the record §3.8. It leaves two
witness-internal stops and one test-side stop, and the ack-loss row confirms round 5's defect
byte-for-byte:

- ack-loss (row 8): the fixture's request writer treated the `drop_ack_after=<stage>` selector as
  the barrier hold and never wrote the top-level field, so the daemon's request load refused the
  fixture's own request. Fixed in the owned writer and its caller (record §6.4), with the pre/post
  probe against the real writer and a stubbed claim endpoint.
- the witness command missing its requested durable stage (rows 1-2, 5-7, 9-14) and the pi leg's
  staged executable invisible to A inside the guest (row 3): both are named by the test but not
  explained by the fixture's own diagnostics. Round 8 adds a bounded failure context — witness
  documents, fixture and StateDir inventories, the guest's A view of the staged tools, and the App's
  own owner-install preflight run as A for every staged witness config — and keeps every fence
  unchanged.
- `synthetic_goal_cleanup` (row 4): the test builds a real `execution.LinuxV2Invocation` without
  `Callbacks.CheckNativeAdmission`, so the App refuses it
  (`daemon/internal/execution/linux_owner_v2_linux.go:339`); a `daemon/e2e/**` construction outside
  the owned files, raised as `DECISION_NEEDED` with the run's evidence.

Round 8 (`45374416995`, head `26ba0065`) carries the ack-loss fix plus that diagnostic set; its rows
go in the record §3.9. Dispatches used: 2 of the ruling's further ≤6.

## 13. Addendum (2026-11-24, rounds 9-10)

Round 9 (`45378500332`, head `a51d26cb`) ran the full context and resolved both unnamed causes: the
OpenCode-family witness configs are refused by the App's own witness gate
(`owner witness config native harness version differs from the tested release`, observed 210 times
from the App's binary), and the pi share directory `/usr/local/lib/symmetry-pi` reaches the guest as
mode 0700, so A cannot traverse it and the `pi-baseline` witness refuses its staged executable. Both
are the same class of image-coreutils attribute behaviour round 3 found for the A XDG cache.

Round 10 (`45382412572`, head `196bfb27`) fixes the fixture side of the pi stop and proves it from
A's side in the guest: the three native-tool directories are set to 0755 explicitly after the
recursive pass, the share directory's mode is fenced, and `test -x`/`test -r` as A cover
`/usr/local/lib/symmetry-pi/pi`, its `package.json` and `/usr/local/bin/opencode`. It also adds the
bounded early-phase probe for the two containment rows: one App run with the exact staged config and
request, under `timeout 90`, which either names the refusal verbatim or proves the App passes its
early phase and stops at the barrier's own 30-second release timeout. The OpenCode-family rows stay
`F2-excluded` by decision; their tier's remaining stop is the version gate named in round 9.

## 14. Addendum (2026-11-24, rounds 11-12: the containment root cause)

Round 11 (`45384436642`, head `daeaedda`) keeps the round-10/11 tree-mode fix (every set's A-view
fences pass, no `:35` executable refusal anywhere) and moves the `shared` set four witnesses deep
(`pi-baseline`, `opencode-baseline`, `cleanup-root`, `cleanup-mounts-root` pass; the stop is the
fifth leg's pin to `TestInstalledLinuxOwnerRejectsUnexpectedCgroupKillFD`, which no Go file defines —
the stale pin `docs/TODO.md:1025` already records, so nothing is changed for it here). The other 13
rows keep their round-9/10 names.

The containment rows' unnamed stop is now resolved statically, because round 11's early-phase probe
named a consequence rather than a cause: the probe reports
`existing owner witness clone intent differs from the exact Subject`, which is only possible when the
*witness's own* App run seeded its claim and its RunJournal survived. The App seeds first and only
then runs `linuxV2OwnerResources.Preflight`, which requires the fixed-B XDG cache
(`$B_HOME/.cache`, `linux_owner_v2_linux.go:80-150`) — a directory the fixture never provisioned
(the A-side cache was the round-3 fix). Round 12 therefore provisions `$B_HOME/.cache` B-owned mode
0700 with the same fence as the A-side cache, and, for the non-containment guests, the fixed-B Pi
session directory (`$B_HOME/.pi/agent/sessions`) that the same preflight requires for Pi profiles
(`linux_owner_v2_linux.go:159-170`) and that the fixture also never created. Both are fixture-side
prerequisites of the installed App's own preflight, not contract changes.

## 15. Addendum (2026-11-24, round 12's run: the containment stop survives the round-12 fix)

Round 12's all-14 run (`45388258526`, head `99277aed`, the current tree's bytes) clears and keeps
the ruled classes: one base build (5m08s) plus 14 clones (class B gone), every set's installed
daemon smoke active with `NRestarts=0` (class C gone, F1 live-proven), no class A failure with the
per-kind smoke config, no class D/E, and 210 observations of the OpenCode version gate that keeps
the ten OpenCode-vehicle rows `F2-excluded`. The `shared` set keeps its stale fifth-leg pin
(`docs/TODO.md:1025`).

The round-12 prediction does not hold: `$B_HOME/.cache` is provisioned and fenced, yet the two
containment rows are identical in shape to round 11 — same probe line, same surviving RunJournal
(1268 B / 1284 B), same 30.7 s barrier wait, same fail-closed export. The early-phase probe cannot
name the refusal by construction: the witness's own run seeds the journal before refusing, so the
probe's second App run returns at `validateLinuxV2NativeWitnessExistingJournal`
(`linux_owner_witness_linux.go:2625-2633`) with a nil `LinuxCloneIntent` regardless of the real
cause (record §3.13.1). Consequence: the judging clause "codex_containment and claude_containment …
must reach the credential-dependent step or pass" stays unmet, and the stage raises
`DECISION_NEEDED` with a fixture-side probe redesign (fresh StateDir or the seeded journal moved
aside; or capture the witness harness's discarded stderr,
`daemon/e2e/production_linux_v2_owner_cli_test.go:52-56`) plus one scratch run — 5 of the ruling's 6
used. The tree intentionally stays at the run's bytes, and the remote scratch branch is deleted at
stage close (record §7), so the redesign lands in a follow-up stage.

## 16. Addendum (2026-11-24, round 13: the fifth leg's pin and the parent-death fixture name)

The closing ruling left one in-scope fix and a ≤2-run budget: `shared`'s fifth leg pinned
`TestInstalledLinuxOwnerRejectsUnexpectedCgroupKillFD`, a name no Go file defines. The successor by
intent is `TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD`
(`daemon/native/linux-gate/protocol/launcher_integration_linux_test.go:122`): the leg already runs
that package, the `owner-fd-scratch` profile already sets the test's opt-in environment
(`SYMMETRY_LINUX_OWNER_TEST_SCRATCH`, `SYMMETRY_LINUX_OWNER_TEST_LEAF`), and the rule is the same
(an unexpected fifth cgroup-control FD in a guarded RELEASE is rejected and the exact scratch leaf
stays unpopulated). The pin is re-pointed and the leg renamed accordingly; the ruling's pointer to
#272's `linuxpreflight` tests names a different package than the leg's own `run_go_test`
invocation, so it cannot be the pin's successor.

Run 13 (`45398550682`, head `d4fdb149`) proves the re-point — `verified
TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD: run=1 pass=1 skip=0` — and moves the set
into the owner leg, which stops on three tests. The parent-death test's stop is a fixture defect
proven mechanically: the fixture wrote `z-later-00000` (five digits) while the test reads
`z-later-000000` (record §3.14). Run 13b (`45402706492`, head `8339f991`, the final all-14 run)
carries the six-digit fix and shows that test failing inside its intended path instead of at the
precondition; the other two stops (`clone_integration_linux_test.go:240` clone-status frame with
its `:113` cleanup line, and `launcher_integration_linux_test.go:439` native-exec errno) sit in
product tests and are recorded as a follow-up in `docs/TODO.md` with a guest-side debugging owner.
The same run serves as the contract's final all-14 run on the final script bytes: no class A-E
line, one base build plus 14 clones, 14 `retired` lines, 210 OpenCode version-gate observations,
and zero Change-1 diagnostic firings; the containment witness stderr capture,
`synthetic_goal_cleanup`'s admission callback and the OpenCode F2-excluded rows are recorded as
follow-ups in `docs/TODO.md` (record §8, round-13 update).
