#!/usr/bin/env python3
"""Exercise the model-routing source-subject tool on a synthetic tree.

The properties under test are the ones a record depends on: which paths enter the
binding, that the digest carries no host-specific input, and that a change inside a
declared exclusion cannot move it while a change to bound bytes always does.
"""

import ast
import hashlib
import os
import pathlib
import shutil
import tempfile
import unittest

import citest

SCRIPT = pathlib.Path(__file__).with_name("model-routing-source-subject.py")
RUNNER = SCRIPT.with_name("model-routing-evaluation.py")


def load_tool():
    return citest.load_gate(SCRIPT, "model_routing_source_subject")


def runner_module_closure() -> set[str]:
    """Every `model_routing_*` module the evaluation entry imports, walked transitively."""
    pending = [RUNNER]
    collected: set[str] = set()
    while pending:
        for node in ast.walk(ast.parse(pending.pop().read_text(encoding="utf-8"))):
            if isinstance(node, ast.ImportFrom) and node.module and node.module.startswith("model_routing_"):
                names = [node.module]
            elif isinstance(node, ast.Import):
                names = [alias.name for alias in node.names if alias.name.startswith("model_routing_")]
            else:
                continue
            for name in names:
                if name not in collected:
                    collected.add(name)
                    pending.append(RUNNER.with_name(f"{name}.py"))
    return collected


class SourceSubjectTests(unittest.TestCase):
    def setUp(self) -> None:
        self._temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self._temporary.cleanup)
        self.root = pathlib.Path(self._temporary.name) / "tree"
        self.root.mkdir()
        self.write("frontend/src/app.ts", "export const app = 1;\n")

    def write(self, relative: str, text: str) -> pathlib.Path:
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(text.encode("utf-8"))
        return path

    def test_declared_scope_selects_only_source_paths(self) -> None:
        tool = load_tool()
        self.write("scripts/ci/tool.py", "print('x')\n")
        self.write("frontend/.vite/deps/x.js", "run state outside node_modules\n")
        self.write("docs/evidence/record.md", "docs are not source\n")
        self.write(".git/hooks/pre-commit", "#!/bin/sh\n")
        self.write("__pycache__/tool.cpython-312.pyc", "bytecode\n")
        self.write("node_modules/react/index.js", "provisioned dependency\n")
        self.write(".tmp/scratch.txt", "scratch\n")
        self.write("frontend/dist/bundle.js", "build output\n")
        self.write("playwright-report/index.html", "report\n")
        self.write("test-results/x.json", "results\n")

        self.assertEqual(
            tool.source_paths(self.root),
            ["frontend/src/app.ts", "scripts/ci/tool.py"],
        )

    def test_git_directory_and_worktree_pointer_file_are_both_excluded(self) -> None:
        tool = load_tool()
        self.write(".git/hooks/pre-commit", "#!/bin/sh\n")
        self.assertEqual(tool.source_paths(self.root), ["frontend/src/app.ts"])
        shutil.rmtree(self.root / ".git")
        self.write(".git", "gitdir: elsewhere\n")
        self.assertEqual(tool.source_paths(self.root), ["frontend/src/app.ts"])

    def test_entry_carries_mode_digest_and_path(self) -> None:
        tool = load_tool()
        blob = hashlib.sha256(b"export const app = 1;\n").hexdigest()
        self.assertEqual(
            tool.entry_bytes(self.root, "frontend/src/app.ts"),
            b"100644\0sha256:" + blob.encode("ascii") + b"\0frontend/src/app.ts\0",
        )

    def test_executable_bit_selects_the_other_mode(self) -> None:
        tool = load_tool()
        path = self.write("scripts/run.sh", "#!/bin/sh\n")
        os.chmod(path, 0o755)
        if not path.stat().st_mode & 0o111:
            self.skipTest("this platform does not report an execute bit")
        self.assertEqual(tool.entry_bytes(self.root, "scripts/run.sh").split(b"\0", 1)[0], b"100755")
        self.assertEqual(tool.executable_paths(self.root), ["scripts/run.sh"])

    def test_digest_ignores_the_tree_location_and_the_exclusions(self) -> None:
        tool = load_tool()
        self.write("docs/evidence/record.md", "first\n")
        self.write("node_modules/.vite-temp/x.mjs", "first\n")
        self.write("__pycache__/x.pyc", "first\n")
        before = tool.source_subject(self.root)

        # A copy of the same bytes somewhere else must hash identically: no absolute path
        # may enter the binding.
        elsewhere = pathlib.Path(self._temporary.name) / "copy" / "deeper"
        shutil.copytree(self.root, elsewhere)
        self.assertEqual(tool.source_subject(elsewhere), before)

        # Excluded paths are outside the binding, so changing them must not move it.
        self.write("docs/evidence/record.md", "second\n")
        self.write("node_modules/.vite-temp/x.mjs", "second\n")
        self.write("node_modules/react/index.js", "a new dependency\n")
        self.write("__pycache__/x.pyc", "second\n")
        self.write("frontend/dist/bundle.js", "new build output\n")
        self.assertEqual(tool.source_subject(self.root), before)

        # Bound bytes are inside it, so changing one always must.
        self.write("frontend/src/app.ts", "export const app = 2;\n")
        self.assertEqual(tool.source_subject(self.root)[0], before[0])
        self.assertNotEqual(tool.source_subject(self.root)[1], before[1])

    def test_added_and_removed_source_paths_move_the_digest(self) -> None:
        tool = load_tool()
        before = tool.source_subject(self.root)
        added = self.write("scripts/ci/new-tool.py", "print('new')\n")
        after = tool.source_subject(self.root)
        self.assertEqual(after[0], before[0] + 1)
        self.assertNotEqual(after[1], before[1])
        added.unlink()
        self.assertEqual(tool.source_subject(self.root), before)

    def test_the_shared_module_is_inside_the_declared_scope(self) -> None:
        tool = load_tool()
        module = self.write("scripts/ci/source_subject.py", "# shared rules\n")
        self.assertIn("scripts/ci/source_subject.py", tool.source_paths(self.root))
        before = tool.source_subject(self.root)
        module.write_bytes(b"# changed shared rules\n")
        after = tool.source_subject(self.root)
        self.assertEqual(after[0], before[0])
        self.assertNotEqual(after[1], before[1])

    def test_delivered_tool_paths_cover_the_whole_evaluation_tool_set(self) -> None:
        tool = load_tool()
        self.assertEqual(
            tool.DELIVERED_TOOL_PATHS,
            (
                "scripts/ci/model-routing-evaluation.py",
                "scripts/ci/model_routing_gates.py",
                "scripts/ci/model_routing_receipts.py",
                "scripts/ci/model_routing_records.py",
                "scripts/ci/model_routing_subject.py",
                "scripts/ci/test-model-routing-evaluation.py",
            ),
        )
        imported = {f"scripts/ci/{name}.py" for name in runner_module_closure()}
        self.assertLessEqual(imported, set(tool.DELIVERED_TOOL_PATHS), imported - set(tool.DELIVERED_TOOL_PATHS))
        root = SCRIPT.resolve().parents[2]
        for relative in tool.DELIVERED_TOOL_PATHS:
            self.assertTrue((root / relative).is_file(), relative)


if __name__ == "__main__":
    unittest.main()
