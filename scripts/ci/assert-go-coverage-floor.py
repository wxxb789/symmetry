#!/usr/bin/env python3
"""Enforce the committed Go coverage floor against a `go tool cover -func` summary.

The `coverage-daemon` job runs the daemon suite once with `-coverprofile=coverage.out` and pipes
`go tool cover -func` over that profile into this gate: the tool owns the arithmetic, this gate
owns the verdict. The committed baseline (`go-coverage-baseline.json`) records the runs the floor
rests on; the floor is `min(recorded totals) - 1.00` percentage point, and the gate refuses a
baseline whose arithmetic does not say exactly that.

Exit codes: 0 the measured total clears the floor; 1 the measured total is below the floor; 2 the
summary or the baseline cannot be read or does not say what this gate requires (never a green).
"""

import argparse
import json
import pathlib
import re
import sys
from typing import Any, NoReturn

FLOOR_DROP_POINTS = 1.00
FLOOR_TOLERANCE = 1e-9
DEFAULT_BASELINE = pathlib.Path(__file__).with_name("go-coverage-baseline.json")
TOTAL_LINE = re.compile(r"^total:\s+\(statements\)\s+([0-9]+(?:\.[0-9]+)?)%$")


def refuse(message: str) -> NoReturn:
    print(message, file=sys.stderr)
    raise SystemExit(2)


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--summary", required=True, help="the file holding `go tool cover -func`'s output")
    parser.add_argument("--baseline", default=str(DEFAULT_BASELINE), help="the committed baseline JSON")
    return parser.parse_args(argv)


def read_summary(path: str) -> float:
    try:
        text = pathlib.Path(path).read_text(encoding="utf-8", errors="replace")
    except OSError as error:
        refuse(f"could not read the cover summary {path}: {error}")
    totals = [line.strip() for line in text.splitlines() if TOTAL_LINE.match(line.strip())]
    if len(totals) != 1:
        refuse(f"the cover summary must carry exactly one `total: (statements) N.N%` line; found {len(totals)}")
    lines = [line for line in text.splitlines() if line.strip()]
    if not lines or lines[-1].strip() != totals[0]:
        refuse("the cover summary's total line must be its last non-empty line")
    match = TOTAL_LINE.match(totals[0])
    assert match is not None
    measured = float(match.group(1))
    if not 0.0 <= measured <= 100.0:
        refuse(f"the cover summary's total {measured} is outside 0..100")
    return measured


def read_baseline(path: str) -> dict[str, Any]:
    try:
        document = json.loads(pathlib.Path(path).read_text(encoding="utf-8"))
    except OSError as error:
        refuse(f"could not read the coverage baseline {path}: {error}")
    except json.JSONDecodeError as error:
        refuse(f"the coverage baseline {path} is not valid JSON: {error}")
    if not isinstance(document, dict) or document.get("metric") != "statement":
        refuse(f'the coverage baseline {path} must carry "metric": "statement"')
    runs = document.get("runs")
    if not isinstance(runs, list) or len(runs) < 3:
        refuse(f"the coverage baseline {path} must record at least three runs")
    percents = []
    for run in runs:
        if not isinstance(run, dict):
            refuse(f"the coverage baseline {path} holds a run that is not an object")
        hit, total, percent = run.get("hit"), run.get("total"), run.get("percent")
        if not isinstance(hit, int) or not isinstance(total, int) or total <= 0 or not 0 <= hit <= total:
            refuse(f"the coverage baseline {path} holds a run without a sane hit/total pair")
        if not isinstance(percent, (int, float)):
            refuse(f"the coverage baseline {path} holds a run without a numeric percent")
        measured_percent = round(100.0 * hit / total, 4)
        if abs(float(percent) - measured_percent) > 0.0001:
            refuse(
                f"the coverage baseline {path} records {percent}% for run {run.get('run')} "
                f"but its {hit}/{total} pair measures {measured_percent}%"
            )
        percents.append(float(percent))
    baseline_percent = document.get("baseline_percent")
    floor_percent = document.get("floor_percent")
    if not isinstance(baseline_percent, (int, float)) or not isinstance(floor_percent, (int, float)):
        refuse(f"the coverage baseline {path} must carry numeric baseline_percent and floor_percent")
    if abs(float(baseline_percent) - min(percents)) > FLOOR_TOLERANCE:
        refuse(
            f"the coverage baseline {path} records baseline_percent {baseline_percent} "
            f"but its runs' minimum is {min(percents)}"
        )
    if abs(float(floor_percent) - (float(baseline_percent) - FLOOR_DROP_POINTS)) > FLOOR_TOLERANCE:
        refuse(
            f"the coverage baseline {path} records floor_percent {floor_percent}; "
            f"the floor must be baseline_percent - {FLOOR_DROP_POINTS:.2f}"
        )
    return document


def main(argv: list[str] | None = None) -> int:
    args = parse_args(list(sys.argv[1:] if argv is None else argv))
    measured = read_summary(args.summary)
    baseline = read_baseline(args.baseline)
    floor = float(baseline["floor_percent"])
    baseline_percent = float(baseline["baseline_percent"])
    runs = baseline["runs"]
    if measured < floor:
        print(
            f"the Go coverage total {measured:.2f}% is below the committed floor {floor:.2f}% "
            f"(baseline {baseline_percent:.2f}%, the minimum of {len(runs)} recorded runs)",
            file=sys.stderr,
        )
        return 1
    print(
        f"verified the Go coverage floor: measured {measured:.2f}% >= floor {floor:.2f}% "
        f"(baseline {baseline_percent:.2f}%, the minimum of {len(runs)} recorded runs)"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
