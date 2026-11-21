#!/usr/bin/env python3
"""Unit tests for the folded-gate assertion."""

import importlib.util
import pathlib
import subprocess
import sys
import unittest

SCRIPT = pathlib.Path(__file__).with_name("assert-folded-gates.py")
SPEC = importlib.util.spec_from_file_location("assert_folded_gates", SCRIPT)
FOLDED = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FOLDED)


class ParseTests(unittest.TestCase):
    def test_name_and_outcome_split_on_the_first_equals_sign(self):
        self.assertEqual(FOLDED.parse_gate("Run Linux Pi Control E2E=success"), ("Run Linux Pi Control E2E", "success"))

    def test_missing_separator_is_rejected(self):
        with self.assertRaises(ValueError):
            FOLDED.parse_gate("Run Linux Pi Control E2E")


class CommandLineTests(unittest.TestCase):
    def run_check(self, *arguments):
        return subprocess.run(
            [sys.executable, str(SCRIPT), *arguments],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_all_success_reports_the_checked_count(self):
        result = self.run_check("--job", "control", "Gate one=success", "Gate two=success")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "verified every folded gate in control reported success: 2 checked\n")

    def test_failure_and_skip_both_fail_the_check(self):
        result = self.run_check("--job", "daemon-windows", "Gate one=failure", "Gate two=skipped", "Gate three=success")
        self.assertEqual(result.returncode, 1)
        self.assertIn("Gate one=failure", result.stderr)
        self.assertIn("Gate two=skipped", result.stderr)
        self.assertNotIn("Gate three=success", result.stderr)

    def test_malformed_specification_is_rejected(self):
        result = self.run_check("--job", "daemon", "Gate one")
        self.assertEqual(result.returncode, 2)
        self.assertIn("must be `name=outcome`", result.stderr)

    def test_at_least_one_gate_is_required(self):
        result = self.run_check("--job", "daemon")
        self.assertEqual(result.returncode, 2)
        self.assertIn("required", result.stderr)


if __name__ == "__main__":
    unittest.main()
