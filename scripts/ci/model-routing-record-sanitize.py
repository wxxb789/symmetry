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
render it.

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
        file_url=file_url, space_chunk=space_chunk, root_guard=root_guard,
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


def hashed_path_token(path: str) -> str:
    """Return the stable hashed token for one absolute path."""

    return "path-sha256:" + hashlib.sha256(path.encode("utf-8")).hexdigest()


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
    ``content``: one character for a plain byte, two for a single-letter escape, six
    for ``\\uXXXX``, and twelve for a surrogate pair, which decodes to the one
    character both units spell - the same pairing ``json.loads`` performs.
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
        code = int(content[index + 2:index + 6], 16)
        width = 6
        if 0xD800 <= code < 0xDC00 and content[index + 6:index + 8] == "\\u":
            low = int(content[index + 8:index + 12], 16)
            if 0xDC00 <= low < 0xE000:
                code = 0x10000 + ((code - 0xD800) << 10) + (low - 0xDC00)
                width = 12
        characters.append(chr(code))
        spans.append((index, index + width))
        index += width
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
    replacement claims one, and the claimed spans below then carry the whole mapping.

    A match that overlaps a span an earlier replacement wrote merges with it into one
    span before rendering: the merged span renders once, from its own raw bytes, so a
    later matcher can never bind an earlier token's text and no render leaves an
    orphaned ``:<hex>`` fragment of one token inside or behind another. The passes run
    in the order the caller gives - the root matchers first, then the host or default
    path family, the base tool's one pass sequence.
    """

    current = record
    mapped = list(cells) if cells is not None else None
    # Each claim is ``(current start, current end, raw start, raw end)``: the current
    # span a replacement wrote and the raw span it bound.
    claimed: list[tuple[int, int, int, int]] = []
    replacements: list[tuple[int, int, str]] = []

    def raw_start_of(index: int) -> int:
        if mapped is not None:
            return mapped[index][0]
        for start, end, raw_start, _raw_end in claimed:
            if start <= index < end:
                return raw_start
        return index - sum(end - (raw_end - raw_start) - start
                           for start, end, raw_start, raw_end in claimed if end <= index)

    def raw_end_of(index: int) -> int:
        if mapped is not None:
            return mapped[index][1]
        for start, end, _raw_start, raw_end in claimed:
            if start <= index < end:
                return raw_end
        return index - sum(end - (raw_end - raw_start) - start
                           for start, end, raw_start, raw_end in claimed if end <= index) + 1

    def grow(start: int, end: int) -> tuple[int, int, bool]:
        grown = False
        while True:
            touched = [claim for claim in claimed if claim[0] < end and start < claim[1]]
            if not touched:
                return start, end, grown
            width = start, end
            start = min([start] + [claim[0] for claim in touched])
            end = max([end] + [claim[1] for claim in touched])
            grown = True
            if (start, end) == width:
                # Every touched claim already lies inside the span.
                return start, end, grown

    for pattern, is_root in passes:
        matched = [match.span() for match in pattern.finditer(current)]
        if not matched:
            continue
        plans: list[list[int | bool]] = []
        for start, end in matched:
            merged: list[int | bool] = list(grow(start, end))
            changed = True
            while changed:
                changed = False
                rest: list[list[int | bool]] = []
                for plan in plans:
                    if plan[0] < merged[1] and merged[0] < plan[1]:
                        merged[0] = min(merged[0], plan[0])
                        merged[1] = max(merged[1], plan[1])
                        merged[2] = merged[2] or plan[2]
                        changed = True
                    else:
                        rest.append(plan)
                plans = rest
            plans.append(merged)
        for start, end, grown in sorted(plans, key=lambda plan: plan[0], reverse=True):
            raw_start = raw_start_of(start)
            raw_end = raw_end_of(end - 1)
            replacement = (DISPOSABLE_ROOT_TOKEN if is_root and not grown
                           else _render_record_span(raw, raw_start, raw_end))
            current = current[:start] + replacement + current[end:]
            if mapped is not None:
                mapped = mapped[:start] + [(raw_start, raw_end)] * len(replacement) + mapped[end:]
            # Every claim to the right of the replaced span moves with the text.
            shift = len(replacement) - (end - start)
            claimed = [(claim[0] + shift, claim[1] + shift, claim[2], claim[3])
                       if claim[0] >= end else claim
                       for claim in claimed if not (start <= claim[0] and claim[1] <= end)]
            claimed.append((start, start + len(replacement), raw_start, raw_end))
            # A merged span absorbs the replacements it covered, raw bytes included:
            # their spans lie inside the merged span's own raw range.
            replacements = [item for item in replacements
                            if not (raw_start <= item[0] and item[1] <= raw_end)]
            replacements.append((raw_start, raw_end, replacement))
    return replacements


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


def _render_literal(content: str, roots: list[str], root_only: bool) -> str:
    """Render host locations in one JSON string literal, as decoded text and raw bytes.

    Every matcher runs on the decoded content, so a space-like character or an
    apostrophe an escape spelled is the path character it decodes to and never
    truncates the match, while a control character an escape spells is a boundary; a
    match maps back to the raw span it covers, escapes included, so the token replaces
    exactly the recorded bytes it bound and the digest binds the recorded spelling.
    The passes run in the same order as the raw reading (the root matchers first, then
    the host or default path family).
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
    return _apply_replacements(content, _render_passes(decoded, cells, passes, content))


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
    such as ``/portal/runtimes`` stays literal. The function is idempotent:
    rendering rendered text changes nothing.

    A JSON-structured record is read as its own JSON: every string literal is
    decoded and the matchers run on the decoded text, so an escape a writer spelled
    (``\\u0020``, ``\\u00a0``, ``\\u0027``) is the path character it decodes to and
    a location never splits at one, while the escape a quoted line ends with
    (``\\n``) stays a boundary and the line behind it stays outside the token. A
    match replaces the raw bytes it covered, so the digest still binds the recorded
    spelling. Prose keeps the base tool's raw reading, with one addition: the
    shared leading boundary reads the character an escape ends with as a boundary,
    so a location behind a recorded escape still renders. The module docstring
    names both readings and the boundary each one keeps.
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
