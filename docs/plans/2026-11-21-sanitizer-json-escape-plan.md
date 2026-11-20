# sanitizer-json-escape plan — stop the Windows tail absorbing a JSON escape, and catch a host path after one

Contract: `goal-sanitizer-json-escape.md` (GDD, 2026-11-21). Baseline revision: the delivered
worktree's HEAD `2a85cb82b66303d8d93799db505268dbceb70ed0` (`2a85cb8`, PR #119's merge and retro
simplify). Every `scripts/ci/model-routing-record-sanitize.py` line number below resolves at that
revision. The source `docs/TODO.md` entry's own citations resolve at `978abba`, where they were
written; PR #119's own commits to that file shifted them, so at the delivered baseline the atoms
the entry names sit at `:124` (`_WINDOWS_TAIL`), `:129` (`_WINDOWS_TAIL_PLUS`), `:136`
(`_POSIX_BYTE`), `:147-149` (`_DRIVE_PATH`), `:156-159` (`_UNC_PATH`), `:186-188`
(`_SLASH_UNC_PATH`), `:210-213` (`_ROOT_RELATIVE_PATH`) and `:223-231` (`_FILE_URL_PATH`) -
verified by reading both revisions
(`git show 978abba:scripts/ci/model-routing-record-sanitize.py` and the delivered file).

## Deliverable shape

1. `scripts/ci/model-routing-record-sanitize.py`: two shared rules replace the per-family escape
   handling.
   - **One leading boundary for every family.** `_DRIVE_PATH`, `_UNC_PATH`, `_SLASH_UNC_PATH`,
     `_HOST_POSIX_PATH`, `_ROOT_RELATIVE_PATH`, `_FILE_URL_PATH`, `_POSIX_PATH`,
     `_COLON_POSIX_PATH`, `_ESCAPED_POSIX_PATH` and `_root_patterns`' two matchers take one
     shared lookbehind — the round-4 root rule (`(?:(?<=\\[nrtbf])|(?<=\\[u][0-9A-Fa-f]{4})|…negative…)`)
     — so a host path that starts the line after a JSON escape is a match in every family, and
     `_root_patterns` stops spelling its own copy of the same atom.
   - **One JSON-aware reading for the tails.** A new `_escape_boundary_view(text)` returns a
     one-character-per-character view of the recorded text in which the backslash of every JSON
     escape whose decoded character cannot be a path byte is replaced by `)`. `sanitize_text`
     runs every pass against that view while writing the same replacements into the recorded
     text, so a match's span still maps to the same bytes (digests keep binding the raw span) and
     no tail can consume an escape. A span that does not parse as a JSON string literal keeps the
     raw reading, and a fallback pass over the result runs the unmodified raw reading once more:
     what the JSON reading stops seeing, the raw reading still renders (fail-closed).
2. `scripts/ci/test-model-routing-record-sanitize.py`: the pins the contract names — Windows drive
   path + JSON `\n` + text with the escape and the text outside the token, the raw
   single-backslash spelling, the UNC / slash-UNC / `file:` URL variants, a POSIX host path after
   a JSON escape (caught), the CLI refusal for it, the boundary negatives, and idempotence for
   every new shape; each new pin has a mutation check recorded in the stage report.
3. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md`: the entry "Stop the Windows-family path tail
   from absorbing a JSON escape that follows the path" moves with a closure statement naming the
   branch (`fix/sanitizer-json-escape`), the decision, the pins and the #119 round-5 follow-up
   closed in the same closure; the CLOSED-TODO preamble gains the 2026-11-21 move sentence.
4. `docs/evaluations/model-routing/README.md`: only if the change makes a sentence stale. The
   three accepted false positives and the record-rendering exception are history and stay
   verbatim; the `--root-only` scope sentence gains the escape boundary it now enforces.
5. No new gate, no new file outside `docs/plans/`, no record re-rendered unless the sweep flags
   one (none expected; the two `0008b-go-bounded-regression-strong` records are #134's and are
   reported, not touched).

## Exact edge-case semantics

The reading the contract's decision asks for: **the tool's input is recorded JSON text, so inside
a JSON string literal the JSON grammar — not a validator's guess — decides which backslashes are
escapes.** Concretely:

- A **string literal** is a `"`-quoted span whose end is found by escape parity (a backslash
  consumes the next character) *and* which `json.loads` accepts. `{"out":"working directory:
  /tmp/run\nFAIL: verifier refused"}` is one; `"C:\Users\op\run"` is not (`\U` is not an escape),
  and a raw Markdown spelling therefore keeps today's reading.
- An escape is **masked** in the view when its decoded character cannot be a path byte, i.e. when
  it matches `[\s"'`,;:)]` — the exact exclusion set of every path byte class: `\n`, `\t`, `\r`,
  `\f`, `\"`, `\uXXXX` decoding to whitespace or punctuation. `\\` (a literal backslash, a
  Windows separator), `\/` (a literal slash) and `\b` (U+0008, a control byte the exclusion set
  does not name) are *not* masked, and neither is `\u002F`, so the doubled and escaped spellings
  the tool already renders keep rendering.
- `)` is the mask character: excluded by every byte class (`_WINDOWS_TAIL`, `_POSIX_BYTE`,
  `_ESCAPED_SEGMENT`, `_SPACE_CHUNK`, the separator and name classes) and by no lookbehind's
  negative set, so a masked escape ends a tail *and* is a boundary in front of the next location.
- **Digests keep binding the raw span**: the view has the same length as the text and the same
  replacement is written into both, so `path-sha256:` stays the sha256 of the recorded bytes the
  token replaced (the property `_render_match`'s docstring and the existing pins rely on). The
  trailing-backslash-before-`"` rule stays for raw spellings; in a JSON literal the escape is
  masked before the tail can reach it, so the same span falls out of the reading.
- **`--check` is unchanged**: still "the rendered text differs from the input".
- Boundary negatives that must *not* become matches: a literal name character glued in front
  (`aa/srv/store/x`, `aaC:\Work\run\x`), a longer sibling name (`/srv/store-user`), a relative
  spelling (`../srv/store/x`), a URL authority (`https://tmp/vendor/doc`, `https:\/\/tmp/...`),
  and the trailing structure of an already-rendered `<disposable-root>` token.

Fix of defect 1 (tail): `{"output":"wrote C:\\Users\\op\\run\nFAILED"}` renders the drive path's
own bytes (`path-sha256:26e7b5b4…`, the digest the entry records for the path text alone) and
leaves the two-character `\n` and `FAILED` outside the token, in the record's own JSON syntax;
same for the UNC, slash-UNC and `file:` URL spellings, and for the raw single-backslash spelling
(`C:\Users\op\run`, `\Users\op\run`) which keeps rendering whole because no literal makes it JSON.

Fix of defect 2 (boundary): `{"out":"ok\n/home/runner/work/x failed"}` is refused by
`--check --root-only` (exit 1), and renders a token for `/home/runner/work/x` with the escape
outside it; the same holds for `\t`, for a `\uXXXX` escape, for drive, UNC, slash-UNC,
root-relative and `file:` URL spellings.

## Design trade-offs

- **Rejected: a per-family escaped tail** (a second `\\{2}`-aware tail per Windows family whose
  odd runs stop the match). It doubles the pattern set, and it desynchronizes the two readings:
  a mixed spelling (`C:\\a\repo`) would be matched by the escaped tail first and leave `\repo` as
  a residue under a bounded match, with no rule saying which reading owns the span.
- **Rejected: a shape-only rule** ("stop the tail at a valid `\` + escape letter"). It cannot be
  correct on its own: a raw spelling may separate segments with exactly that shape (`\repo`,
  `\tmp`, `\new`), and the existing pins require `C:\Users\First Last\repo` to render whole. The
  discriminator is JSON string context, which is what the view computes.
- **Rejected: decoding the text before matching** (`\\`→`\`). It would delete the doubled-run
  machinery, but a decoded span no longer maps onto the recorded bytes, and the digests must keep
  binding what the token replaced.
- **Cost**: one linear scan plus one `json.loads` per candidate literal per `sanitize_text` call,
  and a second pass sequence only when the view differs from the text. Records are checked once
  per gate run; the suite's timing pins (linearity cases) stay.
- **Accepted boundary (pinned, not silent)**: in a JSON literal, an escape whose spelling *also*
  reads as a Windows separator is an escape (`\repo` is CR + `epo`), so a *mixed* spelling such as
  `C:\\a\repo` — impossible in valid JSON, but spellable by a producer bug — renders `C:\\a` and
  leaves the escape's own two characters plus `epo`. The raw reading still owns every span no
  literal covers, so a raw (unmasked) spelling never loses a match.

## Evidence plan

- Both contract reproductions, before (at `2a85cb8`) and after, with exit codes and the exact
  rendered bytes/digests.
- `python scripts/ci/test-model-routing-record-sanitize.py` on Windows and in WSL Ubuntu-24.04 as
  a non-root user (uid 1000).
- Mutation checks: each new pin must fail on the pre-fix tool bytes (scratch copy of the tester
  beside the pre-fix sanitizer), and targeted mutations (drop the escape boundary atom; drop the
  view; drop the fallback pass) must fail exactly the pins that name them.
- The sanitizer over every committed record under `docs/evaluations/` and `docs/evidence/` in both
  scopes, before and after, with the flagged list; the two `0008b-go-bounded-regression-strong`
  records are reported separately (PR #134's, not touched here).
- `python scripts/ci/check-record-cleanliness.py --base <range>` over this change's range and over
  the goal's range; `python scripts/ci/test-model-routing-evaluation.py` alone (no harness or
  benchmark run in parallel); `git diff --check`.

## Out of scope

- Other `docs/TODO.md` entries, and any sanitizer refactor the shared rule does not replace: the
  doubled-run machinery, the space-continuation rules, the `--root` matcher bodies and the
  `path-sha256:` token shape are untouched.
- `model-routing-evaluation.py`'s `redact_host_paths`: it redacts decoded measured *values*, not
  recorded JSON text, and keeps using the shared `HOST_PATH_PATTERNS` (which gain the boundary,
  and nothing else). No second JSON reading is added there.
- Re-rendering the two `0008b-go-bounded-regression-strong` records or their binding files (PR
  #134 renders them); the sweep reports their status.
