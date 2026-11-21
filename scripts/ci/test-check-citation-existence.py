#!/usr/bin/env python3
"""Exercise the document-citation existence gate through real Git repositories.

The fixture repositories here are throwaway trees: the gate is a selector over a
revision range plus a citation grammar, so the tests pin what the grammar audits (a
line-suffixed path, a file-shaped path, a directory citation, a wildcard family, a
fenced block's exclusion), what it deliberately does not (an extensionless multi-segment
token, a command, a URL), the resolution boundary (the head tree, or the head revision's
own ignore rules for generated output, with a per-citation notice), the symbol binding
and its whole-word occurrence test, and the range side the gate shares with
`check-record-cleanliness.py` (a changed file's head blob, never the working tree; a
deletion or an unrelated change selects nothing).
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
        self.write("scripts/ci/test-a.py", "# Stand-in bytes: a member of the fixture family.\n")
        self.write("README.md", "# Fixture\n")
        self.write("daemon/main.go", "package main\n")
        self.git("add", "-A")
        self.git("commit", "--quiet", "-m", "fixture base")
        self.base = self.git_text("rev-parse", "HEAD")

    # -- helpers -------------------------------------------------------------

    def git(self, *arguments: str, input: bytes | None = None) -> bytes:
        return subprocess.check_output(["git", "-C", str(self.repo), *arguments], input=input, stderr=subprocess.PIPE)

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
            capture_output=True,
            text=True,
            check=False,
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

    def test_a_glob_beneath_a_missing_directory_is_refused_for_its_members(self) -> None:
        # The anchor's deepest existing ancestor (`docs/runbooks`) makes the token a
        # citation; the member test then refuses it because no tree entry matches the
        # pattern - the anchor itself is not required to be a tree entry.
        head = self.cite_in_runbook("`docs/runbooks/gone/**.md`", "cite a glob under a missing directory")
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
        head = self.cite_in_runbook("`snapshot_progress/2` in `control/lib/thing.ex`", "cite an absent symbol")
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
        head = self.cite_in_runbook("`health/4` was moved out of `control/lib/legacy.ex`", "cite across a sentence")
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
        self.git(
            "update-index", "--add", "--cacheinfo", f"120000,{blob.decode('ascii').strip()},docs/runbooks/portal.md"
        )
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
            capture_output=True,
            text=True,
            check=False,
            env=environment,
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
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("cannot resolve a comparison base", result.stderr)

    # -- the review-round pins -------------------------------------------------

    def test_a_bound_symbol_on_a_missing_path_reports_only_the_path_refusal(self) -> None:
        # The path arm owns a missing path; the symbol arm must not read the absent
        # file's blob, turn the run into an "unusable revision" exit and drop the
        # refusal the path arm already collected.
        head = self.cite_in_runbook("`health/4` in `control/lib/moved.ex`", "bind a symbol to a moved path")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("control/lib/moved.ex", result.stderr)
        self.assertIn("no ignore rule covers it", result.stderr)
        self.assertNotIn("does not hold exactly one entry", result.stderr)

    def test_a_bound_symbol_on_an_ignored_path_is_not_refused(self) -> None:
        # A generated bundle's path is legal prose and stores no committed blob: the
        # symbol binding must not crash the run, and the skip is reported as a notice
        # naming the ignore rule rather than passing silently.
        self.write(".gitignore", "/control/priv/static/portal/\n")
        head = self.cite_in_runbook(
            "`render()` in `control/priv/static/portal/index.html`",
            "bind a symbol to a generated path",
        )
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("the ignore rule .gitignore:1:/control/priv/static/portal/", result.stdout)
        self.assertIn("control/priv/static/portal/index.html", result.stdout)
        self.assertNotIn("does not hold exactly one entry", result.stderr)

    def test_a_wildcard_inside_a_name_matches_the_family(self) -> None:
        # A family glob is a citation and it holds while the tree holds a member: the
        # first author to write `scripts/ci/test-*.py` must not be reddened by the
        # wildcard sitting inside the file-name segment.
        head = self.cite_in_runbook("`scripts/ci/test-*.py`", "cite a file family")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_a_glob_of_an_existing_directorys_members_passes(self) -> None:
        head = self.cite_in_runbook("`docs/runbooks/*.md`", "cite the runbook directory's members")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_a_directory_family_glob_matches_directory_entries(self) -> None:
        # `docs/runbooks/*/` names the directory members: tree paths carry no trailing
        # slash, so the gate drops it and matches directory entries - while a family
        # that has vanished is still refused.
        self.write("docs/runbooks/sub/portal.md", "# Sub\n")
        head = self.cite_in_runbook("`docs/runbooks/*/`", "cite a directory family")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)
        head = self.cite_in_runbook("`docs/runbooks/gone-*/`", "cite a vanished directory family")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("the cited glob `docs/runbooks/gone-*/` matches no entry", result.stderr)

    def test_a_double_star_glob_spans_path_segments(self) -> None:
        # `**` must span path segments: this pattern needs two of them (`runbooks`,
        # `nested`) and no shallower entry satisfies it, so a `**` degraded to a
        # single-segment wildcard cannot pass the fixture.
        self.write("docs/runbooks/nested/deep/portal.md", "# Deep\n")
        head = self.cite_in_runbook("`docs/**/deep/portal.md`", "cite across segments")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_an_ellipsis_truncated_name_matches_the_family(self) -> None:
        # `...` and `…` both mean "and so on": both are wildcards inside the name.
        head = self.cite_in_runbook("`docs/runbooks/port...` and `docs/runbooks/port…`", "cite truncated names")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("2 path citation(s)", result.stdout)

    def test_a_glob_whose_family_has_disappeared_is_refused(self) -> None:
        # The directory standing is not enough: the citation names members, and a
        # family that has vanished entirely is the rot this gate exists for.
        head = self.cite_in_runbook("`docs/runbooks/gone-*.md`", "cite a vanished family")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("gone-*.md", result.stderr)
        self.assertIn("no ignore rule covers it", result.stderr)

    def test_a_line_and_column_suffix_is_stripped_and_never_validated(self) -> None:
        # `file:line:col` is what compilers, test runners and linters print; only the
        # suffix is stripped, never the path, and never the numbers themselves.
        head = self.cite_in_runbook("`control/lib/thing.ex:12:5`", "cite a line and column")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)
        head = self.cite_in_runbook("`control/lib/moved.ex:12:5`", "cite a moved file's column")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("control/lib/moved.ex", result.stderr)
        self.assertNotIn("moved.ex:12", result.stderr)

    def test_a_token_inside_a_fenced_block_is_not_a_citation(self) -> None:
        # JS template literals, Go raw strings and shell examples inside fences write
        # the same backticks prose does; the example code is not a citation, and the
        # prose around the fence still is.
        self.write(
            "docs/runbooks/portal.md", "# Portal runbook\n\n```js\nconst gone = `docs/runbooks/moved.md`;\n```\n"
        )
        head = self.commit("show example code")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n"
            "\n"
            "```js\n"
            "const gone = `docs/runbooks/moved.md`;\n"
            "```\n"
            "\n"
            "The projection lives in `control/lib/thing.ex`.\n",
        )
        head = self.commit("show code beside prose")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_a_token_inside_a_nested_list_fence_is_not_a_citation(self) -> None:
        # A fence nested in a list item sits at the item's content column (four or
        # deeper): CommonMark reads it as a fenced block, so the example code is
        # blanked like any other fence instead of being audited as prose.
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n"
            "\n"
            "- outer step\n"
            "\n"
            "  - inner step\n"
            "\n"
            "    ```js\n"
            "    const gone = `scripts/ci/gone.py`;\n"
            "    ```\n",
        )
        head = self.commit("show a nested example")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)

    def test_an_info_string_does_not_close_a_fence(self) -> None:
        # A closing fence carries nothing after it, so the ```js line inside this fence
        # is content: reading it as a closer would audit the example code below it.
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n\n```md\n```js\nconst gone = `docs/runbooks/moved.md`;\n```\n",
        )
        head = self.commit("show code inside a fence")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)

    def test_a_token_inside_a_blockquoted_fence_is_not_a_citation(self) -> None:
        # A fence inside a blockquote is a fenced block too; the quoted example code
        # must not be audited as prose.
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n\n> ```js\n> const gone = `docs/runbooks/moved.md`;\n> ```\n",
        )
        head = self.commit("show a quoted example")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)

    def test_a_backtick_fence_info_string_may_not_contain_a_backtick(self) -> None:
        # CommonMark: the info string of a backtick fence may not contain a backtick, so
        # a line that begins with an inline triple-backtick span is prose, not an
        # opener. An opener there would run unclosed to the end of the document and
        # silently skip the absent citation below it - a false negative, never a red.
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n\n```code``` is how a span is written.\n\nSee `docs/runbooks/moved.md`.\n",
        )
        head = self.commit("write a prose line that starts with a span")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("docs/runbooks/portal.md:5", result.stderr)
        self.assertIn("moved.md", result.stderr)
        # The rule is the backtick fence's own: a tilde fence may carry backticks in
        # its info string, and the block's content stays blanked.
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n\n~~~ `docs/runbooks/moved.md`\nexample\n~~~\n",
        )
        head = self.commit("quote a path in a tilde fence's info string")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)

    def test_a_coordinating_conjunction_does_not_bind_a_path_and_symbol(self) -> None:
        # `and` joins list items: it must not demand that a symbol belonging to some
        # other file occur in the file the list happens to mention first.
        head = self.cite_in_runbook("`control/lib/thing.ex` and `snapshot_progress/2`", "list beside a symbol")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_a_symbol_after_the_path_it_binds_to_is_checked(self) -> None:
        head = self.cite_in_runbook("`control/lib/thing.ex` calls `snapshot_progress/2`", "bind path-first")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("snapshot_progress", result.stderr)

    def test_a_symbol_that_is_only_a_substring_is_not_an_occurrence(self) -> None:
        # thing.ex carries `health/4`; `heal/1` must not match inside it.
        head = self.cite_in_runbook("`heal/1` in `control/lib/thing.ex`", "cite a substring")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("does not occur", result.stderr)

    def test_a_leading_dot_slash_is_stripped(self) -> None:
        head = self.cite_in_runbook("`./control/lib/thing.ex`", "cite with a leading ./")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_a_top_level_single_segment_entry_is_checked(self) -> None:
        head = self.cite_in_runbook("`README.md`", "cite a top-level file")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)

    def test_a_bare_file_name_that_is_not_a_top_level_entry_is_not_a_citation(self) -> None:
        # A `read_model.ex`-style basename names no repo-relative path: nothing to
        # resolve, and the middle-segment citations are where that file is checked.
        head = self.cite_in_runbook("`read_model.ex`", "cite a bare file name")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)

    def test_a_path_whose_intermediate_directories_are_gone_is_refused(self) -> None:
        # `docs/runbooks` stands and `moved/sub` does not: the deepest existing ancestor
        # is a tree directory, so the token is a citation and the refusal names it.
        head = self.cite_in_runbook("`docs/runbooks/moved/sub/thing.md`", "cite a deep missing path")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("docs/runbooks/moved/sub/thing.md", result.stderr)

    def test_a_tree_the_range_removed_is_refused_for_paths_and_directories(self) -> None:
        # A wholesale removal takes the first segment with it, and the ancestor walk
        # cannot see a segment that is gone: only the head's own history can say the
        # token ever named a repo path. It did, and both shapes are refused - the
        # directory refusal by its own text, not by the file refusal's mention of the
        # tree.
        self.write("browser/tests/portal.spec.mjs", "// fixture\n")
        self.base = self.commit("fixture a browser tree")
        self.git("rm", "-r", "--quiet", "browser")
        head = self.cite_in_runbook("`browser/tests/portal.spec.mjs:12` and `browser/`", "cite a removed tree")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("the cited path `browser/tests/portal.spec.mjs` is in no entry", result.stderr)
        self.assertIn("the cited path `browser` is in no entry", result.stderr)
        self.assertIn("no ignore rule covers it", result.stderr)

    def test_a_tree_removed_before_the_range_is_still_refused(self) -> None:
        # The realistic next-PR shape: the removal merged in an earlier pull request
        # that touched no audited document, so base and head both lack the first
        # segment; the history arm still knows the tree, counts both citations and
        # refuses them instead of letting the stale paths drop out of the count.
        self.write("browser/tests/portal.spec.mjs", "// fixture\n")
        self.commit("fixture a browser tree")
        self.git("rm", "-r", "--quiet", "browser")
        removal = self.commit("remove the browser tree")
        head = self.cite_in_runbook("`browser/tests/portal.spec.mjs:12` and `browser/`", "cite a tree removed earlier")
        result = self.run_gate("--base", removal, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("the cited path `browser/tests/portal.spec.mjs` is in no entry", result.stderr)
        self.assertIn("the cited path `browser` is in no entry", result.stderr)
        self.assertIn("no ignore rule covers it", result.stderr)

    def test_an_ignore_rule_decides_before_a_removal_is_reported(self) -> None:
        # A removed tree the head revision's own ignore rules also cover: the ignore
        # arm is consulted first, so the run passes with the per-citation notice naming
        # the rule instead of the removal refusal. The precedence is deliberate and
        # pinned here, not chosen silently.
        self.write("browser/tests/portal.spec.mjs", "// fixture\n")
        self.base = self.commit("fixture a browser tree")
        self.git("rm", "-r", "--quiet", "browser")
        self.write(".gitignore", "/browser/\n")
        head = self.cite_in_runbook("`browser/tests/portal.spec.mjs:12`", "cite a removed, ignored tree")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("the ignore rule .gitignore:1:/browser/", result.stdout)
        self.assertIn("browser/tests/portal.spec.mjs", result.stdout)

    def test_a_first_segment_neither_revision_held_is_not_a_citation(self) -> None:
        # A subdirectory-relative or upstream path (PostgreSQL's `src/backend/...`)
        # names no repo-relative path: guessing it would demand rewrites of accurate
        # prose, so it is left to review.
        head = self.cite_in_runbook("`src/backend/tcop/postgres.c:1680`", "cite an upstream path")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0 path citation(s)", result.stdout)

    def test_the_head_revisions_ignore_rules_decide_not_the_working_tree(self) -> None:
        # The rule is committed with the head revision and then removed from the
        # worktree: the checkout is no longer at the revision under test, and the gate
        # must still answer from the head revision's own bytes.
        self.write(".gitignore", "/control/priv/static/portal/\n")
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n\nThe build stages hashed assets in `control/priv/static/portal/index.html`.\n",
        )
        head = self.commit("document the generated bundle")
        (self.repo / ".gitignore").unlink()
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 path citation(s)", result.stdout)
        self.assertIn("the ignore rule .gitignore:1:/control/priv/static/portal/", result.stdout)

    def test_an_ignore_rule_is_read_from_case_exact_bytes(self) -> None:
        # The head tree holds `control/gen/.gitignore`; a citation of `control/Gen/...`
        # must not be exempted because a case-insensitive filesystem resolved the
        # directory to the other spelling - the notice is only accepted when the rule's
        # source is one of the committed paths, so both platforms refuse the wrong
        # case. The exactly-cased citation is exempt with the notice.
        self.write("control/gen/.gitignore", "/absent.md\n")
        self.commit("state a generated-output rule")
        head = self.cite_in_runbook("`control/Gen/absent.md`", "cite the wrong case")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("control/Gen/absent.md", result.stderr)
        self.assertIn("no ignore rule covers it", result.stderr)
        head = self.cite_in_runbook("`control/gen/absent.md`", "cite the exact case")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("the ignore rule control/gen/.gitignore:1:/absent.md", result.stdout)
        self.assertIn("control/gen/absent.md", result.stdout)

    def test_a_rule_the_working_tree_alone_holds_does_not_cover_a_citation(self) -> None:
        # An untracked ignore file is not the revision under test: it must not make a
        # citation pass.
        self.write(".gitignore", "/control/generated/\n")
        self.write(
            "docs/runbooks/portal.md",
            "# Portal runbook\n\nThe build stages hashed assets in `control/generated/index.html`.\n",
        )
        self.git("add", "docs/runbooks/portal.md")
        self.git("commit", "--quiet", "-m", "cite an untracked generated path")
        head = self.git_text("rev-parse", "HEAD")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("control/generated/index.html", result.stderr)
        self.assertIn("no ignore rule covers it", result.stderr)

    def test_a_file_cited_with_a_trailing_slash_is_refused_as_a_file(self) -> None:
        # The head tree holds the blob, so "is in no entry" would be false: the
        # directory shape's refusal says what the path is - a file, not a directory.
        head = self.cite_in_runbook("`docs/runbooks/portal.md/`", "cite a file as a directory")
        result = self.run_gate("--base", self.base, "--head", head)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(
            "the cited path `docs/runbooks/portal.md` names a file, not a directory",
            result.stderr,
        )
        self.assertNotIn("the cited path `docs/runbooks/portal.md` is in no entry", result.stderr)

    def test_a_rollback_range_is_compared_with_its_requested_base(self) -> None:
        # The requested base is a descendant of the head (a rollback pushes an older
        # revision): always narrowing to the merge base would collapse the range to the
        # head itself and report "nothing to check", so a requested base is compared
        # directly - the record gate's mirror test pins the same arm.
        self.write("docs/runbooks/portal.md", "# Portal runbook\n\nSee `docs/runbooks/moved.md`.\n")
        dirty = self.commit("land a stale citation")
        self.write("docs/runbooks/portal.md", CLEAN_RUNBOOK)
        clean = self.commit("repair the citation")
        result = self.run_gate("--base", clean, "--head", dirty)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("moved.md", result.stderr)
        self.assertNotIn("nothing to check", result.stdout)

    def test_an_inherited_git_dir_does_not_redirect_the_audit(self) -> None:
        # A hook or wrapper may leave GIT_DIR set. Without stripping it, `-C <repo>`
        # stops deciding: the gate would read the other repository's HEAD^..HEAD -
        # which audits no document here - and pass vacuously instead of refusing the
        # stale citation this repository's range holds.
        other = self.root / "other-repository"
        subprocess.check_call(["git", "clone", "--quiet", str(self.repo), str(other)])
        subprocess.check_call(["git", "config", "user.name", "Fixture"], cwd=str(other))
        subprocess.check_call(["git", "config", "user.email", "fixture@example.invalid"], cwd=str(other))
        (other / "daemon").mkdir(exist_ok=True)
        (other / "daemon" / "main.go").write_text("package main\n\nfunc main() {}\n", encoding="utf-8", newline="\n")
        subprocess.check_call(["git", "add", "-A"], cwd=str(other))
        subprocess.check_call(["git", "commit", "--quiet", "-m", "change only unrelated code"], cwd=str(other))
        self.cite_in_runbook("`docs/runbooks/moved.md`", "cite a moved path")
        environment = dict(os.environ, GIT_DIR=str(other / ".git"))
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--repo", str(self.repo), "--base", "HEAD^", "--head", "HEAD"],
            capture_output=True,
            text=True,
            check=False,
            env=environment,
        )
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("moved.md", result.stderr)
        self.assertNotIn("nothing to check", result.stdout)


if __name__ == "__main__":
    sys.exit(unittest.main())
