#!/usr/bin/env python3
"""Refuse code clones a revision range adds among the files it changes.

This is the changed-file duplication gate (program decision D16): for a revision
range it selects every file added, modified or retyped, and refuses when the lines
the range *adds* to one of those files form a clone with another file. The clone
search runs over the whole head revision, so a changed file copying a block out of
an untouched file is caught; a clone that lives entirely in untouched files, or in
lines the range did not add, is deliberately not refused (repo-wide duplication is a
trend number, never a gate).

The corpus is the ``--head`` revision's own tree, materialized from Git with
``git archive`` - never the working tree - so a dirty checkout cannot change which
clones are seen. jscpd is this repository's pinned devDependency
(``node_modules/jscpd``, installed by ``pnpm install``); the gate never fetches a
tool, and ``.jscpd.json`` supplies the obligatory scope: the dependency/build trees
are never scanned, and the detection mode is pinned. The scope is read from the
comparison base - falling back to the head only when the base holds none - so a range
that rewrites the scope cannot exempt itself.

Refused are *new* duplications only (D16's changed-file boundary, read strictly): a
clone is refused when one of its occurrences holds a run of at least ``--min-lines``
consecutive added lines the file did not already hold as often at the comparison base.
``held`` counts line-boundary-aligned occurrences of the line's whitespace-free text
in the same path's base revision, so a reflow (a joined or split line) still matches
while a comment marker or any other neighbouring token does not: uncommenting a block
the file held only commented is new content, and so is a second copy of a block the
file already held once. A clone that only re-covers lines a reformat rewrote - the
mechanical ``mix format`` pass re-touches every line of the files it reformats - is
reported as tolerated and does not fail the range. Renames follow their base path, so
a moved file is compared against the content it came from.

The limits are explicit and load-bearing, because jscpd's defaults (1000 lines,
100kb) silently skip the largest files: ``--min-lines 10 --min-tokens 50`` are the
audited thresholds and ``--max-lines 5000 --max-size 1mb`` raise the size ceiling.
A changed file that carries added lines but was not scanned anyway (an unknown
format, a path outside the configured scope, a file below the line threshold, or one
still above the ceiling) is reported as a notice, never silently skipped. A run that
scanned no source file at all is refused instead of passing vacuously - unless every
changed file with added lines is shorter than ``--min-lines``, because no clone of
that length can live in them.

Changed-file selection is computed against an explicit comparison base, never from
the checkout's last commit: CI passes the pull-request base commit, or the pushed
``before`` commit for a push. The base fallback and its narrowing are
``revision_scope``'s, shared with the other changed-file gates. Addresses and line
ranges come from the ``--head`` revision; the clone payload comes from the head
tree jscpd scanned.

Usage::

    python scripts/ci/jscpd-changed.py --base <commit> [--head <commit>] [--repo <path>]

Exit codes: 0 when no added line of a changed file is part of a clone (also when
the range changes no file), 1 when at least one clone is refused, 2 when the range,
the corpus, the pinned jscpd or its report cannot be used.
"""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

import revision_scope

SCRIPT_DIRECTORY = pathlib.Path(__file__).resolve().parent
GATE_REPOSITORY = SCRIPT_DIRECTORY.parents[1]
JSCPD_PACKAGE = GATE_REPOSITORY / "node_modules" / "jscpd"
CONFIG_FILE = ".jscpd.json"
JSCPD_LIMITS = ("--min-lines", "10", "--min-tokens", "50", "--max-lines", "5000", "--max-size", "1mb")
REPORT_NAME = "jscpd-report.json"
HUNK_HEADER = re.compile(r"^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@")
MIN_LINES = int(JSCPD_LIMITS[JSCPD_LIMITS.index("--min-lines") + 1])
PROGRAM = "jscpd-changed"


class CorpusRefused(Exception):
    pass


class ReportMissing(CorpusRefused):
    """jscpd selected no source file, so no report exists to read."""


def jscpd_entrypoint(package: pathlib.Path) -> pathlib.Path:
    manifest = package / "package.json"
    if not manifest.is_file():
        raise CorpusRefused(f"{package} is missing; the pinned jscpd is not installed (run pnpm install)")
    try:
        document = json.loads(manifest.read_text(encoding="utf-8"))
    except ValueError as error:
        raise CorpusRefused(f"{manifest} is not valid JSON: {error}") from error
    binary = document.get("bin")
    if isinstance(binary, dict):
        binary = binary.get("jscpd")
    if not isinstance(binary, str) or not binary:
        raise CorpusRefused(f"{manifest} names no jscpd binary")
    entrypoint = package / binary
    if not entrypoint.is_file():
        raise CorpusRefused(f"{entrypoint} is missing")
    return entrypoint


class RangeEdits:
    """The head lines a revision range adds, per path.

    A pure insertion and the replacement half of a rewrite hunk are both added lines:
    the refusal rule counts how often the file held a line's content before, not how
    the hunk that wrote it was shaped.
    """

    def __init__(self) -> None:
        self.added: dict[str, list[tuple[int, int]]] = {}


def parse_target(field: str) -> str | None:
    if field == "/dev/null":
        return None
    field = field.rstrip("\t")
    if field.startswith('"'):
        raise revision_scope.DiffRefused(f"a diff path is still quoted by git: {field}")
    return field[2:] if len(field) > 1 and field[1] == "/" else field


def range_edits(repository: pathlib.Path, base: str, head: str) -> RangeEdits:
    """Parse ``base..head`` into the head lines each path adds.

    ``--no-renames`` keeps a rename a deletion plus an addition, and ``--unified=0``
    keeps every hunk header a bare range. A binary file reports no hunk at all, so it
    keeps its path with no ranges and its lines are never matched.

    ``--no-ext-diff``/``--no-textconv`` keep an inherited ``GIT_EXTERNAL_DIFF``,
    ``diff.external`` or text-conversion filter from replacing the patch this parser
    reads: either would silently drop added lines and let the range pass.

    C-quoting is off and the prefixes are explicit, so a header's path field is the
    report's own raw path; a field git still quotes is refused, never silently skipped.
    Only a line outside a hunk may be a header: a hunk is read by its counts, because a
    content line may itself begin with ``--- ``/``+++ ``.
    """
    completed = revision_scope.run_git(
        repository,
        "-c",
        "core.quotePath=false",
        "diff",
        "--unified=0",
        "--no-renames",
        "--no-color",
        "--no-ext-diff",
        "--no-textconv",
        "--src-prefix=a/",
        "--dst-prefix=b/",
        base,
        head,
    )
    if completed.returncode != 0:
        raise revision_scope.DiffRefused(
            f"cannot diff {base}..{head}: {completed.stderr.decode('utf-8', 'replace').strip()}"
        )
    edits = RangeEdits()
    path: str | None = None
    base_left = 0
    head_left = 0

    for line in completed.stdout.decode("utf-8", "replace").splitlines():
        if base_left > 0 or head_left > 0:
            if line.startswith("-"):
                base_left -= 1
            elif line.startswith("+"):
                head_left -= 1
            continue
        if line.startswith("+++ "):
            path = parse_target(line[4:])
            if path is not None:
                edits.added.setdefault(path, [])
        elif line.startswith("@@"):
            match = HUNK_HEADER.match(line)
            if match is None:
                continue
            _, base_count, head_start, head_count = (int(match.group(index) or "1") for index in (1, 2, 3, 4))
            base_left, head_left = base_count, head_count
            if head_count > 0 and path is not None:
                edits.added[path].append((head_start, head_start + head_count - 1))
    return edits


def materialize_head(repository: pathlib.Path, head: str, corpus: pathlib.Path) -> None:
    """Unpack the head tree into ``corpus``; an unpackable member refuses the corpus.

    ``filter="data"`` rejects a member tarfile considers unsafe - an absolute or
    escaping path, an absolute-target symlink - and Git stores a symlink as a plain
    blob, so the tree can legally hold one. The unpack therefore runs inside the
    refusal: the gate reports the unusable corpus (exit 2) instead of a traceback.
    """
    archive = corpus.parent / "head.tar"
    completed = revision_scope.run_git(repository, "archive", "--format=tar", f"--output={archive}", head)
    if completed.returncode != 0:
        raise CorpusRefused(f"cannot archive {head}: {completed.stderr.decode('utf-8', 'replace').strip()}")
    try:
        with tarfile.open(archive) as stream:
            for member in stream.getmembers():
                relative = pathlib.PurePosixPath(member.name)
                if relative.is_absolute() or ".." in relative.parts:
                    raise CorpusRefused(f"the archive of {head} holds an unpackable member {member.name}")
            stream.extractall(corpus, filter="data")
        archive.unlink()
    except (tarfile.TarError, OSError) as error:
        raise CorpusRefused(f"cannot materialize the corpus of {head}: {error}") from error


def run_jscpd(
    entrypoint: pathlib.Path, corpus: pathlib.Path, config: pathlib.Path, report_directory: pathlib.Path
) -> None:
    node = shutil.which("node")
    if node is None:
        raise CorpusRefused("node is not on PATH; the pinned jscpd cannot run")
    command = [
        node,
        str(entrypoint),
        ".",
        "--config",
        str(config),
        *JSCPD_LIMITS,
        "--reporters",
        "json",
        "--output",
        str(report_directory),
        "--silent",
        "--no-colors",
    ]
    completed = subprocess.run(command, cwd=corpus, capture_output=True, check=False)
    if completed.returncode != 0:
        output = (completed.stderr or completed.stdout).decode("utf-8", "replace").strip()
        raise CorpusRefused(
            f"jscpd exited {completed.returncode}: {output.splitlines()[-1] if output else 'no output'}"
        )


def scanned_name(name: str) -> str:
    normalized = name.replace("\\", "/")
    return normalized[2:] if normalized.startswith("./") else normalized


def load_report(report_directory: pathlib.Path) -> dict:
    report = report_directory / REPORT_NAME
    if not report.is_file():
        raise ReportMissing(
            "jscpd wrote no report: it recognized no source file in the corpus, so this gate cannot "
            "testify about this revision"
        )
    try:
        document = json.loads(report.read_text(encoding="utf-8"))
    except ValueError as error:
        raise CorpusRefused(f"{report} is not valid JSON: {error}") from error
    if not isinstance(document, dict) or not isinstance(document.get("duplicates"), list):
        raise CorpusRefused(f"{report} does not hold a duplicate list")
    return document


def too_short_to_clone(corpus: pathlib.Path, path: str) -> bool:
    """Whether the head file is shorter than ``--min-lines``, so no clone can live in it.

    jscpd's walker skips such a file, and a clone of ``--min-lines`` consecutive lines
    cannot fit in fewer lines than that. A file the corpus cannot read is not too short:
    the gate must not pass on a path it could not measure.
    """
    try:
        lines = (corpus / path).read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeDecodeError):
        return False
    return len(lines) < MIN_LINES


def clone_report(duplicate: dict) -> dict[str, dict[str, int | str]]:
    sides = {}
    for key in ("firstFile", "secondFile"):
        side = duplicate.get(key)
        if not isinstance(side, dict) or not isinstance(side.get("name"), str):
            raise CorpusRefused(f"a duplicate entry holds no usable {key}")
        start, end = side.get("start"), side.get("end")
        if not isinstance(start, int) or not isinstance(end, int) or start < 1 or end < 1:
            raise CorpusRefused(f"a duplicate entry holds no usable {key} range")
        sides[key] = {
            "name": scanned_name(side["name"]),
            "start": start,
            "end": end,
        }
    return sides


def overlaps(ranges: list[tuple[int, int]], start: int, end: int) -> bool:
    return any(start <= range_end and range_start <= end for range_start, range_end in ranges)


def blob_text(repository: pathlib.Path, revision: str, relative: str) -> str:
    """One path's bytes at ``revision``, empty when that revision does not hold it."""
    blob = revision_scope.run_git(repository, "show", f"{revision}:{relative}")
    if blob.returncode != 0:
        return ""
    return blob.stdout.decode("utf-8", "replace")


def renamed_base_paths(repository: pathlib.Path, base: str, head: str) -> dict[str, str]:
    completed = revision_scope.run_git(repository, "diff", "--name-status", "--find-renames", "-z", base, head)
    if completed.returncode != 0:
        raise revision_scope.DiffRefused(
            f"cannot read renames between {base} and {head}: {completed.stderr.decode('utf-8', 'replace').strip()}"
        )
    fields = [field.decode("utf-8", "surrogateescape") for field in completed.stdout.split(b"\0") if field]
    renamed: dict[str, str] = {}
    index = 0
    while index < len(fields):
        status = fields[index]
        if status[:1] in {"R", "C"}:
            if index + 2 >= len(fields):
                raise revision_scope.DiffRefused("unexpected git diff rename shape")
            renamed[fields[index + 2]] = fields[index + 1]
            index += 3
        else:
            index += 2
    return renamed


def runs(flags: list[bool], minimum: int) -> list[tuple[int, int]]:
    """The inclusive index spans of every run of at least ``minimum`` true flags."""
    found: list[tuple[int, int]] = []
    start: int | None = None
    for index, flag in enumerate([*flags, False]):
        if flag and start is None:
            start = index
        elif not flag and start is not None:
            if index - start >= minimum:
                found.append((start, index - 1))
            start = None
    return found


class TextIndex:
    """One file's whitespace-free text, its line-boundary offsets and a count cache.

    ``line_occurrences`` answers how often the file holds a line's content as a whole
    number of its own lines: an occurrence must start at a line start and end at a line
    end. A reflow therefore still matches - a joined line's text spans the two base
    lines it came from, and a split line's text is inside its base line - while a
    comment marker or any other neighbouring token keeps the occurrence from counting:
    a block the base held only commented is not content the file "already held".
    """

    def __init__(self, lines: list[str]) -> None:
        self._lines = ["".join(line.split()) for line in lines]
        self._text = "".join(self._lines)
        self._starts = {0}
        self._ends: set[int] = set()
        position = 0
        for line in self._lines:
            position += len(line)
            self._starts.add(position)
            self._ends.add(position)
        self._counts: dict[str, int] = {}

    def line_occurrences(self, line: str) -> int:
        needle = "".join(line.split())
        if not needle:
            return 0
        if needle not in self._counts:
            count = 0
            position = self._text.find(needle)
            while position != -1:
                if position in self._starts and position + len(needle) in self._ends:
                    count += 1
                position = self._text.find(needle, position + 1)
            self._counts[needle] = count
        return self._counts[needle]


class Occurrence:
    """One clone occurrence: the head lines it covers, against its base revision.

    A line is *new* when the range added it and the file held fewer boundary-aligned
    occurrences of its content at the comparison base than the head holds. A copy of a
    block the file already carried is new because its multiplicity grew; a line a
    reformat merely rewrote is not, because its content is still there.
    """

    def __init__(
        self,
        head_lines: list[str],
        head_index: TextIndex,
        base_index: TextIndex,
        edits: RangeEdits,
        path: str,
        span: tuple[int, int],
    ):
        self.window = list(range(span[0], min(span[1], len(head_lines)) + 1))
        self.lines = head_lines
        self.absent: list[bool] = []
        self.new: list[bool] = []
        for line in self.window:
            text = head_lines[line - 1]
            base_count = base_index.line_occurrences(text)
            added = overlaps(edits.added.get(path, []), line, line)
            self.absent.append(base_count == 0)
            self.new.append(added and base_count < head_index.line_occurrences(text))

    def refused_runs(self) -> list[tuple[str, tuple[int, int]]]:
        refused = []
        for start, end in runs(self.new, MIN_LINES):
            reason = "new content" if all(self.absent[start : end + 1]) else "copied, not moved"
            refused.append((reason, (self.window[start], self.window[end])))
        return refused


def refuse(message: str) -> int:
    print(f"{PROGRAM} refused: {message}", file=sys.stderr)
    return 2


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Refuse code clones a revision range adds to the files it changes.",
    )
    parser.add_argument("--repo", default=".", metavar="PATH", help="repository to inspect (default: .)")
    parser.add_argument(
        "--base",
        metavar="COMMIT",
        help="comparison base; empty or all-zero falls back to origin/main, then the head's first parent",
    )
    parser.add_argument("--head", default="HEAD", metavar="COMMIT", help="revision to scan (default: HEAD)")
    return parser


def governing_scope(repository: pathlib.Path, base: str, head: str) -> str:
    """The scope config the range is judged by: the comparison base's, never its own.

    A range that rewrites ``.jscpd.json`` would otherwise choose what is scanned and
    could exempt its own files; the base revision's scope decides such a range, and the
    edit takes effect from the next range on. Only a range that first introduces the
    config is judged by the revision that adds it.
    """
    at_base = blob_text(repository, base, CONFIG_FILE)
    at_head = blob_text(repository, head, CONFIG_FILE)
    if at_base:
        if at_base != at_head:
            print(
                f"{PROGRAM} notice: this range rewrites {CONFIG_FILE}; the scope of {base[:12]} "
                "governs it, and the edit applies from the next range",
                file=sys.stderr,
            )
        return at_base
    if at_head:
        print(
            f"{PROGRAM} notice: {CONFIG_FILE} first appears in this range; it supplies the scope it is judged by",
            file=sys.stderr,
        )
    return at_head


def main(argv: list[str]) -> int:
    arguments = build_parser().parse_args(argv)
    repository = pathlib.Path(arguments.repo).resolve()
    if not (repository / ".git").exists():
        return refuse(f"{repository} is not a repository")
    try:
        entrypoint = jscpd_entrypoint(JSCPD_PACKAGE)
    except CorpusRefused as refusal:
        return refuse(str(refusal))
    head = revision_scope.resolve_commit(repository, arguments.head)
    if head is None:
        return refuse(f"cannot resolve the head {arguments.head!r}")
    try:
        base, fallback = revision_scope.resolve_base(repository, arguments.base, head, PROGRAM)
    except revision_scope.ComparisonBaseRefused as refusal:
        return refuse(str(refusal))
    if base is None:
        return refuse(f"cannot resolve a comparison base ({arguments.base!r} against origin/main or HEAD^)")
    if fallback:
        base = revision_scope.narrow_to_merge_base(repository, base, head)
    scope = governing_scope(repository, base, head)
    if not scope:
        return refuse(f"{CONFIG_FILE} is missing at {base[:12]} and {head[:12]} (the committed jscpd scope config)")
    try:
        changed = revision_scope.changed_paths(repository, base, head, ())
        edits = range_edits(repository, base, head)
    except revision_scope.DiffRefused as refusal:
        return refuse(str(refusal))
    if not changed:
        print(f"{PROGRAM}: no file added or modified between {base[:12]} and {head[:12]}; nothing to check")
        return 0
    working = pathlib.Path(tempfile.mkdtemp(prefix="jscpd-changed."))
    try:
        corpus = working / "corpus"
        corpus.mkdir()
        report_directory = working / "report"
        report_directory.mkdir()
        try:
            materialize_head(repository, head, corpus)
            config = working / CONFIG_FILE
            config.write_text(scope, encoding="utf-8")
            run_jscpd(entrypoint, corpus, config, report_directory)
            try:
                document = load_report(report_directory)
            except ReportMissing:
                citable = [path for path in changed if edits.added.get(path) and not too_short_to_clone(corpus, path)]
                if citable:
                    raise
                print(
                    f"{PROGRAM}: {len(changed)} changed file(s), every added line in a file shorter than "
                    f"{MIN_LINES} lines; no clone of {MIN_LINES}+ lines can live in them, so there is "
                    "nothing to check"
                )
                return 0
        except CorpusRefused as refusal:
            return refuse(str(refusal))

        statistics = document.get("statistics") or {}
        formats = statistics.get("formats") or {}
        scanned = {
            scanned_name(name)
            for format_statistics in formats.values()
            for name in (format_statistics.get("sources") or {})
        }
        total = statistics.get("total") or {}
        if not scanned:
            return refuse(
                f"jscpd scanned no source file in the corpus of {len(changed)} changed file(s); "
                "the gate cannot testify about this revision"
            )

        refusals = []
        tolerated = []
        try:
            renamed = renamed_base_paths(repository, base, head)
            head_lines_by_path: dict[str, list[str]] = {}
            head_indexes: dict[str, TextIndex] = {}
            base_indexes: dict[str, TextIndex] = {}

            def read_head_lines(path: str) -> list[str]:
                if path not in head_lines_by_path:
                    try:
                        head_lines_by_path[path] = (corpus / path).read_text(encoding="utf-8").splitlines()
                    except (OSError, UnicodeDecodeError) as error:
                        raise CorpusRefused(f"cannot read the scanned file {path}: {error}") from error
                return head_lines_by_path[path]

            def head_index(path: str) -> TextIndex:
                if path not in head_indexes:
                    head_indexes[path] = TextIndex(read_head_lines(path))
                return head_indexes[path]

            def base_index(source: str) -> TextIndex:
                if source not in base_indexes:
                    base_indexes[source] = TextIndex(blob_text(repository, base, source).splitlines())
                return base_indexes[source]

            for duplicate in document["duplicates"]:
                sides = clone_report(duplicate)
                reached = []
                for side in sides.values():
                    path = side["name"]
                    if path not in edits.added or not overlaps(edits.added[path], side["start"], side["end"]):
                        continue
                    source = renamed.get(path, path)
                    occurrence = Occurrence(
                        read_head_lines(path),
                        head_index(path),
                        base_index(source),
                        edits,
                        path,
                        (min(side["start"], side["end"]), max(side["start"], side["end"])),
                    )
                    reached.append((side, occurrence.refused_runs()))
                if not reached:
                    continue
                reasons = [(side["name"], reason, span) for side, spans in reached for reason, span in spans]
                if reasons:
                    refusals.append((duplicate, sides, reasons))
                else:
                    tolerated.append((duplicate, sides))
        except (revision_scope.DiffRefused, CorpusRefused) as refusal:
            return refuse(str(refusal))
        notices = [path for path in changed if edits.added.get(path) and path not in scanned]
        for path in notices:
            reason = (
                f"it is shorter than {MIN_LINES} lines, so no clone of {MIN_LINES}+ lines can live in it"
                if too_short_to_clone(corpus, path)
                else "its format, path or size is outside the configured scope"
            )
            print(
                f"{PROGRAM} notice: {path} carries added lines but jscpd did not scan it "
                f"({reason}); clones inside it are not checked",
                file=sys.stderr,
            )
        for duplicate, sides in tolerated:
            first, second = sides["firstFile"], sides["secondFile"]
            print(
                f"{PROGRAM}: tolerated: {first['name']} [{first['start']}-{first['end']}] duplicates "
                f"{second['name']} [{second['start']}-{second['end']}] ({duplicate.get('lines')} lines, "
                f"{duplicate.get('tokens')} tokens); no run of {MIN_LINES}+ added lines in it holds "
                "content the file did not already hold"
            )
        if refusals:
            for duplicate, sides, reasons in refusals:
                first, second = sides["firstFile"], sides["secondFile"]
                print(
                    f"{PROGRAM} refused: {first['name']} [{first['start']}-{first['end']}] duplicates "
                    f"{second['name']} [{second['start']}-{second['end']}] "
                    f"({duplicate.get('lines')} lines, {duplicate.get('tokens')} tokens); a run of "
                    f"{MIN_LINES}+ lines at "
                    + ", ".join(f"{path}:{span[0]}-{span[1]} ({reason})" for path, reason, span in reasons),
                    file=sys.stderr,
                )
            print(
                f"{PROGRAM} refused: {len(refusals)} new clone(s) among the lines this range adds "
                f"({len(tolerated)} clone(s) only re-covered reformatted lines); deduplicate them (or extend "
                "the shared helper) instead of copying, and do not lower the limits to shrink the count",
                file=sys.stderr,
            )
            return 1
        print(
            f"{PROGRAM}: {len(changed)} changed file(s), {len(scanned)} scanned source(s), "
            f"{total.get('clones', 0)} clone(s) in the corpus, {len(tolerated)} tolerated among the added "
            "lines, none new"
        )
        return 0
    finally:
        shutil.rmtree(working, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
