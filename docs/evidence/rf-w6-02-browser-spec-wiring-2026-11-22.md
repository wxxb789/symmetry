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
`dockerDesktopLinuxEngine` pipe), no PostgreSQL listener on the planning driver's 55441 loopback
port, no staged portal assets (`control/priv/static/portal`) for the planning release and no
WSL-visible `bash` for the Linux carriers, so the Compose stack, the connection fixture instance
and the planning release cannot execute here; every such step is a recorded host limitation (§5)
with the exact command the CI job runs. The workspace `pnpm install` cannot resolve the three feed gaps W5-08 and W6-01
recorded (`knip@6.46.1`, `@vitest/coverage-v8@5.1.1`, `msw@3.0.7`), so the browser gates ran with
the pinned `@eslint/js@10.0.1` / `@playwright/test@1.65.1` / `eslint@10.15.0` / `globals@17.14.0`
installed into a scratch directory and exposed as `browser/node_modules` (junction, removed
after the runs; `git status --porcelain` carries no tracked change from it).

## 1. Deliverable shape (files, counts)

| file | status | lines | note |
| --- | --- | ---: | --- |
| `.github/workflows/ci.yml` | edited | 1,846 → 2,164 | +318/−0: the `compose` job's five appended steps and the new job (§3); two unused step ids (`compose_browser`, `compose_restart`) removed by the apply stage (simplify F1); the fix stage added the planning step's `PHX_HOST` export (+2 lines) and the connection step's trailing canary guard (+12 lines, review F1/F2); the review-fix stage made the three new `/healthz` probes tolerant (`--retry-all-errors`, rulings P0-1/P1-3), restored the provider-call-log canary grep and pinned the full executed sets (rulings P3-5/P3-6, §3.5); the render gate `:479-556` and its 7-name `--expect-file` list are byte-identical |
| `browser/README.md` | edited | 615 → 613 | +11/−13: the manual matrix intro and rows (§3.4) |
| `browser/tests/portal-connections.spec.mjs` | edited | 1,489 → 1,443 | +74/−120: the four E6 conversions plus `bindResourceThroughApi` → shared `attachResource` (§7; the latter in the apply stage, simplify F2) |
| `browser/tests/portal-compose-daemon.spec.mjs` | edited | 577 → 571 | +15/−21: four helper copies converted (§7) |
| `browser/tests/portal-compose-restart.spec.mjs` | edited | 206 → 205 | +1/−2: `workUrl` converted (§7) |
| `browser/tests/portal-work-planning.spec.mjs` | edited | 875 | +1/−1: the review-fix stage's replayed-create expectation — the served text, which PR #111 appended the owner-discard note to (ruling P0-2, cited in §3.5); no helper, no title and no other assertion changed |
| `browser/tests/readable-fixtures.mjs` | **unedited** | 431 | the failure control forbids mutating it; `git diff` is empty |
| the 7 render-gate specs | **unedited** | — | no rename, no title change |
| `browser/scripts/run-*.ps1` | **unedited** | 98 / 270 / 250 | each driver stays the local reproduction path; no owned-file clause forced an edit; the isolation suite still passes 7/7 |
| `browser/playwright.config.mjs`, `browser/package.json` | **unedited** | — | no new reporter, no new script |
| `docs/plans/2026-11-22-rf-w6-02-browser-spec-wiring-plan.md` | new | 248 | the plan (228 after the implement stage; the apply stage's F6/F7 record edits add two lines; the fix stage's `PHX_HOST` description, canary guard, isolation-suite count and mutation clause add eight; the review-fix stage's probe, executed-set, canary and mutation wording add ten) |
| `docs/evidence/rf-w6-02-browser-spec-wiring-2026-11-22.md` | new | — | this record |
| `docs/TODO.md` / `docs/archive/CLOSED-TODO.md` | **unedited** | — | no entry closes; the remaining helper copies are recorded here instead (§8) |

## 2. Inventory before/after (W2-01's gate, consumed never re-created)

Before (base tree): `browser_suite_coverage: suites=16 wired=7 manual=9 unclassified=0`, exit 0.
After: `suites=16 wired=14 manual=2 unclassified=0`, exit 0 — the four newly wired specs, the
three node suites and the unchanged seven render-gate specs wired; `portal-live-daemon` and
`portal-chat-live` the only manual rows; no unknown matrix entry, no overlap. Both outputs are in
§5.

## 3. Changes

### 3.1 The Compose family in the `compose` job (`:1848-1963`)

Five appended steps after the rootless evidence upload: pnpm/node setup + `pnpm install` +
`playwright install chromium`; `Run the Compose daemon acceptance on a stack this job provisions`
(bring up `symmetry-compose-browser-<run>-<attempt>`, tolerant `/healthz` wait (`curl
--retry-all-errors --retry 90`), `daemon` service-running wait, the daemon spec on desktop
Chromium, the executed-set assertion); `Verify the control restart preserves the Compose
workspace` (the `DC%` project read, the eight-table fingerprint before, `docker compose restart
control`, tolerant `/healthz` wait (`--retry-all-errors --retry 30`), the fingerprint after,
byte equality, the restart spec, its executed-set assertion); `Tear down the Compose acceptance
stack` (`if: always()`); `Upload the Compose acceptance reports` (`if: always()`). P1 is
implemented: the stack is provisioned by this family (the smoke step's stack dies in its own
`EXIT` trap), never reused across steps.

### 3.2 `browser-fixture-linux` (`:1969-2164`)

`needs: [changes, contracts]`, `if: needs.changes.outputs.run-heavy == 'true'`, ubuntu-latest,
the `postgres:18.6-alpine` service, `portal-assets` + `control-release-linux` downloads, the
dev-build compile, then three phases: the node suites; the connection fixture instance (per-run
token/identity, empty steering document, `symmetry_0007e_test_4021`, readiness on the identity
file + the transport marker + `/healthz`, then the trailing canary guard over the fixture log and
the provider-call log); the planning release (`DATABASE_URL`, `PHX_HOST=127.0.0.1` exported
before the eval so the prod runtime config provider resolves, `SYMMETRY_PLANNING_HARNESS=1`,
`SYMMETRY_FIXTURE_MIX=mix`, the release archive's assets, `Release.migrate()`, `start`, tolerant
`/healthz` wait (`--retry-all-errors --retry 60`)). Each phase traps its own teardown; the
executed-set assertions pin both projects, zero skips, the file and every case title (11
connections, 18 planning; §3.5); an `if: always()` upload keeps the reports, logs and fixture
call log.

### 3.3 Job-model constraints

The new job passes the workflow-model pins: lowercase-hyphenated header, `changes` in `needs`,
`if: needs.changes.outputs.run-heavy == 'true'` exactly; `legacy-route-audit` stays the only
unconditional job (`test-ci-changed-path-classifier.py`, 31 tests, §5). Every new inline body is
≤ 80 lines (27/48/6/2/72/47) and adds no advisory step, so
`test-ci-structure-extraction.py` stays green (23 tests).

### 3.4 The README matrix

The matrix section now states the wired sets — the render step's seven specs, the Compose job's
two specs, the fixture job's two specs and the three `node --test` suites — and keeps exactly two
rows (`portal-live-daemon`, `portal-chat-live`) with their drivers and host requirement. The gate
reads exactly one backticked suite path per row; no prose row is gate-visible.

### 3.5 The review-fix stage (PR #260 review rulings)

Four changes at the final tree: (1) the three new `/healthz` probes (`:1867`, `:1924`, `:2113`)
run `curl --fail --silent --show-error --retry <n> --retry-delay 1 --retry-all-errors`, the ps1
drivers' retry pattern; the HEAD bytes retried only ECONNREFUSED, so one port reset (CURLE 56)
killed the step (§6 row l); (2) the connection guard scans the fixture log and the
provider-call log for both provider canaries (§6 row j); (3) the two executed-set assertions pin
every case title — 11 connections, 18 planning (§6 rows h/i, m); (4) the planning spec's
replayed-create expectation now names the served text. PR #111 (`db318383`, 2026-11-20) added
the owner-discard note text (`git log -S` over `frontend/src/features/planning`), and the
frontend unit pin is `planning.test.tsx:203-205`; the stale expectation dates to PR #35
(`5c2e0ada`, 2026-11-17) and was unchanged since (`git log -S` over the spec file). The case
still asserts one stored work item; the two CI failures were exactly this stale expectation.

## 4. Control-flow preservation (lesson 32)

Base-vs-HEAD audit of the three drivers (read in full at the base revision). “Preserved” means the
phase's observable assertion exists in the wired CI step; deltas are named.

| family | driver phase (base) | wired carrier | preserved |
| --- | --- | --- | --- |
| compose | `docker compose up -d --build` + `curl /portal/login` readiness | step brings up its own named project; tolerant `/healthz` probe (`--retry-all-errors --retry 90`) + `daemon` service-running loop | yes (probe delta: `/healthz`/`daemon` in place of `/portal/login`, which also serves; the tolerance matches the driver's `--retry-all-errors`) |
| compose | `SYMMETRY_PORTAL_URL/OPERATOR_TOKEN/COMPOSE_DAEMON` env + `pnpm run test:compose` (desktop) | same env; `test tests/portal-compose-daemon.spec.mjs --project=desktop-chromium` + executed-set assertion | yes (assertion added: a skip is not a verdict) |
| compose | `docker compose exec psql` `DC%` record read (`-At -F '|'`, `$LASTEXITCODE` checked) | same query via `docker compose exec -T postgres psql` | yes |
| compose | eight-table `md5(jsonb_build_object(...))` fingerprint before/after `docker compose restart control` + `/portal/login` re-readiness | SQL ported verbatim; `restart control`; tolerant `/healthz` (`--retry-all-errors --retry 30`) | yes |
| compose | `SYMMETRY_COMPOSE_PROJECT_ID/KEY/NAME` + `playwright test tests/portal-compose-restart.spec.mjs --project=desktop-chromium` | same env; same invocation + executed-set assertion | yes |
| compose | (no teardown; the manual stack stays) | `down --volumes --remove-orphans` in an `always()` step | added by P1 |
| connection | `ecto.drop/create/migrate` with `$LASTEXITCODE` throws | `mix ecto.create` + `mix ecto.migrate` after `mix deps.get`/`mix compile` | created+migrated; the drop is rerun hygiene on a machine where the DB persists — the CI service DB is fresh per run |
| connection | per-run identity + token mint; empty steering document; `SYMMETRY_FIXTURE_*` + `SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS=3600000` + `SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS=expired-fixture` | same, minted with `openssl rand` | yes |
| connection | `mix run --no-halt --no-start ../scripts/ci/portal-connection-fixture.exs` (PHX_SERVER/PORT) | same command from `control/` | yes |
| connection | readiness: identity file (identity+port), log marker `connection fixture transport installed` + logged identity, `/healthz` 200; server-exit early exit | same four checks + `kill -0` liveness per poll | yes |
| connection | spec run both projects + summary assertion (22 passed, no skip) | same invocation + `assert-playwright-json.py` (skips fail; file and all 11 case titles pinned in both projects) | yes (stricter: the driver read a `22 passed` summary, the assertion pins every case's identity and state — a case deleted from the spec is red there and stayed green in the driver) |
| connection | canary scan of control log/err + provider-call log | the step's trailing guard (14 lines) greps the fixture log and the provider-call log for both provider canaries and requires a non-empty `connection-fixture-calls.jsonl`; the spec's own response/DOM/header/cookie/console canary scans still run | yes (delta: the step merges the fixture's stderr into the one log, so the driver's separate `.err` file has no counterpart; the provider-call-log grep is carried) |
| connection | `taskkill` the started tree + listener PID; port-free wait | `trap ... EXIT` kills the started process | yes (PID/lock mechanics are local-concurrency guards, not assertions; a taken port fails closed at bind + readiness) |
| planning | `ecto.drop/create/migrate` checked | `mix ecto.create` + `mix ecto.migrate` | as above |
| planning | per-run token; `SYMMETRY_PLANNING_HARNESS=1`; pinned toolchain | `openssl rand` token; same harness flag; setup-beam is the CI toolchain | yes |
| planning | `mix phx.server` dev launcher with a restart loop (host BEAM-kill workaround) + `/healthz` + `/portal/login` form + staged-asset/service checks | the release (`bin/symmetry_control start`) after `Release.migrate()`, tolerant `/healthz` wait (`--retry-all-errors --retry 60`), `SYMMETRY_FIXTURE_MIX=mix`; the spec exercises the same routes | yes, carrier delta: the release is the contract's named carrier; the launcher loop and early-fail probes are host affordances, not assertions (the release log is uploaded on `always()`) |
| planning | spec both projects + summary assertion (≥1 passed, no skip) | same invocation + `assert-playwright-json.py` (skips fail; file and all 18 case titles pinned in both projects) | yes (stricter: the driver's summary required one pass, the assertion pins every case's identity and state) |
| planning | `taskkill` + port-free wait + mutex | `trap ... EXIT` | yes |
| connection + planning | instance build isolation: `MIX_BUILD_PATH=control/_build/instances/<family>-<port>`, with an inherited value overridden and reported | not carried: the job compiles and runs `mix` from `control/` into the one shared `control/_build/dev`, and the release serves from its own extracted archive | **no** (recorded; carrier: a single job hosts one instance at a time, so no second dev build can collide — and the isolation suite's six harness cases stay Windows-only, so the Linux node step cannot assert it either) |

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
| new inline bodies | `bash -n` over the 40 `shell: bash` `run: \|` bodies of `ci.yml` and the PowerShell parser over its 12 `shell: pwsh` bodies | 0 / 0 | 52 blocks, 0 syntax failures (parsed by their own shell); new bodies 27/48/6/2/60/30 lines ≤ 80 at the implement stage, 27/48/6/2/72/47 at the final tree (§3.5) |
| browser lint | `node node_modules/eslint/bin/eslint.js . --max-warnings 0` from `browser/` | 0 | 0 findings (converted specs included) |
| collection parity | `playwright test --list` on the four converted specs, base copies vs HEAD | 0 / 0 | 62 normalized listing lines each, **identical** titles and order |
| importer link check | `playwright test --list` (all 13 specs) | 0 | `Total: 168 tests in 13 files` |
| node suites | `node --test scripts/connection-spec-env.test.mjs scripts/goal-context-fixtures.test.mjs` from `browser/` | 0 | 5 pass / 0 fail (the env suite drives the real CLI against the converted spec: 22 skipped ×2, the partial-identity collection failure, `Total: 22 tests in 1 file`) |
| isolation suite | `node --test scripts/control-instance-isolation.test.mjs` | 0 | 7 pass / 0 fail (Windows; on Linux its six harness cases carry their own recorded skip) |
| assertion shape (synthetic) | the four new `assert-playwright-json.py` invocations against well-formed synthetic reports | 0 | each verifies its file/title/projects |
| step control-flow dry run | the four new step bodies (27/48/6/2/60/30 lines) extracted from `ci.yml` and run under `bash` with command-level stubs for `docker`/`curl`/`mix`/`tar`/`pnpm`/`jq` and the real assertion program (implement stage; the two bodies the fix stage changed are re-run at the final bytes in the next row) | 0 / 0 / 0 / 0 | daemon: up → healthz → service check → spec → assertion; restart: record → fingerprint → restart → healthz → fingerprint equality → spec → assertion; connection: create/migrate → fixture start → identity+marker+healthz readiness → spec → assertion → canary guard; planning: create/migrate → release extract/eval/start → healthz → spec → assertion (the psql argument log shows the exact fingerprint SQL and the interpolated project id) |
| fix-stage dry run of the two changed bodies | the connection and planning bodies extracted from `ci.yml` at the final tree and run under `bash` in a sandbox (stub `mix` that publishes the identity file, the transport marker, the logged identity and the recorded call; stub `tar` that lays down a release stub which fetches `PHX_HOST` as `config/runtime.exs:150` does; stub `curl`; stub `pnpm` writing the report shape; the real `scripts/ci/assert-playwright-json.py`) | 0 / 0 | connection: exit 0 with `render gate executed set verified: 0 skipped; 'a connection is created, inspected, tested and removed through the real API' passed in every project`, guard silent; planning: exit 0 with the same verified line for the planning title, `release eval` then `release start` in the stub log |
| release provider probe (F1 mechanism, measured on this host) | `Config.Reader.read!("config/runtime.exs", env: :prod, target: :host)` with the CI planning env's `DATABASE_URL`/`SECRET_KEY_BASE`; then a scratch `mix release` project whose prod `runtime.exs` fetches `PHX_HOST`, run as `bin/<app> eval` | 1 (no `PHX_HOST`) / 0 (with it) | no-`PHX_HOST` run: `** (System.EnvError) could not fetch environment variable "PHX_HOST" because it is not set`, reported at `releases/<vsn>/runtime.exs` for the release `eval`; with `PHX_HOST=127.0.0.1` both runs succeed (`endpoint host=127.0.0.1`, `PROBE_EVAL_RAN`) |
| diff hygiene | `git diff --check` | 0 | no whitespace errors |
| DLP | case-insensitive profile-directory-fragment grep over the diff; backslash-u count over the raw diff | 1 / 0 | no host-path match, 0 backslash-u sequences |

Apply stage re-run (simplify F1 + F2, then-final tree): the same foreground gate set re-run with
unchanged counts — inventory `suites=16 wired=14 manual=2 unclassified=0`, classifier 31 tests,
extraction 23, assertion-program 17, actionlint 0 findings, the 52 `run: |` bodies clean under
their own parser (40 `bash -n`, 12 pwsh), `git diff --check` clean, DLP clean — plus `node --check`
and eslint on the converted spec, its `playwright test --list` parity against the base bytes, the
two node suites, and the converted 201 guard's stub-driven mutation (§7).

Fix stage re-run (review F1–F4, final tree): the same foreground gate set re-run — inventory
`suites=16 wired=14 manual=2 unclassified=0`, classifier 31 tests, extraction 23,
assertion-program 17, actionlint 0 findings, the 52 `run: |` bodies clean under their own parser
(40 `bash -n`, 12 pwsh), `git diff --check` clean, DLP clean — plus `node --check` on the touched
`.mjs`, the `yaml.safe_load` model read (15 jobs, the new job's `needs`/`if`/`runs-on` as
pinned), the fix-stage dry run of the two changed bodies (§5), and mutation rows d–g (§6). The
`PHX_HOST` export and the connection canary guard are the only behaviour changes; the plan's
isolation-suite count, its `PHX_HOST` description and this record's rows are the document
changes.

Review-fix stage re-run (PR #260 review rulings, final tree): the same foreground gate set
re-run — inventory `suites=16 wired=14 manual=2 unclassified=0`, classifier 31 tests, extraction
23 tests (all 40 bash inline bodies clean under `bash -n`, the 12 PowerShell bodies clean under
the PowerShell parser; the four changed bodies at 27/48/72/47 lines stay under the 80-line
ceiling), assertion-program 17 tests, actionlint 0 findings, the `yaml.safe_load` model read (15
jobs), `node --check` on the touched `.mjs`, a Playwright `--list` over the edited planning spec
(36 tests; the CLI's JSON collection leaf titles are identical to both executed-set lists),
`git diff --check` clean, DLP clean — plus the synthetic-report assertion runs, the restored
canary-guard runs and the real-curl probe comparison (§6 rows h–m).

Not run on this host (recorded as missing, never passed):

- the Compose stack and both browser phases (`docker compose up --build --detach`, `docker
  compose exec -T postgres psql …`, `mix run --no-halt --no-start
  ../scripts/ci/portal-connection-fixture.exs`, `bin/symmetry_control start`): no Docker daemon
  on this host; the planning phase additionally lacks its 55441 database listener and the staged
  portal assets. The CI run is the executed version of these commands.
- the ps1 drivers' fixture sequences (they need the same stack, `mise` Elixir/Node and staged
  portal assets): recorded, not executed. Their harness contract is still covered locally by the
  isolation suite (7/7) and the env suite against the converted spec.
- the planning driver (`browser/scripts/run-planning-tests.ps1`) additionally needs PostgreSQL on
  its 55441 loopback port and the staged portal assets (`control/priv/static/portal/index.html`):
  this host has no listener on that port and the assets are absent (the frontend build that
  stages them needs the workspace install with the recorded feed gaps). The reachable part of
  its contract was re-run — `playwright test --list` over the edited spec collects 36 tests and
  the CLI's JSON leaf titles match the 18-title assertion exactly.
- the render gate and the new jobs' end-to-end runs: conductor-verified on the PR.

## 6. Mutations (judging checks, executed)

| # | mutation | carrier | result |
| --- | --- | --- | --- |
| a | one `--expect-file` removed from the Compose assertion, against a two-file report | synthetic report + the exact step assertion | exit 1: `the report's spec files are not the invocation: unexpected portal-compose-restart.spec.mjs` |
| b | the asserted file absent from the run's report (a wired spec pointed at a file it did not run) | synthetic one-file report + the full assertion | exit 1: `missing portal-compose-restart.spec.mjs` and the missing case's `no result` |
| c | a wired family labelled manual in the README while its step exists | README row added, gate run, row reverted | exit 1: `portal-connections.spec.mjs is both named by .github/workflows/ci.yml and listed in the browser/README.md manual matrix` |
| control | the unmutated shapes | the same synthetic reports and the final README | exit 0 (a/b green on the two-file report; the inventory PASS after the revert) |
| d | a provider canary written into the connection phase's fixture log (both canary values tried) | the step's trailing guard as it stands in `ci.yml`, run under `bash` against synthetic logs | exit 1 after the executed-set assertion had already verified: `canary scan FAILED: <log> contains gho_fixture_connection_token_canary` (and the Azure canary likewise) |
| e | the provider-call log empty, then missing | the same guard lines, same synthetic logs | exit 1: `canary scan FAILED: provider-call log <path> is missing or empty: no recorded provider call to scan` |
| f | the planning step's `export PHX_HOST=127.0.0.1` line deleted, with a release stub that fetches `PHX_HOST` as the prod runtime config does | the planning body extracted from `ci.yml` (final bytes) minus that one line, run in the fix-stage sandbox | exit 1: `** (System.EnvError) could not fetch environment variable "PHX_HOST" because it is not set` at the eval, before the release starts |
| g | the unmutated shapes | the two bodies at the final bytes, clean logs, non-empty provider-call log, export present | exit 0 each, guard silent, and the real assertion program reports the verified executed set |
| h | one non-first connections case dropped from the run's report | the full 11-title invocation extracted from `ci.yml` (final bytes) vs the HEAD single-title invocation, same synthetic report | exit 1: `case 'a GitHub repository resource is bound, …' did not run as expected: desktop-chromium: no result; mobile-chromium: no result`; the HEAD invocation exit 0 on the same report (the P3-6 gap demonstrated) |
| i | one non-first planning case dropped from the run's report | the full 18-title invocation vs HEAD, same synthetic report | exit 1: `case 'applies a late board move only to its own project' did not run as expected: …`; the HEAD invocation exit 0 |
| j | a provider canary in the provider-call log | the restored guard (final bytes) vs the HEAD guard, synthetic logs | final: exit 1 `canary scan FAILED: <calls.jsonl> contains fixture-ado-access-token-canary`; HEAD: exit 0 (the dropped grep) |
| k | a canary in the fixture log; the provider-call log empty; the provider-call log missing | the same guard pair, synthetic logs | both guards exit 1 on each of the three; the clean pair is silent and exit 0 |
| l | the three changed probes against a socket server that resets its first two accepted connections | the HEAD and final command lines with the URL substituted for the local port | HEAD: exit 56 after one connection (`curl: (56) Recv failure: Connection was reset`); final: exit 0 after three connections, the two resets printed by `--show-error` |
| m | the unmutated shapes of the review-fix changes | the two full-title invocations against reports carrying exactly the pinned titles; the restored guard against clean logs; the three final probes against a healthy endpoint | exit 0 each |

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
| `portal-connections` | `bindResourceThroughApi(page, projectId, { kind, name, reference, connectionId })` (a field-renaming wrapper over the same POST) | shared `attachResource(page, projectId, resource)` at its nine call sites, with `external_ref`/`connection_id` spelled at the site | collection parity (apply stage); eslint 0; env suite; stub-driven mutation: 500 → red with the JSON body message, 201 → returns `body.resource` |
| `portal-compose-daemon` | `uniqueProject` key minting | shared `uniqueKey(prefix)` inside the same-named preset | collection parity |
| `portal-compose-daemon` | `projectUrl`, `workUrl`, `servedColumnTitles` | shared imports of the same names (`projectUrl` was a byte-identical copy; `servedColumnTitles` gains the shared 200 + project assertions) | collection parity |
| `portal-compose-restart` | `workUrl` | shared import | collection parity |
| `portal-work-planning` | — | nothing removable: its local `columnOf`/`moveByKeyboard` and its `seedProject` preset have no shared counterpart, and adding one would mutate the W4-14 module | collection parity (36 tests); the review-fix stage edits only the replayed-create expectation (§3.5) — no title, no helper, no other assertion (the CLI's JSON leaf titles match the 18-title pin) |

Collection parity: `playwright test --list` over the four specs, base copies (HEAD bytes) vs the
converted files, identical 62-line normalized listings (titles, order, projects).

Apply stage (simplify F1 + F2, applied after the implement gate run): the two unused step ids
above were deleted, and the `bindResourceThroughApi` conversion above was re-verified on the final
tree — `playwright test --list` parity for the spec (identical titles and order), `node --check`,
eslint 0, the two node suites, `git diff --check` and the DLP greps. Its 201 guard was
mutation-checked with a stub `page` over a local HTTP server: 500 reddens the helper with the JSON
body message, 201 resolves `body.resource`. The section 5 counts were re-run unchanged on the
final tree; the section 1 line counts, the section 3.1/3.2 ranges and the section 9 dLOC are
republished at the final tree.

## 8. Residual risks and recorded remainder

- The CI steps' first real execution is the PR's CI run (the conductor's slow oracle). The
  instrument shapes, the workflow model, the assertion invocations and the collections were
  verified locally; the stack/release execution could not be.
- Connection-phase scan delta: the driver scans the control log, its `.err` file and the
  provider-call log as three files; the CI step merges the fixture's stderr into its one log, so
  the guard greps the merged log and the provider-call log; the driver's separate `.err` file has
  no counterpart (§4).
- The planning step's `PHX_HOST` export is what the release's prod runtime config provider fetches
  for every command, `eval` included (`config/runtime.exs:150`); an edit that moved it back onto
  the `start` line alone reintroduces the abort (mutation row f, §6).
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

`git diff --numstat` over the changed files at the final tree (PR base `f0448773`): `ci.yml`
+318/−0 (report-only per D2; the plan's ~+200 estimate), `browser/README.md` +11/−13,
`portal-connections.spec.mjs` +74/−120, `portal-compose-daemon.spec.mjs` +15/−21,
`portal-compose-restart.spec.mjs` +1/−2, `portal-work-planning.spec.mjs` +1/−1 — total
**+420/−157 = +263** tracked lines, plus the plan (248) and this record (292).
