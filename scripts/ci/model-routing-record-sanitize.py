#!/usr/bin/env python3
"""Render host-specific absolute paths in recorded evaluation evidence.

Published evaluation records must not carry a personal absolute path: a
disposable coordinator root under an operator's home directory, a worktree
path, or any other host-specific location. This helper renders host paths in
recorded text as a stable token instead:

* a configured disposable root becomes the literal token
  ``<disposable-root>`` with its trailing structure kept after it, so a reader
  still sees which run or control directory the path names;
* any other absolute path (a Windows drive path, a UNC path or a POSIX path)
  becomes ``path-sha256:<hex digest of the path's UTF-8 bytes>``, a stable
  hashed token that leaks no host layout and lets two occurrences of the same
  path be correlated.

Relative paths, URLs and ``sha256:`` digests pass through unchanged, and
rendering is idempotent - except that a ``file:`` URL or a doubled-slash
authority spelling is an absolute location, not a URL to spare: both modes
render it. Two non-fixpoints are disclosed rather than closed, and both bind a
rendered token's own text instead of a host byte: a separator directly in front
of a rendered token reads as a POSIX path on a second render in the default
scope, and text that already spells a token of its own (``//<disposable-root>``,
a ``path-sha256:`` spelling inside a location) can bind that token's text in
either scope. No host byte survives either render, the ``--root-only`` gate
scope is a fixpoint for records that carry no rendered token text of their own,
and `sanitize_text`'s docstring names both boundaries.

With ``--root-only`` the renderer flags only host absolute paths: Windows drive
and UNC roots, a root-relative Windows spelling (``\\Users\\op\\run``, which
classic Windows semantics resolve against the current drive root - a profile
location) and the POSIX roots a host path actually starts under (``/home/``,
``/Users/``, ``/root/``, ``/tmp/``, ``/var/``, ``/mnt/``, ``/private/`` - the
shapes the evaluation runner's host-path redaction and the committed records
name), plus every ``--root`` disposable root. A product route such as
``/portal/runtimes`` is not a host path and stays literal in this mode - in its
single-slash spelling only: spelled behind an authority (``//portal/...``) or
as a ``file:`` URL it is an absolute location like any other, and this mode
renders (and the gate refuses) it. A bare root token with no following
separator or segment (``/tmp`` quoted in prose) stays literal too: it names no
host-specific location. The default mode keeps rendering every absolute path -
including a POSIX path spelled after a key's colon and the JSON-escaped spelling
of one (``\\/etc\\/hosts``) - which is its documented boundary for argv/output
capture.

The text is read in one of two ways, decided at the document level:

* A record is **JSON-structured** when the whole file parses with
  ``json.loads`` (a single JSON document) or every non-empty line does
  (JSONL). Every string literal - a key or a value - is then decoded with the
  JSON grammar's own escapes, and every path matcher runs on the decoded text.
  A match maps back to the raw bytes it covers, every escape inside it
  included, so the digest still binds the recorded spelling the token
  replaces, and the rendered bytes stay valid JSON. A decoded space-like
  character (the plain space, NBSP, U+3000) continues a location and a
  decoded apostrophe is a path byte, because in a decoded value they are
  characters a directory name may carry, not syntax; the control characters
  an escape spells (a newline, a tab, ...) stay boundaries, so a match never
  absorbs a following escape and the evidence line behind it. A later matcher
  never touches bytes an earlier replacement wrote, so no token can bind
  another token's text. Text outside string literals is never scanned in this
  reading: in a JSON document it can only be numbers, literals and
  punctuation.
  A literal's recorded bytes are read a second time by the Windows-shaped
  families (drive, UNC, slash-UNC, root-relative, ``file:`` URL), because a
  *raw* Windows spelling is the one location the decoded reading cannot see: its
  separators are the escapes ``\\n``, ``\\t``, ``\\r``, ``\\f`` and ``\\b``,
  so ``C:\\temp`` decodes to ``C:``, a tab and ``emp``: the decoded value names
  no location while the recorded bytes spell one. The decoded reading's spans
  (mapped back to the literal's recorded bytes) and those raw matches are
  unioned: every match contributes the parts of its span that no claim and no
  rendered token's own text covers, overlapping or touching spans become one
  token, and no raw match is discarded because a decoded span overlaps it. A
  match whose span carries no escaped backslash pair - ``\\\\``, the spelling
  the decoded reading itself uses for a literal separator - is a raw spelling
  through all of its parts, so a forward-slash prefix continued by raw
  separators renders whole, and the escape and text behind a forward-slash
  spelling are absorbed with it (over-redaction, the fail-closed direction: the
  prose reading's own over-approximation). A match that carries a pair is a
  producer mix of the decoded reading's own spelling and a raw one; of its
  uncovered parts only those carrying a backslash beyond their opening escape
  character are raw spellings (``...\\\\a\\repo`` renders whole), while a part
  that is one escape character and the text behind it stays outside the token
  exactly as the decoded reading spells it - ``C:\\\\Users\\\\op\\\\run`` + the
  newline escape + ``FAILED`` keeps the escape and ``FAILED`` outside, and the
  pinned mixed spelling ``C:\\\\a\\repo`` keeps ``\\repo`` outside. A raw match
  whose span carries no backslash at all is left to the decoded reading, which
  reads those bytes as written; and every replacement is snapped to the
  backslash runs its span splits, so the rendered bytes stay valid JSON. The
  families also read the decoded reading's own rebuild, anchored at each claim's
  end, because that rebuild is the text the next render will read: a ``--root``
  token's own end exposes a spelling the byte it replaced hid, so the first
  render absorbs it rather than leaving a residue the next render would bind
  with the token's own text (``_raw_literal_spans``' anchored paragraph). POSIX
  spellings are not re-read this way: their separators carry no backslash, so
  the decoded reading already reads them as written.
* Anything else is **prose**, and prose keeps the base tool's raw reading,
  plus the shared leading boundary's escape arm: a matcher sees the recorded
  bytes exactly as written, so a raw Windows spelling whose separators read
  like escapes (``\\run`` starts with ``\\r``) still renders whole, and a
  recorded escape in front of a location is a boundary rather than a name
  character. Prose keeps the base tool's fail-closed over-approximation: a
  space-continued chunk (`C:\\Users\\op\\my project`, `.../x today`) renders
  whole, because a partial render would leave path bytes behind, and a quoted
  raw spelling whose escapes decode into control characters keeps the raw
  reading.

Usage (record publishers apply this before writing a record)::

    python scripts/ci/model-routing-record-sanitize.py \
        --root "$DISPOSABLE_ROOT" < recorded-output.txt

The CLI renders files (or standard input) to standard output. With ``--check``
it writes nothing and exits 1 when the input would change, so a publishing
script can gate on "no host path is present"; ``--check --root-only`` is the
shape the committed-record cleanliness gate uses.
"""

from __future__ import annotations

import argparse
import bisect
import hashlib
import json
import pathlib
import re
import sys
from typing import NamedTuple

DISPOSABLE_ROOT_TOKEN = "<disposable-root>"


def leading_boundary(excluded: str) -> str:
    """The leading lookbehind every path family that takes one carries: what may sit in front.

    A JSON escape in front of a location is a boundary, not a name: the letter of a
    two-character escape (``\\n``, ``\\t``, ``\\r``, ``\\b``, ``\\f``) or the last hex
    digit of a ``\\uXXXX`` escape ends the escape sequence, so a path that starts the
    next line of a recorded record - or follows an escape the raw spelling would read
    as path text - still matches, exactly as the ``--root`` matchers treat it. Every
    other character the family refuses is spelled by its own negative set: a name
    character glued in front (``aa/srv/store/x``), a separator, a colon or an
    already-rendered token's ``>`` keeps relative paths, URLs and token tails out.
    ``_COLON_POSIX_PATH`` is the one family that does not take it: the character after
    a key's colon decides between an absolute path and a URL authority, so that pattern
    keeps its own bare ``(?<=:)``.
    """

    return r"(?:(?<=\\[nrtbf])|(?<=\\[u][0-9A-Fa-f]{4})|(?<![" + excluded + r"]))"


class _ByteClasses(NamedTuple):
    """The syntax characters one reading's byte classes refuse.

    ``tail`` is the body of every path tail's byte class, ``posix`` the body of a
    POSIX segment byte's class, and ``escaped`` the body of the escaped-spelling
    segment class. The raw reading spells the recorded syntax: whitespace and the
    quote, backtick, comma, semicolon, colon and ``)`` that separate a location from
    JSON, Markdown, shell or prose text. The decoded reading spells a decoded string
    value's syntax instead: the control characters and the plain space (a decoded
    newline, tab or space is a boundary), the quote, backtick, comma, semicolon,
    colon and ``)`` - while an apostrophe and the non-control space-like characters
    a JSON writer escapes (NBSP, U+3000) are path bytes, because a decoded value
    carries them in directory names, not as syntax.
    """

    tail: str
    posix: str
    escaped: str


_RAW_SYNTAX = "\\s\"'`,;:)"
_DECODED_SYNTAX = "\\x00-\\x20\\x7f\"`,;:)"

_RAW_BYTE_CLASSES = _ByteClasses(
    tail=_RAW_SYNTAX,
    posix="/" + _RAW_SYNTAX + "\\\\",
    escaped="\\\\/" + _RAW_SYNTAX,
)
_DECODED_BYTE_CLASSES = _ByteClasses(
    tail=_DECODED_SYNTAX,
    posix="/" + _DECODED_SYNTAX + "\\\\",
    escaped="\\\\/" + _DECODED_SYNTAX,
)

# The boundary that keeps a space-continued path chunk inside one location: the
# chunk must reach another separator (`\`, any slash spelling or `\/`), close on a
# quote, or end the line. A chunk bounded only by end-of-line (or a quote) is the
# documented over-approximation: `C:\Users\op\my project` renders whole, and so
# does the prose word in `C:\Users\op\x today`, because the two are the same shape
# and a partial render of a space-containing segment would leave path bytes in the
# record. A word followed by another word is not a continuation, so
# `C:\Users\x and the run continues` still ends at `x`.
_SPACE_END = r"(?=[\\/]|[\"'`]|\r|\n|\Z)"
# One separator of a location whose separators may be mixed: a Windows UNC accepts
# both slash directions, and a recorded JSON escape doubles each backslash run at
# every nesting level, so a backslash separator is any positive run and a forward
# slash may be escaped as `\/`.
_SEPARATOR = r"(?:\\|(?:\\{2})+|/|\\/)"
# A forward slash, or its JSON-escaped spelling (`\/`): a record quoted into a JSON
# string carries both forms in the raw reading, and the checker must not miss a host
# path because the writer escaped it.
_HOST_SEP = r"(?:/|\\/)"
# One separator run of a disposable root as recorded text spells it: any nonempty
# mix of backslashes (raw, or the JSON-escaped run at any nesting depth) and
# forward slashes or their `\/` escape - the units concatenate, so every recorded
# spelling is this one character class. `_root_patterns` joins the root's segments
# with it, so both compiled matchers accept every recorded spelling, and a single
# class also keeps a long non-matching separator run linear: an overlapping
# alternation written as a repetition (`(?:\\+|/|\\/)+`) parses one run of N
# backslashes 2**(N-1) ways and turns a failed match exponential.
_ROOT_SEPARATOR = r"[\\/]+"
# The name characters a recorded path token continues with. A root boundary is a
# name boundary: a root glued to a name on either side (`.../run-user`,
# `.../run.bak`) names a different location, while punctuation (`.../run.`,
# `.../run!`) leaves the root the whole name.
_ROOT_NAME_CHAR = r"[A-Za-z0-9_-]"
# The scheme a `file:` URL carries is case-insensitive - `FILE:`, `File:` and
# `file:` are the same scheme - so a record spelling it any other way must not
# pass the checker unchanged.
_FILE_SCHEME = r"(?i:file):"
# The prefixes a Windows location may carry: a `file:` URL scheme (with any run of
# separators after it, since concatenated escapes are still the scheme's own
# separators) and a device namespace (`\\?\` or the `\\.\` device form, whose
# backslashes a recorded JSON escape doubles). The lookbehinds accept a preceding
# `:` so a location spelled after a key (`root:C:\x`, `root:/home/x`) is still a
# location, while a preceding letter, digit, underscore, separator or rendered
# token keeps relative paths and URLs out.
_FILE_PREFIX = r"(?:" + _FILE_SCHEME + r"[\\/]*)?"
_DEVICE_PREFIX = r"(?:\\{1,4}[?.]\\{1,4})?"
# The filesystem roots a host absolute path actually starts with. `/portal/runtimes`
# and any other product route stay literal in `--root-only` mode because their first
# segment is not one of these roots, and a bare root token with no following separator
# or segment (`/tmp` quoted in prose) stays literal too: it names no host-specific
# location. `HOST_PATH_PATTERNS` is the one shared definition: the evaluation runner's
# measured-value redaction imports it, so both tools agree on what a host path is.
HOST_ROOT_SEGMENTS = ("home", "Users", "root", "tmp", "var", "mnt", "private")
_HOST_ROOT = "(?:" + "|".join(HOST_ROOT_SEGMENTS) + ")"


class _Patterns(NamedTuple):
    """One reading's compiled matchers and the class strings it spells them with."""

    drive: re.Pattern[str]
    unc: re.Pattern[str]
    slash_unc: re.Pattern[str]
    posix: re.Pattern[str]
    colon_posix: re.Pattern[str]
    escaped_posix: re.Pattern[str]
    host_posix: re.Pattern[str]
    root_relative: re.Pattern[str]
    file_url: re.Pattern[str]
    # The families whose spelling a record may carry with single backslashes as its
    # separators: the raw look inside a JSON string literal runs exactly these over the
    # literal's recorded bytes (a POSIX location's separators are forward slashes, which
    # the decoded reading reads as written).
    windows: tuple[re.Pattern[str], ...]
    space_chunk: str
    # The negative lookbehind a `--root` matcher carries (the decoded reading keeps a
    # rendered token's own text from re-claiming the root behind it).
    root_guard: str
    # The root-only scope: every host shape, and nothing that could be a product
    # route. The default scope adds every absolute path the POSIX pattern cannot see,
    # including a host-rooted one spelled after a colon, the JSON-escaped spelling of
    # a POSIX path, and a `file:` URL.
    host: tuple[re.Pattern[str], ...]
    default: tuple[re.Pattern[str], ...]


def _compile_patterns(classes: _ByteClasses, root_guard: str) -> _Patterns:
    """Compile one reading's pattern set from its byte classes.

    The pattern text is shared: only the characters the byte classes refuse differ
    between the raw reading (the recorded text's own syntax) and the decoded reading
    (a JSON string value's characters). Keeping one compiler means the two readings
    can never drift into a family the other does not carry.
    """

    tail = classes.tail
    no_backslash = tail + r"\\"
    no_separator = tail + r"\\/"
    # A Windows path tail: path bytes, or a space-continued chunk. The byte class
    # stops at the reading's syntax characters, so a quoted or comma-separated path
    # renders cleanly, and a single backslash stays a path byte: in raw recorded text
    # it IS the separator, and the JSON-escaped spelling doubles it, so excluding it
    # would stop every raw Windows location at its first inner separator.
    space_chunk = r" [^" + tail + r"]+" + _SPACE_END
    windows_tail_unit = r"(?:[^" + tail + r"]|" + space_chunk + r")"
    windows_tail = windows_tail_unit + r"*"
    # The same tail where the shape requires a real byte after the separator (a UNC
    # location whose second segment is empty, `\\server\`, is not a path: an empty tail
    # would read the two-character prefix of an escaped source literal like `\\n\` as
    # one, the false positive the corpus sweep caught in an embedded code sample).
    windows_tail_plus = windows_tail_unit + r"+"
    # One byte of a POSIX path segment: any byte except a separator, the reading's
    # syntax characters - or a JSON-escaped forward slash, which in the raw reading is
    # the escaped spelling of the segment separator. A bare backslash is deliberately
    # NOT a segment byte: a POSIX location never contains one. A raw spelling that puts
    # one there ends the location, and a JSON record's decoded segment carries none.
    posix_byte = r"(?:[^" + classes.posix + r"]|\\/)"
    posix_run = r"(?:" + posix_byte + r")+"
    # The POSIX spelling of the same space continuation: the chunk after the space
    # must reach a separator or end the line (`/home/first last/repo` and
    # `/home/op/my project` are one location each).
    posix_segment = r"(?:" + posix_run + r"(?: " + posix_run + _SPACE_END + r")*)"
    # A segment of the escaped raw spelling (`\/etc\/hosts`), whose escaped
    # separator pairs sit outside the segment bytes.
    escaped_segment = r"(?:[^" + classes.escaped + r"]+(?: [^" + classes.escaped + r"]+" + _SPACE_END + r")*)"
    drive = re.compile(
        leading_boundary(r"A-Za-z0-9_/\\>") + _FILE_PREFIX + _DEVICE_PREFIX
        + r"[A-Za-z]:[\\/]" + windows_tail
    )
    # A UNC path whose separators are backslashes, each written once or escaped by JSON
    # quoting at any nesting level (an even run of two or more): `\\server\share` and
    # `{"p":"\\\\server\\share"}` are the same location in the raw reading, and so is
    # the doubly escaped spelling of either separator. The inner separators (and the
    # tail after them) may also be forward slashes or their escapes, because Windows
    # reads the mixed spellings as the same location.
    unc = re.compile(
        leading_boundary(r"A-Za-z0-9_/\\>") + _FILE_PREFIX + _DEVICE_PREFIX + r"(?:\\{2})+"
        + r"[^" + no_backslash + r"]+" + _SEPARATOR + windows_tail_plus
    )
    # The lookbehind keeps URLs (`https://`) and relative paths out, and `:` too: a
    # location spelled after a colon would otherwise swallow a URL authority
    # (`https://x` must never start a match), and the colon-adjacent spelling has its
    # own pattern below.
    posix = re.compile(
        leading_boundary(r"A-Za-z0-9_:/\\>.") + r"/(?:" + posix_segment + r"/)*" + posix_segment
    )
    # A POSIX path spelled directly after a colon (`PATH=/usr/bin:/usr/local/bin`): the
    # colon allowance the pattern above must not take, because the very next character
    # decides between an absolute path and a URL authority - a URL's colon is followed
    # by two separators, and this pattern requires exactly one.
    colon_posix = re.compile(r"(?<=:)/(?!/)(?:" + posix_segment + r"/)*" + posix_segment)
    # A POSIX path a JSON writer escaped (`\/etc\/hosts`): the escaped spelling of an
    # absolute location in the raw reading. The lookbehind keeps URL authorities
    # (`https:\/\/tmp` - the pair after the colon, and the pair after a separator) and
    # relative paths out, and every separator must be the escaped pairing, so an
    # unescaped `/` cannot drift this pattern into a URL. A rendered root token is one
    # more boundary: the root pass leaves an escaped descendant separator behind it
    # (`C:\Work\run\/child` renders as `<disposable-root>\/child`), and that separator
    # is the token's trailing structure, not the start of a new escaped location.
    escaped_posix = re.compile(
        r"(?<!" + re.escape(DISPOSABLE_ROOT_TOKEN) + r")" + leading_boundary(r"A-Za-z0-9_:\\/")
        + r"(?:\\/)(?:" + escaped_segment + r"\\/)*" + escaped_segment
    )
    # A forward-slash authority path (`//server/share`), with each separator optionally
    # escaped or a backslash (the mixed spellings are the same UNC location). The
    # lookbehind keeps URLs (`https://`) out: their `//` follows a colon.
    slash_unc = re.compile(
        leading_boundary(r"A-Za-z0-9_:\\/") + r"(?:/|\\/)(?:/|\\/)[^" + no_separator + r"]+"
        + _SEPARATOR + windows_tail_plus
    )
    # The segment class allows an empty segment, so a naive concatenation (`/tmp//run`)
    # is still a host path; a preceding `:` is accepted for the same reason as above,
    # while a preceding separator is not: that spelling is an authority path or a URL,
    # handled by their own patterns, and it keeps `https://tmp/vendor/doc` literal.
    host_posix = re.compile(
        leading_boundary(r"A-Za-z0-9_>./\\") + r"(?:" + _HOST_SEP + r")" + _HOST_ROOT
        + r"(?:(?:" + _HOST_SEP + r")(?:" + posix_segment + r")?)+"
    )
    # A root-relative Windows spelling (`\Users\op\run`): classic Windows semantics
    # resolve a leading single backslash against the current drive root, so it names
    # the same profile location as `C:\Users\op\run`. The leader is an odd backslash run
    # (the raw and Markdown-prose spelling); the even runs are the JSON-escaped
    # spellings the UNC patterns already cover in the raw reading.
    root_relative = re.compile(
        leading_boundary(r"A-Za-z0-9_>./\\") + r"(?:\\{2})*\\" + _HOST_ROOT
        + r"(?:\\[^" + no_backslash + r"]*|" + space_chunk + r")+"
    )
    # A `file:` URL names an absolute location; without this, the drive or POSIX
    # pattern's lookbehind (the URL's own slashes) would hide a host path inside one.
    # After `file:` a Windows location may be spelled with backslashes (a single one
    # before an authority is a legitimate file-URL form), so this pattern accepts them
    # where the bare drive/UNC leaders require two; the scheme is followed by any run
    # of separators (concatenated escapes included), and the host-root arm does not
    # require a segment after the root (`file:///tmp` names an absolute location too).
    # An authority followed by a drive location (`file://localhost/C:/x`) is one
    # location: the authority's own separator is not where the path ends.
    file_url = re.compile(
        leading_boundary(r"A-Za-z0-9_") + _FILE_SCHEME + r"[\\/]*(?:"
        + _DEVICE_PREFIX + r"[A-Za-z]:[\\/]" + windows_tail
        + r"|" + _DEVICE_PREFIX + r"\\{1,4}[^" + no_backslash + r"]+(?:\\|\\\\)[^" + tail + r"]+"
        + windows_tail
        + r"|" + _HOST_SEP + _HOST_ROOT + r"(?:(?:" + _HOST_SEP + r")(?:" + posix_segment + r")?)*"
        + r"|(?://|\\/\\/)[^" + no_separator + r"]+(?:[\\/]|\\/)(?:[A-Za-z]:[\\/])?" + windows_tail
        + r")"
    )
    # The host-rooted POSIX pattern runs before the general POSIX one: it carries the
    # same space continuation, but a spelling only it recognises (a host root directly
    # after the separator) must not be consumed piecewise by the general pattern first.
    return _Patterns(
        drive=drive, unc=unc, slash_unc=slash_unc, posix=posix, colon_posix=colon_posix,
        escaped_posix=escaped_posix, host_posix=host_posix, root_relative=root_relative,
        file_url=file_url,
        windows=(drive, unc, slash_unc, root_relative, file_url), space_chunk=space_chunk,
        root_guard=root_guard,
        host=(drive, unc, slash_unc, host_posix, root_relative, file_url),
        default=(drive, unc, slash_unc, host_posix, posix, file_url, root_relative,
                 escaped_posix, colon_posix),
    )


# The raw reading: the recorded text's own bytes, with the base tool's byte classes.
# `HOST_PATH_PATTERNS` is exported for the evaluation runner's measured-value
# redaction, which reads decoded values and keeps the base reading's classes.
# A rendered token's own text is a boundary for the `--root` matchers in both
# readings: a root spelling a render left behind a token is not re-claimed on the
# next render, so rendering rendered text is a no-op.
_ROOT_TOKEN_GUARD = r"(?<!" + re.escape(DISPOSABLE_ROOT_TOKEN) + r")"
_RAW_READING = _compile_patterns(_RAW_BYTE_CLASSES, _ROOT_TOKEN_GUARD)
# The decoded reading: one JSON string literal's decoded content, whose characters are
# a value's characters rather than recorded syntax.
_DECODED_READING = _compile_patterns(_DECODED_BYTE_CLASSES, _ROOT_TOKEN_GUARD)
HOST_PATH_PATTERNS = _RAW_READING.host
DEFAULT_PATH_PATTERNS = _RAW_READING.default


HASHED_PATH_TOKEN_PREFIX = "path-sha256:"
# A recorded literal's bytes can hold a raw Windows spelling only where a backslash run
# is odd - the grammar pairs a run's backslashes left to right, so its last backslash is
# then a two-character escape's own backslash - and that backslash is followed by one of
# the escapes that hides a location from the decoded reading: `\n`, `\r`, `\t`, `\f` and
# `\b` decode into a control character, and a `\u` escape's character can break a
# location just as well. Every other backslash the JSON grammar spells is `\\`, `\/` or
# `\"`, which the decoded reading reads as a path byte, a separator or a boundary, so a
# literal carrying none of those escapes carries no raw spelling to look for. The guard
# is also what keeps a long non-matching backslash run - `file:` plus thousands of
# separators - from being scanned a second time: the UNC family's doubled-run leader is
# superlinear over one run, and the recorded bytes are the longer of the two texts. The
# guard is per literal, so one escape-shaped backslash anywhere in a literal lets the
# scan run over the whole of it.
_RAW_SPELLING_ESCAPE = re.compile(r"(?<!\\)(?:\\\\)*\\[nrtbfu]")
# A rendered token's own text: the `--root` token, or the hashed spelling above. The
# raw look never renders a part that overlaps one - binding a token's own text would
# write a digest over it, the `path-sha256:<A>:<B>` corruption the decoded reading's
# inert token alphabet prevents for free. The bytes beside a token in the same raw
# match are still raw spellings; they render as their own parts.
_TOKEN_SPELLING = re.compile(
    re.escape(DISPOSABLE_ROOT_TOKEN) + r"|" + re.escape(HASHED_PATH_TOKEN_PREFIX) + r"[0-9a-f]{64}"
)


def hashed_path_token(path: str) -> str:
    """Return the stable hashed token for one absolute path."""

    return HASHED_PATH_TOKEN_PREFIX + hashlib.sha256(path.encode("utf-8")).hexdigest()


def _render_record_span(raw: str, start: int, end: int) -> str:
    """Render one matched span of the recorded text, leaving a JSON ``\\"`` escape outside it.

    In JSON-escaped raw text a matched path immediately before a recorded quote is
    spelled ``...\\"``: its final backslash escapes the quote, not a path
    separator, so consuming it would leave a bare quote and corrupt the
    record's JSON syntax. An odd trailing run of backslashes ends in such an
    escape: the run's pairs are literal backslashes, and its final backslash
    stays outside the token, which still binds the raw path text minus the
    backslash the rule treats as the escape.

    The rule is applied by shape, without JSON state, so it also governs
    literal text that spells a host path with raw backslashes before a quote
    (a Markdown code span, say): the token binds the path minus the backslash
    the rule treats as the escape - the trailing separator, for a
    single-backslash run - and that backslash survives outside it, keeping the
    render idempotent. In the decoded reading the rule cannot fire: a match ends
    at a decoded-character boundary, where the raw span of a JSON escape is
    all-or-nothing, so a span never ends inside the ``\\"`` pair and its
    trailing backslash run is always even.
    """

    matched = raw[start:end]
    if raw[end:end + 1] == '"':
        trailing = len(matched) - len(matched.rstrip("\\"))
        if trailing % 2 == 1:
            return hashed_path_token(matched[:-1]) + "\\"
    return hashed_path_token(matched)


def _root_patterns(root: str, reading: _Patterns) -> tuple[re.Pattern[str], re.Pattern[str]] | None:
    """Compile the bounded and chunked matchers for one disposable root.

    The bounded matcher consumes the root only when it is spelled with a recorded
    separator mixture (`_ROOT_SEPARATOR`) and only at a whole-name boundary: the
    leading lookbehind refuses a name character glued in front - except after a
    JSON escape sequence, whose final character (the letter of ``\\n``, ``\\t``,
    ``\\r``, ``\\b`` or ``\\f``, or the last hex digit of a ``\\uXXXX`` escape)
    ends the escape and is a boundary - and the trailing lookahead refuses a name
    continuation behind it - a further name character, a ``.``-continued name,
    or a space-continued path chunk (`...-user`,
    ``.../run.bak``, ``.../run user/private``, the same continuation the path
    tails absorb). The chunked matcher is its fail-closed sibling: the same root
    followed by one or more space-continued chunks is a longer sibling location,
    which the bounded matcher declines and the chunked one renders whole as a
    hashed token, because leaving the exact root bytes literal on a POSIX root
    outside the host list would fail open under ``--root-only``. A root that
    leads with a separator keeps that run in the match, spelled only with the
    separator kind the root itself carries, so a UNC root never claims the POSIX
    spelling of its segments and the reverse; a root without a leading separator
    matches only after a non-separator boundary. ``None`` means the root names no
    segment (a bare ``/``).
    """

    stripped = root.rstrip("\\/")
    segments = [segment for segment in re.split(r"[\\/]+", stripped) if segment]
    if not segments:
        return None
    body = _ROOT_SEPARATOR.join(re.escape(segment) for segment in segments)
    if re.match(r"[\\/]", stripped):
        # The root's own leading separator kind gates the match: a backslash-led
        # root (a UNC or device spelling) matches only a backslash run, and a
        # slash-led root only the forward-slash spellings, so the two never claim
        # each other's locations.
        body = (r"\\+" if stripped.startswith("\\") else r"(?:/|\\/)+") + body
    # A separator in front of the root is not a boundary. For a separator-less
    # root (a drive path) that spelling continues a longer location (a device
    # namespace's `\\?\C:`, say), not this root; for a separator-led root the
    # match must start at the first separator of its run - a start inside the
    # run would re-try the body at every position of a long run with no match
    # behind it (quadratic over `"/" * 100000`). `leading_boundary` is the same
    # lookbehind every other family carries, so a JSON escape in front of the root
    # is a boundary, not a name: a root that starts the next recorded line inside a
    # quoted record (`{"out":"ok\n/srv/store/x"}`) still matches, while a literal
    # name character glued in front (`aa/srv/store/x`) still rejects the match.
    lookbehind = reading.root_guard + leading_boundary(r"A-Za-z0-9_.\-\\/")
    after = r"(?!" + _ROOT_NAME_CHAR + r"|\." + _ROOT_NAME_CHAR + r"|" + reading.space_chunk + r")"
    return (
        re.compile(lookbehind + body + after),
        re.compile(lookbehind + body + r"(?:" + reading.space_chunk + r")+"),
    )


def _string_literal_end(text: str, start: int) -> int:
    """The offset after the ``"`` that closes the literal opening at ``start``, or -1.

    The closing quote is the first one no backslash run escapes - the odd/even
    pairing the JSON grammar itself uses - so a ``\\"`` inside the literal does not
    end it, and an unterminated literal reports -1 instead of swallowing the rest of
    the text.
    """

    index = start + 1
    while index < len(text):
        if text[index] == "\\":
            index += 2
            continue
        if text[index] == '"':
            return index + 1
        index += 1
    return -1


def _decode_literal(content: str) -> tuple[str, list[tuple[int, int]]]:
    """Decode one JSON string literal's content and map every decoded character.

    ``content`` is the literal's raw spelling between its quotes, which the
    document-level ``json.loads`` gate has already accepted, so every escape is the
    JSON grammar's own. The returned spans are ``(start, end)`` offsets into
    ``content``: one character for a plain byte, two for a single-letter escape and
    six for ``\\uXXXX``. A surrogate pair stays its two units, each covering its own
    six recorded bytes: the character the pair spells (above U+FFFF) and each lone
    unit are characters no byte class refuses and no pattern atom reads, so the
    pairing ``json.loads`` performs would change no match, no raw mapping and no
    token, and the map carries the spelling the literal writes.
    """

    characters: list[str] = []
    spans: list[tuple[int, int]] = []
    index = 0
    while index < len(content):
        character = content[index]
        if character != "\\":
            characters.append(character)
            spans.append((index, index + 1))
            index += 1
            continue
        letter = content[index + 1]
        if letter != "u":
            characters.append(_LITERAL_ESCAPES[letter])
            spans.append((index, index + 2))
            index += 2
            continue
        characters.append(chr(int(content[index + 2:index + 6], 16)))
        spans.append((index, index + 6))
        index += 6
    return "".join(characters), spans


# The single-letter escapes the JSON grammar spells, and the character each decodes
# to. ``\\`` and ``\/`` decode to the separators themselves and ``\b`` to the control
# byte U+0008; the rest decode to the control characters the decoded reading's classes
# refuse, so a match ends in front of them instead of taking the escape and the text
# behind it into the token.
_LITERAL_ESCAPES = {'"': '"', "\\": "\\", "/": "/", "b": "\b", "f": "\f",
                    "n": "\n", "r": "\r", "t": "\t"}


def _render_passes(
    record: str, cells: list[tuple[int, int]] | None,
    passes: list[tuple[re.Pattern[str], bool]], raw: str,
) -> list[tuple[int, int, str]]:
    """Run one reading's pass sequence over ``record``, mapping every match to raw bytes.

    ``record`` is the text the matchers run on - the recorded text itself for the raw
    reading, a literal's decoded content for the decoded reading - and the mapping back
    to raw offsets is exact, so a match's span selects exactly the recorded bytes its
    token replaces and a digest still binds the recorded spelling, escapes included.
    ``cells`` spells the mapping per character for the decoded reading (a decoded
    character's raw width depends on the escape that spelled it); the raw reading
    passes ``None``, because there every character is its own byte until a
    replacement claims one.

    One pass is one rebuild. The pass's matches run on the text the earlier passes
    wrote, union with the spans those passes' replacements claim (a match that
    overlaps a claim merges with it into one span, so a later matcher can never bind
    an earlier token's text and no render leaves an orphaned ``:<hex>`` fragment of
    one token inside or behind another), and `_rebuild_with_groups` then writes the
    union back in one join of slices. Rendering k matches over a text of n characters
    is therefore O(n + k) per pass, where the per-match rebuild was O(k*n);
    `_rebuild_with_groups` is that one rebuild, and the wide-record pin counts its
    calls. The passes run in the order the caller gives - the root matchers first,
    then the host or default path family, the base tool's one pass sequence.
    """

    current = record
    mapped = list(cells) if cells is not None else None
    # Each claim is ``(current start, current end, raw start, raw end, text)``: one
    # replacement-written span in the current text's coordinates, the raw span it
    # binds, and the text it wrote.
    claims: list[tuple[int, int, int, int, str]] = []
    for pattern, is_root in passes:
        spans = sorted(match.span() for match in pattern.finditer(current))
        if not spans:
            continue
        current, mapped, claims = _rebuild_with_groups(
            current, mapped, claims, _pass_groups(claims, spans), raw, is_root,
        )
    return [(raw_start, raw_end, text) for _start, _end, raw_start, raw_end, text in claims]


def _pass_groups(
    claims: list[tuple[int, int, int, int, str]], spans: list[tuple[int, int]]
) -> list[list]:
    """Union one pass's match spans with the claim spans the earlier passes wrote.

    Returns ``(start, end, matched, absorbed)`` groups, sorted by start and pairwise
    disjoint: ``matched`` is true when the group carries at least one of this pass's
    matches and must be rendered now, and ``absorbed`` lists the indexes of the
    claims inside the group. Overlap only - a claim no match strictly overlaps is its
    own group and keeps its text. This union is the per-match growth the first
    version ran, done once per pass, so a match can still never bind a replacement's
    own text.
    """

    events = [(claim[0], claim[1], False, index) for index, claim in enumerate(claims)]
    events += [(start, end, True, -1) for start, end in spans]
    events.sort()
    groups: list[list] = []
    for start, end, matched, index in events:
        if groups and start < groups[-1][1]:
            groups[-1][1] = max(groups[-1][1], end)
            groups[-1][2] = groups[-1][2] or matched
            if not matched:
                groups[-1][3].append(index)
        else:
            groups.append([start, end, matched, [] if matched else [index]])
    return groups


def _rebuild_with_groups(
    current: str, mapped: list[tuple[int, int]] | None,
    claims: list[tuple[int, int, int, int, str]], groups: list[list],
    raw: str, is_root: bool,
) -> tuple[str, list[tuple[int, int]] | None, list[tuple[int, int, int, int, str]]]:
    """Write one pass's groups into ``current`` and its mapping, in one rebuild.

    ``groups`` are `_pass_groups`' union: a group carrying a match renders now - one
    token from its own raw bytes, or the root matcher's token when this is a root
    pass and no earlier claim merged with it - while a group that is a single
    untouched claim keeps that claim's text. Every group contributes at most one
    slice and the text is joined once, so the cost is the text's own length and the
    claims', never one rebuild per match. The raw offsets of an unclaimed span
    follow the claims before it: a claim's text stands for its own raw span and need
    not be as long as it (a digest is longer than the path it replaces), so
    ``bisect`` over the claims replaces the per-match scan the first version ran.
    """

    starts = [claim[0] for claim in claims]
    ends = [claim[1] for claim in claims]
    # The cumulative difference between each claim's text length and its raw span's,
    # so an unclaimed position maps back to raw bytes with the claim count in front
    # of it. Claims are written in order and pairwise disjoint, so both `starts` and
    # `ends` ascend, and the claims in front of a position are exactly the claims up
    # to the last one whose start is behind it - the `position` the lookup already
    # computes. That is why the shift is indexed by `position + 1` and not by a
    # second bisect over `ends`: a claim ending exactly at the position still stands
    # for its own raw span and must count, where a `bisect_left(ends, index)`
    # respelling drops it and moves the span forward by that claim's own shift. The
    # `--root` case where a slash-UNC location starts exactly where a rendered root
    # token ends pins the difference: the respelling leaves the authority literal and
    # binds the location's tail (the raw module's
    # `test_a_location_at_a_root_tokens_end_binds_its_own_bytes`). The end side obeys
    # the same rule - a group whose last character is a claim's end counts that claim
    # too - and the respelling there moves the raw end past the group, over-binding
    # the bytes behind it (the raw module's
    # `test_a_group_ending_at_a_claims_end_binds_its_whole_raw_span` pins that side).
    shifts = [0]
    for _start, _end, claim_start, claim_end, text in claims:
        shifts.append(shifts[-1] + len(text) - (claim_end - claim_start))

    def raw_start_of(index: int) -> int:
        if mapped is not None:
            return mapped[index][0]
        position = bisect.bisect_right(starts, index) - 1
        if position >= 0 and ends[position] > index:
            return claims[position][2]
        # No claim covers `index`; the last one behind it ends at or before it, so the
        # claims in front are `0 .. position` and their shifts are `shifts[position+1]`.
        return index - shifts[position + 1]

    def raw_end_of(end: int) -> int:
        if mapped is not None:
            return mapped[end - 1][1]
        index = end - 1
        position = bisect.bisect_right(starts, index) - 1
        if position >= 0 and ends[position] > index:
            return claims[position][3]
        return end - shifts[position + 1]

    pieces: list[str] = []
    piece_mapped: list[tuple[int, int]] | None = [] if mapped is not None else None
    new_claims: list[tuple[int, int, int, int, str]] = []
    position = 0
    written = 0
    for start, end, matched, absorbed in groups:
        pieces.append(current[position:start])
        if piece_mapped is not None:
            piece_mapped.extend(mapped[position:start])
        written += start - position
        if matched:
            raw_start = raw_start_of(start)
            raw_end = raw_end_of(end)
            text = (DISPOSABLE_ROOT_TOKEN if is_root and not absorbed
                    else _render_record_span(raw, raw_start, raw_end))
            if piece_mapped is not None:
                piece_mapped.extend([(raw_start, raw_end)] * len(text))
        else:
            claim = claims[absorbed[0]]
            raw_start, raw_end = claim[2], claim[3]
            text = current[start:end]
            if piece_mapped is not None:
                piece_mapped.extend(mapped[start:end])
        new_claims.append((written, written + len(text), raw_start, raw_end, text))
        pieces.append(text)
        written += len(text)
        position = end
    pieces.append(current[position:])
    if piece_mapped is not None:
        piece_mapped.extend(mapped[position:])
    return "".join(pieces), piece_mapped, new_claims


def _apply_replacements(raw: str, replacements: list[tuple[int, int, str]]) -> str:
    """Write each replacement into ``raw`` at the span it bound, in raw-offset order."""

    if not replacements:
        return raw
    pieces: list[str] = []
    position = 0
    for start, end, replacement in sorted(replacements):
        pieces.append(raw[position:start])
        pieces.append(replacement)
        position = end
    pieces.append(raw[position:])
    return "".join(pieces)


def _merged_spans(spans: list[tuple[int, int]]) -> list[tuple[int, int]]:
    """Union every overlapping or touching group of spans into one span, ordered by start.

    Every caller renders each returned span once, from its own bytes, so overlapping
    matches must merge: a span left separate would nest one token inside another or
    split one location across two tokens. Touching spans merge for the same reason:
    a raw match beginning where a decoded claim ends is one location's two halves,
    and rendering only the first half would leave the second half's bytes in the
    record; over-union is over-redaction, the fail-closed direction.
    """

    merged: list[list[int]] = []
    for start, end in sorted(spans):
        if merged and start <= merged[-1][1]:
            merged[-1][1] = max(merged[-1][1], end)
        else:
            merged.append([start, end])
    return [(start, end) for start, end in merged]


# The bytes `_raw_literal_spans`' scan re-enters behind a match it kept. A match
# starting deeper inside the kept match consumes the same tail run and ends at the
# same position (the kept match's tail already runs to that end), so the window only
# has to be wide enough to expose the starts whose own prefix straddles that end:
# the widest such prefix is a drive location's device namespace (`_DEVICE_PREFIX`'s
# own two backslash runs, four bytes each, around the one-byte `?`/`.` selector)
# plus the drive letter behind it, ten bytes, and a `file:` URL's scheme colon sits
# four bytes behind its start. The namespace's width is read off its pattern text,
# so widening the pattern widens the window with it; a narrower window hides a
# second location whose start crossed that far behind the stop (the round-4
# review's 10-byte straddle, pinned in the JSON module).
_DEVICE_PREFIX_WIDTH = 1 + sum(
    int(bound) for bound in re.findall(r"\{1,(\d+)\}", _DEVICE_PREFIX)
)
_CONTAINED_RESUME = _DEVICE_PREFIX_WIDTH + 1  # the drive letter behind the prefix


def _raw_literal_spans(content: str, claimed: list[tuple[int, int, str]]) -> list[tuple[int, int]]:
    """The raw Windows spellings a JSON string literal's recorded bytes carry.

    ``claimed`` are the spans the decoded reading read a location in - its claims,
    mapped back to the literal's raw offsets, and the spans earlier raw looks wrote.
    The Windows-shaped raw families run over the recorded bytes for the one location
    the decoded reading cannot see: a *raw* spelling, whose separators are single
    backslashes that decode into control characters, so ``C:\\temp`` reads as ``C:``,
    a tab and ``emp`` - no location - while the recorded bytes spell one. Every span
    returned here is unioned with the claims by the caller, which renders each
    connected group as one token, so no token can nest inside or overlap another.

    Every match the scan reads contributes the parts of its span that no claim and
    no rendered token's own text covers: the union merges a part with the claims it
    touches, so one location renders as one token, and a rendered token's text is
    never bound into a digest (the ``path-sha256:<A>:<B>`` corruption). The scan
    reads each family's matches in one pass and does not re-enter a match it kept
    deeper than `_CONTAINED_RESUME` bytes: a match starting there consumes the same
    byte run the kept match's tail already reaches the end of, so its span adds no
    byte the kept match does not hold, and the parts it would render are inside the
    kept match's own parts - a pair-bearing match's one-escape tail stays outside as
    the decoded reading spells it, the rule the kept match already applies, so a
    spelling nested inside that tail is not read as a second, pair-free match over
    it. A match whose span carries no escaped backslash pair (``\\\\``) is the raw
    spelling throughout all its parts, and it is never discarded for overlapping a
    claim: a forward-slash prefix continued by raw separators renders whole, and the
    escape and text behind a forward-slash spelling are absorbed with it
    (over-redaction, the fail-closed direction). A match that carries a pair is the
    producer mix of the decoded reading's own spelling and a raw one, and there the
    decoded reading governs: of the uncovered parts, only those carrying a backslash
    beyond their opening escape character are raw spellings and render
    (``...\\\\a\\repo`` renders whole); a part that is one escape character and the
    text behind it stays outside the token, exactly as the decoded reading spells it:
    ``C:\\\\Users\\\\op\\\\run`` + the
    newline escape + ``FAILED`` keeps the escape and ``FAILED`` outside, and the
    pinned mixed spelling ``C:\\\\a\\repo`` keeps ``\\repo`` outside. No pair-bearing
    match's raw bytes are left for a later render to find, so rendering stays a
    fixpoint.

    The scan reads the recorded bytes, where the left context of a spelling is the
    byte the *input* wrote, but the decoded reading's rebuild - every claim's
    rendered text in place of the raw span it replaced - is the text the next render
    will read. The left context therefore differs in exactly one place: a spelling
    that begins where a claim's text ends. The same families run a second time over
    that rebuild, anchored at each claim's end (`pattern.match` at the rebuilt
    offset sees the rebuilt left context), because a `--root` token's own end
    exposes a spelling the raw byte before it hides: the slash-UNC and `file:`
    families accept a preceding `>` and refuse the root name character the token
    replaced, so `{"out":"/srv/store//run` + the newline escape + `FAILED"}`
    renders as one group in the first pass instead of leaving `//run` and the
    escape for the next pass to bind together with the token's own text (the
    round-5 review's P2, where the documented `--root` publish flow wrote a record
    the gate then refused). An anchored match contributes exactly what the same
    family's raw scan would have contributed from that start - the recorded bytes
    from the claim's raw end, under the pair rules below - and the containment rule
    above governs the anchored attempts too: a match starting at a deeper claim's
    end consumes the same tail run the kept match already reaches the end of.

    A match whose span carries no backslash at all is left to the decoded reading,
    which reads those bytes as written: the raw look exists for the single-backslash
    spellings, and where the two readings' byte classes still differ outside one (a
    recorded ``\\x7f`` the decoded reading refuses and the raw tail admits) the
    decoded reading's boundary decision is the governing one.
    """

    if not _RAW_SPELLING_ESCAPE.search(content):
        return []
    # The spans no part may cover: the claims the decoded reading wrote and every
    # rendered token's own text, merged into one sorted, disjoint set.
    covered = _merged_spans(
        sorted((claim[0], claim[1]) for claim in claimed)
        + [match.span() for match in _TOKEN_SPELLING.finditer(content)]
    )
    covered_starts = [start for start, _end in covered]
    covered_ends = [end for _start, end in covered]
    # Every recorded backslash, so a part's "further separator" test is a bisect.
    backslashes = [index for index, character in enumerate(content) if character == "\\"]

    def uncovered(start: int, end: int) -> list[tuple[int, int]]:
        """The parts of ``[start, end)`` the covered spans leave, in order."""
        parts: list[tuple[int, int]] = []
        position = start
        index = bisect.bisect_right(covered_ends, start)
        while index < len(covered_starts) and covered_starts[index] < end:
            if covered_starts[index] > position:
                parts.append((position, covered_starts[index]))
            position = max(position, covered_ends[index])
            index += 1
        if position < end:
            parts.append((position, end))
        return parts

    def separator_after_first(part_start: int, part_end: int) -> bool:
        index = bisect.bisect_left(backslashes, part_start + 1)
        return index < len(backslashes) and backslashes[index] < part_end

    def contribute(span_start: int, span_end: int) -> None:
        """Union one match's uncovered parts, under the docstring's pair rules.

        ``span_start``/``span_end`` are raw offsets: the match's own span for the
        recorded-bytes scan, and the recorded bytes an anchored match covers behind
        the claim it starts at. A span carrying no backslash at all is left to the
        decoded reading; a span carrying no escaped pair is a raw spelling through
        all its parts; a pair-bearing span keeps only the parts carrying a backslash
        beyond their opening escape character.
        """

        text = content[span_start:span_end]
        if "\\" not in text:
            return
        if "\\\\" not in text:
            spans.extend(uncovered(span_start, span_end))
            return
        spans.extend(
            part for part in uncovered(span_start, span_end) if separator_after_first(*part)
        )

    # The decoded reading's rebuild (`_render_passes`' own output text) and where each
    # claim's text sits in it, so the anchored pass below reads the left context the
    # next render will read. The shifts mirror `_rebuild_with_groups`': claims are
    # written in order and pairwise disjoint, so both lists ascend.
    rebuilt: list[str] = []
    rebuilt_starts: list[int] = []
    rebuilt_ends: list[int] = []
    claim_ends: list[tuple[int, int]] = []
    shifts = [0]
    position = 0
    for raw_start, raw_end, text in claimed:
        rebuilt.append(content[position:raw_start])
        rebuilt.append(text)
        rebuilt_starts.append(raw_start + shifts[-1])
        rebuilt_ends.append(raw_start + shifts[-1] + len(text))
        claim_ends.append((raw_end, rebuilt_ends[-1]))
        shifts.append(shifts[-1] + len(text) - (raw_end - raw_start))
        position = raw_end
    rebuilt.append(content[position:])
    rebuilt_text = "".join(rebuilt)

    def raw_of(rebuilt_offset: int) -> int:
        """The raw offset one rebuilt-text offset maps back to, through the claims' shifts.

        An offset inside a claim's text is the far side of that claim's raw span (the
        claim's rendered text stands for the whole span); an offset outside every
        claim drops the shifts of the claims in front of it.
        """

        index = bisect.bisect_right(rebuilt_starts, rebuilt_offset) - 1
        if index >= 0 and rebuilt_offset < rebuilt_ends[index]:
            return claimed[index][1]
        return rebuilt_offset - shifts[bisect.bisect_right(rebuilt_ends, rebuilt_offset)]

    spans: list[tuple[int, int]] = []
    for pattern in _RAW_READING.windows:
        start = 0
        frontier = -1
        while True:
            # `finditer` would resume at a match's end, so a location whose tail
            # swallowed the next location's first bytes was hidden from this render
            # and found by the next one - the non-idempotence the union exists to
            # close (the review's M6 shape: the second location's prefix straddles
            # the stop that ended the first match). The scan therefore re-enters a
            # match it kept, but never deeper than `_CONTAINED_RESUME` bytes: a start
            # deeper inside begins the same tail run the kept match's tail already
            # reaches the end of, so its span adds no byte and its parts are the kept
            # match's own (the docstring's rule), while every scan runs at most to its
            # own match's end - a listing whose lines are one run no longer walks that
            # run once per line (the review's P1: 99 KB in 49 s).
            match = pattern.search(content, start)
            if match is None:
                break
            if match.end() <= frontier:
                # Contained in the kept match whose window this start is in: its own
                # span adds no byte, and scanning one byte on stays inside the window.
                start = match.start() + 1
                continue
            frontier = match.end()
            start = max(match.start() + 1, frontier - _CONTAINED_RESUME)
            contribute(match.start(), match.end())
        # The same family over the decoded reading's rebuild, anchored at each claim's
        # end: the one left context the recorded bytes get wrong (the docstring's
        # anchored paragraph). An attempt starting inside a kept match is contained -
        # its own tail run is the kept match's, which already reaches its end - and a
        # claim's rendered text ends with a name character for every hashed token, so
        # a family body only starts behind a `--root` token's own end.
        frontier = -1
        for raw_end, rebuilt_end in claim_ends:
            if rebuilt_end < frontier:
                continue
            match = pattern.match(rebuilt_text, rebuilt_end)
            if match is None:
                continue
            frontier = match.end()
            contribute(raw_end, raw_of(match.end()))
    return _merged_spans(spans)


def _align_span(raw: str, start: int, end: int) -> tuple[int, int]:
    """Snap a literal replacement's start to the backslash run it splits, for valid JSON.

    A replacement's text is the inert token alphabet, so a backslash run the span
    splits re-pairs from the new boundary: an odd prefix in front of the token spells
    an escape whose letter is the token's own first character (``\\p``, ``\\<``),
    which is never a valid escape, and the literal stops parsing as JSON. The span
    therefore absorbs the prefix whenever it is odd, so every replacement's start
    falls on a pair boundary of the run it touches. No end needs snapping: a raw
    family's tail is greedy over a backslash run, so a match ends at a run's end or
    before the whole run, and a claim ends on the spelling its last character read (a
    pair's end or a plain byte) - never inside a run at an odd offset. The deletion
    of an end rule is measured, not assumed: 346k generated run-focused literals and
    200k mixed generated literals produced no invalid render without it.
    """

    run_start = start
    while run_start > 0 and raw[run_start - 1] == "\\":
        run_start -= 1
    if (start - run_start) % 2:
        start = run_start
    return start, end


def _literal_replacements(
    raw: str, replacements: list[tuple[int, int, str]], extra: list[tuple[int, int]]
) -> list[tuple[int, int, str]]:
    """Union the raw look's spans with the spans the decoded reading claimed.

    Overlapping or touching spans become one connected group, and each group renders
    once: a group that is exactly one replacement keeps that replacement's own text,
    so a ``--root`` token stays the root token it is; every other group renders from
    its own recorded bytes, so no token nests inside another, no digest binds a
    token's own text, and no raw span's bytes are left behind beside a claim. Every
    span is snapped to the backslash runs it splits first (`_align_span`), because a
    boundary inside a run would re-pair the recorded escapes and the rendered literal
    would stop parsing as JSON.
    """

    texts = {(start, end): text for start, end, text in replacements}
    return [
        (start, end, texts[(start, end)] if (start, end) in texts
         else _render_record_span(raw, start, end))
        for start, end in _merged_spans(
            [_align_span(raw, start, end) for start, end, _text in replacements]
            + [_align_span(raw, start, end) for start, end in extra]
        )
    ]


def _render_literal(content: str, roots: list[str], root_only: bool) -> str:
    """Render host locations in one JSON string literal, as decoded text and raw bytes.

    Every matcher runs on the decoded content, so a space-like character or an
    apostrophe an escape spelled is the path character it decodes to and never
    truncates the match, while a control character an escape spells is a boundary; a
    match maps back to the raw span it covers, escapes included, so the token replaces
    exactly the recorded bytes it bound and the digest binds the recorded spelling.
    The passes run in the same order as the raw reading (the root matchers first, then
    the host or default path family).

    The literal's recorded bytes are read a second time by the Windows-shaped raw
    families (`_raw_literal_spans`), because a raw Windows spelling is the one location
    the decoded reading cannot see: the caller unions those spans with the decoded ones
    before rendering, so a raw spelling renders whole, a raw span overlapping a decoded
    span is one token and never two, and a raw match is not discarded for overlapping a
    decoded claim (the one exception, a raw match carrying an escaped backslash pair
    beside such a claim, is `_raw_literal_spans`' own boundary).
    """

    decoded, cells = _decode_literal(content)
    if not cells:
        return content
    passes: list[tuple[re.Pattern[str], bool]] = []
    for root in roots:
        matchers = _root_patterns(root, _DECODED_READING)
        if matchers is None:
            continue
        bounded, chunked = matchers
        passes.append((chunked, False))
        passes.append((bounded, True))
    for pattern in (_DECODED_READING.host if root_only else _DECODED_READING.default):
        passes.append((pattern, False))
    replacements = _render_passes(decoded, cells, passes, content)
    return _apply_replacements(content, _literal_replacements(
        content, replacements, _raw_literal_spans(content, replacements),
    ))


def _is_json_structured(text: str) -> bool:
    """Whether the whole text is a JSON document, or every non-empty line is.

    The reading is decided once per document, not per quoted span: a JSON document's
    strings are decoded, and everything else is prose that keeps the raw reading. A
    JSONL file's lines are each a document, so a line's strings decode the same way.
    """

    try:
        json.loads(text)
        return True
    except ValueError:
        pass
    lines = [line for line in text.splitlines() if line.strip()]
    if not lines:
        return False
    for line in lines:
        try:
            json.loads(line)
        except ValueError:
            return False
    return True


def _render_json_record(text: str, roots: list[str], root_only: bool) -> str:
    """Render a JSON-structured record: decode every string literal, render, re-spell.

    Only string literals carry text in a JSON document; keys are literals too and are
    read the same way. A literal's quotes never move, and every replacement is spelled
    from the token alphabet, so the rendered record stays valid JSON.
    """

    pieces: list[str] = []
    position = 0
    index = 0
    while index < len(text):
        if text[index] != '"':
            index += 1
            continue
        end = _string_literal_end(text, index)
        if end < 0:
            break
        pieces.append(text[position:index + 1])
        pieces.append(_render_literal(text[index + 1:end - 1], roots, root_only))
        position = end - 1
        index = end
    pieces.append(text[position:])
    return "".join(pieces)


def _render_prose(text: str, roots: list[str], root_only: bool) -> str:
    """Render prose with the base tool's raw reading and the shared escape boundary."""

    passes: list[tuple[re.Pattern[str], bool]] = []
    for root in roots:
        matchers = _root_patterns(root, _RAW_READING)
        if matchers is None:
            continue
        bounded, chunked = matchers
        # A root followed by a space-continued chunk continues a longer sibling
        # name, so the bounded matcher declines it there; the chunked matcher
        # renders the whole span as a hashed token first: the span is not this
        # root, and leaving it literal would publish the root bytes on a POSIX
        # root the host-path patterns do not cover.
        passes.append((chunked, False))
        passes.append((bounded, True))
    for pattern in (_RAW_READING.host if root_only else _RAW_READING.default):
        passes.append((pattern, False))
    return _apply_replacements(text, _render_passes(text, None, passes, text))


def sanitize_text(text: str, roots: list[str], root_only: bool = False) -> str:
    """Render host-specific absolute paths in ``text`` as tokens.

    ``roots`` are disposable coordinator roots (absolute paths) to render as
    ``<disposable-root>``. A root matches any recorded separator mixture,
    including the JSON-escaped runs of a quoted record, but only at a path
    boundary: a longer sibling name such as ``...-user`` is never absorbed into
    the token, and a root followed by a space-continued chunk (`.../run
    user/private`) is that same longer sibling, rendered whole as a hashed
    token. With ``root_only`` false every other absolute path is rendered as
    a hashed token; with ``root_only`` true only host absolute paths (drive, UNC
    and the POSIX home/root/temp/mount roots) are rendered, so a product route
    such as ``/portal/runtimes`` stays literal. Rendering is a fixpoint in
    ``root_only`` mode for text that carries no rendered token text of its own:
    an input already spelling a token (``//<disposable-root>`` + an escape, a
    ``path-sha256:`` spelling inside a location) can bind that token's own text on
    a later render. The default scope carries one further disclosed exception, where
    re-rendering a literal whose decoded text has a separator directly in front of a
    rendered token (an escaped forward slash ahead of a raw spelling's token) reads
    the separator and the token's text as a POSIX path and binds the token's own text
    into a new digest. No host byte survives either render - what a second render
    binds is a rendered token, not host data - so these are disclosed
    non-fixpoints, not leaks; ``docs/TODO.md`` carries the entries.

    A JSON-structured record is read as its own JSON: every string literal is
    decoded and the matchers run on the decoded text, so an escape a writer spelled
    (``\\u0020``, ``\\u00a0``, ``\\u0027``) is the path character it decodes to and
    a location never splits at one, while the escape a quoted line ends with
    (``\\n``) stays a boundary and the line behind it stays outside the token. A
    match replaces the raw bytes it covered, so the digest still binds the recorded
    spelling. The Windows families also run over the literal's recorded bytes, and
    the parts they read - a *raw* spelling, whose separators are single backslashes
    that decode into control characters, leaving the decoded reading no location to
    read - are unioned with the decoded reading's own spans; they run a second time,
    anchored at each claim's end, over the decoded reading's own rebuild, where a
    ``--root`` token's end exposes a spelling the raw byte before it hid (the
    publish flow's root directly followed by ``//child``). A raw match is never
    left behind beside a decoded claim: a forward-slash prefix continued by raw
    separators renders whole, the escape and the text behind it included, where the
    decoded reading read only the prefix. A raw match carrying an escaped backslash
    pair is a producer mix, and there the decoded reading's render governs its
    one-escape tails: a pair-bearing match's parts with a further backslash render
    whole, while ``C:\\\\Users\\\\op\\\\run`` + the newline escape + ``FAILED``
    keeps the escape and ``FAILED`` outside the token. Prose keeps the base tool's
    raw reading, with one addition: the shared leading boundary reads the character
    an escape ends with as a boundary, so a location behind a recorded escape still
    renders. The module docstring names both readings and the boundary each one
    keeps.
    """

    if _is_json_structured(text):
        return _render_json_record(text, roots, root_only)
    return _render_prose(text, roots, root_only)


def _parse_arguments(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Render host-specific absolute paths in recorded evaluation text as tokens.",
    )
    parser.add_argument("--root", action="append", default=[], metavar="ABSOLUTE_PATH",
                        help="disposable coordinator root to render as <disposable-root>; repeatable")
    parser.add_argument("--check", action="store_true",
                        help="write nothing and exit 1 when the input still carries a host path")
    parser.add_argument("--root-only", action="store_true",
                        help="render/flag only host absolute paths and --root disposable roots; "
                             "product routes such as /portal/runtimes stay literal")
    parser.add_argument("file", nargs="?", default="-", metavar="FILE",
                        help="file to render, or - for standard input (default)")
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    arguments = _parse_arguments(argv)
    # A record is recorded text: bytes that do not decode as UTF-8 would be checked as
    # mojibake, whose contiguous ASCII patterns can hide a host path, so refuse instead.
    if arguments.file == "-":
        raw = sys.stdin.buffer.read()
        source = "standard input"
    else:
        path = pathlib.Path(arguments.file)
        if not path.is_file():
            print(f"model-routing-record-sanitize refused: {path} is not a readable file", file=sys.stderr)
            return 2
        try:
            # A file that exists but cannot be read is not text the checker got to
            # inspect: report it as unusable (exit 2) instead of crashing, because an
            # uncaught traceback exits 1 and a `--check --root-only` run reads that as
            # "still carries a host absolute path" - a verdict about bytes never seen.
            raw = path.read_bytes()
        except OSError as error:
            print(f"model-routing-record-sanitize refused: cannot read {path}: {error}", file=sys.stderr)
            return 2
        source = str(path)
    try:
        # newline="" keeps the recorded line endings byte-faithful: a record's
        # CRLF/LF convention must survive rendering unchanged.
        text = raw.decode("utf-8")
    except UnicodeDecodeError as error:
        print(
            f"model-routing-record-sanitize refused: {source} does not decode as UTF-8 ({error})",
            file=sys.stderr,
        )
        return 2
    if "\x00" in text:
        # UTF-16 text whose characters are ASCII decodes as UTF-8 with interleaved NULs,
        # and interleaved NULs hide a host path from every contiguous pattern; recorded
        # text never carries one, so refuse instead of checking mojibake.
        print(
            f"model-routing-record-sanitize refused: {source} carries NUL bytes, so it is not "
            "recorded text",
            file=sys.stderr,
        )
        return 2
    rendered = sanitize_text(text, arguments.root, root_only=arguments.root_only)
    if arguments.check:
        if rendered != text:
            # The refusal names the scope it refused: the default scope renders every
            # absolute location (a product route refuses too), while `--root-only`
            # refuses nothing but a host absolute path.
            refusal = ("a host absolute path is present" if arguments.root_only
                       else "an absolute location would be rendered")
            print(f"model-routing-record-sanitize refused: {refusal}", file=sys.stderr)
            return 1
        return 0
    sys.stdout.buffer.write(rendered.encode("utf-8"))
    sys.stdout.buffer.flush()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
