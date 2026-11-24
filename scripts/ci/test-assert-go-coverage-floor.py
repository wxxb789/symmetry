#!/usr/bin/env python3
"""The Go coverage floor gate's suite: the committed baseline, a real capture and every refusal."""

import json
import pathlib
import tempfile
import unittest

import citest

SCRIPT = pathlib.Path(__file__).with_name("assert-go-coverage-floor.py")
BASELINE = SCRIPT.with_name("go-coverage-baseline.json")
CAPTURE = SCRIPT.with_name("go-coverage-capture-fixture.txt")

CAPTURE_TOTAL = 86.2


def summary_text(total, *, trailing=""):
    return (
        "github.com/wxxb789/symmetry/daemon/internal/notification/client.go:398:\thintFromFrame\t75.0%\n"
        f"total:\t\t\t\t\t(statements)\t{total}%\n"
        f"{trailing}"
    )


def baseline_document(**overrides):
    document = json.loads(BASELINE.read_text(encoding="utf-8"))
    document.update(overrides)
    return document


class CoverageFloorGateTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = pathlib.Path(self.directory.name)

    def write(self, name, text):
        path = self.root / name
        path.write_text(text, encoding="utf-8")
        return path

    def run_gate(self, summary, baseline=None):
        return citest.run_gate(
            SCRIPT,
            "--summary",
            str(summary),
            "--baseline",
            str(BASELINE if baseline is None else baseline),
            encoding="utf-8",
        )

    def test_the_committed_baseline_is_the_runs_minimum_minus_one_point(self):
        document = json.loads(BASELINE.read_text(encoding="utf-8"))
        percents = [run["percent"] for run in document["runs"]]
        self.assertEqual(document["baseline_percent"], min(percents))
        self.assertEqual(document["floor_percent"], min(percents) - 1.0)
        self.assertGreaterEqual(len(document["runs"]), 3)
        self.assertEqual(document["metric"], "statement")
        for run in document["runs"]:
            self.assertAlmostEqual(run["percent"], round(100.0 * run["hit"] / run["total"], 4), places=4)

    def test_the_capture_fixture_parses_with_its_recorded_total(self):
        result = self.run_gate(CAPTURE)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"measured {CAPTURE_TOTAL:.2f}%", result.stdout)
        self.assertIn("the minimum of 4 recorded runs", result.stdout)

    def test_a_total_below_the_floor_is_red(self):
        document = json.loads(BASELINE.read_text(encoding="utf-8"))
        below = document["floor_percent"] - 0.01
        result = self.run_gate(self.write("summary.txt", summary_text(f"{below:.2f}")))
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"is below the committed floor {document['floor_percent']:.2f}%", result.stderr)

    def test_a_total_exactly_at_the_floor_is_green(self):
        document = json.loads(BASELINE.read_text(encoding="utf-8"))
        result = self.run_gate(self.write("summary.txt", summary_text(f"{document['floor_percent']:.3f}")))
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_a_summary_without_a_total_is_refused(self):
        result = self.run_gate(self.write("summary.txt", "client.go:1:\tRun\t50.0%\n"))
        self.assertEqual(result.returncode, 2)
        self.assertIn("must carry exactly one", result.stderr)

    def test_a_summary_with_a_duplicated_total_is_refused(self):
        doubled = summary_text("86.2") + "total:\t\t\t\t\t(statements)\t86.2%\n"
        result = self.run_gate(self.write("summary.txt", doubled))
        self.assertEqual(result.returncode, 2)
        self.assertIn("found 2", result.stderr)

    def test_a_summary_whose_total_is_not_last_is_refused(self):
        result = self.run_gate(self.write("summary.txt", summary_text("86.2", trailing="client.go:1:\tRun\t50.0%\n")))
        self.assertEqual(result.returncode, 2)
        self.assertIn("last non-empty line", result.stderr)

    def test_a_summary_total_outside_zero_to_a_hundred_is_refused(self):
        result = self.run_gate(self.write("summary.txt", summary_text("150.0")))
        self.assertEqual(result.returncode, 2)
        self.assertIn("outside 0..100", result.stderr)

    def test_a_missing_summary_is_refused(self):
        result = self.run_gate(self.root / "absent.txt")
        self.assertEqual(result.returncode, 2)
        self.assertIn("could not read the cover summary", result.stderr)

    def test_a_missing_baseline_is_refused(self):
        result = self.run_gate(CAPTURE, baseline=self.root / "absent.json")
        self.assertEqual(result.returncode, 2)
        self.assertIn("could not read the coverage baseline", result.stderr)

    def test_a_malformed_baseline_is_refused(self):
        baseline = self.write("baseline.json", "{not json")
        result = self.run_gate(CAPTURE, baseline=baseline)
        self.assertEqual(result.returncode, 2)
        self.assertIn("not valid JSON", result.stderr)

    def test_a_baseline_with_a_wrong_metric_is_refused(self):
        baseline = self.write("baseline.json", json.dumps(baseline_document(metric="line")))
        result = self.run_gate(CAPTURE, baseline=baseline)
        self.assertEqual(result.returncode, 2)
        self.assertIn('"metric": "statement"', result.stderr)

    def test_a_baseline_with_fewer_than_three_runs_is_refused(self):
        document = baseline_document()
        document["runs"] = document["runs"][:2]
        result = self.run_gate(CAPTURE, baseline=self.write("baseline.json", json.dumps(document)))
        self.assertEqual(result.returncode, 2)
        self.assertIn("at least three runs", result.stderr)

    def test_a_baseline_with_a_hand_edited_percent_is_refused(self):
        document = baseline_document()
        document["runs"][0]["percent"] = document["runs"][0]["percent"] + 0.5
        result = self.run_gate(CAPTURE, baseline=self.write("baseline.json", json.dumps(document)))
        self.assertEqual(result.returncode, 2)
        self.assertIn("pair measures", result.stderr)

    def test_a_baseline_run_that_is_not_an_object_is_refused(self):
        document = baseline_document()
        document["runs"][0] = "not an object"
        result = self.run_gate(CAPTURE, baseline=self.write("baseline.json", json.dumps(document)))
        self.assertEqual(result.returncode, 2)
        self.assertIn("not an object", result.stderr)

    def test_a_baseline_run_with_hit_above_total_is_refused(self):
        document = baseline_document()
        document["runs"][0]["hit"] = document["runs"][0]["total"] + 1
        result = self.run_gate(CAPTURE, baseline=self.write("baseline.json", json.dumps(document)))
        self.assertEqual(result.returncode, 2)
        self.assertIn("sane hit/total", result.stderr)

    def test_a_baseline_run_with_a_non_numeric_percent_is_refused(self):
        document = baseline_document()
        document["runs"][0]["percent"] = str(document["runs"][0]["percent"])
        result = self.run_gate(CAPTURE, baseline=self.write("baseline.json", json.dumps(document)))
        self.assertEqual(result.returncode, 2)
        self.assertIn("numeric percent", result.stderr)

    def test_a_baseline_without_numeric_baseline_and_floor_percents_is_refused(self):
        document = baseline_document()
        del document["baseline_percent"]
        del document["floor_percent"]
        result = self.run_gate(CAPTURE, baseline=self.write("baseline.json", json.dumps(document)))
        self.assertEqual(result.returncode, 2)
        self.assertIn("numeric baseline_percent and floor_percent", result.stderr)

    def test_a_baseline_whose_baseline_is_not_the_runs_minimum_is_refused(self):
        result = self.run_gate(
            CAPTURE, baseline=self.write("baseline.json", json.dumps(baseline_document(baseline_percent=56.664)))
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("runs' minimum", result.stderr)

    def test_a_baseline_whose_floor_drifted_from_the_arithmetic_is_refused(self):
        result = self.run_gate(
            CAPTURE, baseline=self.write("baseline.json", json.dumps(baseline_document(floor_percent=54.0)))
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("the floor must be baseline_percent", result.stderr)

    def test_mutation_raising_the_floor_without_the_baseline_reddens_the_baseline_validation(self):
        document = json.loads(BASELINE.read_text(encoding="utf-8"))
        mutated = baseline_document(floor_percent=document["floor_percent"] + 1.0)
        result = self.run_gate(CAPTURE, baseline=self.write("baseline.json", json.dumps(mutated)))
        self.assertEqual(result.returncode, 2)
        self.assertIn("the floor must be baseline_percent", result.stderr)

    def test_mutation_zeroing_a_recorded_percent_reddens_the_baseline_validation(self):
        document = baseline_document()
        document["runs"][0]["percent"] = 0
        result = self.run_gate(CAPTURE, baseline=self.write("baseline.json", json.dumps(document)))
        self.assertEqual(result.returncode, 2)
        self.assertIn("pair measures", result.stderr)


if __name__ == "__main__":
    unittest.main()
