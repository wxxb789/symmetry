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
import unittest

import citest
from testsupport.gitfixture import FixtureRepo

SCRIPT = pathlib.Path(__file__).with_name("check-record-cleanliness.py")
ZERO = "0" * 40


class RecordCleanlinessTests(FixtureRepo, unittest.TestCase):
    REPOSITORY_PREFIX = "record-cleanliness-"

    def setUp(self) -> None:
        super().setUp()
        self.write("docs/evaluations/model-routing/clean.json", '{"route": "/portal/runtimes"}\n')
        # A pre-existing dirty record is deliberately present in the base: the gate
        # checks what a range changes, never the whole tree.
        self.write("docs/evidence/old-dirty.md", "the operator's temp root was /home/operator/eval\n")
        self.write("daemon/main.go", "package main\n")
        self.base = self.commit("fixture base")

    def run_gate(self, *arguments: str) -> subprocess.CompletedProcess:
        return citest.run_gate(SCRIPT, "--repo", str(self.repo), *arguments)

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
        # The fixture bytes are inert - the gate resolves the sanitize tool from its
        # own directory (--repo only names the git repository) - so a change to that
        # path alone selects no record and the gate reports "nothing to check": the
        # selector side of the tool-path case, with the tool's own behaviour covered
        # by its own suite. The ci.yml classifier arm that turns this path into a job
        # trigger is not executed here (this suite runs no workflow conditions); its own
        # fixture suite is scripts/ci/test-ci-changed-path-classifier.py, recorded as
        # closed under "Record and documentation hygiene" in docs/archive/CLOSED-TODO.md.
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
        self.assertIn("not present in this checkout", result.stderr)
        self.assertIn("fallback is the head itself", result.stderr)
        self.assertNotIn("nothing to check", result.stdout)

    def test_an_empty_or_zero_base_at_the_main_tip_reports_nothing_to_check(self) -> None:
        # A tag push or a manual dispatch passes no base or an all-zero one. When the head
        # is already origin/main's tip the fallback range is empty by construction, which
        # is the documented pass - even though the head adds a host-path record, because
        # such a revision is not "this change" compared against an earlier one.
        self.write("docs/evidence/host-path.md", "the run store was /tmp/symmetry-0008d-eval\n")
        head = self.commit("tagged record")
        self.git("update-ref", "refs/remotes/origin/main", head)
        for requested in ("", ZERO):
            with self.subTest(base=requested):
                result = self.run_gate("--base", requested, "--head", head)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("nothing to check", result.stdout)

    def test_an_all_zero_base_on_an_older_main_checks_the_added_records(self) -> None:
        # The documented fallback with a real range: origin/main is behind the head, so a
        # tag-like run checks the records the head added.
        self.git("update-ref", "refs/remotes/origin/main", self.base)
        self.write("docs/evidence/clean-add.md", "the route /portal/runtimes stays literal\n")
        head = self.commit("all-zero base on an older main")
        result = self.run_gate("--base", ZERO, "--head", head)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("1 changed record(s) pass the root-only check", result.stdout)

    def test_a_record_that_is_not_text_is_reported_as_uncheckable(self) -> None:
        # A UTF-16 record hiding a host path is not text the sanitize tool can check; the
        # gate must say so instead of claiming the record carries a host absolute path.
        payload = '{"root":"/tmp/symmetry-0008d-eval"}\n'.encode("utf-16-le")
        path = self.repo / "docs" / "evidence" / "binary-record.md"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(payload)
        head = self.commit("add a non-text record")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("could not be checked at all", result.stderr)
        self.assertNotIn("still carry a host absolute path", result.stderr)

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
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 2, result.stdout)
        self.assertIn("cannot resolve a comparison base", result.stderr)

    def test_a_renamed_record_is_checked_under_its_new_path(self) -> None:
        # `--no-renames` reports a rename as delete + add, and the added path carries
        # the content the gate must parse.
        self.git("mv", "docs/evaluations/model-routing/clean.json", "docs/evaluations/model-routing/renamed.json")
        self.write("docs/evaluations/model-routing/renamed.json", '{"store": "/tmp/symmetry-0008d-eval"}\n')
        head = self.commit("rename a record and smuggle a host path")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("renamed.json", result.stderr)

    def test_a_record_committed_as_a_symlink_is_refused_even_with_a_clean_target(self) -> None:
        # F04: the gate reads the head revision's own blob, so the record's *stored*
        # bytes are checked - a committed symlink stores its target text, while the
        # worktree keeps serving the (clean) target file. Pre-fix this passed, because
        # the gate followed the worktree link to the clean target.
        target = self.root / "clean-target.md"
        target.write_text("the route /portal/runtimes stays literal\n", encoding="utf-8")
        # Record the head entry as a symlink blob through the index, so the case works
        # on hosts without symlink privileges: mode 120000, bytes = the link text, and
        # the worktree file stays the clean regular file it already is.
        blob = self.git("hash-object", "-w", "--stdin", input=str(target).encode("utf-8"))
        self.git(
            "update-index",
            "--add",
            "--cacheinfo",
            f"120000,{blob.decode('ascii').strip()},docs/evaluations/model-routing/clean.json",
        )
        tree = self.git_text("write-tree")
        head = self.git_text("commit-tree", tree, "-p", self.base, "-m", "commit the record as a link")
        self.git("update-ref", "HEAD", head)
        status = self.git_text(
            "diff", "--name-status", self.base, head, "--", "docs/evaluations/model-routing/clean.json"
        )
        self.assertTrue(status.startswith("T"), status)
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("clean.json", result.stderr)
        self.assertIn("not regular file blobs", result.stderr)

    def test_a_record_committed_as_a_gitlink_takes_the_nonregular_arm(self) -> None:
        # STD-A08: a real gitlink's object id is the subproject commit, so
        # `cat-file blob <that id>` fails with "bad file" and the gate exited 2
        # (an unusable range) instead of the documented non-regular refusal
        # (exit 1). The head entry's mode decides before the blob is read.
        relative = "docs/evaluations/model-routing/gitlink-record"
        self.git("update-index", "--add", "--cacheinfo", f"160000,{self.base},{relative}")
        tree = self.git_text("write-tree")
        head = self.git_text("commit-tree", tree, "-p", self.base, "-m", "commit a record as a gitlink")
        self.git("update-ref", "HEAD", head)
        status = self.git_text("diff", "--name-status", self.base, head, "--", relative)
        self.assertTrue(status.startswith("A"), status)
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("gitlink-record", result.stderr)
        self.assertIn("stores it as mode 160000", result.stderr)
        self.assertIn("not regular file blobs", result.stderr)
        self.assertNotIn("cannot read the blob", result.stderr)

    def test_an_all_zero_base_falls_back_to_the_merge_base(self) -> None:
        self.git("update-ref", "refs/remotes/origin/main", self.base)
        self.write("docs/evidence/host-path.md", "the run store was /tmp/symmetry-0008d-eval\n")
        head = self.commit("add a record on a new branch")
        result = self.run_gate("--base", ZERO, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("host-path.md", result.stderr)

    def test_a_non_fast_forward_push_is_compared_with_its_requested_base(self) -> None:
        # F05: a rollback push (the head reintroduces a record an earlier commit had
        # cleaned) makes the head an ancestor of the requested base. Narrowing to the
        # merge base collapsed the range to the head itself and reported "nothing to
        # check" while the head's stored bytes were dirty; a requested base is now
        # compared directly, so the reintroduced record is selected and refused.
        self.write("docs/evidence/rollback.md", "the run store was /tmp/symmetry-0008d-eval\n")
        dirty = self.commit("add the record with a host path")
        self.write("docs/evidence/rollback.md", "the run store renders as <disposable-root>\n")
        clean = self.commit("clean the record")
        self.git("update-ref", "HEAD", dirty)
        result = self.run_gate("--base", clean, "--head", dirty)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("rollback.md", result.stderr)
        self.assertNotIn("nothing to check", result.stdout)

    def test_an_unusable_diff_exits_with_the_unusable_revision_code(self) -> None:
        # F11: a git diff that cannot run (here: `diff.orderFile` names a missing file)
        # is an unusable revision range, so the gate must exit 2 like every other
        # unusable-range arm, not the record-failure code 1.
        self.write("docs/evidence/host-path.md", "the run store was /tmp/symmetry-0008d-eval\n")
        head = self.commit("add a record the diff cannot inspect")
        environment = dict(
            os.environ,
            GIT_CONFIG_COUNT="1",
            GIT_CONFIG_KEY_0="diff.orderFile",
            GIT_CONFIG_VALUE_0=str(self.root / "missing-order-file"),
        )
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--repo", str(self.repo), "--base", self.base, "--head", head],
            capture_output=True,
            text=True,
            check=False,
            env=environment,
        )
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("cannot diff", result.stderr)


if __name__ == "__main__":
    unittest.main()
