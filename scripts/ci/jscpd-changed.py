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
tool, and ``.jscpd.json`` at the repository root supplies the obligatory scope: the
dependency/build trees are never scanned, and the detection mode is pinned.

Refused are *new* duplications only (D16's changed-file boundary, read strictly): a
clone is refused when one of its occurrences holds a run of at least ``--min-lines``
consecutive lines the range added whose content that file did not already hold at the
comparison base, or a run of that length the range inserted that the same file did not
delete elsewhere (a copy, not a move). A clone that only re-covers lines a reformat
rewrote - the mechanical ``mix format`` pass re-touches every line of the files it
reformats - is reported as tolerated and does not fail the range. Renames follow their
base path, so a moved file is compared against the content it came from.

The limits are explicit and load-bearing, because jscpd's defaults (1000 lines,
100kb) silently skip the largest files: ``--min-lines 10 --min-tokens 50`` are the
audited thresholds and ``--max-lines 5000 --max-size 1mb`` raise the size ceiling.
A changed file that carries added lines but was not scanned anyway (an unknown
format, a path outside the configured scope, a file below the line threshold, or one
still above the ceiling) is reported as a notice, never silently skipped - and a run
that scanned no source file at all is refused instead of passing vacuously.

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
    """What a revision range does to each path's lines.

    ``added`` holds the ranges of lines the range writes; ``inserted`` narrows them to
    the lines a hunk adds beyond the ones it replaces - a pure insertion, or the surplus
    of a mixed hunk (the shape a copied block takes); ``deleted`` holds the base lines
    the range removes, so an inserted block can be told from a block the same file
    merely moved.
    """

    def __init__(self) -> None:
        self.added: dict[str, list[tuple[int, int]]] = {}
        self.inserted: dict[str, list[tuple[int, int]]] = {}
        self.deleted: dict[str, str] = {}


def parse_target(field: str) -> str | None:
    if field == "/dev/null":
        return None
    field = field.rstrip("\t")
    if field.startswith('"'):
        raise revision_scope.DiffRefused(f"a diff path is still quoted by git: {field}")
    return field[2:] if len(field) > 1 and field[1] == "/" else field


def range_edits(repository: pathlib.Path, base: str, head: str) -> RangeEdits:
    """Parse ``base..head`` into added ranges, pure insertions and deleted lines.

    ``--no-renames`` keeps a rename a deletion plus an addition, and ``--unified=0``
    keeps every hunk header a bare range. A binary file reports no hunk at all, so it
    keeps its path with no ranges and its lines are never matched.

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
    old_path: str | None = None
    removed: list[str] = []
    base_left = 0
    head_left = 0

    def flush() -> None:
        if removed and old_path is not None:
            edits.deleted[old_path] = "\n".join([edits.deleted.get(old_path, ""), *removed])
        removed.clear()

    for line in completed.stdout.decode("utf-8", "replace").splitlines():
        if base_left > 0 or head_left > 0:
            if line.startswith("-"):
                removed.append(line[1:])
                base_left -= 1
            elif line.startswith("+"):
                head_left -= 1
            continue
        if line.startswith("--- "):
            flush()
            old_path = parse_target(line[4:])
        elif line.startswith("+++ "):
            path = parse_target(line[4:])
            if path is not None:
                edits.added.setdefault(path, [])
                edits.inserted.setdefault(path, [])
        elif line.startswith("@@"):
            match = HUNK_HEADER.match(line)
            if match is None:
                continue
            _, base_count, head_start, head_count = (int(match.group(index) or "1") for index in (1, 2, 3, 4))
            base_left, head_left = base_count, head_count
            if head_count > 0 and path is not None:
                edits.added[path].append((head_start, head_start + head_count - 1))
                if base_count == 0:
                    edits.inserted[path].append((head_start, head_start + head_count - 1))
                elif head_count > base_count:
                    edits.inserted[path].append((head_start + base_count, head_start + head_count - 1))
    flush()
    return edits


def materialize_head(repository: pathlib.Path, head: str, corpus: pathlib.Path) -> None:
    archive = corpus.parent / "head.tar"
    completed = revision_scope.run_git(repository, "archive", "--format=tar", f"--output={archive}", head)
    if completed.returncode != 0:
        raise CorpusRefused(f"cannot archive {head}: {completed.stderr.decode('utf-8', 'replace').strip()}")
    with tarfile.open(archive) as stream:
        for member in stream.getmembers():
            relative = pathlib.PurePosixPath(member.name)
            if relative.is_absolute() or ".." in relative.parts:
                raise CorpusRefused(f"the archive of {head} holds an unpackable member {member.name}")
        stream.extractall(corpus, filter="data")
    archive.unlink()


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
        raise CorpusRefused(
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


class Occurrence:
    def __init__(self, head_lines: list[str], base_text: str, edits: RangeEdits, path: str, span: tuple[int, int]):
        self.window = list(range(span[0], min(span[1], len(head_lines)) + 1))
        self.lines = head_lines
        self.content_new = [
            overlaps(edits.added.get(path, []), line, line) and "".join(head_lines[line - 1].split()) not in base_text
            for line in self.window
        ]
        self.inserted = [overlaps(edits.inserted.get(path, []), line, line) for line in self.window]

    def new_content_runs(self) -> list[tuple[int, int]]:
        return [(self.window[start], self.window[end]) for start, end in runs(self.content_new, MIN_LINES)]

    def copied_runs(self, squashed_deleted: str) -> list[tuple[int, int]]:
        copied = []
        for start, end in runs(self.inserted, MIN_LINES):
            block = "".join("".join(self.lines[line - 1].split()) for line in self.window[start : end + 1])
            if block not in squashed_deleted:
                copied.append((self.window[start], self.window[end]))
        return copied


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
    scope = blob_text(repository, head, CONFIG_FILE)
    if not scope:
        return refuse(f"{CONFIG_FILE} is missing at {head[:12]} (the committed jscpd scope config)")
    try:
        base, fallback = revision_scope.resolve_base(repository, arguments.base, head, PROGRAM)
    except revision_scope.ComparisonBaseRefused as refusal:
        return refuse(str(refusal))
    if base is None:
        return refuse(f"cannot resolve a comparison base ({arguments.base!r} against origin/main or HEAD^)")
    if fallback:
        base = revision_scope.narrow_to_merge_base(repository, base, head)
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
            document = load_report(report_directory)
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
            squashed_base: dict[str, str] = {}
            squashed_deletions: dict[str, str] = {}

            def read_head_lines(path: str) -> list[str]:
                if path not in head_lines_by_path:
                    try:
                        head_lines_by_path[path] = (corpus / path).read_text(encoding="utf-8").splitlines()
                    except (OSError, UnicodeDecodeError) as error:
                        raise CorpusRefused(f"cannot read the scanned file {path}: {error}") from error
                return head_lines_by_path[path]

            def whitespace_free_base(source: str) -> str:
                if source not in squashed_base:
                    squashed_base[source] = "".join(blob_text(repository, base, source).split())
                return squashed_base[source]

            def whitespace_free_deletions(source: str) -> str:
                if source not in squashed_deletions:
                    squashed_deletions[source] = "".join(edits.deleted.get(source, "").split())
                return squashed_deletions[source]

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
                        whitespace_free_base(source),
                        edits,
                        path,
                        (min(side["start"], side["end"]), max(side["start"], side["end"])),
                    )
                    reached.append(
                        (
                            side,
                            occurrence.new_content_runs(),
                            occurrence.copied_runs(whitespace_free_deletions(source)),
                        )
                    )
                if not reached:
                    continue
                reasons = [
                    (side["name"], reason, span)
                    for side, new_spans, copied_spans in reached
                    for reason, spans in (("new content", new_spans), ("copied, not moved", copied_spans))
                    for span in spans
                ]
                if reasons:
                    refusals.append((duplicate, sides, reasons))
                else:
                    tolerated.append((duplicate, sides))
        except (revision_scope.DiffRefused, CorpusRefused) as refusal:
            return refuse(str(refusal))
        notices = [path for path in changed if edits.added.get(path) and path not in scanned]
        for path in notices:
            print(
                f"{PROGRAM} notice: {path} carries added lines but jscpd did not scan it "
                "(format, scope or size limits); clones inside it are not checked",
                file=sys.stderr,
            )
        for duplicate, sides in tolerated:
            first, second = sides["firstFile"], sides["secondFile"]
            print(
                f"{PROGRAM}: tolerated: {first['name']} [{first['start']}-{first['end']}] duplicates "
                f"{second['name']} [{second['start']}-{second['end']}] ({duplicate.get('lines')} lines, "
                f"{duplicate.get('tokens')} tokens); no run of {MIN_LINES}+ added lines in it holds new "
                "content or an unmoved copy"
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
