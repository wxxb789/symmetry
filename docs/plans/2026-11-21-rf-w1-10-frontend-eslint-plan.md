# rf-w1-10-frontend-eslint plan — strict ESLint baseline, dead-surface trim, coverage + knip config

Contract: `goal-rf-w1-10-frontend-eslint.md` (GDD, 2026-11-21). The contract's plan-review
amendments (A5 root lockfile, A6 base-red test, "ratchet = measured count after autofix") and the
opus-1 P2-5 amendment (`--fix` review at served-data boundaries) are binding here. Base for every
coordinate in this plan: `origin/main` at `a3039f0d17fc7c7905b63bdf247cac5bf339b213` (this
worktree's HEAD, clean tree).

Sources: `program-plan.md` §4 W1-10 (owned files, acceptance, oracle), §10.3 (ESLint plan incl.
the proposed config), §10.5 (coverage plane); `findings/frontend.md` §1.4-1.6 and §5 (proposed
config, counts, staged rollout, knip audit config, base-red test), F8 (dead exports, two
strategies agree); `plan-review-opus-1.md` P2-5; `plan-review-ds-1.md` P2-1/P2-2.

## Deliverable shape

1. **`frontend/eslint.config.js`** — `strictTypeChecked` + `stylisticTypeChecked` replace
   `recommendedTypeChecked`; react-hooks `recommended-latest` replaces `recommended` (adds exactly
   `react-hooks/void-use-memo`, verified against the installed plugin); the two §10.3 size caps
   (`complexity: 25`, `max-lines-per-function: 150` with `skipBlankLines`/`skipComments`) are
   present; one marked residual block downgrades each still-reporting rule to `warn`, with the
   measured per-rule counts recorded in the block's comment (the ratchet entry).
2. **Autofix churn in `frontend/src/**`** — one `eslint --fix` pass over the strict config; 515
   message-level fixes expected (re-measured, not trusted). Hunks under `src/services/**` and the
   decode modules are reviewed by hand for the P2-5 class (fixers that trust static types at
   served-data boundaries: `no-unnecessary-boolean-literal-compare`,
   `non-nullable-type-assertion-style`; `no-unnecessary-type-assertion` reports zero here).
3. **`frontend/package.json`** — `lint` becomes `eslint . --max-warnings <measured>`; devDeps
   `@vitest/coverage-v8` and `knip@5.88.1` (both pinned to exact versions, matching the repo's
   style); a `knip` script (`knip`) so the config has a documented entry point.
4. **Root `pnpm-lock.yaml`** — the only lockfile in this workspace (amendment A5); updated by
   `pnpm install` after the devDep additions.
5. **`frontend/vitest.config.ts`** — coverage config: provider `v8`, reporters
   `["text-summary", "lcov"]`, `reportsDirectory` under `SYMMETRY_TOOL_CACHE_DIR` when the sealed
   harness exports it and `coverage/` otherwise, keeping coverage output inside the existing
   tool-run-state contract (`scripts/ci/model-routing-evaluation.py`, "Tool run state").
6. **Dead-surface trim in `frontend/src/**`** — delete the three knip-proven exports
   (`api.fixtures.ts:249 readGoalEvents`, `api.ts:121 ExecutionReceipt`, `:122 WaitingInput`) plus
   `app/test-harness.tsx:applicationOf` (declared, zero references anywhere, in-file included);
   un-export the 80 module-internal exports whose only references are inside their own file
   (mechanical scan at HEAD; test-only seams stay, see decision D3).
7. **`frontend/src/tool-cache-config.test.ts`** — the base-red case (A6): both cases get an
   explicit per-test ceiling sized from the recorded cold measurement, with the measurement chain
   in a comment.
8. **`frontend/knip.json`** (new, host-path-free) — the §5 audit config: entry `src/main.tsx`,
   tests, `vite.config.ts`/`vitest.config.ts`, `scripts/`; `ignoreExportsUsedInFile`; the two
   documented false-positive ignores (`@symmetry/contracts`, `tailwindcss`). Gate wiring stays out
   (contract clause 4).

No other file changes. `docs/TODO.md`/`CLOSED-TODO.md` are not moved: no open TODO entry names
this slice's surface (`grep -n "eslint\|knip\|coverage" docs/TODO.md` → no hits outside the
archive).

## Measured basis (re-derived on this tree, 2026-11-21)

The §5 proposed config is `strictTypeChecked` + `stylisticTypeChecked` + `recommended-latest` plus
the two caps. With that config and no downgrades the tree reports **743 errors / 0 warnings across
42 files; 515 of the messages carry an autofix (69.3 %)**, exactly the 515 the plan names:

| rule | errors | fixable |
| --- | ---: | ---: |
| `@typescript-eslint/dot-notation` | 244 | 244 |
| `@typescript-eslint/no-confusing-void-expression` | 220 | 219 |
| `@typescript-eslint/no-unnecessary-condition` | 79 | 0 |
| `@typescript-eslint/restrict-template-expressions` | 41 | 0 |
| `@typescript-eslint/array-type` | 32 | 32 |
| `@typescript-eslint/no-invalid-void-type` | 26 | 0 |
| `max-lines-per-function` | 16 | 0 |
| `complexity` | 14 | 0 |
| `@typescript-eslint/no-unnecessary-boolean-literal-compare` | 14 | 14 |
| `@typescript-eslint/no-deprecated` | 13 | 0 |
| `@typescript-eslint/no-non-null-assertion` | 9 | 0 |
| `@typescript-eslint/prefer-nullish-coalescing` | 8 | 0 |
| `@typescript-eslint/no-dynamic-delete` | 6 | 0 |
| `@typescript-eslint/prefer-find` | 5 | 0 |
| `@typescript-eslint/no-empty-function` | 4 | 0 |
| `@typescript-eslint/prefer-optional-chain` | 4 | 0 |
| `@typescript-eslint/non-nullable-type-assertion-style` | 2 | 2 |
| `@typescript-eslint/no-unnecessary-type-conversion` | 2 | 0 |
| `@typescript-eslint/no-unnecessary-type-arguments` | 1 | 1 |
| `@typescript-eslint/consistent-type-definitions` | 1 | 1 |
| `@typescript-eslint/prefer-regexp-exec` | 1 | 1 |
| `@typescript-eslint/no-meaningless-void-operator` | 1 | 1 |

`react-hooks/void-use-memo` reports nothing on this tree, so the preset swap adds no finding. The
audit's 713-error figure excludes the 30 cap findings (14 + 16), which is why the strict-config
measurement here is 743; the 515 autofixable count is unchanged, so the contract's "the 515
mechanical findings" is satisfied as written. The caps are counted by ESLint, not by the audit's
approximate AST walk: the §5 comment "5 src functions > 25 / 8 src functions > 150" under-counts
(14 complexity + 16 length across src+tests: 30 cap findings over 21 files — 14 production + 7 test,
`chat.tsx` and `planning.test.tsx` carrying both rules).

Post-`--fix` residual (the ratchet table; each row's count is re-measured after the fix pass, and
`package.json`'s `--max-warnings` is the exact total):

| rule (severity `warn`) | findings |
| --- | ---: |
| `@typescript-eslint/no-unnecessary-condition` | 78 |
| `@typescript-eslint/restrict-template-expressions` | 40 |
| `@typescript-eslint/no-invalid-void-type` | 26 |
| `max-lines-per-function` | 16 |
| `complexity` | 14 |
| `@typescript-eslint/no-deprecated` | 13 |
| `@typescript-eslint/no-non-null-assertion` | 11 |
| `@typescript-eslint/prefer-nullish-coalescing` | 8 |
| `@typescript-eslint/no-dynamic-delete` | 6 |
| `@typescript-eslint/prefer-find` | 5 |
| `@typescript-eslint/no-empty-function` | 4 |
| `@typescript-eslint/prefer-optional-chain` | 4 |
| `@typescript-eslint/dot-notation` | 2 |
| `@typescript-eslint/no-unnecessary-type-conversion` | 2 |
| **ceiling** (`eslint . --max-warnings 229`) | **229** |

The final count is 229, and each movement from the immediate post-autofix state (231) is accounted
for by a named change, re-derived from the two JSON runs (per-message diff): the fix pass moved two
findings from `non-nullable-type-assertion-style` (fixed to `!`) into `no-non-null-assertion`
(9 -> 11); `dot-notation` is 2 rather than 0 because both remaining findings sit in
`frontend/vite.config.ts`, outside this slice's owned set (the autofix was reverted there and the
rule's two findings join the ratchet with the note in `eslint.config.js`); fixing the `never`
narrowing in `goal-attention.test.tsx` removed one always-true guard finding
(`no-unnecessary-condition` 79 -> 78); deleting the dead `readGoalEvents` removed one
`restrict-template-expressions` finding (`event-${entry.sequence}` — the strict preset rejects a
`number` interpolation, 41 -> 40). `no-confusing-void-expression`
reached 0 (220 -> 0) and keeps `error` severity, as do the other rules that the fix pass emptied
(`array-type`, `no-unnecessary-boolean-literal-compare`, `non-nullable-type-assertion-style`,
`no-unnecessary-type-arguments`, `consistent-type-definitions`, `prefer-regexp-exec`,
`no-meaningless-void-operator`).

The lockfile is the workspace-root `pnpm-lock.yaml` (amendment A5). It was regenerated with the
host's pnpm 12.21.0 (the repo pins 12.17.0, which is not installed here); the diff adds the two
devDeps and their 35 transitives, re-keys the vitest peer graph for the new `@vitest/coverage-v8`
peer, and drops 103 pre-existing explicit `tarball:` URLs (integrity unchanged — pnpm 12.21's own
normalization, inert: every entry either keeps its `registry.npmjs.org` URL or derives it from the
configured registry). No proxy/feed URL is recorded anywhere in it (the first regenerated attempt
carried 160 host-specific feed URLs; it was regenerated with the public feed as the registry so
those resolved URLs equal the registry default and are omitted).

## Decisions (options where the contract leaves room)

- **D1 — size caps land as `warn`, not as `error` + file-level allowlist.** The contract's Change
  clause enumerates the config swap, the autofix, the residual downgrade, coverage, the dead-export
  trim and knip; §10.3's staged-rollout item 5 instead says "enable with the allowlist above".
  Options: (A) caps as errors with the §10.3 allowlist; (B) caps as errors scoped to production
  source only, with a src-file allowlist; (C) caps enabled but inside the residual warn block;
  (D) caps omitted. Measured, (A) needs 21 file-level exemptions (14 src files + 7 test files,
  `chat.tsx` and `planning.test.tsx` carrying both rules), which cannot be "shrink-only" inside
  ESLint itself and weakens
  enforcement exactly in the files that carry the debt; (C) keeps the rules active for every file
  with no exemptions, and the `--max-warnings` ceiling makes any NEW oversized function fail
  `pnpm lint` exactly as an error severity would (any new finding of any rule exceeds the ceiling).
  Recommendation and choice: **(C)** — the ratchet is the enforcement channel the contract names,
  and it avoids a 21-file allowlist that the contract never requires. Rejected: (A) allowlist
  bloat + false "CI fails on any new allowance" claim; (B) same, narrower; (D) loses the caps.
- **D2 — `applicationOf` is deleted with the three knip exports.** It is the only other export in
  `frontend/src/**` with zero references anywhere (declaration-only; `git grep -nw applicationOf`
  outside `frontend/src` → none). Un-exporting it alone would leave an unused local
  (`noUnusedLocals: true`), so the trim terminates in deletion. Recorded here because the contract
  names three deletions; this is the same class, evidenced mechanically.
- **D3 — test-only seams stay exported.** The audit's second class ("referenced only from test
  files": `safeReturnTo`, saved-views storage helpers, `invalidationKeys`, `servedLink`,
  `BoardColumnSnapshot`, `resetPendingWorkItemCreations`, `receiptRowLimit`, capability helpers,
  `api.fixtures.ts` builders) is not "internals no other module needs" — the tests are modules and
  they import these names. F8's alternative (move them to `*.test-utils.ts`) rewrites production
  and test imports across six modules; it does not change any observable the contract names, and
  the slice's size envelope (≈−40…−80 LOC, low risk) does not fit it. They are recorded here as the
  M8 residual for the owning follow-up; nothing in this slice narrows their tests.
- **D4 — timeout fix is per-test, not suite-level.** A6 offers either. A suite-level `testTimeout`
  in `vitest.config.ts` would change the budget of all 509 tests to serve two cases and pre-empt
  W5-04's later explicit decision for the same file; a per-test ceiling keeps the blast radius at
  the two cases that pay the cold import, and a filtered run can start at either, so both get it.
  W5-04 may still introduce its suite-level 15 s later without conflict (a per-test timeout
  overrides the default in this direction).
- **D5 — local pnpm runs need `--pm-on-fail=ignore`.** The repo pins `pnpm@12.17.0`; this host has
  12.21.0 and pnpm refuses to run scripts otherwise (findings F10). CI uses the pinned action and
  is unaffected; every locally-run gate in the answer records the flag.

## Edge-case semantics that the contract demands

- **Ratchet, not suppression.** No rule is disabled (`off`); every residual rule stays enabled at
  `warn`, each with its measured count recorded in the config block, and
  `eslint . --max-warnings <total>` fails on any new finding of any rule, including the caps and
  `no-unnecessary-condition` (whose burn-down clause 4 defers).
- **Autofix review at served-data boundaries (P2-5).** The three named fixers are checked hunk by
  hunk in `src/services/**` and the decode modules: `dot-notation` (244) rewrites string-literal
  member access; `no-unnecessary-boolean-literal-compare` (14) removes `=== true/false` against
  typed booleans; `non-nullable-type-assertion-style` (2) rewrites `as T` to `!`. Any hunk that
  removes a *runtime* check on served JSON is reverted and the rule stays warned for that shape.
- **Un-exports are compile-checked, not trust-checked.** The scan's criterion is zero references
  outside the defining file across all of `frontend/src` (tests included) plus zero hits for every
  name outside `frontend/src` (the tree-wide `git grep` recorded in the answer). `tsc --noEmit`
  with `noUnusedLocals` plus the 509-test suite are the oracle; knip is the second.
- **Coverage output is run state.** `reportsDirectory` follows `SYMMETRY_TOOL_CACHE_DIR` when the
  sealed harness exports it; without the variable the default `coverage/` inside the package is
  used (the same shape as `cacheDir` in the same file).
- **Base-red fix sizes the bound from measurement, not taste.** Recorded chain: the audit measured
  the cold `vite.config` import at 8.2 s against vitest's 5 s default (`findings/frontend.md`
  §1.5, test at `tool-cache-config.test.ts:44`); this host measures ~0.5-0.7 s warm
  (`node --input-type=module` import 477 ms; vitest case 678 ms; with transform caches off 503 ms),
  so the red is not reproducible here but the default bound is demonstrably ambient. The explicit
  per-test ceiling is 30 000 ms: ~3.7x the only cold measurement, comfortably above any plausible
  warm run, and still finite for a genuinely hung import.

## Evidence plan (what will be run, and what proves what)

Front-facing gates (all foreground, serialized — never a measurement harness beside the suite):

| what | command (from repo root unless noted) | proves |
| --- | --- | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | un-exports and deletions compile; no unused locals |
| lint (final tree) | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | exit 0 at the ceiling; warnings == recorded total |
| ratchet bites | `pnpm exec eslint . --max-warnings <total-1>` (in `frontend/`) | exit 1: the ceiling is exact, not slack |
| tests | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | 509 tests, 20 files, 0 failed (was 509/1 failed at base on the audit host) |
| test-file fix | `vitest run src/tool-cache-config.test.ts --testTimeout=100` | with the per-test ceiling present both cases pass despite an ambient 100 ms default; at base the same command fails |
| coverage | `pnpm exec vitest run --coverage` (in `frontend/`) | provider installed, coverage table emitted, `coverage/` written |
| build | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend build` | vite build + `stage-assets.mjs` green |
| knip | `pnpm exec knip` (in `frontend/`) | config resolves; 0 unused files/exports after the trim (3 before, re-measured) |
| diff hygiene | `git diff --check` | no whitespace damage |

Judging artifacts recorded in the answer: lint warning count, test count, knip output, plus the
per-rule ratchet table, the deletion/un-export inventory, and the autofix counts by rule.

Failure-control checks: the deletion of the three dead exports is verified by knip reporting zero
unused exports afterwards (and the same run before the deletion reporting exactly those three);
the un-export set is verified by `tsc` (a wrong un-export is a compile error only if the name is
used, and the suite covers behavior); no test is renamed, deleted, or weakened, and the collected
count is asserted unchanged at 509 by the suite output. No assertion is deleted or converted in
this slice, so no mutation check for assertions is required; the ratchet check above is the
mutation check for the gate itself.

## Out of scope (with reasons)

- `browser/**` flat ESLint config (contract clause 4; W6-01).
- knip gate wiring in CI (contract; §10.3) and coverage thresholds/upload (W2-01/§10.5).
- MSW harness (W4-11); `no-unnecessary-condition` burn-down (after F4/F11); the ~198 residual
  findings themselves (they are the ratchet, burned down per §5 items 2-4 one rule at a time).
- Size-cap *burn-down* (splitting the oversized functions/files) — S4/S7/S9 own those; this slice
  only makes the debt visible and ratcheted.
- Test-only seam relocation (D3) and the W5-04 poll/wait rework (its own slice).
- No `//eslint-disable` or `eslint-disable-next-line` is added anywhere; no suppression.

## Residual risks / known limits

- The `--max-warnings` ceiling counts all residual findings, so a legitimate burn-down that lands
  outside this repo slice must lower the number in `package.json` in the same change; a stale high
  ceiling tolerates re-introduction of exactly that many findings. The config comment names the
  per-rule counts so drift is visible in review.
- Caps as warnings mean a reviewer sees 30 of the 228 residual findings as size debt rather than
  errors; the ratchet makes their enforcement identical (decision D1).
- The cold-import red is host-dependent (8.2 s cold on the audit host, ~0.5 s warm here); the
  30 s ceiling is sized for the recorded cold case, not for a benchmark of this host.
- knip's one documented false positive (`@symmetry/contracts` workspace exports layout) is ignored
  with a reason; if knip learns the exports layout, the ignore should be removed. The `tailwindcss`
  ignore the plan proposed is no longer needed: the plugin resolves the peer itself (the apply note
  below records the drop). `ignoreFiles: ["coverage/**"]` excludes the default-path coverage report:
  with `SYMMETRY_TOOL_CACHE_DIR` unset, `vitest run --coverage` writes `coverage/lcov-report/` into
  the package and knip's project globs would report its six asset files as unused (reproduced:
  exit 1, `Unused files (6)`). Before the first coverage run knip reports the entry as a
  configuration hint (`coverage/** … Remove from ignoreFiles`, exit 0) — advisory, and gone once the
  directory exists. That directory still needs a `/frontend/coverage/` entry in the root
  `.gitignore`, which is outside this slice's owned set (routed to the owning change). (`knip.json`
  supports no comments, so the reasons live here and in the answer.)

## Apply-stage note (2026-11-21, final tree)

- **A fifth dead export surfaced from the deletions.** `api.ts`'s `Execution` was referenced only by
  the deleted `ExecutionReceipt`/`WaitingInput`, so after the trim it had no reference anywhere;
  knip's first post-trim run flagged it and it was deleted (`Execution` was not in the original
  scan's zero-external list because it was used in-file at HEAD). Final deletions: `readGoalEvents`,
  `ExecutionReceipt`, `WaitingInput`, `Execution`, `applicationOf`; un-exports: 80 (the 80th is
  `features/planning/editors.tsx`'s `FieldErrors`, un-exported in the simplify apply).
- **One autofix hunk was not type-preserving and was fixed, not reverted.** In
  `goal-attention.test.tsx` the `dot-notation` fixer rewrote `payload["subject"]` to
  `payload.subject`, which fails `tsc` because the payload capture narrows to `never` (the
  element-access form masked the collapse: property access on `never` is an error, element access
  is not). The capture's sentinel became `undefined` (no `= null` initializer), the guard checks
  `undefined`, and the wait assertion became `toBeDefined()` — semantics preserved, and the
  converted assertion has a mutation check: with the capture disabled the case fails
  (`AssertionError: expected undefined to be defined`, exit 1) and the file's sha256 is unchanged
  after restore (`29b8f00a…`).
- **The mechanical un-export pass over-reached on three exports** (`Connection`,
  `ConnectionProvider`, `ConnectionResource`, all used by the connections surfaces) and
  `tsc --noEmit` caught it; they were restored and a tree-wide diff audit confirmed no other
  out-of-list removal (every remaining `-export …` pair is one of the 80 list members or one of the
  five deletions).
- **Out-of-set autofix reverted.** `frontend/vite.config.ts` (not in this slice's owned set) had its
  two `dot-notation` fixes reverted; those two findings join the ratchet (see the table).
- **Coverage under the sealed environment.** The full suite passes with `--coverage` and the
  `SYMMETRY_TOOL_CACHE_DIR` redirect (509/509, report under the redirect). The one failing shape on
  this host was `--coverage` + `SYMMETRY_DISABLE_TOOL_CACHES=1` across the whole suite:
  `router.test.tsx > workspace navigation > does not report unloaded project work as zero …`
  (failed at 1,426 ms) and `goal-attention.test.tsx > attention entry point > lists Goal attention
  and the project blocked work together` (1,227 ms) exceeded their content waits under the extra
  transform cost — the F7/W5-04 wall-clock-budget class; both files pass in isolation under the same
  environment (71/71, `--coverage` with and without the cache switch).
- **knip.json shipped in its measured-minimal shape (simplify apply).** Item 8 above (entry
  `src/main.tsx`, tests, vite/vitest configs, `scripts/`, `ignoreExportsUsedInFile`, two ignores) is
  superseded: the shipped file had tests + `scripts/**/*.mjs` as `entry`, and both remaining pieces
  of it were removed after measurement — the vitest plugin globs the test files, the vite plugin
  resolves `index.html` and package.json script detection covers `scripts/**/*.mjs` (a no-entry run
  reports no entry-related findings), and `ignoreExportsUsedInFile: true` (knip's default is `false`)
  suppressed exactly one real finding, `FieldErrors` in `features/planning/editors.tsx` — the
  implement-stage scan missed it because `features/connections/resources.tsx` defines a
  byte-identical same-named component. The flag was removed atomically with un-exporting that
  component, so `pnpm knip` stays at exit 0 with strictly less config; the tailwindcss ignore had
  already been dropped (answer.md records why).
- **Size-cap exemption arithmetic corrected to the measurement (simplify apply).** The tree carries
  30 cap findings (16 `max-lines-per-function` + 14 `complexity`) across 21 files — 14 production +
  7 test — with `features/chat/chat.tsx` and `features/planning/planning.test.tsx` carrying both
  rules; the `eslint.config.js` comment and decision D1 above now record that instead of the
  earlier 19-file arithmetic. D1's choice (C) is unaffected; the ratchet table and the
  `--max-warnings 229` ceiling are unchanged.
