#!/usr/bin/env python3
"""Summarize a `go test -json` event stream: durations, cap and p95 ratchet.

One deterministic consumer of a captured `go test -json` stream (the daemon
job's race pass writes `$RUNNER_TEMP/daemon-go-race.json` through
`go-test-json-gate.py`; any other capture can be fed the same way):

* the slowest per-test durations are printed as a table, so a green run states
  where the job's time goes instead of leaving it to be felt as "today the
  daemon job is slow";
* the slowest package totals are printed from the stream's package-level
  terminal events, for the same attribution;
* a test whose elapsed time exceeds `MAX_TEST_SECONDS` fails the summary, and
  the per-test p95 (nearest-rank over the tests that ran) fails it when it
  exceeds `P95_LIMIT_FACTOR` times the committed baseline below.

The distribution is the stream's terminal `pass`/`fail` events with a `Test`
field, keyed by `(Package, Test)`; a repeated key keeps its largest observed
elapsed. `skip` events are excluded - a skipped test performed no work, and the
skip share differs by host - but they are counted and printed. A malformed
line, a missing file, a stream without outcome events, or a package-level
failure no named test explains (a build failure, a timeout panic: the package
was cut off, so its durations are incomplete) is an error (exit 2), never a
silent green.

The thresholds are advisory until refactor slice W6-12 flips them to blocking
(program plan section 10.5); the exit status is the verdict either way, and the
step that runs this tool owns whether that verdict gates the job.
"""

import argparse
import json
import math
import sys

# A test over this many seconds fails the summary (program plan section 5,
# W2-01: "fail a test >30 s").
MAX_TEST_SECONDS = 30.0

# The per-test p95 fails the summary when it exceeds the committed baseline by
# more than this factor (W2-01: "p95 +20 % vs the committed baseline").
P95_LIMIT_FACTOR = 1.2

# Committed baseline: the nearest-rank p95 of the per-test elapsed times of the
# daemon module's full `go test -count=1 -json ./...` stream, measured from
# `daemon/` on this host (Windows, go1.27.4) at revision 6a815c0f through this
# same summarizer, with the per-package timeout raised to 1500s so a contended
# host could not truncate the sample: 4,340 outcomes, 56 skips, 3 Windows-host
# flakes outside the slow tail. The p95 index lands on the fixed 5.00s
# probe-deadline cluster (`claudeProbeTimeout`, 23 outcomes in [4.9s, 5.2s]),
# which is what lets a non-race proxy seed a race-stream ratchet. The CI step
# runs the same command under `-race` with the repository's 600s cap; a
# race-shaped stream is not obtainable on this host (both WSL environments
# stall on loopback HTTP, recorded in the slice's plan), and the runner's own
# race timings swing ~1.4x run to run, which is why the summary step is
# advisory (`continue-on-error`) and prints `p95=`, `baseline=` and `ratio=`
# side by side. Refactor slice W6-12 re-baselines on the runner and flips the
# thresholds to blocking.
P95_BASELINE_SECONDS = 4.91

TOP_TEST_ROWS = 20
TOP_PACKAGE_ROWS = 10


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("events", metavar="EVENTS.json", help="the captured `go test -json` event stream to summarize")
    return parser.parse_args(argv)


def load_events(path: str) -> tuple[list[dict], int]:
    """The parsed events and the count of lines that are not JSON."""
    events: list[dict] = []
    invalid = 0
    with open(path, encoding="utf-8", errors="replace") as stream:
        for line in stream:
            if not line.strip():
                continue
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                invalid += 1
    return events, invalid


def collect_outcomes(
    events: list[dict],
) -> tuple[dict[tuple[str, str], float], dict[str, float], int, set[tuple[str, str]], list[str]]:
    """Per-test elapsed times, package totals, skip count, failed tests, failed packages.

    The per-test map is keyed by `(Package, Test)` and keeps the largest elapsed
    time seen for a key; package totals come from the package-level terminal
    events (the only events that carry a package's whole elapsed time).
    """
    tests: dict[tuple[str, str], float] = {}
    packages: dict[str, float] = {}
    skipped = 0
    failed: set[tuple[str, str]] = set()
    failed_packages: list[str] = []
    for event in events:
        action = event.get("Action")
        package = event.get("Package")
        if not isinstance(package, str):
            continue
        test = event.get("Test")
        if isinstance(test, str):
            if action not in ("pass", "fail", "skip"):
                continue
            key = (package, test)
            if action == "skip":
                skipped += 1
                continue
            elapsed = float(event.get("Elapsed") or 0.0)
            tests[key] = max(tests.get(key, 0.0), elapsed)
            if action == "fail":
                failed.add(key)
            continue
        if action in ("pass", "fail"):
            packages[package] = float(event.get("Elapsed") or 0.0)
            if action == "fail":
                failed_packages.append(package)
    return tests, packages, skipped, failed, failed_packages


def nearest_rank(values: list[float], fraction: float) -> float:
    """The nearest-rank percentile: the smallest value covering `fraction`."""
    ordered = sorted(values)
    index = max(math.ceil(fraction * len(ordered)) - 1, 0)
    return ordered[index]


def print_test_table(tests: dict[tuple[str, str], float]) -> None:
    ordered = sorted(tests.items(), key=lambda item: (-item[1], item[0][0], item[0][1]))
    print(f"Top {min(TOP_TEST_ROWS, len(ordered))} slowest tests:")
    for (package, test), elapsed in ordered[:TOP_TEST_ROWS]:
        print(f"  {elapsed:8.2f}s  {package}  {test}")


def print_package_table(packages: dict[str, float]) -> None:
    ordered = sorted(packages.items(), key=lambda item: (-item[1], item[0]))
    print(f"Top {min(TOP_PACKAGE_ROWS, len(ordered))} slowest packages:")
    for package, elapsed in ordered[:TOP_PACKAGE_ROWS]:
        print(f"  {elapsed:8.2f}s  {package}")


def main(argv: list[str] | None = None) -> int:
    args = parse_args(list(sys.argv[1:] if argv is None else argv))
    try:
        events, invalid = load_events(args.events)
    except OSError as error:
        print(f"could not read the Go test JSON events: {error}", file=sys.stderr)
        return 2
    tests, packages, skipped, failed, failed_packages = collect_outcomes(events)
    if invalid:
        print(f"the Go test JSON stream carries {invalid} unparsable line(s)", file=sys.stderr)
        return 2
    if not tests:
        print("the Go test JSON stream carries no per-test pass/fail events", file=sys.stderr)
        return 2
    unexplained = [package for package in failed_packages if not any(key[0] == package for key in failed)]
    if unexplained:
        print(
            "the Go test JSON stream reports package-level failures no named test explains "
            f"({', '.join(sorted(unexplained))}); the durations for those packages are incomplete",
            file=sys.stderr,
        )
        return 2

    elapsed_values = list(tests.values())
    p95 = nearest_rank(elapsed_values, 0.95)
    limit = P95_BASELINE_SECONDS * P95_LIMIT_FACTOR
    over_cap = sorted(
        ((package, test, elapsed) for (package, test), elapsed in tests.items() if elapsed > MAX_TEST_SECONDS),
        key=lambda item: (-item[2], item[0], item[1]),
    )

    print_test_table(tests)
    print_package_table(packages)
    ratio = p95 / P95_BASELINE_SECONDS
    print(
        f"go_test_durations: tests={len(tests)} skipped={skipped} packages={len(packages)} "
        f"p95={p95:.3f}s baseline={P95_BASELINE_SECONDS:.3f}s ratio={ratio:.2f}x limit={limit:.3f}s "
        f"over_{MAX_TEST_SECONDS:.0f}s={len(over_cap)} failed={len(failed)} package_fail={len(failed_packages)}"
    )

    verdict = 0
    for package, test, elapsed in over_cap:
        print(
            f"go_test_durations: FAIL: {package} {test} took {elapsed:.2f}s, over the {MAX_TEST_SECONDS:.0f}s cap",
        )
        verdict = 1
    if p95 > limit:
        print(
            f"go_test_durations: FAIL: the per-test p95 {p95:.3f}s exceeds the "
            f"{P95_LIMIT_FACTOR}x limit {limit:.3f}s of the committed baseline "
            f"{P95_BASELINE_SECONDS:.3f}s",
        )
        verdict = 1
    if verdict == 0:
        print("go_test_durations: PASS: no test over the cap and the p95 is within the committed baseline band")
    return verdict


if __name__ == "__main__":
    raise SystemExit(main())
