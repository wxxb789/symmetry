# rf-w6-02-browser-spec-wiring — evidence record (2026-11-22)

Subject: D7 browser-spec wiring — the Compose family runs in the `compose` job on a stack that
job provisions (conductor P1), a new `browser-fixture-linux` job runs the connection-fixture and
planning families plus the three browser node suites, chat-live/live-daemon stay the explicit
manual matrix, and W4-14's E6-deferred helper conversions are owned here. Contract:
`goal-rf-w6-02-browser-spec-wiring` (GDD, 2026-11-21, binding with D1/D2/D7, the lesson-32
control-flow decision, the W6 reconcile P1 and the owner comment policy of 2026-11-22). Base
revision `6d55c57532cf3c5579bebcd5580658e3ca08764d` (`test(frontend): keep layout, scroll and focus
invariants in the browser layer only (#254)`; the contract's dispatch dependencies — W2-01,
W4-14, W5-08, W6-01 — are merged into it, tree clean). Plan:
`docs/plans/2026-11-22-rf-w6-02-browser-spec-wiring-plan.md`. Judging artifact (contract clause
2): this record, the implement-stage report and the PR's CI run.

Host note: this workstation has no Docker daemon (`docker info` fails on the
`dockerDesktopLinuxEngine` pipe), no PostgreSQL listener on 5432 and no WSL-visible `bash` for the
Linux carriers, so the Compose stack, the connection fixture instance and the planning release
cannot execute here; every such step is a recorded host limitation (§5) with the exact command
the CI job runs. The workspace `pnpm install` cannot resolve the three feed gaps W5-08 and W6-01
recorded (`knip@6.46.1`, `@vitest/coverage-v8@5.1.1`, `msw@3.0.7`), so the browser gates ran with
the pinned `@eslint/js@10.0.1` / `@playwright/test@1.65.1` / `eslint@10.15.0` / `globals@17.14.0`
installed into a scratch directory and exposed as `browser/node_modules` (junction, removed
after the runs; `git status --porcelain` carries no tracked change from it).

## 1. Deliverable shape (files, counts)

| file | status | lines | note |
| --- | --- | ---: | --- |
| `.github/workflows/ci.yml` | edited | 1,846 → 2,123 | +277/−0: the `compose` job's five appended steps and the new job (§3); the render gate `:479-556` and its 7-name `--expect-file` list are byte-identical |
| `browser/README.md` | edited | 615 → 613 | +11/−13: the manual matrix intro and rows (§3.4) |
| `browser/tests/portal-connections.spec.mjs` | edited | 1,489 → 1,451 | +67/−105: the four E6 conversions (§7) |
| `browser/tests/portal-compose-daemon.spec.mjs` | edited | 577 → 571 | +15/−21: four helper copies converted (§7) |
| `browser/tests/portal-compose-restart.spec.mjs` | edited | 206 → 205 | +1/−2: `workUrl` converted (§7) |
| `browser/tests/portal-work-planning.spec.mjs` | **unedited** | 875 | no shared counterpart exists for its remainder helpers (§7) |
| `browser/tests/readable-fixtures.mjs` | **unedited** | 431 | the failure control forbids mutating it; `git diff` is empty |
| the 7 render-gate specs | **unedited** | — | no rename, no title change |
| `browser/scripts/run-*.ps1` | **unedited** | 98 / 270 / 250 | each driver stays the local reproduction path; no owned-file clause forced an edit; the isolation suite still passes 7/7 |
| `browser/playwright.config.mjs`, `browser/package.json` | **unedited** | — | no new reporter, no new script |
| `docs/plans/2026-11-22-rf-w6-02-browser-spec-wiring-plan.md` | new | 228 | the plan |
| `docs/evidence/rf-w6-02-browser-spec-wiring-2026-11-22.md` | new | — | this record |
| `docs/TODO.md` / `docs/archive/CLOSED-TODO.md` | **unedited** | — | no entry closes; the remaining helper copies are recorded here instead (§8) |

## 2. Inventory before/after (W2-01's gate, consumed never re-created)

Before (base tree): `browser_suite_coverage: suites=16 wired=7 manual=9 unclassified=0`, exit 0.
After: `suites=16 wired=14 manual=2 unclassified=0`, exit 0 — the four newly wired specs, the
three node suites and the unchanged seven render-gate specs wired; `portal-live-daemon` and
`portal-chat-live` the only manual rows; no unknown matrix entry, no overlap. Both outputs are in
§5.

## 3. Changes

### 3.1 The Compose family in the `compose` job (`:1848-1970`)

Five appended steps after the rootless evidence upload: pnpm/node setup + `pnpm install` +
`playwright install chromium`; `Run the Compose daemon acceptance on a stack this job provisions`
(bring up `symmetry-compose-browser-<run>-<attempt>`, `/healthz` wait, `daemon` service-running
wait, the daemon spec on desktop Chromium, the executed-set assertion); `Verify the control
restart preserves the Compose workspace` (the `DC%` project read, the eight-table fingerprint
before, `docker compose restart control`, healthz, the fingerprint after, byte equality, the
restart spec, its executed-set assertion); `Tear down the Compose acceptance stack` (`if:
always()`); `Upload the Compose acceptance reports` (`if: always()`). P1 is implemented: the stack
is provisioned by this family (the smoke step's stack dies in its own `EXIT` trap), never reused
across steps.

### 3.2 `browser-fixture-linux` (`:1971-2123`)

`needs: [changes, contracts]`, `if: needs.changes.outputs.run-heavy == 'true'`, ubuntu-latest,
the `postgres:18.6-alpine` service, `portal-assets` + `control-release-linux` downloads, the
dev-build compile, then three phases: the node suites; the connection fixture instance (per-run
token/identity, empty steering document, `symmetry_0007e_test_4021`, readiness on the identity
file + the transport marker + `/healthz`); the planning release (`DATABASE_URL`,
`SYMMETRY_PLANNING_HARNESS=1`, `SYMMETRY_FIXTURE_MIX=mix`, the release archive's assets,
`Release.migrate()`, `start`, healthz). Each phase traps its own teardown; the executed-set
assertion pins both projects, zero skips, the file and one title per spec; an `if: always()`
upload keeps the reports, logs and fixture call log.

### 3.3 Job-model constraints

The new job passes the workflow-model pins: lowercase-hyphenated header, `changes` in `needs`,
`if: needs.changes.outputs.run-heavy == 'true'` exactly; `legacy-route-audit` stays the only
unconditional job (`test-ci-changed-path-classifier.py`, 31 tests, §5). Every new inline body is
≤ 80 lines (27/48/6/2/48/28) and adds no advisory step, so
`test-ci-structure-extraction.py` stays green (23 tests).

### 3.4 The README matrix

The matrix section now states the wired sets — the render step's seven specs, the Compose job's
two specs, the fixture job's two specs and the three `node --test` suites — and keeps exactly two
rows (`portal-live-daemon`, `portal-chat-live`) with their drivers and host requirement. The gate
reads exactly one backticked suite path per row; no prose row is gate-visible.

## 4. Control-flow preservation (lesson 32)

Base-vs-HEAD audit of the three drivers (read in full at the base revision). “Preserved” means the
phase's observable assertion exists in the wired CI step; deltas are named.

| family | driver phase (base) | wired carrier | preserved |
| --- | --- | --- | --- |
| compose | `docker compose up -d --build` + `curl /portal/login` readiness | step brings up its own named project; `/healthz` + `daemon` service-running waits | yes (probe delta: the smoke step's own readiness pair in place of `/portal/login`, which also serves) |
| compose | `SYMMETRY_PORTAL_URL/OPERATOR_TOKEN/COMPOSE_DAEMON` env + `pnpm run test:compose` (desktop) | same env; `test tests/portal-compose-daemon.spec.mjs --project=desktop-chromium` + executed-set assertion | yes (assertion added: a skip is not a verdict) |
| compose | `docker compose exec psql` `DC%` record read (`-At -F '|'`, `$LASTEXITCODE` checked) | same query via `docker compose exec -T postgres psql` | yes |
| compose | eight-table `md5(jsonb_build_object(...))` fingerprint before/after `docker compose restart control` + `/portal/login` re-readiness | SQL ported verbatim; `restart control`; `/healthz` (retry 30) | yes |
| compose | `SYMMETRY_COMPOSE_PROJECT_ID/KEY/NAME` + `playwright test tests/portal-compose-restart.spec.mjs --project=desktop-chromium` | same env; same invocation + executed-set assertion | yes |
| compose | (no teardown; the manual stack stays) | `down --volumes --remove-orphans` in an `always()` step | added by P1 |
| connection | `ecto.drop/create/migrate` with `$LASTEXITCODE` throws | `mix ecto.create` + `mix ecto.migrate` after `mix deps.get`/`mix compile` | created+migrated; the drop is rerun hygiene on a machine where the DB persists — the CI service DB is fresh per run |
| connection | per-run identity + token mint; empty steering document; `SYMMETRY_FIXTURE_*` + `SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS=3600000` + `SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS=expired-fixture` | same, minted with `openssl rand` | yes |
| connection | `mix run --no-halt --no-start ../scripts/ci/portal-connection-fixture.exs` (PHX_SERVER/PORT) | same command from `control/` | yes |
| connection | readiness: identity file (identity+port), log marker `connection fixture transport installed` + logged identity, `/healthz` 200; server-exit early exit | same four checks + `kill -0` liveness per poll | yes |
| connection | spec run both projects + summary assertion (22 passed, no skip) | same invocation + `assert-playwright-json.py` (skips fail; file and title pinned) | yes (stricter) |
| connection | canary scan of control log/err + provider-call log | not in the CI step; the spec's own response/DOM/header/cookie/console canary scans still run, and the driver keeps the log scan locally | **no** (recorded; the wired equivalent is the spec-side scan) |
| connection | `taskkill` the started tree + listener PID; port-free wait | `trap ... EXIT` kills the started process | yes (PID/lock mechanics are local-concurrency guards, not assertions; a taken port fails closed at bind + readiness) |
| planning | `ecto.drop/create/migrate` checked | `mix ecto.create` + `mix ecto.migrate` | as above |
| planning | per-run token; `SYMMETRY_PLANNING_HARNESS=1`; pinned toolchain | `openssl rand` token; same harness flag; setup-beam is the CI toolchain | yes |
| planning | `mix phx.server` dev launcher with a restart loop (host BEAM-kill workaround) + `/healthz` + `/portal/login` form + staged-asset/service checks | the release (`bin/symmetry_control start`) after `Release.migrate()`, `/healthz` wait, `SYMMETRY_FIXTURE_MIX=mix`; the spec exercises the same routes | yes, carrier delta: the release is the contract's named carrier; the launcher loop and early-fail probes are host affordances, not assertions (the release log is uploaded on `always()`) |
| planning | spec both projects + summary assertion (≥1 passed, no skip) | same invocation + `assert-playwright-json.py` | yes (stricter) |
| planning | `taskkill` + port-free wait + mutex | `trap ... EXIT` | yes |

Spec-side preconditions mapped to the new jobs: `portal-connections` needs the five
`SYMMETRY_FIXTURE_*`/overrides plus the identity file — minted/written by the connection phase;
`portal-work-planning` needs `SYMMETRY_PLANNING_HARNESS`, `SYMMETRY_PORTAL_URL`,
`SYMMETRY_OPERATOR_TOKEN` (all set) and `mix` for `scripts/ci/portal-planning-fixture.exs`
(`SYMMETRY_FIXTURE_MIX=mix`); the Compose specs need `SYMMETRY_COMPOSE_DAEMON=1` (daemon step) and
`SYMMETRY_COMPOSE_PROJECT_*` (restart step) — the psql record read supplies the three values.

## 5. Gates (real commands, counts, exit codes)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| inventory before | `python3 scripts/ci/assert-browser-suite-coverage.py` (base bytes) | 0 | `suites=16 wired=7 manual=9 unclassified=0`; PASS |
| inventory after | same on the final tree | 0 | `suites=16 wired=14 manual=2 unclassified=0`; PASS |
| workflow model | `python3 scripts/ci/test-ci-changed-path-classifier.py` | 0 | `Ran 31 tests … OK` |
| extraction pin (inline ceiling, step pins) | `python3 scripts/ci/test-ci-structure-extraction.py` | 0 | `Ran 23 tests … OK` |
| assertion-program pin | `python3 scripts/ci/test-assert-playwright-json.py` | 0 | `Ran 17 tests … OK` |
| workflow syntax | `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml` | 0 | 0 findings |
| YAML model | `python3 -c 'yaml.safe_load(...)'` | 0 | 15 jobs; the new job's `needs`/`if`/`runs-on` as pinned |
| new inline bodies | `bash -n` over every `run: \|` body of `ci.yml` (52 blocks) | 0 | 52 blocks, 0 syntax failures; new bodies 27/48/6/2/48/28 lines ≤ 80 |
| browser lint | `node node_modules/eslint/bin/eslint.js . --max-warnings 0` from `browser/` | 0 | 0 findings (converted specs included) |
| collection parity | `playwright test --list` on the four converted specs, base copies vs HEAD | 0 / 0 | 62 normalized listing lines each, **identical** titles and order |
| importer link check | `playwright test --list` (all 13 specs) | 0 | `Total: 168 tests in 13 files` |
| node suites | `node --test scripts/connection-spec-env.test.mjs scripts/goal-context-fixtures.test.mjs` from `browser/` | 0 | 5 pass / 0 fail (the env suite drives the real CLI against the converted spec: 22 skipped ×2, the partial-identity collection failure, `Total: 22 tests in 1 file`) |
| isolation suite | `node --test scripts/control-instance-isolation.test.mjs` | 0 | 7 pass / 0 fail (Windows; on Linux its six harness cases carry their own recorded skip) |
| assertion shape (synthetic) | the four new `assert-playwright-json.py` invocations against well-formed synthetic reports | 0 | each verifies its file/title/projects |
| step control-flow dry run | the four new step bodies (27/48/48/28 lines) extracted from `ci.yml` and run under `bash` with command-level stubs for `docker`/`curl`/`mix`/`tar`/`pnpm`/`jq` and the real assertion program | 0 / 0 / 0 / 0 | daemon: up → healthz → service check → spec → assertion; restart: record → fingerprint → restart → healthz → fingerprint equality → spec → assertion; connection: create/migrate → fixture start → identity+marker+healthz readiness → spec → assertion; planning: create/migrate → release extract/eval/start → healthz → spec → assertion (the psql argument log shows the exact fingerprint SQL and the interpolated project id) |
| diff hygiene | `git diff --check` | 0 | no whitespace errors |
| DLP | case-insensitive profile-directory-fragment grep over the diff; backslash-u count over the raw diff | 1 / 0 | no host-path match, 0 backslash-u sequences |

Not run on this host (recorded as missing, never passed):

- the Compose stack and both browser phases (`docker compose up --build --detach`, `docker
  compose exec -T postgres psql …`, `mix run --no-halt --no-start
  ../scripts/ci/portal-connection-fixture.exs`, `bin/symmetry_control start`): no Docker daemon
  and no PostgreSQL listener on this host. The CI run is the executed version of these commands.
- the ps1 drivers' fixture sequences (they need the same stack, `mise` Elixir/Node and staged
  portal assets): recorded, not executed. Their harness contract is still covered locally by the
  isolation suite (7/7) and the env suite against the converted spec.
- the render gate and the new jobs' end-to-end runs: conductor-verified on the PR.

## 6. Mutations (judging checks, executed)

| # | mutation | carrier | result |
| --- | --- | --- | --- |
| a | one `--expect-file` removed from the Compose assertion, against a two-file report | synthetic report + the exact step assertion | exit 1: `the report's spec files are not the invocation: unexpected portal-compose-restart.spec.mjs` |
| b | the asserted file absent from the run's report (a wired spec pointed at a file it did not run) | synthetic one-file report + the full assertion | exit 1: `missing portal-compose-restart.spec.mjs` and the missing case's `no result` |
| c | a wired family labelled manual in the README while its step exists | README row added, gate run, row reverted | exit 1: `portal-connections.spec.mjs is both named by .github/workflows/ci.yml and listed in the browser/README.md manual matrix` |
| control | the unmutated shapes | the same synthetic reports and the final README | exit 0 (a/b green on the two-file report; the inventory PASS after the revert) |

The real reports of (a)/(b) are CI-side; the synthetic reports reproduce the report shape the
Playwright JSON reporter writes for these specs (file, per-project case state), which is what the
assertion reads.

## 7. Conversions (W4-14 E6 remainder, call-compatible against `readable-fixtures.mjs`)

| spec | removed local definition | replacement | evidence |
| --- | --- | --- | --- |
| `portal-connections` | `login` (asserted the heading after a raw goto) | `connectionsLogin` preset: `assertFixtureTarget()` then shared `login(page, "/portal/connections", { assert: [heading] })` | collection parity; env suite (22 tests, skip contract) |
| `portal-connections` | `api(page, path, {method, body})` (33 call sites) | shared `api(page, method, path, body)` at every site; return `{status, body}` (the unused `text`/`contentType` and the null-vs-`{}` empty body have no consumer) | collection parity; eslint 0; env suite |
| `portal-connections` | `createProject(page)` (sequence-minted key) | shared `createProject(page, { key, name, description })` behind the `connectionProject` preset using shared `uniqueKey("C")` | collection parity |
| `portal-connections` | `deviceContextOptions(testInfo)` (baked origin) | shared `deviceContextOptions(testInfo, { baseURL: portalOrigin })` | collection parity |
| `portal-compose-daemon` | `uniqueProject` key minting | shared `uniqueKey(prefix)` inside the same-named preset | collection parity |
| `portal-compose-daemon` | `projectUrl`, `workUrl`, `servedColumnTitles` | shared imports of the same names (`projectUrl` was a byte-identical copy; `servedColumnTitles` gains the shared 200 + project assertions) | collection parity |
| `portal-compose-restart` | `workUrl` | shared import | collection parity |
| `portal-work-planning` | — | nothing removable: its local `columnOf`/`moveByKeyboard` and its `seedProject` preset have no shared counterpart, and adding one would mutate the W4-14 module | collection parity (unchanged file, 36 tests) |

Collection parity: `playwright test --list` over the four specs, base copies (HEAD bytes) vs the
converted files, identical 62-line normalized listings (titles, order, projects).

## 8. Residual risks and recorded remainder

- The CI steps' first real execution is the PR's CI run (the conductor's slow oracle). The
  instrument shapes, the workflow model, the assertion invocations and the collections were
  verified locally; the stack/release execution could not be.
- The connection-fixture CI phase keeps the spec-side canary scans but not the driver's control
  log + provider-call-log canary scan (§4, recorded “no”).
- Remaining helper copies with no shared counterpart stay in the three wired specs: `columnOf`,
  `moveByKeyboard` (work-planning, compose-daemon, compose-restart), `itemDetail`, `openWork`,
  `fixtureDescription` (compose-daemon), `workItemDetail` (compose-restart). They are
  deliberately not consolidated here (the shared module is a frozen input for the wiring).
- `browser/node_modules` in this worktree is an untracked junction to a scratch install (removed
  with the gates); it is gitignored and leaves no tracked change.
- Delegation: the optional `ce-work mode:return-to-caller` handoff was skipped — its eligibility
  requires a real gate command to travel with the work statement, and the stage's gate row
  supplies none (`-`); the loop ran inline under this stage's own gates, no unit commits.

## 9. dLOC (D2, report-only)

`git diff --numstat` over the changed files: `ci.yml` +277/−0 (report-only per D2; the plan's
~+200 estimate), `browser/README.md` +11/−13, `portal-connections.spec.mjs` +67/−105,
`portal-compose-daemon.spec.mjs` +15/−21, `portal-compose-restart.spec.mjs` +1/−2 — total
**+371/−141 = +230** tracked lines, plus the plan (228) and this record.
