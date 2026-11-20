#!/usr/bin/env python3
"""Refuse host paths in evaluation records a change adds or modifies.

This is the committed-record cleanliness gate: for a revision range it selects every
file added or modified under ``docs/evaluations/**`` and ``docs/evidence/**`` and runs
``model-routing-record-sanitize.py --check --root-only`` on each one. The guard never
re-implements the renderer's patterns - it executes the tool, so what the gate
constrains is exactly what the sanitize tool's own check parses. Product routes such
as ``/portal/runtimes`` stay literal in that mode; a Windows drive or UNC path, a
POSIX ``/home``, ``/Users``, ``/root``, ``/tmp``, ``/var``, ``/mnt`` or ``/private``
path still refuses. A disposable root is rendered only when a ``--root`` argument
names it, and this gate passes none, so a store outside those shapes (a ``/srv/...``
root, say) stays literal here - by the tool's own boundary, not by omission.

Changed-file selection is computed against an explicit comparison base, never from
the checkout's last commit: CI passes the pull-request base commit, or the pushed
``before`` commit for a push. The selected paths' bytes are read from the ``--head``
revision's Git blob - never from the working tree - so the checkout need not be at
``--head`` and a worktree file cannot substitute bytes for the revision under test;
a record whose head entry is not a regular file blob (a symlink storing its target
text, or a gitlink) is refused. An empty or all-zero base - a new branch, a tag push,
a manual run - names no earlier revision of this branch and falls back to the merge
base with ``origin/main`` and then to ``HEAD^``; when that range is empty it reports
``nothing to check`` and passes, because the revision was compared with itself by
construction, not by omission. A base that was requested and is not present in this
checkout (a force-push's ``before``) takes the same fallback with a hint, but a
fallback that would compare the head with itself refuses: the rewritten history now
sits on ``origin/main``, and a vacuous pass would hide the records such a push adds.
A requested base that resolves is compared directly (``git diff base head``), so a
non-fast-forward push that reintroduces an earlier record is still checked instead of
being narrowed to an empty range; only the fallback base - already a merge base with
``origin/main``, or ``HEAD^`` - is narrowed to its merge base with the head. Only a
range that cannot be compared at all, or a requested base that cannot be, refuses.
Unchanged records are deliberately not checked (the pre-existing 0008-series records
are tracked by their own TODO entries), and deleted records have nothing to check.

Usage::

    python scripts/ci/check-record-cleanliness.py --base <commit> [--head <commit>]

Exit codes: 0 when every selected record passes (also when the range changes no
record), 1 when a selected record still carries a host absolute path, its bytes are
not text the sanitize tool can check, or its head entry is not a regular file blob
(a symlink stores its target text and a gitlink a subproject commit, so neither is
the record), 2 when the revision range or the sanitize tool cannot be used.
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
# A record's head entry must be a regular file blob: a symlink's stored bytes are its
# target text and a gitlink's are a subproject commit, neither of which is the record.
REGULAR_BLOB_MODES = {"100644", "100755"}
# A record is selected when the diff reports it as added, modified or type-changed:
# each case leaves a file at the head revision whose text this gate must parse.
SELECTED_STATUSES = {"A", "M", "T"}


class ComparisonBaseRefused(Exception):
    """A requested comparison base that cannot yield a comparable range."""


class DiffRefused(Exception):
    """A revision range git cannot diff, or whose output shape is unexpected."""


def run_git(repository: pathlib.Path, *arguments: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", "-C", str(repository), *arguments], capture_output=True, check=False,
    )


def resolve_commit(repository: pathlib.Path, revision: str) -> str | None:
    completed = run_git(repository, "rev-parse", "--verify", "--quiet", f"{revision}^{{commit}}")
    if completed.returncode != 0:
        return None
    return completed.stdout.decode("ascii", "replace").strip()


def resolve_base(repository: pathlib.Path, requested: str | None, head: str) -> tuple[str | None, bool]:
    """The commit to compare ``head`` against, plus whether it came from the fallback.

    The fallback rules are the module docstring's; the shape only this function owns
    is its refusal: a fallback that would compare the head with itself raises
    ``ComparisonBaseRefused`` instead of returning a base. A requested base that
    resolves is returned as ``(commit, False)`` and is compared directly; only a
    fallback base is returned as ``(commit, True)`` for the caller to narrow, because
    narrowing a requested base would collapse a non-fast-forward push to an empty
    range and hide the records it reintroduces.
    """
    requested_unreachable = False
    if requested and requested != ZERO_COMMIT:
        resolved = resolve_commit(repository, requested)
        if resolved is not None:
            return resolved, False
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
        return resolved, True
    first_parent = resolve_commit(repository, f"{head}^")
    return first_parent, True


def narrow_to_merge_base(repository: pathlib.Path, base: str, head: str) -> str:
    """The merge base of base and head when one exists, so base-branch commits stay out.

    Only the fallback base is narrowed. A requested base that resolves is compared
    directly, because a rollback or rewritten push makes the head an ancestor of that
    base: ``merge-base`` would then collapse the range to the head itself and report
    ``nothing to check`` while the push reintroduces records the base had cleaned.
    """
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
        raise DiffRefused(
            "cannot diff "
            f"{base}..{head}: {completed.stderr.decode('utf-8', 'replace').strip()}"
        )
    fields = [field.decode("utf-8", "surrogateescape") for field in completed.stdout.split(b"\0") if field]
    if len(fields) % 2 != 0:
        raise DiffRefused("unexpected git diff output shape")
    selected = [fields[index + 1] for index in range(0, len(fields), 2) if fields[index] in SELECTED_STATUSES]
    return sorted(selected)


def head_blob(repository: pathlib.Path, head: str, relative: str) -> tuple[str, bytes]:
    """The Git mode and blob bytes of one selected record at the head revision.

    Reading the head commit's own blob is what binds the revision under test: a
    working tree left at another revision, or a worktree file that is a symlink to
    elsewhere, cannot substitute bytes for it. The mode is returned so the caller can
    refuse a record the head does not store as a regular file.
    """
    listed = run_git(repository, "ls-tree", "-z", head, "--", relative)
    if listed.returncode != 0:
        raise DiffRefused(
            f"cannot read {relative} at {head}: {listed.stderr.decode('utf-8', 'replace').strip()}"
        )
    entries = [entry for entry in listed.stdout.split(b"\0") if entry]
    if len(entries) != 1:
        raise DiffRefused(f"the head revision {head} does not hold exactly one entry for {relative}")
    header = entries[0].split(b"\t", 1)[0].split(b" ")
    if len(header) != 3:
        raise DiffRefused(f"unexpected ls-tree entry for {relative} at {head}")
    mode, _kind, object_id = (field.decode("ascii") for field in header)
    if mode not in REGULAR_BLOB_MODES:
        # The mode decides before the blob is read: a gitlink's object id is the
        # subproject commit, so `cat-file blob` on it fails with "bad file" and the
        # gate would exit 2 (an unusable range) instead of the documented
        # non-regular refusal (exit 1). The caller refuses every non-regular mode
        # and never reads the payload.
        return mode, b""
    blob = run_git(repository, "cat-file", "blob", object_id)
    if blob.returncode != 0:
        raise DiffRefused(
            f"cannot read the blob of {relative} at {head}: "
            f"{blob.stderr.decode('utf-8', 'replace').strip()}"
        )
    return mode, blob.stdout


def check_record(payload: bytes) -> tuple[str, str]:
    """Run the sanitize tool's own root-only check over one record.

    Returns ``(status, detail)``: ``pass`` when the tool exits 0, ``dirty`` when it
    exits 1 (a host absolute path remains) and ``unreadable`` when it exits 2 (the
    bytes are not text the tool can check).
    """
    completed = subprocess.run(
        [sys.executable, str(SANITIZE_TOOL), "--check", "--root-only", "-"],
        input=payload, capture_output=True, check=False,
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
        base, fallback = resolve_base(repository, arguments.base, head)
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

    if fallback:
        base = narrow_to_merge_base(repository, base, head)
    try:
        records = changed_records(repository, base, head)
    except DiffRefused as refusal:
        print(f"check-record-cleanliness refused: {refusal}", file=sys.stderr)
        return 2
    if not records:
        print(f"check-record-cleanliness: no record added or modified between {base[:12]} and {head[:12]}; nothing to check")
        return 0
    dirty: list[tuple[str, str]] = []
    unreadable: list[tuple[str, str]] = []
    nonregular: list[tuple[str, str]] = []
    for relative in records:
        try:
            mode, payload = head_blob(repository, head, relative)
        except DiffRefused as refusal:
            print(f"check-record-cleanliness refused: {refusal}", file=sys.stderr)
            return 2
        if mode not in REGULAR_BLOB_MODES:
            nonregular.append((relative, f"the head revision stores it as mode {mode}"))
            continue
        status, detail = check_record(payload)
        if status == "dirty":
            dirty.append((relative, detail))
        elif status == "unreadable":
            unreadable.append((relative, detail))
    if dirty or unreadable or nonregular:
        for relative, detail in dirty + unreadable + nonregular:
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
        if nonregular:
            print(
                f"check-record-cleanliness refused: {len(nonregular)} of {len(records)} changed "
                "records are not regular file blobs at the head revision (a symlink stores its "
                "target text, a gitlink a subproject commit), so this gate cannot check them",
                file=sys.stderr,
            )
        return 1
    print(f"check-record-cleanliness: {len(records)} changed record(s) pass the root-only check")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
