#!/usr/bin/env python3
"""Bind Linux containment fixture builds to a clean, exact source commit."""

from __future__ import annotations

import hashlib
import json
import subprocess
import sys
from typing import TypedDict


class SourceSubject(TypedDict):
    commit: str
    tree_digest: str


def source_subject(repository: str, expected_commit: str) -> SourceSubject:
    command = ["git", "-c", f"safe.directory={repository}", "-C", repository]

    def git(*arguments: str) -> bytes:
        return subprocess.check_output([*command, *arguments], stderr=subprocess.PIPE)

    commit = git("rev-parse", "HEAD").decode("ascii").strip()
    if commit != expected_commit:
        raise SystemExit("source checkout does not match the exact expected commit")
    if git("status", "--porcelain=v1", "--untracked-files=all"):
        raise SystemExit("source checkout has staged, unstaged, or untracked changes")

    records = git("ls-tree", "--full-tree", "-r", "-z", commit).split(b"\0")
    tree_hash = hashlib.sha256()
    with subprocess.Popen(
        [*command, "cat-file", "--batch"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    ) as batch:
        if batch.stdin is None or batch.stdout is None:
            raise SystemExit("Git batch pipes are unavailable")
        for record in sorted(filter(None, records), key=lambda value: value.split(b"\t", 1)[1]):
            header, path = record.split(b"\t", 1)
            mode, kind, object_id = header.split(b" ", 2)
            if kind != b"blob":
                raise SystemExit("source tree contains a non-blob entry")
            batch.stdin.write(object_id + b"\n")
            batch.stdin.flush()
            object_header = batch.stdout.readline().split()
            if len(object_header) != 3 or object_header[1] != b"blob":
                raise SystemExit("Git returned an invalid source object")
            size = int(object_header[2])
            blob = batch.stdout.read(size)
            if len(blob) != size or batch.stdout.read(1) != b"\n":
                raise SystemExit("Git returned a truncated source blob")
            blob_digest = b"sha256:" + hashlib.sha256(blob).hexdigest().encode("ascii")
            for field in (mode, b"\0", kind, b"\0", path, b"\0", blob_digest, b"\0"):
                tree_hash.update(field)
        batch.stdin.close()
        if batch.wait() != 0:
            raise SystemExit("Git could not finish reading the source tree")

    # A checkout change during hashing must not produce a successful receipt.
    if git("rev-parse", "HEAD").decode("ascii").strip() != commit or git(
        "status", "--porcelain=v1", "--untracked-files=all"
    ):
        raise SystemExit("source checkout changed while binding its subject")
    return {"commit": commit, "tree_digest": "sha256:" + tree_hash.hexdigest()}


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
