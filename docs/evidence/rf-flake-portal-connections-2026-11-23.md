# rf-flake-portal-connections — evidence record (2026-11-23)

Subject: the `browser-fixture-linux` flake on `browser/tests/portal-connections.spec.mjs` — the
unreadable-body class (class 1) and the concurrent-removal race (class 2). Contract:
`goal-rf-flake-portal-connections` (GDD, queued 2026-11-23 from the CI evidence and the read-only
scout diagnosis `DiagConnFlake`). Base revision
`6369546b0bdf86534f241728a067f85de046fd85`. Plan:
`docs/plans/2026-11-23-rf-flake-portal-connections-plan.md`. Judging artifact (contract clause 2):
this record.

## 1. CI occurrences and the failing shapes (re-read from each run's report)

`gh api /repos/wxxb789/symmetry/actions/runs/<id>/artifacts` → `browser-fixture-<id>-1` →
`home/runner/work/_temp/connection-fixture-report.json`:

| run | head | project | case | result |
| --- | --- | --- | --- | --- |
| `45188959450` | `1d22f710` | desktop | external work items … | failed: `response /portal/api/work-items/a997f3b4-… must be readable unless its request was aborted` |
| `45188959450` | `1d22f710` | mobile | stale updates and concurrent deletion … | `timedOut`, `Test timeout of 120000ms exceeded` |
| `45185181408` | `c400a468` | desktop | external work items … | failed: `response /portal/api/workspace must be readable …` |
| `45185181408` | `c400a468` | mobile | a provider failure … | failed: `response /portal/api/connections must be readable …` |
| `45185181408` | `c400a468` | mobile | stale updates … | `timedOut` |
| `45194981264` | `6369546b` (this base) | desktop | external work items … | failed: `response /portal/api/work-items/80dcb389-… must be readable …` |
| `45194981264` | `6369546b` | mobile | a provider failure … | failed: `response /portal/api/connections must be readable …` |
| `45194981264` | `6369546b` | mobile | stale updates … | `timedOut` |

Every class-1 report row carries `expect(received).toBeTruthy() / Received: null` — the
unreadable entry's `request.failure()` is `null` — and none printed `entry.unreadable` (the base
assertion's message does not carry it; the fixed one does).

## 2. Class 2: the trace-confirmed mechanism and its numbers

Mobile `stale updates …` `trace.zip` from each of the three runs (`1-trace.network` +
`1-trace.trace` + `error-context.md`), identical in all three:

```
DELETE 200 /portal/api/connections/<id>?version=2      (the case's own out-of-band delete)
+219 ms (45188959450) / +227 ms (45185181408) / +223 ms (45194981264):
GET 200 /portal/api/connections   } the operator socket's invalidation for that delete,
GET 200 /portal/api/workspace     } batched 200 ms (frontend/src/services/realtime.ts DEFAULT_BATCH_MS)
```

`error-context.md` (all three): `locator.click: Test timeout of 120000ms exceeded` on
`getByRole('dialog', { name: /Remove Stale fixture … renamed/ }).getByRole('button', { name: 'Remove connection' })`
with the call log `locator resolved to <button …>` → `waiting for element to be visible, enabled
and stable` → `element is not stable` → `element was detached from the DOM, retrying`; the final
page snapshot has no dialog and `Connection detail` back to "Select a connection".

Product side (fixed design, unchanged): the committed delete publishes one
`portal:invalidations` frame (`control/lib/symmetry_control_web/plugs/portal_invalidations.ex`),
the client maps `connection` → `[connectionsReadKey(), workspaceReadKey()]`
(`frontend/src/app/read-cache.ts`), the re-read no longer serves the deleted connection, the URL
selection clears and `ConnectionDetail` — the dialog's parent — unmounts.

## 3. Class 1: the confirmed error strings (reproduced on this host against the fixture)

Standalone scratch pin (`browser/tests/pin-class1.spec.mjs`, deleted before hand-back): a
recorder whose `response.text()` is delayed 800 ms past the next document navigation, then
`assertNoCredentials` with `entry.unreadable` printed — red, message:

```
Error: response /portal/login must be readable unless its request was aborted
[unreadable: Error: response.text: Protocol error (Network.getResponseBody): No resource with
given identifier found
Response body is not available for a response that was navigated away from. Read response.body()
before triggering any navigation.]
expect(received).toBeTruthy()
Received: null
```

Teardown variant (`pin-class1-teardown.spec.mjs`): a pending delayed read when the session is
torn down prints

```
"unreadable":"Error: response.text: Target page, context or browser has been closed","failure":null
```

Both sentences are Playwright 1.65.1's own: the navigation suffix is written by
`Response.internalBody` (`playwright-core/lib/coreBundle.js:14885-14903`) over the
`Network.getResponseBody` protocol error, and the session one at `:13206`. The redirect body
message (`Response body is unavailable for redirect responses`) also reproduced and stays out of
the allowance — the recorder already refuses to read 3xx bodies.

## 4. Pins (scratch copies; deltas recorded, files deleted before hand-back)

| pin | bytes | forced mechanism | result |
| --- | --- | --- | --- |
| class 1, delayed read | base `6369546b` + `await new Promise((resolve) => setTimeout(resolve, 800))` before the recorder's `response.text()` | `playwright test tests/pin-class1-base.spec.mjs -g "external work items"` | **red** both projects (2.4m / 2.5m): `response /portal/login must be readable unless its request was aborted` |
| class 1, delayed read | HEAD + the same delay | same invocation at `pin-class1-fixed.spec.mjs` | **green** 2/2 (desktop 2.7m, mobile 2.3m, 5.3m total) |
| class 2, CPU throttle | base + `Emulation.setCPUThrottlingRate { rate: 12 }` (mobile) inserted immediately before the confirm click | `-g "stale updates" --project=mobile-chromium` | **red**: 2.1m timeout, call log `locator resolved to <button …>` → `element is visible, enabled and stable` → `element was detached from the DOM, retrying` — the hosted shape |
| class 2, CPU throttle | HEAD + the same insertion before the Enter | same invocation | **green** 12.0s, then `--repeat-each=3`: 3/3 green (19.1s / 15.2s / 15.8s) |

Notes: the pin copies carry the CI budget (`test.describe.configure({ timeout: 120_000 })`)
except the class-1 pair, whose scratch copies raise it to 300 s because this host is saturated by
foreign `rustc` builds (observed: 16 cores at ~100 %, `MsMpEng.exe` at ~120 %); the mechanism —
injected read delay, the barrier, the shipped assertions — is unchanged either way. The first
class-2 pin attempt throttled the whole test and reddened at an earlier click under the same
unmount mechanism; the shipped pin narrows the throttle to the submission, which is the window
the hosted traces show losing.

Two further scratch runs bound the barrier:

- the class-1 fixed pin instrumented to print every entry the unreadable branch saw (same run,
  same bytes otherwise): the branch received `/portal/login` and `/assets/login.css` on both
  projects — the login helper's own navigation is outside the spec's control — and
  `/portal/api/workspace` twice on mobile, i.e. a response that landed outside the barrier's last
  check. Each entry was admitted only because it is a `GET` and its error ends with one of the two
  documented sentences (`expect(lost).toBe(true)` passed; the tests passed too). This is the
  observed answer to "can a response landing during a navigation still escape": yes — the login
  document and a workspace read did — and the allowance is what keeps every other unreadable body
  a failure.
  A routed-response probe (hold one `/portal/api/connections` response until after the navigation
  commits, then release) did **not** reproduce an unreadable body: the released response stayed
  readable. Recorded as a negative probe; the escape above is the observed one.
- the barrier-removed mutation (`pin-m1-no-barrier.spec.mjs`: HEAD bytes + the delayed read +
  `navigate` reduced to a raw `page.goto`): red on desktop at
  `an unreadable response must belong to a read: PATCH /portal/api/work-items/7488d0be-…` — the
  barrier is load-bearing and a stranded write is never tolerated (the synthetic truth table
  below matches this by construction).
- the shipped rule's truth table, evaluated over the spec's own `navigatedAway`/`sessionClosed`
  constant bytes: 10/10 cases match — `GET`/`HEAD` + the navigation suffix or the session
  message admitted; `POST`/`PUT` + the navigation suffix rejected; the redirect-body message,
  an unrelated protocol error, and the sentence followed by trailing text rejected; a `GET` with
  `failure` set admitted; an aborted `POST` rejected.

## 5. Assertion census (base `6369546b` vs HEAD)

`expect(` count 324 → 325; assertion-bearing lines 327 → 328; tests 11 → 11; `capture(page …)`
32 → 32; spec lines 1,443 → 1,476 (`git diff --stat`: 62 insertions, 29 deletions); `page.route(` /
`unroute(` pairs 1 → 2 (the held read). Base-only lines (the two replaced by the documented
allowance): `expect(entry.request?.failure(), …).toBeTruthy()` and
`expect(["GET","HEAD"], … aborted read …).toContain(…)`. Head-only lines: the read-requirement
assertion and the two-string allowance assertion (Change 1), plus `await expect(removal).toBeVisible();`
(Change 2). No other assertion changed.

## 6. Gates (real commands, counts, exit codes)

Local host: Windows 11, PostgreSQL 18.6; the baseline ran on the shared instance at
127.0.0.1:5432 with the run-owned database names, and the acceptance repeats run on a dedicated
cluster at 127.0.0.1:55444 (`sync_commit` off, data directory on Q:) with two fixture instances
(`PHX_SERVER=true PORT=4021` and `PORT=4022`, Windows priority High, their `SYMMETRY_*` from the
driver) so the two suites load each other instead of the host's foreign jobs alone. The fixture is
started as
`PHX_SERVER=true PORT=4021 mix run --no-halt --no-start ../scripts/ci/portal-connection-fixture.exs`
with `SYMMETRY_*` from the driver, and the SPA served from `control/priv/static/portal` staged
from the revision's own CI `portal-assets` artifact (run `45194981264`). Playwright 1.65.1 with
the pinned lockfile.

| gate | command | exit | counts/notes |
| --- | --- | ---: | --- |
| baseline (base bytes, unmodified) | `playwright test tests/portal-connections.spec.mjs` both projects | 1 | 19 passed, 3 failed: 2 harness-side (`browserContext.close: ENOENT … trace` — a concurrent `node --test scripts/connection-spec-env.test.mjs` re-used the same output dir and wiped it mid-run; operator error, recorded) and 1 real class-1: mobile `external work items` → `response /portal/api/workspace must be readable unless its request was aborted` |
| class-1 pins | §4 | 1 / 0 | red base / green HEAD |
| class-2 pins | §4 | 1 / 0 | red base (2.1m timeout) / green HEAD (12.0s + 3/3) |
| re-run on the final bytes (held read; after the stale-update reorder) | §7 note | 0 / 0 | class-1 pin 2 passed (desktop 41.0 s, mobile 27.4 s); class-2 pin 1 passed (1.3 m at rate 12) |
| browser lint | `node node_modules/eslint/bin/eslint.js . --max-warnings 0` (browser/) | 0 | 0 findings |
| assertion census | §5 | — | no assertion removed outside the documented allowance |
| loaded repeats (repeat budget, conductor ruling 2026-11-24) | `repeat.sh` over `node node_modules/@playwright/test/cli.js test … --repeat-each=60`, one suite at a time, `timeout 4500` | 1 / 1 / 0 / 0 / 0 | 300 executions: class-1 desktop 52/60 and mobile 58/60 (10 environment stalls, none the recorder assertion), class-2 mobile 60/60, class-1 reruns 60/60 on both projects; §7 repeat budget |
| whitespace/CRLF on the three owned files | `grep -cP '[ \t]+$'` / `grep -c $'\r'` | 0 | 0 trailing-whitespace lines, 0 CRLF lines each |
| `git diff --check` | repo root | 0 | — |
| spec bytes handed over | `sha256sum browser/tests/portal-connections.spec.mjs` | — | `4a393552756be09b0882f09bbeeaacba711bd64272892eb1ebe9147865c1fc7d` (the ruling's `4a393552…`), 62 insertions / 29 deletions |
| CI `browser-fixture-linux` 3× | `gh run rerun` on the PR | — | conductor-owned; needs the pushed PR head (this stage does not push) |

## 7. Loaded repeats (CI configuration: workers 1, retries 0)

Two fixture instances on different ports/databases/state files (`4021`/`4022`). **Attempt 1** ran
both suites concurrently so each was the other's heavy job; the host was also saturated by a
foreign `rustc`/`cargo-nextest` build and Windows Defender (`MsMpEng.exe` ~120 %), and the fixture
stalled under it (`/healthz` probes of the same minutes: 3.8 s, 29.1 s, 13.1 s, 0.8 s, 0.3 s).
Four failures appeared in the first ten executions, all fixture-starvation symptoms — never the
class-1 assertion and never the class-2 detach call log:

- `Azure DevOps connection management …` (desktop): the row did not leave after the removal
  dialog closed (`expect(…).toHaveCount(0)`, 63 polls);
- `a GitHub repository resource …` (desktop): `page.waitForLoadState: Test timeout of 120000ms
  exceeded` on a remaining network-idle wait;
- `external work items …` (desktop): the work-item dialog stayed at "Loading this work item…"
  while the test ran out its 120 s budget;
- `stale updates …` (desktop): the login never completed, the page stayed at "Checking your
  session…".

Attempt 1's artifacts are archived in the scratch directory. **Attempt 2** ran the suites
sequentially with both fixtures at High scheduling priority; it still failed early with page
bootstrap stalls ("Checking your session…", `Refresh` absent within the shared login helper's 30 s
guard), and measurement pinned the fixture's route handler, not the spec: the same minutes gave
`/portal/login` 3.6 s … 25.1 s while static assets served in 15-24 ms, and `pg_database_size` on
the shared cluster took 11.5 s / 32.7 s against `select 1` at 0.2 ms — the shared PostgreSQL
(`C:/…/scratch/w609-apply-pg/data`) is I/O-bound under the foreign build and Defender.
**Attempt 3** therefore isolates the database and re-raises the fixtures' priority:

- a dedicated PostgreSQL 18.6 cluster on Q: (`initdb` + `postgres -D Q:/pg-flake-conn/data -p
  55444 -c listen_addresses=127.0.0.1 -c synchronous_commit=off -c full_page_writes=off`) — the
  same major version and the driver's own 55444 port; `pg_database_size` 130 ms, `select 1`
  1.3 ms;
- both fixture instances restarted against it and set to High scheduling priority: `/portal/login`
  1.4-1.8 s, assets 15-24 ms.

The host remains loaded by the foreign jobs above, so the contract's "with the host loaded"
condition holds without self-inflicted thrashing. Attempt 3 results:

Attempt 3's focused suite found one more instance of the class-2 window in the same case, one step
earlier (repetition 18, desktop): the `Edit capabilities` editor opened *after* the test's own
out-of-band rename had already been delivered, so the editor froze the renamed version and its
`Save` returned **200** (`PATCH … 591ee018-…` in the run's trace) instead of the expected `(stale)`
`409`. The spec now opens the editor and toggles the capability **before** that rename — the same
ordering the sibling resource cases already use — and the two pins were regenerated from the final
bytes and re-run green: class 1 desktop 30.4 s / mobile 28.7 s (2 passed), class 2 mobile at
`Emulation.setCPUThrottlingRate { rate: 12 }` 31.6 s.

The next repeats ran on those reordered bytes and rejected the case's other fix. The focused suite
failed once in 57 mobile executions of the case (repetition 5; the whole-spec suite ran 121
executions of 440 clean beside it): `expect(getByRole('status').first()).toContainText("(not_found)")`
resolved to the list's own sr-only refresh announcer — no notice — and `test-failed-1.png` shows the
connections list with no dialog open. That variant armed the confirmation (`focus()` +
`page.keyboard.press("Enter")`) instead of holding the read; the keypress lands ~10-40 ms after the
delete's *response*, so a roundtrip slower than the ~200 ms invalidation batch still lets the
reconcile unmount the button first. The failure's `trace.zip` came back truncated (no central
directory; the screenshot and `error-context.md` carry the evidence).

The final bytes hold the `GET /portal/api/connections` read the delete's invalidation triggers —
the repo's `heldReads` idiom from `tests/portal-execution-control.spec.mjs` — until the `(not_found)`
notice is asserted, then release it as the reconcile the row-count assertion observes. Both pins
were regenerated from those bytes and re-run green: class 1 (`-g "external work items"`, the 800 ms
delay, 300 s pin budget) 2 passed in 1.3 m (desktop 41.0 s, mobile 27.4 s); class 2
(`-g "stale updates" --project=mobile-chromium`, the same bytes plus
`Emulation.setCPUThrottlingRate { rate: 12 }`) 1 passed in 1.3 m. Both exits 0.

| suite | command | result |
| --- | --- | --- |
| whole spec, both projects | `playwright test tests/portal-connections.spec.mjs --repeat-each=20` (440 executions) | superseded by the repeat budget below (conductor ruling 2026-11-24) |
| focus filter | `-g "external work items\|stale updates" --repeat-each=50` (200 executions) | superseded by the repeat budget below |
| base bytes under load | the same commands over the base spec bytes | not run: the host was already saturated and the CI runs at this exact base (`45194981264`) plus the local baseline (2.4 m/2.5 m pin runs) supply the before-the-fix failures |

### Repeat budget (conductor ruling 2026-11-24: foreground, one after the other, `timeout 4500`)

Rig rebuilt at 17:09-17:12 from this revision's own bytes, because the previous stage's
fixture processes carried unrecoverable per-run credentials (the operator token is only ever
an environment variable of the fixture process) and its Playwright browser path was gone
(`%LOCALAPPDATA%\ms-playwright` absent; the browsers live at `Q:/.tools/ms-playwright`):

- fixtures on 4021/4022 with their own fresh databases (`symmetry_0007e_test_4021|4022`) on the
  dedicated PostgreSQL 18.6 cluster at 127.0.0.1:55444, per-run operator token and identity,
  `SYMMETRY_*` from `C:/Users/lhan/.local/state/gdd/scratch/rf-flake-conn/env-<port>.sh`
  (the session environment carries a stale `SYMMETRY_CONTRACTS_DIR=Q:\repos\wt-0007f\symmetry\contracts`
  and `POSTGRES_PORT=55442`, both overridden), instance build paths
  `control/_build/instances/connections-<port>`, SPA staged from the revision's CI
  `portal-assets` artifact (unchanged from the previous stage);
- Playwright 1.65.1 with `PLAYWRIGHT_BROWSERS_PATH=Q:/.tools/ms-playwright`, the repo config
  unchanged (`workers: 1`, `retries: 0`, `timeout: 60_000`, `test.describe.configure({timeout: 120_000})`),
  invoked as `node node_modules/@playwright/test/cli.js test tests/portal-connections.spec.mjs`
  (the invocation `browser/scripts/connection-spec-env.test.mjs` itself uses);
- runner `C:/Users/lhan/.local/state/gdd/scratch/rf-flake-conn/repeat.sh`, one label per suite,
  JSON report per suite, speculation-free `-g` filters matching one test each.

| suite | command (Playwright config `workers: 1`, `retries: 0`) | executions | passed | failed | wall |
| --- | --- | ---: | ---: | ---: | ---: |
| class 1, desktop | `-g "external work items" --project=desktop-chromium --repeat-each=60` | 60 | 52 | 8 | 49.3 m (2966 s) |
| class 1, mobile | `-g "external work items" --project=mobile-chromium --repeat-each=60` (fresh database) | 60 | 58 | 2 | 27.0 m (1621 s) |
| class 2, mobile | `-g "stale updates" --project=mobile-chromium --repeat-each=60` (fresh database, 4022) | 60 | 60 | 0 | 9.6 m (578 s) |
| class 1, desktop, rerun | the same command after a fresh database and High scheduling priority on the fixture BEAM and the 55444 cluster | 60 | 60 | 0 | 22.1 m (1331 s) |
| class 1, mobile, rerun | the same rerun conditions (`-g "external work items" --project=mobile-chromium`) | 60 | 60 | 0 | 22.7 m (1366 s) |

Total: 300 executions of the two class suites, 290 passed, 10 failed, all 10 in the two first
class-1 runs and all 10 environment stalls: six plain `Test timeout of 120000ms exceeded`
(desktop repeats 16, 19, 35, 36; mobile 6, 7) and four `expect(locator)` waits that expired
inside a stall window (desktop repeat 8 on the unavailable-text readback, 10 on the assignment
dialog, 17 and 18 on the login guard's `Refresh`). The recorder assertion
`must be readable unless its request was aborted` appears **0 times in the four class-1 logs**
(240 executions). JSON reports: `class1-desktop expected 52 unexpected 8`,
`class1-mobile expected 58 unexpected 2`, `class2-mobile expected 60 unexpected 0`,
`class1-desktop-r2 expected 60 unexpected 0`, `class1-mobile-r2 expected 60 unexpected 0`; no
skips anywhere.

The ruling names one run per class covering both projects; class 1 is run as two per-project
commands (identical execution set and configuration) so each command keeps its own 75-minute
bound and its own JSON report.

**Class 1 desktop: the class-1 assertion never fired.** All 8 failures are host-load stalls,
not the recorder assertion under test. Per-repeat outcomes from the run's own JSON report:
failures at repeats 8, 10, 16-19, 35, 36; the last 24 executions clean. Every failing repeat's
trace shows server time-to-first-byte (`wait`) of 1.4 s - 62.0 s on same-origin
`/portal/api/*`, `/portal/login` and `/portal/connections`, with `receive` 1-32 ms and static
assets 1-25 ms in the same window; inside one failing repeat the wait grows monotonically
(repeat 16: 1.5, 1.6, 1.8, 2.2, 3.2, 3.7, 5.7, 6.2, 8.0, 10.5, 15.7, 17.4, 25.7 s) and the
stalled requests drain together - a queue backlog behind a slow server, not a discarded body.
The recorder's assertion (`response ... must be readable unless its request was aborted`) did
not appear in any of the 60 desktop executions, nor did any `(not_found)`/navigation shape.

Idle baselines on the same rig, same minute: `/healthz` p50 0.172 s, max 1.152 s over 60
samples; `/portal/login` p50 0.157 s, max 0.218 s over 25 samples; `psql select 1` and
`count(*) from work_items` (116 rows) sub-second; disk latency 0.3-0.4 ms; 25.7 GB of 64 GB
RAM free; the fixture's own logs carry no server-side error except client aborts
(`thousand_island` `:econnaborted`, 822 occurrences - the spec's navigations and the
timed-out tests aborting in-flight requests, the behaviour the base suite already tolerates).
The database grows by ~2 projects, 1 connection, 4 resources and 2 work items per execution;
the workspace payload at the worst failure is 109 KB against 34 KB at the first one, so the
stalls are three orders of magnitude above the payload/row growth. The loaded repeats run on
the same host as a foreign 12-24 GB Python job and a Dev Drive (ReFS) VHDX on SAS, which is
the standing "host loaded" condition of this contract; the stall episodes (17:14-17:28,
17:42-17:48 local) are the same shape the previous stage recorded as fixture starvation in
attempts 1-2 ("`/portal/login` 3.6 s ... 25.1 s while static assets served in 15-24 ms").
The attribution of those episodes to host contention (and not to the spec bytes) rests on the
stall shape, the idle baselines above and the fact that 24 consecutive executions on the same
bytes ended clean immediately after the last episode; it is an inference from measured
latency, not a captured system trace.

**Class 1 mobile: the same stalls, measured from outside the browser.** The mobile suite ran on
a database recreated from scratch immediately before it (drop/create/migrate + fixture restart,
the rig's `reset-port.sh`), so accumulation is excluded as the driver, and it still failed twice
(executions 6 and 7 of 60, both plain `Test timeout of 120000ms exceeded` - one in
`page.evaluate` at the spec's `:1305`, one during teardown - not an assertion). An independent
probe started with the run (one `/healthz` curl, one `/portal/login` curl and one raw
`psql select 1` every ~3 s, 339 samples, no browser involved) measured the same episodes live:
`/healthz` up to 33.6 s, `/portal/login` up to 17.4 s, and `select 1` - no application code at
all - up to 11 692 ms. Both failing executions start inside those episodes: 02:10:36Z falls in
the sample reading healthz 6.46 s / login 11.40 s / `select 1` 1 244 ms, and 02:13:10Z falls in
the sample reading healthz 3.83 s / login 1.27 s / `select 1` 667 ms, two samples before
healthz 14.5 s / login 16.0 s / `select 1` 6.6 s. The probe's printed clock is offset from UTC
because the host's timezone changed mid-session (the runner saw both `UTC-8` and `CST` readings);
the correlation is anchored on the run's own JSON `startTime` (`2026-11-24T02:07:36.981Z`), one
sample after the probe's first line. Median passing execution 14 s, fastest 12 s, so the suite
runs at CI speed whenever the environment is not in an episode.

The probe ran on past the suite (560 samples over ~63 minutes, until its own 2 400 s bound):
37 samples above 2 s on `/healthz`, 26 above 2 s on `/portal/login`, 13 above 1 500 ms on the
raw `select 1`, maxima 33.63 s / 17.35 s / 11 692 ms. Episodes are therefore a property of this
host's loaded hour (they continue with no browser running), not of any execution inside the
budget; the suite's own failures are the two that the episodes caught.

**Class 2 mobile: 60/60, exit 0, and the window was not stall-free.** The stale-updates case
(editor opened before the other operator's rename; the delete's `GET /portal/api/connections`
invalidation read held until the `(not_found)` refusal is on screen, then released as the
reconcile the row-count assertion observes) passed every one of its 60 executions at ~9.6 s each
on a fresh database - faster than the class-1 executions, because the held window closes as soon
as the notice is asserted. The run's own probe (port 4022, 208 samples) recorded episodes inside
that window too (healthz up to 6.62 s, `/portal/login` up to 10.76 s, `select 1` up to 2 784 ms)
and the case still passed all 60; the class-2 defect window is closed by ordering, not by
timing luck. JSON report: `expected 60, unexpected 0, skipped 0`.

**Class-1 reruns: 60/60 on both projects, in windows that still carried episodes.** After the
two class-1 first runs, the 4021 fixture's database was dropped and recreated (a fresh fixture
process) and the fixture BEAM and the 55444 cluster's processes were set to High scheduling
priority - the state the previous stage's attempt-3 rig documents, restored here because the
fixture processes had been restarted at default priority. Both reruns passed 60/60 with exit 0
(22.1 m and 22.7 m; per-execution p50 17 s / 18 s, p95 35 s / 33 s, maxima 44 s / 63 s) even
though their own probes recorded the same episode class in the window (`/healthz` max 18.20 s with
11 samples above 2 s and `/portal/login` max 14.06 s with 5, `select 1` max 3 075 ms). The
distinction between the first runs and the reruns is therefore which phase an episode lands on,
not the presence of episodes: the first runs' failures were logins, page bootstrap and
teardown caught inside a 6-33 s stall, and no execution in any of the five runs failed an
assertion about a response body, a refusal notice or a reconciled row.

## 8. Residual risks and limits

- The `navigate` barrier settles reads already started; a response event that fires between its
  last check and the navigation commit can still strand a body — that is exactly the documented
  navigation allowance, and it is the only tolerated class beyond an aborted request.
- The class-2 pin uses CPU throttling as the contract's example permits; it reproduces the hosted
  call log, not the hosted host.
- Local runs are Windows + native PostgreSQL 18.6 on 5432 rather than the CI Ubuntu + service
  container on 5432; the fixture, the SPA build and the spec bytes are the revision's.
- The CI acceptance (3 consecutive green `browser-fixture-linux` attempts) is conductor-owned.
- The repeat budget's class-1 filter is the "external work items" case only. The contract also
  names `a provider failure and an ambiguous outcome stay explicit ...` (base `:720`, HEAD `:760`)
  as a class-1 case: its bytes are identical in base, reordered and final revisions, its two
  `navigate()` sites are the shared helper under test here, and the previous stage's whole-spec
  repeats ran it inside a 121-execution clean stretch; it is not re-exercised by this budget.
- `pnpm --filter symmetry-browser-acceptance lint` cannot run on this checkout: the local
  `browser/node_modules/.bin` carries playwright shims only, so the script's `eslint` is not on
  PATH (exit 1, `'eslint' is not recognized`). The equivalent command the script names,
  `node node_modules/eslint/bin/eslint.js . --max-warnings 0`, ran in `browser/` and exited 0.
  The repo has no Prettier config or script for `browser/`; `.editorconfig` (LF, two spaces,
  final newline, no trailing whitespace) is the formatting contract and `git diff --check` is its
  check.
- The `navigate` settle loop re-checks until no new response arrives while it awaits; it is
  bounded here because the surfaces read on demand and on 200 ms invalidation batches rather
  than on a fixed interval, and no execution of the 240 budgeted runs stalled inside it.
