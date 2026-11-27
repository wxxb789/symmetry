#!/usr/bin/env python3
"""Require each named Go test to run and pass exactly once, without skips."""

import argparse
import json
import sys


def first_matching_target(name: str, targets: list[str]) -> str | None:
    for target in targets:
        if name == target or name.startswith(target + "/"):
            return target
    return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("events", metavar="EVENTS.json")
    parser.add_argument("--package")
    parser.add_argument(
        "--credential-gated",
        metavar="TEST",
        action="append",
        default=[],
        help="a named test that must run without passing: its placeholder credential cannot "
        "produce an authentic turn, so run=1 pass=0 skip=0 with its own fail events is the "
        "expected shape and is reported as credential-gated instead of verified",
    )
    parser.add_argument("tests", metavar="TEST", nargs="+")
    args = parser.parse_args()

    path = args.events
    expected = args.tests
    if len(expected) != len(set(expected)):
        print("expected test names must be unique", file=sys.stderr)
        return 2

    gated = list(dict.fromkeys(args.credential_gated))
    if any(name not in expected for name in gated):
        print("credential-gated test names must be expected test names", file=sys.stderr)
        return 2

    counts = {name: {"run": 0, "pass": 0, "skip": 0} for name in expected}
    failures = 0
    invalid = 0
    skipped = []
    gated_failures = {name: 0 for name in gated}
    foreign_failures = 0
    try:
        with open(path, encoding="utf-8", errors="strict") as stream:
            for line in stream:
                if not line.strip():
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    invalid += 1
                    continue

                action = event.get("Action")
                name = event.get("Test")
                selected_package = args.package is None or event.get("Package") == args.package
                if action == "fail":
                    failures += 1
                    if selected_package and isinstance(name, str):
                        matching = first_matching_target(name, gated)
                        if matching is not None:
                            gated_failures[matching] += 1
                        elif gated:
                            foreign_failures += 1
                if selected_package and name in counts and action in ("run", "pass", "skip"):
                    counts[name][action] += 1
                if (
                    selected_package
                    and action == "skip"
                    and isinstance(name, str)
                    and first_matching_target(name, expected) is not None
                ):
                    skipped.append(name)
    except (OSError, UnicodeError) as error:
        print(f"could not read Go test JSON events: {error}", file=sys.stderr)
        return 2

    bad = {
        name: values
        for name, values in counts.items()
        if values != {"run": 1, "pass": 1, "skip": 0} and name not in gated
    }
    gated_bad = {
        name: values for name, values in counts.items() if name in gated and values != {"run": 1, "pass": 0, "skip": 0}
    }
    gated_without_failure = [name for name in gated if gated_failures[name] == 0]
    unexpected_failures = failures if not gated else foreign_failures
    if invalid or unexpected_failures or bad or skipped or gated_bad or gated_without_failure:
        summary = "; ".join(
            f"{name}=run:{values['run']},pass:{values['pass']},skip:{values['skip']}"
            for name, values in {**bad, **gated_bad}.items()
        )
        detail = (
            f"bad={summary or 'none'}, skipped={sorted(skipped)}, fail_events={failures}, "
            f"invalid_json_lines={invalid}"
        )
        if gated:
            detail += (
                f", credential_gated={sorted(gated)}, "
                f"credential_gated_fail_events={sorted(gated_failures.items())}, "
                f"foreign_test_fail_events={foreign_failures}"
            )
        print(f"Go native acceptance event gate failed: {detail}", file=sys.stderr)
        return 1

    for name in expected:
        if name in gated:
            print(f"credential-gated {name}: run=1 pass=0 skip=0")
        else:
            print(f"verified {name}: run=1 pass=1 skip=0")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
