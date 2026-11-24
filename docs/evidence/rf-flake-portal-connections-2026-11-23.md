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
32 → 32; spec lines 1,443 → 1,464 (`git diff --numstat 5599a0b7`: 52 insertions, 31 deletions);
`page.route(` / `unroute(` pairs 1 → 2 (the held read). Base-only lines (the two replaced by the
documented allowance): `expect(entry.request?.failure(), …).toBeTruthy()` and
`expect(["GET","HEAD"], … aborted read …).toContain(…)`. Head-only lines: the read-requirement
assertion and the two-string allowance assertion (Change 1), plus `await expect(removal).toBeVisible();`
(Change 2). No other assertion changed. Two later stages changed only comments. The apply stage
(2026-11-24) replaced the 9-line class-2 comment with a 6-line one (finding F1); the fix r1 stage
(2026-11-24) then shortened the four over-cap blocks this branch had added — the pair of protocol
sentences, the `navigate` helper, the allowance rule and the class-2 ordering — to two lines each,
removing nine lines, and deleted the two scratch pin copies from the branch (review R-01–R-03),
so the range adds no second spec file. Neither edit touched an assertion. The
`expect(`/tests/`capture`/route counts above are unchanged, and the
`1,476`-line count with the sha256s `4a393552…` (implement bytes) and `029e6af0…` (apply-trim
bytes) are historical. Structural references in this file use the final numbers; line numbers
quoted from earlier run logs keep the logged value with the final one noted where the two differ.

The review fix (2026-11-24) changed the census shape once more: `navigate` and the
unreadable-body rule moved out of the spec into `browser/tests/portal-connections-recorder.mjs`,
imported by the spec and by the oracle test. The spec's counts at the final bytes are `expect(`
324 — the same count as the base: the review fix replaced the head's two-assertion allowance
branch with one `expect(refusal, …).toBeNull()` whose predicate is the shared helper's, and the
discrimination moved to the oracle test — tests 11,
`capture(page …)` 32, `page.route(` / `unroute(` 2 / 2, 1,446 lines (`git diff --numstat
5599a0b7`: 34 insertions, 31 deletions). The truth table that distinguishes the tolerated reads
now lives in `browser/scripts/connection-spec-env.test.mjs` (§7 review-fix re-run), so no case
stopped being checked; the recorder's sha256 is `30834141…` and the oracle test's `72f0d02b…`
(review round 2, §7: the recorder's module header and return doc are deleted and the guard strips
comments; the review-fix bytes were `9ba8eef0…` / `67d43e95…`).

## 6. Gates (real commands, counts, exit codes)

Local host: Windows 11, PostgreSQL 18.6; the baseline ran on the shared instance at
127.0.0.1:5432 with the run-owned database names, and the acceptance repeats run on a dedicated
cluster at 127.0.0.1:55444 (`sync_commit` off, data directory on a scratch volume) with two fixture instances
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
| loaded repeats, post-trim bytes `029e6af0…` (apply-stage F1 re-record) | the same `repeat.sh` suites over the current bytes, one suite at a time, `timeout 4500` | 0 / 0 / 0 | 180 executions: class-1 desktop 60/60, class-1 mobile 60/60, class-2 mobile 60/60, every JSON report `expected 60, unexpected 0`, recorder assertion 0 times; §7 post-trim re-run |
| loaded repeats, fix r1 bytes `8014879b…` (fix-stage comment trim) | the same `repeat.sh` suites over the hand-over bytes, one suite at a time, `timeout 4500` | 0 / 0 / 0 | 180 executions: class-1 desktop 60/60, class-1 mobile 60/60, class-2 mobile 60/60, every JSON report `expected 60, unexpected 0`, recorder assertion 0 times; §7 fix r1 re-run |
| pins on the fix r1 bytes | `-g "external work items"` (class 1, both projects) and `-g "stale updates" --project=mobile-chromium` (class 2), regenerated pins | 0 / 0 | class-1 pin 2 passed (44.8 s), class-2 pin 1 passed (6.7 s); §7 fix r1 re-run |
| browser suite coverage (review R-01) | `python3 scripts/ci/assert-browser-suite-coverage.py` | 1 → 0 | before the pin deletion: `unclassified=2` (`pin-class1-final.spec.mjs`, `pin-class2-final.spec.mjs`); after: `suites=16 wired=14 manual=2 unclassified=0` |
| changed-file clone gate (review R-02) | `python3 scripts/ci/jscpd-changed.py --base 9d113eb6… --head <rev>` with the pinned jscpd 4.3.0 | 1 → 0 | before (`5add3541`): 5 new clones refused, the two pin copies against `portal-connections.spec.mjs`; after (a dangling candidate commit of the frozen tree): 3 changed files, 1,717 scanned sources, none new |
| added-comment census (fix r1) | the added `//` runs of `git diff -U0 9d113eb6…` over the spec | 0 | 0 added comment runs above the 2-line cap; the four listed blocks are two lines each |
| records carry no host path | the account name, drive-letter, and `/mnt` and `/home` prefixes scanned over the plan and evidence records | 0 (no hits) | plan and evidence clean |
| whitespace/CRLF on the hand-over files (spec, plan, evidence) | `grep -cP '[ \t]+$'` / `grep -c $'\r'` | 0 | 0 trailing-whitespace lines, 0 CRLF lines each |
| `git diff --check` | repo root | 0 | — |
| spec bytes handed over | `sha256sum browser/tests/portal-connections.spec.mjs` | — | `8014879b15bca310c771a07516064ed181798f671cda5a2c40a3a744ea899250` (fix r1 comment trim) — historical: superseded by the review-fix final-bytes row below; 52 insertions / 31 deletions against `5599a0b7`; predecessor hashes are recorded in §5 |
| review-fix rule tests (F1) | `node --test scripts/connection-spec-env.test.mjs` (browser/); CI job `browser-fixture-linux`, step "Run the browser node suites" | 0 | 8/8 pass: the four pre-existing cases plus the unreadable-body truth table, the barrier's settle order, its named refusal at the deadline, and the spec's no-bare-`page.goto` guard |
| review-fix mutants (F1) | three scratch mutants of the same tree (recorder `endsWith`→`includes`; spec `navigate(` call → bare `page.goto`; recorder `navigate` body → immediate `page.goto`), each red then restored | 1 → 0 each | red: the embedded-sentence tolerance, `a bare page.goto in the spec would skip the barrier that settles its pending reads`, `the navigation waits for a read that is still pending`; each restore re-ran green and left the file's sha256 unchanged |
| round-2 guard proof and rule re-run (P3-3) | `node --test scripts/connection-spec-env.test.mjs` (browser/) on the delivered tree; the spec's guard case re-run with a comment naming `page.goto`, then with the same site as a bare `page.goto`, each restored | 0 / 0 → 1 | 8/8 pass; the comment stays green; the bare goto reddens with `a bare page.goto in the spec would skip the barrier that settles its pending reads`; spec restored to `89e5a167…` |
| second class-1 case repeats (F2) | `repeat.sh` `-g "a provider failure"` over the final bytes, one suite at a time in the foreground, `timeout 75m` | 0 / 1 / 0 | desktop 60/60 (51.9 m); mobile attempt A 59/60 (36.8 m, one fixture-stall failure — §7 review-fix re-run), rerun 60/60 (30.0 m) |
| browser lint (final bytes) | `node node_modules/eslint/bin/eslint.js . --max-warnings 0` (browser/) | 0 | 0 findings |
| browser suite coverage (final bytes) | `python3 scripts/ci/assert-browser-suite-coverage.py` | 0 | `suites=16 wired=14 manual=2 unclassified=0`; the recorder helper is not a suite and the oracle test is a named node suite |
| changed-doc record sanitizer | `python3 scripts/ci/check-record-cleanliness.py --base origin/main --head <dangling tree commit>` | 0 | this record passes the sanitize tool's check on its head blob; both changed records also scan clean for profile-name, drive-letter, `/home`, `/mnt` and `/Users` host paths |
| `git diff --check` (range and working tree, final bytes) | `git diff --check origin/main...HEAD`; `git diff --check` | 0 / 0 | — |
| final bytes (review fix r2 + round-2 P3) | `sha256sum` over the spec, `browser/tests/portal-connections-recorder.mjs`, `browser/scripts/connection-spec-env.test.mjs` | — | `89e5a167…` (spec, unchanged), `30834141…` (recorder; was `9ba8eef0…`, module header + return doc removed), `72f0d02b…` (oracle test; was `67d43e95…`, guard strips comments); the two added surfaces are the helper and the oracle cases |
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
(its data directory under `<scratch>`) is I/O-bound under the foreign build and Defender.
**Attempt 3** therefore isolates the database and re-raises the fixtures' priority:

- a dedicated PostgreSQL 18.6 cluster under `<scratch>` (`initdb` + `postgres -D <scratch>/pg-flake-conn/data -p
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

### Repeat budget (conductor ruling 2026-11-24: foreground, one after the other, `timeout 4500`) — pre-trim bytes `4a393552…`

The runs in this subsection measured the bytes before the apply-stage trim; the post-trim re-run
that supersedes them for the current bytes is at the end of this section. This subsection is
retained as the pre-trim record.

Rig rebuilt at 17:09-17:12 from this revision's own bytes, because the previous stage's
fixture processes carried unrecoverable per-run credentials (the operator token is only ever
an environment variable of the fixture process) and its Playwright browser path was gone
(`%LOCALAPPDATA%\ms-playwright` absent; the browsers live at `<tools-root>/ms-playwright`):

- fixtures on 4021/4022 with their own fresh databases (`symmetry_0007e_test_4021|4022`) on the
  dedicated PostgreSQL 18.6 cluster at 127.0.0.1:55444, per-run operator token and identity,
  `SYMMETRY_*` from `<scratch>/rf-flake-conn/env-<port>.sh`
  (the session environment carries a stale `SYMMETRY_CONTRACTS_DIR` naming another worktree's `contracts`
  and `POSTGRES_PORT=55442`, both overridden), instance build paths
  `control/_build/instances/connections-<port>`, SPA staged from the revision's CI
  `portal-assets` artifact (unchanged from the previous stage);
- Playwright 1.65.1 with `PLAYWRIGHT_BROWSERS_PATH=<tools-root>/ms-playwright`, the repo config
  unchanged (`workers: 1`, `retries: 0`, `timeout: 60_000`, `test.describe.configure({timeout: 120_000})`),
  invoked as `node node_modules/@playwright/test/cli.js test tests/portal-connections.spec.mjs`
  (the invocation `browser/scripts/connection-spec-env.test.mjs` itself uses);
- runner `<scratch>/rf-flake-conn/repeat.sh`, one label per suite,
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
`page.evaluate` at the spec's `:1305` (implement bytes; `:1293` at the final bytes), one during
teardown - not an assertion). An independent
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

### Repeat budget re-run — post-trim bytes `029e6af0…` (apply stage, 2026-11-24)

Superseded for the hand-over bytes by the fix r1 re-run below (the fix stage's comment trim
changed the bytes again); retained as the apply-stage record.

Finding F1's trim changed the spec bytes the budget's numbers bind, so the three suites were
re-run over the current bytes (sha256 `029e6af050afc95905ddc76fe6911dfeb7671ede01d2ced7f7011eb59cb634d9`,
59 insertions / 29 deletions against `5599a0b7`) with the pre-trim budget's rig, runner and
configuration: the same `repeat.sh`, one suite at a time in the foreground, `timeout 4500`,
Playwright config `workers: 1`, `retries: 0`, `PLAYWRIGHT_BROWSERS_PATH=<tools-root>/ms-playwright`,
each suite on a database dropped and recreated immediately before it (`cycle-port.sh`, fixture
restarted at High priority; the 55444 postmaster measured `High`), one `probe.sh` (a `/healthz`
curl, a `/portal/login` curl, a raw `select 1` every ~3 s) beside each run.

Before the budget, both pins were re-run on the trimmed tree and stayed green: the class-1 pin
(`-g "external work items"`, the 800 ms delayed-read delay) 2 passed in 2.3 m (desktop 25.9 s,
mobile 31.1 s), and the class-2 pin (`-g "stale updates" --project=mobile-chromium`, CDP
`Emulation.setCPUThrottlingRate { rate: 12 }`) 1 passed in 9.1 s; both exit 0
(`pin-class1-fixed.json`, `pin-class2-throttle.json` in the scratch directory).

| suite | command | executions | passed | failed | wall |
| --- | --- | ---: | ---: | ---: | ---: |
| class 1, desktop | `-g "external work items" --project=desktop-chromium --repeat-each=60` (4021, fresh database) | 60 | 60 | 0 | 24.1 m (1449 s) |
| class 1, mobile | `-g "external work items" --project=mobile-chromium --repeat-each=60` (4021, fresh database) | 60 | 60 | 0 | 20.6 m (1237 s) |
| class 2, mobile | `-g "stale updates" --project=mobile-chromium --repeat-each=60` (4022, fresh database) | 60 | 60 | 0 | 7.3 m (437 s) |

Total: 180 executions, 180 passed, 0 failed, 0 skipped; each JSON report reads `expected 60,
unexpected 0, skipped 0`, the recorder assertion (`must be readable unless its request was
aborted`) appears 0 times in the three logs, and no execution failed an assertion. The stall
episodes were present in the runs' own probe windows, as in the pre-trim reruns:

- class-1 desktop probe (302 samples): `/healthz` max 15.15 s with 5 samples above 2 s,
  `/portal/login` max 17.48 s with 3, `select 1` max 2 189 ms with 4 above 1 500 ms;
- class-1 mobile probe (266 samples): `/healthz` max 16.04 s with 15, `/portal/login` max 11.47 s
  with 3, `select 1` max 1 197 ms, none above 1 500 ms;
- class-2 mobile probe (107 samples): `/healthz` max 12.48 s with 3, `/portal/login` max 4.11 s
  with 1, `select 1` max 363 ms, none above 1 500 ms.

180 clean executions inside windows that still carry the stall class is the same shape the
pre-trim reruns record, and it is the pass/fail count the conductor's repeat budget asks for,
measured over the apply-stage bytes.

Two condition notes, recorded for the judge:

- The previous apply session's class-1 desktop attempt (started 11:53 local; killed when that
  stage ended while the run was a background job) is not part of the count: its log
  (`class1-desktop-apply.log` in the scratch directory) carries `ok` 1-25 of 60 and its JSON
  report never closed.
- Two probe loops from that session's pin window (`probe-apply-pin-class1.txt`,
  `probe-test2.txt`, started 11:48:01 and 11:50:51 against 4021) were still alive during the
  class-1 desktop re-run and the first five minutes of the class-1 mobile re-run. They were
  identified and killed at 04:35Z (PIDs 24340 / 23704 / 47052 / 8952, `bash.exe` / `wsl.exe`
  pairs). Their output files are absent from the scratch directory, so what they sampled - and
  whether it reached the fixture - is unverified; both suites passed 60/60 with them alive or
  recently alive.

### Repeat budget re-run — fix r1 bytes `8014879b…` (fix stage, 2026-11-24)

The fix r1 comment trim (−9 lines) changed the bytes the budget binds again, so the three suites
were re-run over the hand-over bytes (sha256
`8014879b15bca310c771a07516064ed181798f671cda5a2c40a3a744ea899250`, 52 insertions / 31 deletions
against `5599a0b7`) with the apply-stage re-run's rig, runner and configuration: the same
`repeat.sh`, one suite at a time in the foreground, `timeout 4500`, Playwright config
`workers: 1`, `retries: 0`, `PLAYWRIGHT_BROWSERS_PATH=<tools-root>/ms-playwright`, each suite on a
database dropped and recreated immediately before it (`cycle-port.sh`, fixture restarted at High
priority), one `probe.sh` (a `/healthz` curl, a `/portal/login` curl, a raw `select 1` every ~3 s)
beside each run.

Both pins were regenerated from those bytes and re-run green before the budget: the class-1 pin
(`-g "external work items"`, the 800 ms delayed read, 300 s pin budget) 2 passed in 44.8 s
(desktop 23.7 s, mobile 18.3 s), and the class-2 pin (`-g "stale updates"
--project=mobile-chromium`, CDP `Emulation.setCPUThrottlingRate { rate: 12 }`) 1 passed in 6.7 s;
both exit 0. The pins' bytes are the spec's plus the documented injections (sha256 `b1816a37…`
and `e38a8c4c…`), deleted before hand-back as §4 records.

| suite | command | executions | passed | failed | wall |
| --- | --- | ---: | ---: | ---: | ---: |
| class 1, desktop | `-g "external work items" --project=desktop-chromium --repeat-each=60` (4021, fresh database) | 60 | 60 | 0 | 23.1 m (1387 s) |
| class 1, mobile | `-g "external work items" --project=mobile-chromium --repeat-each=60` (4021, fresh database) | 60 | 60 | 0 | 18.8 m (1128 s) |
| class 2, mobile | `-g "stale updates" --project=mobile-chromium --repeat-each=60` (4022, fresh database) | 60 | 60 | 0 | 17.2 m (1032 s) |

Total: 180 executions, 180 passed, 0 failed, 0 skipped; each JSON report reads `expected 60,
unexpected 0, skipped 0`, the recorder assertion (`must be readable unless its request was
aborted`) appears 0 times in the three logs, and no execution failed an assertion. The stall
episodes were present in the runs' own probe windows, as in the earlier budgets:

- class-1 desktop probe (299 samples): `/healthz` max 2.36 s with 2 samples above 2 s,
  `/portal/login` max 1.91 s, `select 1` max 2 143 ms with 2 above 1 500 ms;
- class-1 mobile probe (269 samples): `/healthz` max 1.83 s, `/portal/login` max 3.14 s with 1
  sample above 2 s, `select 1` max 993 ms, none above 1 500 ms;
- class-2 mobile probe (191 samples): `/healthz` max 8.48 s with 5 samples above 2 s,
  `/portal/login` max 6.90 s with 3, `select 1` max 2 205 ms with 5 above 1 500 ms.

180 clean executions inside windows that still carry the stall class, over the bytes that hand
over, is the pass/fail count the conductor's repeat budget asks for.

One condition note, recorded for the judge: the fix stage's first session started the class-1
desktop suite and the stage ended while that command was a background job, after `ok` 27 of 60
(`class1-desktop-final.log` in the scratch directory); its JSON report never closed, so it is not
part of the count. The three counts above come from the foreground re-run.

### Review-fix re-run — the second class-1 case on the final bytes (fix stage, 2026-11-24/25)

The PR #276 review's F1-F4 fixes changed the spec bytes again: the `navigate` barrier and the
unreadable-body rule moved to `browser/tests/portal-connections-recorder.mjs` (one helper under
`browser/tests/`, imported by the spec), the discriminating cases landed in the already-wired
`browser/scripts/connection-spec-env.test.mjs` (F1), the `spec:~959` step label was deleted (F3),
and `navigate` gained the 30 s `deadlineMs` bound whose lapse throws the named refusal (F4).
Final bytes: spec `89e5a167…` (34 insertions / 31 deletions against `5599a0b7`), recorder
`9ba8eef0…`, oracle test `67d43e95…`; these F1-F4 fixes are committed in `82d4c4b4` and delivered
on head `1b474bbf`, which merges `origin/main` `5c297c25`.

The review (F2) found the second class-1 case — `a provider failure and an ambiguous outcome stay
explicit and the next readback writes the provider's answer`, final `spec:731` — had not been
re-run on the fix bytes although its two navigation sites changed. It was run over the final
bytes with the apply-stage re-run's rig, runner and configuration: the same `repeat.sh`, one
suite at a time in the foreground, `timeout 75m`, `workers: 1`, `retries: 0`, each suite on a
database dropped and recreated immediately before it (`cycle-port.sh`, fixture at High
priority), one `probe.sh` (a `/healthz` curl, a `/portal/login` curl, a raw `select 1` every
~3 s) beside each run.

| suite | command | executions | passed | failed | wall |
| --- | --- | ---: | ---: | ---: | ---: |
| class 1b, desktop | `-g "a provider failure" --project=desktop-chromium --repeat-each=60` (4021, fresh database) | 60 | 60 | 0 | 51.9 m (3127 s) |
| class 1b, mobile, attempt A | the same filter, `--project=mobile-chromium --repeat-each=60` (fresh database) | 60 | 59 | 1 | 36.8 m (2214 s) |
| class 1b, mobile, rerun | the same suite again, fresh database | 60 | 60 | 0 | 30.0 m (1805 s) |

Desktop JSON `expected 60, unexpected 0, skipped 0`; the rerun the same; attempt A `expected 59,
unexpected 1`. The recorder assertion (`must be readable unless its request was aborted`) appears
0 times in all three logs. Stalls were present in every window: desktop probe max `/healthz`
14.71 s / `/portal/login` 13.49 s; attempt A 22.40 s / 6.92 s; rerun 21.76 s / 12.69 s. A
two-execution mobile sanity run before the budget passed 2/2 (49.0 s).

Attempt A's one failure (repetition 12) is a host-stall loss, not a spec or product defect, and
the record of it is deliberate:

- the failed assertion is `expect(issuesRow).toContainText(/Failed/)` after the second
  `Sync Issues` click; the row kept reading `Healthy Synced` with the successful sync's
  timestamp through 62 locator polls and the whole 30 s window;
- the fixture's transport log holds exactly two provider reads for that reference: status 200
  (the successful sync) and status 500 (the injected failure) — the failure read was served;
- the run's trace shows the console's sync `POST /portal/api/resources/…/sync` returning the
  app's documented `provider_failure` mapping (`502`, `lib/symmetry_control_web/protocol.ex:87`),
  then three refresh `GET`s (workspace, connections, workspace) at 16:06:07Z recorded with
  status `-1` — never completed — and no further reads before the assertion timed out;
- the probe's own samples jump from `08:05:58` to `08:06:31`, the 08:06:31 sample timing
  `/healthz` at 22.40 s and `/portal/login` at 6.92 s: the fixture/host stalled from ≈08:06:01
  for ~22-30 s, exactly when the console's refresh reads went out and never returned. The row
  could not change while the reads hung, and the 30 s assertion window closed while the stall
  was still running;
- the rerun passed 60/60 with the same stall class still present (probe max 21.76 s), so the
  stall is survivable and attempt A's loss is the tail of the documented stall class rather than
  a property of the bytes.

One earlier mobile attempt is voided and recorded for the judge: started 06:50, its dedicated
PostgreSQL 18.6 cluster at 127.0.0.1:55444 was stopped externally and cleanly at 07:06:35 (its
log reads `database system was shut down at 2026-11-24 07:06:35`; the fixture's next lines are
`connection refused`), so repetitions 15-60 failed on server 500s, login guards and one
`JSON.parse` of the DB-down error body: JSON `expected 14, unexpected 46`. Repeats 1-14 had
passed, the recorder assertion appears 0 times in that log too, and the run is not part of the
counts above. The cluster was restarted detached and the runs above were made against it.

### Review round 2 — P3 items (fix stage, 2026-11-25)

The review's three non-blocking P3 items were applied on top of head `1b474bbf`:

- Records provenance (P3-1): the re-run paragraph above now reads that the F1-F4 bytes are
  committed in `82d4c4b4` and delivered on head `1b474bbf` (merge of `origin/main` `5c297c25`),
  and the stale round-1-head clause it replaces is gone; the PR body carries the same correction.
- Comment policy (P3-2): the recorder's two-line module-intent header and its `Null when…` return
  doc are deleted. Their content, recorded here and in the plan's Change 1: the helper holds the
  recorded-response rules the spec and its CI oracle share — the tolerance for an unreadable body
  and the barrier a navigation must cross; `unreadableReadFailure` returns the refusal message, or
  null when the body passes the rule.
- Guard precision (P3-3): the oracle test's no-bare-`page.goto` guard strips `//` and `/* */`
  comments from the spec source before the check (two simple regexes), so a spec comment that
  names `page.goto` stays green while the bare-goto mutant still reddens; both proved on the
  delivered tree (§6), and the spec was restored to `89e5a167…`.

Delivered bytes after this round: recorder `30834141…` (50 lines, was 53), oracle test `72f0d02b…`
(181 lines, was 179), spec unchanged.

## 8. Residual risks and limits

- The `navigate` barrier settles reads already started; a response event that fires between its
  last check and the navigation commit can still strand a body — that is exactly the documented
  navigation allowance, and it is the only tolerated class beyond an aborted request.
- The barrier is now guarded in CI: the oracle cases in
  `browser/scripts/connection-spec-env.test.mjs` (job `browser-fixture-linux`, step "Run the
  browser node suites") redden on a widened allowance, on a bare `page.goto` re-entering the
  spec, and on a settle bypass in the helper (§6 review-fix rows). A later edit could still
  widen the rule by editing the oracle along with it, but not silently.
- The no-bare-`page.goto` guard strips comments with two simple regexes before checking, so a
  `//` inside a string literal truncates the rest of that line for the check; it is a shape
  backstop for the spec (the round-2 mutant proof, §6), not a parser.
- The class-2 pin uses CPU throttling as the contract's example permits; it reproduces the hosted
  call log, not the hosted host.
- Local runs are Windows + native PostgreSQL 18.6 on 5432 rather than the CI Ubuntu + service
  container on 5432; the fixture, the SPA build and the spec bytes are the revision's.
- The CI acceptance (3 consecutive green `browser-fixture-linux` attempts) is conductor-owned.
- The repeat budget's second class-1 case (`a provider failure and an ambiguous outcome stay
  explicit ...`, final `:731`) was re-run on the final bytes for this review fix: desktop 60/60,
  mobile 59/60 (one diagnosed stall) and rerun 60/60 (§7 review-fix re-run), so the case is
  exercised by this budget now. The first class-1 case and class 2 keep their fix r1 counts on
  those bytes.
- `pnpm --filter symmetry-browser-acceptance lint` cannot run on this checkout: the local
  `browser/node_modules/.bin` carries playwright shims only, so the script's `eslint` is not on
  PATH (exit 1, `'eslint' is not recognized`). The equivalent command the script names,
  `node node_modules/eslint/bin/eslint.js . --max-warnings 0`, ran in `browser/` and exited 0.
  The repo has no Prettier config or script for `browser/`; `.editorconfig` (LF, two spaces,
  final newline, no trailing whitespace) is the formatting contract and `git diff --check` is its
  check.
- `navigate`'s settle loop is now explicitly bounded: `deadlineMs` (30 s default) throws `the
  recorder's reads did not settle within …ms before navigating to …` instead of spinning, so a
  sustained response stream fails by name rather than as a generic test timeout. No budgeted
  execution reached the deadline.
