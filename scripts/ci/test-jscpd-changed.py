#!/usr/bin/env python3

import json
import pathlib
import subprocess
import sys
import unittest

import citest
from testsupport.gitfixture import FixtureRepo

SCRIPT = pathlib.Path(__file__).with_name("jscpd-changed.py")
ROOT = SCRIPT.resolve().parents[2]
CONFIG = ROOT / ".jscpd.json"
JSCPD_PACKAGE = ROOT / "node_modules" / "jscpd"


def clone_block(prefix: str) -> str:
    return "\n".join(
        f"    {prefix}_{index} = compute_{index}(payload, options) + offset_{index} * {index + 1}"
        for index in range(12)
    )


CLONE = clone_block("value")
CLEAN = """\
def untouched(payload, options):
    total = 0
    for entry in payload.entries:
        total += entry.weight(options.scale)
    return total
"""


class JscpdChangedTests(FixtureRepo, unittest.TestCase):
    REPOSITORY_PREFIX = "jscpd-changed-"

    def setUp(self) -> None:
        super().setUp()
        self.write(".jscpd.json", CONFIG.read_text(encoding="utf-8"))

    def write_config(self, document: dict) -> None:
        citest.write_json(self.repo / ".jscpd.json", document)

    def gate(self, *arguments: str) -> subprocess.CompletedProcess:
        return citest.run_gate(SCRIPT, "--repo", str(self.repo), *arguments)

    def base_and_head(self) -> tuple[str, str]:
        return self.git_text("rev-parse", "HEAD~1"), self.git_text("rev-parse", "HEAD")

    def seed_clone_pair(self) -> None:
        self.write("src/source.py", f"def build():\n{CLONE}\n    return value_0\n")
        self.write("src/left.py", "def left():\n    return 1\n")
        self.write("src/clean.py", CLEAN)
        self.commit("base")

    def seed_twin_pair(self) -> None:
        self.write("src/source.py", f"def build():\n{CLONE}\n    return value_0\n")
        self.write("src/twin.py", f"def twin():\n{CLONE}\n    return value_0\n")

    def test_a_changed_file_carrying_a_new_clone_is_refused(self) -> None:
        self.seed_clone_pair()
        self.write("src/left.py", f"def left():\n{CLONE}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("src/left.py", completed.stderr)
        self.assertIn("src/source.py", completed.stderr)
        self.assertIn("deduplicate", completed.stderr)

    def test_a_clean_changed_file_is_green(self) -> None:
        self.seed_clone_pair()
        self.write("src/clean.py", CLEAN + "\ndef added():\n    return 2\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("none new", completed.stdout)

    def test_a_reformat_that_only_rewrites_a_clone_is_tolerated(self) -> None:
        self.seed_twin_pair()
        self.commit("base")
        reflowed = "\n".join(line.strip() for line in CLONE.splitlines())
        self.write("src/source.py", f"def build():\n{reflowed}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("1 tolerated among the added lines", completed.stdout)

    def test_a_block_moved_inside_its_file_is_tolerated(self) -> None:
        self.seed_twin_pair()
        self.commit("base")
        self.write("src/source.py", f"def build():\n    return 1\n\ndef moved():\n{CLONE}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("1 tolerated among the added lines", completed.stdout)

    def test_a_block_copied_inside_its_file_is_refused(self) -> None:
        self.seed_twin_pair()
        self.commit("base")
        self.write(
            "src/source.py",
            f"def build():\n{CLONE}\n    return value_0\n\ndef copied():\n{CLONE}\n    return value_0\n",
        )
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("copied, not moved", completed.stderr)

    def test_a_third_copy_of_a_pre_existing_duplication_is_refused(self) -> None:
        self.seed_twin_pair()
        self.write("src/clean.py", CLEAN)
        self.commit("base")
        self.write("src/clean.py", CLEAN + f"\ndef copied():\n{CLONE}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("src/clean.py", completed.stderr)

    def test_a_renamed_file_is_compared_against_its_base_path(self) -> None:
        self.seed_twin_pair()
        self.commit("base")
        self.git("mv", "src/source.py", "src/moved.py")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("1 tolerated among the added lines", completed.stdout)

    def test_a_clone_between_untouched_files_is_green(self) -> None:
        self.seed_twin_pair()
        self.write("src/clean.py", CLEAN)
        self.commit("base")
        self.write("src/clean.py", CLEAN + "\ndef added():\n    return 2\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)

    def test_a_clone_outside_the_added_lines_is_green(self) -> None:
        self.seed_twin_pair()
        self.commit("base")
        self.write("src/source.py", f"# first\n# second\n# third\n\ndef build():\n{CLONE}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("0 tolerated among the added lines", completed.stdout)

    def test_a_copier_with_a_non_ascii_path_is_refused(self) -> None:
        self.write("src/source.py", f"def build():\n{CLONE}\n    return value_0\n")
        self.write("src/clean.py", CLEAN)
        self.commit("base")
        self.write("src/café.py", f"def copied():\n{CLONE}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("src/café.py", completed.stderr)

    def test_a_copier_with_a_space_in_its_path_is_refused(self) -> None:
        self.write("src/source.py", f"def build():\n{CLONE}\n    return value_0\n")
        self.write("src/clean.py", CLEAN)
        self.commit("base")
        self.write("src/le ft.py", f"def copied():\n{CLONE}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("src/le ft.py", completed.stderr)

    def test_a_content_line_shaped_like_an_added_header_is_not_a_header(self) -> None:
        self.write("src/source.py", f"def build():\n{CLONE}\n    return value_0\n")
        self.write("src/clean.py", "def untouched(payload, options):\n    total = 0\n    return total\n")
        self.commit("base")
        self.write(
            "src/clean.py",
            "def untouched(payload, options):\n"
            "    total = 0\n"
            "++ b/decoy.py\n"
            "    return total\n"
            "\n"
            f"def copied(payload, options):\n{CLONE}\n    return value_0\n",
        )
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("src/clean.py", completed.stderr)

    def test_a_content_line_shaped_like_a_removed_header_is_not_a_header(self) -> None:
        filler = "\n".join(f"    step_{index} = transform(payload, options, {index})" for index in range(20))
        self.write("src/source.py", f"-- first\n{CLONE}\n{filler}\n")
        self.write("src/twin.py", f"def twin():\n{CLONE}\n    return value_0\n")
        self.commit("base")
        self.write("src/source.py", f"{filler}\n{CLONE}\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("1 tolerated among the added lines", completed.stdout)

    def test_a_self_copy_merged_into_an_edit_hunk_is_refused(self) -> None:
        self.seed_twin_pair()
        self.commit("base")
        self.write(
            "src/source.py", f"def build():\n{CLONE}\n    return 1\n\ndef copied():\n{CLONE}\n    return computed()\n"
        )
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("copied, not moved", completed.stderr)

    def test_a_range_without_changes_is_green(self) -> None:
        self.seed_clone_pair()
        head = self.git_text("rev-parse", "HEAD")
        completed = self.gate("--base", head, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("nothing to check", completed.stdout)

    def test_a_deleted_file_leaves_nothing_to_check(self) -> None:
        self.seed_clone_pair()
        self.git("rm", "--quiet", "src/left.py")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("nothing to check", completed.stdout)

    def test_a_clone_in_a_large_changed_file_is_refused_only_with_the_explicit_limits(self) -> None:
        self.write("src/source.py", f"def build():\n{CLONE}\n    return value_0\n")
        self.write("src/clean.py", CLEAN)
        self.commit("base")
        filler = "\n".join(
            f"    payload_{index} = transform_{index}(payload, options, {index})" + " " * 60 for index in range(1250)
        )
        self.write("src/large.py", f"def large():\n{filler}\n{CLONE}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()

        report_directory = pathlib.Path(str(self.repo)) / "report"
        gate = citest.load_gate(SCRIPT, "jscpd_changed")
        defaults = subprocess.run(
            [
                "node",
                str(gate.jscpd_entrypoint(JSCPD_PACKAGE)),
                "src",
                "--reporters",
                "json",
                "--output",
                str(report_directory),
                "--silent",
                "--no-colors",
            ],
            cwd=self.repo,
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(defaults.returncode, 0, defaults.stderr)
        default_report = json.loads((report_directory / "jscpd-report.json").read_text(encoding="utf-8"))
        self.assertEqual(default_report["duplicates"], [], "jscpd's defaults must skip the large file silently")

        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("src/large.py", completed.stderr)

    def test_the_gate_carries_the_explicit_limits(self) -> None:
        gate = citest.load_gate(SCRIPT, "jscpd_changed")
        self.assertEqual(
            gate.JSCPD_LIMITS,
            ("--min-lines", "10", "--min-tokens", "50", "--max-lines", "5000", "--max-size", "1mb"),
        )

    def test_the_committed_config_pins_the_detection_mode(self) -> None:
        commented = CLONE.replace("    value_3 =", "    # reconcile the third slot\n    value_3 =")
        self.write("src/source.py", f"def build():\n{CLONE}\n    return value_0\n")
        self.write("src/clean.py", CLEAN)
        self.commit("base")
        self.write("src/left.py", f"def left():\n{commented}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()

        committed = self.gate("--base", base, "--head", head)
        self.assertEqual(committed.returncode, 0, committed.stderr)

        self.write_config({"gitignore": False, "mode": "strict", "formatsExts": {"python": ["py"]}})
        dirty = self.gate("--base", base, "--head", head)
        self.assertEqual(dirty.returncode, 0, dirty.stderr)

        strict_head = self.commit("strict config")
        strict = self.gate("--base", base, "--head", strict_head)
        self.assertEqual(strict.returncode, 1, strict.stderr)

    def test_the_committed_config_declares_the_elixir_format(self) -> None:
        block = CLONE
        self.write(
            "lib/source.ex",
            f"defmodule Source do\n  def build(payload, options) do\n{block}\n    value_0\n  end\nend\n",
        )
        self.write("lib/clean.ex", "defmodule Clean do\n  def run(), do: :ok\nend\n")
        self.commit("base")
        self.write(
            "lib/twin.ex", f"defmodule Twin do\n  def build(payload, options) do\n{block}\n    value_0\n  end\nend\n"
        )
        self.commit("head")
        base, head = self.base_and_head()

        committed = self.gate("--base", base, "--head", head)
        self.assertEqual(committed.returncode, 1, committed.stderr)
        self.assertIn("lib/twin.ex", committed.stderr)

        self.write_config({"gitignore": False, "mode": "mild", "formatsExts": {"python": ["py"]}})
        python_only = self.commit("python-only config")
        undeclared = self.gate("--base", base, "--head", python_only)
        self.assertEqual(undeclared.returncode, 2, undeclared.stderr)
        self.assertIn("wrote no report", undeclared.stderr)

        self.write("src/helper.py", f"def helper(payload, options):\n{clone_block('helper')}\n    return helper_0\n")
        self.commit("python context")
        context_head = self.git_text("rev-parse", "HEAD")
        noticed = self.gate("--base", base, "--head", context_head)
        self.assertEqual(noticed.returncode, 0, noticed.stderr)
        self.assertIn("lib/twin.ex", noticed.stderr)
        self.assertIn("did not scan it", noticed.stderr)

    def test_the_committed_config_keeps_dependency_trees_out_of_the_corpus(self) -> None:
        self.write("deps/lib/source.py", f"def build():\n{CLONE}\n    return value_0\n")
        self.write("src/clean.py", CLEAN)
        self.commit("base")
        self.write("deps/lib/twin.py", f"def twin():\n{CLONE}\n    return value_0\n")
        self.commit("head")
        base, head = self.base_and_head()

        committed = self.gate("--base", base, "--head", head)
        self.assertEqual(committed.returncode, 0, committed.stderr)
        self.assertIn("deps/lib/twin.py carries added lines but jscpd did not scan it", committed.stderr)

        self.write_config({"gitignore": False, "mode": "mild"})
        unscoped_head = self.commit("unscoped config")
        unscoped = self.gate("--base", base, "--head", unscoped_head)
        self.assertEqual(unscoped.returncode, 1, unscoped.stderr)

    def test_a_changed_file_jscpd_cannot_scan_is_a_notice_not_a_pass(self) -> None:
        self.write("src/clean.py", CLEAN)
        self.write(".gitattributes", "* text=auto\n")
        self.commit("base")
        self.write(".gitattributes", "* text=auto\neol=lf\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn(".gitattributes carries added lines but jscpd did not scan it", completed.stderr)

    def test_a_corpus_without_a_scannable_source_is_refused(self) -> None:
        self.write_config({"formatsExts": {}})
        self.write(".gitattributes", "* text=auto\n")
        self.write("docs/notes.md", "# one\n\ntwo\n")
        self.commit("base")
        self.write(".gitattributes", "* text=auto\neol=lf\n")
        self.commit("head")
        base, head = self.base_and_head()
        completed = self.gate("--base", base, "--head", head)
        self.assertEqual(completed.returncode, 2, completed.stderr)
        self.assertIn("wrote no report", completed.stderr)


if __name__ == "__main__":
    sys.exit(unittest.main())
