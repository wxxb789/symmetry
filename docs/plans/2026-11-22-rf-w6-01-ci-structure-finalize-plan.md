# rf-w6-01 ci-structure-finalize plan — five inline programs as real files, one contracts composite action

Contract: `goal-rf-w6-01-ci-structure-finalize` (queued, dispatch gates satisfied at
`40314211`; W6 reconcile of 2026-11-21 binding). Base revision: `403142114adab9b58e45fc2809fc91f142680e1b`
(`origin/main` at dispatch). Shape references: `rf-w3-01` (#212, the classifier extraction — the
nearest move-only CI extraction) and `rf-w4-16` (#224, the one Python CI-test harness the new pin is
written against).

## Owned set (STRICT: re-derived at this head)

`.github/workflows/ci.yml` (the five named steps, the `contracts` job's build steps, and the
reconcile-added steps; the classifier heredoc, the routing pins, every `--assert`/folded-gate step
and every witness step stay byte-identical) · `.github/workflows/linux-native-v2-acceptance.yml`
(only its `contracts` job) · `.github/actions/contracts-build/action.yml` (new) · five new
`scripts/ci` programs (names below) · `scripts/ci/test-ci-structure-extraction.py` (new) ·
`.github/dependabot.yml` (reconcile) · `browser/eslint.config.mjs`, `browser/package.json`,
`pnpm-lock.yaml` (reconcile: the browser ESLint block) · `docs/TODO.md`,
`docs/archive/CLOSED-TODO.md` (two closures) · `docs/design/quality.md` (gate table reconcile) ·
this plan file. `docs/goals/0010-refactor-program.md` is not touched: its status rows are the
conductor's T1 bookkeeping (the merged W5-01 row still says "queued"), so the W6-01 row update
belongs to the T1 refresh, not to this slice.

Not owned (oracles only): `scripts/ci/test-ci-changed-path-classifier.py`,
`test-assert-folded-gates.py`, `test-assert-playwright-json.py`, `scripts/ci/assert-*.py`, the
`changes` job's classifier, `pyproject.toml`.

## Inventory (re-measured at `40314211`, `git show origin/main:.github/workflows/ci.yml`)

`ci.yml` = 2,642 lines, 52 `run: |` blocks. The five largest bodies, by the dedented body line count
(the surgery's own measurement, `bodies/ci.rewritten.yml` diff = 792 removed / 9 added lines):

| step (name) | job | step line | body lines | program | effective cwd | invocation |
| --- | --- | --- | --- | --- | --- | --- |
| `Smoke-test local HTTP and production HTTPS Compose stacks` | `compose` | 2345 | 225 | `scripts/ci/compose-stack-smoke.sh` | repo root | `bash scripts/ci/compose-stack-smoke.sh` |
| `Verify Linux Pi cleanup pidfd group signaling` | `control` | 387 | 201 | `scripts/ci/verify-linux-pi-pidfd-cleanup.sh` | `control/` (job default) | `bash ../scripts/ci/verify-linux-pi-pidfd-cleanup.sh` |
| `Start isolated PostgreSQL loopback` | `pi-control-e2e-windows` | 1022 | 179 | `scripts/ci/pi-windows-postgres-loopback.ps1` | `control/` (job default) | `pwsh -NoProfile -File ../scripts/ci/pi-windows-postgres-loopback.ps1` |
| `Verify Windows Pi Control cleanup evidence` | `pi-control-e2e-windows` | 1299 | 94 | `scripts/ci/pi-windows-cleanup-evidence.ps1` | `control/` (job default) | `pwsh -NoProfile -File ../scripts/ci/pi-windows-cleanup-evidence.ps1` |
| `Verify daemon recovers a live run after control restart` | `integration` | 2149 | 88 | `scripts/ci/daemon-recovery-after-control-restart.sh` | `daemon/` (step key) | `bash ../scripts/ci/daemon-recovery-after-control-restart.sh` |

Five-body total = **787** lines; next largest surviving body = **55** (`Initialize Windows Pi E2E
workspace`), so the pin's 80-line ceiling is safe (the smallest extracted body is 88). The
classifier heredoc pattern `python3 - <<'PY'` occurs **0** times (W3-01, #212 landed); the pidfd
body's `python3 - <path> <<'PY'` is a different program and moves out with it.

Contract-level deltas against the re-derivation notes (lesson 16: cite by content): the draft's
"789 body lines" is 787 here (blank-line convention); the ds-1 A1-3/A1-4 corrections are adopted
(the ruff step needs the digest, not just the version); `docs/TODO.md` carries two W6-01 entries —
the ruff pin and the W3-16 PyYAML follow-up (both closed here, see Changes 4 and 8).

## Change 1 — the five bodies move verbatim into `scripts/ci` programs

Each step keeps its keys and its body becomes exactly one invocation line. Per the W6 reconcile the
step's working directory is **stated explicitly** (the reviewers' "do not add the key" reading is
superseded; the value is the step's existing effective cwd, so behaviour is identical and the pin
machine-checks resolution): `working-directory: control` added to the three `control`-default steps,
`working-directory: ${{ github.workspace }}` added to the `compose` step, `daemon` already stated.

The five programs are the base bodies, byte-identical, plus these enumerated deltas and nothing else:

1. `#!/usr/bin/env bash` (three bash programs) — the `scripts/ci` entry-point convention (rf-w3-01
   delta 1); also the ShellCheck dialect: without a shebang the advisory ShellCheck step over
   `scripts/ci/*.sh` reports SC2148 (error) and reads the file as `sh`.
2. `Set-StrictMode -Version Latest` (two PowerShell programs), after the existing
   `$ErrorActionPreference = 'Stop'` first line — the W6 reconcile P1 fail-open prologue.
3. The runner's own PowerShell epilogue as the last line of each PowerShell program —
   `if (Test-Path -LiteralPath variable:/LASTEXITCODE) { if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } }`
   — because `pwsh -NoProfile -File <program>` exits 0 on normal termination, dropping the
   runner-injected `exit $LASTEXITCODE` the base step had (reviewer P1-1). `variable:/LASTEXITCODE`
   is absent in a fresh `-File` process, which the guard handles; that is the runner's own
   mechanism, not a new one.

Per-native-call propagation (reconcile P1) is already present in the moved bytes and is **not**
re-written: `pi-windows-postgres-loopback.ps1` checks each of its four native families
(`pg_ctl --version`, `initdb`, `pg_ctl start` via `WaitForExit`/`ExitCode`, `createdb`) and throws
on non-zero; `pi-windows-cleanup-evidence.ps1` calls no native command. The audit table is in
"Frozen surfaces" below; the pin asserts the shape.

Move proof (machine-checked, judging artifact): `extract_bodies.py` reads
`git show <base>:.github/workflows/ci.yml`, locates each step by name, dedents the block scalar by
its own indentation and writes the body to the program path; the base-vs-HEAD diff per program is
the artifact. Bash programs: empty diff. PowerShell programs: exactly deltas 2 and 3. Mutation (a):
one character flipped inside a program → the proof reddens.

## Change 2 — one composite action for the duplicated `contracts` job

`.github/actions/contracts-build/action.yml` (composite) absorbs the step list both jobs spell
today, in base order: `Check lockfile registry provenance`, the advisory `Measure production
dependency advisories (advisory)` (`audit-prod` input; keeps `continue-on-error: true` and
`if: always() && inputs.audit-prod == 'true'`), `pnpm install --frozen-lockfile`,
`pnpm contracts:check`, `@symmetry/frontend` typecheck/lint/test/build, the `portal-assets` upload
(`control/priv/static/portal`), and — under `build-control-release` — `erlef/setup-beam@v1`,
`Build production Control release` and the `control-release-linux` upload
(`${{ runner.temp }}/control-release-linux.tar.gz`, `if-no-files-found: error`).

Both call sites keep their own `checkout`, `pnpm/action-setup`, `setup-node`, job name, `needs`,
`if` and `runs-on`; the step list after those becomes `uses: ./.github/actions/contracts-build` with
`audit-prod`/`build-control-release` = `true/true` in `ci.yml` and `false/false` in
`linux-native-v2-acceptance.yml` (whose base copy deliberately omits the advisory audit and the
shared release producer). Composite `run` steps state `shell:` (a workflow-level bare `run:` does
not need it); unnamed `run:` steps keep their command text and named steps keep their names.
`erlef/setup-beam` moves into the gated block because it exists only for the release producer; the
dependabot `directories` addition (Change 5) is what keeps that `uses:` scanned.

Observable delta, recorded: the acceptance `contracts` job now shows the release-producer and
advisory-audit steps as skipped (it showed no such steps). No artifact name, artifact path, job
name, `needs` or `if` changes; the artifact consumers are `control` (portal-assets and
control-release-linux), `integration` (control-release-linux) and the acceptance native job
(portal-assets) — the PR body names all three.

## Change 3 — `scripts/ci/test-ci-structure-extraction.py` (new pin) and its CI wiring

unittest, text-only over `ci.yml` and `action.yml` (it never executes the workflow), mirroring
`test-ci-changed-path-classifier.py`'s parsers:

1. the five steps' exact key/value sets and bodies are pinned (name/id/if/shell/working-directory/
   env), and each body's program path is resolved against the step's **effective** cwd
   (step key > job `defaults.run.working-directory` > repo root) and required to land on
   `scripts/ci/<program>`;
2. each program exists and is non-empty; bash programs start with `set -Eeuo pipefail`; PowerShell
   programs start `$ErrorActionPreference = 'Stop'` + `Set-StrictMode -Version Latest` and end with
   the propagation epilogue, and every native call site is followed by a status check
   (`$LASTEXITCODE`/`ExitCode`) before the next native call;
3. no `run: |` body in `ci.yml` exceeds 80 dedented lines; `python3 - <<'PY'` occurrences = 0;
4. the composite action exists, carries exactly the pinned step list/artifact names/paths/inputs and
   no caller re-spells `pnpm contracts:check`, `name: portal-assets` or `name: control-release-linux`;
5. **folded-gate completeness** (arch-decision-1, Gemini candidate 08): every step with
   `continue-on-error: true` is either named in its job's `assert-folded-gates.py` gate array or
   listed in the pin's explicit advisory allowlist; job-level `continue-on-error` is allowlisted by
   job; an unrecognized `continue-on-error:` value is refused; a stale allowlist entry fails;
6. a PowerShell fail-closed reproduction (skipped when `pwsh` is absent): a generated fixture whose
   native child exits 7, invoked exactly as the steps invoke their programs, must exit 7; the same
   fixture without the propagation line must exit 0 (the red witness for reviewer P1-1).

Mutation (c): reintroduce a 60-line inline block → check 3 reddens. Mutation for the action:
delete the `portal-assets` upload step from the action text → check 4 reddens (CI itself cannot be
run locally; recorded as such).

CI wiring: `legacy-route-audit` (the unconditional job) gets
`- run: python3 scripts/ci/test-ci-structure-extraction.py` beside
`test-ci-changed-path-classifier.py` (reviewer P2-3), so the pin runs on every event.

## Change 4 — the advisory ruff step installs 0.17.5 exactly; TODO sub-item (b) closed

`pyproject.toml` carries `required-version = "==0.17.5"`, so the step's `ruff_version='0.17.6'`
makes ruff refuse the config (exit 2) and the advisory metric never measures. The step moves to
`ruff_version='0.17.5'` **and** the matching digest
`ruff_archive_sha256='7d261fc981a784668e19e9fa6ad437bfd95b67f2dc263fc1eab87d54d72fcc90'` (re-derived
from the released `ruff-x86_64-unknown-linux-gnu.tar.gz` on 2026-11-22; the digest, not the version
string, is what proves the archive), so the download/digest guards can no longer skip the step by
mismatch. Advisory semantics (`continue-on-error`, skip notes) stay unchanged. Only sub-item (b) of
the TODO entry closes; sub-item (a) (`pyproject.toml`'s `extend-exclude`) stays open because
`pyproject.toml` is not owned and the excluded files still carry findings — its text stays in
`docs/TODO.md` with a line naming where (b) went.

## Change 5 — dependabot scans the composite action

`.github/dependabot.yml`'s `github-actions` entry gains
`directories: ["/", "/.github/actions/*"]` (the `directory:` key cannot express both; `directories`
is the list form) so the action's `uses:` pins stay in the update plane (reviewer P2-2).

## Change 6 — `GOOS=linux` vet for the host peer probe

`scripts/ci/linux-v2-host-peer-probe.go` is only ever compiled inside a dispatch-only acceptance
run (`linux-v2-host-prerequisite-probe.sh:231`, from `daemon/`), so it can rot silently
(arch-decision-1). The `daemon` job gains a step that mirrors that build environment:
`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=readonly go vet ../scripts/ci/linux-v2-host-peer-probe.go`
(vet type-checks; `go vet <file>` in module mode resolves `golang.org/x/sys` from `daemon/go.mod`).

## Change 7 — the `browser/**` flat ESLint block and its gate (plan §10.3, W6 reconcile)

`browser/eslint.config.mjs` (new flat config: `@eslint/js` recommended + node/browser globals for
`**/*.mjs`), `browser/package.json` gains an `eslint`/`@eslint/js`/`globals` devDependency trio at
the frontend's pinned versions and a `lint` script (`eslint . --max-warnings 0`, the measured
warning count), the root `pnpm-lock.yaml` is updated with `mise exec pnpm@12.17.0`, and the
`control` job (which already installs the workspace and drives the browser render gate) gains
`- run: pnpm --filter symmetry-browser-acceptance lint` right after its
`pnpm install --frozen-lockfile`. The finding count and the ratchet value are measured in the
evidence section; the acceptance workflow is untouched by this gate.

## Change 8 — the pinned PyYAML + `SYMMETRY_REQUIRE_PYYAML=1` step (W3-16 follow-up)

`docs/TODO.md`'s "install a pinned PyYAML for the lockfile differential oracle (W6-01)" entry (the
recorded follow-up of merged `goal-rf-w3-16-scripts-defects`) is closed here because this slice owns
the wave-6 `ci.yml`: the `daemon` job's `Check CI gate helper scripts` step gains
`env: SYMMETRY_REQUIRE_PYYAML: "1"` and, before its suites,
`python3 -m pip install --user --break-system-packages --quiet 'PyYAML==6.0.3'` followed by a
`test` on `yaml.__version__`, so a lost or drifted install reddens the job instead of silently
shrinking the oracle. `--break-system-packages` is required on the ubuntu runner's PEP 668
externally-managed Python; the alternative (a venv, or apt's unpinned PyYAML) is recorded in
"Ambiguities". The suite-side switch was verified locally (host PyYAML 6.0.3).

## Frozen surfaces and the base→HEAD key audit

Per-step control-flow audit (lesson 32), produced from `git show <base>:.github/workflows/ci.yml` and
the HEAD program side by side, not from memory:

| step | base keys | HEAD keys | early-exit / trap / `|| true` / `$LASTEXITCODE` branches in the base body | HEAD equivalent |
| --- | --- | --- | --- | --- |
| compose smoke | `name`, `shell` | same + explicit `working-directory` (repo root) | `trap cleanup EXIT` (prints both stacks, tears both down, removes temp dir, re-exits with `status`), `|| true` on every teardown command, explicit `exit 1` guards, `set +e` around the websocket probe | byte-identical in `compose-stack-smoke.sh` |
| pidfd | `name`, `id`, `if: always()`, `shell` | same + explicit `working-directory: control` | `set -Eeuo pipefail`, Python child's `finally` cleanup + non-zero `run_cleanup()` assertions, `sys.exit` via uncaught `RuntimeError` | byte-identical in `verify-linux-pi-pidfd-cleanup.sh` |
| Windows postgres | `name`, `id`, `shell` | same + explicit `working-directory: control` | `try/catch` that writes `postgres_status=unavailable` and rethrows; per-native status checks (`$versionStatus`, `$initStatus`, `$startStatus`, `$createStatus`); `::error::` annotation | byte-identical + StrictMode prologue + terminal epilogue |
| Windows cleanup evidence | `name`, `if: always()`, `shell`, `env` | same + explicit `working-directory: control` | `$cleanupFailed` accumulation, `exit 1`, `continue` on an unreadable marker | byte-identical + StrictMode prologue + terminal epilogue |
| daemon recovery | `name`, `working-directory`, `shell`, `env` | same | `trap cleanup EXIT` (kills the restarter, prints the control log, re-exits with `status`), `set +e`/`set -e` around the gate and the restarter wait, explicit `exit` of the first failing status | byte-identical in `daemon-recovery-after-control-restart.sh` |

Frozen: every job name, `needs`, `if`, `--assert` name, folded-gate list entry, artifact name and
artifact path; the classifier step and heredoc region; the opencode assertion steps and the witness
steps; the acceptance workflow's native job. The `--assert`/folded-gate steps are not touched by any
hunk.

## Tests added

- `scripts/ci/test-ci-structure-extraction.py` — the pin above (checks 1-6), wired into
  `legacy-route-audit`.
- No test pins a clause this slice does not implement; no existing test is renamed, weakened or
  deleted.

## Evidence plan (exact commands; carriers: this plan + the stage report = `answer.md`)

| what | command | proves |
| --- | --- | --- |
| move proof | `python3 extract_bodies.py --base 40314211…` then `diff` per program | the five bodies moved; only the documented deltas |
| surgery proof | `extract_bodies.py --verify` | worktree `ci.yml` = base + the five region collapses + the four cwd keys, byte for byte |
| pin | `python3 scripts/ci/test-ci-structure-extraction.py` | checks 1-6 green |
| workflow model | `python3 scripts/ci/test-ci-changed-path-classifier.py` | job graph/routing unchanged |
| folded gates | `python3 scripts/ci/test-assert-folded-gates.py`, `python3 scripts/ci/assert-folded-gates.py --help` | the oracle and its suite still green |
| playwright gate suite | `python3 scripts/ci/test-assert-playwright-json.py` | contract gate untouched |
| syntax | `bash -n` per bash program; the PowerShell parser per PowerShell program | parse-clean on the host (pwsh 7.7.0 present) |
| actionlint | pinned 1.7.12, `-shellcheck= -pyflakes=`, both touched workflows, before/after | findings not increased |
| shellcheck | `shellcheck --severity=warning scripts/ci/*.sh` before/after | the new programs' dialect/finding delta |
| ruff | `ruff 0.17.5` `check`/`format --check` over `scripts/ci` | the advisory metric's own verdict on the new file |
| browser lint | `pnpm --filter symmetry-browser-acceptance lint` (pnpm 12.17.0) | the new gate's own verdict + the ratchet value |
| lint ratchet bites | `eslint . --rule '{"no-unused-vars":["warn",{"ignoreRestSiblings":false}]}'`, with and without `--max-warnings 0` | 2 warnings: exit 0 without the ceiling, exit 1 with it; the shipped ceiling equals the measured 0 warnings |
| PyYAML switch | `SYMMETRY_REQUIRE_PYYAML=1 python3 scripts/ci/test-check-lockfile-registry.py`; and `python3 -S …` | the switch fails instead of skipping |
| real-script probes | `pwsh -NoProfile -File scripts/ci/pi-windows-cleanup-evidence.ps1` and the postgres program with a fabricated env | StrictMode did not break the moved logic; the fail-closed path still throws |
| whitespace | `git diff --check` | no whitespace damage |
| CI | the PR's full run | the slow oracle: every job name/gate unchanged, artifacts consumed, the composite action exercised |

## Mutation records (judging)

(a) one character inside `compose-stack-smoke.sh` → the move proof diff is non-empty;
(b) the `portal-assets` upload step deleted from `action.yml` → the pin's action check reddens;
(c) a 60-line inline block reintroduced into `ci.yml` → the pin's size check reddens;
(d) the propagation line removed from the fail-closed fixture → the runtime reproduction reddens
(and the un-guarded fixture exits 0, which is the hazard itself).

## Ambiguities resolved (options and the recommendation taken)

1. **`working-directory` keys (reconcile bullet 2 vs reviewers P2-1/A1-1).** Options: (a) keep the
   keys as they are and write invocations relative to the effective cwd only; (b) state the key
   explicitly with the same value. (b) is the reconcile's literal text, keeps the invocation
   unambiguous for later writers, and the pin machine-checks the resolution; behaviour is identical
   either way. Taken: (b).
2. **PowerShell fail-closed mechanism.** Options: (a) invoke in-process (`& ../scripts/ci/x.ps1`) so
   the runner's prelude/epilogue apply (reviewer P1-1's own amendment); (b) keep the contract's
   `pwsh -NoProfile -File` one-liner and make the programs fail-closed themselves, which is what the
   reconcile's acceptance test (`a local pwsh -File reproduction`) describes. Taken: (b), with the
   reconcile's prologue/epilogue deltas and the red/green fixture reproduction.
3. **Composite action boundary.** Options: (a) absorb only the enumerated list, leaving `setup-beam`
   in the caller; (b) absorb the release producer whole, `setup-beam` included, gated by
   `build-control-release`. (b) is what "the step list both jobs spell" means for a self-contained
   action and dependabot (Change 5) restores the pin scanning the reviewer flagged. Taken: (b).
4. **PyYAML install under PEP 668.** Options: (a) `pip install --user --break-system-packages`;
   (b) a venv per step; (c) rely on apt's PyYAML. (a) is one line, keeps the pinned version, installs
   to the user site (not over apt files) and is exactly the documented escape hatch; (c) is unpinned.
   Taken: (a).
5. **Browser ESLint carrier.** Options: (a) the browser package owns a flat config + devDeps and CI
   runs it from the `control` job; (b) a root-level config reusing the frontend's dependency
   closure. Flat-config `files` patterns resolve against the config's base path, so (b) needs path
   trickery for a package that is not the frontend's; (a) is the conventional shape and puts the
   gate where the browser toolchain already installs. Taken: (a).
6. **Evidence carrier.** The strict owned set has no `docs/evidence/` file, so the record lives in
   this plan file plus the stage report, following rf-w5-01/rf-w3-01. Conductor can rule otherwise.
7. **Two TODO entries point at W6-01.** The ruff entry is in the contract; the PyYAML entry is not,
   but a merged contract (`rf-w3-16`) name W6-01 as its owner and its fix is a `ci.yml` edit only the
   wave-6 owner can land. Dropping it would be a silent scope narrowing. Taken: implement both.

## Design trade-offs

- **Move verbatim vs tidy the bodies.** The bodies keep their exact bytes, including their existing
  inline comments, because the empty-diff proof is the contract's primary control-flow artifact;
  the three deltas above are the entire authored diff inside a program.
- **One action vs a reusable workflow.** A composite action keeps the callers' job graph (needs/if/
  runs-on/environment) unchanged and needs no `secrets`/`permissions` re-declaration; a reusable
  workflow would move the jobs and change job names.
- **Pin the five steps' exact key sets.** A weaker "program exists and is invoked" pin would pass
  with a renamed step or a dropped `if: always()`. The exact sets make a future edit redden; the five
  steps are frozen by D1, and W6-02/W6-10/W6-12 do not own them.
- **Advisory allowlist rather than auto-classifying.** The completeness check cannot know which
  `continue-on-error` steps are intentional; the explicit allowlist is the record, and a new advisory
  step must be added deliberately.

## Out of scope (with reasons)

The classifier extraction (W3-01, landed); browser wiring and the new browser job (W6-02); the
Elixir credo/coverage flips (W6-10); wall-clock thresholds (W6-12); the duration summarizer and
browser inventory (W2-01, landed); coverage jobs (W2-17, landed);
`scripts/ci/linux-v2-native-acceptance.sh` internals and the acceptance case sets (W5-02/W6-05);
dependency version bumps (dependabot); the other 47 `run: |` blocks (only the five named move);
`pyproject.toml`'s `extend-exclude` (TODO sub-item (a) stays open).

## Dependency / rebase note

Dispatch chain re-derived at `40314211`: W1-01 #157, W2-01 #188, W2-17 #186, W3-01 #212 and the
in-flight harness W4-16 (#224) are merged; #180, #137 and the dependabot PRs are merged/closed;
W5-01 was removed from the chain by arch-decision-2 R13. This slice is the wave-6 `ci.yml`
initiator; W6-02 dispatches after it, W6-10 after W6-02, W6-12b last.

## Method note: how the five regions were moved

The move is a machine-checked function of the base bytes, not a transcription. `extract_bodies.py`
(scratch tool, sha256 `f5f59fd12aca68d2c78c6c5558a2252d4d89b35406dfb7e88f686b3646cb9b60`, kept with the
run artifacts) reads `git show <base>:.github/workflows/ci.yml`, locates each step by name, dedents
its block scalar and writes the body to `scripts/ci/<program>`; `--ci` regenerates the post-surgery
workflow text from the base and `--verify` compares it with the worktree file. The extracted
programs were written from that tool; every authored byte inside them (the three shebangs, the two
prologues, the two epilogues) went through the patch tool. Everything else in `ci.yml` — the
composite-action swap in the two `contracts` jobs, the browser-lint step, the peer-probe vet, the
ruff pin, the PyYAML install, the pin wiring — was authored with the patch tool directly.

## Executed gates (this tree, 2026-11-22; uncommitted worktree)

This table binds the implement-stage revision; the apply-stage refresh below supersedes the pin,
browser-lint, ruff, shellcheck and actionlint rows it names.

| Command | Result |
| --- | --- |
| `extract_bodies.py --programs` (base `40314211`) | wrote the five programs; five-body total 787 lines |
| `extract_bodies.py --ci --verify` | `verify: worktree ci.yml equals the generated extraction` (run at the surgery; the pin re-checks the five regions on the final tree) |
| base-vs-program `diff` per program | `compose-stack-smoke.sh` +1 line (`#!/usr/bin/env bash`); `verify-linux-pi-pidfd-cleanup.sh` +1 (shebang); `daemon-recovery-after-control-restart.sh` +1 (shebang); `pi-windows-postgres-loopback.ps1` +2 (`Set-StrictMode -Version Latest`, the propagation epilogue); `pi-windows-cleanup-evidence.ps1` +2 (same two); **no other difference** |
| `python3 scripts/ci/test-ci-structure-extraction.py` | **exit 0**, `Ran 13 tests`, `OK` |
| `python3 scripts/ci/test-ci-changed-path-classifier.py` | **exit 0**, `Ran 31 tests in 62.246s`, `OK`; all 18 fixture triples printed; re-run on the final bytes: **exit 0**, `Ran 31 tests in 36.129s`, `OK` |
| `python3 scripts/ci/test-assert-folded-gates.py` | **exit 0**, `Ran 6 tests`, `OK` |
| `python3 scripts/ci/test-assert-playwright-json.py` | **exit 0**, `Ran 17 tests`, `OK` |
| `python3 scripts/ci/assert-folded-gates.py --help` | **exit 0** |
| `SYMMETRY_REQUIRE_PYYAML=1 python3 scripts/ci/test-check-lockfile-registry.py` | **exit 0**, `Ran 25 tests`, `OK`, no skip |
| `python3 -S scripts/ci/test-check-lockfile-registry.py` | **exit 0**, `OK (skipped=1)`, one `PyYAML differential oracle skipped: module not importable` notice |
| `SYMMETRY_REQUIRE_PYYAML=1 python3 -S scripts/ci/test-check-lockfile-registry.py` | **exit 1**, `FAILED (failures=1)`: `SYMMETRY_REQUIRE_PYYAML=1 requires the PyYAML differential oracle, and it is not importable` |
| `bash -n` on the three bash programs | **exit 0** each |
| PowerShell parser over the two `.ps1` programs (pwsh 7.7.0) | **exit 0** each, no parse errors |
| `pwsh -NoProfile -NonInteractive -File scripts/ci/pi-windows-cleanup-evidence.ps1` with a fabricated env | **exit 0**; logs `cleanup verification e2e_outcome=success` and `cleanup verification accepted: …`; provenance `orphan_cleanup_status=passed` |
| `pwsh -NoProfile -NonInteractive -File scripts/ci/pi-windows-postgres-loopback.ps1` with a fabricated env, `POSTGRES_PORT=abc` | **exit 1** at the intended `postgres_status=unavailable` throw (`postgres_candidate_bins=none`; this host has no `pg_ctl.exe`) — the full discovery path ran under `Set-StrictMode -Version Latest` with no StrictMode error |
| `shellcheck --severity=warning --format=gcc scripts/ci/*.sh` (0.11.0) | pre-change set (6 files) 10 findings; changed set (9 files) **exit 1, 10 findings** → the three programs add **0** (the advisory step's baseline is unchanged); the same program without its shebang adds 1 (`SC2148`) |
| `python3 scripts/ci/assert-browser-suite-coverage.py` | **exit 0**, `browser_suite_coverage: suites=16 wired=7 manual=9 unclassified=0`, `PASS` (the two edited specs keep their names and titles) |
| `python3 scripts/ci/test-no-legacy-routes-audit.py` | **exit 1**, `FAILED (failures=11)`, every failure `/bin/bash: C:/…/audit-no-legacy-routes.sh: No such file or directory` (127) — the host artifact rf-w3-01's plan already recorded for the base on Windows (the suite plants Windows temp paths for `/bin/bash`); this slice touches no legacy-route surface and the same suite runs on ubuntu in the unconditional `legacy-route-audit` job. A WSL re-run was attempted and is inconclusive here (this distro has no `python3`). |
| `ruff check scripts/ci` / `ruff format --check scripts/ci` (0.17.5) | **exit 0** `All checks passed!` / **exit 0** `58 files already formatted` |
| `ruff check` / `format --check` on the new pin | **exit 0** / **exit 0** |
| actionlint 1.7.12 `-shellcheck= -pyflakes=`, the two touched workflows | base **exit 0**, 0 findings; HEAD **exit 0**, 0 findings (all workflows: base 0, HEAD 0; a negative control with an injected `inputs.nope` expression fails, so the run is live) |
| `node node_modules/eslint/bin/eslint.js --config browser/eslint.config.mjs browser` (eslint 10.14.0, `@eslint/js` 10.0.1, globals 17.14.0 — a scratch install of the pinned versions, see limits) | **exit 0**, no findings, 22 files linted (every `.mjs` under `browser/`) |
| `pnpm install --lockfile-only --frozen-lockfile` (pnpm 12.17.0) | against the **base** lockfile: `ERR_PNPM_OUTDATED_LOCKFILE … 3 dependencies were added: @eslint/js@10.0.1, eslint@10.14.0, globals@17.14.0`; against the **edited** lockfile: no error, `Verifying lockfile against supply-chain policies (489 entries)…` completes (two independent runs) |
| frozen-region hunk audit: `git diff -U0 .github/workflows/ci.yml` | 14 hunks; base-side ranges 124-167 (contracts job), 391-592, 1025-1204, 1304-1398, 1747+1749, 2157-2245, 2347-2572 plus five pure additions; no hunk falls in the classifier step, the opencode assertion steps (1509-1527, 1584-1626, 1880-1932) or the witness steps (1836-1872, 2058-2070) |
| `git diff --check` | **exit 0** |
| CI (the PR's full run) | not run here: no commit, no push; conductor-verified on the PR, and it is the only place the composite action's steps, the pinned PyYAML download and the artifact hand-off are exercised |

## Apply-stage refresh (rf-w6-01 simplify-apply, 2026-11-22)

The simplify review's accepted findings changed five surfaces (the pin, the action description, the
browser flat config, the lint script and the two TODO records); `ci.yml`, the acceptance workflow
and the five programs are untouched by the apply stage. Gates re-run on the changed tree:

| Command | Result |
| --- | --- |
| `python3 scripts/ci/test-ci-structure-extraction.py` | **exit 0**, `Ran 14 tests in 2.183s`, `OK` |
| `python3 scripts/ci/test-ci-changed-path-classifier.py` | **exit 0**, `Ran 31 tests in 48.778s`, `OK` |
| `python3 scripts/ci/test-assert-folded-gates.py` | **exit 0**, `Ran 6 tests in 0.589s`, `OK` |
| `python3 scripts/ci/test-assert-playwright-json.py` | **exit 0**, `Ran 17 tests in 3.502s`, `OK` |
| `python3 scripts/ci/assert-folded-gates.py --help` | **exit 0** |
| `SYMMETRY_REQUIRE_PYYAML=1 python3 scripts/ci/test-check-lockfile-registry.py` | **exit 0**, `Ran 25 tests in 5.574s`, `OK`, no skip |
| `bash -n` on the three bash programs | **exit 0** each |
| PowerShell parser over the two `.ps1` programs (pwsh 7.7.0) | **exit 0** each, no parse errors |
| actionlint 1.7.12 `-shellcheck= -pyflakes=`, the two touched workflows and every workflow | **exit 0**, 0 findings |
| `shellcheck --severity=warning --format=gcc scripts/ci/*.sh` (0.11.0) | exit 1, 10 findings, all in the two pre-existing acceptance programs; the three moved programs add 0 |
| `ruff check scripts/ci` / `ruff format --check scripts/ci` (0.17.5) | **exit 0** `All checks passed!` / **exit 0** `58 files already formatted` |
| `eslint . --max-warnings 0`, the shipped script's own command (eslint 10.14.0, `@eslint/js` 10.0.1, globals 17.14.0 from a scratch install, see limits) | **exit 0**, 22 files linted, 0 errors, 0 warnings |
| the ratchet probe: the same file set with `--rule '{"no-unused-vars":["warn",{"ignoreRestSiblings":false}]}'` | **exit 0** (2 warnings) without the ceiling, **exit 1** with it, so the ceiling is the measured warning count 0 |
| `git diff --check` | **exit 0** |

The accepted findings: the dead `RUN_BLOCK` constant and the unreachable job-default
`working-directory` fallback are gone (the exact-keys assertion already freezes each step's
`working-directory`); the action's two inputs are pinned per input (each block must carry
`required: true`, with a mutation that drops one flag and reddens the check); the browser config
comment naming a non-existent identifier is gone; the action description is one sentence.
Refreshed counts: the pin 637 → 647, `.github/actions/contracts-build/action.yml` 75 → 73, the
browser flat config 19 → 18.

## Mutation records (all on the real files, restored and md5-verified)

| Mutation | Observed |
| --- | --- |
| (a) `compose-stack-smoke.sh`: `wait_for "local control health endpoint" 60` → `61` | the base-vs-program diff grows the extra hunk (`67c68  < … 60 \  ---  > … 61 \`); restored md5 `33569536314322d77593b9ac106fc9a8` |
| (b) `action.yml`: the `portal-assets` upload step deleted | pin **exit 1**, `FAILED (failures=2)`; `test_the_action_and_both_call_sites_match_the_pinned_shape` fails on the changed artifact list; restored md5 `d5d9ae89915b872d1d63896917185853` |
| (c) `ci.yml`: the compose step's one-line body replaced by a 60-line inline block | pin **exit 1**, `FAILED (failures=2)`; `AssertionError: scripts/ci/compose-stack-smoke.sh: the step keys changed: {… 'run': '\|'} != {… 'run': 'bash scripts/ci/compose-stack-smoke.sh'}`; restored md5 `8ed1e9743ca3a2b8e753738c00f479ff` |
| (d) the fail-closed fixture without its propagation line (in-suite, `FailClosedPowerShellTests`) | `pwsh -NoProfile -File` exits **0** on a failing native child; with the line it exits **7** — the reviewer P1-1 hazard and its closure in one run |
| browser gate: `const unusedProbe = 1;` appended to `browser/tests/readable-fixtures.mjs` | eslint **exit 1**, `'unusedProbe' is assigned a value but never used`; restored |
| in-suite mutations (`MutationTests`) | the five YAML/action mutations (the apply stage added the one-flag-dropped input mutation) redden the same checkers the tests call; the folded-gate positive control (an advisory flag on a step already named in its job's `assert-folded-gates.py` array) stays green |

## dLOC (report-only, D2)

`ci.yml` 2,642 → **1,831** lines (net −811; `+26/−837`): −783 the five extractions, −39 the
`contracts` job's absorbed steps becoming one `uses:` call, +12 the four reconcile-added steps
(pin wiring 3, browser lint 2, peer-probe vet 2, PyYAML install 5 minus the 2 lines the step's
`run:` reused), and ±1 from the surrounding hunks. The draft's "≈1,780 (≤1,800)" assumed a larger
action reduction than the measured 39 lines and no reconcile additions; the re-derived number is
1,831. `linux-native-v2-acceptance.yml` 181 → 175 (−6); new `.github/actions/contracts-build/action.yml`
73 (75 at the implement revision: the apply stage shortened its description); the five programs 794
lines (787 base + 7 authored prologue/epilogue lines); the pin 647 (637 at the implement revision,
minus the dead constant and the unreachable fallback, plus the per-input input pin and its
mutation); the browser flat config 18; modified tracked files `+125/−888` (`git diff --numstat` over
the files present in both revisions) plus the new files.

## Simplification review

- The five programs are the workflow's own bytes; nothing was reformatted, renamed or re-commented
  inside them.
- The composite action replaces two hand-kept copies of one step list with one; the only added
  complexity is the two boolean inputs, which are what keeps the acceptance job's base semantics.
- The pin is one text-parsing file with module-level checkers; the mutation tests call the same
  checkers instead of re-implementing the expectations, so a checker change cannot pass unnoticed.
- Nothing in the extracted bodies was abstracted: `verify-linux-pi-pidfd-cleanup.sh` keeps its
  inline Python harness, `compose-stack-smoke.sh` keeps its heredocs.

## Residual risks / known limits

- **The runner image's registry keys.** `Set-StrictMode -Version Latest` makes a *missing* property
  access an error; `pi-windows-postgres-loopback.ps1` reads `$properties.'Base Directory'` from
  `Get-ItemProperty` on each discovered `HKLM/HKCU …\PostgreSQL\Installations\*` key. On this host
  no such key exists (discovery returned `postgres_candidate_bins=none`), so that one access was
  not exercised locally; on the Windows runner the key exists and carries `Base Directory`
  (the base script has always required it to build a candidate), and any mismatch now fails closed
  in the script's own `catch` instead of silently. The `daemon-windows`-independent
  `pi-control-e2e-windows` job is the oracle.
- **The browser-lint measurement.** The corporate npm mirror cannot resolve
  `typescript-eslint@8.73.0`, which `frontend/package.json` pins, so no workspace install (and no
  pnpm lockfile regeneration) is possible in this environment; `registry.npmjs.org` is
  unreachable. The three new versions were installed into a scratch directory and ESLint was run
  from this tree through a `node_modules` junction, i.e. the same config file, same versions and
  same file set; the `pnpm --filter symmetry-browser-acceptance lint` wrapper and the
  `pnpm install --frozen-lockfile` consumption of the lockfile hunk are CI-only oracles.
- **The lockfile hunk.** `pnpm-lock.yaml` gains nine importer lines (three specifier/version pairs
  copied from the frontend importer's own resolved forms); the resolutions themselves already
  exist in `packages:`/`snapshots:`, so no new resolution or registry URL is introduced. pnpm's
  own frozen validation accepts the edited file (table above) and rejects the base one with exactly
  the three new specifiers, which is the machine check that the hunk is complete; the full
  `pnpm install --frozen-lockfile` on the runner remains the CI oracle, because the corporate
  mirror here cannot resolve the pre-existing `typescript-eslint@8.73.0` and `registry.npmjs.org`
  is unreachable, so no lockfile *regeneration* was possible in this environment.
- **The composite action's `always()` reachability (disclosed delta, D1 escalation).** Inside the
  action the advisory audit keeps its base `if: always()`, but the base step also ran when an
  earlier *caller* step (`checkout`, `pnpm/action-setup`, `setup-node`) failed, while the unguarded
  `uses:` call step is skipped in that case: the advisory metric is lost exactly when the
  toolchain install fails. Left as a disclosed behaviour delta rather than fixed — giving the call
  step `if: always()` would run install and build after a failed setup, and hoisting the advisory
  step back to the callers would drop the dedup this slice exists for. Composite-action step
  gating is exercised only by CI.
- **CI-only checks.** The pinned ruff archive, the pinned PyYAML wheel, the composite action's step
  list, the artifact hand-off and the Windows-runner PostgreSQL discovery run only on the runner.
