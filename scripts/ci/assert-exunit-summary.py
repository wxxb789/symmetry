#!/usr/bin/env python3
"""Require a gated ExUnit run to have really run and passed its selected test.

One implementation of the selective-suite summary rule shared by `control`,
`pi-control-e2e-linux` and `pi-control-e2e-windows` (it replaced a byte-identical
bash heredoc in two jobs and a forked PowerShell reimplementation in the third).

This is a **text heuristic over ExUnit's CLI output** as produced by the pinned
toolchain, not a structured event gate: the CLI prints `Result:`/`Failed:`
summary lines and the names of passing tests are not in that output, so the
checks below can only reason about the summary counts and the run's own selector
line. What it cannot see:

* a summary printed in another shape or locale (their absence fails the gate
  closed, because no summary is accepted as a failure);
* a summary printed by a different `--formatter` that omits or rewrites
  `Result:`/`Failed:`;
* which individual test a count belongs to, beyond the selector tag the caller
  names with `--expected-selector`.

A structured route was considered first: `mix test --formatter Module` writing
one JSON event per test would give this gate the same shape as
`go test -json` + `assert-go-test-json.py`. It was rejected for this change
because the pinned toolchain's formatter event API cannot be exercised or
verified on the development host, and an unverified formatter would sit inside
the acceptance path; the three fail-closed rules below were added instead.

Fail-closed rules, in order:

1. the runner's exit status is an explicit input (`--runner-status`) and a
   non-zero status fails the gate, so a run that died cannot pass on its text;
2. a summary must be present, with `tests > 0` and `failures == 0`;
3. a reported `skipped` count must be zero: a skipped test in a gate that
   selected exactly one test means the thing being gated did not run. The
   `excluded` count is deliberately tolerated - it counts the tests this
   filtered run did not select;
4. with `--expected-selector TAG`, the run's `Including tags:` line must name
   that tag, so a gate cannot pass against a mis-wired selection.
"""

import argparse
import re
import sys

ANSI_ESCAPE = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")
RESULT_SOURCE = r"(?m)^\s*Result:\s*(\d+)(?:/(\d+))?\s+passed\b([^\r\n]*)$"
REPORTED_FAILURES_SOURCE = r"(?m)^\s*Failed:\s*(\d+)\s+tests?\b"
SUMMARIES_SOURCE = r"(?m)^\s*(\d+)\s+tests?,\s*(\d+)\s+failures?\b"
TAIL_FAILURE_SOURCE = r"(\d+)\s+failed\b"
TAIL_COUNT_SOURCE = r"(\d+)\s+(excluded|skipped)\b"
SELECTOR_SOURCE = r"(?m)^\s*Including tags:.*$"

RESULT_PATTERN = re.compile(RESULT_SOURCE)
REPORTED_FAILURES_PATTERN = re.compile(REPORTED_FAILURES_SOURCE)
SUMMARIES_PATTERN = re.compile(SUMMARIES_SOURCE)
TAIL_COUNT_PATTERN = re.compile(TAIL_COUNT_SOURCE)
SELECTOR_PATTERN = re.compile(SELECTOR_SOURCE)


class SummaryMissing(Exception):
    """The log carried no recognisable ExUnit summary."""


class SummaryInvalid(Exception):
    """The log carried a summary that reports zero tests, failures or skips."""

    def __init__(self, tests: int, failures: int, skips: int, reason: str) -> None:
        super().__init__(reason)
        self.tests = tests
        self.failures = failures
        self.skips = skips
        self.reason = reason


class Summary:
    def __init__(self, tests: int, failures: int, skips: int, tail: str = "") -> None:
        self.tests = tests
        self.failures = failures
        self.skips = skips
        self.tail = tail


def normalise(text: str) -> str:
    """Remove ANSI escapes and make every line ending LF-only."""
    text = ANSI_ESCAPE.sub("", text)
    return text.replace("\r\n", "\n").replace("\r", "\n")


def tail_counts(tail: str) -> dict[str, int]:
    """`skipped`/`excluded` counts that follow the passed count on the same line."""
    return {label: int(count) for count, label in TAIL_COUNT_PATTERN.findall(tail)}


def parse_summary(text: str) -> Summary:
    """Return the last ExUnit summary in the text.

    Raises SummaryMissing when no summary line is present and SummaryInvalid when
    the summary reports zero tests, any failure or any skipped test.
    """
    text = normalise(text)
    results = RESULT_PATTERN.findall(text)
    reported_failures = REPORTED_FAILURES_PATTERN.findall(text)
    summaries = SUMMARIES_PATTERN.findall(text)
    tail = ""
    if results:
        passed_text, total_text, tail = results[-1]
        passed = int(passed_text)
        failed = re.search(TAIL_FAILURE_SOURCE, tail)
        failures = (
            int(reported_failures[-1])
            if reported_failures
            else int(failed.group(1))
            if failed
            else int(total_text) - passed
            if total_text
            else 0
        )
        tests = int(total_text) if total_text else passed + failures
    elif summaries:
        tests, failures = (int(value) for value in summaries[-1])
    else:
        raise SummaryMissing("ExUnit summary was not found")
    skips = tail_counts(tail).get("skipped", 0)
    if tests <= 0 or failures != 0 or skips != 0:
        raise SummaryInvalid(
            tests,
            failures,
            skips,
            "ExUnit selective gate expected tests>0, failures=0 and skipped=0; "
            f"got tests={tests}, failures={failures}, skipped={skips}",
        )
    return Summary(tests, failures, skips, tail)


def check(
    text: str,
    runner_status: int,
    expected_selector: str | None = None,
    provenance: str | None = None,
) -> int:
    """Apply the rule to `text`; return the process status to propagate."""
    def record(lines: list[str]) -> None:
        if provenance:
            with open(provenance, "a", encoding="utf-8") as stream:
                for line in lines:
                    stream.write(f"{line}\n")

    if runner_status != 0:
        record([f"gate_status=failed: the ExUnit runner exited with status {runner_status}"])
        print(f"the ExUnit runner exited with status {runner_status}", file=sys.stderr)
        return 1
    normalised = normalise(text)
    if expected_selector:
        selectors = SELECTOR_PATTERN.findall(normalised)
        if not any(expected_selector in selector for selector in selectors):
            record(
                [
                    "gate_status=failed: the ExUnit run did not select "
                    f"{expected_selector}"
                ]
            )
            print(
                f"the ExUnit run did not select {expected_selector}: {selectors}",
                file=sys.stderr,
            )
            return 1
    try:
        summary = parse_summary(normalised)
    except SummaryMissing:
        record(["gate_status=failed: ExUnit summary was not found"])
        print("ExUnit summary was not found", file=sys.stderr)
        return 1
    except SummaryInvalid as invalid:
        record(
            [
                f"test_summary=tests={invalid.tests};failures={invalid.failures};skipped={invalid.skips}",
                "gate_status=failed: invalid ExUnit summary "
                f"tests={invalid.tests} failures={invalid.failures} skipped={invalid.skips}",
            ]
        )
        print(invalid.reason, file=sys.stderr)
        return 1
    record(
        [
            f"test_summary=tests={summary.tests};failures={summary.failures};skipped={summary.skips}"
        ]
    )
    print(f"verified ExUnit summary: tests={summary.tests}, failures={summary.failures}")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("log", metavar="LOG", help="captured ExUnit output")
    parser.add_argument(
        "--runner-status",
        metavar="STATUS",
        required=True,
        type=int,
        help="exit status of the `mix test` run that produced LOG",
    )
    parser.add_argument(
        "--expected-selector",
        metavar="TAG",
        help="tag the run must have selected, checked against its `Including tags:` line",
    )
    parser.add_argument(
        "--provenance",
        metavar="FILE",
        help="append the per-step test_summary/gate_status records the PowerShell gate wrote inline",
    )
    args = parser.parse_args()
    try:
        with open(args.log, encoding="utf-8", errors="replace") as stream:
            text = stream.read()
    except OSError as error:
        print(f"could not read ExUnit output: {error}", file=sys.stderr)
        return 2
    return check(text, args.runner_status, args.expected_selector, args.provenance)


if __name__ == "__main__":
    raise SystemExit(main())
