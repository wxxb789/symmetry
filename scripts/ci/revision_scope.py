#!/usr/bin/env python3
"""The revision-range machinery the changed-file CI gates share.

Both ``check-record-cleanliness.py`` and ``check-citation-existence.py`` select the
files a revision range changes and read those files from the head revision's own Git
blob. The selection is computed against an explicit comparison base, never from the
checkout's last commit: CI passes the pull-request base commit, or the pushed
``before`` commit for a push. The selected paths' bytes are read from the ``--head``
revision's Git blob - never from the working tree - so the checkout need not be at
``--head`` and a worktree file cannot substitute bytes for the revision under test.

An empty or all-zero base - a new branch, a tag push, a manual run - names no earlier
revision of this branch and falls back to the merge base with ``origin/main`` and then
to ``HEAD^``; when that range is empty it reports ``nothing to check`` and passes,
because the revision was compared with itself by construction, not by omission. A base
that was requested and is not present in this checkout (a force-push's ``before``)
takes the same fallback with a hint, but a fallback that would compare the head with
itself refuses: the rewritten history now sits on ``origin/main``, and a vacuous pass
would hide the files such a push adds. A requested base that resolves is compared
directly (``git diff base head``), so a non-fast-forward push that reintroduces an
earlier file is still checked instead of being narrowed to an empty range; only the
fallback base - already a merge base with ``origin/main``, or ``HEAD^`` - is narrowed
to its merge base with the head.

A selected path whose head entry is not a regular file blob is left for its gate to
refuse: a symlink stores its target text and a gitlink a subproject commit, so neither
is the document the gate must parse. ``head_blob`` returns the mode without reading such
an object, because a gitlink's object id is the subproject commit and ``cat-file blob``
on it fails as an unusable range instead of a non-regular document.

This file is a library, not a gate: it is imported by the two check scripts (``import
revision_scope``, resolved from the importing script's own directory) and executed by
nothing. Keep it import-safe - no work at import time beyond these definitions - and
keep the messages free of a gate name: the caller passes its own ``label`` where a
message needs one.
"""

from __future__ import annotations

import pathlib
import subprocess
import sys

ZERO_COMMIT = "0" * 40
# The branch a fallback base is derived from when the requested one names no revision.
DEFAULT_BRANCH = "origin/main"
# A selected path's head entry must be a regular file blob: a symlink's stored bytes are
# its target text and a gitlink's are a subproject commit, neither of which is the file.
REGULAR_BLOB_MODES = {"100644", "100755"}
# A path is selected when the diff reports it as added, modified or type-changed: each
# case leaves a file at the head revision whose text the gate must parse.
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


def resolve_base(
    repository: pathlib.Path, requested: str | None, head: str, label: str,
) -> tuple[str | None, bool]:
    """The commit to compare ``head`` against, plus whether it came from the fallback.

    The fallback rules are the module docstring's; the shape only this function owns is
    its refusal: a fallback that would compare the head with itself raises
    ``ComparisonBaseRefused`` instead of returning a base. A requested base that resolves
    is returned as ``(commit, False)`` and is compared directly; only a fallback base is
    returned as ``(commit, True)`` for the caller to narrow, because narrowing a
    requested base would collapse a non-fast-forward push to an empty range and hide the
    files it reintroduces. ``label`` names the calling gate in the hint printed when a
    requested base is not present in this checkout.
    """
    requested_unreachable = False
    if requested and requested != ZERO_COMMIT:
        resolved = resolve_commit(repository, requested)
        if resolved is not None:
            return resolved, False
        requested_unreachable = True
        print(
            f"{label}: base {requested} is not present in this checkout "
            f"(a force-push?); comparing against the {DEFAULT_BRANCH} merge base instead",
            file=sys.stderr,
        )
    merge_base = run_git(repository, "merge-base", head, DEFAULT_BRANCH)
    if merge_base.returncode == 0:
        resolved = merge_base.stdout.decode("ascii", "replace").strip()
        if requested_unreachable and resolved == head:
            raise ComparisonBaseRefused(
                f"the requested base {requested} is not present in this checkout and the "
                f"{DEFAULT_BRANCH} fallback is the head itself, so the range would be empty; a "
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
    ``nothing to check`` while the push reintroduces files the base had cleaned.
    """
    merge_base = run_git(repository, "merge-base", base, head)
    if merge_base.returncode != 0:
        return base
    return merge_base.stdout.decode("ascii", "replace").strip() or base


def changed_paths(
    repository: pathlib.Path, base: str, head: str, pathspecs: tuple[str, ...],
) -> list[str]:
    """Paths added, modified or retyped between ``base`` and ``head``, sorted.

    ``--no-renames`` keeps a rename deterministic: it is reported as a deletion plus an
    addition, and the added path carries the content the gate must read. The statuses
    are ``SELECTED_STATUSES``; the pathspecs are the caller's audited surface.
    """
    completed = run_git(
        repository, "diff", "--name-status", "--no-renames", "-z", base, head, "--", *pathspecs,
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
    """The Git mode and blob bytes of one selected path at the head revision.

    Reading the head commit's own blob is what binds the revision under test: a working
    tree left at another revision, or a worktree file that is a symlink to elsewhere,
    cannot substitute bytes for it. The mode is returned so the caller can refuse a path
    the head does not store as a regular file.

    The pathspec is anchored to the repository root and matched literally
    (``:(top,literal)``): the read does not depend on the process working directory, and
    no character of the path is interpreted as a glob. No tracked path holds a glob
    metacharacter today, so this closes the interpretation rather than a measured
    mis-read.
    """
    listed = run_git(repository, "ls-tree", "-z", head, "--", f":(top,literal){relative}")
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
        # subproject commit, so `cat-file blob` on it fails with "bad file" and the gate
        # would exit 2 (an unusable range) instead of the documented non-regular refusal.
        # The caller refuses every non-regular mode and never reads the payload.
        return mode, b""
    blob = run_git(repository, "cat-file", "blob", object_id)
    if blob.returncode != 0:
        raise DiffRefused(
            f"cannot read the blob of {relative} at {head}: "
            f"{blob.stderr.decode('utf-8', 'replace').strip()}"
        )
    return mode, blob.stdout
