# rf-flake-portal-connections plan — a deterministic `portal-connections.spec.mjs`: the confirmed unreadable-body class, a navigation barrier, and the trace-proven concurrent-removal race

Contract: `goal-rf-flake-portal-connections` (queued 2026-11-23 by the conductor from CI evidence
and the read-only scout diagnosis `DiagConnFlake`). Base revision
`6369546b0bdf86534f241728a067f85de046fd85` (`refactor(platform): remove the test-binary fork from
containment launch (#269)`; tree clean at dispatch). Shape references:
`docs/plans/2026-11-22-rf-w6-02-browser-spec-wiring-plan.md` (the nearest browser-acceptance
slice: the CI job that runs this spec, its per-project executed set and its evidence-record
convention) and the gdd flake contracts `goal-flake-ledger-2` / `goal-flake4-closure` (the
diagnose-before-close and TODO-discipline shape).

## Owned set (STRICT: re-derived at this head)

`browser/tests/portal-connections.spec.mjs` · the slice plan (this file) ·
`docs/evidence/rf-flake-portal-connections-2026-11-23.md` · `docs/TODO.md` (only if an entry
closes or a debt is recorded — none does; see "Out of scope"). No helper file is added: the
navigation barrier lives in the spec itself, which is the only file that calls it. No shared
module, no driver, no `ci.yml` (W6-12 owns it), no other spec is touched.

## Measured basis

CI occurrences of `browser-fixture-linux` (the job that runs this spec on both projects with
`workers: 1`, `retries: 0`), all re-read from the run's own `connection-fixture-report.json`:

| run | head | failing cases (project) | class |
| --- | --- | --- | --- |
| `45188959450` | `1d22f710` | external work items (desktop), stale updates (mobile) | 1, 2 |
| `45185181408` | `c400a468` | external work items (desktop), provider failure (mobile), stale updates (mobile) | 1 ×2, 2 |
| `45194981264` | `6369546b` (this base) | external work items (desktop), provider failure (mobile), stale updates (mobile) | 1 ×2, 2 |

Class-1 error text, as the report records it (the assertion's own message; `entry.unreadable` was
never printed, which is why this slice prints it):

```
Error: response /portal/api/work-items/80dcb389-2e17-48ca-9323-d87c3db8e9dd must be readable unless its request was aborted
expect(received).toBeTruthy()
Received: null
Error: response /portal/api/connections must be readable unless its request was aborted
Error: response /portal/api/workspace must be readable unless its request was aborted
```

Class-2 trace (`gh run download 45188959450 -n browser-fixture-45188959450-1`, the mobile stale
case's `trace.zip`; the same sequence is in `45185181408` and `45194981264`):

```
14:01:11.810 DELETE 200 /portal/api/connections/a9798a10-…?version=2   (the spec's own out-of-band delete)
14:01:12.030 GET    200 /portal/api/connections   } the operator socket's invalidation for that
14:01:12.030 GET    200 /portal/api/workspace     } delete, batched 200 ms (realtime.ts DEFAULT_BATCH_MS)
  click on the removal dialog's confirm button: "element is not stable" (t=+2.357 s)
  "element was detached from the DOM, retrying" (t=+2.411 s)
  → `locator.click: Test timeout of 120000ms exceeded`; the page snapshot has no dialog and "Select a connection"
```

The gap `DELETE 200` → invalidation read pair is 219 ms (`45188959450`), 227 ms (`45185181408`) and
223 ms (`45194981264`). The product mechanism is fixed and intended: the server publishes one
`portal:invalidations` frame for the committed delete
(`control/lib/symmetry_control_web/plugs/portal_invalidations.ex`), the client maps `connection` to
`[connectionsReadKey(), workspaceReadKey()]` (`frontend/src/app/read-cache.ts`) and re-reads; the
re-read no longer serves the deleted connection, `selected` becomes `undefined`, the page clears
the URL selection (`frontend/src/features/connections/connections.tsx`), and `ConnectionDetail` —
which hosts the removal dialog — unmounts under the in-flight click.

## Change 1 — class 1: the confirmed error strings, a navigation barrier, and exactly two tolerated errors

Confirmed on this host against the fixture target (both pins below), with `request.failure()`
`null` in every case:

- navigation: `Error: response.text: Protocol error (Network.getResponseBody): No resource with
  given identifier found\nResponse body is not available for a response that was navigated away
  from. Read response.body() before triggering any navigation.`
  (the sentence is Playwright 1.65.1's own rewrite in `Response.internalBody`,
  `playwright-core/lib/coreBundle.js:14885-14903`, over the `Network.getResponseBody` protocol
  error built at `:38181-38185`);
- torn-down session: `Error: response.text: Target page, context or browser has been closed`
  (`coreBundle.js:13206`).

Changes in `browser/tests/portal-connections.spec.mjs`:

1. `const navigate = async (page, recorded, url)`: awaits every body read already started —
   including ones that land while it waits — and only then performs the document navigation.
   Used at all six former `page.goto` sites (the three `page.waitForLoadState("networkidle")`
   waits that existed for those navigations — two commented, one bare — are gone with them; the
   remaining network-idle waits precede project-scope changes and stay).
2. `assertNoCredentials`'s unreadable branch keeps the read requirement (`GET`/`HEAD`), keeps the
   aborted-request allowance, and adds exactly two tolerated strings for a read whose request did
   not fail: the navigation sentence (as a suffix, because Playwright prefixes the protocol error)
   and the torn-down-session message. Every message now prints `entry.unreadable`; every other
   unreadable body stays a failure.

Trade-off (contract Change 2): a body the page discarded is not scanned for canaries. Its headers
were already scanned, every other completed response body still is, and the CI job's own
server-side scans (the step's canary guard over the fixture log and the provider-call log, plus
the spec's DOM/storage/cookie/request/console scans) are unchanged.

## Change 2 — class 2: the reconcile that unmounts the dialog is held until the refusal is shown

The stale-updates case opens the removal dialog before the out-of-band `DELETE` and holds the
`GET /portal/api/connections` read that delete's invalidation triggers (the repo's own held-read
idiom, `portal-execution-control.spec.mjs`'s `heldReads`). That read lands ~220 ms after the row is
written (`DELETE 200` → read pair, the failing traces) and unmounts the removed connection's
detail — the open dialog with it — so a submission racing it is refused by the page instead of by
the server. The hold is released with `route.continue()` once the `(not_found)` notice is asserted;
the released read is the reconcile the row-count assertion observes. No timeout, no retry, no
`force`, no assertion removed: the dialog still freezes the version the operator saw, the server
still refuses it with `(not_found)`, and the notice path still tolerates a reconcile that clears
the selection first (`connections.tsx`'s `noticeFor`).

The first attempt at this Change armed the confirmation instead — `focus()` plus
`page.keyboard.press("Enter")` before the out-of-band delete — on the theory that the focused
control dispatches without the actionability wait the base click loses. The loaded repeats rejected
it: 1 failure in 57 mobile executions of the case. The artifact's screenshot shows the connections
list with no dialog and no notice, and the keypress lands ~10-40 ms after the delete's *response*,
so a roundtrip slower than the ~200 ms invalidation batch unmounts the button before the `Enter`
reaches the body. Narrowing the window did not close it; holding the read does.

The same case's *stale-update* phase has the same window one step earlier, and the loaded repeats
found it: at repetition 18 the console's `Save` returned **200** instead of the expected `409`,
because the rename's own invalidation (200 ms batch, `connections` + `workspace`) reached the page
between the test's out-of-band `PATCH` and its `Edit capabilities` click, so the editor froze the
*renamed* version and its submission was current. The editor is now opened and the capability
toggled **before** the other operator's rename, so the frozen version is the one the server
refuses; every assertion stays (the `(stale)` refusal, the disabled name field, the renamed row
after the refusal's re-read). This matches the sibling resource cases, which already open their
dialog before the other operator's move.

## Evidence plan

- Class-1 pins (scratch copies of the spec, deleted before hand-back; the deltas are one injected
  `await new Promise((resolve) => setTimeout(resolve, 800))` before the recorder's `response.text()`
  and a 300 s pin-only test budget, because foreign `rustc` builds saturate this host — the
  property under test is the assertion, not the wall-clock): base bytes at
  `-g "external work items"` → red with the printed navigation string; HEAD bytes → green (the
  barrier's wait settles the delayed reads before each `goto`). Standalone scratch pins print the
  torn-down-session string and the response-landing-during-a-navigation residual (a routed
  response released after the navigation starts), which the barrier cannot prevent and the
  allowance admits.
- Class-2 pin (scratch copy of the spec + CDP `Emulation.setCPUThrottlingRate { rate: 12 }` on the
  mobile project, installed immediately before the submission dispatches): base bytes → red with
  the hosted call log; HEAD bytes → green, repeated; re-run on the final bytes that hold the
  invalidation read (the fix this plan settles on).
- Mutation rows: an unreadable `POST` still reddens the assertion (the read-only requirement);
  removing the `navigate` barrier reddens the delayed-read pin; a base-bytes run under the loaded
  repeats.
- Loaded repeats on the final bytes under the repeat budget the conductor's 2026-11-24 ruling
  set (it replaces the whole-spec `--repeat-each=20` / focus `--repeat-each=50` pair above):
  two foreground runs, one after the other, each bounded by `timeout 4500` - class 1 as the
  `-g "external work items"` test on both projects and class 2 as `-g "stale updates"
  --project=mobile-chromium`, `--repeat-each=60` each, at the repo's CI worker count
  (`workers: 1`, `retries: 0`; `--workers=4` is not usable here - parallel executions of the
  class-1 test share the fixture's steering document and its `resetFixtureState()` clears a
  sibling's `issues` override mid-flight). Two fresh fixture databases on the dedicated
  PostgreSQL at 127.0.0.1:55444; the host's standing foreign load is the "host loaded"
  condition. Recorded in the evidence record's repeat-budget section.
- Assertion census base vs HEAD over the spec.
- `browser/` lint (`eslint . --max-warnings 0`) and `git diff --check`.
- CI on the PR: `browser-fixture-linux` green on 3 consecutive attempts (conductor-verified).

## Out of scope (with reasons)

- `ci.yml`, the drivers, the shared fixture modules, other specs (contract owned set).
- Any product change: the reconcile that unmounts the deleted connection's detail is the fixed
  design's "server's current state" behavior the case itself asserts, and `noticeFor` already
  tolerates the selection clearing before a refusal lands; the class-2 defect is the test's
  ordering assumption, not a UI bug, so no DECISION_NEEDED. Observed boundary, recorded here
  rather than chased: when the reconcile lands *before* the operator submits, the open dialog
  unmounts with its subject and no notice names the concurrent removal; a submission that beats
  the reconcile is refused by the server (the keyboard runs proved that path), and the case's
  contract is the refusal path.
- `docs/TODO.md`: no entry closes (the flake was tracked in the contract, not in TODO) and no new
  debt needs filing; the canary-scan trade-off is recorded above and in the evidence record.
