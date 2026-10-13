#!/usr/bin/env python3
"""Require each named Go test to run and pass exactly once, without skips."""

import argparse
import json
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("events", metavar="EVENTS.json")
    parser.add_argument("--package")
    parser.add_argument("tests", metavar="TEST", nargs="+")
    args = parser.parse_args()

    path = args.events
    expected = args.tests
    if len(expected) != len(set(expected)):
        print("expected test names must be unique", file=sys.stderr)
        return 2

    counts = {name: {"run": 0, "pass": 0, "skip": 0} for name in expected}
    failures = 0
    invalid = 0
    skipped = []
    try:
        with open(path, encoding="utf-8", errors="strict") as stream:
            for line_number, line in enumerate(stream, 1):
                if not line.strip():
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    invalid += 1
                    continue

                action = event.get("Action")
                name = event.get("Test")
                if action == "fail":
                    failures += 1
                selected_package = args.package is None or event.get("Package") == args.package
                if selected_package and name in counts and action in ("run", "pass", "skip"):
                    counts[name][action] += 1
                if selected_package and action == "skip" and isinstance(name, str):
                    if any(name == target or name.startswith(target + "/") for target in expected):
                        skipped.append(name)
    except (OSError, UnicodeError) as error:
        print(f"could not read Go test JSON events: {error}", file=sys.stderr)
        return 2

    bad = {
        name: values
        for name, values in counts.items()
        if values != {"run": 1, "pass": 1, "skip": 0}
    }
    if invalid or failures or bad or skipped:
        summary = "; ".join(
            f"{name}=run:{values['run']},pass:{values['pass']},skip:{values['skip']}"
            for name, values in bad.items()
        )
        print(
            "Go native acceptance event gate failed: "
            f"bad={summary or 'none'}, skipped={sorted(skipped)}, fail_events={failures}, "
            f"invalid_json_lines={invalid}",
            file=sys.stderr,
        )
        return 1

    for name in expected:
        print(f"verified {name}: run=1 pass=1 skip=0")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
