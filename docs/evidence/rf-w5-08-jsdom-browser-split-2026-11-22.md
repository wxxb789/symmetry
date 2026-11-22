# rf-w5-08-jsdom-browser-split — evidence record (2026-11-22)

Subject: the D15 split — jsdom owns decoding/state/DOM contracts, Playwright owns layout, scroll
retention, focus return and real-network flows — with the duplicated jsdom pane-simulation
assertions deleted one red/green pair at a time and every remaining candidate recorded, either
against a surviving render-gate twin or as kept-and-named. Contract:
`goal-rf-w5-08-jsdom-browser-split.md` (GDD, 2026-11-21, binding with D15, D1, D2, the conductor
decisions of 2026-11-21 and the owner comment policy of 2026-11-22). Base revision `956ff4f6`
(`refactor(frontend): settle goals from the query and make wait budgets explicit (#251)`, this
worktree's dispatch HEAD, tree clean). Plan: `docs/plans/2026-11-22-rf-w5-08-jsdom-browser-split-plan.md`.
Judging artifact (contract clause 2, "answer.md"): this record plus the implement-stage report.

Host note: every `pnpm` invocation ran as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile
--config.verify-deps-before-run=false ...` (the workspace pins pnpm 12.17.0; the verify step is
skipped because the local install carries the three override versions below). The host's npm feed
404s `knip@6.46.1`, `@vitest/coverage-v8@5.1.1` and `msw@3.0.7`, so the one-time `pnpm install
--no-frozen-lockfile` ran with the documented temporary `overrides` block in `pnpm-workspace.yaml`
pinning exactly those three to `6.45.1`/`5.1.0`/`3.0.5`; both `pnpm-workspace.yaml` and
`pnpm-lock.yaml` were restored immediately after the install (`git status --porcelain` shows
neither file, re-checked; their md5s are the dispatch-time ones), and none of the three is a gate
tool.

Browser runs: the release-shell render gate (a built release, three instances, PostgreSQL, the
seven specs and the executed-set assertion) cannot run on this host — W4-14's recorded limitation
— so the twins were run against a local **dev-configuration** control plane: `mix phx.server` at
`127.0.0.1:4090` with `MIX_BUILD_PATH` under `control/_build/instances/w508`, a dedicated database
`symmetry_w508_test`, `SYMMETRY_GOALS_ROLLOUT_ENABLED=true`, the frontend's built assets staged
into `control/priv/static/portal`, and Chromium 1255 (`playwright install chromium`). Same
surface, same production carrier, same routes as the render gate; §10 names what only CI's release
shell can decide.

## 1. Deliverable shape (files, counts)

| file | status | lines | note |
| --- | --- | ---: | --- |
| `frontend/AGENTS.md` | edited | 24 → 28 | the D15 split rule, +4 lines (§4) |
| `frontend/src/features/chat/chat.test.tsx` | edited | 2,062 → 2,013 | one case deleted (−49 lines; 48 → 47 cases): the pair's jsdom copy (§6, §7) |
| `frontend/src/features/connections/connections.test.tsx` | **unedited** | 1,516 | 57 cases; the six focus sites are kept-and-named (§6) |
| `frontend/src/features/goals/goal-attention.test.tsx` | **unedited** | 1,321 | 29 cases; the probe's goal-attention claim does not reproduce (§3) |
| the seven render-gate specs | **unedited** | — | no twin addition was needed; no file renamed, no title touched, no case added or skipped |
| `docs/plans/2026-11-22-rf-w5-08-jsdom-browser-split-plan.md` | new | 144 | the plan (its §4 carries the settled summary; §8 below is the matrix) |
| `docs/evidence/rf-w5-08-jsdom-browser-split-2026-11-22.md` | new | — | this record |

Not edited: every `frontend/src/**` production file (all six mutations were reverted; §8 —
`git diff -- frontend/src/features/chat/use-chat-scroll-restoration.ts` is empty), `vitest.config.ts`,
`browser/**` (specs, fixtures, configs), `pnpm-lock.yaml`, `pnpm-workspace.yaml`, `docs/TODO.md` /
`docs/archive/CLOSED-TODO.md` (no entry closes), `docs/design/**` (`docs/design/frontend.md:11`
keeps "Vitest + Testing Library for behavior/unit tests; existing Playwright for cross-service
browser acceptance" — consistent with the split, not stale).

## 2. Dispatch gates (base `956ff4f6`)

| gate | state |
| --- | --- |
| `#164` (W2-15, `c6b2c352`), W3-12, W4-11 (`#220`, `e84973a7`), W4-08, W4-09, W4-10, W4-14 (`#205`, `105d6eda`) merged | yes — every named revision is an ancestor of HEAD (`git merge-base --is-ancestor`) |
| W5-03 (`#249`, `cbe4910e`) and W5-04 (`#251`, this HEAD) merged, W5-04's wait budgets in the tree | yes |
| open PRs | `gh pr list --state open` → `[]`; no open PR names an owned file |
| owned files untouched by other work | `git status --porcelain` empty at dispatch |
| the three in-scope suites at dispatch | `chat.test.tsx` 2,062 L / 48 cases; `connections.test.tsx` 1,516 L / 57 cases; `goal-attention.test.tsx` 1,321 L / 29 cases |

## 3. Re-derivation of the contract's probe against HEAD

1. **The census reproduces.** `chat.test.tsx` carries the 48 `scroll` mentions and 3 `focus`; the
   `connections.test.tsx` 23 `focus` mentions are its six focus cases; `getBoundingClientRect`,
   `IntersectionObserver` and `ResizeObserver` are 0 in all three suites.
2. **Goal attention does not reproduce T12.** `goal-attention.test.tsx` has 0 `scroll`, 0 `focus`
   and 0 layout-observer mentions, and the goal surfaces' jsdom cases assert served state
   (attention rows, plan admission, versions, receipts), not simulated interaction; no goal
   deletion was invented, and the non-reproduction is this record's outcome for the contract's
   goal-attention clause.
3. **The four chat pane-simulation cases** are `chat.test.tsx:569` (deleted, §7), `:618`, `:666`
   and `:701` (kept, §6). Each builds a `scrollTop`/`scrollHeight`/`clientHeight` model on
   `#chat-scroll` by `Object.defineProperty` and drives it with `fireEvent.scroll`.
4. **The single production carrier is `frontend/src/features/chat/use-chat-scroll-restoration.ts`**
   (69 L): `panePinnedBelowOffset` (`:10-13`), the owed-offset re-assertion (`:19-26`),
   `assertRecordedOffset` with its two effects (`:28-40`), `onScroll` (`:52-59`) and
   `rememberLeavingScope` (`:61-66`), called by `chat.tsx:174`/`:181`. Both render-gate twins
   (`portal-chat.spec.mjs:114`, `:477`) drive this hook through the real surface.

## 4. The D15 split as landed (`frontend/AGENTS.md`)

```diff
+- jsdom owns decoding, state and DOM contracts; Playwright owns layout, scroll
+  retention, focus return and real-network flows. A jsdom assertion that only
+  simulates an interaction (scroll position, layout, focus) may not be the
+  authoritative test for it: the authoritative one lives in `browser/tests/`.
```

Placed directly after the existing "Preserve keyboard focus, drafts, selection, and scroll during
refresh" bullet, so the preservation rule and its authoritative layer read together. The line
"browser/ remains the actual Playwright suite and CI contract" is untouched and stays true.

## 5. Twin eligibility (this slice's decision, and why the table is small)

A surviving twin is eligible only when it drives the **same production carrier** as the deleted
assertion, so that flipping the guarded behaviour reddens it while the deleted copy is gone. A twin
that shares only the *name* of the invariant on another surface cannot redden on a mutation of the
deleted assertion's carrier, and deleting the jsdom copy would leave that carrier guarded by
nothing that runs in CI. Measured consequences:

- **Chat pane cases: eligible.** `portal-chat.spec.mjs` is one of the seven render-gate specs and
  drives `use-chat-scroll-restoration.ts` directly.
- **Connections focus cases: not eligible.** Their carriers are `components/focus.ts`
  (`restoreFocus`), `components/use-dialog-opener.ts` and the connections detail's own focus
  handoff. No render-gate spec drives any of them: the assignment dialog that shares
  `restoreFocus` (`features/work/assignment.tsx:261`) and the planning editors
  (`features/planning/editors.tsx:59,230`) run in `portal-work-planning.spec.mjs` (unwired manual
  family), and the connections dialogs themselves only in `portal-connections.spec.mjs` (unwired
  manual family). The five browser focus twins the contract measures (`portal-execution-control:266`,
  `portal-work-views:585`, `workspace-shell:78`, `portal-cutover:352`, `portal-goal-attention:498`)
  drive `app/shell.tsx`'s own `previousFocus` logic, Radix's drawer trap and the work detail's
  route-change heading focus — different carriers. This is the contract's own dispatch note: a
  connections deletion whose twin would have to live in `portal-connections.spec.mjs` is deferred
  to W6-02.
- **The chat composer focus case (`:732`): kept.** Its twin would have to be added to
  `portal-chat.spec.mjs`, and deleting the case would remove its draft-retention state assertion
  (D15 keeps state in jsdom) while its frozen name (D1) names focus — an added twin was therefore
  not the cheaper side of the trade.

## 6. The pair table (deletion and the kept candidates)

| # | invariant | jsdom site (base 956ff4f6) | production carrier | named twin (render gate) | mutation | mutation result | outcome |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | a scope's own recorded offset is re-asserted when its pane renders again after a switch (the surface's per-scope offsets are the authority, not the pane's last scroll event) | `chat.test.tsx:569` "settles a scope's pane on its own offset after a switch, even while the pane clamped it" | `use-chat-scroll-restoration.ts:28-40` (`assertRecordedOffset` + its two effects); exercised through `chat.tsx:174`/`:181` | `browser/tests/portal-chat.spec.mjs:114` "each scope keeps its own draft, intent and scroll, and a late read never overwrites another scope's draft" (assertions `:162`, `:190`, `:197`) | **M-A**: both `assertRecordedOffset` effects removed | twin red while the deleted copy is gone: `:162` `Expected: 40, Received: 120` (1 failed / 1 passed, exit 1); green on the settled tree (`:114` 35.5 s, `:477` 21.2 s, 2 passed, exit 0) | **deleted** |
| 2 | a scope left while its pane is clamped does not record the clamped position | `chat.test.tsx:569`'s first half (the same deleted case) | `use-chat-scroll-restoration.ts:61-66` (`rememberLeavingScope`'s pinned guard) | none | **M1**: the pinned guard dropped in `rememberLeavingScope` | jsdom at base: **48 passed (48)** — no case pins it; the twins were not run under M1 (no jsdom case reddened, so no pair could be formed) | recorded as an uncovered guard (unchanged by this slice) |
| 3 | a pinning scroll event (pane at its maximum under a replaced transcript) is not a reader move | `chat.test.tsx:618` "keeps a scope's offset when the browser pins the pane under a replaced transcript" | `use-chat-scroll-restoration.ts:52-59` (`onScroll`) + `:19-26` (the owed re-assertion it marks) | none: **no render-gate twin reddens on either half** | **M-B**: `onScroll`'s pinned guard dropped; **M-F**: the owed re-assertion effect dropped | M-B: jsdom `:618` red (1 failed / 46 passed), both twins green (2 passed, exit 0). M-F: jsdom `:618` red, both twins green (2 passed, exit 0) | **kept and named** |
| 4 | a reader's move below a too-short pane's maximum is still the reader's own position | `chat.test.tsx:666` "still records a reader's own move inside a pane too short for the scope's offset" | `use-chat-scroll-restoration.ts:10-13` (the `scrollTop === Math.max(0, range)` discriminator) | none | **M-E**: the discriminator relaxed to `range < wanted` | jsdom `:666` red (1 failed / 46 passed), both twins green (2 passed, exit 0) | **kept and named** |
| 5 | the offset a scope is left with is captured from the live pane at switch time (not only from scroll events) | `chat.test.tsx:701` "keeps the offset a scope was left with when the pane's own positioning was never reported as a scroll" | `use-chat-scroll-restoration.ts:61-66` (`rememberLeavingScope`'s live read) | none | **M-D**: `rememberLeavingScope` becomes a no-op | jsdom `:701` red (1 failed / 46 passed), both twins green (2 passed, exit 0) | **kept and named** |
| 6 | dialog close hands focus back to the invoking control (with the pending-write fallback) | `connections.test.tsx:749`, `:769`, `:793`, `:814`, `:1489` | `components/focus.ts`, `components/use-dialog-opener.ts`, `connections.tsx`'s detail handoff | none in a render-gate spec (§5) | — | — | **kept and named; deferred to W6-02** |
| 7 | keyboard focus is retained across a background refresh | `connections.test.tsx:844`; `chat.test.tsx:732` (composer + caret) | connections list identity; `chat-sections.tsx` composer identity | `portal-execution-control.spec.mjs:266` asserts the same invariant on the work detail's own carrier only | — | — | **kept and named** (§5) |
| 8 | per-scope draft/intent state and scroll restore on a switch | `chat.test.tsx:231` | `scopes.ts` state + the pane hook | `portal-chat.spec.mjs:114` | — | — | **kept and named**: the case carries the draft/intent state assertions D15 keeps in jsdom; its frozen name names scroll; it is the case that still reddens under M-A in jsdom after the deletion |
| 9 | goal attention interaction duplication | `goal-attention.test.tsx` (0 scroll/focus/layout mentions) | — | — | — | — | **non-reproduction recorded** (§3) |

## 7. The deleted assertion's audit (lesson 32, inverted)

| field | value |
| --- | --- |
| base assertion | `chat.test.tsx:569` "settles a scope's pane on its own offset after a switch, even while the pane clamped it" (case body `:569-616`) |
| guarded invariant | a scope's own recorded offset is re-asserted whenever its pane renders that scope again, after a scope switch, a router repositioning and an unrelated cache invalidation — the pane's own last scroll event is not the authority |
| the jsdom-simulated aspect | the pane model: `scrollTop`/`scrollHeight`/`clientHeight` defined by `Object.defineProperty` with a loading-state clamp (`#chat-messages === null → 0`), driven by `fireEvent.scroll`; Chromium's own clamping is replaced by a hand-written getter/setter |
| the browser twin that now owns it | `browser/tests/portal-chat.spec.mjs:114` "each scope keeps its own draft, intent and scroll, and a late read never overwrites another scope's draft" — a real-data walk (44 seeded messages, three scope switches, a held read) whose `:162`, `:190` and `:197` assertions are the same invariant on real Chromium |
| mutation that reddens the twin | **M-A**: `frontend/src/features/chat/use-chat-scroll-restoration.ts` — the two `assertRecordedOffset` effects (`useLayoutEffect(assertRecordedOffset, [...])` and `useEffect(() => router.subscribe("onRendered", assertRecordedOffset), [...])`) removed, so a scope's recorded offset is never re-applied on return |
| mutation result (deleted copy gone) | twin `:114` **red** at `:162` — `Expected: 40, Received: 120` (1 failed / 1 passed, exit 1) — while `chat.test.tsx` reports 47 cases with the case absent; the same mutation also reddened the kept `:231` in jsdom, and both twins green on the reverted tree |
| preserved | yes — the invariant is observed in the browser layer (and still in jsdom through `:231`); the case's *state* assertions stay wherever they were |

## 8. The mutation matrix (all runs on this host, all mutations reverted)

Production file: `frontend/src/features/chat/use-chat-scroll-restoration.ts` only. Each mutation was
applied, the affected jsdom file run, the frontend rebuilt (`pnpm --filter @symmetry/frontend
build`), the two twins run against the local dev instance, the mutation reverted, and the assets
rebuilt from the reverted source.

| mutation | edit | jsdom (`chat.test.tsx`) | twins (`portal-chat.spec.mjs:114` + `:477`, desktop) |
| --- | --- | --- | --- |
| M1 | `rememberLeavingScope`'s pinned guard dropped | 48 passed (48), exit 0 | not run (no pair) |
| M-A | both `assertRecordedOffset` effects dropped | 48 → 2 failed: `:231` + `:569`; after the deletion: 1 failed (`:231`) / 46 passed (47) | **red**: `:114` fails at `:162` (`Expected: 40, Received: 120`), `:477` passes, exit 1 |
| M-B | `onScroll`'s pinned guard dropped | 1 failed (`:618`) / 47 passed | green: 2 passed, exit 0 |
| M-D | `rememberLeavingScope` no-op | 1 failed (`:701`) / 47 passed | green: 2 passed, exit 0 |
| M-E | `panePinnedBelowOffset` discriminator relaxed | 1 failed (`:666`) / 47 passed | green: 2 passed, exit 0 |
| M-F | the owed-offset `useLayoutEffect` dropped | 1 failed (`:618`) / 47 passed | green: 2 passed, exit 0 |

One M-F browser attempt is **excluded as inconclusive**: both cases failed at login/`openChat`
(`spec.mjs:66`, `#chat-scope` never visible) and at the fixture POST (`spec.mjs:57`, a 150 s
`page.evaluate` timeout) because the first dev instance's stdout had backpressured and its
responses had slowed to 5–7 s; the instance was restarted with its output redirected to a file
(login page back to 0.3–1.1 s) and the re-run is the recorded row. No mutation was judged on that
attempt.

## 9. Gates (foreground, real output)

| gate | command | exit | result |
| --- | ---: | ---: | --- |
| typecheck | `pnpm --filter @symmetry/frontend typecheck` | 0 | clean (`tsc --noEmit`) |
| lint | `pnpm --filter @symmetry/frontend lint` | 0 | `✖ 220 problems (0 errors, 220 warnings)` — the pinned `--max-warnings 229` is unraised and unedited |
| diff hygiene | `git diff --check` | 0 | no whitespace damage |
| jsdom suite (base) | the same suite before the edit | 0 | **26 files / 619 tests passed**, 40.26 s |
| jsdom suite (head) | `pnpm --filter @symmetry/frontend test` | 0 | **26 files / 618 tests passed** — 36.25 s, and 30.91 s on the frozen tree after the last mutation revert and the doc writes |
| test-name reconciliation | the same suite with the JSON reporter, base vs head | 0 | **exactly 1 removed name** (`sending settles a scope's pane on its own offset after a switch, even while the pane clamped it`), **0 added**, **0 status changes** — every surviving jsdom test name identical |
| per-file durations (head) | the JSON run | 0 | `chat.test.tsx` 17,375 ms (base 21,852); `connections.test.tsx` 30,423 (27,420); `goal-attention.test.tsx` 17,933 (18,026); the remaining 23 files within ±2.4 s of base except `tool-cache-config.test.ts` 6,150 (840, config-import cold start) |
| pair mutation | M-A with the deleted copy gone | 1 | twin `portal-chat.spec.mjs:114` red at `:162` (`Expected: 40, Received: 120`); jsdom `chat.test.tsx` 1 failed (`:231`) / 46 passed |
| twins green (settled tree) | `playwright test tests/portal-chat.spec.mjs --project=desktop-chromium -g "keeps its own"` on the local dev instance | 0 | two runs on clean assets: `:114` 35.5 s / `:477` 21.2 s (2 passed, 58.2 s) and, on the final assets after the last revert, `:114` 30.4 s / `:477` 10.5 s (2 passed, 43.9 s) |
| browser suite inventory | `python3 scripts/ci/assert-browser-suite-coverage.py` | 0 | `suites=16 wired=7 manual=9 unclassified=0` → the seven render-gate specs and the nine manual families are exactly as at base; no suite added, none rewired |
| assets rebuilt clean | `pnpm --filter @symmetry/frontend build` after the last revert | 0 | staged `control/priv/static/portal` matches the settled tree |

## 10. Not run on this host (named, not assumed)

- **The release-shell render gate** (`.github/workflows/ci.yml:480-530`: a built release, three
  instances, the seven specs) and **the executed-set assertion**
  (`scripts/ci/assert-playwright-json.py`, `--expect-skipped 1` + the seven `--expect-file`s + the
  three pinned titles): the release harness is not runnable on this host (W4-14's recorded
  limitation). They are CI-verified on the draft PR.
- **The twins' mobile project**: the local runs are desktop-chromium only; the render gate runs
  both projects, and the added-twin path was not taken, so the only browser behaviour this slice
  changes is the *deleted* jsdom case — the specs are byte-identical to base.
- **The env-gated case inventory** (the Goal-managed fixture case, the rollout-off attention case
  and the expiry case) is untouched by this slice: no case was added, skipped or renamed, so the
  pinned skip count (1) and the pinned titles hold by construction, but the assertion itself runs
  in CI only.

## 11. LOC delta (D2: report-only)

- Deleted-line total at head: **49 lines** (`chat.test.tsx`, one case) — the recorded
  deleted-and-mutated set. Added: 4 lines in `frontend/AGENTS.md`; the plan (144) and this record
  are documentation.
- The plan's `−1,000…−2,000 test` is an audit estimate over both trees and remains report-only;
  the gate is the pair table with its mutations, and the honest outcome of this slice is one
  deletion plus eight recorded keep-and-name rows (§5 explains why the rest cannot move while the
  connections/planning browser families stay unwired).

## 12. Residual risks and limits

1. **The `rememberLeavingScope` pinned guard is uncovered** (M1: 48/48 jsdom cases passed at base,
   twins not run under M1). This slice neither created nor removed that gap; deleting the case that
   sat closest to it does not change it, but a future twin for the clamp-on-leaving path would need
   a real Chromium scroll event at the moment of leaving.
2. **The hook's scroll state machine stays jsdom-authoritative** for the `onScroll` pinning guard,
   the reader-move discriminator and the live-capture-at-leaving path (rows 3-5): every mutation of
   those reddens a jsdom case and no render-gate twin. Moving them needs twins the render gate does
   not yet carry.
3. **Connections and planning focus carriers remain jsdom-only** until W6-02 wires their browser
   families; the contract's dispatch note defers those deletions.
4. **`chat-hooks.test.tsx:216-222`** (outside the owned set) keeps its own pane model for the same
   hook; it is untouched and recorded here as the sibling-duplicate residual.
5. **The local browser verdicts are single-sample** on a dev-configuration instance; the render
   gate's release shell and both widths are CI's verdict, and the record names that rather than
   assuming it green.
