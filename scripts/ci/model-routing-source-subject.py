#!/usr/bin/env python3
"""Recompute the source subject the 0008 model-routing records quote.

A record may only quote a subject its reader can re-derive from the tree in front of
them, so this tool is the delivered definition of that computation: the declared scope,
the entry grammar and the digest are ``source_subject.working_tree_digest``'s, and this
CLI prints them.

    source_paths: <declared path count>
    source_subject: sha256:<digest over the entries sorted by their UTF-8 path bytes>
    executable_paths: <comma-joined paths whose execute bit is set, or `none`>
    delivered <path>: sha256:<file digest>          each bound evaluation tool file

Usage::

    python scripts/ci/model-routing-source-subject.py [REPOSITORY]

The repository defaults to the tree this script is delivered in, and the `delivered`
lines are the tooling digests the 0008 records bind: the evaluation entry, the modules
it imports and its suite, so one command re-derives a whole subject row.
"""

from __future__ import annotations

import pathlib
import sys

from source_subject import (
    file_digest,
    working_tree_digest,
    working_tree_entry,
    working_tree_executable_paths,
    working_tree_paths,
)

DELIVERED_TOOL_PATHS = (
    "scripts/ci/model-routing-evaluation.py",
    "scripts/ci/model_routing_gates.py",
    "scripts/ci/model_routing_receipts.py",
    "scripts/ci/model_routing_records.py",
    "scripts/ci/model_routing_subject.py",
    "scripts/ci/test-model-routing-evaluation.py",
)

entry_bytes = working_tree_entry
executable_paths = working_tree_executable_paths
source_paths = working_tree_paths
source_subject = working_tree_digest


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
        delivered = {relative: file_digest(root / relative) for relative in DELIVERED_TOOL_PATHS}
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
