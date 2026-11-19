#!/usr/bin/env python3
"""Require the render gate's executed set from the run's own Playwright JSON report.

The render step exports `SYMMETRY_RENDER_GATE_REQUIRED=1` so a gate run whose
fixture environment is missing fails collection instead of skipping green, but
a dropped guard, a dropped flag or a spec file dropped from the invocation only
turns the case into a named skip — and the Playwright runner still exits 0. This
gate reads the JSON report the run itself wrote (`SYMMETRY_PLAYWRIGHT_JSON_REPORT`):
the run must report exactly the expected number of skips, and every named case
must have run once per project the report declares, in state `expected`.

`skipped` for a named case fails — the case did not execute. `flaky` fails —
the green result needed a retry. A named case absent from the report — excluded
by a filter or dropped from the CLI file list — fails because it did not run at
all. The expected skip count is the caller's single number for the suite's
by-design skip; it is never inferred from the report.
"""

import argparse
import json
import sys

EXPECTED_STATE = "expected"


def scan_suite(suite: dict, titles: set[str], outcomes: dict[str, list[tuple[str, str]]]) -> None:
    """Record (project, state) for every test whose spec title is gated."""
    for spec in suite.get("specs", []):
        title = spec.get("title")
        if title in titles:
            for case in spec.get("tests", []):
                outcomes.setdefault(title, []).append(
                    (case.get("projectName"), case.get("status"))
                )
    for child in suite.get("suites", []):
        scan_suite(child, titles, outcomes)


def describe(states: list[str]) -> str:
    if not states:
        return "no result"
    counts: dict[str, int] = {}
    for state in states:
        counts[str(state)] = counts.get(str(state), 0) + 1
    return "{" + ", ".join(f"{state}:{count}" for state, count in counts.items()) + "}"


def check(report: object, expect_skipped: int, titles: list[str]) -> list[str]:
    """Return the problems that make the executed set not what was asserted."""
    if not isinstance(report, dict):
        return ["the report is not a JSON object"]

    stats = report.get("stats")
    if not isinstance(stats, dict) or not isinstance(stats.get("skipped"), int):
        return ["the report carries no integer stats.skipped"]

    projects = report.get("config", {}).get("projects")
    names = [project.get("name") for project in projects] if isinstance(projects, list) else []
    if not names or not all(isinstance(name, str) for name in names):
        return ["the report declares no project names"]

    outcomes: dict[str, list[tuple[str, str]]] = {}
    for suite in report.get("suites", []):
        if isinstance(suite, dict):
            scan_suite(suite, set(titles), outcomes)

    problems = []
    if stats["skipped"] != expect_skipped:
        problems.append(f"stats.skipped is {stats['skipped']}, expected {expect_skipped}")

    for title in titles:
        title_problems = []
        per_project: dict[str, list[str]] = {name: [] for name in names}
        for project, state in outcomes.get(title, []):
            if project in per_project:
                per_project[project].append(state)
            else:
                title_problems.append(f"reported under unknown project {project!r}")
        for name in names:
            states = per_project[name]
            if states != [EXPECTED_STATE]:
                title_problems.append(f"{name}: {describe(states)}")
        if title_problems:
            problems.append(f"case {title!r} did not run as expected: " + "; ".join(title_problems))

    return problems


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("report", help="Playwright JSON report written by the run under assertion")
    parser.add_argument(
        "--expect-skipped",
        type=int,
        required=True,
        help="the exact number of skipped tests the run must report (its by-design skips)",
    )
    parser.add_argument(
        "titles",
        nargs="+",
        metavar="TITLE",
        help="a case title that must run as `expected` once per declared project",
    )
    args = parser.parse_args()

    try:
        with open(args.report, encoding="utf-8") as handle:
            report = json.load(handle)
    except (OSError, json.JSONDecodeError) as error:
        print(f"cannot read the Playwright report: {error}", file=sys.stderr)
        return 1

    problems = check(report, args.expect_skipped, args.titles)
    if problems:
        for problem in problems:
            print(f"render gate executed set: {problem}", file=sys.stderr)
        return 1

    print(
        f"render gate executed set verified: {args.expect_skipped} skipped; "
        + ", ".join(f"{title!r} passed in every project" for title in args.titles)
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
