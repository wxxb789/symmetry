#!/usr/bin/env python3
"""Exercise exact-subject evidence through the real Git and CLI surfaces."""

import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("linux-v2-source-subject.py")


class SourceSubjectTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory(prefix="symmetry-subject-")
        self.addCleanup(temporary.cleanup)
        self.repository = pathlib.Path(temporary.name)
        self.git("init", "--quiet")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "core.autocrlf", "false")
        (self.repository / "source.txt").write_bytes(b"reviewed source\n")
        self.git("add", "source.txt")
        self.git("commit", "--quiet", "-m", "fixture")
        self.commit = self.git("rev-parse", "HEAD").decode("ascii").strip()

    def git(self, *arguments: str) -> bytes:
        return subprocess.check_output(
            ["git", "-C", str(self.repository), *arguments], stderr=subprocess.PIPE
        )

    def invoke(self, commit: str | None = None) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(self.repository), commit or self.commit],
            capture_output=True, text=True, check=False,
        )

    def test_clean_source_emits_the_known_subject(self) -> None:
        result = self.invoke()
        blob = b"sha256:" + hashlib.sha256(b"reviewed source\n").hexdigest().encode("ascii")
        digest = hashlib.sha256(b"100644\0blob\0source.txt\0" + blob + b"\0").hexdigest()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout), {
            "commit": self.commit, "tree_digest": "sha256:" + digest,
        })

    def test_wrong_commit_emits_no_subject(self) -> None:
        result = self.invoke("0" * 40)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_dirty_source_emits_no_subject_and_preserves_changes(self) -> None:
        for kind in ("staged", "unstaged", "untracked"):
            with self.subTest(kind=kind):
                path = self.repository / ("extra.go" if kind == "untracked" else "source.txt")
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
            result = subprocess.run(
                [sys.executable, str(SCRIPT), directory, self.commit],
                capture_output=True, text=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
