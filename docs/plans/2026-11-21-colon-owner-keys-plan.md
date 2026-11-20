# colon-owner-keys plan — accept and document the pre-#111 colon-leading owner keys

Contract: `goal-colon-owner-keys.md` (GDD, 2026-11-21). Baseline for every coordinate in this
plan: `origin/main` at `adf6d3868161e08f671520d4bcbe76ae52f508dd` (the worktree's HEAD). Touched
package: `frontend/` (tests and one comment) plus `docs/`. Closing branch: `fix/colon-owner-keys`.

The contract's source entry is the open `docs/TODO.md` item "Re-point the pre-#111 owner keys
minted for recorded names that start with a colon." (`docs/TODO.md:254-281`, section
`## Retro review follow-ups (second pass, 2026-11-20)`), the residual of the legacy-sentinel
increment (`docs/plans/2026-11-20-legacy-owner-bookmarks-plan.md`, "Out of scope"). The repo
owner/conductor decided on 2026-11-21, under the human owner's standing "no backward
compatibility" directive: ACCEPT the collision and document it; do NOT add a new legacy re-point
for colon-leading keys. This increment implements that decision — pins, documentation, archive —
and changes no production behavior.

## Decision and exact semantics

The accepted reading: **a key is always read as a current mint.** `ownerKeyFilter`
(`frontend/src/features/work/presentation.ts`) re-points only the two pre-#111 sentinel words
(`agent:unrecorded`, `human:unrecorded`); every other key travels as written. The pre-#111
encoder spelled a recorded value unescaped, while the current encoder prefixes one colon to a
value that starts with `:` or equals the legacy word (`ownerKeyValue`), so the pre-#111 key for a
recorded name with `k >= 2` leading colons, or for the name `:unrecorded`, is byte-identical to
the current mint for the same name with one colon fewer; only those colliding spellings
cannot be disambiguated. The remaining case, a name with exactly one leading colon and a
remainder other than the legacy word (`:Ada`), equals no current mint: the legacy key
`human::Ada` selects no row while it still labels as `Ada`. Re-pointing only that unambiguous
subset was declined as compatibility code the owner does not require.

Semantics fixed by the pins (`ownerKeyLabel` renders `raw.slice(1)` for a value starting with
`:`, `raw === "" || raw === legacyAbsentValue` as the absence):

| Key read | Read as | Row it selects | Pre-#111 row it once named |
| --- | --- | --- | --- |
| `human::Ada` | `human::Ada`, the mint of no row | none, and its label renders `Ada` | name `:Ada` (now mints `human:::Ada`) |
| `human::unrecorded` | `human::unrecorded`, the mint of the recorded name `unrecorded` | the row named `unrecorded`, not the row named `:unrecorded` | name `:unrecorded` (now mints `human:::unrecorded`) |
| `human:::Ada` | `human:::Ada`, the mint of the recorded name `:Ada` | the row named `:Ada`, not the row named `::Ada` | name `::Ada` (now mints `human::::Ada`) |

Accepted consequences: a legacy bookmark for the name `:Ada` shows the label `Ada` (the colon
is the escape marker, not part of the name) while selecting no row; the colliding spellings
select the other row silently, and no surface can tell which name the bookmark meant. The
agent-side mirrors (`agent::Ada`, `agent::unrecorded`) behave the same through the shared
`ownerKeyValue`; the contract pins the human side.

## Public / API shape

No production shape changes: no exported symbol, option, route, storage version or wire field
moves. The only production-source edit is the doc comment above `ownerKeyFilter`
(`frontend/src/features/work/presentation.ts`), extended to state the accepted reading and name
the decision; comments do not change the emitted bundle. `frontend/src/features/projects/filters.ts`
and every surface file stay byte-identical.

## Pins (contract clause 1)

`frontend/src/features/projects/filters.test.ts` gains a fixture of human rows named `Ada`,
`:Ada`, `::Ada`, `unrecorded`, `:unrecorded` (minting `human:Ada`, `human:::Ada`,
`human::::Ada`, `human::unrecorded`, `human:::unrecorded`) and one case in "work filtering and
projections", each pin carrying a comment naming the accepted decision:

- `workSearch({ owner: "human::Ada" })` keeps its spelling, `filterWorkItems` through it selects
  no row (neither the `Ada` row nor the colliding `:Ada` row), and `ownerKeyLabel("human::Ada")`
  is `"Ada"` — what the code renders today.
- `human::unrecorded` selects the row named `unrecorded` and not the row named `:unrecorded`;
  pinned through the URL reader (`filtersOf(workSearch(...))`) and the bare-`WorkSearch` reader
  (`filtersOf({ ... })`), the two call sites the previous increment pinned.
- `human:::Ada` selects the row named `:Ada` and not the row named `::Ada`, through the URL
  reader and the bare-`WorkSearch` reader (`filtersOf({ ... })`); `workSearch` keeps its
  spelling as well.

Mutation check (contract clause 2): temporarily re-pointing `human::Ada` to `human:::Ada` inside
`ownerKeyFilter` must redden the first pin (the empty selection and the spelled-key equality);
reverted afterwards, with both runs recorded.

## Documentation (contract clause 1)

- Code: the `ownerKeyFilter` doc comment states the accepted reading, the three shapes and the
  decision date.
- Records: this plan; the 2026-11-20 plan's "Out of scope" note gains a pointer to the decision
  (its "tracked as an open entry in `docs/TODO.md`" sentence would otherwise dangle after the
  archive move); the entry moves from `docs/TODO.md` to `docs/archive/CLOSED-TODO.md` with the
  closing branch, the decision and the pinning test names, and the CLOSED-TODO preamble names the
  move date and branch.

## Design trade-offs

1. **Accept-and-document vs re-point.** Settled by the owner and quoted in the contract; this
   increment does not relitigate it. The code cost of a partial re-point is real: deciding
   "unambiguous" requires reasoning about the escape lattice per key, and the ambiguous subset
   (`human::unrecorded`, `human:::Ada`, every deeper prefix) would still need the accepted
   collision documented.
2. **Pin location and shape.** `filters.test.ts` is the contract's named location and already
   holds the sentinel case; the label pin imports `ownerKeyLabel` from `presentation.ts`, the
   function the surface reads the select label through, so the pin names the real reader.
3. **No behavior change.** The pins freeze the current reading: a future increment that
   re-points colon-leading keys reds them and must revisit the decision explicitly instead of
   drifting past it.

## Out of scope (contract clause 4)

- Other `docs/TODO.md` entries, including the Windows path-tail and canonical-JSON siblings in
  the same section.
- Changing the encoder (`ownerKey`) or the sentinel re-point (`ownerKeyFilter`); server changes;
  the label reader (`ownerKeyLabel`).
- Saved-view/router/surface tests: no surface bytes change, so the existing sentinel pins there
  stay as they are.

## Evidence plan

1. Focused vitest: `pnpm --filter @symmetry/frontend exec vitest run
   src/features/projects/filters.test.ts` (green on the pinned bytes; the mutation run above is
   the discriminating red).
2. Gates: `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile`; `pnpm --filter
   @symmetry/frontend typecheck`, `lint`, `test` (full vitest); `git diff --check`.
3. Build sanity: `pnpm --filter @symmetry/frontend build` (production compile; output lands in
   the gitignored `control/priv/static/portal`).
4. Browser render gate: **not required** — the only production-source edit is a comment, no
   production frontend bytes change, and the contract scopes the gate to a production-bytes
   change. Recorded as not run in the report.
5. Runs and exit codes recorded in the stage report (`answer.md`).
