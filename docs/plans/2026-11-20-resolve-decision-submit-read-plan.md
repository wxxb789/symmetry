# resolve-decision-submit-read plan — build the Goal surface's resolve_decision press from a submit-time read

Contract: `goal-resolve-decision-submit-read.md` (GDD, 2026-11-20). Baseline for every coordinate
in this plan and in the delivered entries: `origin/main` at
`fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, the worktree's HEAD). Frontend-only
increment: `frontend/src/features/goals/detail.tsx`, its vitest suite
`frontend/src/features/goals/goal-attention.test.tsx`, `docs/TODO.md`,
`docs/archive/CLOSED-TODO.md` and this plan file.

## Owner decision (binding)

Option A (owner, 2026-11-20, quoted in the contract): the press envelope is built from a
submit-time served read the way the plan admission and the page's sibling commands
(`servedEnvelope`, `detail.tsx:91`) already are, re-deriving the presented decision's own version
from that same read, **and** the existing retry-freeze rule stays: a retry of the same press
resends identical bytes (owner Q4=A). No backward compatibility is required (first release not
shipped).

## Deliverable shape

1. `frontend/src/features/goals/detail.tsx` — the plan-approval press:
   - the `resolve_decision` envelope's `expected_version`/`expected_revision` come from the goal
     read taken at submit (`application.api.goal`, the same call `servedEnvelope` makes), not from
     `query.data`;
   - `expected_decision_version` comes from the served decision found by id in that same read
     (`current.decisions.find(...).version`), not from the decision object the page rendered;
   - both envelopes of the press (resolution, then admission) are frozen per logical intent until
     the server answers, so a lost acknowledgement resends the recorded bytes instead of a body
     rebuilt from a fresh read.
2. `frontend/src/features/goals/goal-attention.test.tsx` — two new cases (one per behavior change):
   the served-version press, and the byte-identical retry. Both are red on the pre-fix bytes.
3. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md`: the entry moved verbatim under
   `## Retro review follow-ups` with a closure paragraph naming the owner decision, the branch and
   the pinning test names (the AGENTS.md TODO rule); the file header note gains the move sentence.
4. This plan file.

No Elixir, Go, contract-schema, generated-binding or browser-spec change: the wire shape of
`resolve_decision` and the server's validation are unchanged, and no gate or fixture moves.

## Exact edge-case semantics

- **Press input.** The press still targets `decision.id` and `optionId` from the rendered card
  (`planApprovalOption` drives the control); the decision *identity* is page state, the decision
  *version* is server state and must be read at submit. `comment` stays
  `"Accepted from the Goal surface."`; `proposal`/`proposal_hash` stay the values
  `planApprovalCommand` produced for the pasted document.
- **Decision no longer served by the read.** `find` returns `undefined`; the surface refuses
  locally ("The server no longer serves that decision; reload the current state.") and sends
  nothing. This branch is total-function narrowing, not a reachable server state: the read
  preloads every `GoalDecision` row of the goal (`read_model.ex:391` `:decisions`, served through
  `portal_json.ex:190-191`), and nothing deletes a decision row. It exists because falling back to
  the page-held decision would reintroduce exactly the presented-stale-state defect the owner
  decision removes.
- **Server refusal the fix removes.** `ensure_preconditions!/2`
  (`control/lib/symmetry_control/goals.ex:8571-8585`) compares `expected_version` to the goal's
  `lock_version` and `expected_revision` to `current_revision` before any command runs; an
  asynchronous reconcile/goal-control advance alone makes the page-held pair stale (409 `stale`,
  `protocol.ex:33`). Every accepted command also bumps the goal's `lock_version` through
  `append_event!/7` (`goals.ex:8308`, `optimistic_lock` at `:8359`), so the page-held pair is stale
  after any accepted sibling press too.
- **Decision-version re-derivation.** `resolve_decision` requires `decision.lock_version ==
  expected_decision_version` (`goals.ex:6484-6493`), else 409 `invalid_decision`
  (`protocol.ex:45`). The presented version must therefore be the one the served read carries. The
  one writer that bumps an open decision's `lock_version` without resolving it is the amend branch
  (`goals.ex:6327-6333`, `inc: [lock_version: 1]` with `state: "superseded"`), which also advances
  the revision and so already refuses; the re-derivation is still required because the press may
  only present the version the read it presents carries, and the envelope's pair must come from
  one read.
- **Retry freeze.** Freeze key: `<decision.id>|<optionId>|<proposalHash>`. Frozen values: the
  resolution envelope and, once produced, the admission envelope. Release rule mirrors the generic
  command (`detail.tsx` `sentEnvelope`): released on success and on any answer that settles the
  attempt (`MutationRefused`, session expiry, unreadable answer), kept on `Network` /
  `UnexpectedResponse` so the next press repeats the recorded bytes. Server-side, a repeated
  request replays only when its body hashes equal to the recorded `request_hash`
  (`replay_command`/`replay_event`, `goals.ex:8278-8305`); a fresh read after an applied resolution
  would carry the post-application goal version and the resolved decision's version (409 `stale` /
  `invalid_decision` for a command that already applied). The admission is frozen with the attempt
  for the same reason: after the resolution applied, its own read produces the only body the
  server will replay.
- **Ordering.** First attempt and retry both send resolution → (fresh read, unless frozen) →
  admission, so a retry after the resolution's lost acknowledgement replays the resolution, takes
  the fresh read the applied resolution produced, and admits with it.

## Evidence plan

1. `mise x pnpm@12.17.0 -- pnpm install --frozen-lockfile` at the repository root.
2. Red-first: add both vitest cases and run
   `mise x pnpm@12.17.0 -- pnpm --filter @symmetry/frontend test src/features/goals/goal-attention.test.tsx`
   on the unmodified `detail.tsx`, recording the failures and exit code.
3. Apply the source change; rerun the same file (green), then the gates the contract names:
   `pnpm --filter @symmetry/frontend typecheck`, `... lint`, `... test` (full suite),
   `... build`.
4. Path-triggered render gate per `docs/design/quality.md` (Amendment 2026-11-20, owner Q10C):
   the CI render step's spec list with `SYMMETRY_RENDER_GATE_REQUIRED=1` and the expiry and
   Goal-rollout-off URLs. The CI step consumes a Linux release artifact; on this host the same
   spec list and gate environment ran against three `mix phx.server` instances serving this
   worktree's freshly staged bundle (the substitution the host's own record already used, 0007i),
   backed by a private PostgreSQL instance. Outcome: the full invocation was killed at its
   40-minute bound after 29/94 cases with 21 timeout-shaped failures while three other browser
   gates shared the host — recorded pending, never passed. The spec case that presses this surface
   ("plan approval, activation, pause, resume and amendment run through the delegated commands
   with server versions", `browser/tests/portal-goal-attention.spec.mjs:220`) then ran alone with
   a 300 s test timeout and passed on both Chromium projects (exit 0, 2 passed, 2.4 min), which is
   the changed path rendered and driven end to end by a real browser against the real server.
5. Both runs (red, green) with their exact commands and outputs are recorded in the run's
   `answer.md`.

## Out of scope (contract clause 4)

- Any other TODO entry; the `can_retry` pin and the served-read entries stay untouched.
- No server-side change: no new writer for an open decision's `lock_version`, no protocol or
  contract-schema edit.
- The pre-existing two-step fragility after the *admission's* own refusal (a re-press after an
  answered `accept_plan` refusal re-derives against the now-resolved decision and is refused
  `invalid_decision`) is unchanged by this increment; the entry records the resolution press, and
  the frozen-attempt rule covers the unanswered case the entry is about.
- The browser spec is unchanged: it already presses the same control and asserts both command
  statuses; the increment does not add a browser case.

## Design trade-offs and decisions

1. **Freeze the whole press, not only the resolution envelope.** The contract's retry-freeze rule
   says a retry of the same press resends identical bytes. With only the resolution frozen, a
   retry after the admission's lost acknowledgement would re-read and re-admit with a fresh
   identity — a body the server refuses for a command that already applied. Freezing both
   envelopes is the same rule applied to the press's actual command pair, and it is what makes the
   resolution's own replay meaningful: the admission bytes are then reproducible too.
2. **A read per press, resolution first.** The read happens inside the press (not on render), so a
   page left open for hours still presents what the server serves at submit; the alternative
   (refetch on focus/interval) would add a second read cadence and still race.
3. **`servedEnvelope`'s doc comment is corrected, not the helper.** The helper's sentence "(and
   both plan-approval commands) mints one" is no longer true for the resolution; the comment is
   updated in place and the helper keeps its signature (the admission still calls it).
4. **Local refusal for a decision the read no longer serves** instead of sending the page-held
   pair: sending a command built from state the server has stopped serving is the defect class,
   and the refusal names the operator's next step. Trade-off: this path yields the surface's own
   message rather than a server code — accepted because the read is the authority for what this
   press may present, and the pattern already exists on this page ("The goal is not loaded.",
   "The session token is unavailable; refresh the page.").
5. **Assertion level.** The new cases assert the request bodies the surface sends and the notice
   it renders, not internal refs: the exact wire bytes are the contract the server validates
   (`ensure_preconditions!/2`, `RequestHash`), and the "not 409" clause is what the handler
   emulates.

## Residual risks

- The new cases emulate the server's two checks (envelope pair and decision version) in the
  fixture handler; they do not execute Elixir. The server's side of every refusal cited above is
  read from `goals.ex`/`protocol.ex`, not re-executed by this increment (the control suite covers
  `resolve_decision` refusal shapes; no control test is added here).
- The render gate's local environment is heavy (prod release + fixture database + three
  instances); if it cannot run in this worktree it is recorded pending with the missing
  environment, per the quality policy, and the increment's behavioral evidence rests on the
  vitest pair and the full frontend suite.
- Editing the pasted proposal between two presses of the same decision is a different intent (a
  different freeze key) and therefore a new attempt: the resolution is re-sent with a fresh
  identity against a decision the first attempt may already have resolved, so the server answers
  `invalid_decision` instead of replaying. This is the generic command's documented rule ("a
  changed payload is a different intent with a new identity"), applied to a press whose resolution
  payload does not carry the proposal; the increment does not invent a second identity rule.
- The render gate's full spec list ran pending (killed at the harness bound under host contention;
  the per-case failures are Playwright timeouts that also hit cases this increment cannot affect),
  and the focused run of this surface's case used a 300 s test timeout instead of the suite's 60 s
  under the same load. Neither run substitutes for the hosted gate; the changed path's behavioral
  evidence is the vitest pair, and the focused browser pass is integration evidence only.
