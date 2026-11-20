#!/usr/bin/env python3
"""Exercise the document-citation existence gate through real Git repositories.

The fixture repositories here are throwaway trees: the gate is a selector over a
revision range plus a citation grammar, so the tests pin what the grammar audits (a
line-suffixed path, a file-shaped path, a directory citation, a wildcard anchor), what
it deliberately does not (an extensionless multi-segment token, a command, a URL), the
resolution boundary (the head tree, or the checkout's own ignore rules for generated
output), the symbol binding and its whole-word occurrence test, and the range side the
gate shares with `check-record-cleanliness.py` (a changed file's head blob, never the
working tree; a deletion or an unrelated change selects nothing).
"""

import os
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("check-citation-existence.py")
ZERO = "0" * 40

CLEAN_RUNBOOK = """\
# Portal runbook

The projection lives in `control/lib/thing.ex`'s `health/4`.
"""


class CitationExistenceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="citation-existence-")
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.repo = self.root / "repository"
        self.repo.mkdir()
        self.git("init", "--quiet")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.write("docs/runbooks/portal.md", CLEAN_RUNBOOK)
        self.write("docs/design/quality.md", "# Quality\n")
        self.write("docs/archive/CLOSED-TODO.md", "# CLOSED TODO\n")
        self.write("docs/TODO.md", "## Rules\n")
        self.write("control/lib/thing.ex", "defmodule Thing do\n  def health(_), do: :ok\nend\n")
        self.write("control/lib/legacy.ex", "defmodule Legacy do\n  def unrelated, do: :ok\nend\n")
        self.write("daemon/main.go", "package main\n")
        self.git("add", "-A")
        self.git("commit", "--quiet", "-m", "fixture base")
        self.base = self.git_text("rev-parse", "HEAD")

    # -- helpers -------------------------------------------------------------

    def git(self, *arguments: str, input: bytes | None = None) -> bytes:
        return subprocess.check_output(
            ["git", "-C", str(self.repo), *arguments], input=input, stderr=subprocess.PIPE
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

    def cite_in_runbook(self, citation: str, message: str = "cite") -> str:
        self.write("docs/runbooks/portal.md", f"# Portal runbook\n\nSee {citation} for the shape.\n")
        return self.commit(message)

    # -- tests ---------------------------------------------------------------

    def test_a_cited_path_the_head_does_not_hold_is_refused(self) -> None:
        head = self.cite_in_runbook("`docs/runbooks/moved.md:12`", "cite a moved path")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("docs/runbooks/portal.md:3", result.stderr)
        self.assertIn("moved.md", result.stderr)
        self.assertIn("no ignore rule covers it", result.stderr)

    def test_a_line_suffix_is_stripped_and_never_validated(self) -> None:
        # The cited line does not exist in the file and the gate must not read it: any
        # edit above a cited line would otherwise redden the gate.
        head = self.cite_in_runbook("`control/lib/thing.ex:99999`", "cite past the end")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 changed document(s) pass the path and symbol check", result.stdout)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_a_citation_the_ignore_rules_cover_is_not_refused(self) -> None:
        # The staged portal bundle is the case the runbooks document: it is generated
        # output the repository's own ignore rule declares, not a moved source file.
        self.write(".gitignore", "/control/priv/static/portal/\n")
        self.write("docs/runbooks/portal.md", "# Portal runbook\n")
        self.commit("state the generated bundle rule")
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n\nThe build stages hashed assets in "
            "`control/priv/static/portal/` and `control/priv/static/portal/index.html`.\n",
        )
        head = self.commit("document the generated bundle")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("2 path citation(s)", result.stdout)

    def test_a_wildcard_anchor_that_is_not_a_tree_entry_is_refused(self) -> None:
        head = self.cite_in_runbook("`docs/runbooks/gone/**.md`", "cite an absent glob anchor")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("docs/runbooks/gone", result.stderr)

    def test_an_extensionless_token_is_not_a_path_citation(self) -> None:
        # This corpus writes branch names in backticks; a branch name and a directory
        # citation are the same shape, so the gate leaves that ambiguity to review.
        head = self.cite_in_runbook("`docs/close-linux-platform-flakes`", "cite a branch")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)

    def test_a_command_and_a_url_are_not_path_citations(self) -> None:
        head = self.cite_in_runbook(
            "`python3 scripts/ci/absent.py --flag` and `https://example.invalid/docs/x.md`",
            "cite a command and a URL",
        )
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)

    def test_a_bound_symbol_absent_from_the_cited_file_is_refused(self) -> None:
        head = self.cite_in_runbook(
            "`snapshot_progress/2` in `control/lib/thing.ex`", "cite an absent symbol"
        )
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("snapshot_progress", result.stderr)
        self.assertIn("control/lib/thing.ex", result.stderr)

    def test_a_bound_symbol_the_cited_file_carries_passes(self) -> None:
        head = self.cite_in_runbook("`health/4` of `control/lib/thing.ex`", "cite a real symbol")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("pass the path and symbol check", result.stdout)

    def test_a_symbol_the_prose_does_not_bind_is_not_checked(self) -> None:
        # The connective list is closed and the sentence break is respected: prose the
        # gate cannot parse as a binding must never demand a rewrite.
        head = self.cite_in_runbook(
            "`health/4` was moved out of `control/lib/legacy.ex`", "cite across a sentence"
        )
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_a_symbol_bound_to_a_directory_citation_is_not_checked(self) -> None:
        head = self.cite_in_runbook("`health/4` in `control/lib/`", "cite a directory")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_an_unchanged_document_is_not_audited(self) -> None:
        # The stale citation lands in the base revision and no audited document changes:
        # the reading of an older document is its own review.
        self.write("docs/runbooks/portal.md", "# Portal runbook\n\nSee `docs/runbooks/moved.md`.\n")
        self.base = self.commit("land a stale citation in the base")
        self.write("daemon/main.go", "package main\n\nfunc main() {}\n")
        head = self.commit("change only unrelated code")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("nothing to check", result.stdout)

    def test_a_non_corpus_document_is_not_audited(self) -> None:
        self.write("docs/TODO.md", "## Rules\n\nSee `docs/runbooks/moved.md`.\n")
        head = self.commit("cite from an unaudited document")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("nothing to check", result.stdout)

    def test_a_deleted_document_is_not_audited(self) -> None:
        (self.repo / "docs/runbooks/portal.md").unlink()
        head = self.commit("delete the runbook")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("nothing to check", result.stdout)

    def test_the_head_blob_decides_and_not_the_working_tree(self) -> None:
        # The stale citation is committed and the worktree is then cleaned: the gate
        # still reads the revision under test, never the file on disk.
        head = self.cite_in_runbook("`docs/runbooks/moved.md`", "commit a stale citation")
        self.write("docs/runbooks/portal.md", CLEAN_RUNBOOK)
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("moved.md", result.stderr)

    def test_a_document_committed_as_a_symlink_is_refused(self) -> None:
        # A symlink's stored bytes are its target text, so the gate cannot read the
        # document it was asked to audit and must say so instead of passing.
        target = self.root / "clean-target.md"
        target.write_text(CLEAN_RUNBOOK, encoding="utf-8")
        blob = self.git("hash-object", "-w", "--stdin", input=str(target).encode("utf-8"))
        self.git("update-index", "--add", "--cacheinfo",
                 f"120000,{blob.decode('ascii').strip()},docs/runbooks/portal.md")
        tree = self.git_text("write-tree")
        head = self.git_text("commit-tree", tree, "-p", self.base, "-m", "commit the runbook as a link")
        self.git("update-ref", "HEAD", head)
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("portal.md", result.stderr)
        self.assertIn("not regular file blobs", result.stderr)

    def test_an_unusable_range_refuses_with_the_unusable_revision_code(self) -> None:
        # A git diff that cannot run is an unusable revision range, not a citation
        # failure, and the gate inherits the shared arm's exit code.
        head = self.cite_in_runbook("`docs/runbooks/moved.md`", "cite a moved path")
        environment = dict(
            os.environ,
            GIT_CONFIG_COUNT="1",
            GIT_CONFIG_KEY_0="diff.orderFile",
            GIT_CONFIG_VALUE_0=str(self.root / "missing-order-file"),
        )
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--repo", str(self.repo), "--base", self.base, "--head", head],
            capture_output=True, text=True, check=False, env=environment,
        )
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("cannot diff", result.stderr)

    def test_an_unresolvable_head_refuses(self) -> None:
        result = self.run_gate("--base", self.base, "--head", "no-such-revision")
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("cannot resolve the head", result.stderr)

    def test_a_repository_without_a_base_refuses(self) -> None:
        orphan = self.root / "orphan-repository"
        orphan.mkdir()
        subprocess.check_call(["git", "init", "--quiet"], cwd=str(orphan))
        subprocess.check_call(["git", "config", "user.name", "Fixture"], cwd=str(orphan))
        subprocess.check_call(["git", "config", "user.email", "fixture@example.invalid"], cwd=str(orphan))
        (orphan / "docs" / "runbooks").mkdir(parents=True)
        (orphan / "docs" / "runbooks" / "portal.md").write_text(CLEAN_RUNBOOK, encoding="utf-8")
        subprocess.check_call(["git", "add", "-A"], cwd=str(orphan))
        subprocess.check_call(["git", "commit", "--quiet", "-m", "only commit"], cwd=str(orphan))
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--repo", str(orphan), "--base", ZERO],
            capture_output=True, text=True, check=False,
        )
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("cannot resolve a comparison base", result.stderr)


if __name__ == "__main__":
    sys.exit(unittest.main())
