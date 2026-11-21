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

1. `scripts/ci/model-routing-record-sanitize.py`: one shared leading boundary plus a
   document-level reading replace the per-family escape handling. The fix round replaced the
   first design's mask view (`_escape_boundary_view`) and its raw fallback pass; the trade-offs
   section records why.
   - **One leading boundary for every family that takes one.** `_DRIVE_PATH`, `_UNC_PATH`,
     `_SLASH_UNC_PATH`, `_HOST_POSIX_PATH`, `_ROOT_RELATIVE_PATH`, `_FILE_URL_PATH`, `_POSIX_PATH`,
     `_ESCAPED_POSIX_PATH` and `_root_patterns`' two matchers take one shared lookbehind — the
     round-4 root rule (`(?:(?<=\\[nrtbf])|(?<=\\[u][0-9A-Fa-f]{4})|…negative…)`) — so a location
     behind a recorded escape is a match in every family, and `_root_patterns` stops spelling its
     own copy of the same atom. `_COLON_POSIX_PATH` keeps its bare `(?<=:)`: the very next
     character after a key's colon decides between an absolute path and a URL authority, which no
     negative set can do from behind, so it is the one family without the shared atom. A rendered
     `<disposable-root>` token is a boundary for the root matchers in both readings, so a root
     spelling a render left behind a token is not re-claimed and rendering rendered text is a
     no-op.
   - **The reading is decided at the document level.** A record whose whole text parses with
     `json.loads`, or whose every non-empty line does (JSONL), is read through its string
     literals: `_decode_literal` decodes each literal with the JSON grammar's own escapes and
     maps every decoded character to the raw span that spelled it (a surrogate pair covers
     twelve recorded bytes); `_render_passes` runs the matchers on the decoded text and maps
     each match back to its raw span, so a digest binds the recorded spelling and the rendered
     record stays valid JSON. The passes are the base tool's one sequence (each `--root`'s
     chunked then bounded matcher, then the host or default family); a match that overlaps an
     earlier replacement merges into one span rendered once from its own raw bytes, so a later
     matcher can never bind an earlier token's text. Text outside string literals is never
     scanned in this reading: a JSON document carries numbers, literals and punctuation there.
   - **The decoded reading stays primary inside a literal, and the Windows families close
     its fail-open side** (the fix round's r2 rule). A *raw* Windows spelling is the one
     location the decoded reading cannot see: its separators are single backslashes, which
     decode into control characters, so the decoded value of the entry's own raw shape names
     no location while the recorded bytes spell one, and the record used to render unchanged.
     `_raw_literal_spans` therefore runs `_RAW_READING.windows` (drive, UNC, slash-UNC,
     root-relative and `file:` URL - the families whose separators a record may spell with
     single backslashes) over the literal's recorded bytes, and every match the decoded
     reading reads no location in renders whole, merged with any decoded span it overlaps into
     one token (`_literal_replacements`), so no token nests in or overlaps another. Three
     boundaries keep the rule fail-closed and idempotent without re-opening the entry's own
     defect: a match whose span carries no backslash is left to the decoded reading, which
     reads those bytes as written (the two readings agree there, so a `--root` token and a
     spelling the decoded reading's name boundary deliberately spares both govern); a match
     overlapping a rendered token's own text (`path-sha256:<64 hex>`, `<disposable-root>`) is
     the tail behind a render, not a spelling; and the scan itself runs only where a literal's
     bytes can hold such a spelling at all - a single backslash, not one of an escaped pair,
     followed by `n`, `r`, `t`, `f`, `b` or `u` (`_RAW_SPELLING_ESCAPE`) - which is what keeps
     a long non-matching backslash run from being scanned a second time.
   - **Two byte-class profiles, one pattern compiler.** `_compile_patterns(classes, guard)`
     builds each reading's matcher set from a `_ByteClasses` record: the raw reading keeps the
     base tool's recorded-syntax set, and the decoded reading refuses the control characters and
     the plain space, the quote, backtick, comma, semicolon, colon and `)` — so a space-like
     character a writer escaped (the plain space, NBSP, U+3000) and an escaped apostrophe are
     path bytes, while a decoded newline, tab or other control character stays a boundary.
   - **Prose keeps the base tool's raw reading**: the same pass sequence over the recorded bytes,
     with the shared boundary's escape arm added (a location behind a recorded escape renders),
     the trailing-backslash-before-quote rule, and the disclosed over-approximations unchanged.
2. `scripts/ci/test-model-routing-record-sanitize.py` and its sibling
   `scripts/ci/test-model-routing-record-sanitize-json.py` (the split keeps every file under the
   1000-line decomposition threshold; the JSON module loads the raw module by path for the
   shared fixtures): the raw module keeps the pattern and CLI battery, and the JSON module pins
   the entry's own Windows reproduction (`path-sha256:26e7b5b4…`), every family behind an
   escape, the escaped space / NBSP / apostrophe locations, the escaped-quote parity, surrogate
   pairs, token opacity (no composite digest, no orphaned `:<hex>` fragment), the
   root-metacharacter roots, the document-level decision, the decoded-control-spelling boundary,
   the `--check --root-only` round trip, the r2 raw-spelling pins (the drive, both UNC dialects,
   root-relative and `file:` URL raw shapes render whole, the escape behind a decoded-read
   location stays outside the token, a forward-slash drive root keeps its `<disposable-root>`
   structure and a raw one renders as the family's token), and the fail-closed property corpus
   over every repro shape (twenty-seven cases after the r2 extension); each pin's discrimination
   (pre-fix bytes or a named mutation) is measured and recorded in the closure.
3. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md`: the entry "Stop the Windows-family path tail
   from absorbing a JSON escape that follows the path" moves with a closure statement naming the
   branch (`fix/sanitizer-json-escape`), the decision, the pins and the #119 round-5 follow-up
   closed in the same closure; the CLOSED-TODO preamble gains the 2026-11-21 move sentence.
4. `docs/evaluations/model-routing/README.md`: only if the change makes a sentence stale. The
   three accepted false positives and the record-rendering exception are history and stay
   verbatim; the `--root-only` scope sentence gains the escape boundary it now enforces.
5. No new gate: the split's sibling tester runs in the same `ci.yml` step and the same README
   gate list as the raw module, and no other file is added. No record is re-rendered unless the
   sweep flags one (none expected; the two `0008b-go-bounded-regression-strong` records are
   #134's and are reported, not touched).

## Exact edge-case semantics

The reading the contract's decision asks for: **the tool's input is recorded JSON text, so the
document's own JSON structure — not a validator's guess about a quoted span — decides which
reading owns the text.** Concretely:

- The reading is **document-level**: the whole text parses with `json.loads` (a single JSON
  document) or every non-empty line does (JSONL); anything else is prose. `{"out":"working
  directory: /tmp/run\nFAIL: verifier refused"}` is a JSON record; a Markdown record carrying the
  same quoted span is prose, and `"C:\Users\op\run"` spells `\U`, which no JSON escape is, so a
  document shaped that way does not parse and keeps the raw reading too.
- In a JSON record every **string literal** (keys included) is decoded with the JSON grammar's
  own escapes, and the matchers run on the decoded text; a match maps back to the raw span it
  covered, so `path-sha256:` stays the sha256 of the recorded bytes the token replaced. A
  decoded space-like character (the plain space, NBSP, U+3000) continues a location, a decoded
  apostrophe is a path byte, and the control character a `\n`, `\t`, `\r` or `\f` escape
  decodes to is a boundary, so a tail never absorbs a following escape and the line behind it.
  A literal backslash in the decoded value (a recorded doubled backslash) is a normal separator.
- **`--check` is unchanged**: still "the rendered text differs from the input".
- Boundary negatives that must *not* become matches: a literal name character glued in front
  (`aa/srv/store/x`, `aaC:\Work\run\x`), a longer sibling name (`/srv/store-user`), a relative
  spelling (`../srv/store/x`), a URL authority (`https://tmp/vendor/doc`, `https:\/\/tmp/...`),
  and the trailing structure of an already-rendered `<disposable-root>` token.

Fix of defect 1 (tail): `{"output":"wrote C:\\Users\\op\\run\nFAILED"}` renders the drive path's
own bytes (`path-sha256:26e7b5b4…`, the digest the entry records for the path text alone) and
leaves the two-character `\n` and `FAILED` outside the token, in the record's own JSON syntax;
same for the UNC, slash-UNC and `file:` URL spellings, and for the raw single-backslash spelling
(`C:\Users\op\run`, `\Users\op\run`) which keeps rendering whole because the document does not
parse as JSON.

Fix of defect 2 (boundary): `{"out":"ok\n/home/runner/work/x failed"}` is refused by
`--check --root-only` (exit 1), and renders a token for `/home/runner/work/x` with the escape
outside it; the same holds for `\t`, for the four-hex-digit `u` escape, for drive, UNC,
slash-UNC, root-relative and `file:` URL spellings.

## Design trade-offs

- **Rejected: a per-family escaped tail** (a second `\\{2}`-aware tail per Windows family whose
  odd runs stop the match). It doubles the pattern set, and it desynchronizes the two readings:
  a mixed spelling (`C:\\a\repo`) would be matched by the escaped tail first and leave `\repo` as
  a residue under a bounded match, with no rule saying which reading owns the span.
- **Rejected: a shape-only rule** ("stop the tail at a valid `\` + escape letter"). It cannot be
  correct on its own: a raw spelling may separate segments with exactly that shape (`\repo`,
  `\tmp`, `\new`), and the existing pins require `C:\Users\First Last\repo` to render whole. The
  discriminator is the document's JSON structure, which is what the document-level gate computes.
- **Replaced: the mask view and the raw fallback pass** (the first design in this plan's
  history). `_escape_boundary_view` replaced each escape's spelling with `)` in a 1:1 view and
  ran the matchers against a text that was neither the recorded nor the decoded string; a space
  or apostrophe the writer escaped was masked, so a location split there and published its tail
  (and the `--root` matchers could match the substituted byte), while the fallback pass then ran
  the raw reading over rendered text and could bind a token's own text. The decoded reading
  matches the characters a value actually carries, and overlapping matches merge into one span,
  so neither failure has a shape to occur in. The cost is paid at the literal level instead: one
  decode and one pass sequence per literal.
- **Adopted: decoding the text before matching** (`\\`→`\`), with the map the first design
  rejected: `_decode_literal` keeps every decoded character's raw span, so a match maps back to
  exactly the recorded bytes the token replaces (escapes inside the span included) and the
  digest still binds the recorded spelling. The doubled-run machinery stays for the raw reading,
  where a doubled run is a recorded spelling rather than a decoded one.
- **Cost**: one `json.loads` per document (and per line for JSONL), one decode and one pass
  sequence per string literal, and one whole-text pass sequence for prose. Records are checked
  once per gate run; the corpus sweep stays sub-second per record, and the suite's timing pins
  (linearity cases) stay. One superlinear shape survives from the previous round and is reported
  rather than hidden: a `file:` URL followed by a long backslash run inside a literal still
  grows superlinearly (about 4x per doubling; 0.55 s at 16,000 backslashes, against 5.9 s for
  the previous design at the same size and under 3 ms for the base tool). The r2 raw look does
  not change it: its scan is guarded by `_RAW_SPELLING_ESCAPE`, so a run the decoded reading
  cannot read a location in is never scanned a second time, and the residual's wall-clock at
  16,000 backslashes re-measures at the pre-r2 tool's level (1.04 s against 1.02 s, the same
  4x-per-doubling slope).
- **Rejected: rendering every raw match the decoded reading also reads** (the brief's
  single-condition rule, "no escaped backslash pair in the span"). A location whose separators
  are forward slashes and which is followed by an escape - the entry's own
  `//server/share/run\nFAILED` shape - is *one* match to the raw reading, so that rule would
  absorb the escape and the evidence line into the token and re-open the defect this entry
  closes. The discriminator has to be whether the decoded reading reads the location at all,
  which is what `_raw_literal_spans` tests against the decoded reading's own claimed spans.
- **Accepted boundaries (pinned, not silent)**: in a JSON literal, an escape whose spelling
  *also* reads as a Windows separator is an escape (`\repo` is CR + `epo`), so a *mixed*
  spelling such as `C:\\a\repo` — valid JSON, because `\r` is a grammar escape, but no correct
  producer writes a path that way — renders `C:\\a` and leaves the escape's own two characters
  plus `epo`; a *raw* single backslash spelling inside a valid literal decodes into control
  characters (`{"note":"see C:\repo\tmp"}` places a carriage return and a tab in the value), and
  the decoded reading reads no location there, so the Windows families render the recorded bytes
  whole — the shape the entry's own evidence used to leave literal (the r2 rule); a mixed
  spelling whose decoded reading *does* read a location keeps that reading's render, so a raw
  separator behind it can stay literal as a fragment (`...\repo`, `...\tmp`), which is the same
  boundary the mixed case above pins; and a raw spelling of a `--root` is rendered by the
  Windows family it belongs to, as a hashed token binding the raw bytes rather than
  `<disposable-root>` (the raw look runs the path families, not the decoded root matchers), so
  the root's bytes are removed either way. All four cases are pinned in the JSON module so a
  later narrowing is a visible decision.

## Evidence plan

- Both contract reproductions, before (at `2a85cb8`) and after, with exit codes and the exact
  rendered bytes/digests.
- `python scripts/ci/test-model-routing-record-sanitize.py` and
  `python scripts/ci/test-model-routing-record-sanitize-json.py` on Windows and in WSL
  Ubuntu-24.04 as a non-root user (uid 1000).
- Mutation checks: each pin must fail on the pre-fix tool bytes (`7a71383`) or on a named
  mutation (scratch copy of the testers beside the mutated tool: the document gate dropped, the
  decoded reading compiled with the raw byte classes, the merge/absorb rule dropped, the
  escaped-quote parity broken, the root-token guard dropped, the escape arms dropped, a
  four-hex-digit escape mis-mapped, the apostrophe or the space-like characters removed from the
  decoded byte classes), with the measured failure sets recorded in the closure.
- The sanitizer over every committed record under `docs/evaluations/` and `docs/evidence/` in both
  scopes, before and after, with the flagged list; the two `0008b-go-bounded-regression-strong`
  records are reported separately (PR #134's, not touched here).
- `python scripts/ci/check-record-cleanliness.py --base <range>` over this change's range and over
  the goal's range; `python scripts/ci/test-model-routing-evaluation.py` alone (no harness or
  benchmark run in parallel); `git diff --check`.

## Out of scope

- Other `docs/TODO.md` entries. The doubled-run machinery, the space-continuation rules, the
  `--root` matcher bodies (beyond the shared boundary and the token guard) and the
  `path-sha256:` token shape are untouched.
- `model-routing-evaluation.py`'s `redact_host_paths`: it redacts decoded measured *values*, not
  recorded JSON text, and keeps using the shared `HOST_PATH_PATTERNS` (the base reading's
  classes with the shared boundary, and nothing else). No second JSON reading is added there.
- Re-rendering the two `0008b-go-bounded-regression-strong` records or their binding files (PR
  #134 renders them); the sweep reports their status.
