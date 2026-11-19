#!/usr/bin/env python3
"""Unit tests for the render gate's executed-set JSON gate."""

import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("assert-playwright-json.py")

PROJECTS = ["desktop-chromium", "mobile-chromium"]
ROLLOUT_OFF = "with Goal rollout off a present Goal still serves its real attention entry"
GOAL_MANAGED = "a real Goal-managed work item presents no legacy path, and the server refuses one"
EXPIRY = "expired authentication clears private content and recovers the deep link"


def test_case(project, status="expected"):
    return {"projectName": project, "status": status, "results": [{"status": status}]}


def spec(title, tests):
    return {"title": title, "ok": True, "tests": tests}


def passing_report(*titles, skipped=1, nested=False):
    """A report where every title ran as expected in both projects."""
    suites = [
        spec(title, [test_case(project) for project in PROJECTS]) for title in titles
    ]
    if nested:
        # Case titles live under test.describe nodes, which the JSON reporter
        # nests as child suites.
        return {
            "config": {"projects": [{"name": project} for project in PROJECTS]},
            "suites": [{"title": "nested.spec.mjs", "suites": [{"title": "describe", "specs": suites}]}],
            "stats": {"expected": 6, "skipped": skipped, "unexpected": 0, "flaky": 0},
        }
    return {
        "config": {"projects": [{"name": project} for project in PROJECTS]},
        "suites": [{"title": "gate.spec.mjs", "specs": suites}],
        "stats": {"expected": 6, "skipped": skipped, "unexpected": 0, "flaky": 0},
    }


class GateTestCase(unittest.TestCase):
    def run_gate(self, report, *arguments):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "report.json")
            if isinstance(report, str):
                path.write_text(report, encoding="utf-8")
            else:
                path.write_text(json.dumps(report), encoding="utf-8")
            return subprocess.run(
                [sys.executable, str(SCRIPT), str(path), *arguments],
                capture_output=True,
                text=True,
                check=False,
            )

    def run_gate_path(self, path, *arguments):
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(path), *arguments],
            capture_output=True,
            text=True,
            check=False,
        )


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
            passing_report(EXPIRY, nested=True), "--expect-skipped", "1", EXPIRY
        )
        self.assertEqual(result.returncode, 0, result.stderr)


class SkipCountTests(GateTestCase):
    def test_more_skips_than_expected_fail(self):
        # Dropping a guard turns its case into a named skip while the runner
        # still exits 0; the count is the assertion that notices.
        result = self.run_gate(
            passing_report(ROLLOUT_OFF, skipped=3), "--expect-skipped", "1", ROLLOUT_OFF
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("stats.skipped is 3, expected 1", result.stderr)

    def test_fewer_skips_than_expected_fail(self):
        result = self.run_gate(
            passing_report(ROLLOUT_OFF, skipped=0), "--expect-skipped", "1", ROLLOUT_OFF
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("stats.skipped is 0, expected 1", result.stderr)


class GatedTitleTests(GateTestCase):
    def test_a_title_skipped_in_one_project_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["specs"][0]["tests"][1]["status"] = "skipped"
        report["stats"]["skipped"] = 1
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("mobile-chromium: {skipped:1}", result.stderr)

    def test_a_flaky_title_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["specs"][0]["tests"][0]["status"] = "flaky"
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("desktop-chromium: {flaky:1}", result.stderr)

    def test_a_title_dropped_from_the_invocation_fails(self):
        result = self.run_gate(
            passing_report(ROLLOUT_OFF), "--expect-skipped", "1", ROLLOUT_OFF, GOAL_MANAGED
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("mobile-chromium: no result", result.stderr)
        self.assertIn(GOAL_MANAGED, result.stderr)

    def test_a_title_duplicated_in_one_project_fails(self):
        report = passing_report(ROLLOUT_OFF)
        report["suites"][0]["specs"][0]["tests"].append(test_case("desktop-chromium"))
        result = self.run_gate(report, "--expect-skipped", "1", ROLLOUT_OFF)
        self.assertEqual(result.returncode, 1)
        self.assertIn("desktop-chromium: {expected:2}", result.stderr)


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
            result = self.run_gate_path(
                pathlib.Path(directory, "absent.json"), "--expect-skipped", "1", ROLLOUT_OFF
            )
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
