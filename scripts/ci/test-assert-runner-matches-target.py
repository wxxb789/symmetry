#!/usr/bin/env python3
"""Unit tests for the pinned-release target/runner guard."""

import importlib.util
import os
import pathlib
import platform
import subprocess
import sys
import unittest


SCRIPT = pathlib.Path(__file__).with_name("assert-runner-matches-target.py")
SPEC = importlib.util.spec_from_file_location("assert_runner_matches_target", SCRIPT)
GUARD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GUARD)

LINUX_TARGET = "x86_64-pc-linux-gnu"
WINDOWS_TARGET = "x86_64-pc-windows"


def run_guard(target, *arguments, env=None):
    environment = dict(os.environ)
    environment.pop("PI_RUNNER_SYSTEM", None)
    environment.pop("PI_RUNNER_MACHINE", None)
    if env:
        environment.update(env)
    return subprocess.run(
        [sys.executable, str(SCRIPT), "--target", target, *arguments],
        capture_output=True,
        text=True,
        check=False,
        env=environment,
    )


class ParseTests(unittest.TestCase):
    def test_linux_triple_yields_architecture_and_system(self):
        self.assertEqual(GUARD.parse_target(LINUX_TARGET), ("x86_64", "linux"))

    def test_windows_triple_yields_architecture_and_system(self):
        self.assertEqual(GUARD.parse_target(WINDOWS_TARGET), ("x86_64", "windows"))

    def test_environment_and_vendor_tokens_are_ignored(self):
        self.assertEqual(GUARD.parse_target("x86_64-unknown-linux-musl"), ("x86_64", "linux"))
        self.assertEqual(GUARD.parse_target("x86_64-apple-darwin"), ("x86_64", "darwin"))

    def test_architecture_aliases_are_normalised(self):
        for spelling in ("x86_64", "amd64", "AMD64", "x64"):
            self.assertEqual(GUARD.normalise_architecture(spelling), "x86_64")
        self.assertEqual(GUARD.normalise_architecture("aarch64"), "aarch64")
        self.assertEqual(GUARD.normalise_architecture("arm64"), "aarch64")

    def test_system_aliases_are_normalised(self):
        self.assertEqual(GUARD.normalise_system("Linux"), "linux")
        self.assertEqual(GUARD.normalise_system("windows"), "windows")
        self.assertEqual(GUARD.normalise_system("MINGW64_NT-10.0"), "windows")

    def test_malformed_triples_are_rejected(self):
        for triple in ("x86_64-linux", "x86_64-pc-gnu", "foo-pc-linux", "x86_64-pc-linux-windows", "x86_64-pc-"):
            with self.subTest(triple=triple):
                with self.assertRaises(GUARD.TargetError):
                    GUARD.parse_target(triple)


class CommandLineTests(unittest.TestCase):
    def test_linux_runner_accepts_the_linux_triple(self):
        result = run_guard(LINUX_TARGET, "--system", "Linux", "--machine", "x86_64")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("verified runner matches pinned Pi target x86_64-pc-linux-gnu", result.stdout)

    def test_environment_variables_inject_the_uname_values(self):
        result = run_guard(
            LINUX_TARGET, env={"PI_RUNNER_SYSTEM": "Linux", "PI_RUNNER_MACHINE": "x86_64"}
        )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_windows_runner_accepts_the_windows_triple(self):
        result = run_guard(WINDOWS_TARGET, "--system", "Windows", "--machine", "AMD64")
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_wrong_architecture_fails_closed_with_both_values(self):
        result = run_guard(LINUX_TARGET, "--system", "Linux", "--machine", "aarch64")
        self.assertEqual(result.returncode, 1)
        self.assertIn("requires architecture=x86_64 system=linux", result.stderr)
        self.assertIn("machine 'aarch64' != 'x86_64'", result.stderr)
        self.assertIn("uname -s='Linux' uname -m='aarch64'", result.stderr)

    def test_wrong_system_fails_closed_with_both_values(self):
        result = run_guard(LINUX_TARGET, "--system", "Windows", "--machine", "x86_64")
        self.assertEqual(result.returncode, 1)
        self.assertIn("system 'Windows' != 'linux'", result.stderr)
        self.assertIn("uname -s='Windows' uname -m='x86_64'", result.stderr)

    def test_malformed_triple_is_a_usage_error(self):
        result = run_guard("x86_64-pc-", "--system", "Linux", "--machine", "x86_64")
        self.assertEqual(result.returncode, 2)
        self.assertIn("malformed pinned Pi target", result.stderr)

    def test_unknown_runner_machine_fails_closed(self):
        result = run_guard(LINUX_TARGET, "--system", "Linux", "--machine", "riscv64")
        self.assertEqual(result.returncode, 1)
        self.assertIn("machine 'riscv64' is not one this guard recognises", result.stderr)

    def test_defaults_report_the_real_host_values(self):
        result = run_guard("aarch64-pc-darwin")
        self.assertIn(f"uname -s={platform.system()!r}", result.stdout + result.stderr)
        self.assertIn(f"uname -m={platform.machine()!r}", result.stdout + result.stderr)
        expected = 0 if (platform.system() == "Darwin" and platform.machine() == "arm64") else 1
        self.assertEqual(result.returncode, expected)


if __name__ == "__main__":
    unittest.main()
