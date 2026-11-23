#!/usr/bin/env python3
"""Fail when the Control Credo strict analysis breaches its committed baseline.

The strict count is the ratchet: `credo-strict-baseline.json` records the measured
issue count (per category), the analyzed-file count and the check count, and the
baseline may only decrease - every decrease is recorded in the same change as the fix
that causes it. A count above the baseline, a count below a stale baseline (a ceiling
nobody lowered would let a later regression back in), fewer analyzed files or fewer
checks than the baseline records, a source file Credo skipped, and any output line of
the report this parser does not account for all fail: Credo's own exit status is a
bitmask of issue severities rather than a count, so it is reported, never compared.

The log is the whole `mix credo --strict` output (both streams) written under
`control/.credo.exs`, whose explicit check list and `strict: true` make the printed
report complete: every issue is rendered, in one block per category, and the closing
report carries the analyzed-file count, the check count and the per-category wording.
Lines before the `Checking N source files ...` banner are `mix`'s own output and are
tolerated; from that banner on every line must be accounted for, so a changed output
shape (a new Credo version, an added warning block, a coloured log) refuses instead of
silently under-reporting the count.

Usage:
  assert-credo-strict-baseline.py --log PATH [--baseline PATH]
"""

import argparse
import json
import pathlib
import re
import sys

BASELINE_KEYS = ("revision", "issues", "files_analyzed", "checks_run", "by_category")

CATEGORY_ORDER = ("design", "readability", "refactor", "warning", "consistency")
CATEGORY_TITLE = {
    "design": "Software Design",
    "readability": "Code Readability",
    "refactor": "Refactoring opportunities",
    "warning": "Warnings - please take a look",
    "consistency": "Consistency",
}
CATEGORY_TAG = {
    "design": "D",
    "readability": "R",
    "refactor": "F",
    "warning": "W",
    "consistency": "C",
}
CATEGORY_WORDING = {
    "consistency": ("consistency issue", "consistency issues"),
    "warning": ("warning", "warnings"),
    "refactor": ("refactoring opportunity", "refactoring opportunities"),
    "readability": ("code readability issue", "code readability issues"),
    "design": ("software design suggestion", "software design suggestions"),
}
SUMMARY_ORDER = ("consistency", "warning", "refactor", "readability", "design")

CHECKING = re.compile(r"^Checking (?P<files>\d+) source files?(?: \(this might take a while\))? \.\.\.$")
NO_FILES = "No files found!"
SECTION_HEADER = re.compile(
    r"^  (?P<title>Software Design|Code Readability|Refactoring opportunities"
    r"|Warnings - please take a look|Consistency) +$"
)
SECTION_EDGE = re.compile(r"^┃ *$")
ISSUE = re.compile(r"^┃ \[(?P<tag>[DRFWC])\] [↑↗→↘] ")
CONTINUATION = re.compile(r"^┃")
CRY_FOR_HELP = "Please report incorrect results: https://github.com/rrrene/credo/issues"
ANALYSIS = re.compile(
    r"^Analysis took [\d.]+ seconds? \([\d.]+s to load, [\d.]+s running "
    r"(?P<checks>\d+) checks? on (?P<files>\d+) files?\)$"
)
SUMMARY = re.compile(r"^(?P<scopes>\d+) mods/funs, found (?P<issues>.+)\.$")
SUMMARY_FRAGMENT = re.compile(r"^(?P<count>\d+) (?P<wording>.+)$")
# `--strict` sets `min_priority: -99`, which prints this closing line rather than the priority hint.
CLOSING_HINT = "Use `mix credo explain` to explain issues, `mix credo --help` for options."
SKIPPED_SOURCE_FILES = (
    "Some source files could not be parsed correctly and are excluded:",
    "Some source files were not parsed in the time allotted:",
)


class Refusal(Exception):
    """The log is not a complete, fully accounted Credo strict report."""


class Baseline:
    """The committed baseline artefact: the counts this run must reproduce."""

    def __init__(self, path: pathlib.Path) -> None:
        self.path = path
        try:
            document = json.loads(path.read_text(encoding="utf-8"))
        except OSError as error:
            raise Refusal(f"the baseline {path} is not readable: {error}") from error
        except json.JSONDecodeError as error:
            raise Refusal(f"the baseline {path} is not valid JSON: {error}") from error
        if not isinstance(document, dict):
            raise Refusal(f"the baseline {path} is not a JSON object")
        for key in BASELINE_KEYS:
            if key not in document:
                raise Refusal(f"the baseline {path} carries no {key!r} key")
        for key in ("issues", "files_analyzed", "checks_run"):
            if not isinstance(document[key], int) or isinstance(document[key], bool):
                raise Refusal(f"the baseline {path}: {key!r} must be an integer")
            if document[key] < 1:
                raise Refusal(f"the baseline {path}: {key!r} must be a positive integer")
        by_category = document["by_category"]
        if not isinstance(by_category, dict) or not by_category:
            raise Refusal(f"the baseline {path}: 'by_category' must be a non-empty object")
        for category, count in by_category.items():
            if category not in CATEGORY_ORDER:
                raise Refusal(f"the baseline {path}: unknown category {category!r}")
            if not isinstance(count, int) or isinstance(count, bool) or count < 1:
                raise Refusal(f"the baseline {path}: category {category!r} needs a positive count")
        if sum(by_category.values()) != document["issues"]:
            raise Refusal(f"the baseline {path}: 'by_category' does not sum to 'issues'")
        self.revision = str(document["revision"])
        self.issues = document["issues"]
        self.files_analyzed = document["files_analyzed"]
        self.checks_run = document["checks_run"]
        self.by_category = dict(by_category)


class Report:
    """The counts and per-category split read out of one Credo strict log."""

    def __init__(self, files_analyzed: int, checks_run: int, scopes: int, by_category: dict[str, int]) -> None:
        self.files_analyzed = files_analyzed
        self.checks_run = checks_run
        self.scopes = scopes
        self.by_category = by_category

    @property
    def issues(self) -> int:
        return sum(self.by_category.values())

    def breakdown(self) -> str:
        return ", ".join(
            f"{self.by_category[category]} {CATEGORY_WORDING[category][1]}"
            for category in SUMMARY_ORDER
            if category in self.by_category
        )


def skip_blanks(lines: list[str], cursor: int) -> int:
    while cursor < len(lines) and lines[cursor] == "":
        cursor += 1
    return cursor


def parse_report(log: str) -> Report:
    lines = log.replace("\r\n", "\n").split("\n")
    for number, line in enumerate(lines, 1):
        for marker in SKIPPED_SOURCE_FILES:
            if marker in line:
                raise Refusal(
                    f"line {number}: Credo skipped source file(s), so the analyzed set is incomplete: {line.strip()}"
                )

    anchor = next((index for index, line in enumerate(lines) if CHECKING.match(line) or line == NO_FILES), None)
    if anchor is None:
        raise Refusal("the log holds no `Checking N source files ...` banner: Credo did not run to completion")
    banner = CHECKING.match(lines[anchor])
    banner_files = int(banner["files"]) if banner is not None else 0
    if banner_files == 0:
        raise Refusal(
            "Credo analyzed no source file(s): the analyzed set is empty (.credo.exs's file scope or the run)"
        )

    cursor = skip_blanks(lines, anchor + 1)
    sections: dict[str, int] = {}
    order: list[str] = []
    while cursor < len(lines) and lines[cursor] != CRY_FOR_HELP:
        header = SECTION_HEADER.match(lines[cursor])
        if header is None:
            raise Refusal(f"line {cursor + 1}: unaccounted Credo output: {lines[cursor]!r}")
        title = header["title"]
        category = next(name for name, rendered in CATEGORY_TITLE.items() if rendered == title)
        if order and CATEGORY_ORDER.index(category) <= CATEGORY_ORDER.index(order[-1]):
            raise Refusal(f"line {cursor + 1}: category {title!r} is out of the report's fixed order")
        order.append(category)
        cursor += 1
        if cursor >= len(lines) or not SECTION_EDGE.match(lines[cursor]):
            raise Refusal(f"line {cursor + 1}: category {title!r} has no edge line under its header")
        cursor += 1
        counted = 0
        while cursor < len(lines):
            issue = ISSUE.match(lines[cursor])
            if issue is not None:
                tag = issue["tag"]
                if CATEGORY_TAG[category] != tag:
                    raise Refusal(
                        f"line {cursor + 1}: a [{tag}] issue is listed under {title!r}; the sections are crossed"
                    )
                counted += 1
            elif not CONTINUATION.match(lines[cursor]):
                break
            cursor += 1
        if counted == 0:
            raise Refusal(f"line {cursor + 1}: category {title!r} is rendered with no issue")
        sections[category] = counted
        cursor = skip_blanks(lines, cursor)

    if cursor >= len(lines):
        raise Refusal("the log ends before Credo's closing report")
    cursor = skip_blanks(lines, cursor + 1)
    analysis = ANALYSIS.match(lines[cursor]) if cursor < len(lines) else None
    if analysis is None:
        raise Refusal(f"no `Analysis took ...` line at the end of the report (line {cursor + 1})")
    cursor = skip_blanks(lines, cursor + 1)
    summary = SUMMARY.match(lines[cursor]) if cursor < len(lines) else None
    if summary is None:
        raise Refusal(f"no `N mods/funs, found ...` summary line at the end of the report (line {cursor + 1})")
    cursor = skip_blanks(lines, cursor + 1)
    if cursor >= len(lines) or lines[cursor] != CLOSING_HINT:
        found = lines[cursor] if cursor < len(lines) else "<end of log>"
        raise Refusal(f"the report has no strict-mode closing line (line {cursor + 1}: {found!r})")
    cursor = skip_blanks(lines, cursor + 1)
    if cursor != len(lines):
        raise Refusal(f"line {cursor + 1}: unaccounted Credo output after the report: {lines[cursor]!r}")

    analysis_files = int(analysis["files"])
    if banner_files != analysis_files:
        raise Refusal(f"the banner names {banner_files} source file(s) and the report {analysis_files}")
    summary_split = parse_summary(summary["issues"])
    if summary_split != sections:
        raise Refusal(
            f"the parsed category split {sections} is not the summary's own {summary_split}; "
            "the report holds an issue line this parser did not account for"
        )
    return Report(
        files_analyzed=analysis_files,
        checks_run=int(analysis["checks"]),
        scopes=int(summary["scopes"]),
        by_category=summary_split,
    )


def parse_summary(body: str) -> dict[str, int]:
    if body == "no issues":
        raise Refusal("the report found no issues at all; the configured check set cannot produce that")
    split: dict[str, int] = {}
    previous = -1
    for fragment in body.split(", "):
        match = SUMMARY_FRAGMENT.match(fragment)
        if match is None:
            raise Refusal(f"the summary fragment {fragment!r} is not accounted for")
        wording = match["wording"]
        category = next(
            (name for name, (singular, plural) in CATEGORY_WORDING.items() if wording in (singular, plural)),
            None,
        )
        if category is None:
            raise Refusal(f"the summary fragment {fragment!r} names no known category")
        position = SUMMARY_ORDER.index(category)
        if position <= previous:
            raise Refusal(f"the summary fragment {fragment!r} is out of the summary's fixed order")
        previous = position
        split[category] = int(match["count"])
    return split


def compare(report: Report, baseline: Baseline) -> list[str]:
    failures = []
    if report.files_analyzed < baseline.files_analyzed:
        failures.append(
            f"Credo analyzed {report.files_analyzed} source file(s), below the baseline's {baseline.files_analyzed}: "
            "files were skipped or dropped out of the analyzed set"
        )
    if report.checks_run < baseline.checks_run:
        failures.append(
            f"Credo ran {report.checks_run} check(s), below the baseline's {baseline.checks_run}: "
            "a check was disabled, removed or skipped"
        )
    categories = sorted(set(report.by_category) | set(baseline.by_category))
    for category in categories:
        measured = report.by_category.get(category, 0)
        recorded = baseline.by_category.get(category, 0)
        if measured > recorded:
            failures.append(
                f"{CATEGORY_TITLE[category]}: {measured} issue(s) against the baseline's {recorded} "
                "(the strict count rose; fix the finding, never raise the baseline)"
            )
        elif measured < recorded:
            failures.append(
                f"{CATEGORY_TITLE[category]}: {measured} issue(s) against the baseline's {recorded} "
                f"(record the new counts in {baseline.path} in the same change as the fix)"
            )
    return failures


def main() -> int:
    parser = argparse.ArgumentParser(description="the Control Credo strict ratchet")
    parser.add_argument("--log", required=True, type=pathlib.Path, help="the `mix credo --strict` log to judge")
    parser.add_argument(
        "--baseline",
        type=pathlib.Path,
        default=pathlib.Path(__file__).with_name("credo-strict-baseline.json"),
        help="the committed baseline artefact",
    )
    args = parser.parse_args()

    try:
        baseline = Baseline(args.baseline)
        log = args.log.read_text(encoding="utf-8", errors="replace")
    except OSError as error:
        print(f"cannot judge the Credo strict ratchet: {error}", file=sys.stderr)
        return 2
    except Refusal as error:
        print(f"cannot judge the Credo strict ratchet: {error}", file=sys.stderr)
        return 2

    try:
        report = parse_report(log)
    except Refusal as error:
        print(f"the Credo strict report is not usable: {error}", file=sys.stderr)
        return 1

    print(f"credo_strict_issue_total={report.issues}")
    print(f"credo_strict_files_analyzed={report.files_analyzed}")
    print(f"credo_strict_checks_run={report.checks_run}")
    print(f"credo_strict_scopes={report.scopes}")
    for category in CATEGORY_ORDER:
        if category in report.by_category:
            print(f"credo_strict_{category}={report.by_category[category]}")

    failures = compare(report, baseline)
    if failures:
        print(
            f"the Credo strict ratchet failed: {report.issues} issue(s) against the baseline {args.baseline} "
            f"({baseline.issues}, recorded at {baseline.revision}):",
            file=sys.stderr,
        )
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return 1

    print(
        f"verified the Credo strict ratchet: {report.issues} issue(s) ({report.breakdown()}) over "
        f"{report.files_analyzed} file(s) and {report.checks_run} check(s) at the baseline recorded "
        f"from {baseline.revision}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
