# legacy-owner-bookmarks plan — re-point legacy work-surface owner bookmarks carrying the pre-#111 sentinel

Contract: `goal-legacy-owner-bookmarks.md` (GDD, 2026-11-20). Baseline for every coordinate in this
plan: `origin/main` at `fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (the worktree's HEAD). Touched
package: `frontend/`, plus the closing documentation move in `docs/`. Closing branch:
`fix/legacy-owner-bookmarks`.

The contract's source entry is the open `docs/TODO.md` item "Re-point legacy work-surface owner
bookmarks that carry the pre-#111 sentinel." (`docs/TODO.md:292-325`, section `## Retro review
follow-ups (second pass, 2026-11-20)`). It carries the reproduction, the fix direction and the pin
location; this plan implements that direction and states the edge-case semantics exactly.

## Public / API shape

One new exported reader in `frontend/src/features/work/presentation.ts`, beside the existing key
codec (`ownerKey` at `:99-106`, `ownerKeyLabel` at `:110-126`):

```ts
/** Reads a filter key that a URL or saved view carries into the key the current encoder mints. */
export function ownerKeyFilter(key: string): string
```

- `agent:unrecorded` → `agent:`
- `human:unrecorded` → `human:`
- every other key → returned unchanged, byte-for-byte.

It is applied at the two places a filter key is read into state, both in
`frontend/src/features/projects/filters.ts`:

1. `workSearch` (`:46-58`), where the `owner` param is read from the router's search record
   (`:50`) — this is the reader every produced `WorkSearch` goes through: the route's
   `validateSearch: workSearch` (`frontend/src/app/router.tsx:47-48`), every `setSearch` in the
   toolbar, `readSavedViews` (`frontend/src/features/projects/saved-views.ts:66`), and the work
   detail's `listState` (`frontend/src/features/work/detail.tsx:70`).
2. `filtersOf` (`:80-87`), where the filter state's `owner` key is built and later compared to
   `ownerKey(item.assignee)` in `filterWorkItems` (`:94`).

No other public shape changes: no new option, route, storage version or wire field. `WorkSearch`,
`WorkFilters`, `ownerKey`, `ownerKeyLabel`, `ownerOptions` and the saved-view schema keep their
signatures.

## Exact edge-case semantics the contract demands

- **The defect.** After #111 (`db31838`) the encoder mints `agent:` for a row with no recorded
  name and escapes a recorded name equal to the legacy word or starting with `:` (`agent::unrecorded`
  for a row genuinely named `unrecorded`, `presentation.ts:94-106`). A bookmark carrying the
  pre-#111 spelling `?owner=agent:unrecorded` therefore matched no row (`filterWorkItems(...)`
  length 0 of 3 in the entry's probe), while the Work owner select rendered
  `ownerKeyLabel("agent:unrecorded")` = "Agent" + " (not in this project)" above the empty list
  (`work-surface.tsx:168-172`).
- **The chosen reading.** The legacy word means the absence it named. The pre-#111 encoder minted
  `agent:unrecorded` for an absent profile/name, so that key cannot be distinguished from a row
  whose recorded name was literally `unrecorded`; the label side already resolves the ambiguity
  toward absence, is already committed and pinned ("labels the legacy sentinel keys old bookmarks
  carry as the absence they named", `frontend/src/features/work/presentation.test.ts:81-89`), and
  this increment makes the filter side agree with it. This is the entry's own wording ("the legacy
  sentinel word maps to the empty-value sentinel the encoder now mints").
- **Semantics table** (pinned in `filters.test.ts` unless noted):

  | Key read | Normalised to | Row it matches |
  | --- | --- | --- |
  | `agent:unrecorded` | `agent:` | the agent row with no profile and no name |
  | `human:unrecorded` | `human:` | the human row with no name |
  | `agent:` | `agent:` | unchanged |
  | `human:` | `human:` | unchanged |
  | `agent::unrecorded` | `agent::unrecorded` | the row whose recorded name is `unrecorded` (a real name, not absence) |
  | `agent::Ada:`, `human::Ada` | unchanged | the escaped recorded names the current encoder mints |
  | `agent:builder`, `human:Ada`, `unassigned`, `type:robot`, `""` | unchanged | unchanged |

- **Idempotence.** The reader is a fixed point on every key it returns, so applying it twice — or
  applying it at both read boundaries as production does (`workSearch` then `filtersOf`) — is the
  same as applying it once.
- **Select honesty after the fix.** With the search normalised, `work-surface.tsx`'s `absentOwner`
  (`:141`) tests the minted key against `ownerOptions` values: a project with an unnamed-agent row
  selects that row's own option ("Agent"), and a project without one renders the absent option
  labelled from the minted key ("Agent (not in this project)") instead of a legacy duplicate. No
  surface file changes.
- **Escaped-name non-collapse.** The reader must not re-read `agent::unrecorded` as the legacy
  word: that is the escape the current encoder added so a real name cannot collide with the
  sentinel, and collapsing it would re-introduce the collision #111 removed.

## Design trade-offs and decisions

1. **One normalising reader, applied at both read boundaries** (entry's first-named fix direction;
   the second, "redirect the legacy value at the surface", is out of scope). Applying it only in
   `workSearch` would leave the entry's own probe shape (`filtersOf({ owner })` on a `WorkSearch`
   literal — the type does not carry the canonical-key invariant) matching nothing, and the filter
   chain would depend on every caller passing through the URL parser; applying it only in
   `filtersOf` would leave the toolbar's `absentOwner`/select reads on the raw legacy key, i.e. the
   "(not in this project) above a non-empty list" presentation the entry names. One function, two
   read sites, idempotent: the search state and the filter state always agree.
2. **Not a surface redirect.** Rewriting the URL on load would drop the operator's bookmark text
   and touch navigation state for a read-side compatibility rule; the repository's existing pattern
   is that readers normalise (`workSearch` already normalises JSON-shaped params, `filtersOf` trims
   `q`), and the entry's chosen direction is the reader.
3. **Placement in `presentation.ts`, not `filters.ts`.** The function encodes the same two facts
   the codec owns — the legacy word (`legacyAbsentValue`, private at `:85`) and the minted
   empty-value sentinel — and sits beside its sibling inverse reader `ownerKeyLabel`. `filters.ts`
   already imports from that module.
4. **No label or encoder change.** `ownerKeyLabel`'s handling of the legacy word is already
   committed and pinned; the encoder is correct and pinned. Only the filter read changes.
5. **No storage migration.** Saved views are decoded through `workSearch` on read
   (`saved-views.ts:66`), so a stored pre-fix view carrying the legacy key is repaired by the same
   reader; the storage document, its version and `sameWorkSearch` are untouched, and a saved view
   with the legacy key now compares as the same search as the URL's repaired key.
6. **No contract/schema change.** No wire member, fixture or generated file changes; the increment
   does not run `contracts:check` because nothing under `contracts/` moves.

## Evidence plan

1. **Regression test** in `frontend/src/features/projects/filters.test.ts` (the entry's named pin
   location): `workSearch` maps the two legacy sentinel words; `filterWorkItems` through
   `filtersOf(workSearch({ owner: "agent:unrecorded" }))` selects the unnamed-agent row; a
   `filtersOf({ owner: "agent:unrecorded" })` literal selects it too; `agent::unrecorded` still
   selects the row whose recorded name is `unrecorded` and not the unnamed one; `human:unrecorded`
   selects the unnamed human row.
2. **Before/after runs** of that test file on the pre-fix and post-fix bytes (red first, then
   green), plus the entry's own reproduction command: a throwaway Node ESM probe importing
   `filters.ts`/`presentation.ts` through a type-stripping resolve hook in the system temp
   directory (never committed), run against the pre-fix and post-fix bytes.
3. **Frontend gates** for the touched package, exact commands and exit codes: `mise exec
   pnpm@12.17.0 -- pnpm install --frozen-lockfile`; `pnpm --filter @symmetry/frontend typecheck`,
   `lint`, `test` (full vitest), `build`.
4. **Path-triggered check** per `docs/design/quality.md` (2026-11-20 amendment): the local browser
   render gate with `SYMMETRY_RENDER_GATE_REQUIRED=1` — the focused release-shell gate's seven spec
   files at both widths (`browser/README.md`), run against a `MIX_ENV=prod mix release` built from
   this tree, three release instances (4000 rollout on, 4001 expiry with
   `SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2`, 4002 rollout off) and a fresh
   `symmetry_control_render` database migrated through `SymmetryControl.Release.migrate`. Recorded
   carrier deviations from the CI step: the prod endpoint binds IPv6-any
   (`control/config/runtime.exs:155-162`) while this platform's sockets are v6-only, so the suite's
   `127.0.0.1` target is served by a raw TCP relay (`127.0.0.1:4100-4102` → `[::1]:4000-4002`, no
   rewriting and no application change); the fixture's `mise exec elixir@1.20.4-otp-29
   erlang@29.1.2 -- mix` resolves through install-directory junctions to the pinned Elixir 1.20.4 /
   OTP 29.0.6 toolchain; `RELEASE_DISTRIBUTION=none` because a release node name cannot register on
   this host. Outcome — three full runs plus an isolation pass: run 1 `88 passed / 5 failed /
   1 skipped` (13.2 m; four failures came from a stale `SYMMETRY_FIXTURE_MIX` export left in the
   shell, one is the recorded load class), run 2 `89 passed / 4 failed / 1 skipped` (13.7 m; two
   fixture `mix run` database-pool drops, one 60 s test timeout on a fixture case, one Playwright
   trace-write `ENOENT` in the shared temp output directory — the class recorded at
   `docs/evidence/goal-0007c-safe-work-planning-2026-11-16.md:759-763` and
   `docs/evidence/goal-0007g-scoped-engineering-chat-2026-11-17.md:611-615`), run 3 with a private
   `SYMMETRY_PLAYWRIGHT_OUTPUT` `92 passed / 1 failed / 1 skipped` (16.9 m; the single failure is
   the fixture-heavy `portal-goal-attention.spec.mjs:435` desktop case exceeding its 60 s timeout),
   and an isolation pass of the failing cases at both widths `4 passed / 2 failed`. No failure was
   on the changed owner-key path: the owner-filter end-to-end case
   (`browser/tests/portal-work-views.spec.mjs:117`, list/board/filters over real served rows)
   passed at both widths in every run, every failing case passed in at least one other run on the
   same bytes, and the residual failures are the documented host-contention class of `docs/TODO.md`
   "Browser acceptance under host load" (a second session held 36 database connections on the
   shared server throughout). CI remains the merge gate.

## Out of scope (contract clause 4)

- Every other `docs/TODO.md` entry, including the two siblings in the same second-pass section.
- The label side (`ownerKeyLabel`), the encoder (`ownerKey`) and the `type:` family — correct and
  pinned by #111; no legacy sentinel existed there.
- Any refactor of `work-surface.tsx`, saved-view storage, or the owner-option projection beyond
  what the two reader call sites need.
- Contract/schema/fixture changes: the fix is client-side read compatibility for a first release
  that is not shipped; no backward-compatibility obligation was accepted.
