# rf-w6-02 browser-spec-wiring plan — compose steps in the compose job, a Linux fixture job, and the E6-deferred conversions

Contract: `goal-rf-w6-02-browser-spec-wiring` (queued 2026-11-21; the Wave-6 conductor
decisions D1/D2/D7, the lesson-32 control-flow decision and the W6 reconcile P1 are binding).
Base revision: `6d55c57532cf3c5579bebcd5580658e3ca08764d` (`origin/main` at dispatch, tree clean;
W2-01 + W4-14 + W5-08 + W6-01 are merged — the rebase the contract's dependencies require).
Shape references: `docs/plans/2026-11-22-rf-w6-01-ci-structure-finalize-plan.md` (the nearest
`ci.yml` writer: explicit `working-directory`, inline-body ceiling, artifact hand-offs) and
`docs/plans/2026-11-21-rf-w4-14-browser-fixtures-plan.md` (the shared module, its E6 deferral and
the collection-parity method the conversions below reuse).

## Owned set (STRICT: re-derived at this head)

`.github/workflows/ci.yml` (the `compose` job's appended browser steps and the new
`browser-fixture-linux` job only; the render gate at `:479-556` and its 7-name `--expect-file`
list stay byte-identical) · `browser/README.md` (the manual matrix) · `browser/tests/*` (only the
four wired specs, for the E6-deferred conversions, the 2026-11-22 review ruling's one
replayed-create expectation in `portal-work-planning.spec.mjs`, the round-2 ruling's
replay-branch fix in `portal-compose-daemon.spec.mjs` and the round-3 ruling's strict-mode
locator scoping in both compose specs — no rename, no title change,
no render-gate spec, no shared module) · `docs/TODO.md` / `docs/archive/CLOSED-TODO.md` (only if an
entry closes — none does) · this plan file · `docs/evidence/rf-w6-02-browser-spec-wiring-2026-11-22.md`
(the repo's evidence-record convention, `docs/evidence/rf-w5-08-…` style).

Not owned (oracles only): `scripts/ci/assert-browser-suite-coverage.py` (W2-01's — consumed, never
edited) · `scripts/ci/assert-playwright-json.py` · `browser/tests/readable-fixtures.mjs` and the
other fixture modules (W4-14's — the conversions are call-compatible against it and the wiring
does not mutate it: `git diff` on `readable-fixtures.mjs` stays empty) · the 7 render-gate specs ·
`browser/playwright.config.mjs` · `browser/package.json` · `browser/scripts/run-*.ps1` (no edit is
forced: each driver keeps being the local reproduction path, so every owned-file clause that only
fires "where a driver must stop being the manual entry point" stays unused; the reasons are in
Change 4) · `scripts/ci/test-ci-changed-path-classifier.py` (read only; the new job needs no
fixture change) · `scripts/ci/test-ci-structure-extraction.py` (read only; the new steps stay under
its 80-line inline ceiling and add no advisory step).

## Measured basis (re-measured at `6d55c575`, not carried from the draft)

`ci.yml` = 1,846 lines, 46 inline `run: |` blocks at this base (the extraction pin's floor is 40;
the increment's six new blocks take it to 52 at HEAD). Jobs:
`changes` `:38`, `record-cleanliness` `:71`, `citation-existence` `:97`, `contracts` `:111`,
`control` `:145`, `coverage-*` `:601/:654/:691`, `daemon` `:960`, `legacy-route-audit` `:1229`,
`daemon-windows` `:1365`, `integration` `:1526`, `compose` `:1758` (to `:1846`).

| fact | measured value | source |
| --- | --- | --- |
| `browser/tests` | 13 specs + 4 fixture modules | `ls` |
| render gate | run step `:479-533` (7 spec names), executed-set assertion `:535-556` (`--expect-file` ×7 at `:543-549`, 3 pinned titles at `:550-553`) | grep |
| unwired specs | 6: connections (1,489 L), work-planning (875), compose-daemon (577), compose-restart (206), chat-live (531), live-daemon (524) | inventory gate output |
| unwired node suites | 3: `connection-spec-env`, `control-instance-isolation`, `goal-context-fixtures` | inventory gate output |
| inventory gate before | `suites=16 wired=7 manual=9 unclassified=0`, exit 0 | captured run |
| compose job today | smoke `:1776` (stack torn down by the script's `EXIT` trap), rootless witnesses `:1778-1830`, allowlisted evidence upload `:1832`; no pnpm/browser step | sed |
| `compose` job gate shape | `needs: changes` + `if: needs.changes.outputs.run-heavy == 'true'` | `:1759-1760` |
| new-job constraints | header `^  ([a-z][a-z0-9-]*):$`; `needs` must contain `changes`; `if` exactly `needs.changes.outputs.run-heavy == 'true'`; `legacy-route-audit` the only unconditional job | `test-ci-changed-path-classifier.py:403` |
| `run-heavy` predicate | every non-`docs/**`, non-`*.md` path (`browser/**` included) | `classify-changed-paths.py:11` |
| inline ceiling | 80 body lines per `run: \|` block; advisories keyed on `continue-on-error` only | `test-ci-structure-extraction.py:31,363` |
| JSON-report reporter | active when `SYMMETRY_PLAYWRIGHT_JSON_REPORT` is set, else absent | `browser/playwright.config.mjs` |
| driver phase sources | `run-compose-daemon-tests.ps1` (98 L), `run-connection-fixture-tests.ps1` (270 L), `run-planning-tests.ps1` (250 L) | read in full |

Per-spec executed shape (the executed-set assertion's inputs, measured from the specs):

| spec | projects | by-design skips | executed set the assertion pins |
| --- | --- | ---: | --- |
| `portal-connections.spec.mjs` | desktop + mobile | 0 (22 tests) | all 11 case titles in both projects (the review-fix stage extended the one title) |
| `portal-work-planning.spec.mjs` | desktop + mobile | 0 (36 tests) | all 18 case titles in both projects (same) |
| `portal-compose-daemon.spec.mjs` | desktop only (driver base) | 0 (1 test) | its one case title in desktop-chromium |
| `portal-compose-restart.spec.mjs` | desktop only (driver base) | 0 (1 test) | its one case title in desktop-chromium |

The compose run keeps the drivers' `--project=desktop-chromium` invocation base: the daemon spec
skips mobile *inside the spec*, and the drivers' executed sets are the acceptance base the
contract names. The two fixture families keep both projects, as their drivers do.

## Change 1 — the compose family runs in the `compose` job, on a stack this job provisions

The `compose` job gains, after the rootless evidence upload (P1: the smoke script's own stack dies
in its `EXIT` trap, so the browser steps provision a second stack under their own project name
`symmetry-compose-browser-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}`):

1. pnpm/node setup, `pnpm install --frozen-lockfile`, and
   `pnpm --filter symmetry-browser-acceptance exec playwright install --with-deps chromium`
   (the runner's browser; the stack itself is built by `docker compose`).
2. `Run the Compose daemon acceptance on a stack this job provisions` — `docker compose
   --project-name … --file compose.yaml up --build --detach` with a CI-only `$RUNNER_TEMP`
   second file pinning `PHX_HOST=127.0.0.1` for `control`/`control-migrate` (round-2 ruling a:
   the documented target is `127.0.0.1:4000` and the published port binds IPv4 loopback only, so
   the stack's endpoint host is aligned to the documented origin instead of switching the spec
   URL to `localhost`), wait for `/healthz` (`curl --retry-all-errors --retry 90`, the drivers'
   retry pattern) and for the `daemon` service running, then
   `portal-compose-daemon.spec.mjs --project=desktop-chromium` with `SYMMETRY_COMPOSE_DAEMON=1`,
   the driver's URL/token defaults, and the JSON report asserted by
   `assert-playwright-json.py --expect-skipped 0 --expect-project desktop-chromium
   --expect-file portal-compose-daemon.spec.mjs "<its one title>"`.
3. `Verify the control restart preserves the Compose workspace` — the driver's restart phase,
   ported: identify the `DC%` project with `docker compose exec -T postgres psql`, take the
   driver's eight-table `md5(jsonb_build_object(…))` fingerprint, `docker compose restart
   control` under the same two-file model, wait for `/healthz` (`--retry-all-errors --retry 30`),
   re-take the fingerprint and require byte equality, then run
   `portal-compose-restart.spec.mjs --project=desktop-chromium` with
   `SYMMETRY_COMPOSE_PROJECT_ID/KEY/NAME` and its own `--expect-file` + title assertion.
4. `Tear down the Compose acceptance stack` — `if: always()`, the smoke script's cleanup body:
   `ps`, `logs --no-color`, `down --volumes --remove-orphans`, each `|| true`.
5. `Upload the Compose acceptance reports` — `if: always()`: both JSON reports, the Playwright
   HTML report and the default `symmetry-playwright-results` output directory under the runner's
   scratch temp path.

Both browser steps stay under the extraction pin's 80-line inline ceiling (27 and 48 body
lines), keep the script's `set -Eeuo pipefail`, and use `docker compose exec -T` (no TTY on a
runner). The driver's stack is *not* reused between steps: the browser stack is brought up inside
step 2 and torn down in step 4.

## Change 2 — a new `browser-fixture-linux` job for connections, planning and the node suites

Header exactly:

```yaml
  browser-fixture-linux:
    needs: [changes, contracts]
    if: needs.changes.outputs.run-heavy == 'true'
    runs-on: ubuntu-latest
```

`contracts` is a real prerequisite: the job downloads the `contracts` job's `portal-assets`
artifact into `control/priv/static/portal` (the dev instance serves the SPA shell) and its
`control-release-linux` artifact (the planning phase's release; the archive already carries the
portal assets). The job declares the same `postgres:18.6-alpine` service as the `control` job and
job-level `POSTGRES_HOST/PORT/USER/PASSWORD`; each phase names its own database.

Steps:

1. checkout · download both artifacts · `erlef/setup-beam` (elixir 1.20.4 / otp 29.0.6) · pnpm
   setup + node 24 · `pnpm install --frozen-lockfile` · Playwright chromium.
2. `Build the Control instance the fixture families serve from` (`working-directory: control`):
   `mix deps.get` + `mix compile` — the same explicit dev-build compile the control job performs
   for the planning fixture ("the release serves; the fixture only writes the row").
3. `Run the browser node suites` (`working-directory: browser`):
   `node --test scripts/connection-spec-env.test.mjs
   scripts/control-instance-isolation.test.mjs scripts/goal-context-fixtures.test.mjs`.
   On Linux the isolation suite's six harness cases carry their own recorded
   `skip: "the harnesses and their recording stub are Windows-only"` and its one standalone case
   still runs (the file holds seven cases: the standalone one plus three generated per harness
   for the two harnesses); the other two suites run fully (the env suite drives the real
   Playwright CLI, which is installed). The wiring step names all three for the inventory gate.
4. `Run the connection fixture acceptance` — the driver's whole sequence in one self-contained
   step: mint a per-run operator token + fixture identity, write the empty steering document,
   stage the fixture log in `$RUNNER_TEMP`, export the driver's `SYMMETRY_FIXTURE_*`,
   `SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS=3600000` and `POSTGRES_DB=symmetry_0007e_test_4021`
   (the run-owned name in the reserved family), `mix ecto.create` + `mix ecto.migrate`, start
   `PHX_SERVER=true mix run --no-halt --no-start ../scripts/ci/portal-connection-fixture.exs` from
   `control/`, wait for the identity file (identity + port), the fixture's
   `connection fixture transport installed` marker, its logged identity and `/healthz`, then run
   `portal-connections.spec.mjs` (both projects) and assert its JSON report
   (`--expect-skipped 0`, both `--expect-project`, `--expect-file`, all 11 case titles). `trap …
   EXIT` stops the fixture process on every exit path (the driver's `finally`). The step then
   repeats the driver's canary scan: neither provider canary may appear in the fixture log (the
   step merges the fixture's stderr into it) or in the provider-call log, which must also be
   non-empty.
5. `Run the planning acceptance on the Control release` — extract the release archive, create and
   migrate `POSTGRES_DB=symmetry_0007c_test` (the driver's default name; dev `mix` for the create
   step and the fixture), mint `SYMMETRY_OPERATOR_TOKEN`/`SECRET_KEY_BASE`, set `DATABASE_URL` for
   the release and export `PHX_HOST=127.0.0.1` before the eval (the release evaluates the prod
   runtime config for every command, `eval` included), with `PHX_SERVER=true PORT=4030` on the
   start line, plus `SYMMETRY_CONTRACTS_DIR`,
   `SYMMETRY_GOALS_ROLLOUT_ENABLED=true` (the render gate's primary-release posture),
   `SYMMETRY_PLANNING_HARNESS=1` and `SYMMETRY_FIXTURE_MIX=mix` (no `mise` on the runner), start
   `bin/symmetry_control start` after `eval 'SymmetryControl.Release.migrate()'`, wait for
   `/healthz` (`--retry-all-errors --retry 60`), run `portal-work-planning.spec.mjs` (both
   projects) and assert its JSON report with its own `--expect-file` and all 18 case titles.
   `trap … EXIT` kills the release.
6. `Upload the browser fixture evidence` — `if: always()`: the two JSON reports, both logs, the
   fixture transport log and the Playwright report/output directories.

Both phase steps stay under the inline ceiling (60 and 30 body lines). The node-suite step is
the job's first phase on purpose: it proves the harness contracts (env classification, per-instance
build paths, snapshot markers) before the browser phases spend the release/build time.

## Change 3 — the README matrix and the inventory agree exactly

`browser/README.md`'s `## Manual-only suite matrix`: the intro is rewritten to state the wired
set — the render step's seven specs, the `compose` job's two Compose specs, and the
`browser-fixture-linux` job's two fixture specs plus three `node --test` suites — and the four
matrix rows for the newly wired specs and the three node suites are dropped. The two remaining
rows stay with their reason (a real daemon this matrix never starts) and their driver, as the
contract's clause 1 requires. Gate-read cells keep exactly one backticked suite path each.

## Change 4 — the E6-deferred conversions (W4-14's remainder, call-compatible against its module)

W4-14's E6 recorded the four excluded specs' helper copies as "owned by W6-02". This slice
converts every copy that has a counterpart in `readable-fixtures.mjs` — the shared module itself
is not edited (the contract's failure control) — and records the copies that do not:

| spec | removed local helper | replacement | notes |
| --- | --- | --- | --- |
| `portal-connections.spec.mjs` | `login:109` | shared `login` behind a spec-local `connectionsLogin` preset: `login(page, "/portal/connections", { assert: [heading] })`, with the spec's own `assertFixtureTarget()` still called first (the preset is the W4-14 `shellLogin` pattern) | keeps the fixture-identity proof before every login, adds the shared 30 s session guard |
| `portal-connections.spec.mjs` | `api(page, path, {method, body}):117` | shared `api(page, method, path, body)` at every call site (method spelled explicitly) | return shape loses the unused `text`/`contentType` fields; the null-body cases (`{}` from the shared helper) have no consumer that distinguishes them |
| `portal-connections.spec.mjs` | `createProject(page):132` | shared `createProject(page, { key, name, description })` with `uniqueKey("C")` for the key | call sites take `project.id` |
| `portal-connections.spec.mjs` | `deviceContextOptions(testInfo):233` | shared `deviceContextOptions(testInfo, { baseURL: portalOrigin })` | byte-identical option composition |
| `portal-connections.spec.mjs` | `bindResourceThroughApi:176` | shared `attachResource(page, projectId, resource)` at its nine call sites, with the API field names (`external_ref`, `connection_id`) spelled at the site | same POST route, same 201 assertion, same returned `body.resource`; the copy both E6's census and this plan's first pass missed — converted in the apply stage (simplify finding F2) |
| `portal-compose-daemon.spec.mjs` | `uniqueProject:20` | shared `uniqueKey(prefix)` + local name minting | same key grammar |
| `portal-compose-daemon.spec.mjs` | `projectUrl:31`, `workUrl:35`, `servedColumnTitles:51` | shared imports of the same names | `projectUrl` was a byte-identical copy; `servedColumnTitles` gains the shared 200 + project assertions |
| `portal-compose-restart.spec.mjs` | `workUrl:8` | shared import | byte-identical route |
| `portal-work-planning.spec.mjs` | — | nothing removable | its local `columnOf`/`moveByKeyboard` and its `seedProject` preset have no shared counterpart; adding one would mutate the W4-14 module, which the failure control forbids. Recorded in the evidence file as the remaining remainder. |

Remaining spec-local helpers with no shared counterpart (`columnOf`, `moveByKeyboard`,
`itemDetail`, `openWork`, `fixtureDescription`, `workItemDetail` — in the three specs) stay where
they are and are listed in the final report as the recorded remainder; the shared module and the
render-gate specs are untouched.

## Change 5 — the compose specs' strict-mode locator scoping (round-3 ruling)

The failing run's page snapshot showed the daemon spec's four replayed-create evidence texts
repeated by the execution timeline (`TimelineEntry` renders each event headline as a `<p>`), so
the unscoped `getByText` calls tripped Playwright's strict mode at the first of them and every
later assertion in the F1-F7 flow stayed unexecuted. The assertions are now scoped to the
`Latest accepted evidence` region with their text unchanged; the waiting-item `Owner` fact is
scoped to the `Outcome` region in both compose specs (the `Assignment` section renders a second
`dl.detail-facts` with the same row), and the control-bar `Retry` locators use `exact: true` so
the history error state's `Retry history` button cannot collide. The rest of both specs' text
locators were read against the frontend components and resolve to one element in the state each
runs in; the mutation check is the real-Chromium locator-shape harness described in the evidence
file.

## Evidence plan

- Foreground local gates: `python3 scripts/ci/test-ci-changed-path-classifier.py` (the new job's
  gating is machine-checked); `python3 scripts/ci/assert-browser-suite-coverage.py` before/after
  (16 suites: 7 → 14 wired, 9 → 2 manual, no overlap, no unknown entry); `node --check` on every
  touched `.mjs`; `bash -n` on every new inline body; the pwsh drivers parsed (no edit, recorded);
  `pnpm --filter symmetry-browser-acceptance lint` and a Playwright `--list` parity run
  (per-spec test counts and titles) for the converted specs; `node --test` over the three node
  suites on this host (isolation suite runs fully on Windows); `git diff --check`; the DLP greps
  (the profile-directory fragment, backslash-u) over the diff.
- Mutations (the contract's judging checks): (a) an assertion step with one `--expect-file`
  removed against a two-file report reddens (`unexpected …`); (b) an assertion pointed at a file
  the run did not produce reddens (`missing …`); (c) a README matrix row added back for a wired
  suite reddens the inventory gate (`both named by … and listed`). (a)/(b) run locally against
  synthetic reports built from the new shape — the real CI reports are the conductor's oracle;
  (c) runs locally on the edited README; (d) the connection phase's trailing canary guard runs
  locally against synthetic logs — a provider canary in the control log, an empty provider-call
  log and a missing provider-call log each redden it, and the clean pair is green. The
  2026-11-22 review rulings add: (e) one non-first case dropped from the connections/planning
  report reddens the full-title assertions while HEAD's single-title invocations stay green on
  the same report; (f) a provider canary in the provider-call log reddens the restored guard
  while the HEAD guard stays green; (g) a socket server resetting its first connections kills
  the HEAD probes (exit 56) while the final `--retry-all-errors` probes retry through and exit 0.
  The round-2 rulings add: (h) the daemon step's override heredoc, extracted byte-for-byte and
  merged through `docker compose config`, changes exactly the two `PHX_HOST` values
  (`localhost` → `127.0.0.1`); (i) the replay-branch shapes in real Chromium — the route kept
  through the branch passes, while HEAD's release-before-branch shape fails at the poll with
  `Received: undefined`.
  The round-3 ruling adds: (j) a real-Chromium replica of the failing snapshot's structure —
  every unreleased locator shape strict-fails (`strict mode violation`) while each scoped or
  exact shape resolves once and stays visible.
- CI is the slow oracle (green `compose` browser steps, green `browser-fixture-linux`, unchanged
  render gate, the inventory step green): conductor-verified on the PR.
- Host limitations, recorded with the exact commands: no Docker daemon and no PostgreSQL on this
  host, so the Compose stack, the connection fixture instance and the planning release cannot be
  executed here; the two ps1 drivers' fixture sequences are likewise unavailable (they need the
  same stack, `mise` Elixir/Node on PATH and staged portal assets). The CI job's commands are the
  executed versions of these.

## Control-flow preservation (lesson 32)

The per-family base-vs-HEAD audit — each driver phase, early exit, `$LASTEXITCODE` check and
`finally` block mapped to the workflow step or retained driver — lives in the evidence file,
together with each spec's own preconditions (identity env, steering file, `SYMMETRY_PLANNING_HARNESS`,
`SYMMETRY_FIXTURE_MIX`, `SYMMETRY_COMPOSE_DAEMON`, `SYMMETRY_COMPOSE_PROJECT_*`) mapped to the new
jobs' setup. The drivers themselves stay as the local entry points (no owned-file clause fires).

## Out of scope (with reasons)

- Fixture/helper consolidation beyond the conversions above and the render-gate spec set — W4-14.
- jsdom duplicates — W5-08. The inventory gate's implementation — W2-01. CI program extraction —
  W6-01 (the new job's bodies stay inline, under its ceiling, because new `scripts/ci` programs
  are outside this slice's owned set). Credo/coverage flips — W6-10; wall-clock thresholds —
  W6-12; route code splitting — W6-07; `frontend/**` — untouched.
- No driver deletion: no family proved unwireable, so D7's delete fallback does not fire.
