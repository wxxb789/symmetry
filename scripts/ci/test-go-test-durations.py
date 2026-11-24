#!/usr/bin/env python3
"""The duration gate's suite: synthetic `go test -json` streams, refusal rows and the baseline."""

import json
import math
import pathlib
import tempfile
import unittest

import citest

SCRIPT = pathlib.Path(__file__).with_name("go-test-durations.py")
GATE = citest.load_gate(SCRIPT, "go_test_durations")

PACKAGE = "github.com/wxxb789/symmetry/daemon/internal/notification"
APP_PACKAGE = "github.com/wxxb789/symmetry/daemon/internal/app"
STATE_PACKAGE = "github.com/wxxb789/symmetry/daemon/internal/state"

BACKPRESSURE = "TestOutputBackpressureRecoversWithOneTruncationMarker"
RAW_BUDGET = "TestRawOutputBudgetDropsWithoutFailingSemanticEvents"
STATE_UNKNOWN = "TestProviderActionCompletionRejectsOverLimitWorstCaseUnknownWithoutMutation"
STATE_JOURNAL = "TestProviderActionCapacityFailsBeforeDispatchWhenJournalByteBudgetIsExceeded"
STATE_RESERVES = "TestProviderActionCapacityReservesWorstCaseFailureCodeBeforeDispatch"

# The ten green main runs the adoption ceilings and the p95 variance rest on (2026-11-22/23), from
# docs/evidence/rf-w6-12-wall-clock-ratchets-2026-11-24.md; raising a ceiling needs a newer table.
MEASURED_RUNS = {
    "45047053979": (
        0.25,
        {BACKPRESSURE: 167.20, RAW_BUDGET: 124.96, STATE_UNKNOWN: 34.58, STATE_JOURNAL: 32.33, STATE_RESERVES: 33.78},
    ),
    "45087709518": (
        0.25,
        {BACKPRESSURE: 162.48, RAW_BUDGET: 123.65, STATE_UNKNOWN: None, STATE_JOURNAL: 32.69, STATE_RESERVES: 33.15},
    ),
    "45098932836": (
        0.21,
        {BACKPRESSURE: 135.42, RAW_BUDGET: 112.51, STATE_UNKNOWN: 31.37, STATE_JOURNAL: None, STATE_RESERVES: 31.60},
    ),
    "45101489043": (
        0.22,
        {BACKPRESSURE: 163.59, RAW_BUDGET: 124.57, STATE_UNKNOWN: 33.78, STATE_JOURNAL: None, STATE_RESERVES: 31.29},
    ),
    "45109921118": (
        0.23,
        {BACKPRESSURE: 161.41, RAW_BUDGET: 122.93, STATE_UNKNOWN: 33.04, STATE_JOURNAL: None, STATE_RESERVES: 32.74},
    ),
    "45119101867": (
        0.24,
        {BACKPRESSURE: 159.14, RAW_BUDGET: 120.36, STATE_UNKNOWN: 32.70, STATE_JOURNAL: None, STATE_RESERVES: 30.68},
    ),
    "45120528766": (
        0.24,
        {BACKPRESSURE: 163.90, RAW_BUDGET: 124.76, STATE_UNKNOWN: 33.69, STATE_JOURNAL: 31.60, STATE_RESERVES: 33.88},
    ),
    "45154249296": (
        0.17,
        {BACKPRESSURE: 125.00, RAW_BUDGET: 93.64, STATE_UNKNOWN: None, STATE_JOURNAL: None, STATE_RESERVES: None},
    ),
    "45186872699": (
        0.16,
        {BACKPRESSURE: 124.62, RAW_BUDGET: 94.06, STATE_UNKNOWN: None, STATE_JOURNAL: None, STATE_RESERVES: None},
    ),
    "45200375602": (
        0.21,
        {BACKPRESSURE: 163.79, RAW_BUDGET: 124.77, STATE_UNKNOWN: 34.42, STATE_JOURNAL: 32.91, STATE_RESERVES: 30.24},
    ),
}

ROSTER = {
    (APP_PACKAGE, BACKPRESSURE),
    (APP_PACKAGE, RAW_BUDGET),
    (STATE_PACKAGE, STATE_UNKNOWN),
    (STATE_PACKAGE, STATE_JOURNAL),
    (STATE_PACKAGE, STATE_RESERVES),
}


def package_for(test):
    if test in (BACKPRESSURE, RAW_BUDGET):
        return APP_PACKAGE
    return STATE_PACKAGE


def event(action, package, test=None, elapsed=None):
    entry = {"Action": action, "Package": package}
    if test is not None:
        entry["Test"] = test
    if elapsed is not None:
        entry["Elapsed"] = elapsed
    return json.dumps(entry)


def write_stream(messages):
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False, encoding="utf-8") as handle:
        for message in messages:
            handle.write(message + "\n")
    return pathlib.Path(handle.name)


def pass_stream(entries):
    messages = []
    packages = []
    for package, test, elapsed in entries:
        messages.append(event("run", package, test))
        messages.append(event("pass", package, test, elapsed))
        if package not in packages:
            packages.append(package)
    messages.extend(event("pass", package, elapsed=0.0) for package in packages)
    return messages


def with_fast_background(entries, count=100):
    """One slow row among many fast ones, so the p95 arm stays clear of the row's own verdict."""
    background = [(PACKAGE, f"TestFast{index}", 0.02) for index in range(count)]
    return pass_stream(background + entries)


class DurationGateTests(unittest.TestCase):
    def run_gate(self, messages):
        stream = write_stream(messages)
        self.addCleanup(stream.unlink)
        return citest.run_gate(SCRIPT, str(stream), encoding="utf-8")

    def test_a_green_stream_prints_the_tables_and_the_summary(self):
        result = self.run_gate(pass_stream([(PACKAGE, "TestOne", 0.01), (PACKAGE, "TestTwo", 0.04)]))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Top 2 slowest tests:", result.stdout)
        self.assertIn("Top 1 slowest packages:", result.stdout)
        self.assertIn("over_30s=0 adopted=0 failed=0 package_fail=0", result.stdout)
        self.assertIn("PASS: no unadopted test over the cap", result.stdout)

    def test_an_unlisted_test_over_the_cap_fails_with_the_cap_named(self):
        result = self.run_gate(with_fast_background([(PACKAGE, "TestPlanted", 40.0)]))
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"FAIL: {PACKAGE} TestPlanted took 40.00s, over the 30s cap", result.stdout)
        self.assertIn("over_30s=1 adopted=0", result.stdout)

    def test_a_rostered_test_within_its_ceiling_is_adopted_and_green(self):
        result = self.run_gate(with_fast_background([(APP_PACKAGE, BACKPRESSURE, 167.20)]))
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("adopted=1", result.stdout)
        self.assertIn(
            f"adopted: {APP_PACKAGE} {BACKPRESSURE} took 167.20s within its committed ceiling 201s", result.stdout
        )
        self.assertNotIn("FAIL:", result.stdout)

    def test_a_rostered_test_over_its_ceiling_fails_with_the_ceiling_named(self):
        result = self.run_gate(with_fast_background([(APP_PACKAGE, BACKPRESSURE, 201.5)]))
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            f"FAIL: {APP_PACKAGE} {BACKPRESSURE} took 201.50s, over its committed ceiling 201s", result.stdout
        )
        self.assertIn("adopted=0", result.stdout)
        self.assertNotIn("within its committed ceiling", result.stdout)

    def test_every_rostered_name_is_measured_and_the_ceiling_exceeds_the_measured_max(self):
        self.assertEqual(set(GATE.ADOPTED_TEST_CEILINGS), ROSTER)
        for (_package, test), ceiling in GATE.ADOPTED_TEST_CEILINGS.items():
            measured = [run[1][test] for run in MEASURED_RUNS.values() if run[1].get(test) is not None]
            self.assertTrue(measured, f"{test} carries no measured value")
            self.assertEqual(
                ceiling, float(math.ceil(max(measured) * 1.2)), f"{test}'s ceiling is not ceil(measured max x 1.2)"
            )
            self.assertGreater(ceiling, GATE.MAX_TEST_SECONDS, f"{test}'s ceiling hides the cap")

    def test_the_committed_constants_match_the_measured_table(self):
        self.assertEqual(GATE.MAX_TEST_SECONDS, 30.0)
        self.assertEqual(GATE.P95_LIMIT_FACTOR, 1.2)
        self.assertEqual(GATE.P95_BASELINE_SECONDS, 0.26)
        measured_p95 = [run[0] for run in MEASURED_RUNS.values()]
        self.assertLessEqual(max(measured_p95), GATE.P95_BASELINE_SECONDS)

    def test_every_measured_run_replays_green_through_the_gate(self):
        for run, (_p95, rows) in MEASURED_RUNS.items():
            entries = [(package_for(test), test, value) for test, value in rows.items() if value is not None]
            result = self.run_gate(with_fast_background(entries, count=200))
            self.assertEqual(result.returncode, 0, f"the gate must stay green on run {run}: {result.stdout}")

    def test_the_p95_arm_fails_above_the_committed_band(self):
        result = self.run_gate(pass_stream([(PACKAGE, f"Test{index}", 0.4) for index in range(20)]))
        self.assertEqual(result.returncode, 1)
        self.assertIn("FAIL: the per-test p95 0.400s exceeds the 1.2x limit 0.312s", result.stdout)

    def test_the_p95_arm_stays_green_at_the_committed_baseline(self):
        result = self.run_gate(pass_stream([(PACKAGE, f"Test{index}", 0.26) for index in range(20)]))
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("p95=0.260s baseline=0.260s ratio=1.00x", result.stdout)

    def test_a_stream_ending_with_a_running_test_is_refused(self):
        messages = [
            event("run", PACKAGE, "TestFails"),
            event("fail", PACKAGE, "TestFails", 0.02),
            event("run", PACKAGE, "TestHangs"),
            event("fail", PACKAGE, elapsed=600.0),
        ]
        result = self.run_gate(messages)
        self.assertEqual(result.returncode, 2)
        self.assertIn("ends with tests still running (1 in", result.stderr)
        self.assertIn(f"  {PACKAGE} TestHangs", result.stderr)
        self.assertIn("(partial:", result.stdout)

    def test_a_complete_stream_with_a_failed_test_keeps_green_and_replays_its_output(self):
        messages = [
            event("run", PACKAGE, "TestFails"),
            json.dumps({"Action": "output", "Package": PACKAGE, "Test": "TestFails", "Output": "boom\n"}),
            event("fail", PACKAGE, "TestFails", 0.02),
            event("fail", PACKAGE, elapsed=0.03),
        ]
        result = self.run_gate(messages)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("output of failed test", result.stdout)
        self.assertIn("failed=1 package_fail=1", result.stdout)

    def test_a_package_failure_no_named_test_explains_is_refused(self):
        messages = [
            event("run", PACKAGE, "TestOther"),
            event("pass", PACKAGE, "TestOther", 0.01),
            event("fail", PACKAGE, elapsed=0.02),
        ]
        result = self.run_gate(messages)
        self.assertEqual(result.returncode, 2)
        self.assertIn("package-level failures no named test explains", result.stderr)

    def test_an_unparsable_line_is_refused(self):
        messages = [*pass_stream([(PACKAGE, "TestOne", 0.01)]), "{not json"]
        result = self.run_gate(messages)
        self.assertEqual(result.returncode, 2)
        self.assertIn("unparsable line(s)", result.stderr)

    def test_a_stream_without_per_test_outcomes_is_refused(self):
        result = self.run_gate([event("pass", PACKAGE, elapsed=0.01)])
        self.assertEqual(result.returncode, 2)
        self.assertIn("no per-test pass/fail events", result.stderr)

    def test_a_missing_file_is_refused(self):
        result = citest.run_gate(
            SCRIPT, str(pathlib.Path(tempfile.gettempdir()) / "absent-go-test.json"), encoding="utf-8"
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("could not read the Go test JSON events", result.stderr)

    def test_skipped_tests_are_counted_and_excluded_from_the_distribution(self):
        messages = pass_stream([(PACKAGE, "TestOne", 0.01)])
        messages[0:0] = [event("run", PACKAGE, "TestSkipped"), event("skip", PACKAGE, "TestSkipped")]
        result = self.run_gate(messages)
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("skipped=1", result.stdout)
        self.assertIn("Top 1 slowest tests:", result.stdout)

    def test_mutation_marking_a_skip_before_the_finished_mark_reddens_the_skip_row(self):
        anchor = (
            "            finished.add(key)\n"
            '            if action == "skip":\n'
            "                skipped += 1\n"
            "                continue\n"
        )
        gate_text = SCRIPT.read_text(encoding="utf-8")
        self.assertIn(anchor, gate_text, "the mutation's anchor must be the terminal-event bookkeeping")
        mutated = gate_text.replace(
            anchor,
            '            if action == "skip":\n'
            "                skipped += 1\n"
            "                continue\n"
            "            finished.add(key)\n",
        )
        self.assertNotEqual(mutated, gate_text, "the mutation must edit the gate text")
        with tempfile.NamedTemporaryFile("w", suffix=".py", delete=False, encoding="utf-8") as handle:
            handle.write(mutated)
        mutated_script = pathlib.Path(handle.name)
        self.addCleanup(mutated_script.unlink)
        messages = pass_stream([(PACKAGE, "TestOne", 0.01)])
        messages[0:0] = [event("run", PACKAGE, "TestSkipped"), event("skip", PACKAGE, "TestSkipped")]
        stream = write_stream(messages)
        self.addCleanup(stream.unlink)
        result = citest.run_gate(mutated_script, str(stream), encoding="utf-8")
        self.assertEqual(result.returncode, 2)
        self.assertIn("ends with tests still running", result.stderr)
        self.assertIn("TestSkipped", result.stderr)


if __name__ == "__main__":
    unittest.main()
