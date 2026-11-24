# rf-acc-pi-durability-matrices — implement plan

Contract: `goal-rf-acc-pi-durability-matrices.md` (GDD state root, external to the repository).
Base revision: `477e4c62cbd14a671262da727954aa8e67fd0c40` (#277, the rf-acc-native-burndown merge).
Branch: `refactor/rf-acc-pi-durability-matrices`. Rulings: Q1(a) (keep the frozen OpenCode legs
after the pi legs, record the F2-excluded row), Q2(a) (provider-free pi, no daemon change), Q3(a)
(record the pi turn count), Q4 n/a.

The accepted plan is the contract plus the D3 amendment draft `refactor/d3-amendment-draft.md` P6;
this file resolves only the implementation choices those leave open, the per-clause baseline, and
what is out of scope.

## 1. Base census (before any edit)

The contract's literal acceptance command is the all-14 `linux-native-v2-acceptance` dispatch on a
qualifying host. That command's base-tree census already exists and binds these exact bytes: this
worktree's fixture files hash to the burndown's final-run table
(`docs/evidence/rf-acc-native-burndown-2026-11-24.md` §1) —
`scripts/ci/linux-v2-native-acceptance.sh` `f9399352…`,
`scripts/ci/provision-linux-v2-control-claims.exs` `c6fd8e60…`,
`scripts/ci/write-linux-v2-owner-request.py` `17c36b13…`,
`.github/workflows/linux-native-v2-acceptance.yml` `c0d18435…` — and run `45431551609` is the last
all-14 run on exactly those bytes (§3.16).

Red sources that census names, and this goal's disposition:

| # | Source | Disposition |
| --- | --- | --- |
| 1 | `shared`: `run_go_test` owner-leg scratch group (`TestInstalledLinuxOwnerBoundaryScratch`, `TestInstalledLinuxOwnerCloneCheckoutScratch`, `TestInstalledLinuxOwnerCloneCleanupScratch`, `TestInstalledLinuxOwnerCloneCleanupParentDeathScratch`) — three fail in `daemon/native/linux-gate/**` (`clone_cleanup_linux_test.go:259`, `clone_integration_linux_test.go:240`, `launcher_integration_linux_test.go:439`) | Out of scope: product tests under `daemon/native/**`, recorded in `docs/TODO.md` as a follow-up with "no fixture change … without a named root". Blocks `shared`'s later legs; §6. |
| 2 | `synthetic_goal_cleanup`: `production_linux_v2_cleanup_witness_test.go:499` `start "/bin/sh": Linux v2 native release requires an App admission check` | Out of scope: `daemon/e2e/production_linux_v2_cleanup_witness_test.go` is not owned; the stop is kind-independent (it precedes any harness turn). §6. |
| 3 | The ten witness case sets stop at the App's own gate `owner witness config native harness version differs from the tested release` (the F2-excluded OpenCode pin) | The retarget this goal implements removes it. |
| 4 | `codex_containment`/`claude_containment` stop before any stage (unnamed post-seed refusal) | Not retargeted by this goal. |

Local gate baselines on the base tree (WSL Ubuntu-24.04 / Windows, toolchain in §5): `bash -n` and
`shellcheck --severity=warning` on the touched scripts, `python3 scripts/ci/test-linux-v2-evidence.py`,
`gofmt -l`/`go vet ./...` in `daemon/`, `mix format --check-formatted` on the Elixir fixture.
`scripts/ci/test-linux-v2-evidence.py` is a POSIX-only suite (it asserts 0700/0600 fixture modes):
the Windows interpreter fails 68 assertions for that reason alone, so the gate runs in WSL.

## 2. Public/API shape

New fixture surface, all of it inside the owned files:

1. `scripts/ci/linux-v2-native-acceptance.sh`
   - `config()` gains one keyword argument: `provider_access` defaults to `kind == "pi"`, so every
     existing call stays byte-identical; the durability template passes `False`.
   - New template `pi-durability.json`: kind `pi`, profile `pi-durability`, model
     `SYMMETRY_PI_MODEL`, `harness_version` `1.5.2`, `pi_provider_extension_dir` set (the guest
     stages the share unconditionally), `provider_access: false`, bound to the new
     `pi_durability_repository_id`.
   - Generators pointed at the new template: stage-replay holds, ack-loss stages, negative-root
     bundles, `synthetic-goal-cleanup.json`, and the `daemon_service_template` default. The
     unprovisioned bundle and the `opencode-health.json`/`opencode-wsl2.json` writes stay unchanged.
   - `shared`: the pi legs move above the frozen `opencode_native_health` invocation; the frozen
     OpenCode-only invocations keep their bytes and relative order (final order in §3.6).
   - The staged pi executable is checked against the pinned `pi_executable_sha` instead of against
     the unpacked tree.
   - `export_native_evidence` passes the pi native arguments for the retargeted sets.
2. `scripts/ci/linux-v2-native-tools.sh`: only `pi_archive_sha`, `pi_executable_sha` and a new
   `pi_version_line` become shell-visible (the pins and fetch rules are unchanged).
3. `scripts/ci/provision-linux-v2-control-claims.exs`: a fifth tuple member names each case's
   repository role (`:pi_durability`, `:pi_provider`, `:opencode`); `prepare` creates the new
   non-provider-bound pi durability resource; `schedule` asserts
   `provider_access? == (role == :pi_provider)` and emits `pi_durability_repository_resource_id`.
4. `scripts/ci/register-linux-v2-claim-machine.py`: a sixth generic runtime
   `owner-witness-control-pi-durability-generic`, `capabilities` without `provider_access`, bound to
   the durability resource.
5. `scripts/ci/write-linux-v2-owner-request.py`: `CASES["stage_replay"] = ("pi", "pi-durability",
   None)`; provider-required cases keep the authentic-grant checks, durability cases require empty
   `work.required_capabilities` and no `claim.provider_access`.
6. `daemon/e2e/*`: the eight retargeted runners, the stage/ack/negative mappers, the stale-variant
   and unresolved-journal kind checks, the pi durability witness binding (§3.3), and the
   OpenCode-only health requirement scoped to the OpenCode harness.
7. `scripts/ci/export-linux-v2-redacted-evidence.py`: `--native-kind pi` accepted for the retargeted
   case sets, each set's pi witness files resolved from `MANUAL_CASE_SET_FILES`, the pi binding
   asserted against `--native-binary`/`--native-binary-sha256`/`--native-version-line`, and the same
   `manifest["native_binary"]` block as the containment kinds without `manifest["containment"]`.

## 3. Implementation choices the plan left open

### 3.1 The Control-side pi durability runtime is a distinct agent profile (`pi-durability`)

The contract fixes the daemon config's *profile shape* (kind pi, provider-free) and Q2(a) names a
"generic pi-durability runtime", but not the string Control matches on. With one profile string
(`pi-native`) for both pi runtimes, Control's legacy assignment (`assignment.ex:268-303`,
`runtime_matches_binding/0`: profile equality, workspace equality,
`runtime.capabilities @> task.required_capabilities`) matches a durability task
(`required_capabilities = %{}`) against *both* the provider-capable and the provider-free pi
runtime, and the winner is the smaller `runtime.inserted_at` — an implicit ordering dependency
(microsecond timestamps, no tie-break key) that would surface as a confusing fixture assertion.

Chosen: the durability role uses the profile value `pi-durability` in Control (work item, runtime,
session spec), in the daemon config (`agent_profiles` key and `runtime.agent_profile`) and in the
request writer. Each pi runtime then matches exactly one role, deterministically. Rejected
alternatives: (a) keep `pi-native` and register the durability runtime first (deterministic only
while `inserted_at` differs and the spec order never changes); (b) relax the fixture's
`provider_access?` assertion (weakens the check and leaves the provider-bound runtime serving
provider-free durability claims).

### 3.2 `daemon/e2e/production_linux_v2_helper_exit_witness_test.go` joins the edit set

The contract's owned list names the two `daemon/e2e` files that carry eight of the nine
`harness: "opencode"` literals. The ninth is the helper-exit witness wrapper
(`production_linux_v2_helper_exit_witness_test.go:110`); the case is a retargeted negative-root
durability leg of `shared`. Left at `"opencode"`, the wrapper asks for the OpenCode pinned version,
then `newLinuxV2NativeFixture` compares it against the pi config's `1.5.2`, so the leg fails its
config/harness binding before the witness runs. Same class of change, same e2e fixture set, no
`daemon/internal/**` file. Included, and named in the report as a scope addition. The contract did
not own this file when the increment was drafted; the conductor's DS-review ruling (2026-11-25,
F03) amends the owned list to include it for exactly this harness-literal retarget, so the
delivered change is inside the owned set.

### 3.3 Where the pi durability witnesses' `native_binary` binding comes from

The daemon records `native_binary`/`native_binary_sha256` in `runFresh` only
(`linux_owner_witness_linux.go:2906-2911, 2964-2971`); the same-RunKey recovery path that produces
every stage-replay/ack-loss/negative document never re-hashes the executable, so the response
carries no digest for exactly the retargeted documents. `daemon/internal/**` is not owned and needs
no change for the claim shape, so the *publisher* of the document binds the value: each retargeted
e2e runner hashes the staged executable (`harness.HashExecutableFile`, the same function the daemon
uses, `"sha256:<hex>"`) after its existing `linuxV2VerifyPinnedCLI` version check, and writes
`native_binary`, `native_binary_sha256` and `native_version_line` into the published witness. The
exporter then compares those three fields against the CI-side `--native-binary`,
`--native-binary-sha256` (`sha256:$pi_executable_sha`) and `--native-version-line` (`1.5.2`), so the
in-guest hash and the fetch-script pin must agree for publication. Mutant: drop the block or perturb
the sha → the exporter test reddens.

### 3.4 `synthetic_goal_cleanup` binds the vehicle, not a binary

`TestProductionLinuxV2CloneCleanupAuthenticatedScratch` starts a `/bin/sh` Git worker, never the
harness, so its published document records the vehicle as `fixture.runtime_kind` and has no
`harness`/`native_binary` fields (`production_linux_v2_cleanup_witness_test.go:778`; that file is
not owned). For that case set the exporter asserts `fixture.runtime_kind == "pi"` and still records
the pi `manifest["native_binary"]` block from the staged sha. Recorded as a residual in the evidence
record: the set's pi proof is its config's vehicle identity plus the manifest pin, not a
harness-executed binary.

### 3.5 pi staging keeps the whole release tree

`linux_v2_fetch_pi` unpacks a directory whose `package.json` is what `pi --version` reports from, so
the fixture copies the tree (`cp -a`) rather than `stage_guest_binary`'s single-file install. The
contract's intent — bind the staged bytes to the pinned fetch-script digest — is implemented as the
same check on the staged executable (`sha256sum … == pi_executable_sha`). The DS-review ruling
(2026-11-25, F10(ii)) additionally required the *pinned digest to cover the whole installed tree*:
`linux-v2-native-tools.sh` now pins `pi_tree_sha` (a content digest over the release tree's files,
checked right after extraction) and `linux-v2-native-acceptance.sh:459` verifies the staged tree
against it before first use, so a tampered file anywhere in the tree refuses.

### 3.6 `shared` final leg order

```
pi legacy refusal (pi-baseline)
opencode legacy refusal (opencode-baseline)      [OpenCode-only, unchanged bytes, runs]
cleanup-root, cleanup-mounts-root, cgroup-FD leg, owner-leg scratch group, marker-reader
alternate-mount refusal (pi durability; stays first among witnesses: a prior witness's Store row
  would mask the mount proof — the in-fixture comment at that line)
pi_provider_action
helper_sigkill_detached_child, daemon_sigkill_helper_survives_detached_child
stage-replay matrix (14 holds), ack-loss matrix (10 stages)
helper-exit, helper response-loss (3), receipt-persist, pid-reuse
legacy v1 replay (no harness)
opencode_native_health                            [frozen bytes; F2-excluded row, aborts the set]
opencode peer-negative                            [frozen bytes; not reached, F2-excluded row]
```

Only the invocation sequence moves: the frozen `opencode_native_health` invocation keeps its bytes,
the OpenCode peer-negative keeps its byte position immediately after it, and the OpenCode legacy
refusal stays where it runs today (before the pi legs, so it is not lost to the later abort). The
contract's "order them after all pi legs" is satisfied for the two OpenCode legs whose position
controls what runs; deviating for the legacy refusal would move a passing leg behind a failing one
and lose its row.

### 3.7 The fixed-B provisioned HOME moves inside the isolated owner witness root (fixture)

Round-0/2 scratch runs named the first real refusal of every owner-witness leg: the App refuses
with `fixed-B native HOME is outside the isolated owner witness root`
(`daemon/internal/app/linux_owner_witness_linux.go:2902-2905`, checking `linuxV2FixedNativeHome`'s
passwd HOME against `linuxV2NativeOwnerWitnessRoot = /opt/symmetry-owner-test`,
`:47`/`:1166-1175`). The fixture provisioned B with a `/home`-prefixed passwd home
(`linux-v2-native-acceptance.sh:605`), so **no** owner-witness leg of any kind had ever reached a
stage on this fixture. The refusal is the daemon's isolation contract and the fixture is the
non-conforming side, so the fix is fixture-side (owned): `B_HOME="$OWNER_TEST_ROOT/fixed-b-home"`,
with `machine_run_as` deriving `HOME` per account instead of the passwd home's `/home` prefix. Every other
B path (credentials, XDG cache, `.pi/agent`, Pi session dir, `A --x` ACLs, the e2e's
`linuxV2ValidateFixedBSecretFiles`, the daemon's `HOME`/`TMPDIR`/`PI_CODING_AGENT_DIR`) derives from
passwd or `$B_HOME`, so one assignment moves them together. `/opt/symmetry-owner-test` already
exists as root:root 0755 before `useradd` (`:557`), which is what B's traversal needs.

### 3.8 The witness command's stderr is captured instead of discarded (e2e instrument)

`startLinuxV2OwnerWitnessCommandWithRequestPath` sent the App's stderr to `/dev/null`
(`production_linux_v2_owner_cli_test.go:52-66` — the burndown recorded that gap as why its
containment refusal stayed unnamed). The runner now opens a private 0600 `owner-witness-stderr.log`
next to the witness document (`linuxV2OwnerWitnessDiagnosticPath`, `:277-282`) and the fixture
prints a bounded tail of it when a leg fails (`linux-v2-native-acceptance.sh:1705-1717`). It is what
converted round 2's 30-second barrier timeout into the named refusal above. The file stays inside
the A-owned witness output directory beside the witness documents, carries no secret, and is never
published: the exporter reads only its allowlisted sources (no directory enumeration) and refuses
any unexpected file in its own output directory (`export-linux-v2-redacted-evidence.py:1726-1728`).

### 3.9 The clone reserve needs a goal-admission work input the fixture cannot produce (DECISION_NEEDED)

Round 3 (`45460144698`) named the wall behind every owner-witness leg, and it is not fixture-fixable:

- The App's witness path seeds the durable journal from the request's authentic Control claim
  (`seedAuthenticClaim`, `daemon/internal/app/linux_owner_witness_linux.go:3857-3898`, `Work:
  witness.request.Claim.Work`) and then reserves the A/B clone (`prepareClone`, `:2974-2999`)
  before any stage runs.
- The reserve requires that work input to parse as a goal-admission document whose subject equals
  the clone intent (`daemon/internal/state/linux_clone_journal.go:864-877`: `ParseAdmissionInput`
  must report `present`; `daemon/internal/protocol/goal_admission.go:486`).
- The fixture's Control bootstrap launches goal-less one-shot work items
  (`provision-linux-v2-control-claims.exs:431-436`), whose task input carries project/repository
  metadata and no subject (`control/lib/symmetry_control/workspaces.ex:660-690`), and the request
  writer preserves that claim verbatim (`write-linux-v2-owner-request.py:235-250`).
- The e2e *requires* that legacy shape: `production_linux_v2_native_witness_test.go:1564` refuses a
  request whose `Claim.Work.Input` parses as an admission ("must be a real non-Goal Control task").
- The daemon's own witness tests require the opposite: `stageReplayWitnessRequest`
  (`linux_owner_witness_linux_test.go:713-726`) rewrites `Claim.Work.Input` to
  `validAdmissionInput()` with the request subject before it exercises the clone callbacks.

No fixture change satisfies both expectations. Every `run_native_owner_witness` leg therefore stops
at `reserve owner witness clone before source Git effects: Linux clone intent does not match the
current admitted Subject`, whatever the harness kind. Smallest proposed fixes: (a) the App
materializes the admission the clone authority needs from the request subject it already validates
(`seedAuthenticClaim`), and the e2e asserts that shape instead of the legacy one — the shape the
daemon's own tests use; or (b) the fixture synthesizes the admission input, mirroring
`linuxV2CleanupSyntheticAdmission` (`production_linux_v2_cleanup_witness_test.go:311-353,1351`),
which changes what the e2e's "real non-Goal Control task" assertion proves. Both need a ruling;
neither is implemented here.

### 3.10 Admission campaign (the conductor's F1 ruling, implemented; runs 7-11)

The ruling chose shape (c): the *fixture* provisions a real Control-admitted Goal task and neither the
App nor the fixture authors any admission field. Implemented as the evidence record's §2.12-2.16; the
local probe (record §8) verified the shape before any run, and the wall is gone from every leg (run 9:
zero `clone intent does not match` lines; the RunJournal shows `has_clone_intent`/`has_seed_receipt`).
Three further classes now bound the rows, in the order they were found:

1. **Recovery exit contract** (product): the App writes the expected
   `unresolved`/`containment_unproven` stage-replay response and exits 0
   (`linux_owner_witness_linux.go:501,:556,:596`), while `requireLinuxV2RecoveryExitStatus`
   (`production_linux_v2_owner_cli_test.go:177-193`) requires a deliberate non-zero exit for
   `unresolved`; the response-loss variant of the same outcome already joins
   `execution.ErrLinuxV2MutationAckLost` (`:627`), so the non-zero exit is the intended semantics.
   Blocks `stage_replay_create_started`/`leaf_bound`/`launcher_bound` and `ack_loss_launcher_bound`
   (all expected `unresolved`; run 9 + run 10). Smallest fix: return an error after writing the
   summary in those three branches.
2. **Release failure** (product, inner sub-step undetermined): the `native_release` hold is written
   only after a successful release (`linux_owner_witness_linux.go:3207-3211`), and the helper answers
   `release_outcome_unknown` for any release error (`helper_v2_server.go:1183-1189`) with the inner
   sub-step discarded; the same guest's product scratch legs fail the same class pre-existing
   (`launcher_integration_linux_test.go:439` `native exec failed after RELEASE: errno=1`, tracked in
   `docs/TODO.md`). Blocks `migration`, `stale_leaf`, and every `shared` hold strictly after
   `native_release`. Smallest fix: carry the failing sub-step + errno into the unresolved response.
3. **Parked-gate liveness expectation** (owned e2e defect, fixed here): the unresolved-journal guard
   required the parked gate to be live *after* the setup's owner SIGKILL, while the gate exits on
   owner loss by design (`gate.c:731-757`, `helper_v2_recovery.go:223-225`); the gate's identity
   binding stays by the marker/RunJournal comparison, and only anchor/helper liveness remains
   required. Run 10 measures the three variant sets on the pre-fix bytes (their setup does not pin
   the gate) and run 11 re-runs all four stale-journal sets with the fix.
4. **Owner-loss fail-safe versus the unresolved-journal premise** (product-versus-test, needs a
   ruling): run 11 showed the gate fix is necessary but not sufficient — the first failing checks
   moved to `:532` `stage owner does not have exact A credentials` (the anchor/helper was already
   gone milliseconds after the SIGKILL; the helper's `markOwnerLost` → `stopOwned` → exit path,
   `helper_v2_recovery.go:191-236`, resolved the containment) and to `:269` (the recovery leg refuses
   with `recover existing owner witness RunJournal without launch` and writes no response, because
   `linuxV2NativeWitnessJournalHasUnresolvedAuthority`, `linux_owner_witness_linux.go:2801-2811`, is
   false by then). Either the product must let a same-RunKey recovery consume the owner-loss receipt,
   or the test must assert the resolved-by-owner-loss state — both change what this pre-existing
   product test proves, so this goal leaves it to the ruling.

This stage therefore still closes `DECISION_NEEDED` on classes 1, 2 and 4 (all unowned product or
product-test semantics; class 3 was fixed inside the owned set), and the credentialed dispatch cannot
green the post-release rows before class 2 is named and fixed.

## 4. Evidence plan

- Scratch loop per the contract: branch `scratch/rf-acc-pi-durability-matrices`, throwaway workflow
  (real job's steps, no `environment`/`secrets`/`vars`, dummy credentials, the ratified `gh` shim),
  narrowed first runs over one set per class (`stage_replay_create_started`, `migration`,
  `stale_journal_run_key`, `stale_leaf`, `synthetic_goal_cleanup`, `shared`), then the literal
  all-12 run on the exact PR-head bytes. Cap 15 runs; foreground `gh run watch`.
- Mutant rows with run ids: revert one generator to `opencode-health.json`; revert one e2e
  `harness:` literal; revert `@cases`/`CASES` to `:opencode`; perturb the manifest's pi sha
  (the new exporter case reddens locally).
- Frozen-byte gate: `git diff <burndown base>..HEAD` over `daemon/internal/harness/opencode/**`,
  `testdata/opencode/**`, the six named CI assertions and their folded-gate wrappers,
  `docs/design/harness.md`, `docs/goals/README.md`, and the acceptance script's frozen lines.
- Per-set report rows: exact pi hold/boundary, pi turn count, exit code, first failing check.

## 5. Local gates

`bash -n`; `shellcheck --severity=warning` (delta 0); `python3 scripts/ci/test-linux-v2-evidence.py`
and the other `scripts/ci/test-linux-v2-*.py` suites (WSL); `ruff check`/`ruff format --check` on the
touched Python; `go vet ./...` and `gofmt -l` in `daemon/` (WSL, `GOOS=linux`); Elixir parse and
`mix format --check-formatted ../scripts/ci/provision-linux-v2-control-claims.exs`; `actionlint` on
the touched workflow. Never run a scratch/CI run concurrently with the local Go suite or a
measurement harness (host memory).

## 6. Out of scope (with reasons)

- `daemon/native/linux-gate/**` scratch stops in `shared` §1 row 1 and
  `production_linux_v2_cleanup_witness_test.go:499` §1 row 2: product defects in files this goal
  does not own, both already recorded in `docs/TODO.md` with follow-up owners. They keep `shared`
  and `synthetic_goal_cleanup` from reaching their pi legs on any run of this head; the report names
  them as the sets' stop reason and proposes the smallest amendment instead of editing them.
- `daemon/internal/**`, `control/**`, the D3 amendment, `docs/design/harness.md`,
  `docs/goals/README.md`, `.github/workflows/ci.yml`, and the OpenCode package/testdata: not owned;
  this goal needs none of them under Q2(a).
- A new turn-free pi health case (Q1(c)) and retiring the frozen health leg (Q1(b)): rejected by the
  ruling.
- The credentialed dispatch: conductor-dispatched after merge; this stage only records the scratch
  rows and the turn counts.

## 7. Finalization (2026-11-25)

The conductor's ruling on implement `DECISION_NEEDED` #2 closed this stage as: fixture/vehicle work
done; every retargeted set's row recorded as expected-stop-proven, blocked by class (F1-exit,
F2-release, F4-semantics) or out of scope (evidence record §4e); the unmet all-12 acceptance and the
credentialed pi turn counts carried into three named follow-up goals
(`rf-acc-witness-unresolved-exit`, `rf-acc-release-recovery-diagnostics`,
`rf-acc-owner-loss-semantics` in `docs/TODO.md`). §3.9's `DECISION_NEEDED` was ruled and implemented
as §3.10; §3.10's classes 1, 2 and 4 are those three follow-ups. Class 3 (the parked-gate liveness
expectation) stays as this goal's owned fix, kept under the ruling's condition: the design source
proves the old expectation unsatisfiable by construction (`gate.c:731-757`,
`helper_v2_recovery.go:223-225`), the gate identity stays bound by the marker/RunJournal comparison,
and the F3 mutant row (evidence §5 row 6, run `45488371274`) shows a wrong gate identity still fails
the guard. The last scratch run (`45489336388`, run 13) ran the literal all-12 case-set list on the
implement stage's final bytes; the apply stage (evidence §9) and the fix stage (§10) changed nine
of the ten subject files afterwards (only `register-linux-v2-claim-machine.py` still equals the
bytes run 13 executed), so that run does not bind the delivered bytes — the literal
all-12 re-run on them is carried to the conductor (evidence §10.11), and both stages forbid a push.
Runs 12–13 and the scratch-branch deletion are recorded in evidence §3.

### 7b. Fix stage (2026-11-25, DS review MUST_FIX F01–F10)

All ten findings are dispositioned inside the owned set plus the conductor's amendments (F03 owns
`production_linux_v2_helper_exit_witness_test.go` for the harness-literal retarget; F10(i) records
the formatter amendment with a `docs/TODO.md` entry; F10(ii) adds the whole-tree digest
requirement). F01 restored the configured witness output directory that the apply stage's F3
refactor had dropped — without it no set could publish a manifest; F02's false
behavior-neutrality sentences were removed and the pre-apply binding recorded (evidence §1, §3 row
13, §4e); F04 names the case's harness in the provider-access refusal; F05 derives the checked set
from the allowlist; F06 makes the acceptance script's export arm default to pi and pins the two
lists equal in the exporter suite; F07 names the mutant's CI job and stop; F08 derives the
provider-access cases from `CASES`; F09 stays the documented `synthetic_goal_cleanup` carve-out.
Evidence: the record's §10 with the mutant rows (§10.9) and the local probe transcripts (§10.10);
the all-12 run on the delivered bytes remains the conductor's dispatch.

### 7c. Round-1 review fixes (2026-11-25, PR #281 Sol + DS + Sec reconciled)

The conductor's binding ruling items 1-5, implemented and recorded in evidence §11: (1) the
rootless-Compose caller of `write-linux-v2-owner-request.py` migrates to the OpenCode-preserving
`stage_replay_unprovisioned_refusal` case (both Compose fixture files join the owned set for that
migration only; writer and request bytes unchanged, local positive/negative probe); (2) the
goal-create `execution_policy` literal is composed by `Map.merge` so the `contracts` clone gate
stops seeing the Control test's block (thresholds untouched, jscpd-changed exit 0 on the PR event
shapes); (3) `linux_v2_pi_tree_digest` binds entry type, mode, ownership, path, link target and
content, the pin is recomputed for the staged normalized tree (`f5fc36f7…`), and the check lives
at the staging site where that tree is canonicalized (fetch keeps archive/executable/version pins);
(4) the record's stop-line citations and the §4d mutant wording are corrected; (5) the plan file is
ratified as owned. Item 6 is the literal all-12 scratch run on the round-1 bytes (evidence §11.6),
the last code-affecting action; the delivered code bytes carry an all-equal sha256 table against
that run head.
