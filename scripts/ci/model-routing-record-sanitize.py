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
authority spelling is an absolute location, not a URL to spare.

Three render boundaries are disclosed non-fixpoints; `docs/evaluations/model-routing/README.md` names them.

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

A record that parses with ``json.loads`` - the whole text, or every non-empty line
(JSONL) - has every string literal decoded; anything else is prose, read as recorded.

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

    return r"(?:(?<=\\[nrtbf])|(?<=\\[u][0-9A-Fa-f]{4})|(?<![" + excluded + r"]))"


class _ByteClasses(NamedTuple):

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
# One separator of a location whose separators may be mixed.
_SEPARATOR = r"(?:\\|(?:\\{2})+|/|\\/)"
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
# separators) and a device namespace (`\\?\` or the `\\.\` device form).
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

    drive: re.Pattern[str]
    unc: re.Pattern[str]
    slash_unc: re.Pattern[str]
    posix: re.Pattern[str]
    colon_posix: re.Pattern[str]
    escaped_posix: re.Pattern[str]
    host_posix: re.Pattern[str]
    root_relative: re.Pattern[str]
    file_url: re.Pattern[str]
    windows: tuple[re.Pattern[str], ...]
    space_chunk: str
    root_guard: str
    host: tuple[re.Pattern[str], ...]
    default: tuple[re.Pattern[str], ...]


def _compile_patterns(classes: _ByteClasses, root_guard: str) -> _Patterns:

    tail = classes.tail
    no_backslash = tail + r"\\"
    no_separator = tail + r"\\/"
    space_chunk = r" [^" + tail + r"]+" + _SPACE_END
    windows_tail_unit = r"(?:[^" + tail + r"]|" + space_chunk + r")"
    windows_tail = windows_tail_unit + r"*"
    windows_tail_plus = windows_tail_unit + r"+"
    posix_byte = r"(?:[^" + classes.posix + r"]|\\/)"
    posix_run = r"(?:" + posix_byte + r")+"
    posix_segment = r"(?:" + posix_run + r"(?: " + posix_run + _SPACE_END + r")*)"
    escaped_segment = r"(?:[^" + classes.escaped + r"]+(?: [^" + classes.escaped + r"]+" + _SPACE_END + r")*)"
    drive = re.compile(
        leading_boundary(r"A-Za-z0-9_/\\>") + _FILE_PREFIX + _DEVICE_PREFIX
        + r"[A-Za-z]:[\\/]" + windows_tail
    )
    unc = re.compile(
        leading_boundary(r"A-Za-z0-9_/\\>") + _FILE_PREFIX + _DEVICE_PREFIX + r"(?:\\{2})+"
        + r"[^" + no_backslash + r"]+" + _SEPARATOR + windows_tail_plus
    )
    posix = re.compile(
        leading_boundary(r"A-Za-z0-9_:/\\>.") + r"/(?:" + posix_segment + r"/)*" + posix_segment
    )
    colon_posix = re.compile(r"(?<=:)/(?!/)(?:" + posix_segment + r"/)*" + posix_segment)
    escaped_posix = re.compile(
        r"(?<!" + re.escape(DISPOSABLE_ROOT_TOKEN) + r")" + leading_boundary(r"A-Za-z0-9_:\\/")
        + r"(?:\\/)(?:" + escaped_segment + r"\\/)*" + escaped_segment
    )
    slash_unc = re.compile(
        leading_boundary(r"A-Za-z0-9_:\\/") + r"(?:/|\\/)(?:/|\\/)[^" + no_separator + r"]+"
        + _SEPARATOR + windows_tail_plus
    )
    # The segment class allows an empty segment, so a naive concatenation (`/tmp//run`)
    # is still a host path; a preceding `:` is accepted so a location spelled after a colon is still caught,
    # while a preceding separator is not: that spelling is an authority path or a URL,
    # handled by their own patterns, and it keeps `https://tmp/vendor/doc` literal.
    host_posix = re.compile(
        leading_boundary(r"A-Za-z0-9_>./\\") + r"(?:" + _HOST_SEP + r")" + _HOST_ROOT
        + r"(?:(?:" + _HOST_SEP + r")(?:" + posix_segment + r")?)+"
    )
    # A root-relative Windows spelling (`\Users\op\run`): classic Windows semantics
    # resolve a leading single backslash against the current drive root, so it names
    # the same profile location as `C:\Users\op\run`. The leader is an odd backslash run
    # (the raw and Markdown-prose spelling).
    root_relative = re.compile(
        leading_boundary(r"A-Za-z0-9_>./\\") + r"(?:\\{2})*\\" + _HOST_ROOT
        + r"(?:\\[^" + no_backslash + r"]*|" + space_chunk + r")+"
    )
    # The host-root arm needs no segment after the root (`file:///tmp` is a location too);
    # an authority followed by a drive location (`file://localhost/C:/x`) is one location.
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


_ROOT_TOKEN_GUARD = r"(?<!" + re.escape(DISPOSABLE_ROOT_TOKEN) + r")"
_RAW_READING = _compile_patterns(_RAW_BYTE_CLASSES, _ROOT_TOKEN_GUARD)
_DECODED_READING = _compile_patterns(_DECODED_BYTE_CLASSES, _ROOT_TOKEN_GUARD)
HOST_PATH_PATTERNS = _RAW_READING.host
DEFAULT_PATH_PATTERNS = _RAW_READING.default


HASHED_PATH_TOKEN_PREFIX = "path-sha256:"
_RAW_SPELLING_ESCAPE = re.compile(r"(?<!\\)(?:\\\\)*\\[nrtbfu]")
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
    render idempotent. In the decoded reading the rule cannot fire: a match ends at
    a decoded-character boundary, so a span never ends inside the ``\\"`` pair.
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
    lookbehind = reading.root_guard + leading_boundary(r"A-Za-z0-9_.\-\\/")
    after = r"(?!" + _ROOT_NAME_CHAR + r"|\." + _ROOT_NAME_CHAR + r"|" + reading.space_chunk + r")"
    return (
        re.compile(lookbehind + body + after),
        re.compile(lookbehind + body + r"(?:" + reading.space_chunk + r")+"),
    )


def _string_literal_end(text: str, start: int) -> int:

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


# The single-letter escapes the JSON grammar spells: ``\\`` and ``\/`` decode to separators
# that continue a match; every other escape decodes to a byte the decoded reading refuses.
_LITERAL_ESCAPES = {'"': '"', "\\": "\\", "/": "/", "b": "\b", "f": "\f",
                    "n": "\n", "r": "\r", "t": "\t"}


def _render_passes(
    record: str, cells: list[tuple[int, int]] | None,
    passes: list[tuple[re.Pattern[str], bool]], raw: str,
) -> list[tuple[int, int, str]]:

    current = record
    mapped = list(cells) if cells is not None else None
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

    starts = [claim[0] for claim in claims]
    ends = [claim[1] for claim in claims]
    shifts = [0]
    for _start, _end, claim_start, claim_end, text in claims:
        shifts.append(shifts[-1] + len(text) - (claim_end - claim_start))

    def raw_start_of(index: int) -> int:
        if mapped is not None:
            return mapped[index][0]
        position = bisect.bisect_right(starts, index) - 1
        if position >= 0 and ends[position] > index:
            return claims[position][2]
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
    Split spans would nest tokens or leave path bytes; over-union is the fail-closed direction."""

    merged: list[list[int]] = []
    for start, end in sorted(spans):
        if merged and start <= merged[-1][1]:
            merged[-1][1] = max(merged[-1][1], end)
        else:
            merged.append([start, end])
    return [(start, end) for start, end in merged]


_DEVICE_PREFIX_WIDTH = 1 + sum(
    int(bound) for bound in re.findall(r"\{1,(\d+)\}", _DEVICE_PREFIX)
)
_CONTAINED_RESUME = _DEVICE_PREFIX_WIDTH + 1  # the drive letter behind the prefix


def _raw_literal_spans(content: str, claimed: list[tuple[int, int, str]]) -> list[tuple[int, int]]:

    if not _RAW_SPELLING_ESCAPE.search(content):
        return []
    covered = _merged_spans(
        sorted((claim[0], claim[1]) for claim in claimed)
        + [match.span() for match in _TOKEN_SPELLING.finditer(content)]
    )
    covered_starts = [start for start, _end in covered]
    covered_ends = [end for _start, end in covered]
    backslashes = [index for index, character in enumerate(content) if character == "\\"]

    def uncovered(start: int, end: int) -> list[tuple[int, int]]:

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

        text = content[span_start:span_end]
        if "\\" not in text:
            return
        if "\\\\" not in text:
            spans.extend(uncovered(span_start, span_end))
            return
        spans.extend(
            part for part in uncovered(span_start, span_end) if separator_after_first(*part)
        )

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

        index = bisect.bisect_right(rebuilt_starts, rebuilt_offset) - 1
        if index >= 0 and rebuilt_offset < rebuilt_ends[index]:
            return claimed[index][1]
        return rebuilt_offset - shifts[bisect.bisect_right(rebuilt_ends, rebuilt_offset)]

    spans: list[tuple[int, int]] = []
    for pattern in _RAW_READING.windows:
        start = 0
        frontier = -1
        while True:
            match = pattern.search(content, start)
            if match is None:
                break
            if match.end() <= frontier:
                start = match.start() + 1
                continue
            frontier = match.end()
            start = max(match.start() + 1, frontier - _CONTAINED_RESUME)
            contribute(match.start(), match.end())
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

    run_start = start
    while run_start > 0 and raw[run_start - 1] == "\\":
        run_start -= 1
    if (start - run_start) % 2:
        start = run_start
    return start, end


def _literal_replacements(
    raw: str, replacements: list[tuple[int, int, str]], extra: list[tuple[int, int]]
) -> list[tuple[int, int, str]]:

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

    passes: list[tuple[re.Pattern[str], bool]] = []
    for root in roots:
        matchers = _root_patterns(root, _RAW_READING)
        if matchers is None:
            continue
        bounded, chunked = matchers
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
    and the POSIX home/root/temp/mount roots) are rendered.
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
