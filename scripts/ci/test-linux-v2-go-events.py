#!/usr/bin/env python3
"""Verify the shared event gate cannot turn absent or skipped checks green."""

import json
import pathlib
import tempfile
import unittest

import citest

SCRIPT = pathlib.Path(__file__).with_name("assert-go-test-json.py")


class GoEventGateTests(unittest.TestCase):
    def run_gate(self, events, *arguments):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "events.jsonl"
            path.write_text("".join(json.dumps(event) + "\n" for event in events), encoding="utf-8")
            return citest.run_gate(SCRIPT, str(path), *arguments)

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
                '{"Action":"run","Test":"TestWitness"}\nnot a Go event\n{"Action":"pass","Test":"TestWitness"}\n',
                encoding="utf-8",
            )
            result = citest.run_gate(SCRIPT, str(path), "TestWitness")
        self.assertNotEqual(result.returncode, 0)

    def test_credential_gated_mode_requires_the_exact_unproven_counts(self) -> None:
        package = "example/selected"
        run = {"Action": "run", "Package": package, "Test": "TestWitness"}
        fail = {**run, "Action": "fail"}
        child = {**run, "Test": "TestWitness/child"}
        package_fail = {"Action": "fail", "Package": package}
        for name, events, want in (
            ("expected failure", [run, fail, package_fail], 0),
            ("subtest failure", [run, child, {**child, "Action": "fail"}, fail, package_fail], 0),
            ("missing parent fail", [run, child, {**child, "Action": "fail"}, package_fail], 1),
            ("missing package terminal", [run, fail], 1),
            ("contradictory package pass", [run, fail, {**package_fail, "Action": "pass"}, package_fail], 1),
            ("duplicate package fail", [run, fail, package_fail, package_fail], 1),
            ("duplicate parent fail", [run, fail, fail, package_fail], 1),
            ("pass", [run, {**run, "Action": "pass"}, fail, package_fail], 1),
            ("missing run", [fail, package_fail], 1),
            ("skip", [run, {**child, "Action": "skip"}, fail, package_fail], 1),
            ("foreign named failure", [run, {**fail, "Test": "TestOther"}, fail, package_fail], 1),
            ("foreign package terminal", [run, fail, {**package_fail, "Package": "example/foreign"}], 1),
            ("foreign child fail", [run, {**child, "Action": "fail", "Package": "example/foreign"}, fail, package_fail], 1),
            ("foreign run", [run, {**run, "Package": "example/foreign"}, fail, package_fail], 1),
            ("unattributed parent fail", [run, {"Action": "fail", "Test": "TestWitness"}, fail, package_fail], 1),
        ):
            with self.subTest(name=name):
                result = self.run_gate(events, "--package", package, "--credential-gated", "TestWitness", "TestWitness")
                self.assertEqual(result.returncode, want, result.stderr)
                if want == 0:
                    self.assertIn("credential-gated TestWitness: run=1 pass=0 skip=0", result.stdout)
                else:
                    self.assertNotIn("credential-gated", result.stdout)

    def test_credential_gated_mode_requires_a_package(self) -> None:
        events = [
            {"Action": "run", "Package": "example/selected", "Test": "TestWitness"},
            {"Action": "fail", "Package": "example/selected", "Test": "TestWitness"},
            {"Action": "fail", "Package": "example/selected"},
        ]
        result = self.run_gate(events, "--credential-gated", "TestWitness", "TestWitness")
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("requires --package", result.stderr)

    def test_credential_gated_mode_never_relaxes_the_default_gate(self) -> None:
        events = [{"Action": "run", "Test": "TestWitness"}, {"Action": "fail", "Test": "TestWitness"}]
        result = self.run_gate(events, "TestWitness")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("credential-gated", result.stdout)

    def test_credential_gated_mode_refuses_unnamed_targets(self) -> None:
        events = [{"Action": "run", "Test": "TestWitness"}, {"Action": "fail", "Test": "TestWitness"}]
        result = self.run_gate(events, "--credential-gated", "TestOther", "TestWitness")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must be expected test names", result.stderr)


if __name__ == "__main__":
    unittest.main()
