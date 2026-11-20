#!/usr/bin/env python3
"""Refuse backticked path and symbol citations the revision under test does not hold.

This is the document-citation existence gate: for a revision range it selects every
document added or modified under the audited corpus - ``docs/runbooks/**``,
``docs/design/quality.md`` and the archived closures in ``docs/archive/CLOSED-TODO.md`` -
and checks the path and symbol citations those documents write in backticks against the
``--head`` revision. The failure it prevents is silent rot: a cited ``file:line`` or
symbol that no longer exists when its owner moves.

A path citation must hold in the head revision's tree (``git ls-tree -r -t``), or be a
path the checkout's ignore rules cover as a file or as a directory (``git
check-ignore``): the second arm is the deliberate boundary for generated output that no
index can contain by the repository's own rule - the runbook that documents the staged
portal bundle at ``control/priv/static/portal/`` keeps naming it - while a citation the
repository neither holds nor declares is refused. Raw ``file:line`` values are never
validated: the line suffix is stripped before the path is resolved, because any edit
above a cited line would otherwise redden the gate.

A symbol citation must occur in the file the prose binds it to. The binding is
deliberately narrow: the symbol and the path are consecutive backticked tokens in one
paragraph, separated by at most forty characters that hold no sentence terminator and at
most two words, each from the closed connective list below. A wider rule was measured
over the corpus and produced false pairs (identifiers belonging to another file in the
same sentence, config values, commit shas), and a mis-parse must never demand a rewrite;
an unbound pair is left to review rather than silently claimed.

What is audited, exactly. A backticked token is a path citation when, after a trailing
line suffix and a leading ``./`` are stripped:

* it is ``name.ext`` shaped (a file name) or carried a line suffix, and every path
  segment before the last names a real directory of the head tree - the multi-segment
  form; or
* it is, as a single segment, a top-level entry of the head tree (``README.md``,
  ``compose.production.yaml``, ``control``); or
* it ends in ``/`` (a directory citation, stripped of the slash); or
* it holds a wildcard (``**``, ``...``, ``…``, ``*``), whose anchor is the text before
  it and must be a tree entry.

Two shapes are deliberately not citations. An extensionless multi-segment token without
a trailing slash, a line suffix or a wildcard: this corpus writes branch names in
backticks (``docs/close-linux-platform-flakes``), which are indistinguishable from a
directory citation by shape, and that ambiguity is left to review rather than guessed.
And a bare file name (``read_model.ex``, ``runner.go``) that is not a top-level entry:
a basename names no repo-relative path, so there is nothing to resolve - the multi-
segment citations that do carry a directory are where that file is checked. A token
with whitespace, a scheme, a leading ``/``, ``~``, ``$`` or ``-``, or a placeholder
(``<``, ``>``, ``{``, ``}``) is not a citation either: it is a command, a URL, a host
path or a template. A symbol citation is ``name/arity``, ``Module.name/arity``,
``name()`` or ``Module.name()``, and it is checked only against a file-shaped path
citation: a directory or glob citation has no single text to search.

Changed-file selection is computed against an explicit comparison base, never from the
checkout's last commit: CI passes the pull-request base commit, or the pushed ``before``
commit for a push. The selected documents' text and the cited files' text are read from
the ``--head`` revision's Git blob - never from the working tree - so the checkout need
not be at ``--head`` and a worktree file cannot substitute bytes for the revision under
test; a document whose head entry is not a regular file blob (a symlink storing its
target text, or a gitlink) is refused. The base fallback and narrowing rules are
``revision_scope``'s, shared with ``check-record-cleanliness.py``. An audited document
that the range does not change is deliberately not audited: the reading of an older
document is its own review.

Usage::

    python scripts/ci/check-citation-existence.py --base <commit> [--head <commit>]

Exit codes: 0 when every selected document passes (also when the range changes no
document), 1 when a selected document cites a path or a symbol the head revision does
not hold, or its head entry is not a regular file blob, 2 when the repository, the
revision range or an ignore query cannot be used.
"""

from __future__ import annotations

import argparse
import bisect
import pathlib
import re
import sys

# The revision-range machinery is shared with scripts/ci/check-record-cleanliness.py;
# importing the sibling keeps one implementation of the base fallback and the head-blob
# read. The module resolves from this script's own directory, so the gate is run the same
# way from any working directory.
import revision_scope

PROGRAM = "check-citation-existence"
# The documents this gate audits: the operator runbooks, the design file the runbooks
# point at as the canonical gate list, and the archived closures.
AUDITED_PATHS = ("docs/runbooks", "docs/design/quality.md", "docs/archive/CLOSED-TODO.md")

BACKTICKED = re.compile(r"`([^`\n]+)`")
LINE_SUFFIX = re.compile(r":\d+(?:[-–,]\d+)*$")
WILDCARD = re.compile(r"\*\*|\.\.\.|…|\*")
FILENAME = re.compile(r"[A-Za-z0-9._-]+\.[A-Za-z0-9]{1,8}$")
NOT_A_CITATION = re.compile(r"[\s<>{}]")
ARITY_SYMBOL = re.compile(r"^(?:[A-Z][A-Za-z0-9_.]*\.)?([a-z_][A-Za-z0-9_]*[!?]?)/[0-9]+$")
CALL_SYMBOL = re.compile(r"^(?:[A-Z][A-Za-z0-9_]*\.)?([A-Za-z_][A-Za-z0-9_]*[!?]?)\(\)$")
PARAGRAPH_BREAK = re.compile(r"\n[ \t]*\n")
SENTENCE_BREAK = re.compile(r"[.!?]\s")
WORD = re.compile(r"[A-Za-z0-9_]+")
# The closed connective list the gap between a path and its symbol may use; anything else
# leaves the pair unbound (see the module docstring).
CONNECTIVES = frozenset(
    "of in at and or is the its from to with uses used calls see sees runs reads writes "
    "now only by for that so".split()
)
MAX_GAP_CHARACTERS = 40
MAX_GAP_WORDS = 2


class IgnoreQueryRefused(Exception):
    """The checkout's ignore rules could not be asked about a path."""


class Record:
    """One backticked token with its citation shape and its place in the document."""

    __slots__ = ("match", "path_shape", "path_body", "symbol", "line")

    def __init__(self, match: re.Match[str], line: int, files: set[str], directories: set[str]) -> None:
        self.match = match
        self.line = line
        self.path_shape, self.path_body = path_citation(match.group(1).strip(), files, directories)
        self.symbol = symbol_citation(match.group(1).strip())


def path_citation(
    token: str, files: set[str], directories: set[str],
) -> tuple[str | None, str | None]:
    """The ``(shape, body)`` of a path citation, or ``(None, None)`` when it is not one.

    Shapes are ``file``, ``dir`` and ``glob`` (a wildcard anchor); the line suffix is
    stripped from ``body`` and never checked (see the module docstring). The head tree's
    entries decide the shapes only a tree can decide: whether a single segment is an
    entry, and whether every directory above a file-shaped token exists.
    """
    if not token or "://" in token or NOT_A_CITATION.search(token):
        return (None, None)
    if token.startswith(("/", "~", "$", "-")):
        return (None, None)
    suffixed = bool(LINE_SUFFIX.search(token))
    body = LINE_SUFFIX.sub("", token)
    if body.startswith("./"):
        body = body[2:]
    if not body:
        return (None, None)
    wildcard = WILDCARD.search(body)
    if wildcard:
        anchor = body[: wildcard.start()].rstrip("/")
        if not anchor or not directory_prefix_exists(anchor, files, directories):
            return (None, None)
        return ("glob", anchor)
    if body.endswith("/"):
        stripped = body.rstrip("/")
        if not stripped or not directory_prefix_exists(stripped, files, directories):
            return (None, None)
        return ("dir", stripped)
    if "/" not in body:
        # A single segment is a path only as a top-level entry: this corpus writes bare
        # file names in backticks (`read_model.ex`, `runner.go`), and a basename alone
        # names no repo-relative path the gate could resolve.
        if body in files:
            return ("file", body)
        return ("dir", body) if body in directories else (None, None)
    if not directory_prefix_exists(body, files, directories):
        return (None, None)
    return ("file", body) if suffixed or FILENAME.match(body.rsplit("/", 1)[-1]) else (None, None)


def directory_prefix_exists(body: str, files: set[str], directories: set[str]) -> bool:
    """Whether ``body`` is a tree entry, or every directory above it is.

    A citation of a path the head revision does not hold is still this gate's business
    (that is the rot it refuses), so the directory prefix - not the whole path - decides
    whether a token is repo-relative at all: ``docs/runbooks/moved.md`` is a citation
    because ``docs/runbooks`` exists, while ``main`` or ``docs/close-a-branch`` is not.
    """
    if body in files or body in directories:
        return True
    parent = body.rsplit("/", 1)[0]
    while parent:
        if parent in directories:
            return True
        if "/" not in parent:
            return False
        parent = parent.rsplit("/", 1)[0]
    return False


def symbol_citation(token: str) -> str | None:
    """The bare name of a symbol citation, or ``None`` when the token is not one."""
    for pattern in (ARITY_SYMBOL, CALL_SYMBOL):
        match = pattern.match(token)
        if match:
            return match.group(1)
    return None


def prose_binds_path_and_symbol(gap: str) -> bool:
    """Whether the text between two backticked tokens binds them (module docstring)."""
    if len(gap) > MAX_GAP_CHARACTERS or SENTENCE_BREAK.search(gap):
        return False
    words = WORD.findall(gap)
    return len(words) <= MAX_GAP_WORDS and all(word.lower() in CONNECTIVES for word in words)


def paragraphs(text: str):
    """``(offset, chunk)`` for every run of lines between blank lines."""
    start = 0
    for separator in PARAGRAPH_BREAK.finditer(text):
        yield start, text[start:separator.start()]
        start = separator.end()
    yield start, text[start:]


def head_entries(repository: pathlib.Path, head: str) -> tuple[set[str], set[str]]:
    """The head revision's file and directory paths, from the tree it stores."""
    listed = revision_scope.run_git(repository, "ls-tree", "-r", "-t", "-z", head)
    if listed.returncode != 0:
        raise revision_scope.DiffRefused(
            f"cannot read the tree of {head}: {listed.stderr.decode('utf-8', 'replace').strip()}"
        )
    files: set[str] = set()
    directories: set[str] = set()
    for entry in listed.stdout.split(b"\0"):
        if not entry:
            continue
        header, _, path = entry.partition(b"\t")
        fields = header.split(b" ")
        if len(fields) != 3 or not path:
            raise revision_scope.DiffRefused(f"unexpected ls-tree entry for {head}")
        _mode, kind, _object_id = (field.decode("ascii") for field in fields)
        relative = path.decode("utf-8", "surrogateescape")
        if kind == "blob":
            files.add(relative)
        else:
            # A tree is a directory and a gitlink (a submodule) is a path that exists but
            # stores no text; either way a citation of it holds and has no blob to search.
            directories.add(relative)
    return files, directories


def ignore_explains(repository: pathlib.Path, body: str) -> bool:
    """Whether the checkout's ignore rules cover ``body`` as a file or as a directory."""
    for probe in (body, f"{body}/"):
        completed = revision_scope.run_git(repository, "check-ignore", "--quiet", "--", probe)
        if completed.returncode == 0:
            return True
        if completed.returncode != 1:
            raise IgnoreQueryRefused(
                f"git check-ignore could not be asked about {probe}: "
                f"{completed.stderr.decode('utf-8', 'replace').strip()}"
            )
    return False


def occurrence(blob: str, name: str) -> bool:
    """Whether ``name`` occurs in ``blob`` as a whole word."""
    return re.search(rf"(?<![A-Za-z0-9_]){re.escape(name)}(?![A-Za-z0-9_])", blob) is not None


def audit_document(
    repository: pathlib.Path,
    head: str,
    files: set[str],
    directories: set[str],
    relative: str,
    text: str,
) -> tuple[list[str], int]:
    """Check one document's citations; return its refusals and its path-citation count."""
    newlines = [index for index, character in enumerate(text) if character == "\n"]
    refusals: list[str] = []
    citations = 0
    blobs: dict[str, str | None] = {}

    def line_of(offset: int) -> int:
        return bisect.bisect_right(newlines, offset) + 1

    def blob_of(relative_path: str) -> str | None:
        """The cited file's head text, or ``None`` when its entry stores no text."""
        if relative_path not in blobs:
            mode, payload = revision_scope.head_blob(repository, head, relative_path)
            if mode in revision_scope.REGULAR_BLOB_MODES:
                blobs[relative_path] = payload.decode("utf-8", "surrogateescape")
            else:
                blobs[relative_path] = None
        return blobs[relative_path]

    for start, paragraph in paragraphs(text):
        records = [
            Record(match, line_of(start + match.start()), files, directories)
            for match in BACKTICKED.finditer(paragraph)
        ]
        for record in records:
            if record.path_shape is None:
                continue
            body = record.path_body or ""
            citations += 1
            present = body in directories if record.path_shape == "dir" else body in files | directories
            if not present and not ignore_explains(repository, body):
                refusals.append(
                    f"{relative}:{record.line}: the cited path `{body}` is in no entry of "
                    f"{head[:12]} and no ignore rule covers it; cite a path this revision's tree "
                    "holds, or pin the historical one as `git show <rev>:<path>`"
                )
        for first, second in zip(records, records[1:]):
            gap = paragraph[first.match.end():second.match.start()]
            if not prose_binds_path_and_symbol(gap):
                continue
            path_record = symbol_record = None
            if first.path_shape is not None and second.symbol is not None:
                path_record, symbol_record = first, second
            elif second.path_shape is not None and first.symbol is not None:
                path_record, symbol_record = second, first
            if path_record is None or symbol_record is None:
                continue
            if path_record.path_shape != "file":
                # A directory or glob citation has no single text to search.
                continue
            body = path_record.path_body or ""
            blob = blob_of(body)
            if blob is None:
                refusals.append(
                    f"{relative}:{symbol_record.line}: the cited symbol "
                    f"`{symbol_record.symbol}` cannot be checked: `{body}` stores no text at "
                    f"{head[:12]}"
                )
            elif not occurrence(blob, symbol_record.symbol):
                refusals.append(
                    f"{relative}:{symbol_record.line}: the cited symbol "
                    f"`{symbol_record.symbol}` does not occur in `{body}` at {head[:12]}; cite "
                    "the file that carries it, or drop the symbol"
                )
    return refusals, citations


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Check the backticked path and symbol citations of the runbooks, "
                    "docs/design/quality.md and docs/archive/CLOSED-TODO.md that a revision "
                    "range adds or modifies against the head revision.",
    )
    parser.add_argument("--repo", default=".", metavar="PATH", help="repository path (default: .)")
    parser.add_argument("--base", default=None, metavar="COMMIT",
                        help="comparison base; empty or all-zero falls back to origin/main, then HEAD^")
    parser.add_argument("--head", default="HEAD", metavar="COMMIT", help="head revision (default: HEAD)")
    arguments = parser.parse_args(argv)

    repository = pathlib.Path(arguments.repo).resolve()
    if not (repository / ".git").exists():
        print(f"{PROGRAM} refused: {repository} is not a repository", file=sys.stderr)
        return 2
    head = revision_scope.resolve_commit(repository, arguments.head)
    if head is None:
        print(f"{PROGRAM} refused: cannot resolve the head {arguments.head!r}", file=sys.stderr)
        return 2
    try:
        base, fallback = revision_scope.resolve_base(repository, arguments.base, head, PROGRAM)
    except revision_scope.ComparisonBaseRefused as refusal:
        print(f"{PROGRAM} refused: {refusal}", file=sys.stderr)
        return 2
    if base is None:
        print(
            f"{PROGRAM} refused: cannot resolve a comparison base "
            f"({arguments.base!r} against origin/main or HEAD^)",
            file=sys.stderr,
        )
        return 2
    if fallback:
        base = revision_scope.narrow_to_merge_base(repository, base, head)
    try:
        documents = revision_scope.changed_paths(repository, base, head, AUDITED_PATHS)
    except revision_scope.DiffRefused as refusal:
        print(f"{PROGRAM} refused: {refusal}", file=sys.stderr)
        return 2
    if not documents:
        print(
            f"{PROGRAM}: no audited document added or modified between {base[:12]} and "
            f"{head[:12]}; nothing to check"
        )
        return 0
    try:
        files, directories = head_entries(repository, head)
    except revision_scope.DiffRefused as refusal:
        print(f"{PROGRAM} refused: {refusal}", file=sys.stderr)
        return 2
    refusals: list[str] = []
    nonregular: list[str] = []
    citations = 0
    for relative in documents:
        try:
            mode, payload = revision_scope.head_blob(repository, head, relative)
        except revision_scope.DiffRefused as refusal:
            print(f"{PROGRAM} refused: {refusal}", file=sys.stderr)
            return 2
        if mode not in revision_scope.REGULAR_BLOB_MODES:
            nonregular.append(f"{relative}: the head revision stores it as mode {mode}")
            continue
        try:
            document_refusals, document_citations = audit_document(
                repository, head, files, directories, relative,
                payload.decode("utf-8", "surrogateescape"),
            )
        except (IgnoreQueryRefused, revision_scope.DiffRefused) as refusal:
            print(f"{PROGRAM} refused: {refusal}", file=sys.stderr)
            return 2
        refusals += document_refusals
        citations += document_citations
    if refusals or nonregular:
        for refusal in refusals:
            print(f"{PROGRAM} refused: {refusal}", file=sys.stderr)
        if refusals:
            print(
                f"{PROGRAM} refused: {len(refusals)} citation(s) in {len(documents)} changed "
                "document(s) do not hold at the head revision; a cited path or symbol rots "
                "silently when its owner moves, so cite what this revision holds",
                file=sys.stderr,
            )
        for detail in nonregular:
            print(f"{PROGRAM} refused: {detail}", file=sys.stderr)
        if nonregular:
            print(
                f"{PROGRAM} refused: {len(nonregular)} of {len(documents)} changed documents are "
                "not regular file blobs at the head revision (a symlink stores its target text, a "
                "gitlink a subproject commit), so this gate cannot check their citations",
                file=sys.stderr,
            )
        return 1
    print(
        f"{PROGRAM}: {len(documents)} changed document(s) pass the path and symbol check "
        f"({citations} path citation(s))"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
