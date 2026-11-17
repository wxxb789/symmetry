#!/usr/bin/env python3
"""Require each named ExUnit test to run and pass exactly once, from JSON events.

The Elixir twin of `assert-go-test-json.py`. It reads the event stream
`SymmetryControl.ExUnitJsonFormatter` writes (one JSON object per line) instead
of scraping the human-readable ExUnit summary, which cannot distinguish a
skipped-only run, a decoy `Result:` line, or a renamed test from real success.

A gated name must appear exactly once, with state `passed`. Any other state for
a gated name, a missing name, a duplicated name, a line that is not a JSON
object, a stream without the `suite_finished` line, and an empty stream all
fail. `--runner-status` carries the `mix test` exit status, which the event file
itself cannot express, and `--expected-selector TAG` re-checks the run's own
include filters so a selector that no longer matches any tag fails before the
tests are inspected.
"""

import argparse
import json
import sys

PASSED = "passed"
STATES = ("passed", "skipped", "excluded", "failed", "other")


class EventStream:
    """Gated-name outcomes plus the stream properties the gate reasons about."""

    def __init__(self, expected: list[str]) -> None:
        self.expected = expected
        self.counts = {name: {state: 0 for state in STATES} for name in expected}
        self.lines = 0
        self.tests = 0
        self.ran = 0
        self.failures = 0
        self.skips = 0
        self.invalid = 0
        self.selectors: list[str] = []
        self.suite_finished = False

    def feed(self, line: str) -> None:
        self.lines += 1
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            self.invalid += 1
            return
        if not isinstance(event, dict):
            self.invalid += 1
            return

        kind = event.get("event")
        if kind == "test_finished":
            self.record_test(event)
        elif kind == "suite_started":
            self.record_selectors(event)
        elif kind == "suite_finished":
            self.suite_finished = True

    def record_test(self, event: dict) -> None:
        name = event.get("name")
        state = event.get("state")
        if not isinstance(name, str) or not isinstance(state, str):
            self.invalid += 1
            return

        self.tests += 1
        if state in ("passed", "failed"):
            self.ran += 1
        if state == "failed":
            self.failures += 1
        elif state == "skipped":
            self.skips += 1
        if name in self.counts:
            self.counts[name][state if state in STATES else "other"] += 1

    def record_selectors(self, event: dict) -> None:
        include = event.get("include")
        if include is None:
            return
        if not isinstance(include, list):
            self.invalid += 1
            return
        self.selectors.extend(str(selector) for selector in include)

    def bad(self) -> dict[str, dict[str, int]]:
        """Gated names that are not present exactly once in state `passed`."""
        return {
            name: values
            for name, values in self.counts.items()
            if values[PASSED] != 1 or sum(values.values()) != 1
        }

    def describe(self, values: dict[str, int]) -> str:
        return "{" + ", ".join(f"{state}:{count}" for state, count in values.items()) + "}"


def read_stream(path: str, expected: list[str]) -> EventStream:
    stream = EventStream(expected)
    with open(path, encoding="utf-8", errors="strict") as lines:
        for line in lines:
            if line.strip():
                stream.feed(line)
    return stream


def check(
    stream: EventStream,
    runner_status: int,
    expected_selector: str | None = None,
    provenance: str | None = None,
) -> int:
    """Apply the rule to `stream`; return the process status to propagate."""

    def record(lines: list[str]) -> None:
        if provenance:
            with open(provenance, "a", encoding="utf-8") as handle:
                for line in lines:
                    handle.write(f"{line}\n")

    if runner_status != 0:
        record([f"gate_status=failed: the ExUnit runner exited with status {runner_status}"])
        print(f"the ExUnit runner exited with status {runner_status}", file=sys.stderr)
        return 1

    if expected_selector and not any(expected_selector in selector for selector in stream.selectors):
        record([f"gate_status=failed: the ExUnit run did not select {expected_selector}"])
        print(
            f"the ExUnit run did not select {expected_selector}: {stream.selectors}",
            file=sys.stderr,
        )
        return 1

    bad = stream.bad()
    problems = []
    if stream.lines == 0:
        problems.append("the ExUnit event stream was empty")
    if stream.invalid:
        problems.append(f"invalid_json_lines={stream.invalid}")
    if not stream.suite_finished:
        problems.append("the ExUnit event stream has no suite_finished line")
    if bad:
        problems.append(
            "gated tests without exactly one passed outcome: "
            + "; ".join(f"{name}={stream.describe(values)}" for name, values in bad.items())
        )

    if problems:
        record(["gate_status=failed: " + "; ".join(problems)])
        print(
            "ExUnit JSON event gate failed: "
            + "; ".join(problems)
            + f"; observed test_finished={stream.tests} ran={stream.ran} "
            f"failures={stream.failures} skipped={stream.skips}",
            file=sys.stderr,
        )
        return 1

    record([f"test_summary=tests={stream.ran};failures={stream.failures};skipped={stream.skips}"])
    for name in stream.expected:
        print(f"verified {name}: state=passed")
    print(
        f"observed {stream.tests} test_finished events, {stream.ran} of them ran, "
        f"{stream.failures} failed, {stream.skips} skipped"
    )
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("events", metavar="EVENTS.json", help="ExUnit JSON event stream")
    parser.add_argument(
        "--runner-status",
        metavar="STATUS",
        required=True,
        type=int,
        help="exit status of the `mix test` run that produced EVENTS.json",
    )
    parser.add_argument(
        "--expected-selector",
        metavar="TAG",
        help="tag the run must have selected, checked against its suite_started filters",
    )
    parser.add_argument(
        "--provenance",
        metavar="FILE",
        help="append the test_summary/gate_status records the ExUnit summary gate wrote",
    )
    parser.add_argument("tests", metavar="TEST", nargs="+")
    args = parser.parse_args()

    expected = args.tests
    if len(expected) != len(set(expected)):
        print("expected test names must be unique", file=sys.stderr)
        return 2

    try:
        stream = read_stream(args.events, expected)
    except (OSError, UnicodeError) as error:
        print(f"could not read the ExUnit JSON event stream: {error}", file=sys.stderr)
        return 2

    return check(stream, args.runner_status, args.expected_selector, args.provenance)


if __name__ == "__main__":
    raise SystemExit(main())
