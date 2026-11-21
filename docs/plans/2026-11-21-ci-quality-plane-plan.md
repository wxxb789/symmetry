# rf-w1-01-ci-quality-plane plan — the advisory lint/vulnerability plane in `ci.yml`

Contract: `goal-rf-w1-01-ci-quality-plane.md` (GDD, 2026-11-21). Baseline for every coordinate in
this plan: this worktree's HEAD `a3039f0d17fc7c7905b63bdf247cac5bf339b213` (`a3039f0`, also
`origin/main`), branch `refactor/rf-w1-01-ci-quality-plane`. Binding inputs: program plan §4 W1-01
and §10.1-§10.6, `docs/design/quality.md` (gate policy), and the contract's plan-review amendments
(no new jobs; coverage dropped; vulnerability scanning added; `.github/dependabot.yml` owned).

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `.github/workflows/ci.yml` | modified | nine advisory steps inside five existing jobs (no new job) |
| `daemon/.golangci.yml` | new | golangci-lint v2 config (program plan §10.2, verbatim + recorded decisions) |
| `control/.credo.exs` | new | Credo config from `mix credo gen.config` + the two contract changes |
| `.github/dependabot.yml` | new | version updates for gomod, mix, npm, github-actions |
| `docs/TODO.md` | one entry | the actionlint entry's now-false "no CI job invokes actionlint" sentence replaced with the measured state; entry stays open |
| `docs/plans/2026-11-21-ci-quality-plane-plan.md` | new | this plan |

`docs/archive/CLOSED-TODO.md` is untouched: no entry is closed (see "Out of scope").

## The nine advisory steps (job, pin, guard, printed count)

Every step is `continue-on-error: true` and `if: always()`: it never gates, it still runs when an
earlier step of its job failed, and its own failure cannot change a job's conclusion. Nine steps,
five existing jobs — the job set is pinned by
`scripts/ci/test-ci-changed-path-classifier.py` (`PINNED_GATES`/`DEFAULT_GATE`, closed-world over
the `jobs:` block), which the contract's amendment names as owned elsewhere, so the plane grows
inside the jobs that already exist:

| Job | Step | Pin | Guard (printed note + `exit 0`) | Count printed by |
| --- | --- | --- | --- | --- |
| `legacy-route-audit` | `Measure ruff findings in scripts/ci (advisory)` | ruff 0.17.6, GitHub release tarball, sha256 `94b33791…` | no root `pyproject.toml` (W1-11 owns it); download/digest failure | ruff's own `Found N errors.` and `N files would be reformatted, M already formatted` |
| `legacy-route-audit` | `Measure shellcheck findings in scripts/ci (advisory)` | runner-preinstalled shellcheck (0.11.0 locally) | shellcheck absent; no `scripts/ci/*.sh` | the step's `shellcheck_findings=<n>` (gcc-format findings printed too) |
| `legacy-route-audit` | `Measure actionlint findings for the workflows (advisory)` | actionlint 1.7.12, GitHub release tarball, sha256 `8aca8db9…` | download/digest failure; no workflow file | the step's `actionlint_findings=<n>` (findings printed too) |
| `daemon` | `Measure golangci-lint findings (GOOS=linux, advisory)` | golangci-lint v2.15.0, GitHub release tarball, sha256 `9b7fe6a6…` | `daemon/.golangci.yml` absent; download/digest failure | golangci-lint's own `N issues:` summary + per-linter breakdown |
| `daemon` | `Measure govulncheck findings (advisory)` | govulncheck v1.10.0 (`go install …@v1.10.0`) | install failure | the step's `govulncheck_vulnerabilities=<n>` + govulncheck's own output |
| `daemon-windows` | `Measure golangci-lint findings (GOOS=windows, advisory)` | golangci-lint v2.15.0, GitHub release zip, sha256 `cd7ea606…` | same as the Linux step | golangci-lint's own `N issues:` summary |
| `control` | `Measure Credo strict findings (advisory)` | the project's `~> 1.7` resolution (1.7.19 locally) | `:credo` absent from `control/mix.exs` (W1-02 owns the dep); `.credo.exs` absent; `deps/credo` unfetchable | Credo's own `N mods/funs, found …` summary + the step's parsed `credo_strict_issue_total=<n>` |
| `control` | `Measure Hex advisories (advisory)` | the project's Hex | `mix.lock` absent; Hex archive absent | the step's `hex_audit_advisories=<n>` + `mix hex.audit`'s advisory list |
| `contracts` | `Measure production dependency advisories (advisory)` | the project's pnpm 12.17.0 | `pnpm-lock.yaml` absent | pnpm's own `N vulnerabilities found` / `No known vulnerabilities found` |

Placement reasons: `legacy-route-audit` is the workflow's only unconditional job (the classifier
test's `UNCONDITIONAL_JOBS`), so the four scripts/workflow-lint metrics are produced for every
event and no other job's failure skips them; the daemon/control/contracts steps sit directly after
their toolchain prerequisite (`setup-go`, `mix deps.get`, the lockfile provenance check) so they
never add a fetch the job would not already do. The two golangci steps bound themselves
(`timeout-minutes: 12` / `8`) so a hung analysis can only redden an advisory step: the
`daemon-windows` job carries a 20-minute budget the analysis must not eat.

The contract's clause 6 ("mergeable alone — every step tolerates absent configs/deps") is the
guards column: the only prerequisite this increment does not itself ship is the `:credo`
dependency (W1-02) and the ruff config (W1-11); both paths were executed locally and print their
note at exit 0.

## Measured baselines (re-taken, pinned, uncapped)

All numbers below are from this increment's runs on `a3039f0` with the configs as shipped; the
commands and exit codes are in "Evidence plan".

**golangci-lint v2.15.0, `daemon/.golangci.yml`, uncapped**
(`--max-issues-per-linter=0 --max-same-issues=0`):

| Scope | GOOS=linux | GOOS=windows |
| --- | --- | --- |
| module-wide (the CI metric) | **1029** (791 production / 238 test) | **449** (327 / 122) |
| `./internal/app/... ./internal/state/...` (the daemon-app report's scope) | 253 (207 / 46) | 172 (130 / 42) |

The second row is deliberately measured: it reproduces the daemon-app audit's per-linter table
exactly (gocyclo 105, gocritic 50 = 42+8, unparam 23 = 11+12, errcheck 10, exhaustive 12, staticcheck
22, unconvert 6, nilerr 6, funlen 4, ineffassign 6, errorlint 3, copyloopvar 3, misspell 1,
whitespace 2; windows 58/19/18/9/12/17/5/4/5/9/3/1/2/10 for the same linters) — which proves this
config is the report's config, and shows the program plan's §10.2 line "Baselines with this
config: linux 253 (207 prod / 46 test), windows 172 (130 / 42)" is **area-scoped, not
module-wide**. The CI metric is the module-wide pair; the burn-down slices should report against
it, not against 253/172.

**Credo strict, `control/.credo.exs`, Credo 1.7.19** (the version `{:credo, "~> 1.7"}` resolves
to today): **528 issues**, 70 checks, 154 files (lib+test), split 6 warnings / 383 refactoring
opportunities / 84 code readability / 55 software design; 423 in `lib/`, 105 in `test/`. Largest
classes: Nesting 162, CyclomaticComplexity 149, AliasUsage 33, MaxLineLength 32, FunctionArity 29,
DuplicatedCode 22, AliasOrder 22. Against the audits' reference points: 474 is the same scope
under Credo's built-in default set (measured, no `checks:` map), and the two configured changes
account for the whole delta — DuplicatedCode at `mass_threshold: 40` adds exactly its 22 design
findings, and `MaxLineLength` at 98 (the formatter's length, not the generated 120) adds exactly
its 32 readability findings. 474 − 403 = 71 is also reproduced: AliasOrder 22 + AliasUsage 33 +
PreferImplicitTry 8 + StringSigils 0 + Apply 0 = 71, so the audit's 403-vs-474 observation is the
five named checks' findings, while an empty `checks: %{}` map measured **474** here (Credo 1.7.19
falls back to its built-in list instead of disabling those five). The trap is that the map is
*silently* replaced either way, which is why this config spells out both lists and the contract
forbids the key.

**Credo parse-timeout under-count (a real defect found by this re-take).** With the generated
`parse_timeout: 5000`, a run on a loaded host printed `info: Some source files were not parsed in
the time allotted` and skipped six of the largest sources (`goals.ex` is 10,810 lines): 288 issues
over 148 files, still with a `found …` summary — a silent under-count of the metric the gate
exists to record. The shipped config raises the cap to 30,000 (a hang guard, not a budget) and the
step additionally prints `credo_strict_unparsed_files=<n>` with a loud note whenever Credo skipped
anything, so a lowered number can never pass as the baseline. With the cap raised, the same loaded
host reproduced 528 / 154 files / 0 unparsed.

**ruff 0.17.6, `scripts/ci`** (defaults; the W1-11 `pyproject.toml` will change both numbers):
`ruff check scripts/ci` → 94 findings, 42 fixable; `ruff format --check scripts/ci` → 50 of 51
files would be reformatted. Both match the scripts-ci audit's F5.

**shellcheck 0.11.0, `shellcheck --severity=warning scripts/ci/*.sh`** → **10 findings**
(SC2174 ×3, SC2043, SC2024, SC2276 ×2, SC2289, SC1078, SC2288), matching the audit's F6; the
hosted count can drift with the runner image's shellcheck version, which is why the step prints
the version it ran.

**actionlint 1.7.12, `-shellcheck= -pyflakes=`, both workflows** → **0 findings**, exit 0.

**govulncheck v1.10.0, `./...` in `daemon/`** → `No vulnerabilities found.`, exit 0 (DB 2026-11-11).

**`mix hex.audit` in `control/`** → **1 advisory**, exit 1: `plug 1.20.3 - EEF-CVE-2026-120287
(MEDIUM)`, the live advisory W1-02 bumps. **`pnpm audit --prod`** (pnpm 12.17.0, lockfile only,
no install) → `No known vulnerabilities found`, exit 0.

The recorded ratchet baseline for the pull request is therefore: **Credo strict 528** (1.7.19,
`control/.credo.exs`), **golangci-lint linux 1029 / windows 449** (v2.15.0, `daemon/.golangci.yml`,
uncapped), **ruff 94 check / 50 format**, **shellcheck 10**, **actionlint 0**, **govulncheck 0**,
**hex.audit 1**, **pnpm audit 0**.

## Out of scope

- **Coverage artifacts** — dropped by the contract's binding amendment (`mix test --cover` exits 1
  under the 90 % threshold in `mix.exs`; `-coverprofile` on the race step risks its 600 s budget).
  Wave 2 takes it. No step in this increment touches coverage.
- **Blocking flips** (W6-10/W6-12): every new step stays `continue-on-error: true`.
- **`ci.yml` structural extraction** (W2-01/W3-01/W6-01), the browser-suite inventory gate, and any
  change to `.github/workflows/linux-native-v2-acceptance.yml`.
- **Configs owned elsewhere**: `control/mix.exs` + `mix.lock` (W1-02's `:credo` dep), root
  `pyproject.toml` (W1-11's ruff config), `frontend/**` + `package.json` (W1-10). This increment
  only *reads* them and skips with a printed note while they are absent.
- **Closing the actionlint `docs/TODO.md` entry** (see below) and any `docs/archive/CLOSED-TODO.md`
  entry: nothing is closed by an advisory-only plane.
- No test/type/validation/permission is weakened: no existing step's command, inputs, or
  `continue-on-error` state is touched, and no new job is added, so the classifier test's routing
  model is unchanged (it is run as a gate, not edited — it belongs to W2-01/W3-01).

## Decisions and their justification

1. **Advisory by `continue-on-error`, with the tool's own exit status preserved.** The step
   intentionally exits non-zero when its tool reports findings: the CI log then shows a red step
   (the signal a burn-down slice needs) while the job's conclusion stays success, which is exactly
   the contract's "non-blocking step". A step that always exited 0 would hide the signal; a step
   without `continue-on-error` would flip a gate.
2. **Pinned downloads with sha256, not version-only installs.** The repository's existing pattern
   (the OpenCode and Pi release steps) is a pinned URL plus `sha256sum --check --strict`;
   golangci-lint, actionlint and ruff are installed that way. Every digest in this increment was
   taken from the project's own checksum asset and re-verified locally by downloading the asset and
   hashing it (`golangci-lint-2.15.0-linux-amd64.tar.gz` `9b7fe6a6…`,
   `golangci-lint-2.15.0-windows-amd64.zip` `cd7ea606…`, `actionlint_1.7.12_linux_amd64.tar.gz`
   `8aca8db9…`, `ruff-x86_64-unknown-linux-gnu.tar.gz` `94b33791…`), and the Windows golangci-lint
   archive was executed end to end from that download path. `govulncheck` has no release binary, so
   it is pinned by `go install …@v1.10.0` (sumdb-verified) instead of a digest.
3. **ruff from its GitHub release, not `pip install`.** The pin then covers the binary itself
   instead of depending on the image's Python packaging state: `pip install` into a
   Debian-family system Python can be refused as `externally-managed-environment`, and a venv
   adds a moving part for a one-binary tool. The release tarball is the same artifact `pip` would
   install, and the same 0.17.6 release produced the local 94/50 measurement (through its Windows
   asset).
4. **`legacy-route-audit` hosts the scripts/workflow lint metrics.** It is the only unconditional
   job, so the metrics do not depend on the heavy matrix running (a documentation-only pull request
   still produces them) and no new job is needed. Cost: ~20 s of its own runner time.
5. **`control/.credo.exs` from `gen.config`, with the two contract changes and three recorded
   deviations.** `mass_threshold: 40` is the 1.7 spelling of the contract's "mass 40" (`mass:`
   is an unknown param in 1.7.19 — Credo prints a config warning and ignores it, which the first
   cut did and this one does not); `strict: true` mirrors the `--strict` the step passes so a bare
   `mix credo` cannot under-report; `color: false` keeps the log greppable so the extracted count
   equals a local count; `parse_timeout: 30000` is the anti-under-count fix above. The
   `enabled`/`disabled` lists are explicit and the file never carries `checks: %{}`.
6. **`max_length: 98`.** The contract's number, and the length `mix format` and the rest of the
   tree use; it adds exactly 32 findings, visible in the baseline so the burn-down cannot mistake
   them for noise.
7. **dependabot without update groups.** Each bump stays one reviewable PR; the weekly cadence and
   `open-pull-requests-limit: 5` bound the noise. One npm entry for the whole pnpm workspace
   (single root `pnpm-lock.yaml`).
8. **`docs/TODO.md` one-sentence amendment (scope judgment).** The contract permits TODO edits when
   an entry closes; nothing closes here, but the actionlint entry carries the sentence "No CI job
   invokes `actionlint` (a repository search for the name matches `docs/` only)", which this same
   increment falsifies. Shipping a knowingly false sentence in the file the repository audits for
   rot was the worse option, so the sentence was replaced with the measured state (advisory hosted
   actionlint with shellcheck off; the shellcheck-*enabled* shape still runs nowhere) and the entry
   stays open. The alternative — leaving it for the next TODO-touching slice — is recorded here so
   the verify stage can revert one sentence instead of discovering the discrepancy itself.

## Evidence plan (gates actually run, in this order)

1. `golangci-lint config verify` (daemon) — config validity.
2. `GOOS=linux golangci-lint run` and `GOOS=windows golangci-lint run`, same flags as the steps —
   the two metrics, and the area-scoped pair that pins config fidelity.
3. `mix credo --strict` through the portable Elixir toolchain (a scratch copy of `control/` that
   adds only the W1-02 `:credo` dep, so the analysed bytes are the worktree's) — the strict
   baseline, the `checks: %{}` mutation, the parse-timeout experiment, and the extracted CI step
   script run unchanged.
4. `ruff check scripts/ci` and `ruff format --check scripts/ci` with the pinned 0.17.6 binary.
5. `shellcheck --severity=warning scripts/ci/*.sh`; and the extracted CI step script run unchanged.
6. The extracted CI step scripts, run locally where this host can execute them: the Windows
   golangci-lint step end to end (download, digest, extract, analysis — exit 1 with `449 issues`),
   the shellcheck step (exit 1, `shellcheck_findings=10`), the ruff step's no-`pyproject.toml`
   guard branch (note, exit 0), the Credo step's no-`:credo` guard branch in the worktree (note,
   exit 0) and its real run in the scratch copy (exit 30, `credo_strict_issue_total=528`), and the
   Hex step (`hex_audit_advisories=1`, exit 1). The three steps that download Linux-only binaries
   (Linux golangci-lint, actionlint, ruff's real path) cannot be executed here; for those, the
   tool and flag shape was measured directly (see §"Measured baselines") and the digest plus
   archive layout were verified. `pnpm audit --prod` and `govulncheck ./...` were measured
   directly rather than through their step scripts.
7. `actionlint -shellcheck= -pyflakes=` over both workflows, with a mutation case (a broken `if:`)
   proving the oracle is not vacuous.
8. `python3 scripts/ci/test-ci-changed-path-classifier.py` and the other `ci.yml`-touching
   structure suites (`test-check-record-cleanliness.py`, `test-model-routing-evaluation.py`,
   `test-no-legacy-routes-audit.py`, `test-assert-folded-gates.py`). The last of those plants its
   fixtures through `shutil.which("bash")`, and on this host that resolves to the WSL launcher at
   `C:\Windows\system32\bash.exe`, which cannot read the Windows temporary paths the suite passes
   it: with the default `PATH` every test fails with exit 127 and `No such file or directory`
   (11 failures, exit 1 in this worktree; identically 11 failures, exit 1 on a pristine
   `git archive HEAD` tree, which is what makes it a host-resolution artifact rather than this
   increment's defect). With Git's bash (`C:\Program Files\Git\bin`) first on `PATH`, the same
   suite reports 6 tests OK, exit 0 — in this worktree (31.5 s) and on the pristine tree (27.0 s).
   CI runs the suite on `ubuntu-latest`, where no such ambiguity exists.
9. `git diff --check` (and a trailing-whitespace scan over the new untracked files, which
   `git diff --check` does not reach).
10. `shellcheck --shell=bash` over every new bash `run:` block, extracted from the workflow, so the
    step bodies themselves are linted.
11. The `legacy-route-audit` job's first step (`scripts/audit-no-legacy-routes.sh`) derives its
    target list from `git ls-files`, so the four new files and 349 added lines join its scope once
    tracked. Its own matcher (the script's `legacy_route_pattern`, read from the script) reports no
    match over those bytes, which is the state a passing audit needs; the exclusion-list arm is
    untouched.

Not runnable on this host (the draft-PR CI run is their oracle, per the contract): the Linux
runner's downloads and the `ubuntu-latest` shellcheck version, `pnpm audit --prod` inside the
`contracts` job's pnpm 12.17.0, and every job's actual conclusion. Nothing here is reported as
passed from inference.

## Residual risks and known limits

- **The advisory steps are red until their burn-downs land.** Expected reds on merge: golangci
  (1029/449), Credo (528), ruff (94 + 50 files), shellcheck (10), hex.audit (1). Expected greens:
  actionlint (0), govulncheck (0), pnpm audit (0). A red advisory step is the intended signal, and
  each step prints its count so the number, not the colour, is the evidence.
- **Pinned versions age.** The three digests and the two `go install` versions need updating with
  the tools; dependabot does not cover them (it covers the manifest lockfiles).
- **`shellcheck` is unpinned** (the image's build). The step prints `shellcheck --version`, so a
  drifting count is attributable; pinning a shellcheck release is not this slice's call.
- **Credo version drift**: the step runs the project's `~> 1.7` resolution, while the recorded
  baseline is 1.7.19. A future 1.7.x could move the count; the step prints the version it ran.
- **One local `test-model-routing-evaluation.py` run died mid-suite.** On this tree with the
  host's default `PATH` its run exits 0; one re-run (Git's `bash` first on `PATH`)
  ended after 50 of its 80 tests with exit `0xFFFFFFFF` and no traceback, i.e. the interpreter was
  terminated rather than failing an assertion. The suite spawns and kills children
  (`subprocess.Popen`, `taskkill /F /PID` against a recorded PID) and this host runs several
  unrelated agents concurrently, so an external or stray kill is the reading recorded here; the
  increment touches neither the suite nor its subjects, and CI runs it on `ubuntu-latest`.
- **The `checks: %{}` trap did not reproduce as the audit described** (474 here versus the audit's
  403 under an earlier Credo). The config is explicit in either reading, and the recorded
   arithmetic (the five checks summing to 71 = 474 − 403) explains the audit's number; the
  divergence is recorded rather than smoothed over.
- **`legacy-route-audit` gains ~20 s** and the `daemon` job two advisory passes; the `daemon`
  job has no job-level timeout, the two golangci steps are self-bounding.
