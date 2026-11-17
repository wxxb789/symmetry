#!/usr/bin/env python3
"""Unit tests for the shared Go test JSON gate runner."""

import importlib.util
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("go-test-json-gate.py")
SPEC = importlib.util.spec_from_file_location("go_test_json_gate", SCRIPT)
GATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GATE)

PACKAGE = "github.com/wxxb789/symmetry/daemon/internal/harness/pi"
TEST = "TestProviderBridgeCloseReleasesBoundPeerFD"


def events(entries):
    lines = [
        json.dumps({"Action": action, "Package": package, "Test": test}) + "\n"
        for package, action, test in entries
    ]
    return "".join(lines)


def passing_events(test=TEST, package=PACKAGE):
    return events([(package, "run", test), (package, "pass", test)])


def fake_command(body, exit_code=0):
    """A stand-in for `go test -json` that needs no Go toolchain."""
    source = (
        "import sys\n"
        "sys.stderr.write('tool diagnostic\\n')\n"
        f"sys.stdout.write({body!r})\n"
        "sys.stdout.flush()\n"
        f"sys.exit({exit_code})\n"
    )
    return [sys.executable, "-c", source]


class GateTestCase(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = pathlib.Path(self.directory.name)
        self.events_path = self.root / "events.json"
        self.log_path = self.root / "gate.log"
        self.provenance_path = self.root / "provenance.txt"

    def run_gate(self, options, command):
        return subprocess.run(
            [sys.executable, str(SCRIPT), *options, "--", *command],
            capture_output=True,
            text=True,
            check=False,
        )

    def group(self, package, *tests):
        return ["--assert", package, *tests]

    def base_options(self, *extra):
        return [
            "--events",
            str(self.events_path),
            "--log",
            str(self.log_path),
            *extra,
        ]


class RunAndAssertTests(GateTestCase):
    def test_passing_command_and_named_test_exit_zero(self):
        result = self.run_gate(
            self.base_options(*self.group(PACKAGE, TEST)),
            fake_command(passing_events()),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.events_path.read_text(encoding="utf-8"), passing_events())
        self.assertIn("verified TestProviderBridgeCloseReleasesBoundPeerFD", result.stdout)

    def test_tee_writes_events_and_stderr_to_the_log(self):
        result = self.run_gate(
            self.base_options(*self.group(PACKAGE, TEST)),
            fake_command(passing_events()),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        log = self.log_path.read_text(encoding="utf-8")
        self.assertTrue(log.startswith(passing_events()))
        self.assertIn("tool diagnostic", log)
        self.assertIn("tool diagnostic", result.stderr)
        self.assertNotIn("tool diagnostic", result.stdout)

    def test_test_failure_is_fatal_and_preserves_the_test_status(self):
        result = self.run_gate(
            self.base_options(
                "--provenance",
                str(self.provenance_path),
                "--exit-status-key",
                "provider_bridge_smoke_exit_status",
                "--gate-status-key",
                "provider_bridge_smoke_status",
                *self.group(PACKAGE, TEST),
            ),
            fake_command(passing_events(), exit_code=3),
        )
        self.assertEqual(result.returncode, 3)
        self.assertEqual(
            self.provenance_path.read_text(encoding="utf-8"),
            "provider_bridge_smoke_exit_status=3\n"
            "provider_bridge_smoke_status=failed: go test exited with status 3\n",
        )

    def test_missing_named_test_fails_the_assertion(self):
        result = self.run_gate(
            self.base_options(
                "--provenance",
                str(self.provenance_path),
                "--exit-status-key",
                "e2e_exit_status",
                "--gate-status-key",
                "gate_status",
                *self.group(PACKAGE, TEST),
            ),
            fake_command(events([(PACKAGE, "run", "TestSomethingElse")])),
        )
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            self.provenance_path.read_text(encoding="utf-8"),
            "e2e_exit_status=0\n"
            "gate_status=failed: selective witness validation exited with status 1\n",
        )

    def test_failing_test_wins_over_the_assertion_status(self):
        result = self.run_gate(
            self.base_options(*self.group(PACKAGE, TEST)),
            fake_command("", exit_code=5),
        )
        self.assertEqual(result.returncode, 5)

    def test_passed_provenance_record(self):
        result = self.run_gate(
            self.base_options(
                "--provenance",
                str(self.provenance_path),
                "--exit-status-key",
                "provider_bridge_smoke_exit_status",
                "--gate-status-key",
                "provider_bridge_smoke_status",
                *self.group(PACKAGE, TEST),
            ),
            fake_command(passing_events()),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.provenance_path.read_text(encoding="utf-8"),
            "provider_bridge_smoke_exit_status=0\nprovider_bridge_smoke_status=passed\n",
        )

    def test_unscoped_group_accepts_any_package(self):
        result = self.run_gate(
            self.base_options(*self.group("*", TEST)),
            fake_command(passing_events(package="example.com/other")),
        )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_every_group_is_evaluated(self):
        result = self.run_gate(
            self.base_options(
                "--assert",
                PACKAGE,
                "TestAbsent",
                "--assert",
                PACKAGE,
                TEST,
            ),
            fake_command(passing_events()),
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("verified TestProviderBridgeCloseReleasesBoundPeerFD", result.stdout)

    def test_powershell_command_separator_is_accepted(self):
        result = subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "--events",
                str(self.events_path),
                *self.group(PACKAGE, TEST),
                "--command",
                *fake_command(passing_events()),
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.events_path.read_text(encoding="utf-8"), passing_events())

    def test_assertion_script_is_the_sibling_gate(self):
        self.assertEqual(GATE.ASSERTION_SCRIPT.name, "assert-go-test-json.py")
        self.assertTrue(GATE.ASSERTION_SCRIPT.is_file())


class CaptureTests(GateTestCase):
    def test_explicit_stderr_destination(self):
        stderr_path = self.root / "tool.stderr"
        result = self.run_gate(
            self.base_options("--stderr", str(stderr_path), *self.group(PACKAGE, TEST)),
            fake_command(passing_events()),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(stderr_path.read_text(encoding="utf-8"), "tool diagnostic\n")
        self.assertEqual(self.events_path.read_text(encoding="utf-8"), passing_events())

    def test_missing_command_executable_fails_closed(self):
        result = self.run_gate(
            self.base_options(*self.group(PACKAGE, TEST)),
            ["definitely-not-a-real-binary-8f3a2b"],
        )
        self.assertEqual(result.returncode, 127)
        self.assertIn("could not run the Go test command", result.stderr)

    def test_unwritable_log_fails_closed(self):
        result = self.run_gate(
            [
                "--events",
                str(self.events_path),
                "--log",
                str(self.root),
                *self.group(PACKAGE, TEST),
            ],
            fake_command(passing_events()),
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("could not append to the gate log", result.stderr)

    def test_unwritable_provenance_fails_closed(self):
        result = self.run_gate(
            self.base_options(
                "--provenance",
                str(self.root),
                "--exit-status-key",
                "e2e_exit_status",
                *self.group(PACKAGE, TEST),
            ),
            fake_command(passing_events()),
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("could not append to the gate provenance", result.stderr)


class StdioBoundaryTests(GateTestCase):
    def test_boundary_runs_the_command_and_preserves_events(self):
        result = self.run_gate(
            self.base_options("--stdio-boundary", *self.group(PACKAGE, TEST)),
            fake_command(passing_events()),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.events_path.read_text(encoding="utf-8"), passing_events())

    def test_boundary_preserves_the_test_status(self):
        result = self.run_gate(
            self.base_options("--stdio-boundary", *self.group(PACKAGE, TEST)),
            fake_command(passing_events(), exit_code=7),
        )
        self.assertEqual(result.returncode, 7)

    @unittest.skipUnless(os.name == "posix", "the stdio-only boundary is a Linux descriptor contract")
    def test_boundary_leaves_only_stdio_inherited(self):
        inspector = (
            "import os, sys\n"
            "extra = []\n"
            "for fd in sorted(int(name) for name in os.listdir('/proc/self/fd')):\n"
            "    if fd <= 2:\n"
            "        continue\n"
            "    target = os.readlink(f'/proc/self/fd/{fd}')\n"
            "    if target != '/proc/self/fd':\n"
            "        extra.append((fd, target))\n"
            "if extra:\n"
            "    print(f'unexpected inherited descriptors: {extra}', file=sys.stderr)\n"
            "    raise SystemExit(9)\n"
        )
        command = [sys.executable, "-c", inspector]
        result = self.run_gate(
            self.base_options(*self.group(PACKAGE, TEST)),
            command,
        )
        self.assertEqual(result.returncode, 1)  # no events, so the named gate fails closed
        result = self.run_gate(
            self.base_options("--stdio-boundary", *self.group(PACKAGE, TEST)),
            command,
        )
        self.assertEqual(result.returncode, 1)
        self.assertNotIn("unexpected inherited descriptors", result.stderr)


class ArgumentTests(unittest.TestCase):
    def run_script(self, arguments):
        return subprocess.run(
            [sys.executable, str(SCRIPT), *arguments],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_command_separator_is_required(self):
        result = self.run_script(["--events", "events.json", TEST])
        self.assertEqual(result.returncode, 1)
        self.assertIn("requires `--`", result.stderr)

    def test_command_is_required(self):
        result = self.run_script(["--events", "events.json", "--assert", PACKAGE, TEST, "--"])
        self.assertEqual(result.returncode, 1)
        self.assertIn("requires a command", result.stderr)

    def test_assertion_group_is_required(self):
        result = self.run_script(["--events", "events.json", "--", "true"])
        self.assertEqual(result.returncode, 1)
        self.assertIn("requires at least one --assert group", result.stderr)


class StatusTests(unittest.TestCase):
    def test_negative_returncodes_map_to_shell_statuses(self):
        self.assertEqual(GATE.process_status(0), 0)
        self.assertEqual(GATE.process_status(3), 3)
        self.assertEqual(GATE.process_status(-15), 143)


class StdioBoundaryScriptTests(unittest.TestCase):
    """The caller boundary the gate runner and the daemon race pass both use."""

    def run_boundary(self, command):
        return subprocess.run(
            [sys.executable, str(GATE.BOUNDARY_SCRIPT), *command],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_boundary_is_the_documented_sibling(self):
        self.assertEqual(GATE.BOUNDARY_SCRIPT.name, "stdio-only-caller-boundary.py")

    def test_boundary_preserves_the_command_status_and_output(self):
        result = self.run_boundary(
            [sys.executable, "-c", "import sys; print('streamed'); sys.exit(4)"]
        )
        self.assertEqual(result.returncode, 4)
        self.assertEqual(result.stdout, "streamed\n")

    def test_boundary_without_a_command_fails_closed(self):
        result = self.run_boundary([])
        self.assertEqual(result.returncode, 2)
        self.assertIn("requires a command", result.stderr)

    @unittest.skipUnless(os.name == "posix", "the stdio-only boundary is a Linux descriptor contract")
    def test_boundary_leaves_only_stdio_inherited(self):
        inspector = (
            "import os, sys\n"
            "extra = []\n"
            "for fd in sorted(int(name) for name in os.listdir('/proc/self/fd')):\n"
            "    if fd <= 2:\n"
            "        continue\n"
            "    target = os.readlink(f'/proc/self/fd/{fd}')\n"
            "    if target != '/proc/self/fd':\n"
            "        extra.append((fd, target))\n"
            "if extra:\n"
            "    print(f'unexpected inherited descriptors: {extra}', file=sys.stderr)\n"
            "    raise SystemExit(9)\n"
        )
        result = self.run_boundary([sys.executable, "-c", inspector])
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
