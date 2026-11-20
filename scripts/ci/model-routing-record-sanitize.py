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
and UNC roots and the POSIX roots a host path actually starts under (``/home/``,
``/Users/``, ``/root/``, ``/tmp/``, ``/var/``, ``/mnt/``, ``/private/`` - the
shapes the evaluation runner's host-path redaction and the committed records
name), plus every ``--root`` disposable root. A product route such as
``/portal/runtimes`` is not a host path and stays literal in this mode - in its
single-slash spelling only: spelled behind an authority (``//portal/...``) or
as a ``file:`` URL it is an absolute location like any other, and this mode
renders (and the gate refuses) it. A bare root token with no following
separator or segment (``/tmp`` quoted in prose) stays literal too: it names no
host-specific location. The default mode keeps rendering every absolute path,
which is its documented boundary for argv/output capture.

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
# One forward slash, or its JSON-escaped spelling (`\/`): a record quoted into a JSON
# string carries both forms, and the checker must not miss a host path because the
# writer escaped it.
_HOST_SEP = r"(?:/|\\/)"
# The prefixes a Windows location may carry: a `file:` URL scheme (with any number
# of separators after it) and a device namespace (`\\?\` or the `\\.\` device form,
# whose backslashes a JSON writer doubles). The lookbehinds accept a preceding `:`
# so a location spelled after a key (`root:C:\x`, `root:/home/x`) is still a
# location, while a preceding letter, digit, underscore, separator or rendered token
# keeps relative paths and URLs out.
_FILE_PREFIX = r"(?:file:[\\/]{0,4})?"
_DEVICE_PREFIX = r"(?:\\{1,4}[?.]\\{1,4})?"
_DRIVE_PATH = re.compile(
    r"(?<![A-Za-z0-9_/\\>])" + _FILE_PREFIX + _DEVICE_PREFIX + r"[A-Za-z]:[\\/][^\s\"'`,;:)]*"
)
# A UNC path whose separators are backslashes, each written once or escaped by JSON
# quoting at any nesting level (an even run of two or more): `\\server\share` and
# `{"p":"\\\\server\\share"}` are the same location, and so is the doubly escaped
# spelling of either separator.
_UNC_PATH = re.compile(
    r"(?<![A-Za-z0-9_/\\>])" + _FILE_PREFIX + _DEVICE_PREFIX + r"(?:\\{2})+"
    r"[^\s\\\"'`,;:)]+(?:\\|(?:\\{2})+)[^\s\"'`,;:)]+"
)
# The lookbehind keeps URLs (`https://`) and relative paths out; a host-rooted form
# spelled after a colon (`root:/home/x`) is caught by `_HOST_POSIX_PATH` instead,
# which cannot collide with a URL authority.
_POSIX_PATH = re.compile(r"(?<![A-Za-z0-9_:/\\>.])/(?:[^/\s\"'`,;:)]+/)*[^\s\"'`,;:)]+")
# A forward-slash authority path (`//server/share`), with each separator optionally
# escaped. The lookbehind keeps URLs (`https://`) out: their `//` follows a colon.
_SLASH_UNC_PATH = re.compile(
    r"(?<![A-Za-z0-9_:\\/])(?:/|\\/)(?:/|\\/)[^\s\\/\"'`,;:)]+(?:/|\\/)[^\s\"'`,;:)]+"
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
# is still a host path; a preceding `:` is accepted for the same reason as above.
_HOST_POSIX_PATH = re.compile(
    r"(?<![A-Za-z0-9_>.])(?:" + _HOST_SEP + r")" + _HOST_ROOT
    + r"(?:(?:" + _HOST_SEP + r")[^/\s\"'`,;:)]*)+"
)
# A `file:` URL names an absolute location; without this, the drive or POSIX pattern's
# lookbehind (the URL's own slashes) would hide a host path inside one. After `file:`
# a Windows location may be spelled with backslashes (a single one before an
# authority is a legitimate file-URL form), so this pattern accepts them where the
# bare `_DRIVE_PATH`/`_UNC_PATH` leaders require two.
_FILE_URL_PATH = re.compile(
    r"(?<![A-Za-z0-9_])file:[\\/]{0,4}(?:"
    + _DEVICE_PREFIX + r"[A-Za-z]:[\\/]"
    r"|" + _DEVICE_PREFIX + r"\\{1,4}[^\s\\\"'`,;:)]+(?:\\|\\\\)[^\s\"'`,;:)]+"
    r"|(?:" + _HOST_SEP + r")" + _HOST_ROOT + r"(?:" + _HOST_SEP + r")"
    r"|(?://|\\/\\/)[^\s\\/\"'`,;:)]+(?:[\\/]|\\/))"
    r"[^\s\"'`,;:)]*"
)
# The root-only scope: every host shape above, and nothing that could be a product
# route. The default scope adds every absolute path the POSIX pattern cannot see,
# including a host-rooted one spelled after a colon and a `file:` URL.
HOST_PATH_PATTERNS = (_DRIVE_PATH, _UNC_PATH, _SLASH_UNC_PATH, _HOST_POSIX_PATH, _FILE_URL_PATH)
DEFAULT_PATH_PATTERNS = (_DRIVE_PATH, _UNC_PATH, _POSIX_PATH, _FILE_URL_PATH, _HOST_POSIX_PATH)


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


def _root_variants(root: str) -> list[str]:
    """Return the separator variants a root may appear with in recorded text."""

    variants = {root}
    if "\\" in root:
        variants.add(root.replace("\\", "/"))
    if "/" in root:
        variants.add(root.replace("/", "\\"))
    return sorted(variants, key=len, reverse=True)


def sanitize_text(text: str, roots: list[str], root_only: bool = False) -> str:
    """Render host-specific absolute paths in ``text`` as tokens.

    ``roots`` are disposable coordinator roots (absolute paths) to render as
    ``<disposable-root>``. With ``root_only`` false every other absolute path is
    rendered as a hashed token; with ``root_only`` true only host absolute paths
    (drive, UNC and the POSIX home/root/temp/mount roots) are rendered, so a
    product route such as ``/portal/runtimes`` stays literal. The function is
    idempotent: rendering rendered text changes nothing.
    """

    rendered = text
    for root in roots:
        for variant in _root_variants(root):
            rendered = rendered.replace(variant, DISPOSABLE_ROOT_TOKEN)
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
        raw = path.read_bytes()
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
            print("model-routing-record-sanitize refused: a host absolute path is present", file=sys.stderr)
            return 1
        return 0
    sys.stdout.buffer.write(rendered.encode("utf-8"))
    sys.stdout.buffer.flush()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
