#!/usr/bin/env python3
"""Run a command with stdio as its only inherited descriptor interface.

Ordinary CI declares stdio as Go's inherited descriptor interface. This is the
single implementation of that caller boundary: `go-test-json-gate.py` uses it for
captured runs and the daemon race pass uses it directly, and it must be
established before Go, never inside the native census fixture.
"""

import subprocess
import sys


def main(argv: list[str]) -> int:
    if not argv:
        print("stdio-only-caller-boundary.py requires a command", file=sys.stderr)
        return 2
    try:
        result = subprocess.run(argv, close_fds=True, check=False)
    except OSError as error:
        print(f"could not run the boundary command: {error}", file=sys.stderr)
        return 127
    return result.returncode if result.returncode >= 0 else 128 - result.returncode


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
