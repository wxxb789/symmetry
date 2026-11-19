#!/usr/bin/env python3
"""Exercise the committed-record cleanliness gate through real Git repositories.

The fixture repositories here are throwaway trees: the gate is a selector over a
revision range plus the sanitize tool's own root-only check, so the tests pin
selection (added/modified/type-changed records only, nothing when the range changes
no record) and the boundary the tool draws (a product route and a bare root token
stay literal; a real host path is refused).
"""

import os
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("check-record-cleanliness.py")
ZERO = "0" * 40


class RecordCleanlinessTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="record-cleanliness-")
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.repo = self.root / "repository"
        self.repo.mkdir()
        self.git("init", "--quiet")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.write("docs/evaluations/model-routing/clean.json", '{"route": "/portal/runtimes"}\n')
        # A pre-existing dirty record is deliberately present in the base: the gate
        # checks what a range changes, never the whole tree.
        self.write("docs/evidence/old-dirty.md", "the operator's temp root was /home/operator/eval\n")
        self.write("daemon/main.go", "package main\n")
        self.git("add", "-A")
        self.git("commit", "--quiet", "-m", "fixture base")
        self.base = self.git_text("rev-parse", "HEAD")

    # -- helpers -------------------------------------------------------------

    def git(self, *arguments: str) -> bytes:
        return subprocess.check_output(
            ["git", "-C", str(self.repo), *arguments], stderr=subprocess.PIPE
        )

    def git_text(self, *arguments: str) -> str:
        return self.git(*arguments).decode("ascii").strip()

    def write(self, relative: str, content: str) -> pathlib.Path:
        path = self.repo / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return path

    def commit(self, message: str) -> str:
        self.git("add", "-A")
        self.git("commit", "--quiet", "-m", message)
        return self.git_text("rev-parse", "HEAD")

    def run_gate(self, *arguments: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--repo", str(self.repo), *arguments],
            capture_output=True, text=True, check=False,
        )

    # -- tests ---------------------------------------------------------------

    def test_a_range_that_changes_no_record_reports_nothing_to_check(self) -> None:
        self.write("docs/TODO.md", "notes\n")
        self.write("daemon/main.go", "package main\n\nfunc main() {}\n")
        head = self.commit("documentation and code")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("nothing to check", result.stdout)
        # The base's dirty record exists at the head revision and is still not checked.
        self.assertTrue((self.repo / "docs/evidence/old-dirty.md").is_file())

    def test_a_changed_sanitize_tool_alone_changes_no_record(self) -> None:
        self.write("scripts/ci/model-routing-record-sanitize.py", "print('not the real tool')\n")
        head = self.commit("touch the sanitize tool")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("nothing to check", result.stdout)

    def test_deleted_records_are_not_checked(self) -> None:
        (self.repo / "docs/evidence/old-dirty.md").unlink()
        head = self.commit("delete the old record")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("nothing to check", result.stdout)

    def test_an_added_record_with_a_host_path_is_refused(self) -> None:
        self.write("docs/evaluations/model-routing/new.json", '{"trace": "/tmp/symmetry-0008d-eval/log"}\n')
        head = self.commit("add a record with a disposable root")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("new.json", result.stderr)
        self.assertIn("refused", result.stderr)

    def test_a_modified_record_with_a_drive_path_is_refused(self) -> None:
        path = self.repo / "docs/evaluations/model-routing/clean.json"
        self.write("docs/evaluations/model-routing/clean.json", '{"store": "C:/Users/operator/run"}\n')
        self.assertTrue(path.is_file())
        head = self.commit("smuggle a host path into an existing record")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("clean.json", result.stderr)

    def test_evidence_markdown_records_are_checked(self) -> None:
        self.write("docs/evidence/goal-fixture.md", "ran under /mnt/q/repos/symmetry/daemon\n")
        head = self.commit("add evidence with a host path")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("goal-fixture.md", result.stderr)

    def test_the_gate_runs_the_tools_own_root_only_check(self) -> None:
        # These are exactly the boundaries of `--check --root-only`: a product route
        # and a bare root token stay literal, while a real host path refuses. A grep
        # over `/tmp` or `/portal` would draw both lines differently.
        self.write("docs/evidence/bare-roots.md", "the render step was blocked by `/tmp` and `/home`\n")
        head = self.commit("a bare root mention passes")
        passing = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(passing.returncode, 0, passing.stderr)
        self.assertIn("pass the root-only check", passing.stdout)
        self.write("docs/evidence/host-path.md", "the run store was /tmp/symmetry-0008d-eval\n")
        head = self.commit("a host path refuses")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("host-path.md", result.stderr)
        self.assertNotIn("bare-roots.md", result.stderr)

    def test_an_unreachable_base_falls_back_and_says_so(self) -> None:
        # A force-push leaves the pushed `before` commit unreachable; the gate falls
        # back to the origin/main merge base instead of failing open or refusing.
        self.git("update-ref", "refs/remotes/origin/main", self.base)
        self.write("docs/evidence/host-path.md", "the run store was /tmp/symmetry-0008d-eval\n")
        head = self.commit("add a record")
        result = self.run_gate("--base", "0123456789abcdef0123456789abcdef01234567", "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("host-path.md", result.stderr)
        self.assertIn("not present in this checkout", result.stderr)

    def test_a_fallback_base_that_equals_the_head_is_refused(self) -> None:
        # A force-push to main whose `before` is unreachable resolves to a fallback base
        # equal to the head (origin/main now points at it): comparing a revision with
        # itself would pass vacuously while the head adds a host-path record, so the gate
        # refuses instead of reporting "nothing to check".
        self.write("docs/evidence/host-path.md", "the run store was /tmp/symmetry-0008d-eval\n")
        head = self.commit("force-pushed record")
        self.git("update-ref", "refs/remotes/origin/main", head)
        result = self.run_gate("--base", "0123456789abcdef0123456789abcdef01234567", "--head", head)
        self.assertEqual(result.returncode, 2, result.stdout)
        self.assertIn("fallback base is the head", result.stderr)
        self.assertNotIn("nothing to check", result.stdout)

    def test_a_range_with_no_comparison_at_all_is_refused(self) -> None:
        # A repository with a single commit and no origin/main has no base to compare
        # against: refusing is the only safe outcome.
        orphan = self.root / "orphan-repository"
        orphan.mkdir()
        subprocess.check_call(["git", "init", "--quiet"], cwd=str(orphan))
        subprocess.check_call(["git", "config", "user.name", "Fixture"], cwd=str(orphan))
        subprocess.check_call(["git", "config", "user.email", "fixture@example.invalid"], cwd=str(orphan))
        (orphan / "docs" / "evidence").mkdir(parents=True)
        (orphan / "docs" / "evidence" / "record.md").write_text("clean\n", encoding="utf-8")
        subprocess.check_call(["git", "add", "-A"], cwd=str(orphan))
        subprocess.check_call(["git", "commit", "--quiet", "-m", "only commit"], cwd=str(orphan))
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--repo", str(orphan), "--base", ZERO],
            capture_output=True, text=True, check=False,
        )
        self.assertEqual(result.returncode, 2, result.stdout)
        self.assertIn("cannot resolve a comparison base", result.stderr)

    def test_a_renamed_record_is_checked_under_its_new_path(self) -> None:
        # `--no-renames` reports a rename as delete + add, and the added path carries
        # the content the gate must parse.
        self.git("mv", "docs/evaluations/model-routing/clean.json",
                 "docs/evaluations/model-routing/renamed.json")
        self.write("docs/evaluations/model-routing/renamed.json", '{"store": "/tmp/symmetry-0008d-eval"}\n')
        head = self.commit("rename a record and smuggle a host path")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("renamed.json", result.stderr)

    def test_a_type_changed_record_is_checked(self) -> None:
        # The selection includes a path that changed type (a file replaced by a link):
        # the tool reads what the path serves at the head revision.
        target = self.root / "linked-record.json"
        target.write_text('{"store": "/tmp/symmetry-0008d-eval"}\n', encoding="utf-8")
        path = self.repo / "docs" / "evaluations" / "model-routing" / "clean.json"
        path.unlink()
        try:
            os.symlink(target, path)
        except (OSError, NotImplementedError) as error:
            self.skipTest(f"symlink creation is unavailable: {error}")
        head = self.commit("replace a record with a link")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("clean.json", result.stderr)

    def test_an_all_zero_base_falls_back_to_the_merge_base(self) -> None:
        self.git("update-ref", "refs/remotes/origin/main", self.base)
        self.write("docs/evidence/host-path.md", "the run store was /tmp/symmetry-0008d-eval\n")
        head = self.commit("add a record on a new branch")
        result = self.run_gate("--base", ZERO, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("host-path.md", result.stderr)


if __name__ == "__main__":
    unittest.main()
