#!/usr/bin/env python3
"""Unit tests for the shared ExUnit JSON event gate."""

import importlib.util
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("assert-exunit-json.py")
SPEC = importlib.util.spec_from_file_location("assert_exunit_json", SCRIPT)
GATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GATE)

NATIVE_POSITIVE = "real Pi committed candidate is independently validated and achieves the Goal"
NATIVE_OTHER = "real Pi repository work crosses Control receipts without accepting the Goal"


def test_finished(name, state="passed", module="SymmetryControl.ExampleTest", duration_us=1234):
    return json.dumps(
        {
            "event": "test_finished",
            "module": module,
            "name": name,
            "state": state,
            "duration_us": duration_us,
        }
    )


def suite_started(include=("goal_receipt_e2e",), exclude=("test",), seed=0):
    return json.dumps({"event": "suite_started", "include": list(include), "exclude": list(exclude), "seed": seed})


SUITE_FINISHED = json.dumps(
    {
        "event": "suite_finished",
        "run_us": 1000,
        "async_us": None,
        "load_us": 10,
        "counts": {"total": 2, "passed": 1, "failed": 0, "skipped": 0, "excluded": 1, "invalid": 0},
    }
)
EXCLUDED_SIBLING = test_finished("an unselected sibling", state="excluded")


class GateTestCase(unittest.TestCase):
    def run_gate(self, stream, *arguments):
        with tempfile.TemporaryDirectory() as directory:
            events = pathlib.Path(directory, "events.json")
            events.write_text(stream, encoding="utf-8")
            return self.run_gate_file(events, *arguments)

    def run_gate_file(self, events, *arguments):
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(events), *arguments],
            capture_output=True,
            text=True,
            check=False,
        )

    def run_stream(self, stream, *arguments):
        result = self.run_gate(stream, "--runner-status", "0", *arguments)
        return result

    def passing_stream(self, *extra):
        return (
            "\n".join([suite_started(), test_finished(NATIVE_POSITIVE), EXCLUDED_SIBLING, SUITE_FINISHED, *extra])
            + "\n"
        )


class PositiveTests(GateTestCase):
    def test_named_test_passing_once_is_verified(self):
        result = self.run_stream(self.passing_stream(), NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"verified {NATIVE_POSITIVE}: state=passed", result.stdout)
        self.assertIn("1 of them ran", result.stdout)

    def test_unselected_excluded_sibling_does_not_fail_the_gate(self):
        stream = (
            "\n".join(
                [
                    suite_started(),
                    test_finished(NATIVE_POSITIVE),
                    test_finished("excluded one", state="excluded"),
                    test_finished("excluded two", state="excluded"),
                    SUITE_FINISHED,
                ]
            )
            + "\n"
        )
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_selected_tag_passes_the_selector_check(self):
        stream = (
            "\n".join(
                [
                    suite_started(include=("pi_control_e2e_native_positive", "skip:pi_control_e2e_native_positive")),
                    test_finished(NATIVE_POSITIVE),
                    EXCLUDED_SIBLING,
                    SUITE_FINISHED,
                ]
            )
            + "\n"
        )
        result = self.run_stream(stream, "--expected-selector", "pi_control_e2e_native_positive", NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("verified", result.stdout)


class GatedOutcomeTests(GateTestCase):
    def test_missing_name_fails(self):
        stream = "\n".join([suite_started(), test_finished(NATIVE_OTHER), SUITE_FINISHED]) + "\n"
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("gated tests without exactly one passed outcome", result.stderr)
        self.assertIn(NATIVE_POSITIVE, result.stderr)

    def test_skipped_name_fails(self):
        stream = self.passing_stream().replace('"state": "passed"', '"state": "skipped"')
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("skipped:1", result.stderr)

    def test_excluded_name_fails(self):
        stream = self.passing_stream().replace('"state": "passed"', '"state": "excluded"')
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("excluded:1", result.stderr)

    def test_failed_name_fails(self):
        stream = self.passing_stream().replace('"state": "passed"', '"state": "failed"')
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("failed:1", result.stderr)

    def test_unknown_state_fails(self):
        stream = self.passing_stream().replace('"state": "passed"', '"state": "invalid"')
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("other:1", result.stderr)

    def test_duplicate_passed_name_fails(self):
        stream = self.passing_stream(test_finished(NATIVE_POSITIVE))
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("passed:2", result.stderr)

    def test_passed_run_mixed_with_one_excluded_outcome_fails(self):
        stream = self.passing_stream(test_finished(NATIVE_POSITIVE, state="excluded"))
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("passed:1", result.stderr)
        self.assertIn("excluded:1", result.stderr)


class StreamIntegrityTests(GateTestCase):
    def test_empty_stream_fails(self):
        result = self.run_stream("", NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("the ExUnit event stream was empty", result.stderr)

    def test_blank_lines_only_stream_fails(self):
        result = self.run_stream("\n\n  \n", NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("the ExUnit event stream was empty", result.stderr)

    def test_malformed_line_fails(self):
        stream = self.passing_stream("not json at all")
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("invalid_json_lines=1", result.stderr)

    def test_non_object_json_line_fails(self):
        stream = self.passing_stream('"passed"')
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("invalid_json_lines=1", result.stderr)

    def test_test_finished_without_a_name_is_invalid(self):
        stream = self.passing_stream(json.dumps({"event": "test_finished", "state": "passed"}))
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("invalid_json_lines=1", result.stderr)

    def test_missing_suite_finished_line_fails(self):
        stream = "\n".join([suite_started(), test_finished(NATIVE_POSITIVE)]) + "\n"
        result = self.run_stream(stream, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("no suite_finished line", result.stderr)

    def test_unreadable_stream_exits_two(self):
        missing = pathlib.Path(tempfile.gettempdir(), "missing-exunit-events.json")
        result = self.run_gate_file(missing, "--runner-status", "0", NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 2)
        self.assertIn("could not read the ExUnit JSON event stream", result.stderr)

    def test_duplicate_expected_names_exit_two(self):
        result = self.run_stream(self.passing_stream(), NATIVE_POSITIVE, NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 2)
        self.assertIn("expected test names must be unique", result.stderr)


class SelectorAndRunnerStatusTests(GateTestCase):
    def test_selector_that_no_tag_matched_fails(self):
        result = self.run_stream(self.passing_stream(), "--expected-selector", "renamed_tag", NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("the ExUnit run did not select renamed_tag", result.stderr)
        self.assertEqual(result.stdout, "")

    def test_selector_is_matched_against_parsed_filters_only(self):
        """A tag that only appears in test names or free text is not a selected filter."""
        stream = (
            "\n".join(
                [
                    suite_started(include=("goal_receipt_e2e",)),
                    test_finished("pi_control_e2e_native_positive"),
                    SUITE_FINISHED,
                ]
            )
            + "\n"
        )
        result = self.run_stream(
            stream,
            "--expected-selector",
            "pi_control_e2e_native_positive",
            "pi_control_e2e_native_positive",
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("the ExUnit run did not select pi_control_e2e_native_positive", result.stderr)

    def test_nonzero_runner_status_fails_before_the_stream_is_judged(self):
        result = self.run_gate(self.passing_stream(), "--runner-status", "2", NATIVE_POSITIVE)
        self.assertEqual(result.returncode, 1)
        self.assertIn("the ExUnit runner exited with status 2", result.stderr)


class ProvenanceTests(GateTestCase):
    def test_success_records_the_summary_line_the_windows_step_reads(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            stream = (
                "\n".join([suite_started(), test_finished(NATIVE_POSITIVE), EXCLUDED_SIBLING, SUITE_FINISHED]) + "\n"
            )
            result = self.run_stream(stream, "--provenance", str(provenance), NATIVE_POSITIVE)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                provenance.read_text(encoding="utf-8"),
                "test_summary=tests=1;failures=0;skipped=0\n",
            )

    def test_failure_records_a_gate_status_line(self):
        with tempfile.TemporaryDirectory() as directory:
            provenance = pathlib.Path(directory, "provenance.txt")
            result = self.run_stream(
                self.passing_stream().replace('"state": "passed"', '"state": "skipped"'),
                "--provenance",
                str(provenance),
                NATIVE_POSITIVE,
            )
            self.assertEqual(result.returncode, 1)
            self.assertEqual(
                provenance.read_text(encoding="utf-8"),
                "gate_status=failed: gated tests without exactly one passed outcome: "
                f"{NATIVE_POSITIVE}={{passed:0, skipped:1, excluded:0, failed:0, other:0}}\n",
            )


class EventStreamUnitTests(unittest.TestCase):
    def test_filters_are_recorded_from_the_suite_started_line(self):
        stream = GATE.EventStream(["name"])
        stream.feed(suite_started(include=("pi_control_e2e_native_positive", "skip:pi_control_e2e_native_positive")))
        self.assertEqual(
            stream.selectors,
            ["pi_control_e2e_native_positive", "skip:pi_control_e2e_native_positive"],
        )

    def test_include_that_is_not_a_list_is_invalid(self):
        stream = GATE.EventStream(["name"])
        stream.feed(json.dumps({"event": "suite_started", "include": "goal_receipt_e2e"}))
        self.assertEqual(stream.invalid, 1)

    def test_ran_counts_passed_and_failed_but_not_excluded(self):
        stream = GATE.EventStream(["name"])
        for state in ("passed", "failed", "excluded", "skipped"):
            stream.feed(test_finished("sibling", state=state))
        self.assertEqual((stream.tests, stream.ran, stream.failures, stream.skips), (4, 2, 1, 1))


if __name__ == "__main__":
    unittest.main()
