# 0007b readable work views — repair receipt

Status: **authorized blockers fixed; remaining limits recorded**. Round 1
resolved the descriptive-contract conflict and reported the goal blocked on two
verified predicates; round 2 applied exactly those two authorized changes (an
additive authenticated HTML route for the work-detail deep link, and an anchored
extension of the login `return_to` allowlist). No protocol, wire, shape,
pagination, permission or session-lifetime semantics changed, and every path the
allowlist rejected before is still rejected. Non-browser gates, the focused
release-shell browser gate, the broad browser suite and the fresh-session deep
links were re-run on the rebuilt release. Owner visual acceptance is **awaiting
the owner's verdict**; the model does not supply that verdict.

Base and unchanged commit: `343dea6a62484d73e7b577a0be2683ca233510f6`.
The implementation and verification are on the uncommitted working tree; no
commit, push, or history rewrite was performed.

## Findings and authorized boundary

| ID | File / surface | Change or disposition | Evidence |
| --- | --- | --- | --- |
| F1 | `.tmp/0007b-findings.md`; `contracts/v1/`, `contracts/ts/`, `frontend/src/` | Valid. Added descriptive work-detail and timeline schemas, generated DTOs and Effect boundaries, and consumed those boundaries in list/board/detail/history. No handwritten parallel wire DTOs. Implemented project filters, saved views, truthful cost/CI/acceptance labels, desktop drawer/narrow page, and cursor-on-demand history. This was the round-1 blocker; round 2 re-runs its surfaces on the current revision. | Contract, frontend, Go, and Elixir results in this receipt; browser evidence is separate from schema proof. |
| F2 | Authorized resolution in `.tmp/0007b-findings.md` | Applied as a scope rule, not dismissed as an invalid finding. Regenerated `contracts/generated/**` and `daemon/internal/contracts/{generated.go,schema_bundle.json}`; added matching fixtures and passive read-schema dispatch. `control/lib/**` stayed untouched by round 1; round 2 then made only the two changes authorized below. | Generation/drift gate and cross-language fixture decoders; all production read responses keep their original wire contract. |

### Round 2 authorized blockers, fixed

| ID | Surface | Change | Evidence |
| --- | --- | --- | --- |
| F1 | `control/lib/symmetry_control_web/router.ex` | Added `get "/work/:work_item_id", PortalController, :index` to the existing authenticated `/portal` scope, next to the project routes. Additive SPA-shell document route only; no JSON/protocol/route-shape change. | `portal_controller_test.exs` serves the built entry with request CSRF at `/portal/work/work-123`, and still 404s `/portal/work` and `/portal/work/work-123/not-a-route`; live release answers `302` → `return_to=%2Fportal%2Fwork%2F<id>%3Fview%3Dboard`. |
| F2 | `control/lib/symmetry_control_web/portal_session.ex` | Extended the strict `return_to` allowlist in its anchored style: `@shell_path` gained `work/<id>`, and `shell_query?/1` accepts exactly the work surface's own search encoding (`view=list\|board`, `q`/`state`/`owner` text values in URLSearchParams form, `blocked=1\|%221%22`, `projectId=<id>` on the work route only). Any other path, parameter or value still maps to `/portal`. | `portal_session_test.exs` round-trips the work, filtered-project and blocked-flag URLs through login and invalid-credential retry (five new paths), and rejects 13 new work/project query cases while keeping every earlier rejection; live release `GET /portal/projects/<id>?view=board&q=keeper` answers `302` with the full path, `?view=evil` and `?q=//example.com` answer `return_to=%2Fportal`. |

Regression proof for both: with the unmodified `HEAD` copies of the two files
restored in the test image, the two focused suites report `18/20 passed` /
`Failed: 2 tests`; with the fixed files they report `20 passed`. The four
fresh-session browser cases that failed both widths in round 1 now pass (below).

A direct probe of `PortalSession.return_to/1` with the exact encodings the shell
writes returned each filtered URL unchanged
(`/portal/projects/p-1?view=board&q=Human+decision&state=in_progress&owner=agent%3Anix&blocked=%221%22`,
`/portal/work/w-1?view=list&q=%22x%22&projectId=p-1`, `/portal/work/w-1?blocked=1`)
and still rejected `blocked=%220%22`, an unknown parameter and `q=%0d%0aX`.

## Maintained measurements

This receipt is the single source for the run's current measured counts. Older
`test-ablation-*` receipts describe their own historical revisions and are not
live consumers of this run.

| Measurement | Baseline | Round 1 | Round 2 (current revision) | Measurement method |
| --- | ---: | ---: | ---: | --- |
| Frontend unit tests | 33 passed | 62 passed | 62 passed (unchanged) | Baseline archive Vitest run; final `pnpm --filter @symmetry/frontend test`. |
| Contract runtime tests | 31 passed | 32 passed | 32 passed (unchanged) | Baseline archive and final `node --test contracts/scripts/*.test.mjs`. |
| Draft 7 root schemas | 15 | 17 | 17 (unchanged) | Contract generator/checker; common definitions are not a root. |
| Manifest fixtures | 190 | 213 | 213 (unchanged) | Final schema/Effect gates exercise every manifest entry. |
| Browser declarations / spec files | 41 / 8 | 47 / 9 | 47 / 9 (unchanged) | Baseline and final Playwright discovery. |
| Expanded desktop + mobile browser cases | 82 | 94 | 94 (unchanged) | Executed `playwright test --list` in round 1; the round-2 broad run executed all 94 (79 passed + 15 skipped). |
| Largest project snapshot in the completed measurement run | Not measured | 56,632 bytes / 15 work items | 74,388 bytes / 15 work items | `snapshotMetrics` in the final list/board cases; exact response bytes, not a string-length estimate. Round 2's larger value is the same unchanged endpoint in the shared development database after more fixtures accumulated. |

The Round 2 column is the current, authoritative value for this revision; the
Baseline and Round 1 columns are retained history, not competing current numbers.
Round 2 changed only the control-plane route/session layer, so the frontend,
contract and browser-declaration counts are expected to be identical, and the
snapshot value moves with the fixture database rather than with this change.

The baseline browser suite was **listed, not executed**. No before-run pass
count is claimed for Go or Elixir.

## Executed non-browser gates

Node commands below ran in the disposable `symmetry-0007b-node` container at
`/isolated`, using pnpm 12.17.0, Node 24.22.3, the repository's package versions,
and the verified isolated dependency graph described below. They are not a pass
of the failed original frozen-install gate.

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `docker exec -w /isolated symmetry-0007b-node pnpm contracts:generate` | 0 | Generated DTOs; artifacts copied back to the repository. |
| `docker exec -w /isolated symmetry-0007b-node pnpm contracts:check` | 0 | Typecheck, 32 runtime tests, 17 Draft 7 schemas, 213 fixtures, generated-file drift check. Effect checked all 213 raw fixtures; 209 parsed documents reached schema validation. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend typecheck` | 0 | Final decoder-sensitive and project-scope integration typecheck. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend lint` | 0 | Zero lint errors/warnings. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend test` | 0 | 7 files, 62 tests passed. jsdom reports its unimplemented `Window.scrollTo`; that warning is not browser verification. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend build` | 0 | Production assets staged; `index-D8XHWnI_.js` 656.77 kB / gzip 199.06 kB; CSS 20.92 kB / gzip 5.24 kB. Vite's greater-than-500-kB chunk warning remains. |
| `go vet ./...` in `daemon/` | 0 | Windows host vet passed. |
| `go test -json ./...` in `daemon/`, saved with `tee ../.tmp/0007b-go-all.jsonl` under `set -o pipefail` | 0 | 1,730 top-level passes, 55 top-level skips, 2,347 subtest passes; 21 packages passed, one had no tests. This run preceded the final fixture-only/nullability regeneration; the affected packages were repeated below. |
| `go test -json ./internal/contracts ./internal/protocol` in `daemon/`, saved with `tee ../.tmp/0007b-go-contracts-final.jsonl` under `set -o pipefail` | 0 | Final generated artifacts and complete fixture corpus: 73 top-level passes, 445 subtest passes, two packages passed, no failures/skips. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'mix format --check-formatted'` | 0 | Canonical formatter gate passed. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'mix compile --warnings-as-errors'` | 0 | Compile gate passed. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'mix test > /tmp/0007b-mix-full.log 2>&1; result=$?; cat /tmp/0007b-mix-full.log; exit "$result"'` | 0 | Full suite: 826 passed, 21 skipped, seed 900388; 297.85 s. Final contract-data changes were subsequently covered by the focused fixture run below. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'mix test test/symmetry_control/goals/contract_fixture_manifest_test.exs'` | 0 | Final 213-fixture manifest, one test passed, seed 768614. |

The table expands the component commands from the executed shell chains; each
successful component printed an explicit zero marker. Go event counts separate
top-level tests from subtests to avoid double-counting them.

### Round 2 gate re-runs (current revision)

The same containers and toolchains were reused on the fixed tree. Round 2
changed only `control/lib/**` and `control/test/**`; the node gates were re-run
because the isolated workspace had been left at the earlier round-1 file set,
after its gate-relevant files were synced to the current revision (306 files
verified byte-identical between `/isolated` and the working tree).

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'cd /app/control && mix format --check-formatted && mix compile --warnings-as-errors && mix test test/symmetry_control_web/portal_session_test.exs test/symmetry_control_web/controllers/portal_controller_test.exs'` | 0 | Formatter and warnings-as-errors compile clean; the two focused suites report **20 passed**. |
| Same focused command with the unmodified `HEAD` copies of `router.ex` and `portal_session.ex` restored in the test image | 2 | Pre-fix red proof: **18/20 passed, Failed: 2 tests** — the work document returns 404 and the filtered project URL is dropped to `/portal`. |
| `docker exec -w /isolated symmetry-0007b-node pnpm contracts:check` | 0 | 32 runtime tests passed, 17 Draft 7 schemas, 213 fixtures, generated DTOs reported up to date, Effect decoded all 213 raw fixtures (209 reached schema validation). |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit` clean on the synced frontend sources. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend lint` | 0 | `eslint . --max-warnings 0` clean. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend test` | 0 | 7 files, 62 passed; 9.06 s. jsdom still reports its unimplemented `Window.scrollTo`. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend build` | 0 | `vite build && node scripts/stage-assets.mjs`; 2866 modules; `index-D8XHWnI_.js` 656.77 kB / gzip 199.06 kB, `index-D7KuRjr5.css` 20.92 kB / gzip 5.24 kB; the Vite greater-than-500-kB chunk warning remains. |
| `go vet ./...` in `daemon/` | 0 | Windows host vet passed on the current revision. |
| `go test -json ./...` in `daemon/`, redirected to `.tmp/0007b-round2-go-all.jsonl` | 1 | **1,728 top-level passes, 55 top-level skips, 2,351 subtest passes, 20 packages passed, one package failed**; 311.9 s. Two `daemon/internal/app` tests failed on start deadlines: `TestGoalAdmissionLeavesProviderAccessNilForNativeHarness` ("native turn did not start", 1 s deadline) and `TestReconcilePendingRecoverySchedulesSnapshotBeforeBackoff` ("Control snapshot assignment was not started after pending recovery reconcile", 7.38 s). `daemon/**` is unchanged by both rounds except the round-1 contract artifacts, and neither failing file was edited. |
| `go test ./internal/app` in `daemon/`, alone after the full run | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/app 274.200s` — both previously failing tests pass when the host is not also running the browser suite, the live daemon fixtures and another agent's workloads. |
| `docker exec -d -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'cd /app/control && mix test > /tmp/0007b-round2-mix-full2.log 2>&1; echo "EXIT=$?" >> /tmp/0007b-round2-mix-full2.log'` | 0 | Clean full suite on the fixed tree: **826 passed, 21 skipped**, seed 357852, 423.0 s — the same totals as round 1. |

One earlier full-suite attempt, started while the broad browser suite and the
node gates were still running, reported `825/826 passed, 21 skipped, Failed: 1
test` (exit 2): `SymmetryControlWeb.ProviderActionControllerTest` "Goal provider
claim scope locks Project before Goal" failed in `setup` with
`:ets.new(:provider_action_test_state, [:named_table, :public, :set])` raising
"table name already exists"
(`provider_action_controller_test.exs:258`). That setup creates a fixed-name
named table with no `whereis` guard, and it is unrelated to the two changed
surfaces; the isolated re-run above passes. It is recorded as a contended-run
flake with its evidence, not as a passing gate.

The two `daemon/internal/app` failures follow the same contended-run pattern:
both wait on a goroutine start with a 1 s or 3 s deadline
(`harness_test.go:4601`, `recovery_isolation_test.go:625`), in files and a
package that neither round edited, and both pass in the isolated package re-run
above. The `./...` exit code stays `1` in this receipt; the isolated re-run is
recorded as a separate observation, not as retroactive green.

The freshly built assets are byte-identical to the ones the running release
serves: `index-D8XHWnI_.js`
`sha256:b45d41c17e269e0f213f06a9d8842e9a43c5ba2522fdbbcecb1a8ba39bda0f62` and
`index.html`
`sha256:331cf9b2943fc588cf148d410536b837e99ff6aadfa0824138f90aefd7b31613` in
`/isolated/control/priv/static/portal/`, `/app/control/priv/static/portal/` and
the release's own `priv`, so the release under test serves exactly this
revision's build.

pnpm's pre-run dependency verification attempted the registry on every scripted
run and failed with `ERR_PNPM_RESOLVING_NPM_RESOLVER_NETWORK_ERROR` /
TLS `HandshakeFailure` (`ajv`, `quicktype-core`); pnpm printed its own remedy,
"Set verifyDepsBeforeRun: false to skip this install", and then ran the script
anyway. The gates above report their own exit codes from the tools that ran
(`tsc`, `eslint`, `vitest`, `vite`); pnpm's warning that `node_modules` may be
out of sync is a statement about its failed verification step, not about those
assertions, and two of the runs spent minutes waiting on the unreachable
registry before the script started. The frozen-install gate therefore remains
unpassed, exactly as in round 1 — no lockfile, registry policy or TLS validation
was weakened to make it pass.

### Regression proofs and bounded repairs

- Whole valid task-scoped-command workspace/detail/timeline fixtures failed
  against the old generated bundle (exit 1, three rejected projections). Actual
  queued cancel/retry commands may have `run_id: null` and `generation: null`.
  The descriptive schemas now allow the null pair only in command-owned
  identities; event/transition identities and task generation remain strict.
- The whole legal legacy-result detail fixture failed against the old bundle
  (exit 1) at `/outcome/summary` and `/outcome/tests`. The transition API accepts
  JSON-compatible result maps and PortalJSON forwards those fields verbatim.
  Their presence remains required, their values remain open JSON, and the
  frontend narrows them before presentation. No backend normalization was added.
- The final all-fixture gates pass both regressions across Ajv, native Effect,
  Go, and the Elixir fixture validator. These are contract regression fixtures,
  not substitutes for real browser data.
- The Elixir fixture test adapts canonical ECMA-262 `\\u0000` pattern escapes
  to PCRE2 `\\x{0}` in a resolved validator copy, exactly as the existing
  production validator does. Only the test helper changed. The generated
  canonical bundle and existing workspace production validator remain intact.
- Scoped simplification removed the unused summary-state computation and an
  unused export, retained board grouping/order, and removed the empty-string
  cursor sentinel. No registry abstraction, identifier pin set, or new scheduler
  was introduced.

## Toolchain failures and verification limits

The repository lockfile, manifests, TLS validation, and supply-chain policies
were not weakened to make a gate pass.

| Command / attempt | Exit / state | Actual outcome |
| --- | --- | --- |
| `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile` (two root attempts) | Cancelled; no exit code | Parent cancelled stalled supply-chain verification. No success and no invented cancellation exit code. |
| `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend test` at the original root | Timeout; no exit code | 180.02 s in supply-chain verification; zero tests executed. |
| `mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance exec playwright --version` at the original root | Timeout; no exit code | 45 s; runner version was not obtained by this probe. |
| `mise exec pnpm@12.17.0 -- pnpm install --offline --frozen-lockfile --reporter=ndjson` | 1 | Missing official-registry metadata for `@babel/core`: `ERR_PNPM_META_FETCH_FAIL` / `NO_OFFLINE_META`. |
| `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile --fetch-timeout=15000 --fetch-retries=1 --network-concurrency=16 --reporter=ndjson` | 2 | Unsupported `--fetch-retries`; no installation. |
| `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile --fetch-timeout=15000 --network-concurrency=16 --reporter=ndjson` | 1 | Official `@babel%2Ftemplate` metadata fetch failed with TLS `HandshakeFailure`; 354.86 s. |
| Same frozen install with disposable `XDG_CONFIG_HOME=.tmp/pnpm-config` official-host-to-mirror routing | 1 | `ERR_PNPM_TARBALL_URL_MISMATCH`, 316 entries: mirror metadata URLs differed from the locked official URLs; temporary configuration removed. |
| Linux container original-lock `pnpm install --frozen-lockfile --fetch-timeout=15000 --network-concurrency=16` | 1 | Official `@exodus/bytes` metadata/tarball TLS failure; 589.43 s. |
| First Elixir read-fixture validation | 2 | PCRE2 rejected canonical `\\u0000` in the meta-schema; repaired by the test-only adaptation above. |
| First full Elixir suite with a 300 s command deadline | Timeout; no completion exit code | No final suite result; the orphaned test VM was stopped before the successful 900 s-deadline run. |
| Initial Linux browser run against `http://symmetry-0007b-control:4000` | 1 | 27 failed, one skipped; all failures were login 403 / missing Refresh, not UI assertions. Native `127.0.0.1` sessions work. The exact hostname/origin cause is unproven; backend CSRF was not changed. |
| Initial native live-daemon runner | 1 | One desktop lifecycle pass, two history failures from comparing independently signed cursors, one intentional mobile lifecycle skip. Fixed the oracle to bind the exact response consumed by the UI. |
| Old malicious-response probe | 0, invalid proof | Passed through typed-boundary rejection of a malformed injected `external` object. Replaced with schema-valid, positively rendered malicious text/URL assertions; the old pass is not security evidence. |
| Intermediate frontend checks | 1 | Initial optional-field/type errors, one unscoped text assertion, a later no-await test callback lint error, and three modal-background role locators. Corrected rather than suppressed; final gates are recorded above. |
| A command with `--filter symmetry-frontend` | No tests executed | The package is `@symmetry/frontend`; four zero-match messages were not treated as passing gates. Corrected commands appear above. |
| Round 2: `pnpm --filter @symmetry/frontend test`/`build` pre-run installs inside `symmetry-0007b-node` | Stalled, then warned | `ERR_PNPM_RESOLVING_NPM_RESOLVER_NETWORK_ERROR` / TLS `HandshakeFailure` for `ajv` and `quicktype-core`; the stalled installs were stopped instead of retried indefinitely, and the wrappers then ran the real scripts (row details in the round-2 gate subsection above). |

### Isolated dependency route

The normal-check-enabled mirror workspace preserved the four workspace manifests'
versions and pinned the four transitive versions to the original graph:
`@eslint-community/regexpp@4.12.2`, `parse5-parser-stream@8.1.0`, `parse5@8.1.0`,
and `seroval@1.7.9`. Both Linux and native isolated lockfiles were parsed and
compared with the original: all 378 package keys, integrity values, dependency
snapshots, and importer dependency records matched; only origin URLs differed.
No `trustLockfile`, TLS disabling, policy suppression, or repository lockfile
rewrite was used. Mirror checks are useful execution evidence but **do not turn
the original frozen installation into a passing gate**.

Linux/race Go qualification, native credentialed-provider qualification, and the
complete original CI/merge matrix are not established by the Windows Go and
fake-agent runs. Real daemon execution here means the production daemon/control
path with a deterministic fake-agent process, not an installed native provider.

The read API serves no acceptance identity or work-item usage/cost records.
Reported output is therefore never relabelled accepted, unknown cost is never
`$0`, and the absence label is not accepted-evidence proof. The real manual-CI
fixtures exercise unknown freshness, not the timestamp-stale provider branch;
the latter has deterministic presentation-unit coverage only.

## Executed browser gates

Native Playwright used Chromium 156.0.8078.4 with Playwright 1.64.1, the staged
production release at `http://127.0.0.1:4000`, and the verified non-junction
workspace `.tmp/0007b-tooling`. The expiry fixture was the same release at
`http://127.0.0.1:4001` with a two-second session lifetime. Desktop captures use
a 1280 by 720 viewport; narrow captures use 412 by 839. The latter's PNG pixel
dimensions are larger because of device scale factor.

| Executed command | Exit | Actual result / artifact |
| --- | ---: | --- |
| `playwright test --list` (workspace package runner) | 0 | 94 cases in nine files; discovery only. |
| `playwright test` (expanded package test script, `.tmp/0007b-tooling/browser`) | 1 | First complete native run: 70 passed, 15 skipped, nine failed; 307.13 s. `.tmp/0007b-browser-final-results` and `.tmp/0007b-browser-final-report`. All retained classic, Chat, connections, and shell cases that ran passed. |
| `mise exec pnpm@12.17.0 -- pnpm --dir .tmp/0007b-tooling --filter symmetry-browser-acceptance test tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs` | 1 | After the first test-body repairs: 29 passed, one skipped, six failed; 115.00 s. Four fresh-session failures are the backend boundary conflict; two list cases exposed the next incorrect URL-encoding assertion. `.tmp/0007b-browser-v3-results` and `.tmp/0007b-browser-v3-report`. All 16 shell cases passed, including real expiry recovery. |
| `mise exec pnpm@12.17.0 -- pnpm --dir .tmp/0007b-tooling --filter symmetry-browser-acceptance test tests/portal-work-views.spec.mjs --grep "list, board, filters and summary"` | 1 | First focused list repair: both cases reached rendered board ordering, then failed because the nested `has` locator searched for another board inside each column. 17.88 s; `.tmp/0007b-list-v4-results` and `.tmp/0007b-list-v4-report`. |
| Same exact focused command above, after correcting only that locator | 0 | Final desktop and narrow list/board/filter/saved-view/refresh/order checks: two passed, 17.55 s wall / 14.7 s Playwright. `.tmp/0007b-list-v5-results` and `.tmp/0007b-list-v5-report`. |
| `pwsh -NoProfile -File .tmp/0007b-tooling/browser/scripts/run-live-daemon-tests.ps1` | 0 | Clean native runner: three passed, one intentionally skipped mobile lifecycle case; 97.44 s. Both widths executed the real daemon history test. `.tmp/0007b-live-v3-results` and `.tmp/0007b-live-v3-report`. No dependency-preflight error in this run. |

The focused release invocation used these exact environment assignments before
the command in the table:

```powershell
$env:SYMMETRY_PORTAL_URL = "http://127.0.0.1:4000"
$env:SYMMETRY_SHELL_EXPIRY_URL = "http://127.0.0.1:4001"
$env:SYMMETRY_PLAYWRIGHT_OUTPUT = Join-Path $PWD ".tmp/0007b-browser-v3-results"
$env:PLAYWRIGHT_HTML_OUTPUT_DIR = Join-Path $PWD ".tmp/0007b-browser-v3-report"
```

The two final focused list invocations used the same portal URL and respectively
`.tmp/0007b-list-v4-results` / `.tmp/0007b-list-v4-report` and
`.tmp/0007b-list-v5-results` / `.tmp/0007b-list-v5-report` for those two output
variables. The clean live-runner invocation set its output variables to
`.tmp/0007b-live-v3-results` and `.tmp/0007b-live-v3-report`; its script starts
the real control/daemon fixture. Native live-runner logs are at
`C:\Users\lhan\AppData\Local\Temp\symmetry-goal-02-live-6ca824f6386f445d862bfaebe0566b1e`.
Each PowerShell wrapper returned `$LASTEXITCODE`.

These round-1 runs are separate observed runs, not an invented combined green
94-case run; the four fresh-session failures they recorded were the authorized
boundary conflict that round 2 fixed. The new desktop concurrency case passed;
its narrow counterpart is intentionally skipped. Other skips in the broad run
include environment-gated live/Compose suites and pre-existing mobile
concurrency exclusions; a skip is not proof.

### Round 2 runs (current release revision)

The release was rebuilt from the fixed tree (`mix release --overwrite` in
`/app/control`, then both instances restarted: primary on `:4000`, expiry on
`:4001` with `SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2`) and both suites were
re-run against it with Chromium 156.0.8078.4 / Playwright 1.64.1 from
`.tmp/0007b-tooling`.

| Executed command | Exit | Actual result / artifact |
| --- | ---: | --- |
| `mise exec pnpm@12.17.0 -- pnpm --dir .tmp/0007b-tooling --filter symmetry-browser-acceptance test tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs` | 0 | Focused release-shell gate: 36 cases, **35 passed, 1 skipped, 0 failed**; 2.9 min wall. Both fresh-session deep-link cases pass on both widths (cases 5, 6, 23, 24). `.tmp/0007b-round2-focused-results` and `.tmp/0007b-round2-focused-report`. |
| `mise exec pnpm@12.17.0 -- pnpm --dir .tmp/0007b-tooling --filter symmetry-browser-acceptance test` | 0 | Broad native run, nine spec files: 94 cases, **79 passed, 15 skipped, 0 failed**; 304.25 s wall / 5.0 min Playwright. Round 1 was 70/15/9; the nine previously failing cases all pass. `.tmp/0007b-round2-broad-results` and `.tmp/0007b-round2-broad-report`; `.last-run.json` is `{"status":"passed","failedTests":[]}`. |
| `pwsh -NoProfile -File .tmp/0007b-tooling/browser/scripts/run-live-daemon-tests.ps1` | 0 | Native live runner on the current release: **3 passed, 1 intentionally skipped** mobile lifecycle case; 95.8 s wall / 1.4 min Playwright. Both widths executed the real-cursor history case (`firstPageEntries` 100 → `firstEntriesAfterOlderPage` 121, `currentEntriesAfterRetry` 121), the same counts as round 1. `.tmp/0007b-round2-live-results` and `.tmp/0007b-round2-live-report`; logs at `C:\Users\lhan\AppData\Local\Temp\symmetry-goal-02-live-c3d449fb4b71404980b99a52f7f447ab`. |

Round-2 environment assignments (identical to round 1 except for the output
directories; the focused run used `.tmp/0007b-round2-focused-results` and
`-focused-report`, the broad run `.tmp/0007b-round2-broad-results` and
`-broad-report`):

```powershell
$env:SYMMETRY_PORTAL_URL = "http://127.0.0.1:4000"
$env:SYMMETRY_SHELL_EXPIRY_URL = "http://127.0.0.1:4001"
$env:SYMMETRY_PLAYWRIGHT_OUTPUT = Join-Path $PWD ".tmp/0007b-round2-broad-results"
$env:PLAYWRIGHT_HTML_OUTPUT_DIR = Join-Path $PWD ".tmp/0007b-round2-broad-report"
```

The two previously failing expectations now hold with their original
assertions, unchanged: `deep-link-work-reentry.json` records
`/portal/work/<uuid>?view=board` after login and reload, and
`deep-link-project-view.json` records
`/portal/projects/<uuid>?view=board&q=keeper` after login and reload, for both
widths, under these round-2 focused result directories (`<project>` is
`desktop-chromium` or `mobile-chromium`):

- `.tmp/0007b-round2-focused-results/portal-work-views-Readable-c33c8-link-over-its-project-board-<project>/`
- `.tmp/0007b-round2-focused-results/portal-work-views-Readable-77c3c--board-and-filter-deep-link-<project>/`

The 15 skips are the same environment-gated live/Compose and pre-existing
mobile-concurrency cases as round 1; the daemon-free skips are covered by the
native live runner recorded in the round-1 row above only for its own revision.

### Browser repair trail and additional tooling failures

- Round 2 added no browser-test repair: it changed only the two authorized
  backend surfaces, rebuilt the release with `mix release --overwrite` in
  `/app/control`, restarted both instances, and re-ran the untouched suites.
  The four fresh-session cases pass with their original assertions, and the
  broad suite moved from 70/15/9 to 79/15/0 (exit 0).
- The first broad run's five non-backend failures were fixture/helper return
  misuse, an unscoped drawer heading assertion, an unscoped duplicate cost-label
  assertion, and trying to use the disabled pending project picker. Repairs
  retained the ordered sections and both cost consumers, and switched projects
  through the real, usable SPA directory while the prior response stayed held.
- The subsequent list failure compared raw URL text to `1`, although TanStack
  serializes that string as JSON. It now proves the URL has the filter and that
  a reload restores the checked control, the matching row, and exclusion of the
  nonmatching row. No production serialization was changed. The final board
  oracle selects exactly one ready column, waits for all served cards, and
  compares their rendered order with the real position-sorted snapshot; the
  deliberately nonalphabetical Zulu/Mike/Alpha order is retained.
- The first native live run after the cursor repair returned exit 1: one
  lifecycle pass, two history failures, one skip (108.01 s). A legitimate copy
  of reported output in the raw timeline made a whole-detail text locator
  ambiguous. The locator now targets the reported-output section in live,
  Compose, and restart checks; the served summary/source/href assertions remain.
- A temporary junction-based workspace's focused release invocation returned
  exit 1 before tests: `ERR_PNPM_UNSAFE_TASK_RUN_STATE_PATH` (3.00 s).
- `pwsh -NoProfile -File .tmp/0007b-verification-v2/browser/scripts/run-live-daemon-tests.ps1`
  returned exit 0 with three actual passes and one skip (101.76 s), **but** pnpm
  first emitted `ERR_PNPM_OUTDATED_LOCKFILE` for the junction's local
  `@symmetry/contracts` dependency and warned that its pre-script install failed.
  Those artifacts are retained at `.tmp/0007b-live-v2-results`; that run is not
  claimed as a clean dependency gate. The final runner above uses the original
  verified non-junction workspace and has no such preflight failure. No policy
  switch was disabled to bypass either error.
- Fresh-session contexts now inherit the running project's five device options
  but no authenticated storage. Browser-error assertions still run on successful
  paths; `finally` closes the context without overwriting the primary 404 or
  lost-return-path assertion. Neither expected backend defect is allowlisted.

## Required evidence map

Capture roots below are repository-relative. Each `.png` has a same-basename
`.json` recording the actual URL, viewport, and observations. These are local
run artifacts, not checked-in screenshots or claims that a missing CI job ran.

- **L** = `.tmp/0007b-list-v5-results`
- **V** = `.tmp/0007b-browser-v3-results`
- **H** = `.tmp/0007b-live-v3-results`
- **R** = `.tmp/0007b-round2-focused-results` (round 2, current revision)
- **B** = `.tmp/0007b-round2-broad-results` (round 2, current revision)
- **H2** = `.tmp/0007b-round2-live-results` (round 2 native live runner, current revision)
- **D** / **N** = `desktop-chromium` / `mobile-chromium` in the exact directory
  suffixes below; N is the narrow-screen project, not a second desktop context.

The L/V/H rows record the round-1 observations; round 2 re-produced the same
captures on the current revision under R (focused gate), B (broad suite) and H2
(native live runner, which is the only executor of the real-cursor history
case). The two fresh-session rows below were failures in V and are re-pointed to
R, where they pass; the two history rows are re-pointed to H2.

| Required item | Desktop and narrow evidence | Observed result / limit |
| --- | --- | --- |
| Real project list | `L/portal-work-views-Readable-add7e--work-with-stable-URL-state-{D,N}/work-list.png` | Both passed. Real API-created work shows owner/state/blocker, unrecorded acceptance, unknown cost, and honest CI labels. |
| Real project board | Same directory, `work-board.png` | Both passed, five columns, 15 cards, actual nonalphabetical ready-column ordering, view retained after reload. Narrow board scrolls within its own surface. |
| Detail | `V/portal-work-views-Readable-3f6fb-on-and-history-on-real-data-{D,N}/work-detail.png` | Both passed: desktop modal drawer versus narrow full page; Outcome, Latest accepted evidence, Blocker, Next action, Execution history in order. |
| Populated history and reported result/delivery | `H2/portal-live-daemon-Goal-2--fb491-er-daemon-records-on-demand-{D,N}/work-history-pagination.png` | Both passed in round 2 on real daemon/control records; served nonempty reported summary and delivery source/links asserted, plus rendered `debug-001`. Each item's first page has 100 entries and becomes 121 after the real older page (`firstPageEntries`, `firstEntriesAfterOlderPage`, `currentEntriesAfterRetry` in the same-basename JSON). Round 1's capture is under H. |
| Empty project and history | `V/portal-work-views-Readable-6d00e-the-project-and-work-routes-{D,N}/work-list-empty.png`; detail directory above, `work-history-empty.png` | Both widths passed; empty states belong to real API-created projects/work with no history, not substituted DTOs. |
| Project/detail errors and recovery | Same `6d00e` directory, `work-list-error.png` and `work-detail-error.png` | Both passed. Deliberately abort real requests; no fabricated successful payload. Retry must render the real response afterward. |
| Stable filter/view state and keyboard navigation | Final list/board cases; `V/portal-work-views-Readable-69464-focus-return-keep-URL-state-{D,N}/keyboard-walk.json` and `.png` | Both widths passed text/state/blocked filtering, saved-view restoration, reload, list Enter, board arrow navigation, detail open and Escape/Back focus return. Recorded desktop sequence: Enter, Escape, ArrowDown, ArrowUp, ArrowRight, ArrowLeft, Enter, Escape. |
| Fresh signed-out work URL | `R/portal-work-views-Readable-c33c8-link-over-its-project-board-{D,N}/deep-link-work-reentry.json` and `.png` | **Both widths passed in round 2:** the signed-out document load redirects to login with `/portal/work/<uuid>?view=board` as `return_to`, login returns to that URL, and the reload keeps the drawer over its project board. Round 1's failure artifacts (HTTP 404) remain under V. |
| Fresh signed-out filtered board URL | `R/portal-work-views-Readable-77c3c--board-and-filter-deep-link-{D,N}/deep-link-project-view.json` and `.png` | **Both widths passed in round 2:** login `return_to` is the complete `/portal/projects/<uuid>?view=board&q=keeper`, the filtered board renders on return and after reload. Round 1's failure artifacts (received `/portal`) remain under V. |
| Project switch, detail switch, refresh, late responses | `V/portal-work-views-Readable-af807-oject-s-data-or-newer-state-desktop-chromium/late-response-guard.json` and `.png` | Desktop passed with both held real workspace and detail responses recorded as delivered; after release and the newer real refresh, no held project/work/detail data replaced current state. Narrow concurrency is not claimed. |
| Foreign claimed project in work URL | `V/portal-work-views-Readable-5094f--never-renders-foreign-work-{D,N}/work-url-canonicalization.json` and `.png` | Both passed; the actual served project owns the picker/background/navigation and foreign filters/data are cleared. |
| Older-history failure and Retry | Real-history JSON under H2 above (`faultAttemptOutcomes`, `retryCursorBoundTo`, `obsoleteFailureOutcomes`) | Both widths passed in round 2. An obsolete delivered 503 does not affect the newly selected work; a current failed older-page load renders the error and Retry, then requests the cursor from that navigation's consumed detail response and exhausts the real older page. Signed cursors from separate GETs are not compared. |
| Truth rules | List/board/detail captures above and `frontend/src/features/work/presentation.test.ts` | Unknown cost is not `$0`; missing acceptance is not accepted evidence; real manual passed-CI fixtures have unknown freshness and no green tone. Provider timestamp-stale handling has unit proof only, not a real-provider browser capture. |
| Unsafe read content | `V/portal-work-views-Readable-7f7fe-er-as-markup-or-unsafe-URLs-{D,N}/malicious-response-injection.json` and `.png` | Both passed with schema-valid, positively rendered malicious strings and consumed unsafe URLs. Explicitly synthetic fault-injection/security evidence, **not** real-data surface proof. |
| Classic parity and retirement | Migration map below; broad-run report and the final scoped runs | Executed retained classic cases pass; replaced read assertions moved to new routes, not dropped. Compose/restart additions remain unexecuted. |

The coordinator also opened the actual release in a browser and inspected the
desktop drawer, narrow work page, and narrow board, then opened the produced
list/board/detail/history/empty/error captures. This establishes inspection,
not the owner's qualitative clarity/density verdict. Full-page narrow history
captures are long; their same-basename JSON separates work identities and
phase-bound counts instead of attributing one item's cursor/count to another.

### Same-change classic-check retirement

`browser/tests/portal.spec.mjs` goes from 12 to eight declarations. Four
classic read-only declarations are retired **with their predicates migrated**:

| Retired classic declaration | Replacement |
| --- | --- |
| `detail output encodes untrusted statuses and rejects unsafe links` | New typed detail/unknown-value checks and schema-valid malicious detail test; real live output/delivery checks remain separate. |
| `a delayed detail response cannot overwrite a newer drawer selection` | New held-real-response project/detail selection test and foreign-project URL test. |
| `an older history failure cannot notify or mutate a newer drawer` | Always-on, explicitly synthetic obsolete-history guard in the new CI-run spec **and** the real daemon older-page/503/Retry case on both widths. |
| `a delayed workspace response cannot replace the latest project` | New held-real-workspace project-switch/refresh case. |

The mixed CRUD/filter/order and keyboard/dialog tests retain classic mutation
coverage; read-only list/board/filter/order and keyboard predicates move to
`browser/tests/portal-work-views.spec.mjs` (10 declarations). Remaining classic
dirty-form refresh, project/work optimistic-concurrency, archive/restore,
resource, and session-security checks are retained. The broad run executed
12 retained classic cases successfully; its four classic mobile concurrency
cases kept their pre-existing skips.

The existing CI browser command only gains
`tests/portal-work-views.spec.mjs`. `portal-live-daemon.spec.mjs`,
`portal-compose-daemon.spec.mjs`, and `portal-compose-restart.spec.mjs` also
gain new-route read assertions without removing their original control/recovery
flows. This run exercised the native live suite, **not** the Compose/restart
gates or the native credentialed-provider matrix.

## Measurement and evidence subject

Round 1's measurement command read the two completed `work-list.json` files
under L with PowerShell `Get-ChildItem -Recurse -Filter work-list.json` and
`ConvertFrom-Json`, then applied `Measure-Object -Maximum` to
`observations.snapshotBytes` and `observations.workItemCount` (exit 0); the
desktop response measured 56,351 bytes and the narrow run's independently
created project 56,632 bytes. Round 2 re-ran the same list case in both the
focused and broad suites and re-read the same fields from those four files
(desktop 56,913 / 66,861 bytes, narrow 62,168 / 74,388 bytes). Each selected
project had 15 work items, and the maintained maximum for this revision is in
the measurements table above.
This is the largest observed **project-scoped fixture snapshot in this run**,
not a scalability benchmark or a maximum supported size. Snapshot size also
includes the directory metadata served by the unchanged workspace endpoint.
Server-side work-item pagination remains absent; no list endpoint or pagination
shape was invented. History uses only the existing opaque cursor API.

Final uncommitted evidence subject (current revision), superseding round 1's
`sha256:3ddb5e44551f2d498628e8456d87a40684d6a90f0b85b215a22148b5cdb008a6`:
`sha256:aa344b7ddd3f6f5682b0a7963cd294a7d918b44200796fd8d2174af1615c9527`
(390 file entries; `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1`
exited 0). This is a source-manifest digest, not a commit hash and not a claim
that the original frozen-install gate passed. The receipt and local output
artifacts are excluded to avoid self-reference.

Reproduction algorithm: recursively enumerate files under the following roots;
add the direct files in `frontend/`, `browser/`, and `contracts/`; add the
explicit files below; deduplicate full paths. For each file compute lowercase
SHA-256 and a repository-relative path with `/` separators. Sort by path using
PowerShell `Sort-Object -Property Path -Culture ""`. Hash UTF-8 without BOM
of `hash + two spaces + path + LF` for every entry, including the final LF.

```text
Recursive roots:
frontend/src
browser/tests
browser/scripts
contracts/v1
contracts/ts
contracts/scripts
contracts/fixtures
contracts/generated
daemon/internal/contracts
control/lib

Explicit files:
package.json
pnpm-lock.yaml
pnpm-workspace.yaml
.github/workflows/ci.yml
daemon/internal/protocol/goal_contract_fixture_test.go
control/test/symmetry_control/goals/contract_fixture_manifest_test.exs
docs/goals/0007b-readable-work-views.md
docs/design/frontend.md
docs/design/quality.md
```

The production frontend and generated contracts did not change during the
last browser test-body repairs or in round 2. Round 2 changed only
`control/lib/symmetry_control_web/{router,portal_session}.ex` and their two
Elixir test files, plus one documentation paragraph in `browser/README.md`;
the browser, contract and frontend artifacts above are the same bytes as round
1's, and the round-2 runs re-executed them against the rebuilt release.

## Review disposition and remaining limitations

- Independent read-only state, schema, and acceptance reviews found real
  project-scope, modal-loading, nullable-command, legacy-result, unsafe-probe,
  cursor-binding, device-context, and negative-barrier defects. Those were
  repaired rather than exempted. The final scoped/live runs exercise their
  affected paths; schema regressions have old-bundle red and final-bundle green
  proofs. The CI spec-list addition was independently inspected.
- The final local simplification retained board grouping/order and existing
  state owners, removed only unused state-summary work/export and the cursor
  sentinel, and added no parallel cache or retry authority. Last test-only
  repairs strengthen reload, section, and rendered-order predicates rather
  than pinning serialization or selecting an arbitrary duplicate match.
- Two nonblocking review observations remain explicit: the pre-existing picker
  fallback may briefly label an available project unavailable while its query
  is pending; a scopeless narrow work navigation can fetch one additional
  project-scoped workspace directory after detail resolves. The shared detail
  query is deduplicated; the latter is not a second detail request.
- The four fresh-session failures are fixed and re-verified (above), and every
  Done-when predicate of `docs/goals/0007b-readable-work-views.md` now has
  executed evidence on this revision: the list/board/detail/history/empty/error
  browser cases with their desktop and narrow captures, the fresh-session
  re-entry of the work and filtered-project deep links, the recorded keyboard
  walk, the truth labels, the retained classic coverage with its migration map,
  and the real-cursor history counts.
- Still-unavailable evidence, recorded as limits rather than waived: the
  original frozen dependency installation, the Compose/restart browser specs
  (skipped locally, exercised by the CI `compose` job), the provider
  stale-timestamp visual branch (unit proof only), native credentialed-provider
  qualification, and the complete CI/merge matrix for the eventual PR. Nothing
  above satisfies or waives those, and none was made to pass by weakening an
  assertion, a permission, a pagination rule or a wire contract.
- Owner visual acceptance remains the owner's own verdict and is not supplied
  here; the run reports the result as produced and awaiting that verdict. No
  model-issued `achieve` is recorded, and the working tree is uncommitted by
  this stage's rules.
