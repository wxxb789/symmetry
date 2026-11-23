# rf-w5-08-jsdom-browser-split plan — the D15 split text, the chat pane-simulation deletions, and the pair table

Contract: `goal-rf-w5-08-jsdom-browser-split.md` (GDD, 2026-11-21, binding with D15, D1, D2, the
conductor decisions of 2026-11-21 and the owner comment policy of 2026-11-22). Base revision
`956ff4f6` (`refactor(frontend): settle goals from the query and make wait budgets explicit
(#251)`, this worktree's dispatch HEAD; `git status --porcelain` empty at dispatch).

Shape references: `docs/plans/2026-11-22-rf-w5-04-goal-settle-wait-budgets-plan.md` and
`docs/evidence/rf-w5-04-goal-settle-wait-budgets-2026-11-22.md` (dispatch gates, re-derivation,
control-flow audit, mutation table, gate rows, residual risks);
`docs/plans/2026-11-21-rf-w4-14-browser-fixtures-plan.md` (the render gate's host limitation and
the conductor-verified judgement of the seven render-gate specs).

## 1. Dispatch gates (checked at dispatch, base `956ff4f6`)

| gate | state at dispatch |
| --- | --- |
| `#164` (W2-15, `c6b2c352`), W3-12, W4-11 (`#220`, `e84973a7`), W4-08, W4-09, W4-10, W4-14 (`#205`, `105d6eda`) merged | yes — every named revision is an ancestor of HEAD |
| W5-03 (`#249`, `cbe4910e`), W5-04 (`#251`, this HEAD `956ff4f6`) merged | yes — W5-04's wait budgets and `testTimeout` are in the tree, as its contract requires before this slice |
| open PRs touching the owned set | none — `git status --porcelain` empty; no open PR names an owned file |
| the three jsdom suites at dispatch | `chat.test.tsx` 2,062 L / 48 cases; `connections.test.tsx` 1,516 L / 57 cases; `goal-attention.test.tsx` 1,321 L / 29 cases |
| the seven render-gate specs | `workspace-shell` 333 L, `portal-work-views` 1,359 L, `portal-execution-control` 380 L, `portal-goal-managed` 123 L, `portal-goal-attention` 516 L, `portal-cutover` 373 L, `portal-chat` 595 L |

## 2. Re-derivation of the contract's probe against HEAD

1. **Scroll/focus/layout census reproduces.** `chat.test.tsx` 48 `scroll` mentions / 3 `focus`;
   `connections.test.tsx` 0 / 23; `goal-attention.test.tsx` 0 / 0; `getBoundingClientRect`,
   `IntersectionObserver` and `ResizeObserver` are 0 in all three. The T12 "same pattern for goal
   attention" claim does **not** reproduce and no goal-attention deletion is invented — the
   non-reproduction is recorded in the evidence record.
2. **The chat draft-scope state assertion is not a deletion candidate** (decision J: state, not
   layout). `chat.test.tsx:304`/`:327` and the browser twin `portal-chat.spec.mjs:114`'s late-read
   half keep their state assertions in both layers; only pane/scroll *outcomes* move. The deleted
   pane case's two jsdom-owned lines (the `· pending` label, base `:597`, and the loading branch's
   absent `#chat-messages`, base `:598`) were deleted with it in the first apply and **retained in
   the fix stage** at `chat.test.tsx:304` (head `:317-318`), where the held project read keeps the
   pane in the loading branch deterministically (M-G/M-H, record §7).
3. **The jsdom pane-simulation cases are the four mid-file cases** `chat.test.tsx:569` (`settles a
   scope's pane on its own offset after a switch, even while the pane clamped it`), `:618` (`keeps
   a scope's offset when the browser pins the pane under a replaced transcript`), `:666` (`still
   records a reader's own move inside a pane too short for the scope's offset`) and `:701` (`keeps
   the offset a scope was left with when the pane's own positioning was never reported as a
   scroll`). Per case: the deleted `:569` models `scrollTop` alone (`Object.defineProperty` with a
   loading-state getter/setter clamping it to 0 while `#chat-messages` is absent) and drives it
   with `fireEvent.scroll`, `scrollHeight`/`clientHeight` left at jsdom's 0 defaults; `:618` and
   `:666` define `scrollTop`/`scrollHeight`/`clientHeight` by `Object.defineProperty` and drive
   them with `fireEvent.scroll`; `:701` uses plain `scrollTop` assignments. These are jsdom
   simulations of Chromium pane behaviour, superseded by real Chromium in
   `browser/tests/portal-chat.spec.mjs`.
4. **The production carrier is one hook.** `frontend/src/features/chat/use-chat-scroll-restoration.ts`
   (69 L): `panePinnedBelowOffset` (`:10-13`), the owed-offset re-assertion (`:19-26`), the
   recorded-offset re-assertion (`:28-40`), `onScroll` (`:52-59`) and `rememberLeavingScope`
   (`:61-66`), called by `chat.tsx:174`/`:181`. All four jsdom cases exercise this hook, and so do
   `portal-chat.spec.mjs:114` and `:477`.
5. **Twin eligibility rule (this slice's decision, §3.1).** A surviving browser twin must exercise
   the *same production carrier* as the deleted jsdom assertion, so that flipping the guarded
   behaviour reddens the twin. A twin on another surface that only shares the *name* of the
   invariant (focus retention, focus return) cannot redden on a mutation of the deleted assertion's
   carrier; that is a coverage loss, not a split.

## 3. Design decisions the contract leaves unresolved

1. **Same-carrier eligibility.** The pair is accepted only when a render-gate spec drives the
   carrier the deleted assertion drives. Consequences:
   - **Chat pane cases: eligible.** `portal-chat.spec.mjs` is a render-gate spec and drives
     `use-chat-scroll-restoration.ts` through the real surface; a mutation of that hook is
     observable in it.
   - **Connections focus cases: not eligible, kept and named.** Every connections focus case drives
     `frontend/src/components/focus.ts` (`restoreFocus`), `use-dialog-opener.ts` or the
     connections detail's own focus handoff. No render-gate spec drives any of them: the assignment
     dialog that shares `restoreFocus` (`features/work/assignment.tsx:261`) runs in
     `portal-work-planning.spec.mjs` (unwired manual family), the planning editors
     (`features/planning/editors.tsx:59,230`) in the same unwired spec, and the connections dialogs
     themselves only in `portal-connections.spec.mjs` (unwired manual family). The five browser
     focus twins the contract measures (`portal-execution-control:266`, `portal-work-views:585`,
     `workspace-shell:78`, `portal-cutover:352`, `portal-goal-attention:498`) drive
     `app/shell.tsx`'s own `previousFocus` logic, Radix's drawer trap and the work detail's
     route-change heading focus — other carriers. Deleting the connections copies would leave the
     connections dialogs' focus handoff guarded by nothing that runs in CI.
     This is the contract's own deferral: the dispatch note sends a connections deletion whose
     surviving twin would have to live in `portal-connections.spec.mjs` to W6-02.
   - **The chat composer focus case (`:732`): kept and named** — the render-gate twin it would need
     does not exist, and deleting the case would remove its draft-retention state assertion (D15
     keeps state in jsdom) while its frozen name (D1) names focus; the twin-addition path was
     dropped for that reason (§4 row 7).
2. **Mutations are production edits, reverted before the tree is reported.** Each mutation is
   applied to the production hook, the affected jsdom case is run (expecting red, proving the base
   assertion guards the invariant), the named twin is run against a local dev-configuration control
   plane (expecting red, proving the twin owns it), the mutation is reverted, and the twin plus the
   full jsdom suite are run green on the settled tree. The slice branch never carries a mutated
   production file (contract's decision, Opus P2-2).
3. **Local browser runs are supporting evidence; the render-gate verdict is CI's.** The
   release-shell harness (built release, three instances, the executed-set assertion) cannot run on
   this host (W4-14's E8). The twins are run locally against `mix phx.server` in dev configuration
   with a dedicated database (`symmetry_w508_test`), the frontend's built assets staged into
   `control/priv/static/portal`, one instance, `SYMMETRY_GOALS_ROLLOUT_ENABLED=true`, on port 4090
   — the same route and the same production carrier as the release shell. What only the PR's CI run
   can decide (the release build, the env-gated inventory, the executed-set assertion) is named in
   the evidence record, never assumed green.
4. **No promise-added: no skipped, env-gated or renamed case.** No twin is `test.skip`-ed, no pinned
   title changes, no spec file is renamed, and no env-gated case is added (the executed-set step
   pins exactly one by-design skip). An added twin is an ordinary passing case in an already-executed
   spec file.
5. **`git diff --check` and the DLP rules** (no user-profile host path, no backslash immediately
   before the letter u) run over the final diff.

## 4. The pair plan as executed (mutations re-derived empirically)

The draft plan expected cases 1-4 each to pair with a twin under their own mutation. Measurement
revised that: only case 1's *effective* guarded behaviour (the recorded-offset re-assertion, not the
leaving-time clamp guard) reddens a render-gate twin. The full matrix — six mutations, each run
against the jsdom suite and the two `portal-chat` twins — is in
`docs/evidence/rf-w5-08-jsdom-browser-split-2026-11-22.md` §8, and the fix stage's two targeted
jsdom mutations for the retained lines (M-G/M-H) are its §7 rows; this table is the settled
summary.

| # | invariant | jsdom site (base 956ff4f6) | named twin (render gate) | mutation | outcome |
| --- | --- | --- | --- | --- | --- |
| 1 | a scope's own recorded offset is re-asserted when its pane renders again after a switch | `chat.test.tsx:569` (deleted) | `portal-chat.spec.mjs:114` (`:162`, `:190`, `:197`) | M-A: the `assertRecordedOffset` effects dropped | **deleted** (the scroll simulation): twin red at `:162` (`Expected: 40, Received: 120`) with the copy gone; both twins green on the settled tree. The case's jsdom-owned DOM/state lines were retained, not deleted: they run in `chat.test.tsx:304` at `:317-318` (pair row 10, M-G/M-H), D15 keeping DOM/state in jsdom |
| 2 | a scope left while its pane is clamped does not record the clamped position | `chat.test.tsx:569`'s first half (deleted with it) | none | M1: `rememberLeavingScope`'s pinned guard dropped | recorded as an uncovered guard: 48/48 jsdom cases passed at base under M1 |
| 3 | a pinning scroll event is not a reader move | `chat.test.tsx:618` | none | M-B (`onScroll` guard) and M-F (owed re-assertion) | **kept and named**: both mutations redden `:618` alone; both twins green |
| 4 | a reader's move below a too-short pane's maximum is the reader's position | `chat.test.tsx:666` | none | M-E: the discriminator relaxed | **kept and named**: `:666` red alone; both twins green |
| 5 | the offset a scope is left with is captured from the live pane at switch time | `chat.test.tsx:701` | none | M-D: `rememberLeavingScope` no-op | **kept and named**: `:701` red alone; both twins green |
| 6 | dialog close hands focus back to the invoking control | `connections.test.tsx:749`, `:769`, `:793`, `:814`, `:1489` | none in a render-gate spec (§3.1) | — | **kept and named**; deferred to W6-02 by the contract's dispatch note |
| 7 | keyboard focus is retained across a background refresh | `connections.test.tsx:844`; `chat.test.tsx:732` | `portal-execution-control.spec.mjs:266` asserts the invariant on its own carrier only | — | **kept and named** (§3.1) |
| 8 | per-scope draft/intent state and scroll restore | `chat.test.tsx:231` | `portal-chat.spec.mjs:114` | — | **kept**: state assertions stay in jsdom; the case still reddens under M-A |
| 9 | goal-attention interaction duplication | `goal-attention.test.tsx` | — | — | **non-reproduction recorded** |
| 10 | `chat-hooks.test.tsx:216-222` (out of the owned set) | the hook's own unit-level pane model | — | — | named as a residual risk |

Deleted-line total: 49 lines, one jsdom case; the LOC delta is reported (D2: report-only). No twin
addition was needed, so the seven render-gate specs are byte-identical to base.

## 5. Gates (all foreground)

| gate | command | expected |
| --- | --- | --- |
| typecheck | `pnpm --filter @symmetry/frontend typecheck` | exit 0 |
| lint | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`, not raised) | exit 0 |
| jsdom suite | `pnpm --filter @symmetry/frontend test` | 26 files, every surviving test name identical to the pre-change baseline, exit 0 |
| diff hygiene | `git diff --check`; the DLP greps | no whitespace damage, no user-profile path, no backslash-u |
| pair mutations (as executed) | M1, M-A, M-B, M-D, M-E, M-F (six production mutations, each reverted) plus the fix stage's M-G/M-H for the retained lines (also reverted) | measured matrix in the record's §8; the single pair (M-A) reddens the twin with the deleted copy gone; M-G/M-H redden the retained lines and green on the reverted tree (record §7, §9) |
| twins green | `playwright test tests/portal-chat.spec.mjs` on the local dev instance | the twin cases pass on the settled tree |
| render gate (CI) | the PR's `control` job: the seven specs + the executed-set assertion | CI run `45083687616` (head `b939bbc2`): `control` **success** — `1 skipped / 93 passed`, executed set verified; recorded in the evidence record §10 |

## 6. Out of scope, with reasons

- Production component changes (W5-03/W4-x landed): the only production edits in this slice are
  temporary mutations, reverted before the tree is reported.
- The wait budgets (W5-04), the MSW double (W4-11), browser fixture consolidation (W4-14), wiring
  new spec families into CI (W6-02), route code splitting (W6-07), the browser ESLint block
  (W6-01), `vitest.config.ts`, coverage thresholds (W2-17/W6-12).
- `chat-hooks.test.tsx` (the hook's own unit suite) is outside the owned set; its pane model is
  recorded as a residual, not edited.
- Deferring the connections focus deletions to W6-02 as the contract's dispatch note directs.
