#!/usr/bin/env python3
"""Fail when a folded gate did not report success.

Consolidating a job into a platform sibling must never be able to mask a gate:
this step-level check turns "the folded step was skipped or failed" into a job
failure, so a folded gate keeps its own pass/fail outcome.

Usage:
  assert-folded-gates.py --job JOB "Gate name=success" ["Other gate=failure" ...]
"""

import argparse
import sys


def parse_gate(spec: str) -> tuple[str, str]:
    name, separator, outcome = spec.partition("=")
    if not separator or not name.strip() or not outcome.strip():
        raise ValueError(f"gate specification must be `name=outcome`: {spec!r}")
    return name.strip(), outcome.strip()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--job", required=True, help="job the folded gates now run in")
    parser.add_argument("gates", metavar="NAME=OUTCOME", nargs="+")
    args = parser.parse_args()

    try:
        gates = [parse_gate(spec) for spec in args.gates]
    except ValueError as error:
        print(f"invalid folded gate specification: {error}", file=sys.stderr)
        return 2

    failed = [f"{name}={outcome}" for name, outcome in gates if outcome != "success"]
    if failed:
        print(
            f"folded gate(s) in {args.job} did not succeed: {', '.join(failed)}; "
            "a folded gate must keep its own pass/fail outcome",
            file=sys.stderr,
        )
        return 1
    print(f"verified every folded gate in {args.job} reported success: {len(gates)} checked")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
