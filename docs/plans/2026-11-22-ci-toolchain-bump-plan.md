# ci-toolchain-bump plan — Go 1.27.4 pins and the current action majors in CI

Contract: `goal-ci-toolchain-bump.md` (GDD, conductor-authored 2026-11-22, dispatched after PR #212
merged). Baseline: this worktree's HEAD `12de9428a4db5cbe67461ed8511c08978ea3364e`; `git rev-parse
origin/main` and `git merge-base origin/main HEAD` both return it, so the contract's dispatch gate
holds and every coordinate below is re-derived at that revision (contract Change 1).

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `.github/workflows/ci.yml` | modified | 7 `go-version` values `1.27.0` → `1.27.4` (each site's quoting kept); 14 `actions/checkout@v4` → `@v7`; 7 `actions/setup-go@v5` → `@v7`; 4 `actions/download-artifact@v4` → `@v8`. No step, `with:` input, `if:` or `needs:` added, removed, renamed or reordered. |
| `.github/workflows/linux-native-v2-acceptance.yml` | modified | 1 quoted `go-version`; 2 `checkout`; 1 `setup-go`; 1 `download-artifact` — same three actions, same rule. |
| `docs/TODO.md` | modified | the `## CI Go toolchain: bump past the stdlib advisories` section (heading plus its one entry) moves out; nothing else changes. |
| `docs/archive/CLOSED-TODO.md` | modified | that section, with the closure statement; one provenance line in the file header, following the file's own convention. |
| `docs/plans/2026-11-22-ci-toolchain-bump-plan.md` | new | this plan. |

`daemon/go.mod` is deliberately untouched (contract Change 4): it keeps `go 1.27` and gains no
`toolchain` line (`git grep -n -E '^toolchain ' -- '*.mod' '*.work'` → no hits); CI pins the patch.

## Pin inventory (before → after), re-derived at `12de9428`

### `go-version` — 8 sites, 7 + 1 as the contract's evidence bullet states

| # | Site (job) | Before | After |
| --- | --- | --- | --- |
| 1 | `.github/workflows/ci.yml:208` (control) | `1.27.0` | `1.27.4` |
| 2 | `.github/workflows/ci.yml:912` (coverage-daemon) | `1.27.0` | `1.27.4` |
| 3 | `.github/workflows/ci.yml:1060` (pi-control-e2e-windows) | `1.27.0` | `1.27.4` |
| 4 | `.github/workflows/ci.yml:1504` (daemon) | `1.27.0` | `1.27.4` |
| 5 | `.github/workflows/ci.yml:1933` (daemon-windows) | `1.27.0` | `1.27.4` |
| 6 | `.github/workflows/ci.yml:2132` (integration) | `1.27.0` | `1.27.4` |
| 7 | `.github/workflows/ci.yml:2413` (compose) | `'1.27.0'` | `'1.27.4'` |
| 8 | `.github/workflows/linux-native-v2-acceptance.yml:67` (linux-native-v2-acceptance) | `'1.27.0'` | `'1.27.4'` |

Quoting is per site: sites 1-6 unquoted, sites 7-8 single-quoted, exactly as they are today. All eight
sites also pass `cache-dependency-path: daemon/go.sum`.

### `actions/checkout@v4` → `@v7` — 16 sites (14 + 2)

| # | Site (job) | Inputs in use | After |
| --- | --- | --- | --- |
| 1 | `.github/workflows/ci.yml:45` (changes) | `if: github.event_name == 'pull_request'`, `fetch-depth: 2`, `persist-credentials: false` | `@v7` |
| 2 | `.github/workflows/ci.yml:76` (record-cleanliness) | `fetch-depth: 0`, `persist-credentials: false` | `@v7` |
| 3 | `.github/workflows/ci.yml:102` (citation-existence) | `fetch-depth: 0`, `persist-credentials: false` | `@v7` |
| 4 | `.github/workflows/ci.yml:116` (contracts) | none | `@v7` |
| 5 | `.github/workflows/ci.yml:197` (control) | none | `@v7` |
| 6 | `.github/workflows/ci.yml:872` (coverage-control) | none | `@v7` |
| 7 | `.github/workflows/ci.yml:909` (coverage-daemon) | none | `@v7` |
| 8 | `.github/workflows/ci.yml:956` (coverage-frontend) | none | `@v7` |
| 9 | `.github/workflows/ci.yml:995` (pi-control-e2e-windows) | none | `@v7` |
| 10 | `.github/workflows/ci.yml:1501` (daemon) | none | `@v7` |
| 11 | `.github/workflows/ci.yml:1772` (legacy-route-audit) | none | `@v7` |
| 12 | `.github/workflows/ci.yml:1930` (daemon-windows) | none | `@v7` |
| 13 | `.github/workflows/ci.yml:2119` (integration) | none | `@v7` |
| 14 | `.github/workflows/ci.yml:2410` (compose) | none | `@v7` |
| 15 | `.github/workflows/linux-native-v2-acceptance.yml:51` (linux-native-v2-acceptance) | `persist-credentials: false` | `@v7` |
| 16 | `.github/workflows/linux-native-v2-acceptance.yml:162` (contracts) | none | `@v7` |

Twelve sites keep the input defaults `fetch-depth: 1`, `persist-credentials: true`; four set
`persist-credentials: false` (1-3, 15). No checkout site sets `ref`, `fetch-tags`, `lfs`,
`sparse-checkout` or `submodules` (`git grep -n -E '(ref|fetch-tags|lfs|sparse-checkout|submodules):'
-- .github/workflows` → no hits). Every `path:` occurrence under `.github/workflows` belongs to a
non-checkout step: artifact `path:` inputs, setup-go `cache-dependency-path:` values and the local
`prepare-pi-release` `executable-path:`.

### `actions/setup-go@v5` → `@v7` — 8 sites (7 + 1)

`ci.yml:206` (control), `:910` (coverage-daemon), `:1058` (pi-control-e2e-windows), `:1502` (daemon),
`:1931` (daemon-windows), `:2130` (integration), `:2411` (compose),
`linux-native-v2-acceptance.yml:65`. Every site's inputs are `go-version` (row above) and
`cache-dependency-path: daemon/go.sum`; no site sets `go-version-file`, `check-latest`, `token`,
`cache`, `architecture`.

### `actions/download-artifact@v4` → `@v8` — 5 sites (4 + 1)

| # | Site (job) | Inputs in use |
| --- | --- | --- |
| 1 | `.github/workflows/ci.yml:198` (control) | `name: portal-assets`, `path: control/priv/static/portal` |
| 2 | `.github/workflows/ci.yml:683` (control) | `name: control-release-linux`, `path: ${{ runner.temp }}` |
| 3 | `.github/workflows/ci.yml:875` (coverage-control) | `name: portal-assets`, `path: control/priv/static/portal` |
| 4 | `.github/workflows/ci.yml:2122` (integration) | `name: control-release-linux`, `path: ${{ runner.temp }}` |
| 5 | `.github/workflows/linux-native-v2-acceptance.yml:54` (linux-native-v2-acceptance) | `name: portal-assets`, `path: control/priv/static/portal` |

No site uses `artifact-ids`, `pattern`, `merge-multiple`, `repository`, `run-id` or `github-token`.
The upstream producers are `actions/upload-artifact@v7` (`ci.yml:149` uploads the directory
`control/priv/static/portal` as `portal-assets`; `ci.yml:163` uploads the single file
`${{ runner.temp }}/control-release-linux.tar.gz`; `linux-native-v2-acceptance.yml:178` uploads
`portal-assets` again) and none of them sets `archive:` (`git grep -n 'archive:' -- .github` → no
hits), so both artifacts stay zips.

Floating major tags as resolved on 2026-11-22: `actions/checkout@v7` →
`3d3c42e5aac5ba805825da76410c181273ba90b1` (v7.0.1), `actions/setup-go@v7` →
`d8cbb8045d7f43706b9177cceb36c1f91a6644d5` (v7.1.0), `actions/download-artifact@v8` →
`9000827ccba6bdab643e8b6fd33ac0654aef8333` (v8.0.2). The repository's convention is the floating
major tag (Dependabot's `github-actions` ecosystem in `.github/dependabot.yml` maintains it), so the
bump stays at the major tag.

### Two corrections to the contract's evidence bullet (recorded, not silently fixed)

- The bullet's "`actions/upload-artifact` is already `@v7`" holds for nine jobs (seven in `ci.yml`,
  two in the acceptance workflow; 16 `uses:` sites) but not all:
  `ci.yml:892`, `:942` and `:974` (the three coverage jobs' report uploads) are still
  `actions/upload-artifact@v4`, and `ci.yml:960` is `actions/setup-node@v4` beside
  `ci.yml:957` `pnpm/action-setup@v4`. Change 3 names three actions and clause 4 excludes other
  actions, so none of these five sites is touched; they are the residual Node 20 deprecation surface
  in these two files (see Risks).

## Breaking-change table (contract Change 3)

Sources read before editing: `gh api repos/actions/<name>/releases` for `checkout` (v4.3.1, v5.0.0,
v5.0.1, v6.0.0-v6.0.3, v7.0.0, v7.0.1), `setup-go` (v5.6.0, v6.0.0-v6.5.0, v7.0.0, v7.1.0) and
`download-artifact` (v4.3.0, v5.0.0-v8.0.2); each new major's `action.yml` diff against the old one
(input keys and `runs.using`); setup-go's own "Breaking changes in V6" section in its README at
v7.1.0; the release PRs (`checkout` #2286/#2454, `setup-go` #460/#624/#674/#691/#705,
`download-artifact` #416/#461); and `upload-artifact` v5-v7 for the producer side of the five
download sites.

| Action | Major | Documented breaking change | This repo's usage |
| --- | --- | --- | --- |
| checkout | v5.0.0 | `runs.using` node20 → node24; runner ≥ 2.327.1 | Not affected in behaviour. `runs-on: ubuntu-latest` on GitHub-hosted runners is far past 2.327.1; the change removes node20 from these steps. |
| checkout | v6.0.0 | `persist-credentials: true` (the default) now persists the token to a separate credentials config file under `$RUNNER_TEMP` (outside the workspace) instead of `.git/config` (PR #2286) | Not affected. No step of either workflow, and no script they invoke, runs a git network command: `git grep -n -E 'git (fetch\|push\|clone\|ls-remote)' -- .github` → no hits, and the two hits under `scripts/` are a local `--no-hardlinks` clone into a sandbox fixture (`scripts/ci/linux-v2-native-acceptance.sh:796`) and an evaluation harness clone (`scripts/ci/model-routing-evaluation.py:3616`). Nothing reads `.git/config` for credentials. The four sites that set `persist-credentials: false` are unaffected by construction; the twelve default sites keep the token, in a new file location nothing depends on. |
| checkout | v6.0.1-v6.0.3 | worktree support for `persist-credentials` includeIf (#2327); tag handling fix for `fetch-tags` (#2356); SHA-256 repository init (#2439) | Not affected. No `fetch-tags` input (`git grep` → no hits); this repository is SHA-1. |
| checkout | v7.0.0 | refuses to check out fork-PR code when the trigger is `pull_request_target` or `workflow_run` unless the new `allow-unsafe-pr-checkout: true` is set (PR #2454); module migrated to ESM; error wording | Not affected. The triggers in these two files are `pull_request`, `push` (main, tags), `workflow_dispatch` (and `workflow_dispatch` only in the acceptance workflow); no `pull_request_target`/`workflow_run` anywhere (`git grep` over `.github/workflows`). The new input stays unset (its default preserves the v4 behaviour for these triggers). |
| checkout | v7.0.1 | skip the unsafe-PR check when its input is default (#2518); trim only ASCII whitespace for `branch` (#2521); escape `--unset` values (#2530) | Not affected: the guarded triggers are not used, and no site passes `ref`/`branch`. |
| setup-go | v6.0.0 | (a) the release notes list the node20 → node24 runtime upgrade (#624), but the v6.0.0 tag still declares `runs.using: 'node20'` - the flip lands in `action.yml` at v6.2.0 (#691); (b) toolchain handling: the action resolves `toolchain`/`go` directives in `go.mod` and forces `GOTOOLCHAIN=local`, so `go` commands run the toolchain the action installed instead of auto-downloading a newer one (PR #460) | Not affected. (a) the runtime does not change at this tag, and the v6.2.0 flip carries the same runner argument as checkout v5. (b) directive resolution is entered only through `go-version-file`; all eight sites pass an explicit `go-version` (`git grep -n 'go-version-file' -- .github` → no hits). `GOTOOLCHAIN=local` is inert for this tree because `daemon/go.mod` says `go 1.27` with no `toolchain` line and the installed 1.27.4 satisfies it; under v5 the same module also resolved to the installed toolchain, so no observable change. |
| setup-go | v6.2.0 / v6.3.0 | node24 landed in `action.yml` in v6.2.0 (#691); v6.3.0 changes the *default* cache key source from `go.sum` to `go.mod` (PR #705; README "Cache key update") | Not affected. The runtime change is the same node20 → node24 migration; the cache change alters only the default, and all eight sites pass `cache-dependency-path: daemon/go.sum` explicitly, so the cache key keeps hashing `go.sum` exactly as under v5. |
| setup-go | v7.0.0 | module migrated to ESM; `@actions/cache` 6.2.0 (#763, #771). The v7 README states: "No changes to action inputs, outputs, or behavior." | Not affected. |
| setup-go | v7.1.0 | new outputs derived from the installed toolchain's `go env` (#791, #793), including `go-bin-path`; `go-version` accepts the native Go version syntax (e.g. `go1.27.4`) (#804); `go.mod` rc/beta versions accepted (#701) | Not affected: no workflow reads setup-go outputs (`steps.paths` and `steps.windows_postgres` are the only `steps.*.outputs` uses), all eight sites pass a concrete semver `go-version`, and no site sets `go-version-file`. |
| download-artifact | v5.0.0 | single-artifact downloads **by ID** (`artifact-ids`) change from `path/<name>/` to `path/` (PR #416); downloads by `name` are unchanged | Not affected: all five sites download by `name` and no site uses `artifact-ids` (`git grep` → no hits). |
| download-artifact | v6.0.0 | node24 support (`@actions/artifact` v4; `runs.using` still node20 at this tag) | Not affected. |
| download-artifact | v7.0.0 | default runtime now node24; runner ≥ 2.327.1 | Not affected (hosted runner). |
| download-artifact | v8.0.0 | (a) `digest-mismatch` defaults to `error`: a download whose digest check fails now fails the run instead of warning (PR #461); (b) files whose `Content-Type` says they are not zips are no longer force-unzipped, and `skip-decompress` was added (PR #460); ESM | (a) inherited by all five sites and left at the strict default deliberately: this is a security default, and setting `warn` would re-open silent corruption to keep a leniency no site relies on. (b) not affected: both producers use `actions/upload-artifact@v7` with the default `archive: true` (no `archive:` key anywhere), so every artifact is a zip and the extracted layout under `path/` matches v4's. |
| download-artifact | v8.0.1 / v8.0.2 | CJK artifact names (#471); HTTP 429 retry with `Retry-After`, `@actions/artifact` 6.3.1 (#492) | Not affected; no input change. |

**Input-adjustment verdict: no site needs an input change.** For download-artifact the used
semantics are `name` + `path` (`pattern`/`merge-multiple`/`artifact-ids` unused); for checkout they are
`fetch-depth` (2/0/default) and `persist-credentials` (default/`false`) with no `ref`, `fetch-tags` or
`lfs`; for setup-go they are `go-version` plus an explicit `cache-dependency-path`. Contract clause 3
therefore produces a pure version-string diff.

## Edge-case semantics the contract fixes

1. Quoting per `go-version` site is preserved (six unquoted, two single-quoted) — a YAML numeric-parse
   question is avoided the same way the current file avoids it.
2. Nothing but the four pin families changes: no step key, `with:` key, `if:`, `needs:`, `env`,
   comment or blank line is added or removed. The judging diff is checkable hunk by hunk, and the
   pin gate below fails if any old value survives.
3. Both workflows' *triggers* are untouched; the acceptance workflow stays
   `workflow_dispatch`-only.
4. `docs/TODO.md` loses exactly the section `## CI Go toolchain: bump past the stdlib advisories` and
   its one entry; `docs/archive/CLOSED-TODO.md` gains that section at the end of the file plus a
   provenance line in the header, matching the file's convention for moved blocks.
5. Comment policy: this slice writes no comments in either workflow. The upstream breaking-change
   analysis lives here, not beside the pins; the closure narrative lives in the CLOSED-TODO entry.

## Evidence plan

Foreground, on the frozen tree, recorded with real exit codes:

| # | Command | Expectation |
| --- | --- | --- |
| 1 | `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml .github/workflows/linux-native-v2-acceptance.yml` (1.7.12, the version earlier slices used) | exit 0, no output |
| 2 | `git grep -n -E 'go-version\|actions/(checkout\|setup-go\|download-artifact)@' -- .github/workflows` | only `1.27.4` / `@v7` / `@v7` / `@v8` values |
| 3 | `git diff --check` | exit 0 |
| 4 | host-path scan: the whole increment (`git diff HEAD` plus the untracked plan file) grepped for the profile-directory segment the program rule names (`<profile-segment>/`, as in `docs/plans/2026-11-22-rf-w3-01-classifier-extract-plan.md`) | empty |
| 5 | `git diff origin/main -- .github/workflows` | every hunk is a value change in the four pin families |
| 6 | advisory measurement, the same command the `daemon` job's advisory step runs, from `daemon/`: `GOOS=linux GOARCH=amd64 govulncheck ./...` with govulncheck v1.10.0, at go1.27.0 and at go1.27.4 | 12 vulnerabilities (`exit 3`) before, 0 (`exit 0`) after |
| 7 | citation-existence gate over a dangling snapshot commit (`GIT_INDEX_FILE=<temp>`, `git read-tree HEAD` + `git add -A` + `git write-tree` + `git commit-tree`, so the gate reads this increment's bytes without touching the index or the tree): scoped run `--base HEAD --head <snapshot>` (audits the `docs/archive/CLOSED-TODO.md` this change modifies — the file is in the audited corpus) and the full-corpus run from the root commit | exit 0 both |

The local advisory measurement is the same work the gate performs: same scanner version
(`govulncheck_version='v1.10.0'` in the `daemon` job), same database (`https://vuln.go.dev`, snapshot
2026-11-11 on this host), same target (`GOOS=linux GOARCH=amd64`, the runner's platform), same
working directory (`daemon/`, the job's `defaults.run.working-directory`), same module set
(`./...`), and the same counting expression (`grep -c '^Vulnerability #'`). The before run is the
negative control: it must reproduce the hosted baseline (12) or the method itself is unproven.

Hosted CI on the PR head — every job green, `daemon` job log showing `go1.27.4` and
`govulncheck_vulnerabilities=0` — is the behavioural oracle and belongs to the conductor: this stage
must not push, so it cannot observe it. The closure statement is written so that the conductor's
`daemon`-job line either confirms it or reopens it (contract Change 5): the statement cites the
local measurement at both toolchains and names the hosted field; the PR's run id is filled when that
run exists (the same deferral the contract records: "fill the run id after CI runs"), and a
non-zero hosted count keeps the entry open with the remaining advisory IDs instead.

## Out of scope, with reasons

| Item | Reason |
| --- | --- |
| `actions/upload-artifact@v4` (`ci.yml:892`, `:942`, `:974`), `actions/setup-node@v4` (`ci.yml:960`), `pnpm/action-setup@v4` (`ci.yml:957`), `erlef/setup-beam@v1` | Contract clause 4 ("other actions"); Change 3 names three actions. Reported as the residual node20 surface rather than changed. |
| `docker/daemon.Dockerfile:3` `ARG GO_IMAGE=golang:1.27.0-bookworm` | Contract clause 4 ("runner images"): a container base image, not a CI pin; still a pre-existing exposure to the same stdlib advisories for the compose/daemon image build, and it is outside the owned set. |
| `docs/TODO.md:247` (baseline `12de9428`; `:249` on the merged tree) ("pinned tooling Go 1.27.0" in the linux-v2 acceptance entry) | Outside the owned entry (`docs/TODO.md` (this one entry)); the acceptance entry's recorded precondition becomes 1.27.4 with this change and needs a one-token amendment the owner can make, recorded here and in the report instead of edited silently. |
| `docs/goals/0010-refactor-program.md:639-641` ("Go stdlib advisories" bullet: "CI pins `go-version: 1.27.0`" plus its `docs/TODO.md` pointer) | Live program-doc statement and pointer stale after this change: the advisories are fixed at the reviewed revision, and the entry it tracks has moved to `docs/archive/CLOSED-TODO.md`. Outside the owned set (`docs/goals/`), so recorded here rather than edited; the owner re-points or closes the bullet. |
| `daemon/internal/harness/testdata/**` and `scripts/ci/test-linux-v2-evidence.py:110` mentions of `go1.27.0` | Historical observation records and opaque sanitizer fixtures, not pins; nothing validates them against the CI toolchain. |
| `daemon/go.mod` dependencies, `mix`/npm versions, Elixir/OTP/Node/Postgres versions, Dependabot #168/#171 closure | Contract clause 4. |

## Risks and limits

- The v8 `digest-mismatch: error` default is the one inherited behaviour change that can redden a
  job without an input change. It can only fail on an artifact GitHub's digest check rejects, which
  is a real corruption signal; the plan keeps the strict default on purpose.
- `GOTOOLCHAIN=local` (setup-go v6+) removes the implicit toolchain download. For this tree the
  installed 1.27.4 satisfies `go 1.27`; if a future `go.mod` raises its `go` directive above the
  pinned `go-version`, the `daemon` jobs would now fail loudly instead of silently downloading a
  toolchain. That is the upstream intent and is recorded as the coupling to keep in mind when
  `go.mod` moves.
- Node 20 deprecation is only *partly* removed: the three `upload-artifact@v4` sites and the
  `setup-node@v4`/`pnpm/action-setup@v4` pair in the coverage/flake job keep node20 in these files.
- The hosted oracle is unobservable from this stage (no push); the local measurement is
  revision-bound to `12de9428` plus this increment's workflow lines, and its DB snapshot is the
  2026-11-11 vuln.go.dev database, which the conductor's run may have superseded.
