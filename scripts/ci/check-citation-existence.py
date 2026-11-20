#!/usr/bin/env python3
"""Refuse backticked path and symbol citations the revision under test does not hold.

This is the document-citation existence gate: for a revision range it selects every
document added or modified under the audited corpus - ``docs/runbooks/**``,
``docs/design/quality.md`` and the archived closures in ``docs/archive/CLOSED-TODO.md`` -
and checks the path and symbol citations those documents write in backticks against the
``--head`` revision. The failure it prevents is silent rot: a cited ``file:line`` or
symbol that no longer exists when its owner moves.

A path citation must hold in the head revision's tree (``git ls-tree -r -t``), or be a
path the head revision's own ignore rules cover as a file or as a directory: the second
arm is the deliberate boundary for generated output that no index can contain by the
repository's own rule - the runbook that documents the staged portal bundle at
``control/priv/static/portal/`` keeps naming it - while a citation the repository
neither holds nor declares is refused. An ignore-covered citation cannot be verified
against any committed bytes, so each one is reported as a per-citation notice naming
the ``source:line:pattern`` rule that covers it, never as a silent pass. Raw
``file:line`` values are never validated: the line suffix, with an optional column, is
stripped before the path is resolved, because any edit above a cited line would
otherwise redden the gate.

A symbol citation must occur in the file the prose binds it to. The binding is
deliberately narrow: the symbol and the path are consecutive backticked tokens in one
paragraph, separated by at most forty characters that hold no sentence terminator and at
most two words, each from the closed connective list below. A wider rule was measured
over the corpus and produced false pairs (identifiers belonging to another file in the
same sentence, config values, commit shas), and a mis-parse must never demand a rewrite;
an unbound pair is left to review rather than silently claimed. The list holds words
that join a name to its owner (``in``, ``of``, ``calls``, ``reads``); a coordinating
conjunction (``and``, ``or``) only joins list items, so it binds nothing, and a symbol
bound to a path the head revision does not hold is skipped entirely: the path arm
already reports that path, named, at exit 1.

What is audited, exactly. A backticked token is a path citation when, after a trailing
line suffix and a leading ``./`` are stripped:

* it is ``name.ext`` shaped (a file name) or carried a line suffix - the multi-segment
  form - and its first segment, or the deepest existing directory above it, resolves
  in the head tree; or
* it is, as a single segment, a top-level entry of the head tree (``README.md``,
  ``compose.production.yaml``, ``control``); or
* it ends in ``/`` (a directory citation, stripped of the slash), resolved like the
  multi-segment form; or
* it holds a wildcard (``**``, ``...``, ``…``, ``*``); the pattern it spells must match
  at least one entry of the head tree (``**`` spans path segments and ``*`` does not),
  and the text before the first wildcard must resolve like the multi-segment form.

The head tree need not hold a resolved multi-segment citation (that is the rot the gate
refuses). "Resolves" has a third arm beside the head tree: a multi-segment token whose
first segment the head revision lacks but a commit reachable from the head held
(``git rev-list -1 <head> -- <segment>``, asked once per distinct segment) is a
directory or file the repository once had and the tree no longer does - the removal may
lie outside the comparison range, because the repair lands on the next pull request
that touches the citing document. Every citation under such a segment - path, directory
or glob alike - is refused with the path named, unless the head revision's own ignore
rules cover the token: the ignore arm is consulted first and the run passes with a
per-citation notice naming the rule, the recorded precedence for the case where a
removed tree is also ignored. On a shallow clone the arm sees only the fetched history,
and a removal older than the shallow boundary is left to review, as before. A
single-segment token is never resolved this way (a bare file name is a deliberate
non-citation), and a token whose first segment neither the head nor its history holds
at the top level (a subdirectory-relative ``config/test.exs``, an upstream source path
such as PostgreSQL's ``src/backend/...``) names no repo-relative path the gate can
resolve, and is left to review rather than guessed: a mis-parse must never demand a
rewrite.

Two more shapes are deliberately not citations. An extensionless multi-segment token
without a trailing slash, a line suffix or a wildcard: this corpus writes branch names
in backticks (``docs/close-linux-platform-flakes``) in the same shape it cites
directories (``control/lib``, ``daemon/internal/platform``), and the tree separates the
two only when the token is itself an entry - so the whole shape is left to review rather
than guessed, a deliberate recall limit whose cost is that a tree directory cited this
way is not re-checked when it moves.
And a bare file name (``read_model.ex``, ``runner.go``) that is not a top-level entry:
a basename names no repo-relative path, so there is nothing to resolve - the multi-
segment citations that do carry a directory are where that file is checked. A token
with whitespace, a scheme, a leading ``/``, ``~``, ``$`` or ``-``, or a placeholder
(``<``, ``>``, ``{``, ``}``) is not a citation either: it is a command, a URL, a host
path or a template. A symbol citation is ``name/arity``, ``Module.name/arity``,
``name()`` or ``Module.name()``, and it is checked only against a file-shaped path
citation: a directory or glob citation has no single text to search. Lines inside a
fenced code block are not audited at all: the backticks in a JS template literal, a Go
raw string or a shell example are code, not citations. A fence is recognized at any
indentation and behind blockquote markers (a fence nested in a list item sits deeper
than the top level; a fence on the same line as a list marker is not recognized), and
only a line of the same fence character, at least as long, with nothing after it closes
the block (``without_fenced_blocks`` states the boundary).

Changed-file selection is computed against an explicit comparison base, never from the
checkout's last commit: CI passes the pull-request base commit, or the pushed ``before``
commit for a push. The selected documents' text and the cited files' text are read from
the ``--head`` revision's Git blob - never from the working tree - so the checkout need
not be at ``--head`` and a worktree file cannot substitute bytes for the revision under
test; a document whose head entry is not a regular file blob (a symlink storing its
target text, or a gitlink) is refused. The ignore rules are read the same way, from the
head revision's own ``.gitignore`` blobs and not from the checkout's (see
``HeadIgnoreRules`` below). The base fallback and narrowing rules are
``revision_scope``'s, shared with ``check-record-cleanliness.py``.

The audit scope is the whole selected document, not the lines the range changed:
touching a document obliges repairing every stale citation it carries - the archived
closure that cites a file another increment moved must be repointed in the same pull
request. A document the range does not change is deliberately not audited (the reading
of an older document is its own review), so a pull request that only moves or renames a
file does not re-check the documents that cite it: the repair lands, as a red gate, in
the next pull request that touches one of those documents.

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
import os
import pathlib
import re
import subprocess
import sys
import tempfile

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
# The line suffix, with an optional column (``file:line:col`` is what compilers, test
# runners and linters print); the suffix is stripped and never validated.
LINE_SUFFIX = re.compile(r":\d+(?::\d+)?(?:[-–,]\d+(?::\d+)?)*$")
WILDCARD = re.compile(r"\*\*|\.\.\.|…|\*")
# A fenced code block's opening or closing line: any leading whitespace (a fence nested
# in a list item sits deeper than the top level), any blockquote markers, then three or
# more backticks or tildes. The second group is the info string; a closing fence may
# not carry one, which is what keeps a line like ```js inside a fence from closing it.
FENCE = re.compile(r"^[ \t]*(?:>[ \t]*)*(`{3,}|~{3,})(.*)$")
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
    "of in at is the its from to with uses used calls see sees runs reads writes "
    "now only by for that so".split()
)
MAX_GAP_CHARACTERS = 40
MAX_GAP_WORDS = 2


class IgnoreQueryRefused(Exception):
    """The head revision's ignore rules could not be asked about a path."""


class Record:
    """One backticked token with its citation shape and its place in the document."""

    __slots__ = ("match", "path_shape", "path_body", "symbol", "line")

    def __init__(
        self,
        match: re.Match[str],
        line: int,
        files: set[str],
        directories: set[str],
        removed: RemovedOwners,
    ) -> None:
        self.match = match
        self.line = line
        self.path_shape, self.path_body = path_citation(
            match.group(1).strip(), files, directories, removed,
        )
        self.symbol = symbol_citation(match.group(1).strip())


def path_citation(
    token: str, files: set[str], directories: set[str], removed: frozenset[str],
) -> tuple[str | None, str | None]:
    """The ``(shape, body)`` of a path citation, or ``(None, None)`` when it is not one.

    Shapes are ``file``, ``dir`` and ``glob`` (the glob as written); the line suffix is
    stripped from ``body`` and never checked, and a leading ``./`` comes off (see the
    module docstring). The head tree's entries and the head's history decide the shapes
    only they can decide: whether a single segment is an entry, and whether a
    multi-segment token names a repo-relative prefix at all.
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
        if not anchor or not names_a_repo_prefix(anchor, files, directories, removed):
            return (None, None)
        return ("glob", body)
    if body.endswith("/"):
        stripped = body.rstrip("/")
        if not stripped or not names_a_repo_prefix(stripped, files, directories, removed):
            return (None, None)
        return ("dir", stripped)
    if "/" not in body:
        # A single segment is a path only as a top-level entry: this corpus writes bare
        # file names in backticks (`read_model.ex`, `runner.go`), and a basename alone
        # names no repo-relative path the gate could resolve.
        if body in files:
            return ("file", body)
        return ("dir", body) if body in directories else (None, None)
    if not names_a_repo_prefix(body, files, directories, removed):
        return (None, None)
    return ("file", body) if suffixed or FILENAME.match(body.rsplit("/", 1)[-1]) else (None, None)


def names_a_repo_prefix(
    body: str, files: set[str], directories: set[str], removed: RemovedOwners,
) -> bool:
    """Whether ``body`` names a repo-relative prefix the head tree or the range explains.

    The head tree's entries decide for a token it holds or whose deepest existing
    ancestor it holds (``directory_prefix_exists``). When even the first segment of
    ``body`` is gone, one arm remains: a first segment the head revision lacks but the
    history behind it held (``RemovedOwners``) is an entry the repository once had and
    removed, so the token cites the moved owner - while a first segment neither the
    head nor its history holds at the top level (a subdirectory-relative
    ``config/test.exs``, an upstream source path such as PostgreSQL's
    ``src/backend/...``) names nothing the gate can resolve, and is left to review
    rather than guessed (a mis-parse must never demand a rewrite).
    """
    return directory_prefix_exists(body, files, directories) or body.split("/", 1)[0] in removed


def directory_prefix_exists(body: str, files: set[str], directories: set[str]) -> bool:
    """Whether ``body`` is a tree entry, or the deepest existing ancestor above it is.

    A citation of a path the head revision does not hold is still this gate's business
    (that is the rot it refuses), so the directory prefix - not the whole path - decides
    whether a token is repo-relative at all: ``docs/runbooks/moved.md`` is a citation
    because ``docs/runbooks`` exists, and so is ``docs/runbooks/moved/sub/thing.md``,
    because the deepest ancestor the walk finds is a tree directory. ``main`` or
    ``docs/close-a-branch`` is not: no ancestor is a tree directory and neither is the
    token itself.
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


def segment_matches(pattern: str, name: str) -> bool:
    """Whether one path segment matches a wildcard segment pattern; ``*`` is any run."""
    parts = pattern.split("*")
    if len(parts) == 1:
        return pattern == name
    if not name.startswith(parts[0]):
        return False
    name = name[len(parts[0]):]
    for part in parts[1:-1]:
        index = name.find(part)
        if index < 0:
            return False
        name = name[index + len(part):]
    return name.endswith(parts[-1]) and len(name) >= len(parts[-1])


def glob_matches(pattern: str, path: str) -> bool:
    """Whether ``path`` matches a citation glob: ``**`` spans segments, ``*`` does not.

    The two ellipsis spellings were translated to ``*`` before this call. Matching a
    tree path is a presence test, so the semantics are the narrowing kind: ``*`` never
    crosses a ``/``, and ``**`` is the one spelling that does.
    """
    reachable = [True] + [False] * len(path.split("/"))
    names = path.split("/")
    for pattern_segment in pattern.split("/"):
        if pattern_segment == "**":
            spanning = False
            for index in range(len(reachable)):
                spanning = spanning or reachable[index]
                reachable[index] = spanning
            continue
        advanced = [False] * len(reachable)
        for index in range(1, len(reachable)):
            advanced[index] = (
                reachable[index - 1] and segment_matches(pattern_segment, names[index - 1])
            )
        reachable = advanced
    return reachable[len(names)]


def glob_pattern(body: str) -> str:
    """The citation glob as a matcher pattern: the ellipsis spellings become ``*``."""
    return body.replace("...", "*").replace("…", "*")


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


def without_fenced_blocks(text: str) -> str:
    """``text`` with every fenced code block blanked to spaces; line offsets survive.

    A backticked token inside a fence is code, not a citation: JS template literals, Go
    raw strings and shell examples write the same backticks prose does, and demanding a
    rewrite of the example would be the mis-parse the module docstring forbids. The
    blanked lines keep their newlines - and so every line number a refusal reports -
    and become paragraph separators, so a binding never reaches across a fence.

    The blanking needs the fence's shape, not CommonMark's column arithmetic: the
    opener is recognized at any indentation and behind any blockquote markers, so a
    fence nested in a list item (content column four or deeper) or quoted in a
    blockquote is blanked too. A closing line carries the same fence character, at
    least as many of them, and nothing after them - an info string makes a line
    content, so a ```js line inside a fence does not close it early. An unclosed fence
    blanks to the end of the document, the way CommonMark reads it.
    """
    blanked: list[str] = []
    fence: tuple[str, int] | None = None
    for line in text.splitlines(keepends=True):
        marker = FENCE.match(line)
        if fence is not None:
            blanked.append(re.sub(r"[^\n]", " ", line))
            if (
                marker is not None
                and marker.group(1)[0] == fence[0]
                and len(marker.group(1)) >= fence[1]
                and not marker.group(2).strip()
            ):
                fence = None
            continue
        if marker is None:
            blanked.append(line)
            continue
        fence = (marker.group(1)[0], len(marker.group(1)))
        blanked.append(re.sub(r"[^\n]", " ", line))
    return "".join(blanked)


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


class RemovedOwners:
    """Top-level entries the head revision lacks but its history once held.

    ``names_a_repo_prefix`` consults this when the head tree cannot explain a token's
    first segment. The segment is the only one the ancestor walk cannot reach (a
    deeper removal still leaves the first segment standing, and the walk finds it), and
    the comparison base cannot be the source of the answer: a removal that merged in an
    earlier pull request took the segment out of base and head alike, so the next
    document-touching pull request would pass silently and stop counting the stale
    citations. The history can answer - ``git rev-list -1 <head> -- <segment>`` lists a
    commit whenever the repository held that top-level path at a revision reachable
    from the head, whatever the comparison range is - and CI's ``citation-existence``
    job checks out with ``fetch-depth: 0``, so the full history is there. On a shallow
    clone the answer degrades toward the copy's history: a removal older than the
    shallow boundary is not seen and the token is left to review, as it was before the
    history arm existed. Each distinct segment is asked once per run.
    """

    def __init__(self, repository: pathlib.Path, head: str) -> None:
        self.repository = repository
        self.head = head
        self.answers: dict[str, bool] = {}

    def __contains__(self, segment: str) -> bool:
        if segment not in self.answers:
            listed = revision_scope.run_git(
                self.repository, "rev-list", "-1", self.head, "--", f":(top,literal){segment}",
            )
            if listed.returncode != 0:
                raise revision_scope.DiffRefused(
                    f"cannot read the history of {self.head[:12]}: "
                    f"{listed.stderr.decode('utf-8', 'replace').strip()}"
                )
            self.answers[segment] = bool(listed.stdout.strip())
        return self.answers[segment]


def scratch_git(scratch: pathlib.Path, *arguments: str) -> subprocess.CompletedProcess:
    """A git command in the scratch checkout, isolated from the user's own state.

    The scratch checkout exists to answer from the head revision's committed rule
    bytes; the user's global and system configuration, the XDG excludes file and an
    inherited ``GIT_DIR`` (a hook or a wrapper may set one) must not enter the answer.
    The callers that match pathnames pin ``core.ignorecase=false`` so the committed
    bytes decide the same way on every platform, as the tree arm's membership does.
    """
    environment = dict(os.environ)
    environment.update(
        GIT_CONFIG_GLOBAL=os.devnull,
        GIT_CONFIG_SYSTEM=os.devnull,
        GIT_CONFIG_NOSYSTEM="1",
        XDG_CONFIG_HOME=str(scratch / "no-xdg-config"),
        HOME=str(scratch / "no-home"),
    )
    for key in (
        "GIT_DIR",
        "GIT_WORK_TREE",
        "GIT_INDEX_FILE",
        "GIT_OBJECT_DIRECTORY",
        "GIT_COMMON_DIR",
        "GIT_ALTERNATE_OBJECT_DIRECTORIES",
    ):
        environment.pop(key, None)
    return subprocess.run(
        ["git", "-C", str(scratch), *arguments], capture_output=True, check=False, env=environment,
    )


class HeadIgnoreRules:
    """The head revision's own ignore rules, asked in a scratch checkout of them.

    ``git check-ignore`` answers from the ignore files *the worktree* holds, so asking
    the repository itself would let a working tree that lags the head revision, a local
    ``.git/info/exclude`` or a global excludes file answer for the revision under test.
    The gate materializes the head revision's ``.gitignore`` blobs into a scratch
    repository once per run and asks there, so one set of committed bytes decides on
    every checkout and platform. The rule text check-ignore reports back
    (``source:line:pattern``) is what the per-citation notices name.
    """

    def __init__(
        self, repository: pathlib.Path, head: str, files: set[str], scratch: pathlib.Path,
    ) -> None:
        self.scratch = scratch
        # The committed ignore files this checkout materializes: the only rule sources
        # a notice may name (see covering_rule).
        self.sources = frozenset(
            path for path in files if path == ".gitignore" or path.endswith("/.gitignore")
        )
        initialized = scratch_git(scratch, "init", "--quiet")
        if initialized.returncode != 0:
            raise IgnoreQueryRefused(
                "cannot prepare the ignore-rule checkout: "
                f"{initialized.stderr.decode('utf-8', 'replace').strip()}"
            )
        for relative in sorted(self.sources):
            _mode, payload = revision_scope.head_blob(repository, head, relative)
            target = scratch / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(payload)

    def covering_rule(self, body: str) -> str | None:
        """The ``source:line:pattern`` of the rule covering ``body``, or ``None``.

        The decision is the quiet query's exit code - the only shape whose status means
        "ignored" after negation is resolved - and the verbose query only names the
        deciding rule for the notice. The notice is only accepted when the named source
        is one of the committed bytes this checkout materialized, spelling included: a
        case-insensitive filesystem resolves a differently-cased directory to a
        materialized ``.gitignore`` (``control/Gen`` finds ``control/gen/.gitignore``)
        and ``core.ignorecase=false`` pins the pattern match, not that filesystem
        lookup - so a source no case-exact checkout would read is treated as no rule
        here, the answer the tree's own membership gives on every platform.
        """
        for probe in (body, f"{body}/"):
            decision = scratch_git(
                self.scratch, "-c", "core.ignorecase=false", "check-ignore", "--quiet", "--", probe,
            )
            if decision.returncode == 1:
                continue
            if decision.returncode != 0:
                raise IgnoreQueryRefused(
                    f"git check-ignore could not be asked about {probe}: "
                    f"{decision.stderr.decode('utf-8', 'replace').strip()}"
                )
            named = scratch_git(
                self.scratch, "-c", "core.ignorecase=false", "check-ignore", "-v", "--", probe,
            )
            lines = named.stdout.decode("utf-8", "replace").splitlines()
            rule = lines[0].split("\t", 1)[0] if lines else ""
            if rule.split(":", 1)[0] not in self.sources:
                continue
            return rule
        return None


def occurrence(blob: str, name: str) -> bool:
    """Whether ``name`` occurs in ``blob`` as a whole word."""
    return re.search(rf"(?<![A-Za-z0-9_]){re.escape(name)}(?![A-Za-z0-9_])", blob) is not None


def audit_document(
    repository: pathlib.Path,
    head: str,
    files: set[str],
    directories: set[str],
    removed: RemovedOwners,
    ignore_rules: HeadIgnoreRules,
    relative: str,
    text: str,
) -> tuple[list[str], list[str], int]:
    """Check one document's citations; return its refusals, its notices and its count."""
    newlines = [index for index, character in enumerate(text) if character == "\n"]
    refusals: list[str] = []
    notices: list[str] = []
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

    for start, paragraph in paragraphs(without_fenced_blocks(text)):
        records = [
            Record(match, line_of(start + match.start()), files, directories, removed)
            for match in BACKTICKED.finditer(paragraph)
        ]
        for record in records:
            if record.path_shape is None:
                continue
            body = record.path_body or ""
            citations += 1
            if record.path_shape == "glob":
                pattern = glob_pattern(body)
                candidates = files | directories
                if pattern.endswith("/"):
                    # A directory-family glob (`docs/runbooks/*/`) names directories:
                    # tree paths carry no trailing slash, so the slash comes off before
                    # matching and only directory entries are candidates. The citation
                    # as written stays in the diagnostics.
                    pattern = pattern.rstrip("/")
                    candidates = directories
                present = any(
                    glob_matches(pattern, path) for path in candidates
                )
                absent = f"the cited glob `{body}` matches no entry of {head[:12]}"
            elif record.path_shape == "dir":
                present = body in directories
                absent = (
                    f"the cited path `{body}` names a file, not a directory"
                    if body in files
                    else f"the cited path `{body}` is in no entry of {head[:12]}"
                )
            else:
                present = body in files | directories
                absent = f"the cited path `{body}` is in no entry of {head[:12]}"
            if present:
                continue
            rule = ignore_rules.covering_rule(body)
            if rule is None:
                refusals.append(
                    f"{relative}:{record.line}: {absent} and no ignore rule covers it; cite a "
                    "path this revision's tree holds, or pin the historical one as "
                    "`git show <rev>:<path>`"
                )
            else:
                notices.append(
                    f"{relative}:{record.line}: {absent}; the ignore rule {rule} covers it, "
                    "so the committed tree cannot verify it"
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
            if body not in files:
                # The path arm above reports a path the head revision does not hold (a
                # missing path, named, at exit 1), and an ignore-covered path has no
                # blob to search: neither may abort the run as an unusable range.
                continue
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
    return refusals, notices, citations


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
    removed = RemovedOwners(repository, head)
    with tempfile.TemporaryDirectory(prefix="citation-ignore-rules-") as scratch:
        try:
            ignore_rules = HeadIgnoreRules(repository, head, files, pathlib.Path(scratch))
        except (IgnoreQueryRefused, revision_scope.DiffRefused) as refusal:
            print(f"{PROGRAM} refused: {refusal}", file=sys.stderr)
            return 2
        refusals: list[str] = []
        notices: list[str] = []
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
                document_refusals, document_notices, document_citations = audit_document(
                    repository, head, files, directories, removed, ignore_rules, relative,
                    payload.decode("utf-8", "surrogateescape"),
                )
            except (IgnoreQueryRefused, revision_scope.DiffRefused) as refusal:
                print(f"{PROGRAM} refused: {refusal}", file=sys.stderr)
                return 2
            refusals += document_refusals
            notices += document_notices
            citations += document_citations
    for notice in notices:
        print(f"{PROGRAM}: note: {notice}")
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
