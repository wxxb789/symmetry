# chat-history-chain plan — re-key the in-flight `Load older` guard and re-open an ended chat chain

Contract: `goal-chat-history-chain.md` (GDD, 2026-11-20). Baseline for every coordinate in this
plan: `origin/main` at `fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, the worktree's HEAD).
Frontend-only increment: `frontend/src/features/chat/{scopes.ts,chat.tsx,chat.test.tsx}` plus the
documentation closure (`docs/TODO.md`, `docs/archive/CLOSED-TODO.md`) and this plan file.

Source entries (locate by title in `docs/TODO.md`, read fully before this plan):
1. "The chat surface's in-flight `Load older` guard is still keyed by cursor bytes".
2. "An ended chat chain never accepts a later served token".

## Deliverable shape

1. **`scopes.ts`** — one new pending-press identity rule beside the existing `ownsChainPointer`,
   and the ended-chain re-open in `retainServedPage`.
2. **`chat.tsx`** — the `Load older` guard and its `aria-disabled` move from cursor bytes to
   scope + boundary id (bytes fallback); a refusal the newer chain superseded posts no notice.
3. **`chat.test.tsx`** — one regression case per entry (guard; re-open), one case for the entry's
   second named shape (never-continued), and two existing cases re-pinned to the new contract
   (see "Existing tests the contract re-pins").
4. **`docs/TODO.md` → `docs/archive/CLOSED-TODO.md`** — both completed entries moved with branch,
   decision and pinning test names, per `AGENTS.md`.

## Exact semantics the contract's entries demand

### Entry 1 — guard and superseded refusal

- A pending press is identified by **scope key + boundary id**, with cursor bytes as the fallback
  while either side lacks a boundary id — the same two-sided comparison `ownsChainPointer` already
  uses for the chain pointer. A re-serve that re-signs the same boundary position (bytes change,
  id does not) is still the same press: a second `Load older` for it is not admitted.
- A press for a *different* boundary id (a moved served boundary) is admitted: it loads a region
  the in-flight press never captured, which the existing "two in-flight loads each on the boundary
  its own cursor was minted from" case already exercises.
- The scope key fences the press: `ChatPage` is not remounted on a scope switch, so a press held
  for one scope must neither block nor be blocked by another scope's button.
- `aria-disabled` follows the same rule, so the rendered button reports the boundary it is
  already loading through a renewed token as busy.
- A **definitive refusal** (`definitiveReadRefusal`) whose press no longer owns the chain
  boundary it captured is superseded: it neither rewinds the pointer **nor posts its notice** —
  the pointer belongs to the newer chain the reader's next press walks, so the notice would
  contradict what the reader can see. The re-read gate reads the last committed snapshot while
  the write judges the store's queued record, so within one settle tick a superseded refusal may
  still issue one benign re-read of the captured scope (the source comment in `chat.tsx`
  documents the window).
- A refusal whose press still owns the boundary keeps today's behavior exactly: rewind to the
  served page's own token, re-read the captured scope, notice on the status line, button ready.

### Entry 2 — re-open an ended chain

- "Ended" = `nextCursor === null`: the reader drained the chain through its own loads (the
  recorded `nextBoundaryId` names an older message) or the read served the whole history and no
  continuation was ever established (`nextBoundaryId` stays null).
- A serve that offers a token (`served.nextBefore !== null`) while the chain is ended and the
  served page names a boundary re-opens the chain **at the served boundary**: `nextCursor` takes
  the served token, `nextBoundaryId` the served page's oldest message id. Not-moved serves whose
  token today is dropped become pressable again.
- The re-walk is the reader's own press: the token's boundary is the served page's oldest
  message, so the first page below it re-reads the drained region; `chatHistoryView` dedupes the
  retained pages by id and `historyGap` reaches them (their `fetchBeforeId` is the served
  boundary), so the note reports the region as loaded and contiguous.
- A chain that is not ended keeps today's rule in both directions: a serve at the reader's own
  boundary renews its token, a serve whose boundary *moved* re-points the chain to the served
  token and boundary (the case the existing pin "keeps the chain a refresh re-signs at the same
  boundary and restarts it at a moved one" names), and a not-moved serve whose token does not
  designate the position the reader's loads reached — the reader paged past the served boundary —
  leaves the reader's own continuation alone.
- Accepted consequence, stated rather than hidden: a serve in a *drained* scope now re-offers
  the button even when nothing new is being served, because the server cannot know the client's
  retained set; the reader re-walks the drained region one press per page — each press reads
  exactly one page and the retained copies are deduped only at render — until it drains again.
  This is the direction the entry states ("the re-walk re-reads the drained region and dedupes"),
  and the browser walk's post-drain assertions are on the gap and the id set; its two button
  assertions —
  `#chat-load-older` hidden right after the drain and again after the full API paging walk
  (`browser/tests/portal-chat.spec.mjs:319`, `:359`) — hold because no serve at the same boundary
  intervenes while the chain is ended, and that is what the render gate re-checks.

## Existing tests the contract re-pins (not weakened)

The entry's repro names the current admission — "press Load older again (admitted because the
cursor bytes changed)" — as the defect, and its fix direction reverses the behavior two existing
cases pin:

1. "renews the served cursor at an unchanged boundary only while the reader has not paged past
   it" pins a second press that consumes the renewed token while the first is held
   (`chat.test.tsx`, `requested` = `cursor-5-resigned-2`). Under the new guard that press is not
   admitted. The case is rewritten to the new contract with its surviving clauses kept as
   assertions: the renewal keeps the chain pointer at the reader's own boundary, the held press
   is neither cancelled nor rebound, its page lands deduped, and a re-serve after the reader
   paged past the boundary still cannot re-sign that older position.
2. "leaves the chain alone when the refused press no longer owns it" pins "its refusal stays the
   notice", which the entry names as the residual damage to suppress. Its scenario (renewal
   admitting a second press) is unreachable under the new guard, so it is rewritten with a
   moved-boundary supersession — the other way a press loses ownership — keeping the no-rewind
   and no-re-read assertions and replacing the notice assertion with the suppressed one.

## Evidence plan

1. New and rewritten cases must be **red on the pre-fix bytes and green on the delivered bytes**.
   Method: keep a byte-identical scratch copy of the pre-fix `chat.tsx`/`scopes.ts` (from
   `git show fdf332d:…`), run the new cases against it, record the exact assertion text, then
   restore the delivered bytes and verify the copy digests (`sha256sum -c`) before removing the
   scratch.
2. Touched package: `pnpm --dir frontend exec vitest run src/features/chat --no-file-parallelism`
   and the full `pnpm --dir frontend test`; `typecheck`, `lint` (the frontend has no formatter
   script; `git diff --check` covers whitespace); `pnpm --filter @symmetry/frontend build` proves
   the staged bundle still builds.
3. Path-triggered per `docs/design/quality.md` (frontend touched): the focused release-shell
   render gate with `SYMMETRY_RENDER_GATE_REQUIRED=1`. Attempt it against the local provisioning
   (Postgres on 5432, Playwright browsers cached); if the release instances cannot be built on
   this host, record the gate as **pending with its missing environment** in `answer.md`, never
   as passed.
4. Sequencing: the render gate and the frontend suite never run concurrently (host memory).

## Out of scope (contract clause 4)

- The other open TODO entries (the CI browser runs' live-daemon pagination cases; everything
  outside the two source entries).
- Any refactor beyond the fix: no new query shape, no store API change, no server/contract
  change, no new wire member.
- The `adopt`-branch question the second entry's provenance mentions: no branch is resurrected;
  the rule is stated as "ended has no usable continuation", in `retainServedPage`'s own terms.

## Design trade-offs and decisions

1. **Notice suppression is refusal-only** (contract-preserving ambiguity, decision recorded here):
   the entry says "suppress the superseded refusal's notice", and a *non-refusal* failure's
   outcome is unproven (transient read), so it keeps its notice; only a definitive refusal —
   whose recovery the surface itself performs for an owning press — is suppressed when the chain
   has moved past the press. Suppressing every superseded error would also hide transient
   failures from the reader, which no entry asks for.
2. **One functional store update for the refusal's two effects** (rewind + notice): keeping them
   in one `updateWith` makes the ownership judgment and both writes atomic, instead of the
   current separate `updateWith`/`update` pair whose notice is unconditional.
3. **The pending list holds one record per press, not one slot.** Presses can legitimately be
   concurrent at different boundaries (the moved-boundary case), and each clears only its own
   record, so a landed page never unblocks a button another press is still loading.
4. **No new unit-test file for the identity predicate.** `scopes.ts`'s pure rules are pinned
   through `chat.test.tsx`, the file's existing convention; a unit test of the predicate alone
   would pin an implementation detail the UI cases already cover observably.
5. **Boundary id, not bytes, for the button's busy state.** The button renders
   `aria-disabled` from the same predicate the admission check uses, so the rendered state and
   the guard cannot drift.

## Residual risks

- The re-open rule cannot distinguish "a late-visible row exists below" from "the reader has
  everything": any serve with a token in an ended scope re-offers the button, and the reader's
  press decides by reading. The entry chooses this direction; the cost is one duplicate-read
  press per drained page on every not-moved serve (an explicit refresh, a window-focus refetch, a
  realtime `chat` invalidation, a send), deduped at render, and the query cache's default
  structural sharing keeps a deep-equal re-serve — same-millisecond token bytes with unchanged
  messages — from reaching the surface at all (recorded in `docs/archive/CLOSED-TODO.md` by the
  r1 review round, which accepted the record-level option).
- The two re-pinned cases cover behavior the contract reverses; the report states each old pin
  and its replacement assertion explicitly.
- The render gate may not be reachable on this host; if so it is recorded pending with the
  missing environment, and the merge is decided by CI's control job.

## Annotation (2026-11-21, apply stage)

The Entry 1 sentence calling the guard's identity rule "the same two-sided comparison
`ownsChainPointer` already uses for the chain pointer" is inaccurate about `ownsChainPointer`: it
decides on the captured side's id alone and never falls back to cursor bytes while its own id is
present, while `pendingOlderLoadFor` is two-sided and extended by the scope key. The shipped
predicate is what this plan and the contract demand; only the source comment's wording was
corrected (`scopes.ts`), and `docs/archive/CLOSED-TODO.md` carries the same annotation.
