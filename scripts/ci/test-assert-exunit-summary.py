#!/usr/bin/env python3
"""Unit tests for the shared ExUnit summary gate."""

import importlib.util
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("assert-exunit-summary.py")
SPEC = importlib.util.spec_from_file_location("assert_exunit_summary", SCRIPT)
SUMMARY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SUMMARY)

# Filtered-run shapes measured in hosted logs (job 133696386957 for the Pi
# refusal gate, job 133697163227 for the Goal-receipt gate, job 133696387034 for
# the Windows native-positive gate).
PI_REFUSAL_LOG = (
    "Excluding tags: [:test]\n"
    "Including tags: [:pi_control_e2e_linux_refusal, {:skip, \"pi_control_e2e_linux_refusal\"}]\n"
    "\n"
    ".\n"
    "Finished in 13.3 seconds (6.0s async, 7.3s sync)\n"
    "\n"
    "Result: 1 passed, 7 excluded\n"
)
RECEIPT_LOG = (
    "Excluding tags: [:test]\n"
    "Including tags: [:goal_receipt_e2e]\n"
    "\n"
    "Result: 1 passed, 10 excluded\n"
)
WINDOWS_NATIVE_LOG = (
    "Excluding tags: [:test]\n"
    "Including tags: [:pi_control_e2e_native_positive, {:skip, \"pi_control_e2e_native_positive\"}]\n"
    "\n"
    "Result: 5 passed, 3 excluded\n"
)


def ansi(text):
    return f"\x1b[32m{text}\x1b[0m"


def parse(text):
    summary = SUMMARY.parse_summary(text)
    return (summary.tests, summary.failures, summary.skips)


class ParseSummaryTests(unittest.TestCase):
    def test_result_totals_pass(self):
        self.assertEqual(parse("Running tests...\n" + ansi("Result: 5/5 passed") + "\n"), (5, 0, 0))

    def test_ansi_wrapped_summary_is_read(self):
        text = ansi("Result: 3/3 passed") + "\n" + ansi("Randomized with seed 42") + "\n"
        self.assertEqual(parse(text), (3, 0, 0))

    def test_trailing_summary_is_tolerated(self):
        self.assertEqual(parse("Result: 1/1 passed\nRandomized with seed 0\n\nDone.\n"), (1, 0, 0))

    def test_crlf_log_is_accepted(self):
        self.assertEqual(parse("Result: 2/2 passed\r\nRandomized with seed 1\r\n"), (2, 0, 0))

    def test_tail_failure_marks_failure(self):
        with self.assertRaises(SUMMARY.SummaryInvalid) as raised:
            parse("Result: 4/5 passed, 1 failed\n")
        self.assertEqual((raised.exception.tests, raised.exception.failures), (5, 1))

    def test_reported_failure_line_wins_over_tail(self):
        with self.assertRaises(SUMMARY.SummaryInvalid) as raised:
            parse("Result: 5/5 passed\nFailed: 2 tests\n")
        self.assertEqual((raised.exception.tests, raised.exception.failures), (5, 2))

    def test_reported_zero_failures_pass(self):
        self.assertEqual(parse("Result: 5/5 passed\nFailed: 0 tests\n"), (5, 0, 0))

    def test_result_without_total_uses_passed_plus_failures(self):
        self.assertEqual(parse("Result: 4 passed\n"), (4, 0, 0))
        with self.assertRaises(SUMMARY.SummaryInvalid) as raised:
            parse("Result: 3 passed\nFailed: 1 tests\n")
        self.assertEqual((raised.exception.tests, raised.exception.failures), (4, 1))

    def test_tests_failures_summary_form(self):
        self.assertEqual(parse("10 tests, 0 failures\n"), (10, 0, 0))
        with self.assertRaises(SUMMARY.SummaryInvalid) as raised:
            parse("3 tests, 1 failure\n")
        self.assertEqual((raised.exception.tests, raised.exception.failures), (3, 1))

    def test_last_summary_wins(self):
        with self.assertRaises(SUMMARY.SummaryInvalid) as raised:
            parse("Result: 1/1 passed\nResult: 0/0 passed\n")
        self.assertEqual((raised.exception.tests, raised.exception.failures), (0, 0))

    def test_zero_tests_fails(self):
        with self.assertRaises(SUMMARY.SummaryInvalid) as result:
            parse("Result: 0/0 passed\n")
        self.assertEqual((result.exception.tests, result.exception.failures), (0, 0))
        with self.assertRaises(SUMMARY.SummaryInvalid) as summary:
            parse("0 tests, 0 failures\n")
        self.assertEqual((summary.exception.tests, summary.exception.failures), (0, 0))

    def test_missing_summary_fails(self):
        with self.assertRaises(SUMMARY.SummaryMissing):
            parse("Finished in 1.0 seconds\nRandomized with seed 123\n")

    def test_skipped_count_fails_the_gate(self):
        with self.assertRaises(SUMMARY.SummaryInvalid) as raised:
            parse("Result: 859 passed, 21 skipped\n")
        self.assertEqual((raised.exception.tests, raised.exception.failures, raised.exception.skips), (859, 0, 21))

    def test_excluded_count_is_selection_and_is_tolerated(self):
        self.assertEqual(parse("Result: 1 passed, 7 excluded\n"), (1, 0, 0))
        self.assertEqual(parse(PI_REFUSAL_LOG), (1, 0, 0))
        self.assertEqual(parse(RECEIPT_LOG), (1, 0, 0))
        self.assertEqual(parse(WINDOWS_NATIVE_LOG), (5, 0, 0))


class CheckTests(unittest.TestCase):
    def test_success_prints_verified_line_and_records_counts(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            status = SUMMARY.check(PI_REFUSAL_LOG, 0, "pi_control_e2e_linux_refusal", str(provenance))
            self.assertEqual(status, 0)
            self.assertEqual(
                provenance.read_text(encoding="utf-8"),
                "test_summary=tests=1;failures=0;skipped=0\n",
            )

    def test_runner_status_is_an_explicit_fail_closed_input(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            status = SUMMARY.check(PI_REFUSAL_LOG, 2, "pi_control_e2e_linux_refusal", str(provenance))
            self.assertEqual(status, 1)
            self.assertEqual(
                provenance.read_text(encoding="utf-8"),
                "gate_status=failed: the ExUnit runner exited with status 2\n",
            )

    def test_missing_selector_fails_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            status = SUMMARY.check(RECEIPT_LOG, 0, "pi_control_e2e_native_positive", str(provenance))
            self.assertEqual(status, 1)
            self.assertEqual(
                provenance.read_text(encoding="utf-8"),
                "gate_status=failed: the ExUnit run did not select pi_control_e2e_native_positive\n",
            )

    def test_invalid_summary_records_gate_status(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            status = SUMMARY.check("Result: 2/3 passed, 1 failed\n", 0, None, str(provenance))
            self.assertEqual(status, 1)
            self.assertEqual(
                provenance.read_text(encoding="utf-8"),
                "test_summary=tests=3;failures=1;skipped=0\n"
                "gate_status=failed: invalid ExUnit summary tests=3 failures=1 skipped=0\n",
            )

    def test_missing_summary_records_gate_status(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            status = SUMMARY.check("no summary here\n", 0, None, str(provenance))
            self.assertEqual(status, 1)
            self.assertEqual(
                provenance.read_text(encoding="utf-8"),
                "gate_status=failed: ExUnit summary was not found\n",
            )

    def test_check_without_provenance_writes_nothing(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            self.assertEqual(SUMMARY.check("Result: 1/1 passed\n", 0), 0)
            self.assertFalse(provenance.exists())


class CommandLineTests(unittest.TestCase):
    def run_gate(self, text, *arguments):
        with tempfile.TemporaryDirectory() as directory:
            log = pathlib.Path(directory, "test.log")
            log.write_text(text, encoding="utf-8")
            return subprocess.run(
                [sys.executable, str(SCRIPT), str(log), *arguments],
                capture_output=True,
                text=True,
                check=False,
            )

    def test_cli_passes_a_measured_filtered_run(self):
        result = self.run_gate(
            PI_REFUSAL_LOG, "--runner-status", "0", "--expected-selector", "pi_control_e2e_linux_refusal"
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "verified ExUnit summary: tests=1, failures=0\n")

    def test_cli_requires_the_runner_status_input(self):
        result = self.run_gate("Result: 1 passed\n")
        self.assertEqual(result.returncode, 2)
        self.assertIn("--runner-status", result.stderr)

    def test_cli_fails_on_a_non_zero_runner_status(self):
        result = self.run_gate(RECEIPT_LOG, "--runner-status", "2")
        self.assertEqual(result.returncode, 1)
        self.assertIn("the ExUnit runner exited with status 2", result.stderr)

    def test_cli_fails_on_one_failure(self):
        result = self.run_gate("Result: 6/7 passed, 1 failed\n", "--runner-status", "0")
        self.assertEqual(result.returncode, 1)
        self.assertIn("expected tests>0, failures=0 and skipped=0; got tests=7, failures=1, skipped=0", result.stderr)

    def test_cli_fails_on_zero_tests(self):
        result = self.run_gate("0 tests, 0 failures\n", "--runner-status", "0")
        self.assertEqual(result.returncode, 1)
        self.assertIn("got tests=0, failures=0, skipped=0", result.stderr)

    def test_cli_fails_on_a_skipped_test(self):
        result = self.run_gate("Result: 1 passed, 1 skipped\n", "--runner-status", "0")
        self.assertEqual(result.returncode, 1)
        self.assertIn("got tests=1, failures=0, skipped=1", result.stderr)

    def test_cli_fails_on_missing_summary(self):
        result = self.run_gate("Finished in 0.1 seconds\n", "--runner-status", "0")
        self.assertEqual(result.returncode, 1)
        self.assertIn("ExUnit summary was not found", result.stderr)

    def test_cli_fails_on_unreadable_log(self):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    str(pathlib.Path(directory, "missing.log")),
                    "--runner-status",
                    "0",
                ],
                capture_output=True,
                text=True,
                check=False,
            )
        self.assertEqual(result.returncode, 2)
        self.assertIn("could not read ExUnit output", result.stderr)

    def test_cli_records_provenance(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            result = self.run_gate(
                WINDOWS_NATIVE_LOG,
                "--runner-status",
                "0",
                "--expected-selector",
                "pi_control_e2e_native_positive",
                "--provenance",
                str(provenance),
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                provenance.read_text(encoding="utf-8"),
                "test_summary=tests=5;failures=0;skipped=0\n",
            )


if __name__ == "__main__":
    unittest.main()
