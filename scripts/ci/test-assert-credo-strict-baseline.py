#!/usr/bin/env python3

import json
import pathlib
import tempfile
import unittest

import citest

SCRIPT = pathlib.Path(__file__).with_name("assert-credo-strict-baseline.py")
BASELINE = SCRIPT.with_name("credo-strict-baseline.json")
GATE = citest.load_gate(SCRIPT, "assert_credo_strict_baseline")

CRY_FOR_HELP = "Please report incorrect results: https://github.com/rrrene/credo/issues"
HINT = "Use `mix credo explain` to explain issues, `mix credo --help` for options."
PREAMBLE = ("==> styler", "Compiling 17 files (.ex)", "Generated styler app")
SKIPPED_FILES = (
    "info: Some source files were not parsed in the time allotted:",
    "  1) lib/symmetry_control/slow.ex",
)
CATEGORY_ORDER = ("design", "readability", "refactor", "warning", "consistency")


def small_baseline(**overrides):
    document = {
        "revision": "0" * 40,
        "recorded": "2026-11-23",
        "issues": 3,
        "files_analyzed": 3,
        "checks_run": 2,
        "by_category": {"design": 2, "warning": 1},
    }
    document.update(overrides)
    return document


def render_log(
    document,
    *,
    rendered=None,
    by_category=None,
    files=None,
    checks=None,
    preamble=PREAMBLE,
    inside_section=(),
    after_report=(),
    summary=None,
    analysis=None,
    banner=None,
    stop_after_banner=False,
    newline="\n",
):
    """Render a Credo 1.7.19 `--strict` report; `rendered` is the sections, `by_category` the summary."""
    sections = document["by_category"] if rendered is None else rendered
    counts = sections if by_category is None else by_category
    files = document["files_analyzed"] if files is None else files
    checks = document["checks_run"] if checks is None else checks
    if banner is None:
        if files == 0:
            banner = "No files found!"
        else:
            banner = f"Checking {files} source file{'' if files == 1 else 's'} (this might take a while) ..."
    lines = [*preamble, banner, ""]
    if stop_after_banner:
        return newline.join(lines)
    order = list(sections) if rendered is not None else CATEGORY_ORDER
    for category in order:
        count = sections.get(category, 0)
        if count == 0:
            continue
        lines.append(f"  {GATE.CATEGORY_TITLE[category]}".ljust(79))
        lines.append("┃ ")
        for number in range(count):
            lines.append(f"┃ [{GATE.CATEGORY_TAG[category]}] → message {number} of {count}")
            lines.append(f"┃       lib/symmetry_control/sample.ex:1{number}:3 #(Sample.run)")
        lines.extend(inside_section)
        lines.append("")
    if analysis is None:
        analysis = f"Analysis took 16.2 seconds (1.2s to load, 15.0s running {checks} checks on {files} files)"
    if summary is None:
        parts = ", ".join(
            f"{counts[category]} {GATE.CATEGORY_WORDING[category][1]}"
            for category in GATE.SUMMARY_ORDER
            if category in counts
        )
        summary = f"4527 mods/funs, found {parts}."
    lines.extend([CRY_FOR_HELP, "", analysis, summary, "", HINT, "", *after_report])
    return newline.join(lines)


class GateCase(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="symmetry-credo-baseline-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = pathlib.Path(self.temporary.name)
        self.document = small_baseline()
        self.baseline = self.write_baseline(self.document)

    def write_baseline(self, document, name="credo-strict-baseline.json"):
        path = self.directory / name
        path.write_text(json.dumps(document), encoding="utf-8")
        return path

    def write_log(self, log, name="credo-strict.log"):
        path = self.directory / name
        path.write_text(log, encoding="utf-8", newline="")
        return path

    def assert_green(self, log, baseline=None):
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(log)), "--baseline", str(baseline or self.baseline)
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout

    def assert_red(self, log, *fragments, baseline=None, code=1):
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(log)), "--baseline", str(baseline or self.baseline)
        )
        self.assertEqual(result.returncode, code, result.stdout + result.stderr)
        for fragment in fragments:
            self.assertIn(fragment, result.stderr)
        return result.stderr


class ReportTests(GateCase):
    def test_a_report_matching_the_baseline_verifies_with_its_counts(self):
        stdout = self.assert_green(render_log(self.document))
        self.assertIn("credo_strict_issue_total=3\n", stdout)
        self.assertIn("credo_strict_files_analyzed=3\n", stdout)
        self.assertIn("credo_strict_checks_run=2\n", stdout)
        self.assertIn("credo_strict_design=2\n", stdout)
        self.assertIn("credo_strict_warning=1\n", stdout)
        self.assertIn(
            "verified the Credo strict ratchet: 3 issue(s) (1 warnings, 2 software design suggestions)", stdout
        )

    def test_the_shipped_baseline_is_reproduced_by_its_own_report(self):
        document = json.loads(BASELINE.read_text(encoding="utf-8"))
        stdout = self.assert_green(render_log(document), baseline=BASELINE)
        self.assertIn(f"credo_strict_issue_total={document['issues']}\n", stdout)
        self.assertIn(f"at the baseline recorded from {document['revision']}\n", stdout)

    def test_a_carriage_return_log_is_normalised(self):
        self.assert_green(render_log(self.document, newline="\r\n"))

    def test_a_rise_in_one_category_refuses_the_run(self):
        self.assert_red(
            render_log(self.document, rendered={"design": 3, "warning": 1}),
            "Software Design: 3 issue(s) against the baseline's 2",
            "the strict count rose",
        )

    def test_a_fall_in_one_category_refuses_as_a_stale_baseline(self):
        self.assert_red(
            render_log(self.document, rendered={"design": 1, "warning": 1}),
            "Software Design: 1 issue(s) against the baseline's 2",
            f"record the new counts in {self.baseline}",
        )

    def test_a_category_the_baseline_did_not_record_refuses(self):
        self.assert_red(
            render_log(self.document, rendered={"design": 2, "readability": 1, "warning": 1}),
            "Code Readability: 1 issue(s) against the baseline's 0",
        )

    def test_fewer_analyzed_files_than_the_baseline_refuses(self):
        self.assert_red(render_log(self.document, files=2), "analyzed 2 source file(s), below the baseline's 3")

    def test_fewer_checks_than_the_baseline_refuses(self):
        self.assert_red(render_log(self.document, checks=1), "ran 1 check(s), below the baseline's 2")


class ShapeTests(GateCase):
    def test_a_skipped_source_file_warning_refuses_before_anything_else(self):
        log = render_log(self.document, preamble=(*PREAMBLE, *SKIPPED_FILES))
        self.assert_red(log, "Credo skipped source file(s), so the analyzed set is incomplete")

    def test_an_empty_analyzed_set_refuses(self):
        self.assert_red(
            render_log(self.document, files=0, summary="0 mods/funs, found no issues."), "analyzed no source file(s)"
        )

    def test_a_missing_banner_refuses_as_credo_not_running(self):
        self.assert_red(render_log(self.document, preamble=(), banner=""), "Credo did not run to completion")

    def test_a_truncated_report_refuses(self):
        self.assert_red(render_log(self.document, stop_after_banner=True), "the log ends before Credo's closing report")

    def test_a_banner_and_an_analysis_line_that_disagree_refuse(self):
        log = render_log(self.document, banner="Checking 4 source files (this might take a while) ...")
        self.assert_red(log, "the banner names 4 source file(s) and the report 3")

    def test_a_summary_that_disagrees_with_the_sections_refuses(self):
        log = render_log(self.document, summary="4527 mods/funs, found 3 warnings, 2 software design suggestions.")
        self.assert_red(log, "is not the summary's own")

    def test_a_truncated_section_against_a_full_summary_refuses(self):
        log = render_log(
            self.document,
            rendered={"design": 1, "warning": 1},
            by_category={"design": 2, "warning": 1},
            inside_section=("┃ …  (299 more, use `--all` to show them)",),
        )
        self.assert_red(log, "is not the summary's own")

    def test_a_summary_of_no_issues_refuses(self):
        self.assert_red(render_log(self.document, summary="0 mods/funs, found no issues."), "found no issues at all")

    def test_a_missing_summary_refuses(self):
        lines = render_log(self.document).splitlines()
        log = "\n".join(line for line in lines if "mods/funs" not in line)
        self.assert_red(log, "no `N mods/funs, found ...` summary line")

    def test_a_missing_hint_refuses(self):
        lines = render_log(self.document).splitlines()
        log = "\n".join(line for line in lines if line != HINT)
        self.assert_red(log, "has no strict-mode closing line")

    def test_a_non_strict_closing_line_refuses(self):
        log = render_log(self.document).replace(
            HINT,
            "Showing priority issues: ↑ ↗ →  (use `mix credo explain` to explain issues, "
            "`mix credo --help` for options).",
        )
        self.assert_red(log, "has no strict-mode closing line")

    def test_an_out_of_order_category_refuses(self):
        self.assert_red(
            render_log(self.document, rendered={"warning": 1, "design": 2}), "is out of the report's fixed order"
        )

    def test_a_section_carrying_another_categorys_issue_refuses(self):
        log = render_log(self.document).replace("┃ [D] → message 1 of 2", "┃ [W] → message 1 of 2")
        self.assert_red(log, "the sections are crossed")

    def test_a_section_without_its_edge_line_refuses(self):
        log = render_log(self.document)
        start = log.index("  Software Design")
        edge = log.index("┃ \n", start)
        self.assert_red(log[:edge] + log[edge + 2 :], "has no edge line under its header")

    def test_a_missing_analysis_line_refuses(self):
        lines = render_log(self.document).splitlines()
        log = "\n".join(line for line in lines if not line.startswith("Analysis took"))
        self.assert_red(log, "no `Analysis took ...` line")

    def test_a_summary_fragment_naming_no_category_refuses(self):
        log = render_log(self.document, summary="4527 mods/funs, found 3 spelling issues.")
        self.assert_red(log, "names no known category")

    def test_a_section_with_no_issue_refuses(self):
        log = render_log(self.document)
        start = log.index("  Software Design")
        end = log.index("  Warnings - please take a look")
        header_and_edge = "\n".join(log[start:end].splitlines()[:2]) + "\n"
        self.assert_red(log[:start] + header_and_edge + log[end:], "is rendered with no issue")

    def test_an_unaccounted_line_inside_a_section_refuses(self):
        self.assert_red(
            render_log(self.document, inside_section=("the report skips this",)), "unaccounted Credo output"
        )

    def test_an_unaccounted_line_after_the_report_refuses(self):
        self.assert_red(render_log(self.document, after_report=("use `--all` to show the rest",)), "after the report")

    def test_a_coloured_issue_line_refuses(self):
        log = render_log(self.document).replace("┃ [D]", "┃ \x1b[32m[D]", 1)
        self.assert_red(log, "is not the summary's own")


class InvocationTests(GateCase):
    def test_a_missing_log_is_not_usable(self):
        result = citest.run_gate(SCRIPT, "--log", str(self.directory / "absent.log"), "--baseline", str(self.baseline))
        self.assertEqual(result.returncode, 2)
        self.assertIn("cannot judge the Credo strict ratchet", result.stderr)

    def test_a_missing_baseline_is_not_usable(self):
        log = self.write_log(render_log(self.document))
        result = citest.run_gate(SCRIPT, "--log", str(log), "--baseline", str(self.directory / "absent.json"))
        self.assertEqual(result.returncode, 2)
        self.assertIn("is not readable", result.stderr)

    def test_a_baseline_that_is_not_json_is_not_usable(self):
        path = self.directory / "broken.json"
        path.write_text("{", encoding="utf-8")
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(render_log(self.document))), "--baseline", str(path)
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("is not valid JSON", result.stderr)

    def test_a_baseline_whose_split_does_not_sum_to_its_total_is_not_usable(self):
        path = self.write_baseline(small_baseline(issues=9))
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(render_log(self.document))), "--baseline", str(path)
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("does not sum to 'issues'", result.stderr)

    def test_a_baseline_naming_an_unknown_category_is_not_usable(self):
        path = self.write_baseline(small_baseline(by_category={"design": 2, "spelling": 1}, issues=3))
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(render_log(self.document))), "--baseline", str(path)
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("unknown category 'spelling'", result.stderr)

    def test_a_baseline_that_is_not_an_object_is_not_usable(self):
        path = self.write_baseline([1, 2])
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(render_log(self.document))), "--baseline", str(path)
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("is not a JSON object", result.stderr)

    def test_a_baseline_without_the_check_count_is_not_usable(self):
        document = small_baseline()
        del document["checks_run"]
        path = self.write_baseline(document)
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(render_log(self.document))), "--baseline", str(path)
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("carries no 'checks_run' key", result.stderr)

    def test_a_baseline_with_an_empty_split_is_not_usable(self):
        document = small_baseline(by_category={})
        path = self.write_baseline(document)
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(render_log(self.document))), "--baseline", str(path)
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("'by_category' must be a non-empty object", result.stderr)

    def test_a_baseline_with_no_analyzed_file_is_not_usable(self):
        path = self.write_baseline(small_baseline(files_analyzed=0))
        result = citest.run_gate(
            SCRIPT, "--log", str(self.write_log(render_log(self.document))), "--baseline", str(path)
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("'files_analyzed' must be a positive integer", result.stderr)


class ShippedBaselineTests(unittest.TestCase):
    def test_the_shipped_baseline_carries_consistent_counts(self):
        baseline = GATE.Baseline(BASELINE)
        self.assertEqual(baseline.issues, sum(baseline.by_category.values()))
        self.assertGreater(baseline.files_analyzed, 0)
        self.assertGreater(baseline.checks_run, 0)
        self.assertRegex(baseline.revision, r"^[0-9a-f]{40}$")


if __name__ == "__main__":
    unittest.main()
