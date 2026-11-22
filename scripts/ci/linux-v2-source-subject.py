#!/usr/bin/env python3
"""Bind Linux containment fixture builds to a clean, exact source commit."""

from __future__ import annotations

import json
import subprocess
import sys

from source_subject import Refused, SourceSubject, index_mode_digest

REFUSALS = {
    "head_mismatch": "source checkout does not match the exact expected commit",
    "dirty_checkout": "source checkout has staged, unstaged, or untracked changes",
    "checkout_changed": "source checkout changed while binding its subject",
    "tree_entry_not_a_file": "source tree contains a non-blob entry",
    "invalid_tree_record": "Git returned an invalid source object",
    "invalid_source_object": "Git returned an invalid source object",
    "truncated_source_blob": "Git returned a truncated source blob",
    "batch_unavailable": "Git batch pipes are unavailable",
    "batch_failed": "Git could not finish reading the source tree",
}


def source_subject(repository: str, expected_commit: str) -> SourceSubject:
    try:
        return index_mode_digest(repository, expected_commit)
    except Refused as refusal:
        raise SystemExit(REFUSALS[refusal.code]) from refusal


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: linux-v2-source-subject.py REPOSITORY COMMIT", file=sys.stderr)
        return 2
    try:
        subject = source_subject(sys.argv[1], sys.argv[2])
    except (OSError, subprocess.CalledProcessError, UnicodeError, ValueError):
        print("could not read the exact source checkout", file=sys.stderr)
        return 1
    print(json.dumps(subject, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
