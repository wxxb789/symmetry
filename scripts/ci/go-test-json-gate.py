#!/usr/bin/env python3
"""Run one Go test command and gate it with the shared named-test JSON assertion.

One implementation of the "tee the `-json` event stream, preserve the test exit
status, require every named test to run once and pass once, fail the step if
either fails" plumbing that six workflow steps used to spell out inline (bash
`tee`/`PIPESTATUS` variants and PowerShell `Start-Process` variants):

* the command after `--` runs with stdout captured as the Go test JSON events;
* stderr is captured separately so non-JSON diagnostics can never be mistaken
  for events;
* the captured streams are echoed and, with `--log`, appended to a log file;
* every `--assert PACKAGE TEST...` group is checked over the same event stream
  by `scripts/ci/assert-go-test-json.py` (the single assertion implementation);
* the process status is the test status when the test failed, otherwise the
  first failing assertion status.

`--stdio-boundary` wraps the command in the ordinary-ci descriptor boundary that
declares stdio as Go's inherited interface, so the boundary stays identical
wherever the runner is used.
"""

import argparse
import subprocess
import sys
import tempfile
from pathlib import Path

# Everything after this token is the Go test command. `--` is the usual spelling
# in bash; `--command` is accepted as well because PowerShell does not pass a
# bare `--` to native commands unchanged.
COMMAND_SEPARATOR = "--"
POWERSHELL_COMMAND_SEPARATOR = "--command"

ASSERTION_SCRIPT = Path(__file__).resolve().with_name("assert-go-test-json.py")
BOUNDARY_SCRIPT = Path(__file__).resolve().with_name("stdio-only-caller-boundary.py")

# `--assert` package token that keeps assert-go-test-json.py's unscoped
# behaviour (global failure and malformed-line checks, no package filter).
ANY_PACKAGE = "*"


def process_status(returncode: int) -> int:
    """Map a subprocess returncode to the shell status a caller would see."""
    return returncode if returncode >= 0 else 128 - returncode


class Gate:
    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.log = args.log
        self.test_status = 0
        self.assertion_status = 0

    def append_log(self, chunks: list[str]) -> None:
        if not self.log:
            return
        try:
            with open(self.log, "a", encoding="utf-8", errors="replace") as stream:
                for chunk in chunks:
                    stream.write(chunk)
        except OSError as error:
            print(f"could not append to the gate log: {error}", file=sys.stderr)
            raise SystemExit(1) from error

    def echo(self, chunk: str, stream) -> None:
        if chunk:
            stream.write(chunk)
            stream.flush()

    def run_command(self, command: list[str]) -> None:
        if self.args.stdio_boundary:
            command = [sys.executable, str(BOUNDARY_SCRIPT), *command]
        temporary_stderr = None
        stderr_path = self.args.stderr
        if not stderr_path:
            handle = tempfile.NamedTemporaryFile(
                prefix="go-test-json-gate-", suffix=".stderr", delete=False
            )
            handle.close()
            temporary_stderr = handle.name
            stderr_path = temporary_stderr
        try:
            with open(self.args.events, "w", encoding="utf-8") as events_file, open(
                stderr_path, "w", encoding="utf-8"
            ) as stderr_file:
                completed = subprocess.run(
                    command, stdout=events_file, stderr=stderr_file, check=False
                )
            self.test_status = process_status(completed.returncode)
        except FileNotFoundError as error:
            print(f"could not run the Go test command: {error}", file=sys.stderr)
            raise SystemExit(127) from error
        except OSError as error:
            print(f"could not capture the Go test command: {error}", file=sys.stderr)
            raise SystemExit(1) from error
        try:
            events = Path(self.args.events).read_text(encoding="utf-8", errors="replace")
            errors = Path(stderr_path).read_text(encoding="utf-8", errors="replace")
        except OSError as error:
            print(f"could not read the captured Go test output: {error}", file=sys.stderr)
            raise SystemExit(1) from error
        if temporary_stderr:
            try:
                Path(temporary_stderr).unlink()
            except OSError:
                pass
        self.append_log([events, errors])
        self.echo(errors, sys.stderr)
        self.echo(events, sys.stdout)

    def run_assertions(self) -> None:
        for group in self.args.assertion:
            package, tests = group[0], group[1:]
            command = [sys.executable, str(ASSERTION_SCRIPT), self.args.events]
            if package != ANY_PACKAGE:
                command += ["--package", package]
            command += tests
            completed = subprocess.run(command, capture_output=True, text=True, check=False)
            self.append_log([completed.stdout, completed.stderr])
            self.echo(completed.stderr, sys.stderr)
            self.echo(completed.stdout, sys.stdout)
            if completed.returncode != 0 and self.assertion_status == 0:
                self.assertion_status = completed.returncode

    def record_provenance(self) -> None:
        if not self.args.provenance:
            return
        lines = []
        if self.args.exit_status_key:
            lines.append(f"{self.args.exit_status_key}={self.test_status}")
        if self.args.gate_status_key:
            if self.test_status != 0:
                lines.append(
                    f"{self.args.gate_status_key}=failed: go test exited with status {self.test_status}"
                )
            elif self.assertion_status != 0:
                lines.append(
                    f"{self.args.gate_status_key}=failed: selective witness validation exited with "
                    f"status {self.assertion_status}"
                )
            else:
                lines.append(f"{self.args.gate_status_key}=passed")
        try:
            with open(self.args.provenance, "a", encoding="utf-8") as stream:
                for line in lines:
                    stream.write(f"{line}\n")
        except OSError as error:
            print(f"could not append to the gate provenance: {error}", file=sys.stderr)
            raise SystemExit(1) from error

    def status(self) -> int:
        if self.test_status != 0:
            return self.test_status
        return self.assertion_status


def parse_args(argv: list[str]) -> argparse.Namespace:
    separators = [
        index for index, token in enumerate(argv) if token in (COMMAND_SEPARATOR, POWERSHELL_COMMAND_SEPARATOR)
    ]
    if not separators:
        raise SystemExit(
            f"go-test-json-gate.py requires `{COMMAND_SEPARATOR}` (or `{POWERSHELL_COMMAND_SEPARATOR}` "
            "for PowerShell callers) before the Go test command"
        )
    options_end = separators[0]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--events", required=True, metavar="FILE", help="Go test JSON event stream")
    parser.add_argument("--stderr", metavar="FILE", help="optional destination for the command's stderr")
    parser.add_argument("--log", metavar="FILE", help="optional log file the streams are appended to")
    parser.add_argument(
        "--assert",
        dest="assertion",
        action="append",
        nargs="+",
        default=[],
        metavar="PACKAGE TEST",
        help=f"named-test assertion group; use {ANY_PACKAGE!r} as the package for unscoped assertions",
    )
    parser.add_argument(
        "--stdio-boundary",
        action="store_true",
        help="run the command inside the ordinary-ci stdio-only descriptor boundary",
    )
    parser.add_argument("--provenance", metavar="FILE", help="optional provenance file for gate records")
    parser.add_argument("--exit-status-key", metavar="KEY", help="provenance key for the test status")
    parser.add_argument("--gate-status-key", metavar="KEY", help="provenance key for the gate outcome")
    parser.add_argument("command", nargs=argparse.REMAINDER, help="the Go test command after `--`")
    args = parser.parse_args(argv[:options_end])
    args.command = argv[options_end + 1 :]
    if not args.command:
        raise SystemExit("go-test-json-gate.py requires a command after `--`")
    if not args.assertion:
        raise SystemExit("go-test-json-gate.py requires at least one --assert group")
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse_args(list(sys.argv[1:] if argv is None else argv))
    gate = Gate(args)
    gate.run_command(args.command)
    gate.run_assertions()
    gate.record_provenance()
    return gate.status()


if __name__ == "__main__":
    raise SystemExit(main())
