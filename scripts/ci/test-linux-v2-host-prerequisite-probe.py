#!/usr/bin/env python3
"""Dependency-free regression tests for the host prerequisite probe's unit-stop path validation."""

from __future__ import annotations

import os
import pathlib
import shlex
import subprocess
import unittest

SCRIPT = pathlib.Path(__file__).with_name("linux-v2-host-prerequisite-probe.sh")
UNIT = f"symmetry-ci-negtest-{os.getpid()}.service"
VALID_PATH = f"/system.slice/{UNIT}"

# The systemd calls are stubbed in-process: these cases pin unit_stopped_proven's own guards, not systemd.
HARNESS = """\
set -Eeuo pipefail
sudo() {
  if [[ "$1" != systemctl || "$2" != show ]]; then
    return 1
  fi
  local property= argument
  for argument in "$@"; do
    case "$argument" in
      --property=*) property=${argument#--property=} ;;
    esac
  done
  if [[ "$property" == "${PROBE_FAIL_PROPERTY:-}" ]]; then
    return 1
  fi
  case "$property" in
    LoadState) printf '%s\\n' "$PROBE_LOAD_STATE" ;;
    ActiveState) printf '%s\\n' "$PROBE_ACTIVE_STATE" ;;
    MainPID) printf '%s\\n' "$PROBE_MAIN_PID" ;;
    *) return 1 ;;
  esac
}

@@FUNCTION@@

if @@CALL@@; then
  exit 0
fi
exit 1
"""


def unit_stopped_proven_function() -> str:
    lines = SCRIPT.read_text(encoding="utf-8").splitlines()
    start = lines.index("unit_stopped_proven() {")
    end = lines.index("}", start)
    return "\n".join(lines[start : end + 1])


def unit_stopped_proven(recorded_cgroup: str, **states: str) -> subprocess.CompletedProcess:
    environment = dict(os.environ)
    environment.update(
        {
            "PROBE_LOAD_STATE": states.get("load_state", "not-found"),
            "PROBE_ACTIVE_STATE": states.get("active_state", "inactive"),
            "PROBE_MAIN_PID": states.get("main_pid", "0"),
            "PROBE_FAIL_PROPERTY": states.get("fail_property", ""),
        }
    )
    script = HARNESS.replace("@@FUNCTION@@", unit_stopped_proven_function())
    script = script.replace("@@CALL@@", f"unit_stopped_proven {shlex.quote(UNIT)} {shlex.quote(recorded_cgroup)}")
    return subprocess.run(
        ["bash", "-c", script],
        capture_output=True,
        text=True,
        env=environment,
        check=False,
    )


class UnitStoppedProvenPathValidation(unittest.TestCase):
    def assert_rejected(self, recorded_cgroup: str, **states: str) -> None:
        result = unit_stopped_proven(recorded_cgroup, **states)
        self.assertEqual(result.returncode, 1, f"recorded path accepted: {recorded_cgroup!r} {states} {result.stderr}")

    def assert_accepted(self, recorded_cgroup: str, **states: str) -> None:
        result = unit_stopped_proven(recorded_cgroup, **states)
        self.assertEqual(result.returncode, 0, f"recorded path rejected: {recorded_cgroup!r} {states} {result.stderr}")

    def test_extracted_function_keeps_its_guards(self) -> None:
        function = unit_stopped_proven_function()
        for guard in (
            '"${recorded_cgroup##*/}" == "$unit_name"',
            '"${recorded_directory##*/}" == *.slice',
            '"$main_pid" == 0',
            "'populated 0'",
        ):
            self.assertIn(guard, function)

    def test_empty_recorded_path_is_rejected(self) -> None:
        self.assert_rejected("")

    def test_root_recorded_path_is_rejected(self) -> None:
        self.assert_rejected("/")

    def test_relative_recorded_path_is_rejected(self) -> None:
        self.assert_rejected(f"system.slice/{UNIT}")

    def test_parent_traversal_is_rejected(self) -> None:
        self.assert_rejected(f"/system.slice/../system.slice/{UNIT}")

    def test_foreign_unit_name_is_rejected(self) -> None:
        self.assert_rejected("/system.slice/symmetry-ci-foreign.service")

    def test_non_slice_parent_is_rejected(self) -> None:
        self.assert_rejected(f"/system.slice/delegated/{UNIT}")

    def test_running_unit_is_rejected(self) -> None:
        self.assert_rejected(VALID_PATH, load_state="loaded", active_state="active", main_pid="4242")

    def test_activating_unit_is_rejected(self) -> None:
        self.assert_rejected(VALID_PATH, load_state="loaded", active_state="activating", main_pid="4242")

    def test_nonzero_main_pid_is_rejected(self) -> None:
        self.assert_rejected(VALID_PATH, main_pid="4242")

    def test_failed_systemctl_query_is_rejected(self) -> None:
        self.assert_rejected(VALID_PATH, fail_property="LoadState")

    def test_collected_stopped_unit_is_accepted(self) -> None:
        self.assert_accepted(VALID_PATH)

    def test_failed_stopped_unit_is_accepted(self) -> None:
        self.assert_accepted(VALID_PATH, load_state="loaded", active_state="failed", main_pid="0")


if __name__ == "__main__":
    unittest.main(verbosity=2)
