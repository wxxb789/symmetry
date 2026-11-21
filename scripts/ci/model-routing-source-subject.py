#!/usr/bin/env python3
"""Recompute the source subject the 0008 model-routing records quote.

A record may only quote a subject its reader can re-derive from the tree in front of
them, so this tool is the delivered definition of that computation. It walks the
declared scope over a working tree and prints the digest:

* paths - every regular file in the working tree, excluding `.git` in either form (a
  checkout directory or a worktree pointer file), everything under `docs/`, `.tmp/`,
  the declared build and report output directories (`dist`, `playwright-report`,
  `test-results`), `__pycache__` bytecode caches, the whole `node_modules` tree, and the
  tool caches (`.vite`, `.vite-temp`, `.vitest-cache`) wherever else they appear. Why
  these are declared rather than discovered: a provisioned dependency tree, a generated
  bytecode cache, a worktree pointer file, a build output or a package manager's run
  state each would change the digest without any delivered source changing, so they must
  not enter a source binding. This is a wider exclusion than the sealed manifest's, which
  keeps package files bound inside a checkout whose dependency tree it must police: the
  subject binds delivered source, the manifest binds a measured checkout;
* entry - `<mode>\\0sha256:<hex of the file bytes>\\0<path-utf8>\\0`, with mode `100755`
  when an execute bit (owner, group or other) is set and `100644` otherwise;
* digest - sha256 over the sorted entries, ordered by the UTF-8 bytes of the path.
  Directories are never entries and a symlinked file is skipped, so the binding is over
  bytes the tree itself carries.

Reparse points follow `pathlib`: a symlinked file is never an entry and `**` does not
descend a POSIX directory symlink, while a Windows junction is not reported as a symlink
and is descended - so a tree carrying an in-scope junction binds bytes that live outside
it. The delivered tree carries no in-scope reparse point, and the tool's scope is
otherwise the coordinator tool's own, which is what keeps the recorded values comparable.

The mode follows the measured tree's own execute bits: any of the owner, group or other bits
sets `100755`, so a checkout that carries no executable bit (a Windows worktree) hashes those
entries `100644` while a POSIX checkout of the same commit hashes the index's `100755` files
`100755`. The printed
`executable_paths` line names every path whose bit is set in the tree being measured, so
a reader on another platform sees exactly which entries - and only those - explain a
digest that differs from a recorded one.

Usage::

    python scripts/ci/model-routing-source-subject.py [REPOSITORY]

The repository defaults to the tree this script is delivered in. The output also names
the digests of the two files the 0008 records bind as delivered tooling, so one command
re-derives a whole subject row.
"""

from __future__ import annotations

import hashlib
import pathlib
import sys

EXCLUDED_DIRECTORIES = {
    ".git",
    "__pycache__",
    ".tmp",
    "node_modules",
    "dist",
    "playwright-report",
    "test-results",
}
EXCLUDED_CACHE_NAMES = {".vite", ".vite-temp", ".vitest-cache"}
EXCLUDED_ROOTS = {".git", "docs"}
DELIVERED_TOOL_PATHS = (
    "scripts/ci/model-routing-evaluation.py",
    "scripts/ci/test-model-routing-evaluation.py",
)


def source_paths(root: pathlib.Path) -> list[str]:
    """Return the declared source paths of `root`, sorted by their UTF-8 bytes.

    Only the parent directories of a file decide exclusion, so a directory named like a
    cache is skipped whole while a file that merely shares the name is still an entry.
    """
    paths = []
    for path in root.rglob("*"):
        parts = path.relative_to(root).parts
        if not parts or parts[0] in EXCLUDED_ROOTS:
            continue
        if any(part in EXCLUDED_DIRECTORIES or part in EXCLUDED_CACHE_NAMES for part in parts[:-1]):
            continue
        if path.is_file() and not path.is_symlink():
            paths.append(path.relative_to(root).as_posix())
    return sorted(paths, key=lambda value: value.encode("utf-8"))


def entry_bytes(root: pathlib.Path, relative: str) -> bytes:
    """Return the declared digest entry for one source path."""
    target = root / relative
    mode = "100755" if target.stat().st_mode & 0o111 else "100644"
    blob = hashlib.sha256(target.read_bytes()).hexdigest()
    return mode.encode("ascii") + b"\0" + f"sha256:{blob}".encode("ascii") + b"\0" + relative.encode("utf-8") + b"\0"


def source_subject(root: pathlib.Path) -> tuple[int, str]:
    """Return `(path count, subject digest)` for the working tree at `root`."""
    paths = source_paths(root)
    digest = hashlib.sha256()
    for relative in paths:
        digest.update(entry_bytes(root, relative))
    return len(paths), "sha256:" + digest.hexdigest()


def executable_paths(root: pathlib.Path) -> list[str]:
    """Return the declared source paths whose execute bit (any of owner, group or other) is set."""
    return [relative for relative in source_paths(root) if (root / relative).stat().st_mode & 0o111]


def main(argv: list[str]) -> int:
    if len(argv) > 1:
        print("usage: model-routing-source-subject.py [REPOSITORY]", file=sys.stderr)
        return 2
    root = pathlib.Path(argv[0]).resolve() if argv else pathlib.Path(__file__).resolve().parents[2]
    if not root.is_dir():
        print(f"model-routing-source-subject refused: {root} is not a directory", file=sys.stderr)
        return 2
    try:
        count, subject = source_subject(root)
        executable = executable_paths(root)
        delivered = {
            relative: "sha256:" + hashlib.sha256((root / relative).read_bytes()).hexdigest()
            for relative in DELIVERED_TOOL_PATHS
        }
    except OSError as failure:
        print(f"model-routing-source-subject refused: could not read the tree: {failure}", file=sys.stderr)
        return 1
    print(f"source_paths: {count}")
    print(f"source_subject: {subject}")
    print("executable_paths: " + (", ".join(executable) if executable else "none"))
    for relative, digest in delivered.items():
        print(f"delivered {relative}: {digest}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
