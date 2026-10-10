#!/usr/bin/env python3
"""Verify the shared event gate cannot turn absent or skipped checks green."""

import json
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("assert-go-test-json.py")


class GoEventGateTests(unittest.TestCase):
    def run_gate(self, events, *arguments):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "events.jsonl"
            path.write_text("".join(json.dumps(event) + "\n" for event in events), encoding="utf-8")
            return subprocess.run(
                [sys.executable, str(SCRIPT), str(path), *arguments],
                capture_output=True, text=True, check=False,
            )

    def test_gate_requires_exact_run_and_pass_without_skips(self) -> None:
        run = {"Action": "run", "Test": "TestWitness"}
        passed = {"Action": "pass", "Test": "TestWitness"}
        for name, events, want_success in (
            ("exact pass", [run, passed], True),
            ("missing test", [], False),
            ("missing run", [passed], False),
            ("missing pass", [run], False),
            ("duplicate run", [run, run, passed], False),
            ("duplicate pass", [run, passed, passed], False),
            ("package failure", [run, passed, {"Action": "fail"}], False),
            ("parent skip", [run, passed, {"Action": "skip", "Test": "TestWitness"}], False),
            ("child skip", [run, passed, {"Action": "skip", "Test": "TestWitness/child"}], False),
        ):
            with self.subTest(name=name):
                result = self.run_gate(events, "TestWitness")
                self.assertEqual(result.returncode == 0, want_success, result.stderr)

    def test_package_filter_ignores_same_named_foreign_test(self) -> None:
        events = [
            {"Action": "run", "Package": "example/selected", "Test": "TestWitness"},
            {"Action": "pass", "Package": "example/selected", "Test": "TestWitness"},
            {"Action": "run", "Package": "example/foreign", "Test": "TestWitness"},
            {"Action": "pass", "Package": "example/foreign", "Test": "TestWitness"},
        ]
        unfiltered = self.run_gate(events, "TestWitness")
        filtered = self.run_gate(events, "--package", "example/selected", "TestWitness")
        self.assertNotEqual(unfiltered.returncode, 0)
        self.assertEqual(filtered.returncode, 0, filtered.stderr)

    def test_package_filter_rejects_foreign_only_test(self) -> None:
        events = [
            {"Action": "run", "Package": "example/foreign", "Test": "TestWitness"},
            {"Action": "pass", "Package": "example/foreign", "Test": "TestWitness"},
        ]
        result = self.run_gate(events, "--package", "example/selected", "TestWitness")
        self.assertNotEqual(result.returncode, 0)

    def test_package_filter_rejects_selected_child_skip(self) -> None:
        events = [
            {"Action": "run", "Package": "example/selected", "Test": "TestWitness"},
            {"Action": "pass", "Package": "example/selected", "Test": "TestWitness"},
            {"Action": "skip", "Package": "example/selected", "Test": "TestWitness/child"},
        ]
        result = self.run_gate(events, "--package", "example/selected", "TestWitness")
        self.assertNotEqual(result.returncode, 0)

    def test_package_filter_keeps_foreign_failures_fatal(self) -> None:
        events = [
            {"Action": "run", "Package": "example/selected", "Test": "TestWitness"},
            {"Action": "pass", "Package": "example/selected", "Test": "TestWitness"},
            {"Action": "fail", "Package": "example/foreign", "Test": "TestOther"},
        ]
        result = self.run_gate(events, "--package", "example/selected", "TestWitness")
        self.assertNotEqual(result.returncode, 0)

    def test_malformed_output_cannot_accompany_a_pass(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "events.jsonl"
            path.write_text(
                '{"Action":"run","Test":"TestWitness"}\n'
                'not a Go event\n'
                '{"Action":"pass","Test":"TestWitness"}\n', encoding="utf-8",
            )
            result = subprocess.run(
                [sys.executable, str(SCRIPT), str(path), "TestWitness"],
                capture_output=True, text=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
