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
rendering is idempotent.

With ``--root-only`` the renderer flags only host absolute paths: Windows drive
and UNC roots and the POSIX roots a host path actually starts under (``/home/``,
``/Users/``, ``/root/``, ``/tmp/``, ``/var/``, ``/mnt/``, ``/private/`` - the
shapes the evaluation runner's host-path redaction and the committed records
name), plus every ``--root`` disposable root. A product route such as
``/portal/runtimes`` is not a host path and stays literal in this mode, and so
does a bare root token with no following separator or segment (``/tmp`` quoted
in prose), which names no host-specific location; the default mode keeps
rendering every absolute path, which is its documented boundary for argv/output
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
# One forward slash, or its JSON-escaped spelling (`\/`): a record quoted into a JSON
# string carries both forms, and the checker must not miss a host path because the
# writer escaped it.
_HOST_SEP = r"(?:/|\\/)"
_DRIVE_PATH = re.compile(r"(?<![A-Za-z0-9_:/\\>])[A-Za-z]:[\\/][^\s\"'`,;:)]*")
# A UNC path whose separators are backslashes, each written once or doubled by JSON
# escaping: `\\server\share` and `{"p":"\\\\server\\share"}` are the same location.
_UNC_PATH = re.compile(
    r"(?<![A-Za-z0-9_:/\\>])\\\\(?:\\\\)?"
    r"[^\s\\\"'`,;:)]+(?:\\|\\\\)[^\s\"'`,;:)]+"
)
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
_HOST_POSIX_PATH = re.compile(
    r"(?<![A-Za-z0-9_:>.])(?:" + _HOST_SEP + r")" + _HOST_ROOT
    + r"(?:(?:" + _HOST_SEP + r")[^/\s\"'`,;:)]+)+(?:" + _HOST_SEP + r")?"
)
# A `file:` URL names an absolute location; without this, the drive or POSIX pattern's
# lookbehind (the URL's own slashes) would hide a host path inside one.
_FILE_URL_PATH = re.compile(
    r"(?<![A-Za-z0-9_])file:(?:[\\/]{1,3})?"
    r"(?:[A-Za-z]:[\\/]|(?:" + _HOST_SEP + r")" + _HOST_ROOT + r"(?:" + _HOST_SEP + r")"
    r"|(?://|\\/\\/)[^\s\\/\"'`,;:)]+(?:[\\/]|\\/))"
    r"[^\s\"'`,;:)]*"
)
# The root-only scope: every host shape above, and nothing that could be a product
# route. The default scope stays `(_DRIVE_PATH, _UNC_PATH, _POSIX_PATH)`.
HOST_PATH_PATTERNS = (_DRIVE_PATH, _UNC_PATH, _SLASH_UNC_PATH, _HOST_POSIX_PATH, _FILE_URL_PATH)


def hashed_path_token(path: str) -> str:
    """Return the stable hashed token for one absolute path."""

    return "path-sha256:" + hashlib.sha256(path.encode("utf-8")).hexdigest()


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
    patterns = HOST_PATH_PATTERNS if root_only else (_DRIVE_PATH, _UNC_PATH, _POSIX_PATH)
    for pattern in patterns:
        rendered = pattern.sub(lambda match: hashed_path_token(match.group(0)), rendered)
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
    if arguments.file == "-":
        text = sys.stdin.buffer.read().decode("utf-8", errors="replace")
    else:
        path = pathlib.Path(arguments.file)
        if not path.is_file():
            print(f"model-routing-record-sanitize refused: {path} is not a readable file", file=sys.stderr)
            return 2
        # newline="" keeps the recorded line endings byte-faithful: a record's
        # CRLF/LF convention must survive rendering unchanged.
        with path.open(encoding="utf-8", errors="replace", newline="") as handle:
            text = handle.read()
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
