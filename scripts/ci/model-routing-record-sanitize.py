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

Usage (record publishers apply this before writing a record)::

    python scripts/ci/model-routing-record-sanitize.py \
        --root "$DISPOSABLE_ROOT" < recorded-output.txt

The CLI renders files (or standard input) to standard output. With ``--check``
it writes nothing and exits 1 when the input would change, so a publishing
script can gate on "no host path is present".
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
_DRIVE_PATH = re.compile(r"(?<![A-Za-z0-9_:/\\>])[A-Za-z]:[\\/][^\s\"'`,;:)]*")
_UNC_PATH = re.compile(r"(?<![A-Za-z0-9_:/\\>])\\\\[^\s\\\"'`,;:)]+\\[^\s\"'`,;:)]+")
_POSIX_PATH = re.compile(r"(?<![A-Za-z0-9_:/\\>.])/(?:[^/\s\"'`,;:)]+/)*[^\s\"'`,;:)]+")


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


def sanitize_text(text: str, roots: list[str]) -> str:
    """Render host-specific absolute paths in ``text`` as tokens.

    ``roots`` are disposable coordinator roots (absolute paths) to render as
    ``<disposable-root>``; every other absolute path is rendered as a hashed
    token. The function is idempotent: rendering rendered text changes nothing.
    """

    rendered = text
    for root in roots:
        for variant in _root_variants(root):
            rendered = rendered.replace(variant, DISPOSABLE_ROOT_TOKEN)
    for pattern in (_DRIVE_PATH, _UNC_PATH, _POSIX_PATH):
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
    rendered = sanitize_text(text, arguments.root)
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
