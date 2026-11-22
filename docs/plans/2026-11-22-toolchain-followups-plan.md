# toolchain-followups plan — the daemon Go image, the residual action majors and the flake record

Contract: `goal-toolchain-followups.md` (GDD, conductor-authored 2026-11-22, dispatched after PR #218
merged). Baseline: this worktree's HEAD `e07821057dab5e382aa9fcf4a53e964dc8622dcd`; `git rev-parse
origin/main` and `git merge-base origin/main HEAD` both return it, so the contract's dispatch gate
holds and every coordinate below is re-derived at that revision (contract Change 1).

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `docker/daemon.Dockerfile` | modified | `ARG GO_IMAGE=golang:1.27.0-bookworm` → `golang:1.27.4-bookworm`; the single `FROM ${GO_IMAGE}` consumer and every other line are untouched. |
| `scripts/ci/rootless-compose-native-acceptance.sh` | modified | the `compose` job's E2E container image `golang:1.27.0-bookworm` → `golang:1.27.4-bookworm` (Change 2's "any other file that states the current Go toolchain"; it is a live CI path, not a historical record). |
| `.github/workflows/ci.yml` | modified | 3 `actions/upload-artifact@v4` → `@v7`, 1 `actions/setup-node@v4` → `@v7`, 1 `pnpm/action-setup@v4` → `@v6`. No step, `with:` input, `if:` or `needs:` added, removed, renamed or reordered. |
| `scripts/ci/test-ci-changed-path-classifier.py` | modified | the three `COVERAGE_UPLOADS` literals' `uses:` line `actions/upload-artifact@v4` → `@v7`. Required co-change of the `ci.yml` bump: that test pins the coverage jobs' upload-step text and is run unconditionally by the `legacy-route-audit` job (`ci.yml:1782`). Every other token of the literals — `if: always()`, the artifact name and the report path — is unchanged, so no assertion is weakened. |
| `docs/TODO.md` | modified | the linux-v2 acceptance entry's precondition sentence `pinned tooling Go 1.27.0` → `Go 1.27.4`; a new section recording the `admission_deadline` flake (Change 5). |
| `docs/goals/0010-refactor-program.md` | modified | the "Go stdlib advisories" follow-up bullet's live version statement and its now-moved pointer. |
| `docs/plans/2026-11-22-toolchain-followups-plan.md` | new | this plan. |

## Pin inventory (before → after), re-derived at `e0782105`

### `ci.yml` residual action sites — 5 sites, as the contract's evidence bullet names them

| # | Site (job) | Before | After | Inputs in use |
| --- | --- | --- | --- | --- |
| 1 | `.github/workflows/ci.yml:892` (coverage-control) | `actions/upload-artifact@v4` | `@v7` | `name: control-coverage`, `path: control/cover`, `if-no-files-found: warn`, `retention-days: 7`, step `if: always()` |
| 2 | `.github/workflows/ci.yml:942` (coverage-daemon) | `actions/upload-artifact@v4` | `@v7` | `name: daemon-coverage`, `path: daemon/coverage.out`, `if-no-files-found: warn`, `retention-days: 7`, step `if: always()` |
| 3 | `.github/workflows/ci.yml:974` (coverage-frontend) | `actions/upload-artifact@v4` | `@v7` | `name: frontend-coverage`, `path: frontend/coverage`, `if-no-files-found: warn`, `retention-days: 7`, step `if: always()` |
| 4 | `.github/workflows/ci.yml:957` (coverage-frontend) | `pnpm/action-setup@v4` | `@v6` | `version: 12.17.0` |
| 5 | `.github/workflows/ci.yml:960` (coverage-frontend) | `actions/setup-node@v4` | `@v7` | `node-version: 24`, `cache: pnpm` |

The other 13 `upload-artifact`, 14 `checkout`, 7 `setup-go`, 4 `download-artifact`, 2 `pnpm/action-setup`,
2 `setup-node` and 5 `erlef/setup-beam` sites in `ci.yml`, and the whole acceptance workflow, already
carry the majors this slice targets (`git grep -oE 'uses: [A-Za-z0-9._/-]+@[A-Za-z0-9._-]+'` over both
workflows). Floating major tags as resolved on 2026-11-22: `actions/upload-artifact@v7` → `v7.0.2`
(2026-10-07), `actions/setup-node@v7` → `v7.1.0` (2026-10-08), `pnpm/action-setup@v6` → `v6.1.1`
(2026-11-03); those three tags are each repository's highest published major, so "current major" is
unambiguous. The repository's convention is the floating major tag (Dependabot's `github-actions`
ecosystem in `.github/dependabot.yml` maintains it), so the bump stays at the major tag.

### Go image pins

| # | Site | Before | After | Why |
| --- | --- | --- | --- | --- |
| 1 | `docker/daemon.Dockerfile:3` | `golang:1.27.0-bookworm` | `golang:1.27.4-bookworm` | Contract Change 2. Built from `compose.yaml:56`; the `compose` job runs `docker compose up --build --detach` with no service list (`ci.yml:2487`), so hosted CI builds this image and is the behavioural oracle. |
| 2 | `scripts/ci/rootless-compose-native-acceptance.sh:473` | `golang:1.27.0-bookworm` | `golang:1.27.4-bookworm` | Change 2's "any other file that pins a Go image"; live path: the `compose` job runs the script at `ci.yml:2700` and the image runs the rootless Compose refusal E2E (`go test ./e2e -run ...`). The daemon binary itself is built by the runner's pinned `go-version: 1.27.4` (the `compose` job's `setup-go` pin at `ci.yml:2411-2413`; the build runs at `scripts/ci/rootless-compose-native-acceptance.sh:125-126`), so the container only provides the test toolchain; keeping it at 1.27.4 removes the last old-stdlib execution surface in these workflows. |

**Tag existence and digest** (recorded before editing): `golang:1.27.4-bookworm` exists — Docker Hub
tag API `https://hub.docker.com/v2/repositories/library/golang/tags/1.27.4-bookworm`, `last_updated`
`2026-11-10T18:34:06Z`. Index digest `sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea`;
`linux/amd64` platform digest `sha256:fb7cceb528b47141eccd9b60bf3cbd27d5ef5f8cf5b7d338bec866da495b50dc`,
confirmed independently by `docker manifest inspect golang:1.27.4-bookworm` (client-side registry
query; the manifest list also carries `linux/arm64/v8`, `linux/arm/v7`, `linux/386` and `linux/ppc64le`).
The version the container provides is go1.27.4, the patch the `daemon` job's `govulncheck` step is
green at (PR #218 run `44959141686`).

### `Change 2` census: every `git grep -n -E '1\.27\.[0-3]'` hit, classified

79 lines in 39 files at `e0782105`. Four of the hits are live current-toolchain statements (the two Go
image pins of the table above and the two documentation statements of the first table rows below);
every other hit is a historical record and keeps its bytes:

| Class | Files | Lines | Disposition |
| --- | --- | --- | --- |
| Live Go image pins | `docker/daemon.Dockerfile:3`, `scripts/ci/rootless-compose-native-acceptance.sh:473` | 2 | updated |
| Live documentation statements | `docs/TODO.md:249` (linux-v2 acceptance "pinned tooling"), `docs/goals/0010-refactor-program.md:640` ("CI pins `go-version: 1.27.0`") | 2 | updated (contract Change 3) |
| Dated evidence | `docs/evidence/goal-0006-*`, `goal-0006b-*`, `goal-0009-*`, `rf-w3-07-*`, `rf-w3-09-*`, `rf-w4-05-*`, `test-ablation-*` (10 files, 20 lines); `daemon/internal/harness/testdata/**` (8 files, 8 lines) | 28 | untouched: observation records of the toolchain measured then |
| Archives | `docs/archive/CLOSED-TODO.md` (12), `docs/goals/archive/0005-REMEDIATION_PLAN.md:78`, `docs/goals/archive/0006j-…:42`, `docs/goals/archive/0006k-…:45` | 15 | untouched: closed entries and archived goals |
| Merged-slice plans | `docs/plans/2026-11-2{0,1,2}-*-plan.md` (12 files, 31 lines; `2026-11-22-ci-toolchain-bump-plan.md` alone holds 14 and `2026-11-21-ci-quality-plane-plan.md` 6) | 31 | untouched: each plan is bound to its own dispatch revision, and `docs/plans/2026-11-22-ci-toolchain-bump-plan.md` records 1.27.0 as the pre-bump "before" by design |
| Sanitizer fixture | `scripts/ci/test-linux-v2-evidence.py:110` (`"go_version": "go1.27.0"` inside a synthetic receipt) | 1 | untouched: opaque fixture data for the evidence-sanitizer test, not a pin; nothing validates it against the CI toolchain |

`daemon/go.mod` keeps `go 1.27` and gains no `toolchain` line (`git grep -n -E '^toolchain '` → no
hits); contract clause 4 excludes `go.mod`.

## Breaking-change table (contract Change 4)

Sources read before editing: `gh api repos/<owner>/<name>/releases` for `actions/upload-artifact`
(v4.6.2, v5.0.0, v6.0.0, v7.0.0-v7.0.2), `actions/setup-node` (v4.4.0, v5.0.0, v6.0.0-v6.5.0, v7.0.0,
v7.1.0) and `pnpm/action-setup` (v4.3.0, v4.4.0, v5.0.0, v6.0.0-v6.1.1) — every
release from each current tag to the target tag; the `action.yml` input keys and `runs.using` at
`v4.6.2`/`v4.4.0` against `v7.0.2`/`v7.1.0`/`v6.1.1` and at the intermediate `pnpm/action-setup` tags
`v6.0.1`/`v6.0.10`; and `actions/setup-node`'s v7 README caching section.

| Action | Major | Documented breaking change | This repo's usage |
| --- | --- | --- | --- |
| upload-artifact | v5.0.0 | Node 24 support with `@actions/artifact` v4.0.0; the release body calls the Node 24 flip breaking (PR #734 and the "BREAKING CHANGE" header) | Not affected in behaviour: hosted `ubuntu-latest`, no input change (v4.6.2 and v5.0.0 input keys are identical). |
| upload-artifact | v6.0.0 | `runs.using` flips node20 → node24 (`action.yml` diff confirms); runner ≥ 2.327.1 (#719, #744, #745) | Not affected: GitHub-hosted runners are far past 2.327.1; the runtime flip is the purpose of the bump. |
| upload-artifact | v7.0.0 | new `archive` input for direct single-file uploads — when set to `false` the `name` input is ignored and the action fails unless the glob resolves to exactly one file; module moved to ESM (#762, #764) | Not affected: no site sets `archive` (`git grep -n 'archive:' -- .github` → no hits), so every artifact keeps the zip upload v4 produced. The single-file site (#2, `daemon/coverage.out`) is unaffected because the failure mode exists only with `archive: false`. |
| upload-artifact | v7.0.1 / v7.0.2 | `typespec/ts-http-runtime` 0.3.5; artifact-download 429 retry with `Retry-After`, `@actions/artifact` 6.3.1 | Not affected; no input change. |
| setup-node | v5.0.0 | automatic caching when `package.json` carries a valid `packageManager` field, with the new `package-manager-cache: false` opt-out (#1348); node24 runtime, runner ≥ 2.327.1 (#1325) | Not affected: the site already passes an explicit `cache: pnpm`, and the v7.1.0 README scopes automatic caching to npm and to a tree whose `packageManager` or `devEngines.packageManager` field names npm with no explicit cache input provided. This repository's root `package.json` sets `packageManager: pnpm@12.17.0`, which never triggers the npm-only automatic path; the runtime flip is the purpose of the bump. |
| setup-node | v6.0.0 | automatic caching limited to npm (#1374) | Not affected: narrows only the automatic default, and all four setup-node sites are explicit (`cache: pnpm` at the sites that cache). |
| setup-node | v6.1.0 | `always-auth` configuration handling removed (#1436); the input disappears from `action.yml` (confirmed by the v4.4.0 → v7.1.0 input diff) | Not affected: no workflow sets `always-auth` or `registry-url` (`git grep -n -e 'always-auth' -e 'registry-url' -- .github` → no hits), so no `.npmrc` auth line is written at all. |
| setup-node | v6.2.0-v6.5.0 | `@actions/cache` 5.x bumps, `devEngines` parsing for `node-version-file`, `uuid` → `crypto.randomUUID()`, mirror bearer-token handling | Not affected: no site sets `node-version-file`, `mirror`, `mirror-token` or `token`; the cache key source is unchanged for an explicit `cache: pnpm` + lockfile hash. |
| setup-node | v7.0.0 | ESM migration (#1574); new `cache-primary-key`/`cache-matched-key` outputs; dummy `NODE_AUTH_TOKEN` export removed (#1558); `mirrorToken` only when provided (#1548) | Not affected: no workflow reads setup-node outputs (`steps.*.outputs` uses in these two files are `steps.paths` and `steps.windows_postgres` only), and no site sets `registry-url`, `mirror-token` or `token`. |
| setup-node | v7.1.0 | `mise.toml` support (#1421) and absolute `node-version-file` paths (#1633), both reachable only through `node-version-file`; Node version validation and manifest retry (#1580); `@actions/cache` 6.2.0 | Not affected: the site passes a concrete `node-version: 24` and no `node-version-file`. The two other `setup-node@v7` sites in `ci.yml` already run this exact input pair and were green on PR #218's hosted run; the acceptance workflow's site (`.github/workflows/linux-native-v2-acceptance.yml:166-169`) is configured identically but that workflow is `workflow_dispatch`-only and its single run (`44487017117`, 2026-11-19) was cancelled, so it is not yet exercised. |
| pnpm/action-setup | v4.3.0 / v4.4.0 | store caching (#188); "Updated the action to use Node.js 24" (v4.4.0) | Not affected; no input change, and the floating `@v4` tag already resolves to the node24 v4.4.0. |
| pnpm/action-setup | v5.0.0 | "Updated the action to use Node.js 24" (runtime only) | Not affected: `runs.using` is already `node24` at v4.4.0. |
| pnpm/action-setup | v6.0.0 / v6.0.1 | pnpm v11 support; bootstrap moved to `11.0.0-rc.2`, with a lockfile two-document caveat unless the manager version comes from `devEngines.packageManager` | Not affected: the site pins `version: 12.17.0` explicitly and CI installs with `--frozen-lockfile` against the committed lockfile. |
| pnpm/action-setup | v6.0.2-v6.0.10 | bootstrap and self-update fixes ("pnpm self-update binary shadowed by bootstrap on `PATH`", npm co-located with the action's node binary, `PATH` append order, `bin_dest` after self-update, `devEngines.packageManager.onFail=error`, post-state restore, self-update bootstrap to the `packageManager`-pinned version, `patchPnpmEnv` dropped for standalone+self-update on Windows); bundled bootstrap updates to pnpm 11.0.0-rc.5, 11.1.1, 11.7.0 and 11.19.0; cache restore keys; README docs | Not affected: the floating `@v6` tag resolves to `v6.1.1` (`refs/tags/v6` and `refs/tags/v6.1.1` both peel to commit `73e46992`), so no v6.0.2-v6.0.10 dist ever runs at this site, and the input keys are the same seven (`version`, `dest`, `run_install`, `cache`, `cache_dependency_path`, `package_json_file`, `standalone`) at v4.4.0, v6.0.1, v6.0.10 and v6.1.1. |
| pnpm/action-setup | v6.1.0 / v6.1.1 | pnpm v12 support (#288); native bootstrap refresh, pnpm 11 bootstrap 11.28.10, missing-cache-directory skip, pinned-action updates | Not affected: this is the major that makes v6 the correct target for the site's pnpm 12.17.0; the input keys are byte-identical between v4.4.0 and v6.1.1 (confirmed by the `action.yml` diff). |

**Input-adjustment verdict: no site needs an input change.** Contract Change 4's "keep
`node-version`/pnpm version values unchanged" therefore holds as a pure version-string diff: `24` and
`12.17.0` keep their sites, and the only added/removed inputs across the three bumps
(`archive`, `package-manager-cache`, `always-auth`) are unset exactly as the unaffected verdicts
above require.

## The `admission_deadline` flake record (contract Change 5)

Primary observation (PR #217 review, round-1 note N1; the review's own record): the subtest failed
twice on the base tree `394859a9` — once under concurrent load, once alone — with
`attachment requests=[] … context deadline exceeded` (the review elides the middle of the message),
then passed 2/2 alone on that base and 3/3 on the PR head. Coordinates re-derived here:

- Test: `TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline`;
  package `daemon/internal/app`; file `daemon/internal/app/omo_attach_ack_test.go`, four-mode parent
  at `:602`, `admission_deadline` arm at `:616-624`, the request-context wait at `:626`, the failing
  `assertRequests` call at `:629` (the review's cited line).
- The file is byte-identical between `394859a9` and `e0782105` (`git diff --stat 394859a9 e0782105
  -- daemon/internal/app/omo_attach_ack_test.go` → empty) and last changed by `8ded7715`, so the
  observation binds the current bytes.
- The failure text: `assertRequests` (function `:294`, message `:307`) is the failing assertion; its message template is
  `attachment requests=%v, want %v (PUT error=%v local_state=%s terminal_state=%s failure=%s
  native_calls=%v journal_error=%v)`. Every recorded instance of this arm's failure carries an empty
  request list and `PUT error=<nil>`, and shows the deadline inside the `failure` payload in one of
  two shapes: the attach GET leg hitting it —
  `failure={"error":"attach native Goal session: read Goal session attachment: perform request: Get
  \"…/session?…\": context deadline exceeded","reason":"unknown_outcome","stage":"goal_admission",…}`
  with `native_calls=[start details open details close wait]`
  (`<GDD-STATE>/refactor/tools/w3-02-response-table/host-callers-suite.txt:18-19`) — or the production
  admission check firing before native launch — `failure={"error":"Goal admission deadline has
  elapsed before native launch","reason":"unknown_outcome","stage":"goal_admission","summary":"daemon
  failed during goal_admission"}` with `native_calls=[]`
  (`daemon/internal/app/goal_admission.go:413`; shape recorded in full at
  `<GDD-STATE>/tmp/rf-w2-10/omo-attach-final.txt:2-3`, and its `failure` message also at
  `docs/evidence/rf-w4-03-runner-decomposition-2026-11-21.md:490-492`).
- The sibling `caller_deadline` arm fails on its GET leg
  (`<GDD-STATE>/refactor/tools/w2-11-typed-errors/daemon-full.txt:18-19`).
- Cause unknown, no fix in this slice. The refactor program's `rf-w4-13` contract (E5) marks tests
  whose duration is bounded by a production limit as `out of scope (production-bound)` and names this
  test's `request_deadline` arm (`controlRequestLimit = 15 * time.Second`,
  `daemon/internal/app/app.go:38`); whether the `admission_deadline` arm gets its own treatment is an
  owner decision, not this slice's.

`git grep -n 'admission_deadline' e0782105 -- docs/TODO.md` → no hits: the entry this slice adds is
`docs/TODO.md`'s first record of the flake. Earlier records of the same arm live in merged artifacts:
`docs/plans/2026-11-21-rf-w2-10-callback-dedup-plan.md:168-173` (the parent test, loaded host),
`docs/plans/2026-11-21-rf-w4-02-platform-sessions-plan.md:533` (re-observed),
`docs/evidence/rf-w4-03-runner-decomposition-2026-11-21.md:483-491` (starvation analysis).

## Decision recorded: the 0010 advisories bullet is rewritten, not one-token swapped

Change 3 asks for the "current pinned tooling" statements in `docs/TODO.md` and
`docs/goals/0010-refactor-program.md` to say Go 1.27.4. `docs/TODO.md:249` takes that as a literal
three-token swap (the acceptance workflow's `go-version` is `'1.27.4'`). The 0010 bullet cannot: its
sentence is "the `govulncheck` step reports 12 Go standard-library vulnerabilities because CI pins
`go-version: 1.27.0`" — swapping only the version produces the false claim that a 1.27.4 pin reports
12 vulnerabilities, and its `docs/TODO.md` pointer resolves to a section that has since moved to
`docs/archive/CLOSED-TODO.md`. Options considered: (a) rewrite the bullet in the past tense, naming
the current pin and re-pointing the closure record; (b) leave the bullet to the owner as #218's plan
did. (a) is chosen: it is the smallest truthful text that removes the stale current-pin claim while
staying inside the owned scope ("the Go version statement only"), and the closure it cites is
verifiable in `docs/archive/CLOSED-TODO.md` (hosted run `44959141686`, `govulncheck_vulnerabilities=0`).
A stricter reading of Change 3 that wanted the literal token changed would have written a false
sentence, so it is not implemented.

## Edge-case semantics the contract fixes

1. Only the five `uses:` values in `ci.yml` change: no step key, `with:` key, `if:`, `needs:`, `env`,
   comment or blank line is added or removed, so the judging diff is checkable hunk by hunk and the
   pin gate below fails if an old value survives.
2. The classifier test's literals change on their `uses:` line only; `if: always()`, the artifact
   names and the report paths — the parts W2-17's contract clause 1 pins — keep their exact bytes, so
   `check_coverage_artifacts` still reddens on a dropped `if: always()`, a renamed artifact or a moved
   report path.
3. `docker/daemon.Dockerfile`'s `ARG GO_IMAGE` keeps its default-value shape (`golang:<patch>-bookworm`,
   no digest pin): the repository's convention is the moving patch tag, and pinning a digest would be
   a new convention this slice does not own.
4. Both workflows' triggers, `env`, services and defaults are untouched; the coverage-frontend job
   stays `continue-on-error: true` and its upload steps stay `if: always()`.
5. Comment policy: this slice writes no comments in the workflow, the Dockerfile, the shell script or
   the classifier test; the upstream breaking-change analysis lives here, and the flake record lives
   in `docs/TODO.md`.
6. `docs/TODO.md`'s linux-v2 acceptance entry keeps its structure: only the three-token version
   statement changes, and the entry stays open (its dispatch is still pending).

## Evidence plan

Foreground, on the frozen tree, recorded with real exit codes:

| # | Command | Expectation |
| --- | --- | --- |
| 1 | `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml` (1.7.12, the version earlier slices used) | exit 0, no output |
| 2 | `git grep -n -e 'upload-artifact@' -e 'setup-node@' -e 'pnpm/action-setup@' -e 'GO_IMAGE' -- .github docker` (the contract's gate, respelled with `-e` because a raw alternation pipe inside this cell is either a table separator or, escaped, a literal pipe to `-E`) plus the repo-wide `git grep -n -e 'upload-artifact@v' -e 'setup-node@v' -e 'action-setup@v' -- . ':(exclude)docs'` and `git grep -n 'golang:[0-9]' -- . ':(exclude)docs' ':(exclude)daemon/internal/harness/testdata'` | only the new values in `.github`/`docker`: `@v7`/`@v7`/`@v6` and `golang:1.27.4-bookworm`; the repo-wide scan leaves no `@v4` literal and no old Go image outside docs |
| 3 | `python3 scripts/ci/test-ci-changed-path-classifier.py` | exit 0 (the fixture test that pins the three coverage upload steps) |
| 4 | `git diff --check` | exit 0 |
| 5 | host-path scan: the whole increment (`git diff HEAD` plus the untracked plan file) grepped for the profile-directory segment the program rule names (`<profile-segment>/`, the phrasing `docs/plans/2026-11-22-ci-toolchain-bump-plan.md` uses) | empty (program rule) |
| 6 | `git grep -n -E '1\.27\.[0-3]'` | the four live statements above disappear: the census-class survivors are 75 lines in 35 files, and this plan's own 10 before-value quotes make the full-tree command return 85 = 75 + 10; the gate-2 form `git grep -n 'golang:[0-9]' -- . ':(exclude)docs' ':(exclude)daemon/internal/harness/testdata'` returns only the two bumped `golang:1.27.4-bookworm` pins — no old Go image |
| 7 | `docker build -f docker/daemon.Dockerfile .` (engine 29.9.3 reachable on this host) | exit 0; recorded as not run if the engine is unavailable (hosted `compose` job is then the oracle) |
| 8 | `git diff origin/main -- .github docker scripts/ci` | every hunk is a value change in the five action sites, the two Go image sites and the three classifier literals |

Hosted CI on the PR (every job green, including the `compose` job that builds
`docker/daemon.Dockerfile` and runs the rootless script's container, and `legacy-route-audit` that
runs the classifier test) is the behavioural oracle and belongs to the conductor: this stage must not
push, so it cannot observe it. The local gates prove the values, the Dockerfile build and the fixture
test, not the hosted runs.

## Out of scope, with reasons

| Item | Reason |
| --- | --- |
| `erlef/setup-beam@v1` (5 sites in `ci.yml`, 1 in the acceptance workflow) | Contract clause 4 ("other actions"); its own major is unchanged and setup-beam v1 is current. |
| Elixir/OTP/Node/pnpm version values, runner images, `daemon/go.mod` | Contract clause 4, verbatim. |
| Fixing the `admission_deadline` flake | Contract clause 4 ("fixing the flaky test" is out of scope) and Change 5's "no fix in this slice". |
| `docs/plans/2026-11-21-ci-quality-plane-plan.md:131,133-134,146-147,269` ("in CI the step runs under the pinned `go-version: 1.27.0`") | The plan of a merged slice, bound to its own dispatch revision; the census classifies merged plans as historical records, and #218's own plan keeps 1.27.0 as its pre-bump "before" in the same way. |
| `docs/archive/CLOSED-TODO.md`'s toolchain entry, `docs/goals/archive/**`, `docs/evidence/**`, `daemon/internal/harness/testdata/**`, `scripts/ci/test-linux-v2-evidence.py:110` | Historical records, archived goals, dated evidence and an opaque fixture (contract Change 2's second sentence). |
| Dependabot #168/#171 closure | Already recorded as superseded by #218 in `docs/archive/CLOSED-TODO.md`; no PR is open against this tree for them. |
| Pinning the Go image by digest | Needs a repository-wide convention decision; the contract asks for the patch tag and the digest is recorded here instead. |

## Risks and limits

- `docker/daemon.Dockerfile` is built only by the `compose` job (`ci.yml:2487`, `up --build` with no
  service list) and by local `docker compose up`; the acceptance workflow does not build it. The local
  build gate is therefore the only pre-PR proof, and the hosted `compose` job is the oracle.
- The rootless script's container image is exercised only on hosted runners (it requires
  `GITHUB_WORKSPACE`, `RUNNER_TEMP`, `sudo`, a rootless Docker environment). This stage can prove the
  tag exists and the digest matches, not that the container run stays green; the `compose` job is the
  oracle for that too. The image only supplies the `go` binary for `go test ./e2e` (the daemon binary
  is built with the runner's 1.27.4), so the exposure is confined to the test toolchain.
- `upload-artifact@v7`'s `archive` default keeps the artifacts zipped, which is what the
  `download-artifact@v8` consumers of `portal-assets` and `control-release-linux` rely on; the three
  coverage artifacts are upload-only (`git grep -n -E 'control-coverage|daemon-coverage|frontend-coverage' -- . ':(exclude)docs'` matches only the three upload steps in `ci.yml` and the classifier test).
- The classifier test's literals are a text-level pin of `ci.yml`; if a later slice renames the upload
  step's version again, the same co-change is required. That coupling is pre-existing and is why the
  change is minimal (one token per literal) rather than a rewrite of the pin.
