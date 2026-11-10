# Goal 0006b acceptance CI audit

Date: 2026-09-29
Mode: read-only CI investigation; this document is the only intended output
Goal status: **ACTIVE / acceptance incomplete**

## Subject and evidence boundary

Run `36332839164` binds all three jobs below to PR head
`85a7968511f0b499ca205b361d11b5d29e380e62`. The checkout log records the actual
pull-request checkout as synthetic merge
`a5e45b6fc85035dfa55bc10c7c9152ac4d191e7e` (PR head merged into base
`491b2222a41a25699bd8ccf7b7a678d46c2af583`). These failures describe that
historical merge checkout, not the current tree or a future revision.

Local committed HEAD is `d14debbf6ae3ded09e5013ef90952d347c01474d` with extensive
pending changes. `.github/workflows/ci.yml` is modified; all of `scripts/ci/` and
`scripts/cleanup-linux-pi-processes.sh` are untracked. Thus untracked helpers are
pending working-tree candidates, not fixes in `d14debbf` or `85a7968`, and no
historical result or source comparison proves a new exact tree green.

The governing recovery receipt correctly keeps Goal 0006b incomplete. The goal
still requires a clean exact subject, a qualified amd64 systemd host, exclusive
cgroup-v2 delegation, distinct A/B identities, exact cross-UID peer checks,
App-owned RunJournal authority, real Pi/OpenCode positives, complete crash/replay
evidence, protected raw recovery storage, and exact-subject review. Generic
hosted checks and synthetic loopback smokes cannot replace those results.

## Executive disposition

| Failed check | Source-backed disposition | Smallest next proof |
| --- | --- | --- |
| `cgroup-v2-delegation-probe` (`108657962961`) | **Qualified-host failure; not a demonstrated product defect and not fixed.** Delegation, fixed-B maps and userns setup passed, but mapped B could not `fork/exec /proc/self/exe`: `operation not permitted`. Commit `c4afe7d` adds diagnostics only. | Run the pending prerequisite probe on a clean exact commit. Only a passing qualified-host result can precede protected native acceptance. |
| `pi-control-e2e-linux` (`108657963001`) | **Unrelated stale-CI/source-packaging defect; pending working-tree fix only.** Both callers referenced a script absent from the historical tree; the fixture exited 127 and actual Pi Control E2E was skipped. The local script is untracked. | Put the script in the exact commit, prove it is tracked, and let the same fixture and Pi job run once. Do not rerun the old SHA. |
| `opencode-native-synthetic` (`108657962941`) | **Still source-exposed gate mismatch / stale legacy containment wiring.** Tool work reached the synthetic catalog, but close refused to invent proof after legacy process-group observation saw an escaped/vanished descendant. The adapter can forward v2 grants, but this unchanged test supplies neither v2 field. | Run pinned 1.18.32 once on the exact commit. If it repeats, use the qualified v2 owner path; do not weaken descendant-stop checks. |

## 1. Cgroup-v2 and cross-UID peer probe

### Decisive failure

Setup, compilation and cleanup passed. Step 5 failed at this boundary (sanitized):

```text
group-preparation=passed initial_supplementary_group_count=5
namespace_gate_groups=empty
mapping=uid-passed groups=empty
mapping=gid-passed groups=empty
capability=clone3-cgroupfd-pidfd
cross_uid_capability=not-proven
host_cap_eff=0000000000000000
status=failed stage=cross-uid-b-launch detail="... fork/exec /proc/self/exe: operation not permitted"
status=failed stage=systemd-run-or-timeout
```

This was not generic delegation failure. The transient service, group
sanitization, fixed-B maps, child user namespace, leaf launch and pidfd capability
had passed. Host policy denied mapped-B execution. Goal 0006b requires that exact
cross-UID boundary to fail closed, without same-UID or process-group fallback.

The old probe is embedded in `.github/workflows/ci.yml@85a7968:11-1622`.
Post-run commit `c4afe7d45ef274e67ac27ef09d4d7f5267194747` only adds
mapped-worker privilege diagnostics. This audit found no later remote green.

### Current comparison and next proof

The pending workflow separates ordinary `linux-v2-host-prerequisite-probe`
(`.github/workflows/ci.yml:12-33`) from credentialed acceptance behind explicit
`workflow_dispatch` and the protected `linux-v2-native-acceptance` Environment
(`:35-133`). Ordinary events get no provider or Git credentials.

The untracked wrapper emits narrow sanitized evidence and names what it cannot
prove (`scripts/ci/linux-v2-host-prerequisite-probe.sh:45-89`). It checks an
unprivileged delegated leaf plus `cgroup.kill` (`:144-224`), then exact cross-UID
userns/fdinfo/`pidfd_getfd` (`:226-280`). Required output includes namespace
proof, `tcp_state=ESTABLISHED`, `pidfd_getfd=passed`, zero effective capabilities
and `status=passed` (`:255-279`). Better separation does not remove the denied
prerequisite. The disposition remains a qualified-host failure, not a fix.

After committing the workflow and scripts as one exact subject, Actions must run:

```bash
bash scripts/ci/linux-v2-host-prerequisite-probe.sh
```

The evidence artifact is
`$RUNNER_TEMP/linux-v2-host-prerequisite-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT.json`,
bound to `GITHUB_SHA`. A denial is fail-closed evidence, not positive capability.
Only a pass can precede protected dispatch on that commit. Preserve its
180-minute budget, Environment approval, read-only repository permission,
isolated token, Go 1.27.0, Elixir 1.20.4, OTP 29.0.6, provider opt-ins, and all
12 case sets.

## 2. Linux Pi Control E2E

### Decisive failure

Pinned Pi 0.87.1 acquisition/checksum/version, provider-bridge smoke, and Control
format/compile passed. The first failure was:

```text
bash: ../scripts/cleanup-linux-pi-processes.sh: No such file or directory
RuntimeError: matching identity cleanup failed with status 127
```

The `if: always()` cleanup independently exited 127 for the same missing file.
`Run Linux Pi Control E2E` was skipped, so no Pi/Control result exists.

The old workflow references the script at
`.github/workflows/ci.yml@85a7968:1916-2119` and `:2181-2184`, while
`git ls-tree -r 85a7968 -- scripts/cleanup-linux-pi-processes.sh` is empty.

### Current comparison and next proof

The current workflow retains both calls (`.github/workflows/ci.yml:432-635` and
`:697-700`). The local script implements exact marker parsing,
boot/PID-namespace/start-time checks, pidfd-bound group signaling, bounded
TERM/KILL proof, and fail-closed refusal
(`scripts/cleanup-linux-pi-processes.sh:1-275`). The fixture requires a matching
group to stop and a wrong-start identity to remain unsignalled. Do not remove
those checks.

The script is untracked; neither `git ls-tree HEAD` nor the PR tree contains it.
This is the smallest apparent fix but has no commit-bound runtime proof. Before
CI on the new subject:

```bash
git ls-tree -r --name-only "$EXACT_COMMIT" -- scripts/cleanup-linux-pi-processes.sh
git show "$EXACT_COMMIT":scripts/cleanup-linux-pi-processes.sh >/dev/null
```

Then let the unchanged fixture run, followed from `control/` by:

```bash
SYMMETRY_PI_CONTROL_E2E=1 \
SYMMETRY_PI_CONTROL_E2E_EXECUTABLE="$RUNNER_TEMP/pi-0.87.1/pi/pi" \
  mix test test/symmetry_control/pi_control_e2e_test.exs \
    --include skip:true --only pi_control_e2e
```

The final `always()` cleanup and log/provenance upload must also pass. Fixture
success alone is not a Pi Control pass, and this synthetic bridge is not the
Goal's authentic claim-scoped A/B Pi positive.

## 3. OpenCode native synthetic integration

### Decisive failure

Pinned OpenCode 1.18.32 checksum and version validation passed. The test reached:

```text
native OpenCode V2 catalog exposes provider="symmetry-native-loopback" model="symmetry-opencode-test"
```

Close then failed:

```text
Linux descendant containment is unproven: descendant observation was uncertain
leader disappeared without a clean descendant terminal proof
(terminal_reason=observed_escape scan_completed=true scan_lost=true observed_escape=true)
```

Cleanup repeated the refusal. The event gate correctly rejected explicit
test/package fail events and a missing test pass; this was not parser noise. The
old test requires clean bounded stop before pass
(`daemon/internal/harness/opencode/adapter_native_work_test.go@85a7968:213-228`).
Weakening that assertion would violate Goal 0006b.

### Current comparison and next proof

The test body is unchanged. Its `StartRequest` supplies environment and
`PersistProcess`, but no Linux v2 grant
(`daemon/internal/harness/opencode/adapter_native_work_test.go:65-228`). The
workflow still downloads 1.18.32 with the same checksum and runs that opt-in test
(`.github/workflows/ci.yml:1353-1432`).

Production plumbing has advanced: the adapter forwards `LinuxV2Required` and
`LinuxV2` (`daemon/internal/harness/opencode/adapter.go:365-385`), and Runner
forbids direct fallback when v2 is requested
(`daemon/internal/execution/runner.go:544-642`). This test requests neither, so
Runner still takes `ConfigureProcess`, direct `command.Start`, and
`AttachProcess`, the legacy process-group path (`runner.go:598-648`). Current
scan/seal changes are not a pass of this native gate.

The production seam is present, but this generic Linux synthetic check remains
source-exposed to the detached-descendant failure. Treat it as unresolved until
one exact command passes. A synthetic pass still cannot replace qualified
installed OpenCode evidence.

On a clean commit, after checking the pinned archive and version, run from
`daemon/`:

```bash
SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL=1 \
SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL_EXECUTABLE="$RUNNER_TEMP/opencode-1.18.32/opencode" \
  go test -count=1 -timeout 180s -json ./internal/harness/opencode \
    -run '^TestNativeSyntheticGatewayToolIntegration$' \
    >"$RUNNER_TEMP/opencode-native-synthetic-test.json" 2>&1
python3 ../scripts/ci/assert-go-test-json.py \
  "$RUNNER_TEMP/opencode-native-synthetic-test.json" \
  TestNativeSyntheticGatewayToolIntegration
```

If containment fails again, move Linux positive proof to the protected v2 owner
witness with its App-owned journal, delegated leaf, exact peer capability and
distinct A/B identities. Preserve the synthetic opt-in and Windows counterpart
for their narrower tool-integration scope.

## Acceptance ordering

1. Commit one clean exact subject containing every referenced helper.
2. Let ordinary PR CI prove the Pi packaging repair and rerun pinned OpenCode.
3. Treat a generic hosted cross-UID denial as a qualified-host result.
4. On a passing qualified host and the same commit, use protected explicit
   native acceptance with all versions, budgets, permissions and case sets.
5. Keep Goal 0006b active until the full exact-subject matrix and independent
   review close. None of these dispositions permits a new-tree green inference.

## Audit commands and model route

Read-only evidence used:

```bash
gh api repos/wxxb789/symmetry/actions/jobs/108657962961
gh api repos/wxxb789/symmetry/actions/jobs/108657963001
gh api repos/wxxb789/symmetry/actions/jobs/108657962941
gh run view 36332839164 --job 108657962961 --log
gh run view 36332839164 --job 108657963001 --log
gh run view 36332839164 --job 108657962941 --log
```

Source comparison used `git show`, `git diff`, `git ls-tree`, `git log`, and
source reads. No rerun, comment, push, commit, service action, long test, witness,
provider action, or remote mutation occurred. Excerpts omit environment values
and retain only non-secret diagnostics.

The task envelope routed this child as `deep-low`. Runtime metadata reported
provider `ghc`, model `gpt-5.6-sol-fast`, reasoning level `medium`, and
`PI_CODING_AGENT=true`; `PI_MODEL_ROUTE` was unset. Route metadata grants no
acceptance authority.
