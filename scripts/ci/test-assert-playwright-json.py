#!/usr/bin/env python3
"""Unit tests for the render gate's executed-set JSON gate."""

import json
import pathlib
import tempfile
import unittest

import citest

SCRIPT = pathlib.Path(__file__).with_name("assert-playwright-json.py")

PROJECTS = ["desktop-chromium", "mobile-chromium"]
SPEC_FILE = "gate.spec.mjs"
NESTED_FILE = "nested.spec.mjs"
ROLLOUT_OFF = "with Goal rollout off a present Goal still serves its real attention entry"
GOAL_MANAGED = "a real Goal-managed work item presents no legacy path, and the server refuses one"
EXPIRY = "expired authentication clears private content and recovers the deep link"


def test_case(project, status="expected", expected_status="passed", results=("passed",)):
    """One test entry as the installed reporter serializes it."""
    return {
        "projectName": project,
        "status": status,
        "expectedStatus": expected_status,
        "results": [{"status": result} for result in results],
    }


def spec(title, tests):
    return {"title": title, "ok": True, "tests": tests}


def passing_report(*titles, skipped=1, nested=False):
    """A report where every title ran as expected in both projects."""
    suites = [spec(title, [test_case(project) for project in PROJECTS]) for title in titles]
    if nested:
        # Case titles live under test.describe nodes, which the JSON reporter
        # nests as child suites.
        return {
            "config": {"projects": [{"name": project} for project in PROJECTS]},
            "suites": [
                {
                    "title": NESTED_FILE,
                    "file": NESTED_FILE,
                    "suites": [{"title": "describe", "specs": suites}],
                }
            ],
            "stats": {"expected": 6, "skipped": skipped, "unexpected": 0, "flaky": 0},
        }
    return {
        "config": {"projects": [{"name": project} for project in PROJECTS]},
        "suites": [{"title": SPEC_FILE, "file": SPEC_FILE, "specs": suites}],
        "stats": {"expected": 6, "skipped": skipped, "unexpected": 0, "flaky": 0},
    }


class GateTestCase(unittest.TestCase):
    def gate_arguments(self, path, files):
        arguments = [str(path)]
        for project in PROJECTS:
            arguments += ["--expect-project", project]
        for file in files:
            arguments += ["--expect-file", file]
        return arguments

    def run_gate(self, report, *arguments, files=(SPEC_FILE,)):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "report.json")
            if isinstance(report, str):
                path.write_text(report, encoding="utf-8")
            else:
                path.write_text(json.dumps(report), encoding="utf-8")
            return citest.run_gate(SCRIPT, *self.gate_arguments(path, files), *arguments)

    def run_gate_path(self, path, *arguments):
        return citest.run_gate(SCRIPT, *self.gate_arguments(path, (SPEC_FILE,)), *arguments)


class PositiveTests(GateTestCase):
    def test_every_title_expected_in_both_projects_is_verified(self):
        result = self.run_gate(
            passing_report(ROLLOUT_OFF, GOAL_MANAGED, EXPIRY),
            "--expect-skipped",
            "1",
            ROLLOUT_OFF,
            GOAL_MANAGED,
            EXPIRY,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("1 skipped", result.stdout)
        self.assertIn(ROLLOUT_OFF, result.stdout)

    def test_nested_describe_suites_are_searched(self):
        result = self.run_gate(
            passing_report(EXPIRY, nested=True),
            "--expect-skipped",
            "1",
            EXPIRY,
            files=(NESTED_FILE,),
        )
        self.assertEqual(result.returncode, 0, result.stderr)


class SkipCountTests(GateTestCase):
    def test_more_skips_than_expected_fail(self):
        # Dropping a guard turns its case into a named skip while the runner
        # still exits 0; the count is the assertion that notices.
        result = self.run_gate(passing_report(ROLLOUT_OFF, skipped=3), "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("stats.skipped is 3, expected 1", result.stderr)

    def test_fewer_skips_than_expected_fail(self):
        result = self.run_gate(passing_report(ROLLOUT_OFF, skipped=0), "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("stats.skipped is 0, expected 1", result.stderr)


class GatedTitleTests(GateTestCase):
    def test_a_title_skipped_in_one_project_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["specs"][0]["tests"][1] = test_case(
            "mobile-chromium", status="skipped", expected_status="skipped", results=("skipped",)
        )
        report["stats"]["skipped"] = 1
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("mobile-chromium: {skipped:1}", result.stderr)

    def test_a_flaky_title_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["specs"][0]["tests"][0] = test_case(
            "desktop-chromium", status="flaky", results=("failed", "passed")
        )
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("desktop-chromium: {flaky:1}", result.stderr)

    def test_an_expected_failure_is_not_a_pass(self):
        # `test.fail()`: the installed reporter serializes the case as status
        # `expected` with expectedStatus `failed` and a failed result.
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["specs"][0]["tests"][0] = test_case(
            "desktop-chromium", expected_status="failed", results=("failed",)
        )
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("desktop-chromium: {expected-failed:1}", result.stderr)

    def test_a_case_without_a_passed_result_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["specs"][0]["tests"][0]["results"] = [{"status": "failed"}]
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("desktop-chromium: {expected without passed results:1}", result.stderr)

    def test_a_title_dropped_from_the_invocation_fails(self):
        result = self.run_gate(passing_report(ROLLOUT_OFF), "--expect-skipped", "1", ROLLOUT_OFF, GOAL_MANAGED)
        self.assertEqual(result.returncode, 1)
        self.assertIn("mobile-chromium: no result", result.stderr)
        self.assertIn(GOAL_MANAGED, result.stderr)

    def test_a_title_duplicated_in_one_project_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["specs"][0]["tests"].append(test_case("desktop-chromium"))
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("desktop-chromium: {expected:2}", result.stderr)


class CoverageTests(GateTestCase):
    def test_a_report_missing_a_required_project_fails(self):
        # The required projects come from the CLI: a report whose config
        # declares one project must not satisfy a gate asked for both widths.
        report = passing_report(ROLLOUT_OFF, skipped=0)
        report["config"]["projects"] = [{"name": "mobile-chromium"}]
        report["suites"][0]["specs"][0]["tests"] = [test_case("mobile-chromium")]
        result = self.run_gate(report, "--expect-skipped", "0", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("desktop-chromium: no result", result.stderr)

    def test_a_report_of_other_files_than_the_invocation_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["file"] = "other.spec.mjs"
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("missing gate.spec.mjs", result.stderr)
        self.assertIn("unexpected other.spec.mjs", result.stderr)


class ReportIntegrityTests(GateTestCase):
    def test_a_report_without_project_names_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["config"] = {"projects": []}
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("the report declares no project names", result.stderr)

    def test_a_report_without_a_skip_count_fails(self):
        report = passing_report(ROLLOUT_OFF)
        del report["stats"]["skipped"]
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("no integer stats.skipped", result.stderr)

    def test_invalid_json_fails(self):
        result = self.run_gate("{not json", "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("cannot read the Playwright report", result.stderr)

    def test_a_missing_report_file_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_gate_path(pathlib.Path(directory, "absent.json"), "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("cannot read the Playwright report", result.stderr)

    def test_every_problem_is_reported_at_once(self):
        report = passing_report(ROLLOUT_OFF, skipped=5)
        report["suites"][0]["specs"][0]["tests"][0]["status"] = "skipped"
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF, GOAL_MANAGED)
        self.assertEqual(result.returncode, 1)
        self.assertIn("stats.skipped is 5, expected 1", result.stderr)
        self.assertIn("desktop-chromium: {skipped:1}", result.stderr)
        self.assertIn(GOAL_MANAGED, result.stderr)


if __name__ == "__main__":
    unittest.main()
