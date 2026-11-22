#!/usr/bin/env python3
"""Exercise exact-subject evidence through the real Git and CLI surfaces."""

import hashlib
import json
import pathlib
import subprocess
import tempfile
import unittest

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


if __name__ == "__main__":
    unittest.main()
