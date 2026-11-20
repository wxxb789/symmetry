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
import pathlib
import re
import sys

DISPOSABLE_ROOT_TOKEN = "<disposable-root>"

# A drive path (``C:\x`` or ``C:/x``), a UNC path (``\\server\share\x``) or a
# POSIX path (``/x``). The lookbehind keeps these from matching inside URLs
# (``https://x``), inside relative paths (``../x``, ``a/b``), inside bare
# ``/`` separators, inside ``sha256:`` digests, and it keeps the trailing
# structure of an already-rendered ``<disposable-root>`` token from being
# rendered a second time. The trailing character class stops at quotes,
# whitespace and JSON/CLI punctuation so a quoted or comma-separated path
# renders cleanly, and the final ``+`` requires a real path segment so a lone
# separator never matches.
# The boundary that keeps a space-continued path chunk inside one location: the
# chunk must reach another separator (`\`, any slash spelling or `\/`), close on a
# quote, or end the line. A chunk bounded only by end-of-line (or a quote) is the
# documented over-approximation: `C:\Users\op\my project` renders whole, and so
# does the prose word in `C:\Users\op\x today`, because the two are the same shape
# and a partial render of a space-containing segment would leave path bytes in the
# record. A word followed by another word is not a continuation, so
# `C:\Users\x and the run continues` still ends at `x`.
_SPACE_END = r"(?=[\\/]|[\"'`]|\r|\n|\Z)"
# One space-continued chunk: a single space whose following non-space run reaches
# a separator (`First Last\repo` continues) or is a terminal word (`my project` at
# the end of a line or before a closing quote).
_SPACE_CHUNK = r" [^\s\"'`,;:)]+" + _SPACE_END
# One forward slash, or its JSON-escaped spelling (`\/`): a record quoted into a JSON
# string carries both forms, and the checker must not miss a host path because the
# writer escaped it.
_HOST_SEP = r"(?:/|\\/)"
# One separator of a location whose separators may be mixed: a Windows UNC accepts
# both slash directions, and a JSON writer doubles each backslash run at every
# nesting level, so a backslash separator is any positive run and a forward slash
# may be escaped as `\/`.
_SEPARATOR = r"(?:\\|(?:\\{2})+|/|\\/)"
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
# backslashes a JSON writer doubles). The lookbehinds accept a preceding `:` so a
# location spelled after a key (`root:C:\x`, `root:/home/x`) is still a location,
# while a preceding letter, digit, underscore, separator or rendered token keeps
# relative paths and URLs out.
_FILE_PREFIX = r"(?:" + _FILE_SCHEME + r"[\\/]*)?"
_DEVICE_PREFIX = r"(?:\\{1,4}[?.]\\{1,4})?"
# A Windows path tail: path bytes, or a space-continued chunk. The byte class
# stops at quotes, whitespace and JSON/CLI punctuation, so a quoted or
# comma-separated path renders cleanly, and a single backslash stays a path byte:
# in raw recorded text it IS the separator, and the JSON-escaped spelling doubles
# it, so excluding it would stop every raw Windows location at its first inner
# separator.
_WINDOWS_TAIL = r"(?:[^\s\"'`,;:)]|" + _SPACE_CHUNK + r")*"
# The same tail where the shape requires a real byte after the separator (a UNC
# location whose second segment is empty, `\\server\`, is not a path: an empty tail
# would read the two-character prefix of an escaped source literal like `\\n\` as
# one, the false positive the corpus sweep caught in an embedded code sample).
_WINDOWS_TAIL_PLUS = r"(?:[^\s\"'`,;:)]|" + _SPACE_CHUNK + r")+"
# One byte of a POSIX path segment: any byte except a separator, whitespace, a
# quote and JSON/CLI punctuation - or a JSON-escaped forward slash, which is the
# escaped spelling of the segment separator. A bare backslash is deliberately NOT
# a segment byte: a POSIX location never contains one, and consuming it would
# swallow a JSON escape sequence together with the text behind it (the render of
# `/tmp/run\nFAIL` would take the `\n` and the `FAIL` line into its token).
_POSIX_BYTE = r"(?:[^/\s\"'`,;:)\\]|\\/)"
_POSIX_RUN = r"(?:" + _POSIX_BYTE + r")+"
# The POSIX spelling of the same space continuation: the chunk after the space
# must reach a separator or end the line (`/home/first last/repo` and
# `/home/op/my project` are one location each).
_POSIX_SPACE_CHUNK = r" " + _POSIX_RUN + _SPACE_END
# A POSIX path segment: a run of bytes plus any space-continued chunks.
_POSIX_SEGMENT = r"(?:" + _POSIX_RUN + r"(?:" + _POSIX_SPACE_CHUNK + r")*)"
# A segment of the escaped POSIX spelling (`\/etc\/hosts`), whose escaped
# separator pairs sit outside the segment bytes.
_ESCAPED_SEGMENT = r"(?:[^\\/\s\"'`,;:)]+(?: [^\\/\s\"'`,;:)]+" + _SPACE_END + r")*)"
_DRIVE_PATH = re.compile(
    r"(?<![A-Za-z0-9_/\\>])" + _FILE_PREFIX + _DEVICE_PREFIX + r"[A-Za-z]:[\\/]" + _WINDOWS_TAIL
)
# A UNC path whose separators are backslashes, each written once or escaped by JSON
# quoting at any nesting level (an even run of two or more): `\\server\share` and
# `{"p":"\\\\server\\share"}` are the same location, and so is the doubly escaped
# spelling of either separator. The inner separators (and the tail after them) may
# also be forward slashes or their escapes, because Windows reads the mixed
# spellings as the same location.
_UNC_PATH = re.compile(
    r"(?<![A-Za-z0-9_/\\>])" + _FILE_PREFIX + _DEVICE_PREFIX + r"(?:\\{2})+"
    r"[^\s\\\"'`,;:)]+" + _SEPARATOR + _WINDOWS_TAIL_PLUS
)
# The lookbehind keeps URLs (`https://`) and relative paths out, and `:` too: a
# location spelled after a colon would otherwise swallow a URL authority
# (`https://x` must never start a match), and the colon-adjacent spelling has its
# own pattern below.
_POSIX_PATH = re.compile(r"(?<![A-Za-z0-9_:/\\>.])/(?:" + _POSIX_SEGMENT + r"/)*" + _POSIX_SEGMENT)
# A POSIX path spelled directly after a colon (`PATH=/usr/bin:/usr/local/bin`): the
# colon allowance the pattern above must not take, because the very next character
# decides between an absolute path and a URL authority - a URL's colon is followed
# by two separators, and this pattern requires exactly one.
_COLON_POSIX_PATH = re.compile(r"(?<=:)/(?!/)(?:" + _POSIX_SEGMENT + r"/)*" + _POSIX_SEGMENT)
# A POSIX path a JSON writer escaped (`\/etc\/hosts`): the escaped spelling of an
# absolute location, rendered in the default argv/output scope. The lookbehind
# keeps URL authorities (`https:\/\/tmp` - the pair after the colon, and the pair
# after a separator) and relative paths out, and every separator must be the
# escaped pairing, so an unescaped `/` cannot drift this pattern into a URL.
# A rendered root token is one more boundary: the root pass leaves an escaped
# descendant separator behind it (`C:\Work\run\/child` renders as
# `<disposable-root>\/child`), and that separator is the token's trailing
# structure, not the start of a new escaped location.
_ESCAPED_POSIX_PATH = re.compile(
    r"(?<!" + re.escape(DISPOSABLE_ROOT_TOKEN) + r")(?<![A-Za-z0-9_:\\/])"
    + r"(?:\\/)(?:" + _ESCAPED_SEGMENT + r"\\/)*" + _ESCAPED_SEGMENT
)
# A forward-slash authority path (`//server/share`), with each separator optionally
# escaped or a backslash (the mixed spellings are the same UNC location). The
# lookbehind keeps URLs (`https://`) out: their `//` follows a colon.
_SLASH_UNC_PATH = re.compile(
    r"(?<![A-Za-z0-9_:\\/])(?:/|\\/)(?:/|\\/)[^\s\\/\"'`,;:)]+" + _SEPARATOR + _WINDOWS_TAIL_PLUS
)
# The filesystem roots a host absolute path actually starts with. `/portal/runtimes`
# and any other product route stay literal in `--root-only` mode because their first
# segment is not one of these roots, and a bare root token with no following separator
# or segment (`/tmp` quoted in prose) stays literal too: it names no host-specific
# location. `HOST_PATH_PATTERNS` is the one shared definition: the evaluation runner's
# measured-value redaction imports it, so both tools agree on what a host path is.
HOST_ROOT_SEGMENTS = ("home", "Users", "root", "tmp", "var", "mnt", "private")
_HOST_ROOT = "(?:" + "|".join(HOST_ROOT_SEGMENTS) + ")"
# The segment class allows an empty segment, so a naive concatenation (`/tmp//run`)
# is still a host path; a preceding `:` is accepted for the same reason as above,
# while a preceding separator is not: that spelling is an authority path or a URL,
# handled by their own patterns, and it keeps `https://tmp/vendor/doc` literal.
_HOST_POSIX_PATH = re.compile(
    r"(?<![A-Za-z0-9_>./\\])(?:" + _HOST_SEP + r")" + _HOST_ROOT
    + r"(?:(?:" + _HOST_SEP + r")(?:" + _POSIX_SEGMENT + r")?)+"
)
# A root-relative Windows spelling (`\Users\op\run`): classic Windows semantics
# resolve a leading single backslash against the current drive root, so it names
# the same profile location as `C:\Users\op\run`. The leader is an odd backslash run
# (the raw and Markdown-prose spelling); the even runs are the JSON-escaped
# spellings the UNC patterns already cover.
_ROOT_RELATIVE_PATH = re.compile(
    r"(?<![A-Za-z0-9_>./\\])(?:\\{2})*\\" + _HOST_ROOT
    + r"(?:\\[^\\\s\"'`,;:)]*|" + _SPACE_CHUNK + r")+"
)
# A `file:` URL names an absolute location; without this, the drive or POSIX pattern's
# lookbehind (the URL's own slashes) would hide a host path inside one. After `file:`
# a Windows location may be spelled with backslashes (a single one before an
# authority is a legitimate file-URL form), so this pattern accepts them where the
# bare `_DRIVE_PATH`/`_UNC_PATH` leaders require two; the scheme is followed by any
# run of separators (concatenated escapes included), and the host-root arm does not
# require a segment after the root (`file:///tmp` names an absolute location too).
# An authority followed by a drive location (`file://localhost/C:/x`) is one
# location: the authority's own separator is not where the path ends.
_FILE_URL_PATH = re.compile(
    r"(?<![A-Za-z0-9_])" + _FILE_SCHEME + r"[\\/]*(?:"
    + _DEVICE_PREFIX + r"[A-Za-z]:[\\/]" + _WINDOWS_TAIL
    + r"|" + _DEVICE_PREFIX + r"\\{1,4}[^\s\\\"'`,;:)]+(?:\\|\\\\)[^\s\"'`,;:)]+"
    + _WINDOWS_TAIL
    + r"|" + _HOST_SEP + _HOST_ROOT + r"(?:(?:" + _HOST_SEP + r")(?:" + _POSIX_SEGMENT + r")?)*"
    + r"|(?://|\\/\\/)[^\s\\/\"'`,;:)]+(?:[\\/]|\\/)(?:[A-Za-z]:[\\/])?" + _WINDOWS_TAIL
    + r")"
)
# The root-only scope: every host shape above, and nothing that could be a product
# route. The default scope adds every absolute path the POSIX pattern cannot see,
# including a host-rooted one spelled after a colon, the JSON-escaped spelling of a
# POSIX path, and a `file:` URL.
HOST_PATH_PATTERNS = (
    _DRIVE_PATH, _UNC_PATH, _SLASH_UNC_PATH, _HOST_POSIX_PATH, _ROOT_RELATIVE_PATH, _FILE_URL_PATH,
)
# The host-rooted POSIX pattern runs before the general POSIX one: it carries the
# same space continuation, but a spelling only it recognises (a host root directly
# after the separator) must not be consumed piecewise by the general pattern first.
DEFAULT_PATH_PATTERNS = (
    _DRIVE_PATH, _UNC_PATH, _SLASH_UNC_PATH, _HOST_POSIX_PATH, _POSIX_PATH, _FILE_URL_PATH,
    _ROOT_RELATIVE_PATH, _ESCAPED_POSIX_PATH, _COLON_POSIX_PATH,
)


def hashed_path_token(path: str) -> str:
    """Return the stable hashed token for one absolute path."""

    return "path-sha256:" + hashlib.sha256(path.encode("utf-8")).hexdigest()


def _render_match(match: re.Match[str]) -> str:
    """Render one matched path, leaving a JSON ``\\"`` escape outside the token.

    In JSON-escaped text a matched path immediately before a recorded quote is
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
    render idempotent.
    """

    matched = match.group(0)
    if match.string[match.end():match.end() + 1] == '"':
        trailing = len(matched) - len(matched.rstrip("\\"))
        if trailing % 2 == 1:
            return hashed_path_token(matched[:-1]) + "\\"
    return hashed_path_token(matched)


def _root_patterns(root: str) -> tuple[re.Pattern[str], re.Pattern[str]] | None:
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
    # behind it (quadratic over `"/" * 100000`). A JSON escape in front of the
    # root is a boundary, not a name: the letter of a two-character escape
    # (`\n`, `\t`, `\r`, `\b`, `\f`) or the last hex digit of a `\uXXXX` escape
    # ends the escape sequence, so a root that starts the next recorded line
    # inside a quoted record (`{"out":"ok\n/srv/store/x"}`) still matches, while
    # a literal name character glued in front (`aa/srv/store/x`) still rejects
    # the match.
    lookbehind = r"(?:(?<=\\[nrtbf])|(?<=\\[u][0-9A-Fa-f]{4})|(?<![A-Za-z0-9_.\-\\/]))"
    after = r"(?!" + _ROOT_NAME_CHAR + r"|\." + _ROOT_NAME_CHAR + r"|" + _SPACE_CHUNK + r")"
    return (
        re.compile(lookbehind + body + after),
        re.compile(lookbehind + body + r"(?:" + _SPACE_CHUNK + r")+"),
    )


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
    """

    rendered = text
    for root in roots:
        matchers = _root_patterns(root)
        if matchers is None:
            continue
        bounded, chunked = matchers
        # A root followed by a space-continued chunk continues a longer sibling
        # name, so the bounded matcher declines it there; the chunked matcher
        # renders the whole span as a hashed token first: the span is not this
        # root, and leaving it literal would publish the root bytes on a POSIX
        # root the host-path patterns do not cover.
        rendered = chunked.sub(_render_match, rendered)
        rendered = bounded.sub(DISPOSABLE_ROOT_TOKEN, rendered)
    patterns = HOST_PATH_PATTERNS if root_only else DEFAULT_PATH_PATTERNS
    for pattern in patterns:
        rendered = pattern.sub(_render_match, rendered)
    return rendered


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
