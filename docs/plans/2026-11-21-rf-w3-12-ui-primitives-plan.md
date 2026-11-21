# rf-w3-12-ui-primitives plan — one status badge, one field-error line, one dialog opener, one resource dialog, one editor field grid

Contract: `goal-rf-w3-12-ui-primitives.md` (GDD, 2026-11-21), binding with its conductor
decision (2026-11-21): `components/focus.ts` stays **byte-identical** and is *delegated to*; the
`data-*` census is 29 occurrences across 7 non-test files with exactly one `data-status` writer;
the two web-controller citation-comment blocks join the owned set (comment-only); the
−150…−250 LOC band is report-only (D2). Base for every coordinate below:
`c6b2c352cd60ae66c839e713fca2353d25a69f08` (= `origin/main` at dispatch — PR #164
`rf-w2-15-served-value-module` merged — and this worktree's HEAD, clean tree), so the contract's
D6 rebase is a no-op and every line reference is re-derived at it. Shape references:
`docs/plans/2026-11-21-rf-w2-15-served-value-module-plan.md` (#164, same area, same wave-2
dispatch shape) and `docs/plans/2026-11-21-rf-w1-10-frontend-eslint-plan.md` (#146, which
reformatted this tree and owns the `--max-warnings 229` ratchet this slice must not raise).

## Measured basis (base revision)

Declaration census (`grep -rn "function StatusBadge\|const StatusBadge" frontend/src`, same for
`FieldErrors`; every count below is from that command at `c6b2c352`):

| name | locations | policy it renders |
| --- | --- | --- |
| `StatusBadge` | `components/workspace.tsx:58`, `features/connections/resources.tsx:55` | workspace states (`knownStatuses` Set, `statusLabel` text, `status-complete`+`CircleCheck` on `done`, no `data-status`) vs the three resource scopes (own-key maps, capitalized label, `status-complete`/`status-attention`, `data-status`) |
| `FieldErrors` | `features/connections/resources.tsx:63`, `features/planning/editors.tsx:29` | identical bodies; a `ServerRefusal` field's messages joined with a space, `role="alert"`, `null` when empty |
| `restoreFocus` | `components/focus.ts:8` (15 L, exported) | opener → fallback, each kept only while `isConnected && !:disabled`; no target ⇒ Radix default |
| opener capture + restore | `features/planning/editors.tsx:50 useOpenerRef` + `:54 restoreOpener`, call sites `:133`, `:439` | `document.activeElement` at mount, `null` fallback |
| `useWorkFocusRestore` | `features/projects/work-surface.tsx:61` | board-item focus, **not** an opener restore — contract non-goal |
| `focusContent` | `app/shell.tsx:13` | DOM-id target (`content-heading`), no opener, no event |
| resource dialogs | `resources.tsx`: `DialogFrame:181`, `BindResourceDialog:258`, `UpdateResourceDialog:357`, `UnbindResourceDialog:461`, mounted `:657-665` | three dialect bodies over one frame, each mounting its own `useMutation` and its own opener/fallback props |
| editor forms | `editors.tsx`: `ProjectEditor:83` (form `:140`), `WorkItemEditorDialog:425` (form `:446`) | two `.planning-form` grids, 21 label+control+`FieldErrors` rows |

`data-*` census at the base revision (non-test): 29 occurrences in `app/shell.tsx`,
`features/chat/chat.tsx`, `features/connections/resources.tsx`,
`features/projects/work-surface.tsx`, `features/runtimes/runtimes.tsx`,
`features/work/control.tsx`, `features/work/detail.tsx`; `data-status` has exactly one writer
(`resources.tsx:60`, inside the resource-scope badge — the contract's `:59` is a one-line
delta from #164).

LOC baseline: `components/focus.ts` 15, `components/workspace.tsx` 63,
`features/connections/resources.tsx` 668, `features/planning/editors.tsx` 583,
`features/connections/connections.tsx` 503, `app/shell.tsx` 280.

Test pins: `connections.test.tsx` 1,535 L / 57 `it` cases, `planning.test.tsx` 1,012 L / 35,
`work/control.test.tsx` 516 L / 10 `it` sites + one `it.each` (13 executed); 20 test files in
`src/`. The suites select by
`getByRole`/`getByLabelText`/visible text; the resource dialogs' names ("Bind resource",
"Update resource", `Unbind <name>?`), close labels ("Close bind dialog", …), field labels
("Type", "Provider connection", "Name", "Reference"), button text ("Bind", "Save",
"Unbind resource", "Cancel"), the per-field `role="alert"` and the stale/terminal refusal
suffixes are all asserted; no test reads `data-status`.

Citation blocks that join the owned set (comment-only; re-derived at this revision, the
contract's `:142-158`/`:37-50` are the pre-#161 numbers):
`control/test/symmetry_control_web/controllers/portal_resources_test.exs:200-216` and
`portal_workflow_test.exs:37-50`. Their frontend `file:line` pins that this slice moves:
`features/connections/resources.tsx:104-118,132,138,139-142`;
`features/runtimes/runtimes.tsx:16,17,24,25,37-41,50-56,81,85,95`;
`features/projects/projects.tsx:89,108`; `features/work/detail.tsx:309-342,319,369`
(all verified to read what the comment claims before the change).

## Deliverable shape

1. **`frontend/src/components/primitives.tsx` (new)** — the shared display primitives, each with
   one definition and a policy comment:
   - `formatLabel(value)` — moved out of `resources.tsx` (`_` → space), the transform the
     resource-scope badge capitalizes;
   - `knownWorkspaceStatuses` (own-key record, Set-equivalent), `knownResourceStatuses`,
     `knownSyncStatuses` (own-key records) — moved with their badges;
   - `statusLabel(status)` — moved from `workspace.tsx` (exported: `projects/work-surface.tsx`
     is its only other consumer);
   - `StatusBadge({ status, scope = "workspace" })` with
     `scope: "workspace" | "connection" | "resource" | "sync"`, reproducing both policies
     branch-for-branch (see "Exact edge-case semantics");
   - `FieldErrors({ error, field })` — the `resources.tsx` body, one definition;
   - `Field({ id, label, error, field, children })` — **the shared editor field grid row**:
     a Fragment of `<label htmlFor>`, the control, and `FieldErrors`. A Fragment, not an
     element: `.planning-form` is a CSS grid over its direct children
     (`styles.css:225-228`), so a wrapper element would collapse three grid rows into one.
2. **`frontend/src/components/use-dialog-opener.ts` (new)** — `useDialogOpener(openerRef?,
   fallbackFocus?)`: captures `document.activeElement` at first render when the caller owns no
   ref, adopts a caller-owned opener ref and fallback ref when passed, and returns
   `{ restore }` where `restore(event)` clears the opener and **delegates** to
   `restoreFocus/3` from `./focus`.
3. **`frontend/src/components/workspace.tsx`** — keeps `PageHeading`, `LoadingState`,
   `EmptyState`, `ErrorState`, `RefreshButton`; loses `knownStatuses`, `statusLabel`,
   `StatusBadge` and the `CircleCheck` import.
4. **`frontend/src/components/focus.ts`** — untouched, byte-identical (mutation (d)'s target).
5. **`frontend/src/features/connections/resources.tsx`** —
   - `StatusBadge`/`FieldErrors`/`formatLabel` definitions deleted; imported from
     `../../components/primitives`;
   - `DialogFrame` keeps its exported name and prop shape, and its `onCloseAutoFocus` delegates
     to `useDialogOpener(openerRef, fallbackFocus).restore` (the same clear-then-restore
     sequence, now one implementation);
   - the three dialect dialogs replaced by one `ResourceDialog` whose `intent` is the
     discriminated union `{ kind: "bind"; projectId } | { kind: "update"; resource } |
     { kind: "unbind"; resource }`; the per-intent copy (title, close label, description id
     `resource-<kind>-description`, description text) is one `switch`, and the two field sets
     stay two presentational components (`BindResourceFields`, `UpdateResourceFields`) because
     each intent's form is different data, not different machinery — the machinery
     (mutation, `onMutate`/`onSettled` subject counts, `onError: onRefresh`, the freeze rule,
     the notice, the actions row) exists once;
   - `ProjectResourcesReady` computes the intent from the existing `editor`/`editorTarget`
     state via `dialogIntentOf` (a row removed by a background refresh still closes the
     dialog through the existing `editorSubjectRemoved` path) and mounts one `ResourceDialog`
     with the same `key={editor.id}`.
6. **`frontend/src/features/planning/editors.tsx`** — local `FieldErrors`, `useOpenerRef`,
   `restoreOpener` deleted; `Field` imported for all 21 field rows (labels, order, ids,
   `required`/`maxLength`/`rows`, disabled conditions and the create-only/update-only
   conditional blocks are unchanged; the "Blocked" checkbox row stays inline because its label
   wraps its control); the draft state exposes `restore` instead of `openerRef`; `ServerRefusal`
   and `type RefObject` drop from the imports.
7. **Import-line-only edits** (the single definition moved out of `workspace.tsx`):
   `features/attention/attention.tsx:6`, `features/goals/detail.tsx:7`,
   `features/projects/projects.tsx:7`, `features/projects/work-surface.tsx:7` (also
   `statusLabel`), `features/runtimes/runtimes.tsx:10`, `features/work/detail.tsx:8`, plus
   `features/connections/connections.tsx:19` (`StatusBadge`, `FieldErrors`, `formatLabel` now
   come from `../../components/primitives`; `DialogFrame`, `ProjectResources`,
   `providerLabel`, `refusalNotice` keep coming from `./resources`). Each file gains exactly
   one import line, so each cited line below it shifts by exactly +1.
8. **`frontend/src/components/primitives.test.tsx` (new)** — the primitive-level pins for the
   merged branches: the two badge policies (workspace and all three resource scopes, the tone
   classes, `data-status`, the prototype-named status, the unknown fallback with and without a
   status, `FieldErrors` joining and its null case, the `Field` row's direct-child structure)
   and the opener policy (opener, disabled opener → fallback, disconnected opener → fallback,
   neither → no focus change and no `preventDefault`, caller-owned ref cleared, `activeElement`
   capture at mount). No component test file exists today; this is the first one.
9. **Citation comments** — refreshed only where a cited frontend line moved (see "Evidence
   plan").
10. **This plan file.**

No other file changes. `docs/TODO.md` / `docs/archive/CLOSED-TODO.md` do not move: no open entry
names this surface. `docs/design/frontend.md:20` describes `components/` as "reused UI
compositions" — still true, so no design document is made stale; no other document in `docs/`
names `StatusBadge`, `FieldErrors`, `DialogFrame`, `restoreFocus` or `.planning-form` outside
historical plans/evidence (grep over `docs/**/*.md`).

## API shape

```ts
// frontend/src/components/primitives.tsx
export const formatLabel: (value: string) => string;                       // `_` -> space
export function statusLabel(status: string): string;                       // workspace policy
type StatusScope = "workspace" | "connection" | "resource" | "sync";        // module-local
export function StatusBadge({ status, scope }: { status: string; scope?: StatusScope }): ReactElement;
export function FieldErrors({ error, field }: { error: unknown; field: string }): ReactElement | null;
export function Field({ id, label, error, field, children }: {
  id: string; label: string; error: unknown; field: string; children: ReactNode;
}): ReactElement;                                                          // Fragment row

// frontend/src/components/use-dialog-opener.ts
export function useDialogOpener(
  openerRef?: RefObject<HTMLElement | null>,
  fallbackFocus?: RefObject<HTMLElement | null>,
): { restore: (event: Event) => void };

// frontend/src/features/connections/resources.tsx (module-local)
type ResourceIntent =
  | { kind: "bind"; projectId: string }
  | { kind: "update"; resource: ResourceRow }
  | { kind: "unbind"; resource: ResourceRow };
type ResourceSubmission =
  | { kind: "bind"; projectId: string; draft: ResourceDraft }
  | { kind: "update"; resourceId: string; version: number; update: { name: string; connection_id: string | null; external_ref: string | null } }
  | { kind: "unbind"; resourceId: string; version: number; name: string };
```

`DialogFrame`'s exported props are unchanged (`{ title, closeLabel, descriptionId, description,
openerRef, fallbackFocus, onClose, children }`), so `connections.tsx`'s connection dialog keeps
its call shape apart from the import line. Nothing is re-exported anywhere.

## Exact edge-case semantics the contract demands

- **Workspace badge scope** (default): `knownWorkspaceStatuses` own-key membership decides
  `statusLabel` (`done` → `done`; `unknown_thing` → `Unknown status: unknown_thing`; `""` →
  `Unknown status`); `done` adds `status-complete` **and** the `CircleCheck` icon (both
  together, as today); no `data-status`; the class stays
  `` `status-badge ${done ? "status-complete" : ""}` `` byte-for-byte.
- **Resource scopes** (`connection`/`resource`/`sync`): `sync` reads `knownSyncStatuses`, the
  other two read `knownResourceStatuses`, both through `ownValue`, so a served
  `constructor`/`toString` is *not* known (`Object.hasOwn`, never a prototype member); a known
  status renders the capitalized word (`syncing` → `Syncing`, `in_review` → `In Review` for a
  hypothetical multi-word member); `healthy`/`synced` add `status-complete`,
  `degraded`/`failed`/`offline` add `status-attention`, everything else no tone class;
  `data-status={status}` is always present; an unknown status renders
  `` `Unknown ${scope} status: ${status || "unspecified"}` `` — the literal `specified` word
  included.
- **`FieldErrors`**: only a `ServerRefusal` with a non-empty array for the field renders;
  messages are joined with a single space inside one `<p className="field-error" role="alert">`;
  a different field, a non-refusal error, `fields: null` and an empty array all render `null`
  (no empty alert in the DOM).
- **Opener restore policy** (delegated, so `focus.ts` stays the single authority): the opener is
  kept only while connected and not `:disabled`; otherwise the fallback under the same rule;
  otherwise no `preventDefault` and no focus move (Radix's own default). The opener ref is
  cleared before restoring, so a second close cannot re-target a stale element. Editors pass no
  refs (activeElement capture, `null` fallback); the connection and resource dialogs pass the
  page's `openerRef` and the section heading as fallback.
- **Resource dialog freeze rule** (unchanged): a bind never freezes (no version);
  an update freezes on `stale`; an unbind freezes on any of `stale`, `state_conflict`,
  `not_found`. The refusal notice keeps the update's "This resource changed elsewhere; close and
  reopen it to edit the server's current values." and the unbind's "Close and reopen this dialog
  to act on the server's current state." suffixes and stays the only `role="alert"` refusal line.
- **Bind completeness gate** (unchanged): for a local kind (`agent`, `runtime`) the draft is
  incomplete while no reference is registered or none is selected; otherwise while no
  connection is selected — the submit control is the only one gated by completeness.
- **Dialog copy** (unchanged, asserted by tests): titles, close labels, description ids
  (`resource-bind-description`, `resource-update-description`, `resource-unbind-description`),
  the `<dl className="connection-facts">` facts in the update form, every option string
  ("No connection grants the required capability", "Not required for local resources",
  "No registered … is available", "`<ref>` (not registered)", "`<name>` (capability not
  granted)", "Unknown connection: `<id>`"), the button wording ("Bind", "Save",
  "Unbind resource", "Cancel") and the success notices
  (`Resource attached — <name>.` / `Resource updated — <name>.` / `Resource detached — <name>.`).
- **Success/refusal accounting** (unchanged): `onWriteStart`/`onSubjectPending(subject, true)` on
  mutate, both released on settle, `onSaved(editorId, notice)` on success, `onRefresh` on error —
  with the subject key `project:<projectId>` for a bind and `resource:<resourceId>` for the
  other two.

## Decisions

- **D1 — the merged primitives live in a new `components/primitives.tsx`, not in
  `workspace.tsx`.** Options: (A) new module; (B) keep them in `workspace.tsx` and have
  `resources.tsx`/`editors.tsx` import from there. The contract's owned set names the new file
  and the "single definition moves out of `workspace.tsx`" import-line rule. (A) separates two
  concerns that are currently one file's job: page-state shells (`PageHeading`, `LoadingState`,
  `EmptyState`, `ErrorState`, `RefreshButton`) vs display primitives; (B) would make the
  resource surface import its badge from a module named for workspace shells and would leave
  `FieldErrors`/`Field` homeless. Chosen: **(A)**; six import lines move, all inside the owned
  set.
- **D2 — `formatLabel` moves with the badge.** The resource-scope label is
  `capitalize(formatLabel(status))`; keeping the function in `resources.tsx` while the badge
  lives in `primitives.tsx` would either duplicate the one-liner or create an import cycle.
  Chosen: move it, update `connections.tsx`'s import line (its one other consumer). Alternative
  considered and rejected: inlining `status.replaceAll("_", " ")` in the badge, which hides the
  shared transform behind a second copy.
- **D3 — the resource collapse is one dialog + two field components, not one 200-line
  function.** The lint ratchet counts `max-lines-per-function` (150, blank/comment-skipping) and
  `complexity` (25) findings in the 229 ceiling, and the three dialect bodies total ~260 lines:
  a single function holding all three forms would add findings (the `--max-warnings 229` number
  must not be raised). The field sets are also genuinely different data (a bind has a kind
  selector and a completeness gate; an update has the served facts and a freeze rule; an unbind
  has none), while the machinery is genuinely identical. Chosen: one `ResourceDialog` (frame,
  state, mutation, gate, notice, actions) + `BindResourceFields`/`UpdateResourceFields`.
  Alternative considered and rejected: three thin wrappers over a shared frame — that keeps
  three dialect functions and does not satisfy clause 1's "one `ResourceDialog` with a
  three-variant intent".
- **D4 — the `Field` row is a Fragment, and the checkbox row stays inline.**
  A wrapper `element` would break `.planning-form`'s grid over direct children
  (`styles.css:225`); the "Blocked" row's label wraps its control and carries
  `className="checkbox-field"`, so folding it into `Field` would change the DOM. The plan
  accepted the ~+1 line per field this costs (the LOC band is report-only): the contract
  requires the shared grid to exist, and the direct-child structure is its stated invariant.
- **D5 — `restore` is called from `DialogFrame`, not from each dialog body.** The frame is where
  `onCloseAutoFocus` lives for both the connection dialog and the resource dialogs, so one hook
  call there serves all of them and `connections.tsx` keeps its call shape (import line only).
  Editors call `useDialogOpener()` themselves because their `Dialog.Content` is their own.
- **D6 — `shell.tsx`'s `focusContent` is left unchanged.** The contract says it *may* delegate;
  it is not an opener restore (no invoking control, no fallback, no event to `preventDefault`),
  and `restoreFocus` requires an event. Delegating would also replace
  `focus({ preventScroll: true })` with a bare `focus()`, a behaviour change the contract's
  failure control ("a focus-restore regression") forbids. The file is in the owned set but needs
  no edit.
- **D7 — citation refresh is by +1 shift, then verified.** Each cited importer gains exactly one
  import line, so its cited lines move by exactly +1; `resources.tsx`'s four cited sites are
  re-derived by content (the decoders are not edited). Every remaining `file:line` is verified
  to read what the comment claims at the final tree, by printing the cited lines.

## Evidence plan (what will be run, and what proves what)

All gates foreground and serialized (never a vitest run beside another suite on this host).

| what | command (from `frontend/` unless noted) | proves |
| --- | --- | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | every moved import resolves; no stale name survives in `resources`/`editors`; no unused import left |
| lint ratchet | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | exit 0 at `--max-warnings 229`: the merge introduces no new typed-lint finding (the ceiling is not raised) |
| three DOM suites | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` with `vitest run src/features/connections/connections.test.tsx src/features/planning/planning.test.tsx src/features/work/control.test.tsx` | all pinned selectors, aria names, button text, focus targets and refusal text render as before |
| new primitive test | included in the three-suite run and the full run | the merged branches are pinned at the primitive level |
| full suite | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | 20 files, 418 + the new cases, 0 failed |
| definition counts | `grep -rn "function StatusBadge\|const StatusBadge" src`, same for `FieldErrors` and `restoreFocus`/`useDialogOpener` | exactly one definition each; one opener hook |
| mutation (a) | delete the workspace `done` tone, run the primitive test | red: the `status-complete` assertion |
| mutation (b) | delete `data-status` from the resource-scope badge, run the primitive test | red: the `data-status` assertion |
| mutation (c) | render the bare capitalized label for an unknown status, run the primitive test | red: the "Unknown \<scope\> status" assertion |
| mutation (d) | drop the `:disabled` check in `components/focus.ts`, run `connections.test.tsx` | red: `connections.test.tsx:783` (pending-save close), the delegated witness; `:807` is the removal twin |
| citation proof | `grep`/`sed` of every cited `file:line` at the final tree | each pin resolves to the reading line the comment names |
| diff hygiene | `git diff --check`; the program's case-insensitive profile-directory grep over `git diff origin/main...HEAD` | no whitespace damage; no host path in the diff |
| LOC delta | `git diff --stat HEAD -- frontend/src` after the change | reported against the −150…−250 estimate (report-only, D2) |

Judging artifacts for the answer: the count greps, the four flip runs (red output), the suite
outputs with counts, the refreshed citation comments, and the LOC delta. No test is renamed,
deleted or weakened; one new test file is added.

## Out of scope (with reasons)

- Refusal/outcome classification (`services/refusals.ts`, `work/assignment.tsx`,
  `planning/write-policy.ts`) — contract clause 4, W4-10.
- `useWorkFocusRestore` (`projects/work-surface.tsx:61`) — explicit non-goal; its import line
  changes only because `StatusBadge`/`statusLabel` moved.
- `components/focus.ts` — byte-identical, its unowned importer (`work/assignment.tsx:7`) keeps
  working.
- CSS (`styles.css`) — the class and tone strings are the contract; the `.planning-form` grid
  rules are untouched.
- `connections.tsx` beyond its import line — its settings/removal dialogs keep their own
  `restoreFocus` call (the contract's "import line only"; the removal dialog's direct
  `restoreFocus` use is not an opener-hook requirement).
- `app/shell.tsx` — see D6.
- The vitest pool/harness work (W4-11), `api.ts` boundaries (W3-13), ChatPage decomposition
  (W4-08), the goal-settle poll (W5-04).

## Residual risks / known limits

- The three dialect bodies were ~260 lines; the collapse's LOC saving is partly offset by the
  `Field` row's +1 line per field. The delta is reported, not gated (D2), with the breakdown.
- `ResourceDialog` holds bind/update draft state for the unbind intent too (unconditional hooks
  in one component); the state is never rendered for an unbind and its initializer reads the
  served row, which is cheap and side-effect-free. Recorded as the price of one dialog function
  instead of three.
- The merged badge keeps two label policies in one function (workspace vs resource). They are
  selected by `scope`, and the default is the workspace policy, so no caller needed a change
  beyond its import line; a future author adding a scope must add its map deliberately.
- `primitives.test.tsx` pins the `Field` row's direct-child structure; if a future design wants a
  wrapper element, the CSS grid rule and the test are the two places that must change together.

## Gate results

Appended at implement time with the real exit codes and counts (see "Gate results" at the end of
this file).

## Implement-stage result (final tree, 2026-11-21)

Base unchanged (`c6b2c352`): #164 is HEAD, so the D6 rebase was a no-op and no line reference moved
for that reason. All gates foreground, serialized; no measurement harness ran beside a suite.

### Definition census (the clause-2 greps, on the final tree)

| command (from `frontend/`) | result |
| --- | --- |
| `grep -rn "function StatusBadge\|const StatusBadge" src` | **1** — `src/components/primitives.tsx:33` |
| `grep -rn "function FieldErrors\|const FieldErrors" src` | **1** — `src/components/primitives.tsx:48` |
| `grep -rn "function useDialogOpener" src` | **1** — `src/components/use-dialog-opener.ts:18` |
| `grep -rn "function restoreFocus" src` | **1** — `src/components/focus.ts:8`; the file is byte-identical to HEAD (`diff <(git show HEAD:…) <file` empty, exit 0) |
| `grep -rn "BindResourceDialog\|UpdateResourceDialog\|UnbindResourceDialog\|useOpenerRef\|restoreOpener" src` | **0** (the only matches in the repo are this plan's "measured basis" prose) |
| `grep -rn "data-status" src --include=*.tsx \| grep -v test` | **1 writer** — `src/components/primitives.tsx:44` (was `resources.tsx:60`) |
| non-test `data-*` occurrences | 29 (unchanged census) |
| `<Field ` rows / `<FieldErrors` rows in `editors.tsx` | 21 / 1 (the "Blocked" checkbox row keeps its inline message; its label wraps its control) |

### Gates

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | 0 | no output |
| lint | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | 0 | `227 problems (0 errors, 227 warnings)` — 2 below the `--max-warnings 229` ceiling, which was **not** touched (see the ratchet note) |
| three DOM suites + the new primitive test | `pnpm --pm-on-fail=ignore exec vitest run src/features/connections/connections.test.tsx src/features/planning/planning.test.tsx src/features/work/control.test.tsx src/components/primitives.test.tsx` | 0 | 4 files / 117 tests passed, 76.97 s and 79.81 s on two runs |
| per-file counts of that run | same run, `--reporter=json` | 0 | `connections` 57, `planning` 35, `control` 13, `primitives` 12 (declared `it` sites at head: 57/35/10, plus one `it.each` → the 13 executed in `control`) |
| full suite (default invocation) | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | 0 | **21 files / 522 tests passed**, 120.84 s, on the final tree (`frontend/`'s `vitest run`, the package script's own command); the same suite was green at 94.17 s before the export trims. Three earlier attempts of this exact command returned 17 / 27 / 30 **timeout** failures in suites outside the diff while the host was loaded — jsdom environment creation 575 s in the failing runs against 364 s here and 250 s in the fastest green one; a `--maxWorkers=2` diagnostic on the same tree was green (exit 0, 522 passed, 353.25 s). No failing case was in a file this slice changed, and the three DOM suites plus the new test file passed together (4 files / 117) in the same session |
| knip (report-only) | `pnpm --pm-on-fail=ignore knip` | 0 | no findings; one pre-existing config hint (`coverage/**` ignoreFiles), as at #164 |
| diff hygiene | `git diff --check` | 0 | no whitespace damage |
| host-path check | the program's profile-directory grep over `git diff origin/main...HEAD` | 1 | no host path in the diff; the four new files carry none either |

Lint ratchet detail (`eslint . -f json`, counted per rule): `complexity` 14 → **13** and
`prefer-nullish-coalescing` 8 → **7**; every other rule holds its recorded count. No rule
increased. The two burns-down are the merge's own: the collapsed update dialog's branch count is
now split across `ResourceDialog` and `UpdateResourceFields` (both under 25), and the two
`FieldErrors` copies became one. `frontend/package.json`'s `--max-warnings 229` is outside this
slice's owned set and unchanged; the ceiling is now 2 above the measured count, so the W1-10
owner may lower it in a later slice (reported, not silently re-pinned).

### Mutation checks (each flip applied alone, run, reverted; `focus.ts` restored byte-identical)

| flip | command | exit | witness |
| --- | ---: | ---: | --- |
| (a) workspace `done` loses `status-complete` | `vitest run src/components/primitives.test.tsx -t "renders the workspace states"` | 1 | `1 failed \| 11 skipped` — the class assertion |
| (b) resource-scope unknown status loses `data-status` | `… -t "names an unknown resource status"` | 1 | `1 failed \| 11 skipped` — `Expected the element to have attribute` (missing) |
| (c) the `Unknown <scope> status` fallback renders a bare label | `… -t "names an unknown resource status"` | 1 | `1 failed \| 11 skipped` — `Unable to find an element with the text: Unknown resource status: future_kind` |
| (d) `restoreFocus` drops the disabled-control check | `vitest run src/features/connections/connections.test.tsx -t "keeps focus in the detail when the editor closes while its save is pending"` | 1 | `1 failed \| 56 skipped` — `expected <body …> not to be <body …>` (focus dropped to the body) through the delegated `DialogFrame` → `useDialogOpener` → `restoreFocus` path, the contract's witness at `connections.test.tsx:783` |

### Citation-comment refresh (comment-only; every changed line in `control/` starts with `#`)

Cited frontend lines that moved, old → new: `resources.tsx:104-118 → 88-102`, `:132 → 116`,
`:138 → 122`, `:139-142 → 123-126`; `runtimes.tsx:81,85,95 → 82,86,96`, `:17,25,37-41 → 18,26,38-42`,
`:16,50-56 → 17,51-57`, `:24 → 25`; `projects.tsx:89,108 → 90,109`;
`work/detail.tsx:309-342,369 → 310-343,370`, `:319 → 320`. Every remaining `file:line` in both
blocks was printed at the final tree and reads what the comment claims (including the untouched
`write-policy.ts`, `assignment.tsx`, `control.tsx`, `presentation.ts` pins). The Elixir formatter
gate was not run: `mix` is not provisioned for this checkout (`mise` has no version set for the
shim) and every changed line is a comment.

### LOC delta against the −150…−250 estimate (report-only, D2)

| file | head | final | delta |
| --- | ---: | ---: | ---: |
| `components/focus.ts` | 15 | 15 | 0 (byte-identical) |
| `components/workspace.tsx` | 63 | 46 | −17 |
| `components/primitives.tsx` (new) | — | 66 | +66 |
| `components/use-dialog-opener.ts` (new) | — | 27 | +27 |
| `features/connections/resources.tsx` | 668 | 713 | +45 |
| `features/planning/editors.tsx` | 583 | 563 | −20 |
| six workspace-scope importers + `connections.tsx` | — | — | +7 (one import line each) |
| **production net** | | | **+108** |
| `components/primitives.test.tsx` (new) | — | 186 | +186 (test) |

The estimate does not reproduce: the net is **+108**, not −150…−250. Where the lines went, measured:
the two primitives' merge and the badge/error removals save ~28 lines, the `Field` grid is a wash
per row (label + message collapse into the wrapper tag, the two `</>`-style tags cost the same), and
the dialog collapse trades its per-dialect duplication for named helpers (intent/submission/draft
types, `resourceSubmission`, `resourceDialogCopy`, `resourceSavedNotice`, `resourceRefusalNotice`)
plus two field components — 247 → 305 lines in that region, while the machinery, the freeze rule
and the copy exist once instead of three times. The `components/` split also adds the two new files'
headers. Reported, not gated (D2); the duplication removed is real (4 → 2 focus-restore paths, 3 → 1
dialog, 2 → 1 badge, 2 → 1 field-error line, 21 → 1 field-row shape).

### Closeout state

The repo-side increment is the plan + source + the refreshed citation comments; the contract file
(`<HOST-PATH>/.local/state/gdd/contracts/goal-rf-w3-12-ui-primitives.md`) and the ledger row are
conductor-owned and outside the repository: this slice must not commit, and the previous merged
slice's contract file still reads `Status: queued` with the ledger carrying its `done` row, so
nothing in-repo is archived or re-indexed here. No design/contract document is made stale:
`docs/design/frontend.md:20` still describes `components/` as "reused UI compositions", no other
document in `docs/**/*.md` names `StatusBadge`, `FieldErrors`, `DialogFrame`, `restoreFocus` or
`.planning-form` outside historical plans/evidence, and `docs/TODO.md` / `docs/archive/CLOSED-TODO.md`
hold no entry for this surface.

## Apply-stage note (2026-11-21, simplify apply)

Conductor's apply brief (`rf-w3-12-ui-primitives-apply.md`): accept every finding inside the owned
set that does not weaken tests/types/validation/permissions; reject outside-owned-set findings
unless an amendment extends the set. Disposition: **D1–D7, R1–R5, T1–T4 applied; T5, N1, PD3
recorded with no change; PD1, PD2 rejected.**

Applied, all inside the owned set (`focus.ts` byte-identical throughout, sha256
`85cc1708406f11e942140b5a1f25df5babe8c61bcee9c4b260eaec8ab4f1de63`): D1 the hook returns
`{ restore }` only (no caller read the dead `openerRef`); D2 the in-function own-key comment deleted
(the table-side one kept); D3 the false "identical across the three" sentence deleted; D4 the two
extra `editors.tsx` focus-restore comment copies deleted (`:60-62`, `:210`); D5 the hook header's
caller census deleted (caller-owned-ref/`activeElement` semantics and the clear-before-delegating
sentence kept — documented behaviour); D6 the ownership/narrative header sentence deleted
(import-direction rule kept); D7 four self-restating one-liners deleted; R1 the 7 bind/update rows
now use `Field` (28 rows, one shape; `Field`'s doc covers `.connection-form`); R2
`resourceRefusalNotice(intent, error, frozen)` and `submitBlocked = blocked || (bind && incomplete)`;
R3 the bind's `referenceOptions` is computed once per render in `ResourceDialog` and passed down;
R4 `ResourceDraftState` is `{connectionId, name, reference}` with the kind selector as its own
bind-scoped state (the pre-refactor shape); R5 `dialogIntentOf` returns
`{ kind: editor.kind, resource: target }` (narrowed after the bind guard; the full inline was not
taken — reviewer split, nested ternary); T1 `constructor` assertions added to the workspace and sync
unknown-status cases; T2 a non-refusal object carrying `fields` added to the `FieldErrors` null
case; T3 focus assertions added to two clean-close cases (project editor, work-item editor); T4
`defaultPrevented === [true]` added to both fallback cases.

No change, the finding's own reason kept: **T5** (it recommends no action — encoding the ordering
would pin an internal ordering by invoking the handler twice; the gap stays recorded); **N1**
(verified unreachable at implement time; no fix); **PD3** (its own text offers "gate on failure
instead of notice for byte-exact parity; otherwise accept" — the delta is accepted: an empty
`role="alert"` announces nothing and no test reaches the edge). Rejected: **PD1** (`docs/TODO.md`
is editable here only when a TODO entry is closed; no amendment extends it — disclosure below);
**PD2** (`planning/write-policy.ts` is W4-10's file and an explicit non-goal; disclosure only).

Superseded numbers (single source, re-derived on this tree): census pins `FieldErrors`
`primitives.tsx:48 → 46`, `data-status` writer `primitives.tsx:44 → 42`, `useDialogOpener`
`use-dialog-opener.ts:18 → 16`; LOC `primitives.tsx 66 → 64`, `use-dialog-opener.ts 27 → 25`,
`resources.tsx 713 → 706`, `editors.tsx 563 → 559`, `primitives.test.tsx 186 → 196`,
`planning.test.tsx 1012 → 1018`; production net `+108 → +93`; `<Field ` rows `resources.tsx` 7 +
`editors.tsx` 21; citation pins `resources.tsx 88-102 → 85-99`, `116 → 113`, `122 → 119`,
`123-126 → 120-123`; the DOM suites' declared cases 57/35/13 + 12 are unchanged (117 together),
so the full suite stays 21 files / 522 cases.

Gates on this tree (all foreground, serialized; logs under
`<HOST-PATH>/.local/state/gdd/runs/2026-11-21/symmetry-rf-w3-12-ui-primitives-apply/`):

| gate | exit | counts |
| --- | ---: | --- |
| typecheck (final tree, after the flip set) | 0 | `tsc --noEmit`, no output, 16.5 s |
| lint | 0 | `227 problems (0 errors, 227 warnings)`; the `--max-warnings 229` ceiling is not touched |
| four DOM suites (default invocation) | 0 | 4 files / 117 tests passed, 101.19 s (`gates-4file-default-rerun.log`); five other attempts of the same command under host load (one of them `--maxWorkers=2`) returned 1 / 3 / 6 / 1 / 1 failed tests, every failure a `findBy*` 1 s wait or a 5 s test timeout in `control.test.tsx`'s `openDetail` and other untouched find-wait points (`control.test.tsx` alone: 13/13 twice, one 1-failed run) |
| full suite (default invocation) | 1 | attempt 1: 1 failed / 521 passed (a `control.test.tsx` find-timeout); attempt 2: 17 failed / 505 passed (the plan's recorded load signature — wall-clock timeouts across 9 files, none in the diff's assertions) |
| full suite `--maxWorkers=2` diagnostic | 0 | **21 files / 522 tests passed, 282.08 s** |
| knip (report-only) | 0 | no findings; the one pre-existing `coverage/**` hint |
| definition census | — | `StatusBadge` 1 (`primitives.tsx:33`), `FieldErrors` 1 (`:46`), `useDialogOpener` 1 (`use-dialog-opener.ts:16`), `restoreFocus` 1 (`focus.ts:8`), the three dialect names 0, `data-status` one writer (`primitives.tsx:42`) |
| citation proof | — | every `file:line` in both comment blocks printed at this tree; the four `resources.tsx` pins re-derived by content and refreshed |
| diff hygiene | 0 | `git diff --check` clean; no profile path in the diff; `frontend/package.json` untouched |
| apply-stage flips (each applied alone, reverted) | 1 each | (f) workspace plain index → `1 failed` (`Unknown status: constructor`); (g) sync plain index → `1 failed` (`Unknown sync status: constructor`); (h) `instanceof ServerRefusal` dropped → `1 failed`; (i) project editor `onCloseAutoFocus` dropped → `1 failed` at the new `toHaveFocus`; (j) the focus.ts fallback arm dropped → `2 failed`; (k) the contract witness (d) re-run (disabled check dropped) → `1 failed`, focus to body. `focus.ts` sha256 restored after every flip |

Disclosure for the conductor (PD1, recorded, not applied — `docs/TODO.md` is outside the strict
owned set): the live citation block at `docs/TODO.md:130-134` refers to
`frontend/src/features/goals/detail.tsx`, whose lines moved +1 in this range (an import line split
in two above them). Measured on this tree vs the base revision `79548368`: the block's `:408` lands
on a doc-comment line, the function is at base `:412` / now `:413`; the `:381-382` pair is now
`:382-383`; the `accept_plan` note is at base `:538` / now `:539` against the cited `:533`; and the
`actionPayload` `default: return null` is at base `:631-632` / now `:632-633` against the cited
`:626-627`. Three of the four citations were already stale at the base revision (that is, before
this range); the +1 shift adds one more to each. PD2 (plain index for a served refusal code in
`planning/write-policy.ts:135`) stays W4-10's.

## Fix-stage note (2026-11-21, review findings r1)

Conductor brief `rf-w3-12-ui-primitives-fix1.md` (findings from
`<HOST-PATH>/.local/state/gdd/runs/2026-11-21/symmetry-rf-w3-12-ui-primitives-review/answer.md`)
plus the conductor's STD-A10 decision extending the owned set by one comment-only refresh of
`docs/TODO.md:130-134`. All ten findings (STD-A01…STD-A10) addressed; none judged invalid; no
test, type, validation or permission weakened — `primitives.test.tsx` gained assertions and two
cases (14, was 12).

- **STD-A01** — the deliverable shape and the API block now state the shipped interfaces: the
  hook returns `{ restore }` only (`:81`, `:153`), `StatusScope` is module-local (`:142`), and
  `ResourceSubmission` carries `projectId`/`resourceId`/`version` (`:161-163`).
- **STD-A02** — `resourceIntent/3` → `dialogIntentOf` (`:102`).
- **STD-A03** — `knownWorkspaceStatuses` is an own-key record, Set-equivalent (deliverable `:66`;
  edge-case list `:172`). The measured-basis row `:22` keeps "(`knownStatuses` Set)" because it
  records the base revision, where the workspace badge did use a `Set`.
- **STD-A04** — `connections.test.tsx` is 57 `it` cases at base and head, not 56 (`:42`), and
  the declared-vs-executed metric is labelled (`:351`: 57/35/10 `it` sites, `control`'s one
  `it.each` → 13 executed).
- **STD-A05** — the hook header now names its actual callers (the shared dialog frame and the
  planning editors); the connection-removal and assignment dialogs keep their own `restoreFocus`
  calls.
- **STD-A06/A07/A08/A09** — `primitives.test.tsx`: `data-status` pinned on every resource scope
  (including `""` → `unspecified` and `constructor`) and its absence pinned on the workspace
  badge; the two cross-scope pairs (`healthy`@`sync`, `synced`@`resource`) pinned as unknown; the
  `FieldErrors` join compared as exact `textContent`; added the both-enabled case (opener wins)
  and the both-disabled case, and renamed the no-fallback case to what it renders.
- **STD-A10** — `docs/TODO.md` citations re-derived by content at head: `:408 → :413` (the
  function) and `:408 → :414` (the citation's second occurrence, "the `open?` gate"), the
  `PlanApproval` render `:381-382 → :388` (the apply note's `:382-383` reading was a blind +1),
  `:533 → :539`, `:626-627 → :632-633`, and `:218 → :224` (the frozen-attempt release this range
  moved; the apply note did not list that occurrence). No prose or status change.

Superseded numbers (single source, re-derived on this tree): `primitives.test.tsx` 196 → 253 L,
12 → 14 cases; the four-file run 117 → 119; the full-suite total 522 → 524 (arithmetic; the full
suite is conductor-owned and was not re-run at this stage). `primitives.tsx` stays 64 L and
`use-dialog-opener.ts` stays 25 L.

Gates on this tree (all foreground, serialized):

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit`, no output |
| lint | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | 0 | `227 problems (0 errors, 227 warnings)`; `--max-warnings 229` untouched |
| four DOM suites (default invocation) | `pnpm --pm-on-fail=ignore exec vitest run …/connections.test.tsx …/planning.test.tsx …/work/control.test.tsx …/components/primitives.test.tsx` | 0 | **4 files / 119 tests passed**, 92.26 s; the first attempt of the same command returned 2 failed / 117 passed (119), whose visible stack was `control.test.tsx:157` `openDetail`'s 1 s heading find-wait (the recorded host-load signature), and `control.test.tsx` alone (13/13) and `primitives.test.tsx` alone (14/14) were both green immediately after |
| definition census | the clause-2 greps | — | `StatusBadge` 1, `FieldErrors` 1, `useDialogOpener` 1, `restoreFocus` 1 (`focus.ts` untouched, byte-identical), the three dialect names 0, `data-status` one writer (`primitives.tsx:42`), `<Field ` rows 21 + 7 |
| citation proof | every `file:line` in the refreshed TODO block printed at this tree | — | `detail.tsx:413/388/539/632-633/224/414` each read what the prose claims; both `.exs` comment blocks are untouched and no frontend line they cite moved at this stage |
| diff hygiene | `git diff --check` | 0 | clean |
| host-path check | profile-directory grep over `git diff origin/main...HEAD` and over the working tree | 1 | no host path in either |

## Fix-stage note (2026-11-21, review findings r2)

Comment-only. Round-2 finding P2-1: the r1 fix reworded the `resources.tsx` module header
comment, 2 → 4 lines, shifting every later line by +2, so the four pins in the
`portal_resources_test.exs:200-216` block went stale by +2. Refreshed and each printed at
this tree: `resources.tsx 85-99 → 87-101`, `113 → 115`, `119 → 121`, `120-123 → 122-125`
(the apply note's pins at `:460` and the citation-proof row above — "no frontend line they
cite moved" — are superseded for these four pins; no other frontend line the block cites
moved). The tree-wide `resources.tsx:<line>` scan (`git grep -n "resources\.tsx:[0-9]"`)
leaves only dated records: the measured-basis rows (`:22-23`, `:35`, `:55`, `:340`, `:376`),
the W2-15 and served-doc-gates plans, and `docs/archive/CLOSED-TODO.md`. Production net
`+93 → +97`; `resources.tsx` `706 → 710` L.
