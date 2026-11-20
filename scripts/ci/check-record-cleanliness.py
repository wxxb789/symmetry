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
``before`` commit for a push. The paths are selected from that range but their bytes
are read from the working tree, so the checkout must be at ``--head`` (CI checks the
pushed commit out): a ``--repo``/``--head`` pair whose working tree sits elsewhere
checks one revision's selection against another revision's bytes. An empty or all-zero
base - a new branch, a tag push, a manual run - names no earlier revision of this
branch and falls back to the merge base with ``origin/main`` and then to ``HEAD^``;
when that range is empty it reports ``nothing to check`` and passes, because the
revision was compared with itself by construction, not by omission. A base that was
requested and is not present in this checkout (a force-push's ``before``) takes the
same fallback with a hint, but a fallback that would compare the head with itself
refuses: the rewritten history now sits on ``origin/main``, and a vacuous pass would
hide the records such a push adds.
The range is then narrowed to ``git merge-base base head`` when one exists, so commits
that belong to the base branch are not attributed to this change. Only a range that
cannot be compared at all, or a requested base that cannot be, refuses. Unchanged
records are deliberately not checked (the pre-existing 0008-series records are tracked
by their own TODO entries), and deleted records have nothing to check.

Usage::

    python scripts/ci/check-record-cleanliness.py --base <commit> [--head <commit>]

Exit codes: 0 when every selected record passes (also when the range changes no
record), 1 when a selected record still carries a host absolute path or its bytes are
not text the sanitize tool can check, 2 when the revision range or the sanitize tool
cannot be used.
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


class ComparisonBaseRefused(Exception):
    """A requested comparison base that cannot yield a comparable range."""


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
    """The commit to compare ``head`` against, or None when none can be resolved.

    The fallback rules are the module docstring's; the shape only this function owns
    is its refusal: a fallback that would compare the head with itself raises
    ``ComparisonBaseRefused`` instead of returning a base.
    """
    requested_unreachable = False
    if requested and requested != ZERO_COMMIT:
        resolved = resolve_commit(repository, requested)
        if resolved is not None:
            return resolved
        requested_unreachable = True
        print(
            f"check-record-cleanliness: base {requested} is not present in this checkout "
            "(a force-push?); comparing against the origin/main merge base instead",
            file=sys.stderr,
        )
    merge_base = run_git(repository, "merge-base", head, "origin/main")
    if merge_base.returncode == 0:
        resolved = merge_base.stdout.decode("ascii", "replace").strip()
        if requested_unreachable and resolved == head:
            raise ComparisonBaseRefused(
                f"the requested base {requested} is not present in this checkout and the "
                "origin/main fallback is the head itself, so the range would be empty; a "
                "force-push rewrites history, so name the revision this change is to be "
                "compared against"
            )
        return resolved
    first_parent = resolve_commit(repository, f"{head}^")
    return first_parent


def narrow_to_merge_base(repository: pathlib.Path, base: str, head: str) -> str:
    """The merge base of base and head when one exists, so base-branch commits stay out."""
    merge_base = run_git(repository, "merge-base", base, head)
    if merge_base.returncode != 0:
        return base
    return merge_base.stdout.decode("ascii", "replace").strip() or base


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


def check_record(repository: pathlib.Path, relative: str) -> tuple[str, str]:
    """Run the sanitize tool's own root-only check over one record.

    Returns ``(status, detail)``: ``pass`` when the tool exits 0, ``dirty`` when it
    exits 1 (a host absolute path remains) and ``unreadable`` when it exits 2 (the
    bytes are not text the tool can check).
    """
    completed = subprocess.run(
        [sys.executable, str(SANITIZE_TOOL), "--check", "--root-only", relative],
        capture_output=True, cwd=str(repository), check=False,
    )
    detail = (completed.stderr + completed.stdout).decode("utf-8", "replace").strip()
    if completed.returncode == 0:
        return "pass", detail
    if completed.returncode == 1:
        return "dirty", detail
    return "unreadable", detail


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
    try:
        base = resolve_base(repository, arguments.base, head)
    except ComparisonBaseRefused as refusal:
        print(f"check-record-cleanliness refused: {refusal}", file=sys.stderr)
        return 2
    if base is None:
        print(
            "check-record-cleanliness refused: cannot resolve a comparison base "
            f"({arguments.base!r} against origin/main or HEAD^)",
            file=sys.stderr,
        )
        return 2

    base = narrow_to_merge_base(repository, base, head)
    records = changed_records(repository, base, head)
    if not records:
        print(f"check-record-cleanliness: no record added or modified between {base[:12]} and {head[:12]}; nothing to check")
        return 0
    dirty: list[tuple[str, str]] = []
    unreadable: list[tuple[str, str]] = []
    for relative in records:
        status, detail = check_record(repository, relative)
        if status == "dirty":
            dirty.append((relative, detail))
        elif status == "unreadable":
            unreadable.append((relative, detail))
    if dirty or unreadable:
        for relative, detail in dirty + unreadable:
            print(f"check-record-cleanliness refused: {relative}: {detail}", file=sys.stderr)
        if dirty:
            print(
                f"check-record-cleanliness refused: {len(dirty)} of {len(records)} changed records "
                "still carry a host absolute path; render argv/output capture with "
                f"{SANITIZE_TOOL.name} (its default scope) and keep prose free of host paths "
                "instead of publishing the path",
                file=sys.stderr,
            )
        if unreadable:
            print(
                f"check-record-cleanliness refused: {len(unreadable)} of {len(records)} changed "
                "records could not be checked at all: the sanitize tool refused their bytes "
                "(exit 2, not recorded text), so this gate cannot say whether they name a host "
                "path",
                file=sys.stderr,
            )
        return 1
    print(f"check-record-cleanliness: {len(records)} changed record(s) pass the root-only check")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
