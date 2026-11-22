#!/usr/bin/env python3
"""Exercise exact-subject evidence through the real Git and CLI surfaces."""

import hashlib
import json
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import citest
from testsupport.gitfixture import FixtureRepo

SCRIPT = pathlib.Path(__file__).with_name("linux-v2-source-subject.py")


class SourceSubjectTests(FixtureRepo, unittest.TestCase):
    REPOSITORY_PREFIX = "symmetry-subject-"
    REPOSITORY_DIRECTORY = None

    def setUp(self) -> None:
        super().setUp()
        (self.repo / "source.txt").write_bytes(b"reviewed source\n")
        self.commit_sha = self.commit("fixture")

    def invoke(self, commit: str | None = None) -> subprocess.CompletedProcess[str]:
        return citest.run_gate(SCRIPT, str(self.repo), commit or self.commit_sha)

    def test_clean_source_emits_the_known_subject(self) -> None:
        result = self.invoke()
        blob = b"sha256:" + hashlib.sha256(b"reviewed source\n").hexdigest().encode("ascii")
        digest = hashlib.sha256(b"100644\0blob\0source.txt\0" + blob + b"\0").hexdigest()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(result.stdout),
            {
                "commit": self.commit_sha,
                "tree_digest": "sha256:" + digest,
            },
        )

    def test_wrong_commit_emits_no_subject(self) -> None:
        result = self.invoke("0" * 40)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_dirty_source_emits_no_subject_and_preserves_changes(self) -> None:
        for kind in ("staged", "unstaged", "untracked"):
            with self.subTest(kind=kind):
                path = self.repo / ("extra.go" if kind == "untracked" else "source.txt")
                original = path.read_bytes() if path.exists() else None
                path.write_bytes(b"unreviewed source\n")
                if kind == "staged":
                    self.git("add", "source.txt")
                result = self.invoke()
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")
                self.assertEqual(path.read_bytes(), b"unreviewed source\n")
                if original is None:
                    path.unlink()
                else:
                    path.write_bytes(original)
                    self.git("add", "source.txt")

    def test_non_repository_emits_no_subject(self) -> None:
        with tempfile.TemporaryDirectory(prefix="symmetry-not-git-") as directory:
            result = citest.run_gate(SCRIPT, directory, self.commit_sha)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_the_binding_covers_the_shared_module(self) -> None:
        module = self.repo / "scripts/ci/source_subject.py"
        module.parent.mkdir(parents=True)

        def expected(module_bytes: bytes) -> str:
            entries = []
            for path, blob in ((b"scripts/ci/source_subject.py", module_bytes), (b"source.txt", b"reviewed source\n")):
                digest = b"sha256:" + hashlib.sha256(blob).hexdigest().encode("ascii")
                entries.append(b"100644\0blob\0" + path + b"\0" + digest + b"\0")
            return "sha256:" + hashlib.sha256(b"".join(entries)).hexdigest()

        for module_bytes in (b"# shared rules\n", b"# changed shared rules\n"):
            with self.subTest(module_bytes=module_bytes):
                module.write_bytes(module_bytes)
                commit = self.commit("module " + module_bytes.decode().strip())
                result = self.invoke(commit)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(
                    json.loads(result.stdout),
                    {"commit": commit, "tree_digest": expected(module_bytes)},
                )

    def test_a_listing_that_is_not_a_record_refuses_as_an_invalid_source_object(self) -> None:
        tool = citest.load_gate(SCRIPT, "linux_v2_source_subject")
        real_check_output = subprocess.check_output
        real_popen = subprocess.Popen

        def fake_check_output(argv, **options):
            if "ls-tree" in argv:
                return b"100644 blob deadbeef"
            return real_check_output(argv, **options)

        def fake_popen(argv, **options):
            if "cat-file" in argv:
                raise AssertionError("a malformed tree record must be refused before any blob read")
            return real_popen(argv, **options)

        with (
            mock.patch.object(subprocess, "check_output", side_effect=fake_check_output),
            mock.patch.object(subprocess, "Popen", side_effect=fake_popen),
            self.assertRaisesRegex(SystemExit, "Git returned an invalid source object") as refused,
        ):
            tool.source_subject(str(self.repo), self.commit_sha)
        self.assertEqual(refused.exception.__cause__.code, "invalid_tree_record")

    def test_a_symlink_record_is_admitted_by_the_any_mode_rule(self) -> None:
        # With symlinks disabled a regular file carrying the link text is the 120000
        # entry's clean checkout, so the fixture needs no real symlink.
        self.git("config", "core.symlinks", "false")
        target = b"source.txt"
        blob = self.git("hash-object", "-w", "--stdin", input=target).decode("ascii").strip()
        self.git("update-index", "--add", "--cacheinfo", f"120000,{blob},link.txt")
        (self.repo / "link.txt").write_bytes(target)
        self.git("commit", "--quiet", "-m", "symlink")
        commit = self.git_text("rev-parse", "HEAD")

        result = self.invoke(commit)

        entries = [
            b"120000\0blob\0link.txt\0sha256:" + hashlib.sha256(target).hexdigest().encode("ascii") + b"\0",
            b"100644\0blob\0source.txt\0sha256:"
            + hashlib.sha256(b"reviewed source\n").hexdigest().encode("ascii")
            + b"\0",
        ]
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(result.stdout),
            {"commit": commit, "tree_digest": "sha256:" + hashlib.sha256(b"".join(entries)).hexdigest()},
        )


if __name__ == "__main__":
    unittest.main()
