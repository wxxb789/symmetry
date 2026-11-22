import os
import pathlib
import unittest
from unittest import mock

import citest
from testsupport.gitfixture import FixtureRepo


class RepositoryAtRootTests(FixtureRepo, unittest.TestCase):
    REPOSITORY_PREFIX = "gitfixture-root-"
    REPOSITORY_DIRECTORY = None

    def test_the_temporary_root_is_the_repository(self):
        self.assertEqual(self.repo, self.root)
        self.assertTrue(self.root.name.startswith("gitfixture-root-"))
        self.assertEqual(citest.git_text(self.repo, "rev-parse", "--is-inside-work-tree"), "true")

    def test_write_creates_the_parents_and_writes_bytes(self):
        path = self.write("nested/deep/file.txt", "one\ntwo\n")
        self.assertEqual(path, self.repo / "nested/deep/file.txt")
        self.assertEqual(path.read_bytes(), b"one\ntwo\n")

    def test_commit_stages_every_change_and_returns_the_head(self):
        self.write("tracked.txt", "content\n")
        head = self.commit("fixture commit")
        self.assertEqual(head, self.git_text("rev-parse", "HEAD"))
        self.assertEqual(self.git_text("ls-tree", "-r", "--name-only", "HEAD"), "tracked.txt")
        self.assertEqual(self.git_text("log", "-1", "--format=%ae"), "fixture@example.invalid")

    def test_the_identity_signing_and_eol_configuration_is_the_fixture_s(self):
        self.assertEqual(self.git_text("config", "--get", "user.name"), "Fixture")
        self.assertEqual(self.git_text("config", "--get", "user.email"), "fixture@example.invalid")
        self.assertEqual(self.git_text("config", "--get", "commit.gpgsign"), "false")
        self.assertEqual(self.git_text("config", "--get", "core.autocrlf"), "false")

    def test_a_commit_needs_no_host_identity_or_signing_configuration(self):
        empty = {"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_SYSTEM": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}
        with mock.patch.dict(os.environ, empty):
            self.write("tracked.txt", "content\n")
            head = self.commit("isolated commit")
        self.assertEqual(self.git_text("log", "-1", "--format=%ae", head), "fixture@example.invalid")

    def test_an_inherited_git_dir_does_not_redirect_the_fixture(self):
        other = self.root / "other"
        other.mkdir()
        citest.git(other, "init", "--quiet")
        with mock.patch.dict(os.environ, {"GIT_DIR": str(other / ".git")}):
            toplevel = pathlib.Path(self.git_text("rev-parse", "--show-toplevel")).resolve()
        self.assertEqual(toplevel, self.repo.resolve())

    def test_cwd_reads_another_repository_through_the_same_isolation(self):
        other = self.root / "other"
        other.mkdir()
        citest.git(other, "init", "--quiet")
        with mock.patch.dict(os.environ, {"GIT_DIR": str(self.repo / ".git")}):
            toplevel = pathlib.Path(self.git_text("rev-parse", "--show-toplevel", cwd=other)).resolve()
        self.assertEqual(toplevel, other.resolve())


class RepositoryInSubdirectoryTests(FixtureRepo, unittest.TestCase):
    REPOSITORY_PREFIX = "gitfixture-subdir-"

    def test_the_repository_lives_below_the_temporary_root(self):
        self.assertEqual(self.repo, self.root / "repository")
        self.assertEqual(citest.git_text(self.repo, "rev-parse", "--is-inside-work-tree"), "true")

    def test_write_resolves_below_the_repository_root(self):
        path = self.write("docs/note.md", "note\n")
        self.assertEqual(path, self.repo / "docs/note.md")
        self.assertTrue(path.is_file())

    def test_write_keeps_an_absolute_path_outside_the_repository(self):
        outside = self.write(self.root / "outside.txt", "content\n")
        self.assertEqual(outside, self.root / "outside.txt")
        self.assertEqual(outside.read_bytes(), b"content\n")


class LifecycleTests(unittest.TestCase):
    def test_the_temporary_root_is_removed_when_the_case_finishes(self):
        class _Probe(FixtureRepo, unittest.TestCase):
            REPOSITORY_PREFIX = "gitfixture-probe-"
            REPOSITORY_DIRECTORY = None

            def runTest(self):
                pass

        probe = _Probe()
        probe.setUp()
        root = probe.root
        self.assertTrue(root.is_dir())
        probe.doCleanups()
        self.assertFalse(root.exists())


if __name__ == "__main__":
    unittest.main()
