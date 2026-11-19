#!/usr/bin/env python3
"""Refuse host paths in evaluation records a change adds or modifies.

This is the committed-record cleanliness gate: for a revision range it selects every
file added or modified under ``docs/evaluations/**`` and ``docs/evidence/**`` and runs
``model-routing-record-sanitize.py --check --root-only`` on each one. The guard never
re-implements the renderer's patterns - it executes the tool, so what the gate
constrains is exactly what the sanitize tool's own check parses. Product routes such
as ``/portal/runtimes`` stay literal in that mode; a Windows drive or UNC path, a
POSIX ``/home``, ``/Users``, ``/root``, ``/tmp``, ``/var``, ``/mnt`` or ``/private``
path, or a ``--root`` disposable root still refuses.

Changed-file selection is computed against an explicit comparison base, never from
the checkout's last commit: CI passes the pull-request base commit, or the pushed
``before`` commit for a push. An empty or all-zero base - a new branch, a tag, a
manual run - falls back to the merge base with ``origin/main`` and then to ``HEAD^``;
a base that cannot be resolved at all is a refusal, so an unscoped run can never
read as a pass. Unchanged records are deliberately not checked (the pre-existing
0008-series records are tracked by their own TODO entries), and deleted records have
nothing to check.

Usage::

    python scripts/ci/check-record-cleanliness.py --base <commit> [--head <commit>]

Exit codes: 0 when every selected record passes (also when the range changes no
record), 1 when a selected record still carries a host absolute path, 2 when the
revision range or the sanitize tool cannot be used.
"""

from __future__ import annotations

import argparse
import pathlib
import subprocess
import sys

SCRIPT_DIRECTORY = pathlib.Path(__file__).resolve().parent
SANITIZE_TOOL = SCRIPT_DIRECTORY / "model-routing-record-sanitize.py"
RECORD_PATHS = ("docs/evaluations", "docs/evidence")
ZERO_COMMIT = "0" * 40
# A record is selected when the diff reports it as added, modified or type-changed:
# each case leaves a file at the head revision whose text this gate must parse.
SELECTED_STATUSES = {"A", "M", "T"}


def run_git(repository: pathlib.Path, *arguments: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", "-C", str(repository), *arguments], capture_output=True, check=False,
    )


def resolve_commit(repository: pathlib.Path, revision: str) -> str | None:
    completed = run_git(repository, "rev-parse", "--verify", "--quiet", f"{revision}^{{commit}}")
    if completed.returncode != 0:
        return None
    return completed.stdout.decode("ascii", "replace").strip()


def resolve_base(repository: pathlib.Path, requested: str | None, head: str) -> str | None:
    """The commit to compare ``head`` against, or None when none can be resolved."""
    if requested and requested != ZERO_COMMIT:
        return resolve_commit(repository, requested)
    merge_base = run_git(repository, "merge-base", head, "origin/main")
    if merge_base.returncode == 0:
        return merge_base.stdout.decode("ascii", "replace").strip()
    first_parent = resolve_commit(repository, f"{head}^")
    return first_parent


def changed_records(repository: pathlib.Path, base: str, head: str) -> list[str]:
    """Records added or modified between ``base`` and ``head``, sorted."""
    completed = run_git(
        repository, "diff", "--name-status", "--no-renames", "-z", base, head, "--", *RECORD_PATHS,
    )
    if completed.returncode != 0:
        raise SystemExit(
            "check-record-cleanliness refused: cannot diff "
            f"{base}..{head}: {completed.stderr.decode('utf-8', 'replace').strip()}"
        )
    fields = [field.decode("utf-8", "surrogateescape") for field in completed.stdout.split(b"\0") if field]
    if len(fields) % 2 != 0:
        raise SystemExit("check-record-cleanliness refused: unexpected git diff output shape")
    selected = [fields[index + 1] for index in range(0, len(fields), 2) if fields[index] in SELECTED_STATUSES]
    return sorted(selected)


def check_record(repository: pathlib.Path, relative: str) -> tuple[bool, str]:
    """Run the sanitize tool's own root-only check over one record."""
    completed = subprocess.run(
        [sys.executable, str(SANITIZE_TOOL), "--check", "--root-only", relative],
        capture_output=True, cwd=str(repository), check=False,
    )
    detail = (completed.stderr + completed.stdout).decode("utf-8", "replace").strip()
    return completed.returncode == 0, detail


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Run the record sanitize tool's root-only check over records a revision adds or "
                    "modifies.",
    )
    parser.add_argument("--repo", default=".", metavar="PATH", help="repository path (default: .)")
    parser.add_argument("--base", default=None, metavar="COMMIT",
                        help="comparison base; empty or all-zero falls back to origin/main, then HEAD^")
    parser.add_argument("--head", default="HEAD", metavar="COMMIT", help="head revision (default: HEAD)")
    arguments = parser.parse_args(argv)

    repository = pathlib.Path(arguments.repo).resolve()
    if not (repository / ".git").exists():
        print(f"check-record-cleanliness refused: {repository} is not a repository", file=sys.stderr)
        return 2
    if not SANITIZE_TOOL.is_file():
        print(f"check-record-cleanliness refused: {SANITIZE_TOOL} is missing", file=sys.stderr)
        return 2
    head = resolve_commit(repository, arguments.head)
    if head is None:
        print(f"check-record-cleanliness refused: cannot resolve the head {arguments.head!r}", file=sys.stderr)
        return 2
    base = resolve_base(repository, arguments.base, head)
    if base is None:
        print(
            "check-record-cleanliness refused: cannot resolve a comparison base "
            f"({arguments.base!r} against origin/main or HEAD^)",
            file=sys.stderr,
        )
        return 2

    records = changed_records(repository, base, head)
    if not records:
        print(f"check-record-cleanliness: no record added or modified between {base[:12]} and {head[:12]}; nothing to check")
        return 0
    failures: list[tuple[str, str]] = []
    for relative in records:
        passed, detail = check_record(repository, relative)
        if not passed:
            failures.append((relative, detail))
    if failures:
        for relative, detail in failures:
            print(f"check-record-cleanliness refused: {relative}: {detail}", file=sys.stderr)
        print(
            f"check-record-cleanliness refused: {len(failures)} of {len(records)} changed records "
            "still carry a host absolute path; render argv/output capture with "
            f"{SANITIZE_TOOL.name} --root-only or keep prose free of host paths",
            file=sys.stderr,
        )
        return 1
    print(f"check-record-cleanliness: {len(records)} changed record(s) pass the root-only check")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
