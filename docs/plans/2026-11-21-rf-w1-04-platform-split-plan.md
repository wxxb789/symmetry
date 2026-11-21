# rf-w1-04 platform split plan — daemon-platform god-file split (move-only)

Contract: the GDD goal contract `goal-rf-w1-04-platform-split.md` (2026-11-21, at the GDD
state root), plus its binding review amendments
(`refactor/plan-review-ds-1.md` P2-4/A8, `refactor/plan-review-opus-1.md` P2-1/P2-2).
Baseline for every coordinate in this plan: `origin/main` at
`a3039f0d17fc7c7905b63bdf247cac5bf339b213` (the worktree's HEAD). Branch:
`refactor/rf-w1-04-platform-split`. Area: `daemon/internal/platform` only.

The increment moves top-level declarations, by role, out of four god files into 15 new
same-package siblings. No declaration is edited, renamed or re-sorted: every declaration
travels as the exact source bytes it occupied at the baseline, and the movement is judged by
a declaration-set comparison plus `git diff --color-moved`. The slice exists to make the
later slices that own these regions (`W2-11` typed error identities, `W3-02` response
builder, `W4-02` session objects, `W5-09` test-only helpers) local edits instead of
god-file surgery.

## What moved (measured on this tree)

| Source (`//go:build`) | Decls | Residual | Siblings (decls) |
| --- | --- | --- | --- |
| `linux_owner_anchor_linux.go` (`linux && amd64`) | 194 | 23 (the daemon-side anchor client + its clone-cleanup helpers) | `anchor_server.go` 39, `anchor_run.go` 37, `anchor_proc.go` 31, `anchor_gate.go` 20, `anchor_wire.go` 16, `anchor_types.go` 28 |
| `linux_owner_helper_v2_linux.go` (`linux && amd64`) | 178 | 46 (the daemon-side v2 client, its lease handle, its error types) | `helper_v2_stop.go` 37, `helper_v2_protocol.go` 30, `helper_v2_server.go` 24, `helper_v2_recovery.go` 22, `helper_v2_run.go` 19 |
| `containment_supervisor_linux.go` (`linux`) | 150 | 50 (shared wire/endpoint primitives, daemon-side recovery entry points, test scaffolding) | `supervisor_linux_client.go` 71, `supervisor_linux_helper.go` 29 |
| `containment_supervisor_windows.go` (`windows`) | 158 | 62 (shared line protocol, endpoint/secret primitives, declarations, test doubles) | `supervisor_windows_client.go` 63, `supervisor_windows_helper.go` 33 |

Line counts after the split (`wc -l`): anchor 7085 → 1020 + 1888 + 1569 + 1013 + 944 + 365 +
402 = 7201, helper 5903 → 1203 + 1620 + 1210 + 603 + 982 + 375 = 5993, supervisor-linux
4194 → 968 + 2042 + 1228 = 4238, supervisor-windows 3652 → 876 + 1777 + 1040 = 3693. The
delta against each source (116, 90, 44, 41 lines) is exactly the regenerated per-file header,
build line and recomputed import block; no declaration line is added or removed.

## Public / API shape

Unchanged, and provable as such: the package's exported surface is identical because every
top-level declaration keeps its name, kind, receiver and body. The slot W3-02 needs is now
local: the eight anchor response writers (`writeResponse` and the seven
`writeResponseWith*` variants: `writeResponseWithStopEvidence`,
`writeResponseWithCloneRemoval`, `writeResponseWithFDs`, `writeResponseWithReapEvidence`,
`writeResponseWithChildClassification`, `writeResponseWithHelperExit`,
`writeResponseWithRunPins`) and every `validLinuxOwnerAnchor*Request` validator live in
`anchor_server.go`; the anchor request/response structs and their codec live in
`anchor_wire.go`.

## Role rules (the mechanically re-runnable part)

Two ordered rule tables per source file drive the split; the driver
(`refactor/tools/w1-04-platform-split/classify.py`) refuses to emit a mapping that leaves a
declaration unassigned or names a file outside the contract's sibling set.

1. **Declarations** (`type`, `const`, `var`) match the declaration rules first. A single-subject
   type follows its subject (`*StopEvidence*` → `anchor_run.go`, `*Gate*`/`*Witness*`/`*Grant*`
   → `anchor_gate.go`, `*Request*`/`*Response*`/`*Frame*` → the wire file). Everything else falls
   to `anchor_types.go` (the anchor's shared data model, including the 26-name protocol constant
   block and the model's `Clone`/`Equal`/`Validate` helpers). When a source file has no `types`
   sibling the declaration fallback is the source file itself, so declarations stay with their
   subject wherever the subject is unmistakable.
2. **Behaviour** (`func`, `method`) matches the behaviour rules; unmatched declarations fall to
   the residual. Each role file's residual is the source's own subject: the daemon-side client
   for the two owner files, the shared protocol/endpoint/recovery core for the two supervisor
   files. Role-neutral material (test doubles such as `noopContainmentSupervisorLease`, the
   opt-in witness runners, `isGoTestBinary`) stays in the residual deliberately: `CLOSED-TODO.md`
   binds `noopContainmentSupervisorLease` to `containment_supervisor_windows.go`, and keeping the
   declaration there keeps that prose true.
3. **Per-decl assignments** are the reviewable artifact: `map.json` lists every declaration id,
   `classify.py -v` prints the file-by-decl grouping with the rule index that placed it, and
   `pfs inventory` prints the pre-split inventory it was derived from.

## Edge-case semantics the contract demands

- **Build constraints are copied verbatim** (amendment A8/P2-1). The six `anchor_*` and five
  `helper_v2_*` siblings carry `//go:build linux && amd64`, `supervisor_linux_*` carries
  `//go:build linux`, `supervisor_windows_*` carries `//go:build windows` — the exact constraint
  of their source. Verified by `go list` per target: on `linux/arm64` the split's visible files
  are exactly the residual `containment_supervisor_linux.go`, the two new siblings
  `supervisor_linux_client.go` and `supervisor_linux_helper.go`, and the pre-existing
  `linux_owner_anchor_unsupported.go` stand-in (no `anchor_*`/`helper_v2_*` sibling leaks into
  the arm64 build). The same rule holds on every target, measured as the package's `GoFiles`
  count against the baseline: 14 → 16 on `linux/arm64` and 14 → 16 on `windows/amd64` (+2
  supervisor siblings each), 13 → 13 on `darwin/amd64`, and 15 → 28 on `linux/amd64` (+6
  `anchor_*`, +5 `helper_v2_*`, +2 `supervisor_linux_*`).
- **One file per role, one residual per source.** No sibling file is empty and no declaration is
  assigned twice; `pfs apply` fails loudly on either.
- **Import blocks are recomputed per file** from the identifiers the file's own declarations use
  (qualified selectors only, so a parameter named `path` cannot keep an unused import). This is
  the only non-move edit, and it is forced by Go's per-file import scope. `gofmt` then only
  collapses now-redundant blank lines between declarations.
- **Doc citations.** The five `docs/TODO.md` coordinates into `containment_supervisor_linux.go`
  (`verifyMirrorObservation` and `sealInitialMirrorScanForWitness` at their call sites, the
  `sealInitialMirrorScanForWitness` and `waitForScanEvents` timer arms, and the `serveScanEvents`
  close) are re-pointed to `supervisor_linux_client.go` and their line numbers re-derived from the
  new bytes, with one added sentence naming the split. The move of four declarations into one file
  with a regenerated header shifts all five by the same −578 lines; each new coordinate was read
  back from the file.

## Evidence plan (exact commands, run at the final tree)

| Gate | Command | Result |
| --- | --- | --- |
| format | `gofmt -l daemon/internal/platform` | empty |
| vet | `go vet ./...` (host windows/amd64), `GOOS=linux go vet ./...`, `GOOS=darwin go vet ./...` | exit 0 each |
| vet (pre-existing limit) | `GOOS=linux GOARCH=arm64 go vet ./...` | exit 1: one diagnostic in `internal/state/linux_clone_journal_test.go:1077` (`reader.verify` is declared only in `internal/state/linux_clone_cleanup_reader_linux_amd64.go`, `//go:build linux && amd64`, while the test file is `//go:build linux`). No file of this slice is involved, and the amended arm64 oracle is `go build`, which is exit 0 |
| builds | `GOOS=linux go build ./...`, `GOOS=windows go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...`, `GOOS=darwin go build ./...` | exit 0 each |
| package tests | `go test -count=1 ./internal/platform/` | `ok ... 7.139s`, exit 0 |
| daemon suite (supplementary) | `go test -count=1 ./...` in `daemon/`, twice | run 1 on the tree before the final classification fix: exit 0, 22 `ok`, 0 `FAIL`. Run 2 on this tree: exit 1 with one failure, `internal/harness/pi` `TestNativeRepositoryTaskSubjectUsesRepositoryBaseline` (`adapter_native_work_test.go:1114: git add timed out`, a 10 s git deadline, under eight concurrently heavy packages); that package alone is `ok 6.480s` exit 0 and the named test `-count=3` is exit 0. The same failure and the same isolated-re-run remedy are already recorded for this host at `docs/archive/CLOSED-TODO.md:3249-3253` |
| movement | `pfs declset` over the whole `internal/platform` corpus, before (`git archive HEAD`) vs after | 1901 = 1901 sorted `decl`/`comment` digest lines, `diff` exit 0 |
| movement (reviewer aid) | `git diff --color-moved=zebra` against a scratch commit of this tree | 19 files, 17058 insertions / 16767 deletions; 16736 deleted and 16732 added lines recognized as moved, leaving 31 and 326 unmoved — exactly the regenerated per-file headers and import blocks. The four originals are pure-deletion diffs: 0 inserted lines outside their import block |
| whitespace | `git diff --check` | exit 0 |
| doc citations | `refactor/tools/w1-04-platform-split/run-citation-gate.sh` (full audited corpus at a scratch head containing this tree) | exit 0, 10 documents, 496 path citations; negative control exit 1 with the expected refusal |

The declaration-set comparison is the primary proof and is stronger than the move colouring:
for every top-level declaration of the package it prints `decl<TAB>kind<TAB>name<TAB>sha256(raw
source range)<TAB>sha256(go/printer normalization)` and, for every comment group,
`comment<TAB>sha256(raw text)` — sorted, compared as a multiset. Equality means every
declaration's bytes and every comment's bytes are preserved exactly, that none was dropped,
added or duplicated, and that gofmt changed no declaration. Build-constraint comment groups are
excluded from that multiset (each new file necessarily repeats its source's build line) and are
checked instead by the `go list` file sets above.

## Design trade-offs

- **Generated, not hand-edited.** 680 declarations across four files (`git diff` would be ~875 KB
  of hunks) cannot be moved by hand without transcription risk. The split is a Go AST tool plus a
  rule table, both kept outside the repository (in the GDD state tree at
  `refactor/tools/w1-04-platform-split/`), so a rebase re-run is
  `classify.py && pfs apply -src <git-show staging> -dst daemon/internal/platform -map map.json`
  with no review of individual lines. The tools read pristine sources (`git show HEAD:<path>`)
  and never edit the AST, only re-emit source ranges.
- **Rule tables over per-declaration hand lists.** A rule table makes the *policy* reviewable
  and reproducible on a later revision; hand lists would have to be rebuilt per rebase. Decls
  whose role a rule cannot express are the residual, not a silent guess.
- **Residuals keep the source's own subject.** Alternatives were: (a) move nothing into the
  residual and invent client files the contract does not authorise — rejected, the sibling list is
  strict; (b) empty the god files completely — rejected, it would scatter cross-cutting
  primitives (`errOr`, the shared JSON/line codecs, endpoint identity) away from their only
  users. The residual naming matches the file's original identity, so `git log -p` on a moved
  region stays understandable.
- **`anchor_types.go` exists only in the anchor family** because the contract names it only
  there, and the anchor is the one file whose declarations are dominated by a shared data model
  (35 declarations of kind `type`/`const`/`var`: 26 types, 7 const blocks, 2 var blocks; 26 of
  them travel to their subject's role file and 9 stay in `anchor_types.go` as the shared model).
  The other three families keep their declarations with their subject or in the residual.

## Out of scope (with reasons)

- **P-06 session objects and complexity work** (`W4-02`) and **the 34 functions over 100 lines**:
  this slice moves declarations; it does not shrink `serveConnection` (493 lines) or
  `runLinuxSupervisorHelperLoop` (311 lines, 12 parameters). Splitting those bodies is a logic
  edit and belongs to the slices that own them after this split.
- **Witness scaffolding** (`W5-01`) and **platform dead test-only helpers** (`W5-09`): the
  test-only functions stay in the residual where they were, unchanged; `W5-09` moves them into
  tests with its own acceptance.
- **Renames, error-identity sentinels, `errOr` removal** (`W2-11`): no symbol was renamed; `errOr`
  and the text-matching helpers stay exactly where they were.
- **Historical documents.** Dates evidence records (`docs/evidence/**`), merged plans
  (`docs/plans/**`), evaluation receipts (`docs/evaluations/**`) and closed TODO entries
  (`docs/archive/CLOSED-TODO.md`) bind the revision they audited; their `file:line` coordinates
  are not live addresses. Re-pointing them would rewrite history and would touch files outside
  this slice's authority. The audited corpus (`docs/runbooks/**`, `docs/design/quality.md`,
  `docs/archive/CLOSED-TODO.md`) was instead run as the citation gate at this tree's head and
  passes; the one prose binding into a moved symbol
  (`noopContainmentSupervisorLease` (`containment_supervisor_windows.go:934`) in
  `CLOSED-TODO.md:1937`) still holds because that declaration stayed in its file.
- **Any behaviour, protocol, journal, fence, credential or provider-field change**: none.

## Handoff: later-wave ownership must be re-derived over the new names

`plan-review-opus-1.md` P2-8 requires wave-2 ownership to name post-split files. Measured on
this tree:

- **W2-11** (typed error identities, `errOr` removal): `errOr` stays in
  `linux_owner_anchor_linux.go`; the anchor sentinels `ErrLinuxOwnerAnchorHelperAlreadyStarted`
  and `errLinuxOwnerAnchorCleanupAuthority` moved to `anchor_types.go`; the helper's
  `ErrLinuxOwnerHelperV2*` block and the supervisor's `ErrLinuxSupervisor*` block stay in their
  source files. The identification sites that compare or print them are spread across the new
  siblings, so the slice must own those regions, not only the two old file names.
- **W3-02** (response builder + request-shape table): the eight anchor response writers
  (`writeResponse` and the seven `writeResponseWith*`) plus the three
  `validLinuxOwnerAnchor*Request` validators are in `anchor_server.go`; the request/response
  structs, `linuxOwnerAnchorResponse.matches` and the codec (`readLinuxOwnerAnchorRequest`,
  `readLinuxOwnerAnchorResponse`) are in `anchor_wire.go`.
- **W5-09** (test-only helpers): `installLinuxSupervisorScanFailureInjection` and
  `containmentEndpointForIdentity` stay in the source files that W5-09 already names;
  `readLinuxOwnerChildPIDs` and `readLinuxOwnerChildPIDsBounded` now live in
  `helper_v2_run.go`.
- **W4-02** (session objects) inherits the new layout as a whole.

## Ambiguities resolved (options and the chosen reading)

1. **Owned-file set vs the mandated plan document and the citation amendments.** The contract
   restricts edits to the four sources, their new siblings, and the two TODO files; the stage
   template requires a plan in the repo's own plan location, and the binding amendment requires
   citing documents to be updated. Chosen: `docs/plans/2026-11-21-rf-w1-04-platform-split-plan.md`
   (new file, no concurrent writer) and `docs/TODO.md` (already owned) are edited; nothing else
   in `docs/` is. Alternative rejected: skipping the plan file, which would leave the increment
   without its required design record and its citation re-point unfindable.
2. **`git diff --color-moved` vs declaration-set comparison.** The contract accepts either; both
   are produced, but only the multiset equality claims "proof" — the move colouring is a
   reviewer aid (GitHub does not render it), and it cannot see a body edit inside a moved block.
3. **Declarations in a file without a `types` sibling.** Options: give them their own sibling
   (unauthorised file name) or leave them in the residual by subject. Chosen: by subject, fallback
   residual, as stated in the role rules.

## Comment-policy sweep (2026-11-22)

Module intent removed from this slice's files (`daemon/internal/platform/anchor_gate.go`,
`anchor_proc.go`, `anchor_run.go`, `helper_v2_protocol.go`) by the 2026-11-22 comment-policy sweep is
recorded in [`docs/evidence/comment-policy-sweep-2026-11-22-moved-intent.md`](../evidence/comment-policy-sweep-2026-11-22-moved-intent.md)
("ws1-platform-anchor"); the sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
