from __future__ import annotations

import contextlib
import hashlib
import os
import pathlib
import re
import shutil
import signal
import subprocess
import sys
from typing import Any

from model_routing_records import (
    Refused,
    read_bytes,
    require_bool,
    require_digest,
    require_integer,
    require_object,
    require_optional_integer,
    require_optional_text,
    sha256_hex,
    strict_json,
    vitest_entry_index,
    write_bytes,
)

DEFAULT_TIMEOUT = 1800.0
PROCESS_TREE_REAP_SECONDS = 10.0


MAX_RESULT_BYTES = 16 << 20
MAX_CAPTURE_BYTES = 256 << 20


GO_LIST_FAILURE_PREFIX = "the package test binary did not list the named Go tests: "
EXUNIT_FAILURE_PREFIX = "the traced ExUnit run did not execute every named test: "
VITEST_FAILURE_PREFIX = "the traced vitest run did not report every named test as passed: "
SOURCE_CHANGED_SUFFIX = "source changed while executing this gate"


TOOL_CACHE_ENV = "SYMMETRY_TOOL_CACHE_DIR"
TOOL_CACHE_DISABLE_ENV = "SYMMETRY_DISABLE_TOOL_CACHES"
TOOL_CACHE_DISABLED = "1"
TOOL_STATE_DIRNAME = "tool-state"
DEPENDENCY_TREE_NAMESPACE = b"symmetry-dependency-tree/v1"


# Elixir 1.20.4 rewrites one trace line per finished test, preceded by a carriage
# return; ANSI escapes wrap the description on color terminals. The measured shape is
# `  * test <description> (<n>.<d>ms) [L#<line>]` for a finished test and
# `  * test <description> (skipped|excluded) [L#<line>]` otherwise. The matcher
# compares the rendered description as a whole, so a test declared inside a `describe`
# block - rendered as `<describe> <name>` - matches a frozen name only when it spells
# that name in full; declaring tests at module level is the frozen convention, not a
# parser rule.
EXUNIT_ANSI_PATTERN = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")
EXUNIT_OBSERVATION_PATTERN = re.compile(r"^  \* test (?P<name>.+?) \((?P<descriptor>[^()]*)\) \[L#[0-9]+\]$")
EXUNIT_FINISHED_PATTERN = re.compile(r"^[0-9]+(?:\.[0-9]+)?ms$")
# `mix test` owns the summary, not ExUnit: the measured shapes are `Result: 3 passed`,
# `Result: 2 passed, 1 skipped`, `Result: 1/2 passed` (with a following `Failed: 1 test`)
# and `Result: 1 passed, 10 excluded`. Only the `N/M passed` form carries the failure
# count directly; the restated `Failed:` line must not be counted twice.
EXUNIT_RESULT_PATTERN = re.compile(
    r"^\s*Result:\s*(?P<passed>[0-9]+)(?:/(?P<total>[0-9]+))?\s+passed\b(?P<tail>[^\r\n]*)",
    re.MULTILINE,
)
EXUNIT_FAILED_LINE_PATTERN = re.compile(r"^\s*Failed:\s*([0-9]+)\s+tests?\b", re.MULTILINE)
EXUNIT_TAIL_FAILED_PATTERN = re.compile(r"([0-9]+)\s+failed\b")


RESULT_KEYS = {
    "gate_id",
    "cwd",
    "argv",
    "go_tests",
    "go_package",
    "exit_code",
    "timed_out",
    "error",
    "stdout_sha256",
    "stderr_sha256",
    "stdout_bytes",
    "stderr_bytes",
    "tool_state_dir",
    "go_list_argv",
    "go_list_exit_code",
    "go_list_stdout_sha256",
    "go_list_stderr_sha256",
    "go_list_stdout_bytes",
    "go_list_stderr_bytes",
    "env",
    "exunit_tests",
    "exunit_files",
    "exunit_argv",
    "exunit_exit_code",
    "exunit_timed_out",
    "exunit_stdout_sha256",
    "exunit_stderr_sha256",
    "exunit_stdout_bytes",
    "exunit_stderr_bytes",
    "exunit_error",
    "vitest_tests",
    "vitest_files",
    "vitest_argv",
    "vitest_exit_code",
    "vitest_timed_out",
    "vitest_stdout_sha256",
    "vitest_stderr_sha256",
    "vitest_stdout_bytes",
    "vitest_stderr_bytes",
    "vitest_report_sha256",
    "vitest_report_bytes",
    "vitest_error",
    "validator",
    "subject_hash_before",
    "subject_hash_after",
    "passed",
}
VALIDATOR_KEYS = {"argv", "exit_code", "stdout_sha256", "stderr_sha256", "stdout_bytes", "stderr_bytes"}

VALIDATOR_SCRIPT = pathlib.Path(__file__).with_name("assert-go-test-json.py").resolve()


def go_list_argv(gate: dict[str, Any]) -> list[str] | None:
    """Derive the `go test -list` cross-check argv for a named-Go-test gate, if any."""
    if not gate["go_tests"] or not gate["go_package"]:
        return None
    if pathlib.PurePath(gate["argv"][0]).name.lower() not in ("go", "go.exe"):
        return None
    if any("/" in name for name in gate["go_tests"]):
        return None
    pattern = "^(" + "|".join(re.escape(name) for name in gate["go_tests"]) + ")$"
    return ["go", "test", "-list", pattern, gate["go_package"]]


def go_list_detail(expected: list[str], exit_code: int | None, stdout_raw: bytes) -> str | None:
    """First missing or duplicated expected test name in `go test -list` stdout."""
    if exit_code is None:
        return "the list command timed out"
    if exit_code != 0:
        return f"the list command exited with {exit_code}"
    lines = [line.strip() for line in stdout_raw.decode("utf-8", "replace").splitlines()]
    for name in expected:
        count = lines.count(name)
        if count == 0:
            return f"missing {name}"
        if count > 1:
            return f"{name} was listed {count} times"
    return None


def exunit_trace_argv(gate: dict[str, Any]) -> list[str] | None:
    """Derive the `mix test --trace` cross-check argv for a named-ExUnit-test gate, if any."""
    if not gate["exunit_tests"]:
        return None
    return [gate["argv"][0], "test", "--trace", "--seed", "0", *gate["exunit_files"]]


def vitest_report_argv(gate: dict[str, Any], gate_dir: pathlib.Path) -> list[str] | None:
    """Derive the structured vitest cross-check argv for a named-vitest-test gate, if any.

    The derived run writes vitest's own JSON report next to the gate evidence, so
    the check parses a structured document instead of matching printed text. The
    command reuses the gate's own launcher and flags up to its vitest entry (the
    same shape gate validation required), so a flagged spelling reruns vitest
    instead of handing the flags to a different file.
    """
    if not gate["vitest_tests"]:
        return None
    entry_index = vitest_entry_index(gate["argv"])
    if entry_index is None:
        raise Refused(
            f"gate {gate['id']} declares vitest tests but its argv names no recognizable "
            "vitest entry point, so the derived report command cannot be built"
        )
    return [
        *gate["argv"][: entry_index + 1],
        "run",
        "--reporter=json",
        "--outputFile",
        str(gate_dir / "vitest-report.json"),
        *gate["vitest_files"],
    ]


def vitest_report_detail(expected: list[str], exit_code: int | None, report_raw: bytes) -> str | None:
    """First reason the derived vitest report is not acceptance evidence, or None.

    The report is vitest's JSON reporter document: every named test must appear
    exactly once with `status: "passed"` at module level (no `ancestorTitles`, so a
    frozen name binds one top-level declaration), the reporter's own failure count
    must be zero and its success flag true. A missing, empty, unparsable or
    summary-less report fails closed instead of reading as a pass.

    The refusals are ordered so the most specific defect is the one reported: an
    unreadable or non-object document, a non-success flag, a summary whose total is
    not an integer, a non-zero failure count, an empty result list, then the named
    tests themselves - while walking the assertions, a name declared inside a
    describe block or finished with a status other than `passed`, and afterwards a
    name reported zero times or more than once - and last a total smaller than the
    frozen named list. The walk interleaves its structural refusals in that order
    too: a file result that is not an object, a file result without an assertions
    list, an assertion that is not an object, and an assertion without a name,
    status or ancestor list each refuse before the per-name checks reach that entry,
    and the exit-code refusals are checked first of all. This paragraph is the order,
    not an inventory: every branch in this function refuses, and the order only
    decides which reason a reader sees first.
    """
    if exit_code is None:
        return "the derived vitest run timed out"
    if exit_code != 0:
        return f"the derived vitest run exited with code {exit_code}"
    try:
        report = strict_json(report_raw, "derived vitest report", MAX_CAPTURE_BYTES)
    except Refused:
        return "the derived vitest run wrote no readable JSON report"
    if not isinstance(report, dict):
        return "the derived vitest report is not a JSON object"
    if report.get("success") is not True:
        return "the derived vitest report does not record a successful run"
    total = report.get("numTotalTests")
    failed = report.get("numFailedTests")
    if isinstance(total, bool) or not isinstance(total, int):
        return "the derived vitest report carries no complete test summary"
    if isinstance(failed, bool) or not isinstance(failed, int) or failed != 0:
        return "the derived vitest report records failing tests"
    test_results = report.get("testResults")
    if not isinstance(test_results, list) or not test_results:
        return "the derived vitest report carries no test results"
    counts = dict.fromkeys(expected, 0)
    for file_result in test_results:
        if not isinstance(file_result, dict):
            return "the derived vitest report carries a malformed file result"
        assertions = file_result.get("assertionResults")
        if not isinstance(assertions, list):
            return "the derived vitest report carries a file result without assertions"
        for assertion in assertions:
            if not isinstance(assertion, dict):
                return "the derived vitest report carries a malformed assertion"
            full_name = assertion.get("fullName")
            status = assertion.get("status")
            ancestors = assertion.get("ancestorTitles")
            if not isinstance(full_name, str) or not isinstance(status, str) or not isinstance(ancestors, list):
                return "the derived vitest report carries an assertion without a name, status or ancestor list"
            if full_name not in counts:
                continue
            if ancestors:
                return f"the named test {full_name!r} is declared inside a describe block"
            if status != "passed":
                return f"the named test {full_name!r} finished with status {status!r}"
            counts[full_name] += 1
    for name in expected:
        if counts[name] == 0:
            return f"missing {name}"
        if counts[name] > 1:
            return f"{name} was reported {counts[name]} times"
    if total < len(expected):
        return "the derived vitest report carries no complete test summary"
    return None


def exunit_trace_observations(expected: list[str], stdout_raw: bytes) -> list[dict[str, Any]]:
    """Count finished `mix test --trace` observations for each expected test name.

    ANSI escapes are stripped and CR-separated progress lines are split apart first,
    because ExUnit rewrites one trace line per finished test. A passing or failing
    test prints `* <name> (<n>.<d>ms) [L#<line>]`; `(skipped)` and `(excluded)`
    lines are not finished observations.
    """
    text = EXUNIT_ANSI_PATTERN.sub("", stdout_raw.decode("utf-8", "replace"))
    observed: list[str] = []
    for line in text.splitlines():
        match = EXUNIT_OBSERVATION_PATTERN.fullmatch(line)
        if match and EXUNIT_FINISHED_PATTERN.fullmatch(match.group("descriptor")):
            observed.append(match.group("name"))
    return [{"name": name, "count": observed.count(name.replace("\n", " "))} for name in expected]


def exunit_trace_failures(stdout_raw: bytes) -> int | None:
    """Failure count from the last `mix test` result summary, or None without one."""
    text = EXUNIT_ANSI_PATTERN.sub("", stdout_raw.decode("utf-8", "replace"))
    matches = list(EXUNIT_RESULT_PATTERN.finditer(text))
    if not matches:
        return None
    last = matches[-1]
    if last.group("total") is not None:
        # `Result: N/M passed` is a complete count; the restated `Failed: K test` line
        # below it must not be added again.
        return int(last.group("total")) - int(last.group("passed"))
    tail = EXUNIT_TAIL_FAILED_PATTERN.search(last.group("tail"))
    if tail is not None:
        return int(tail.group(1))
    failed_lines = EXUNIT_FAILED_LINE_PATTERN.findall(text[last.end() :])
    return int(failed_lines[-1]) if failed_lines else 0


def exunit_trace_detail(expected: list[str], exit_code: int | None, stdout_raw: bytes) -> str | None:
    """First problem with one derived `mix test --trace` run, or None when acceptable."""
    if exit_code is None:
        return "the traced run timed out"
    if exit_code != 0:
        return f"the traced run exited with {exit_code}"
    for observation in exunit_trace_observations(expected, stdout_raw):
        if observation["count"] == 0:
            return f"missing {observation['name']}"
        if observation["count"] > 1:
            return f"{observation['name']} was observed {observation['count']} times"
    failures = exunit_trace_failures(stdout_raw)
    if failures is None:
        return "the traced run reported no ExUnit summary"
    if failures != 0:
        return f"the traced run reported {failures} failures"
    return None


def validator_argv(gate_dir: pathlib.Path, gate: dict[str, Any]) -> list[str]:
    argv = [sys.executable, str(VALIDATOR_SCRIPT), str(gate_dir / "stdout.bin")]
    if gate["go_package"]:
        argv += ["--package", gate["go_package"]]
    argv += list(gate["go_tests"])
    return argv


def run_validator(gate_dir: pathlib.Path, gate: dict[str, Any], env: dict[str, str] | None = None) -> dict[str, Any]:
    argv = validator_argv(gate_dir, gate)
    try:
        completed = subprocess.run(argv, capture_output=True, check=False, env=env)
    except OSError as error:
        raise Refused(f"could not run the Go event validator: {error}") from error
    write_bytes(gate_dir / "validator.stdout.bin", completed.stdout)
    write_bytes(gate_dir / "validator.stderr.bin", completed.stderr)
    return {
        "argv": argv,
        "exit_code": completed.returncode,
        "stdout_sha256": sha256_hex(completed.stdout),
        "stderr_sha256": sha256_hex(completed.stderr),
        "stdout_bytes": len(completed.stdout),
        "stderr_bytes": len(completed.stderr),
    }


def terminate_owned_process_tree(process: subprocess.Popen) -> None:
    """Terminate one owned gate process group and all its descendants.

    POSIX gates start in their own session, so one SIGKILL to the process group
    reaches every descendant. Windows has no portable group kill; `taskkill /T`
    walks the process tree. A failure is reported instead of continuing, because a
    surviving gate process can still mutate the checkout or hold resources.
    """
    if os.name == "nt":
        killed = subprocess.run(["taskkill", "/F", "/T", "/PID", str(process.pid)], capture_output=True, check=False)
        if killed.returncode != 0 and process.poll() is None:
            raise Refused(f"could not terminate the gate process tree (taskkill exit {killed.returncode})")
        return
    try:
        os.killpg(os.getpgid(process.pid), signal.SIGKILL)
    except OSError:
        process.kill()


def run_owned_command(
    argv: list[str], cwd: str, timeout: float | None, env: dict[str, str] | None = None
) -> tuple[int | None, bytes, bytes, bool]:
    """Run one command in an owned process group; return (exit_code, stdout, stderr, timed_out).

    A given `env` replaces the inherited environment for the command; None means the
    command inherits the runner's own environment.
    On timeout the whole tree is terminated and reaped before returning, so a later
    gate never runs next to survivors that could write into the checkout. A
    descendant that escaped the owned group (or the platform process tree) and still
    holds a pipe cannot be reached; that case marks the run timed out instead of
    hanging the runner, keeping the output the POSIX reap read before the kill (a
    Windows reader publishes pipe data only at EOF, so there the capture a survivor
    keeps open comes back empty).
    """
    created: dict[str, Any] = {}
    if os.name == "nt":
        created["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP
    else:
        created["start_new_session"] = True
    process = subprocess.Popen(argv, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, **created)
    try:
        stdout, stderr = process.communicate(timeout=timeout)
        return process.returncode, stdout, stderr, False
    except subprocess.TimeoutExpired as expired:
        stdout = expired.stdout or b""
        stderr = expired.stderr or b""
        terminate_owned_process_tree(process)
        try:
            stdout, stderr = process.communicate(timeout=PROCESS_TREE_REAP_SECONDS)
        except subprocess.TimeoutExpired as lingering:
            # POSIX `communicate` keeps every window's bytes in one buffer and reports
            # them all from the second timeout, so the capture is assigned, never
            # appended; the first window's value is the fallback for a platform whose
            # timeout carries no partial output (Windows).
            stdout = lingering.stdout or stdout
            stderr = lingering.stderr or stderr
            # A descendant the tree kill missed can still hold the pipe write ends,
            # so the reap never waits on the pipes: only the direct child is killed
            # and waited on, within its own bounded window. POSIX then closes the
            # read ends, because its `communicate` reads on this thread and the close
            # cannot queue behind the survivor, while the finalizer close at return
            # would emit `ResourceWarning: unclosed file`. Windows keeps them open:
            # a close there queues behind the reader thread parked in the pipe, and
            # that thread closes the stream once the survivor releases the write
            # ends, or process exit does.
            with contextlib.suppress(OSError):
                process.kill()
            with contextlib.suppress(subprocess.TimeoutExpired):
                process.wait(timeout=PROCESS_TREE_REAP_SECONDS)
            if os.name != "nt":
                for stream in (process.stdout, process.stderr):
                    with contextlib.suppress(OSError):
                        stream.close()
        return None, stdout, stderr, True


def tool_state_relative(gate_dir: pathlib.Path) -> str:
    """The run-relative directory a gate's tool run state is redirected to.

    The path carries the attempt (`tool-state/<attempt>/<gate>`), so a later attempt
    never executes run state written by an earlier one.
    """
    return f"{TOOL_STATE_DIRNAME}/{gate_dir.parent.name}/{gate_dir.name}"


def execute_gate(
    checkout: pathlib.Path,
    gate: dict[str, Any],
    gate_dir: pathlib.Path,
    timeout: float | None,
    run_dir: pathlib.Path,
) -> dict[str, Any]:
    gate_dir.mkdir(parents=True, exist_ok=True)
    cwd = pathlib.Path(checkout, *gate["cwd"].split("/"))
    gate_env = dict(os.environ)
    gate_env.update(gate["env"])
    # Run state a vitest/vite gate would write into the checkout is redirected into
    # the coordinator-owned run store, outside the sealed Subject; the recorded value
    # stays run-relative so a receipt states the redirect without a store path. See the
    # module docstring's tool-run-state paragraph. The directory is composed once, by
    # the same function that records it, so a caller cannot export a different path
    # than the one the result binds.
    tool_state_dir = run_dir / tool_state_relative(gate_dir)
    shutil.rmtree(tool_state_dir, ignore_errors=True)
    gate_env[TOOL_CACHE_ENV] = str(tool_state_dir)
    gate_env[TOOL_CACHE_DISABLE_ENV] = TOOL_CACHE_DISABLED
    stdout_raw = b""
    stderr_raw = b""
    exit_code: int | None = None
    timed_out = False
    error: str | None = None
    if not cwd.is_dir():
        error = f"gate cwd {gate['cwd']!r} is missing in the checkout"
    else:
        try:
            exit_code, stdout_raw, stderr_raw, timed_out = run_owned_command(gate["argv"], str(cwd), timeout, gate_env)
        except OSError as failure:
            error = f"could not execute gate argv: {failure}"
    # The derived cross-checks below run exactly when the gate command itself ran and
    # exited 0 and the gate's fields derive that cross-check's argv: a derivable
    # `go test -list` needs `go_tests` with `go_package`, a `go` argv and no `/` in a
    # test name (a subtest name has no `-list` spelling), the ExUnit trace run needs
    # `exunit_tests`, the vitest structured report needs `vitest_tests`; a plain
    # exit-0 gate derives none. Keep its error before a reported list failure is
    # appended.
    gate_command_error = error
    write_bytes(gate_dir / "stdout.bin", stdout_raw)
    write_bytes(gate_dir / "stderr.bin", stderr_raw)
    list_argv = go_list_argv(gate)
    list_ran = list_argv is not None and gate_command_error is None and not timed_out and exit_code == 0
    list_exit_code: int | None = None
    list_stdout_raw = b""
    list_stderr_raw = b""
    if list_ran:
        # The event stream alone is not execution-bound: a worker-supplied TestMain can
        # print fake pass frames and exit without running anything. The package binary
        # must also list every named test when asked for it.
        try:
            list_exit_code, list_stdout_raw, list_stderr_raw, _list_timed_out = run_owned_command(
                list_argv, str(cwd), timeout, gate_env
            )
        except OSError as failure:
            raise Refused(f"could not run the Go package test list for gate {gate['id']}: {failure}") from failure
        write_bytes(gate_dir / "go-list.stdout.bin", list_stdout_raw)
        write_bytes(gate_dir / "go-list.stderr.bin", list_stderr_raw)
        list_detail = go_list_detail(gate["go_tests"], list_exit_code, list_stdout_raw)
        if list_detail is not None:
            error = (error + "; " if error else "") + GO_LIST_FAILURE_PREFIX + list_detail
    exunit_argv = exunit_trace_argv(gate)
    exunit_ran = exunit_argv is not None and gate_command_error is None and not timed_out and exit_code == 0
    exunit_exit_code: int | None = None
    exunit_timed_out = False
    exunit_stdout_raw = b""
    exunit_stderr_raw = b""
    exunit_error: str | None = None
    if exunit_ran:
        # A module can print a passing test name and exit before running it, so the
        # derived `mix test --trace` run must finish every named test exactly once,
        # in the same cwd and environment as the gate command.
        try:
            exunit_exit_code, exunit_stdout_raw, exunit_stderr_raw, exunit_timed_out = run_owned_command(
                exunit_argv, str(cwd), timeout, gate_env
            )
        except OSError as failure:
            raise Refused(f"could not run the ExUnit trace for gate {gate['id']}: {failure}") from failure
        write_bytes(gate_dir / "exunit.stdout.bin", exunit_stdout_raw)
        write_bytes(gate_dir / "exunit.stderr.bin", exunit_stderr_raw)
        detail = exunit_trace_detail(gate["exunit_tests"], exunit_exit_code, exunit_stdout_raw)
        if detail is not None:
            exunit_error = EXUNIT_FAILURE_PREFIX + detail
    vitest_argv = vitest_report_argv(gate, gate_dir)
    vitest_ran = vitest_argv is not None and gate_command_error is None and not timed_out and exit_code == 0
    vitest_exit_code: int | None = None
    vitest_timed_out = False
    vitest_stdout_raw = b""
    vitest_stderr_raw = b""
    vitest_report_raw = b""
    vitest_error: str | None = None
    if vitest_ran:
        # A test file can print a passing name and exit before running anything, so the
        # derived run writes vitest's own JSON report and every named test must appear
        # in it exactly once with a passed status.
        try:
            vitest_exit_code, vitest_stdout_raw, vitest_stderr_raw, vitest_timed_out = run_owned_command(
                vitest_argv, str(cwd), timeout, gate_env
            )
        except OSError as failure:
            raise Refused(f"could not run the vitest structured report for gate {gate['id']}: {failure}") from failure
        write_bytes(gate_dir / "vitest.stdout.bin", vitest_stdout_raw)
        write_bytes(gate_dir / "vitest.stderr.bin", vitest_stderr_raw)
        report_path = gate_dir / "vitest-report.json"
        if report_path.is_file():
            vitest_report_raw = read_bytes(report_path, MAX_CAPTURE_BYTES, "derived vitest report")
        write_bytes(report_path, vitest_report_raw)
        detail = vitest_report_detail(gate["vitest_tests"], vitest_exit_code, vitest_report_raw)
        if detail is not None:
            vitest_error = VITEST_FAILURE_PREFIX + detail
    validator = run_validator(gate_dir, gate, gate_env) if gate["go_tests"] is not None else None
    passed = (
        error is None
        and not timed_out
        and exit_code == 0
        and (validator is None or validator["exit_code"] == 0)
        and exunit_error is None
        and vitest_error is None
    )
    return {
        "gate_id": gate["id"],
        "cwd": gate["cwd"],
        "argv": gate["argv"],
        "tool_state_dir": tool_state_relative(gate_dir),
        "go_tests": gate["go_tests"],
        "go_package": gate["go_package"],
        "env": gate["env"],
        "exunit_tests": gate["exunit_tests"],
        "exunit_files": gate["exunit_files"],
        "vitest_tests": gate["vitest_tests"],
        "vitest_files": gate["vitest_files"],
        "exit_code": exit_code,
        "timed_out": timed_out,
        "error": error,
        "stdout_sha256": sha256_hex(stdout_raw),
        "stderr_sha256": sha256_hex(stderr_raw),
        "stdout_bytes": len(stdout_raw),
        "stderr_bytes": len(stderr_raw),
        "go_list_argv": list_argv,
        "go_list_exit_code": list_exit_code if list_ran else None,
        "go_list_stdout_sha256": sha256_hex(list_stdout_raw) if list_ran else None,
        "go_list_stderr_sha256": sha256_hex(list_stderr_raw) if list_ran else None,
        "go_list_stdout_bytes": len(list_stdout_raw) if list_ran else None,
        "go_list_stderr_bytes": len(list_stderr_raw) if list_ran else None,
        "exunit_argv": exunit_argv,
        "exunit_exit_code": exunit_exit_code if exunit_ran else None,
        "exunit_timed_out": exunit_timed_out,
        "exunit_stdout_sha256": sha256_hex(exunit_stdout_raw) if exunit_ran else None,
        "exunit_stderr_sha256": sha256_hex(exunit_stderr_raw) if exunit_ran else None,
        "exunit_stdout_bytes": len(exunit_stdout_raw) if exunit_ran else None,
        "exunit_stderr_bytes": len(exunit_stderr_raw) if exunit_ran else None,
        "exunit_error": exunit_error,
        "vitest_argv": vitest_argv,
        "vitest_exit_code": vitest_exit_code if vitest_ran else None,
        "vitest_timed_out": vitest_timed_out,
        "vitest_stdout_sha256": sha256_hex(vitest_stdout_raw) if vitest_ran else None,
        "vitest_stderr_sha256": sha256_hex(vitest_stderr_raw) if vitest_ran else None,
        "vitest_stdout_bytes": len(vitest_stdout_raw) if vitest_ran else None,
        "vitest_stderr_bytes": len(vitest_stderr_raw) if vitest_ran else None,
        "vitest_report_sha256": sha256_hex(vitest_report_raw) if vitest_ran else None,
        "vitest_report_bytes": len(vitest_report_raw) if vitest_ran else None,
        "vitest_error": vitest_error,
        "validator": validator,
        "subject_hash_before": None,
        "subject_hash_after": None,
        "passed": passed,
    }


def recompute_gate(gate_dir: pathlib.Path, gate: dict[str, Any], sealed_hash: str) -> dict[str, Any]:
    result_raw = read_bytes(gate_dir / "result.json", MAX_RESULT_BYTES, "gate result")
    result = strict_json(result_raw, "gate result", MAX_RESULT_BYTES)
    require_object(result, "gate result", RESULT_KEYS, RESULT_KEYS)
    for key, expected in (
        ("gate_id", gate["id"]),
        ("cwd", gate["cwd"]),
        ("argv", gate["argv"]),
        ("go_tests", gate["go_tests"]),
        ("go_package", gate["go_package"]),
        ("env", gate["env"]),
        ("tool_state_dir", tool_state_relative(gate_dir)),
        ("exunit_tests", gate["exunit_tests"]),
        ("exunit_files", gate["exunit_files"]),
        ("vitest_tests", gate["vitest_tests"]),
        ("vitest_files", gate["vitest_files"]),
    ):
        if result[key] != expected:
            raise Refused(f"the stored result for gate {gate['id']} does not match the frozen gate {key}")
    exit_code = result["exit_code"]
    if exit_code is not None and (isinstance(exit_code, bool) or not isinstance(exit_code, int)):
        raise Refused(f"the stored exit code for gate {gate['id']} is invalid")
    timed_out = require_bool(result["timed_out"], f"gate {gate['id']} timed_out")
    error = require_optional_text(result["error"], f"gate {gate['id']} error", max_length=2000)
    list_argv = go_list_argv(gate)
    if result["go_list_argv"] != list_argv:
        raise Refused(f"the stored result for gate {gate['id']} does not match the derived Go test list command")
    exunit_argv = exunit_trace_argv(gate)
    if result["exunit_argv"] != exunit_argv:
        raise Refused(f"the stored result for gate {gate['id']} does not match the derived ExUnit trace command")
    vitest_argv = vitest_report_argv(gate, gate_dir)
    if result["vitest_argv"] != vitest_argv:
        raise Refused(f"the stored result for gate {gate['id']} does not match the derived vitest report command")
    # The stored error conflates the gate command error, a reported list failure and the
    # post-execution "source changed" note, so peel those off before deciding. The note is
    # appended bare when the gate command itself reported no error.
    gate_command_error = error
    if gate_command_error is not None:
        without_note = gate_command_error
        if without_note.endswith(SOURCE_CHANGED_SUFFIX):
            without_note = without_note[: -len(SOURCE_CHANGED_SUFFIX)]
            if without_note.endswith("; "):
                without_note = without_note[:-2]
        if not without_note or without_note.startswith(GO_LIST_FAILURE_PREFIX):
            gate_command_error = None
    list_expected = list_argv is not None and exit_code == 0 and gate_command_error is None and not timed_out
    exunit_expected = exunit_argv is not None and exit_code == 0 and gate_command_error is None and not timed_out
    vitest_expected = vitest_argv is not None and exit_code == 0 and gate_command_error is None and not timed_out
    expected_files = {"result.json", "stdout.bin", "stderr.bin"}
    if gate["go_tests"] is not None:
        expected_files |= {"validator.stdout.bin", "validator.stderr.bin"}
    if list_expected:
        expected_files |= {"go-list.stdout.bin", "go-list.stderr.bin"}
    if exunit_expected:
        expected_files |= {"exunit.stdout.bin", "exunit.stderr.bin"}
    if vitest_expected:
        expected_files |= {"vitest.stdout.bin", "vitest.stderr.bin", "vitest-report.json"}
    actual_files = sorted(entry.name for entry in gate_dir.iterdir())
    if actual_files != sorted(expected_files):
        raise Refused(f"the stored evidence for gate {gate['id']} contains unexpected files")
    subject_bound = result["subject_hash_before"] == sealed_hash and result["subject_hash_after"] == sealed_hash
    stdout_raw = read_bytes(gate_dir / "stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} stdout")
    stderr_raw = read_bytes(gate_dir / "stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} stderr")
    if result["stdout_bytes"] != len(stdout_raw) or result["stdout_sha256"] != sha256_hex(stdout_raw):
        raise Refused(f"the stored stdout for gate {gate['id']} changed after it was captured")
    if result["stderr_bytes"] != len(stderr_raw) or result["stderr_sha256"] != sha256_hex(stderr_raw):
        raise Refused(f"the stored stderr for gate {gate['id']} changed after it was captured")
    passed = subject_bound and error is None and not timed_out and exit_code == 0
    validator_exit: int | None = None
    validator_stdout: str | None = None
    validator_stderr: str | None = None
    if gate["go_tests"] is not None:
        validator = require_object(result["validator"], f"gate {gate['id']} validator", VALIDATOR_KEYS, VALIDATOR_KEYS)
        stored_stdout = read_bytes(gate_dir / "validator.stdout.bin", MAX_CAPTURE_BYTES, "validator stdout")
        stored_stderr = read_bytes(gate_dir / "validator.stderr.bin", MAX_CAPTURE_BYTES, "validator stderr")
        if (
            validator["stdout_bytes"] != len(stored_stdout)
            or validator["stdout_sha256"] != sha256_hex(stored_stdout)
            or validator["stderr_bytes"] != len(stored_stderr)
            or validator["stderr_sha256"] != sha256_hex(stored_stderr)
        ):
            raise Refused(f"the stored Go event validation for gate {gate['id']} changed after it ran")
        gate_env = dict(os.environ)
        gate_env.update(gate["env"])
        try:
            rerun = subprocess.run(validator_argv(gate_dir, gate), capture_output=True, check=False, env=gate_env)
        except OSError as failure:
            raise Refused(f"could not re-run the Go event validator: {failure}") from failure
        if (
            rerun.returncode != validator["exit_code"]
            or sha256_hex(rerun.stdout) != validator["stdout_sha256"]
            or sha256_hex(rerun.stderr) != validator["stderr_sha256"]
        ):
            raise Refused(f"the stored Go event validation for gate {gate['id']} does not reproduce")
        validator_exit = require_integer(validator["exit_code"], f"gate {gate['id']} validator exit_code", -4096)
        passed = passed and validator_exit == 0
        validator_stdout = validator["stdout_sha256"]
        validator_stderr = validator["stderr_sha256"]
    elif result["validator"] is not None:
        raise Refused(f"gate {gate['id']} stores Go event validation without go_tests")
    if list_expected:
        list_exit_code = require_optional_integer(
            result["go_list_exit_code"], f"gate {gate['id']} go_list_exit_code", -4096
        )
        list_stdout_raw = read_bytes(
            gate_dir / "go-list.stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} Go test list stdout"
        )
        list_stderr_raw = read_bytes(
            gate_dir / "go-list.stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} Go test list stderr"
        )
        if (
            result["go_list_stdout_sha256"] is None
            or result["go_list_stdout_bytes"] is None
            or result["go_list_stderr_sha256"] is None
            or result["go_list_stderr_bytes"] is None
        ):
            raise Refused(f"gate {gate['id']} does not record the executed Go test list output")
        if (
            require_digest(result["go_list_stdout_sha256"], f"gate {gate['id']} go_list_stdout_sha256")
            != sha256_hex(list_stdout_raw)
            or require_integer(result["go_list_stdout_bytes"], f"gate {gate['id']} go_list_stdout_bytes")
            != len(list_stdout_raw)
            or require_digest(result["go_list_stderr_sha256"], f"gate {gate['id']} go_list_stderr_sha256")
            != sha256_hex(list_stderr_raw)
            or require_integer(result["go_list_stderr_bytes"], f"gate {gate['id']} go_list_stderr_bytes")
            != len(list_stderr_raw)
        ):
            raise Refused(f"the stored Go test list output for gate {gate['id']} changed after it was captured")
        if go_list_detail(gate["go_tests"], list_exit_code, list_stdout_raw) is not None:
            passed = False
    else:
        for key in (
            "go_list_exit_code",
            "go_list_stdout_sha256",
            "go_list_stderr_sha256",
            "go_list_stdout_bytes",
            "go_list_stderr_bytes",
        ):
            if result[key] is not None:
                raise Refused(f"gate {gate['id']} records Go test list evidence without an executed list run")
    exunit_error: str | None = None
    exunit_exit_code: int | None = None
    if exunit_expected:
        exunit_stdout_raw = read_bytes(
            gate_dir / "exunit.stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} ExUnit trace stdout"
        )
        exunit_stderr_raw = read_bytes(
            gate_dir / "exunit.stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} ExUnit trace stderr"
        )
        if (
            result["exunit_stdout_sha256"] is None
            or result["exunit_stdout_bytes"] is None
            or result["exunit_stderr_sha256"] is None
            or result["exunit_stderr_bytes"] is None
        ):
            raise Refused(f"gate {gate['id']} does not record the executed ExUnit trace output")
        if (
            require_digest(result["exunit_stdout_sha256"], f"gate {gate['id']} exunit_stdout_sha256")
            != sha256_hex(exunit_stdout_raw)
            or require_integer(result["exunit_stdout_bytes"], f"gate {gate['id']} exunit_stdout_bytes")
            != len(exunit_stdout_raw)
            or require_digest(result["exunit_stderr_sha256"], f"gate {gate['id']} exunit_stderr_sha256")
            != sha256_hex(exunit_stderr_raw)
            or require_integer(result["exunit_stderr_bytes"], f"gate {gate['id']} exunit_stderr_bytes")
            != len(exunit_stderr_raw)
        ):
            raise Refused(f"the stored ExUnit trace output for gate {gate['id']} changed after it was captured")
        exunit_exit_code = require_optional_integer(
            result["exunit_exit_code"], f"gate {gate['id']} exunit_exit_code", -4096
        )
        exunit_timed_out = require_bool(result["exunit_timed_out"], f"gate {gate['id']} exunit_timed_out")
        if exunit_timed_out != (exunit_exit_code is None):
            raise Refused(f"the stored ExUnit trace run for gate {gate['id']} disagrees with its exit code")
        detail = exunit_trace_detail(gate["exunit_tests"], exunit_exit_code, exunit_stdout_raw)
        exunit_error = None if detail is None else EXUNIT_FAILURE_PREFIX + detail
        passed = passed and exunit_error is None
    else:
        for key in (
            "exunit_exit_code",
            "exunit_stdout_sha256",
            "exunit_stderr_sha256",
            "exunit_stdout_bytes",
            "exunit_stderr_bytes",
            "exunit_error",
        ):
            if result[key] is not None:
                raise Refused(f"gate {gate['id']} records ExUnit trace evidence without an executed trace run")
        if result["exunit_timed_out"] is not False:
            raise Refused(f"gate {gate['id']} records ExUnit trace evidence without an executed trace run")
    vitest_error: str | None = None
    vitest_exit_code: int | None = None
    if vitest_expected:
        vitest_stdout_raw = read_bytes(
            gate_dir / "vitest.stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} vitest report stdout"
        )
        vitest_stderr_raw = read_bytes(
            gate_dir / "vitest.stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} vitest report stderr"
        )
        vitest_report_raw = read_bytes(
            gate_dir / "vitest-report.json", MAX_CAPTURE_BYTES, f"gate {gate['id']} vitest report"
        )
        if (
            result["vitest_stdout_sha256"] is None
            or result["vitest_stdout_bytes"] is None
            or result["vitest_stderr_sha256"] is None
            or result["vitest_stderr_bytes"] is None
            or result["vitest_report_sha256"] is None
            or result["vitest_report_bytes"] is None
        ):
            raise Refused(f"gate {gate['id']} does not record the executed vitest report output")
        if (
            require_digest(result["vitest_stdout_sha256"], f"gate {gate['id']} vitest_stdout_sha256")
            != sha256_hex(vitest_stdout_raw)
            or require_integer(result["vitest_stdout_bytes"], f"gate {gate['id']} vitest_stdout_bytes")
            != len(vitest_stdout_raw)
            or require_digest(result["vitest_stderr_sha256"], f"gate {gate['id']} vitest_stderr_sha256")
            != sha256_hex(vitest_stderr_raw)
            or require_integer(result["vitest_stderr_bytes"], f"gate {gate['id']} vitest_stderr_bytes")
            != len(vitest_stderr_raw)
            or require_digest(result["vitest_report_sha256"], f"gate {gate['id']} vitest_report_sha256")
            != sha256_hex(vitest_report_raw)
            or require_integer(result["vitest_report_bytes"], f"gate {gate['id']} vitest_report_bytes")
            != len(vitest_report_raw)
        ):
            raise Refused(f"the stored vitest report for gate {gate['id']} changed after it was captured")
        vitest_exit_code = require_optional_integer(
            result["vitest_exit_code"], f"gate {gate['id']} vitest_exit_code", -4096
        )
        vitest_timed_out = require_bool(result["vitest_timed_out"], f"gate {gate['id']} vitest_timed_out")
        if vitest_timed_out != (vitest_exit_code is None):
            raise Refused(f"the stored vitest report run for gate {gate['id']} disagrees with its exit code")
        detail = vitest_report_detail(gate["vitest_tests"], vitest_exit_code, vitest_report_raw)
        vitest_error = None if detail is None else VITEST_FAILURE_PREFIX + detail
        passed = passed and vitest_error is None
    else:
        for key in (
            "vitest_exit_code",
            "vitest_stdout_sha256",
            "vitest_stderr_sha256",
            "vitest_stdout_bytes",
            "vitest_stderr_bytes",
            "vitest_report_sha256",
            "vitest_report_bytes",
            "vitest_error",
        ):
            if result[key] is not None:
                raise Refused(f"gate {gate['id']} records vitest report evidence without an executed report run")
        if result["vitest_timed_out"] is not False:
            raise Refused(f"gate {gate['id']} records vitest report evidence without an executed report run")
    if result["passed"] is not passed:
        raise Refused(
            f"the stored result for gate {gate['id']} claims passed={result['passed']} but its evidence says {passed}"
        )
    return {
        "id": gate["id"],
        "cwd": gate["cwd"],
        "argv": gate["argv"],
        "tool_state_dir": tool_state_relative(gate_dir),
        "go_tests": gate["go_tests"],
        "go_package": gate["go_package"],
        "env": gate["env"],
        "exunit_tests": gate["exunit_tests"],
        "exunit_files": gate["exunit_files"],
        "vitest_tests": gate["vitest_tests"],
        "vitest_files": gate["vitest_files"],
        "exit_code": exit_code,
        "timed_out": timed_out,
        "error": error,
        "exunit_exit_code": exunit_exit_code,
        "exunit_error": exunit_error,
        "vitest_exit_code": vitest_exit_code,
        "vitest_error": vitest_error,
        "subject_bound": subject_bound,
        "passed": passed,
        "stdout_sha256": result["stdout_sha256"],
        "stderr_sha256": result["stderr_sha256"],
        "validator_exit_code": validator_exit,
        "validator_stdout_sha256": validator_stdout,
        "validator_stderr_sha256": validator_stderr,
        "result_sha256": sha256_hex(result_raw),
    }


def _is_junction(path: pathlib.Path) -> bool:
    """True for a Windows junction, which is a link but not `path.is_symlink()`."""
    isjunction = getattr(os.path, "isjunction", None)
    return bool(isjunction is not None and isjunction(path))


def _file_content_digest(path: pathlib.Path) -> bytes:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest().encode("ascii")


def _within(root: pathlib.Path, candidate: pathlib.Path) -> bool:
    try:
        candidate.relative_to(root)
    except ValueError:
        return False
    return True


def _walk_error(error: OSError) -> None:
    # os.walk swallows a directory it cannot list unless onerror refuses: a silently
    # skipped subtree would be an unbound hole in the digest.
    raise Refused(f"cannot walk the dependency tree at {error.filename}: {error}")


def _link_payload(link: pathlib.Path, root: pathlib.Path, seen: frozenset) -> bytes:
    """The digest payload of one link: its text, where it resolves, and any outside content.

    A link inside the tree is bound by the walked entries the target names, and by the
    tree-relative route its chain resolves to: a link that leaves the tree and comes
    back (a junction or symlink hop) can be retargeted to another in-tree directory
    without changing either link's text, and the gates would then execute different
    bytes under the same recorded digest. A link whose resolved target leaves the tree
    is what pnpm's workspace links look like (`@symmetry/contracts -> ../../contracts`):
    the target's own content is executed by the gates, so it is digested here instead
    of being left unbound; a link cycle refuses rather than recursing forever.
    """
    try:
        target_text = os.readlink(link).encode("utf-8", "surrogateescape")
    except OSError as error:
        raise Refused(f"cannot read dependency tree link {link}: {error}") from error
    try:
        resolved = link.resolve(strict=True)
    except (OSError, RuntimeError) as error:
        # `RuntimeError` is a link (or junction) chain that loops: the target never
        # resolves, so the tree cannot be bound and the run is refused, not crashed.
        raise Refused(f"the dependency tree link {link} does not resolve: {error}") from error
    if _within(root, resolved):
        relative = resolved.relative_to(root).as_posix()
        return b"in-tree\0" + target_text + b"\0" + relative.encode("utf-8")
    if resolved in seen:
        raise Refused(f"the dependency tree link {link} resolves into a cycle at {resolved}")
    return b"out-of-tree\0" + target_text + b"\0" + _target_content_digest(resolved, seen | {resolved})


def _target_content_digest(path: pathlib.Path, seen: frozenset) -> bytes:
    """Content digest of one out-of-tree link target (a file, or a directory tree)."""
    if path.is_dir():
        digest = hashlib.sha256()
        digest.update(b"directory\0")
        for relative, kind, payload in _tree_entries(path, seen):
            digest.update(kind.encode("ascii") + b"\0" + relative.encode("utf-8") + b"\0" + payload + b"\n")
        return digest.hexdigest().encode("ascii")
    try:
        return _file_content_digest(path)
    except OSError as error:
        raise Refused(f"cannot read the dependency tree link target {path}: {error}") from error


def _tree_entries(root: pathlib.Path, seen: frozenset) -> list[tuple[str, str, bytes]]:
    """Every file and link under ``root`` as sorted (relative path, kind, payload)."""
    entries: list[tuple[str, str, bytes]] = []
    for walk_root, dirnames, filenames in os.walk(root, followlinks=False, onerror=_walk_error):
        for dirname in list(dirnames):
            candidate = pathlib.Path(walk_root, dirname)
            relative = candidate.relative_to(root).as_posix()
            if candidate.is_symlink() or _is_junction(candidate):
                dirnames.remove(dirname)
                entries.append((relative, "link", _link_payload(candidate, root, seen)))
                continue
            try:
                info = candidate.lstat()
            except OSError as error:
                raise Refused(f"cannot read dependency tree entry {relative}: {error}") from error
            if getattr(info, "st_reparse_tag", 0):
                # A reparse point that is neither a symlink nor a detectable junction
                # cannot be classified, so it refuses instead of being walked into.
                raise Refused(f"unsupported reparse-point directory in the dependency tree: {relative}")
        for name in filenames:
            candidate = pathlib.Path(walk_root, name)
            relative = candidate.relative_to(root).as_posix()
            if candidate.is_symlink():
                entries.append((relative, "link", _link_payload(candidate, root, seen)))
            else:
                try:
                    entries.append((relative, "file", _file_content_digest(candidate)))
                except OSError as error:
                    raise Refused(f"cannot read dependency tree entry {relative}: {error}") from error
    try:
        entries.sort(key=lambda entry: entry[0].encode("utf-8"))
    except UnicodeEncodeError:
        raise Refused("the dependency tree contains a path that is not valid UTF-8") from None
    return entries


def dependency_tree_digest(tree: pathlib.Path) -> tuple[str, int]:
    """Deterministic digest of every file and link of a resolved dependency tree.

    Each entry contributes its relative path (UTF-8 bytes), its kind and its payload in
    one sorted stream, so two trees with the same entries digest identically regardless
    of traversal order: a file contributes its content digest, a link its target text
    plus the tree-relative route it resolves to (an in-tree junction hop cannot be
    retargeted unseen) - and, when the target resolves outside the tree, that target's
    own content digest, because the gates execute it (pnpm's workspace links point at
    source outside the hoisted tree). Directory links are pruned instead of followed (a
    cycle refuses); empty directories are not entries.
    """
    try:
        resolved_tree = tree.resolve()
    except (OSError, RuntimeError) as error:
        # A tree path that is itself a link loop never resolves: refuse, never crash.
        raise Refused(f"the dependency tree is not a readable directory: {error}") from error
    entries = _tree_entries(resolved_tree, frozenset({resolved_tree}))
    digest = hashlib.sha256()
    digest.update(DEPENDENCY_TREE_NAMESPACE + b"\0")
    for relative, kind, payload in entries:
        digest.update(kind.encode("ascii") + b"\0" + relative.encode("utf-8") + b"\0" + payload + b"\n")
    return sha256_hex(digest.digest()), len(entries)


def verify_dependency_tree(run_dir: pathlib.Path, provisioning: dict[str, Any]) -> None:
    """Recompute the recorded dependency-tree digest and refuse a missing or changed tree.

    The recorded route is run-relative, so this re-verification works from the stored run
    directory alone: a tree that was edited, or pruned after the run, refuses instead of
    leaving the recorded binding unverifiable.
    """
    tree = run_dir / provisioning["tree"]
    if not tree.is_dir():
        raise Refused(f"the recorded dependency tree {provisioning['tree']!r} is missing")
    digest, entries = dependency_tree_digest(tree)
    if digest != provisioning["tree_digest"] or entries != provisioning["tree_entries"]:
        raise Refused(
            "the recorded dependency tree changed after it was recorded: "
            f"{digest} over {entries} entries != {provisioning['tree_digest']} over "
            f"{provisioning['tree_entries']} entries"
        )


def _dependency_tree(value: str) -> pathlib.Path:
    tree = pathlib.Path(value)
    try:
        resolved = tree.resolve(strict=True)
    except (OSError, RuntimeError) as error:
        # A tree whose own path is a link loop never resolves; refuse it as unreadable.
        raise Refused(f"the dependency tree is not a readable directory: {error}") from error
    if not resolved.is_dir():
        raise Refused("--tree must name a directory")
    return resolved


def holds_a_package_entry(node_modules: pathlib.Path) -> bool:
    """True when a `node_modules` holds an entry Node could resolve a package from.

    Node walks past a `node_modules` that lacks the package it needs, so "has any
    entry" is not "the tree the gates execute from". Vite's config loader writes its
    temp files into the nearest existing `node_modules` - the placeholder `prepare`
    creates - so a dot-only directory such as `.vite-temp` must not turn that
    placeholder into the resolved tree and refuse the true hoisted binding
    (STD-A03). A package entry is a non-dot directory, the scoped `@name` directory
    a package installs under included; dot entries (`.bin`, `.vite-temp`, `.vite`,
    `.modules.yaml`, ...) and plain files answer for no package.
    """
    try:
        with os.scandir(node_modules) as entries:
            for entry in entries:
                if entry.name.startswith("."):
                    continue
                if entry.is_dir():
                    return True
    except OSError as error:
        raise Refused(f"cannot list the dependency-tree candidate {node_modules}: {error}") from error
    return False


def resolved_dependency_tree(checkout: pathlib.Path, gate_cwd: str) -> pathlib.Path | None:
    """The `node_modules` tree Node reaches first from one gate's cwd, if any.

    Node resolves a module by walking the requiring directory's ancestors and trying
    each `node_modules` directory it passes; a directory holding no package entry
    answers for no package, so the first such directory on the chain is the tree the
    gates execute from (the runbook's hoisted install sits one level above the run
    directory, past the placeholder `prepare` creates - which Vite later fills with
    dot-only temp state). `None` means nothing on the chain holds a package entry,
    so no tree can be bound as the executed one.
    """
    directory = pathlib.Path(checkout, *gate_cwd.split("/"))
    while True:
        candidate = directory / "node_modules"
        if candidate.is_dir() and holds_a_package_entry(candidate):
            try:
                return candidate.resolve()
            except (OSError, RuntimeError) as error:
                raise Refused(f"the dependency tree at {candidate} does not resolve: {error}") from error
        parent = directory.parent
        if parent == directory:
            return None
        directory = parent


def require_resolved_dependency_tree(run_dir: pathlib.Path, task: dict[str, Any], tree: pathlib.Path) -> None:
    """Refuse a bound tree the checkout's gates do not resolve their modules from.

    The recorded binding attests the tree the gates executed from, so the tree must
    be the one each gate's cwd resolves first: a receipt that bound a decoy tree
    would attest a tree no gate ever executed (the F01 review probe). This runs on
    the bind path (where the binding is created) and on `--check` (where a stale
    binding would otherwise keep verifying a tree the gates no longer use).
    """
    checkout = run_dir / "checkout"
    if not checkout.is_dir():
        raise Refused("the run has no checkout to resolve the dependency tree from; run prepare first")
    for gate in task["gates"]:
        resolved = resolved_dependency_tree(checkout, gate["cwd"])
        if resolved is None:
            raise Refused(
                f"the checkout resolves no dependency tree from gate {gate['id']}'s cwd, so "
                f"--tree {tree} is not the tree the gates execute from"
            )
        if resolved != tree:
            raise Refused(
                f"--tree {tree} is not the dependency tree the gates execute from: gate "
                f"{gate['id']} (cwd {gate['cwd']!r}) resolves {resolved} first"
            )
