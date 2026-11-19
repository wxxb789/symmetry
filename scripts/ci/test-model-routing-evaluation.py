#!/usr/bin/env python3
"""Exercise the evaluation CLI through real Git, subprocess and file surfaces."""

import ast
import hashlib
import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import time
import unittest


SCRIPT = pathlib.Path(__file__).with_name("model-routing-evaluation.py")


def load_runner_module():
    """Import the acceptance runner so gate validation rules can be asserted directly."""
    path = pathlib.Path(__file__).with_name("model-routing-evaluation.py")
    spec = importlib.util.spec_from_file_location("model_routing_evaluation", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def refusal_returns(source_text, function):
    """Parse every refusal `return` of one runner function out of the module's syntax tree.

    The inventory is the function's own `ast`, never a text match: each row is
    `(line, kind, source_segment)` for a `return` whose value is not `None`, where
    `kind` is `literal` for a plain string and `template` for an f-string. A record
    that quotes "N refusal shapes" is quoting this set, so the count follows from the
    artifact instead of from a list written by hand.
    """
    tree = ast.parse(source_text)
    for node in ast.walk(tree):
        if isinstance(node, ast.FunctionDef) and node.name == function:
            rows = []
            for statement in ast.walk(node):
                if not isinstance(statement, ast.Return) or statement.value is None:
                    continue
                value = statement.value
                if isinstance(value, ast.Constant) and value.value is None:
                    continue
                kind = (
                    "literal" if isinstance(value, ast.Constant) and isinstance(value.value, str)
                    else "template" if isinstance(value, ast.JoinedStr)
                    else "other"
                )
                rows.append((statement.lineno, kind, ast.get_source_segment(source_text, value)))
            return sorted(rows)
    raise AssertionError(f"the runner no longer defines {function}")


def exercise(function, call):
    """Call `function` and report the lines of its own body that ran.

    Returns `(result, executed_lines)`. Coverage is measured by execution, so a branch
    counts as reached because it ran, not because its text appears somewhere.
    """
    executed = []
    code = function.__code__

    def tracer(frame, event, _argument):
        if event == "line" and frame.f_code is code:
            executed.append(frame.f_lineno)
        return tracer

    previous = sys.gettrace()
    sys.settrace(tracer)
    try:
        result = call()
    finally:
        sys.settrace(previous)
    return result, executed


EMIT_OK = "print('gate ok')"
EMIT_PASS = (
    "import json\n"
    "for event in (\n"
    "    {'Action': 'run', 'Package': 'example/app', 'Test': 'TestWitness'},\n"
    "    {'Action': 'pass', 'Package': 'example/app', 'Test': 'TestWitness'},\n"
    "):\n"
    "    print(json.dumps(event))\n"
)
EMIT_RUN_ONLY = (
    "import json\n"
    "print(json.dumps({'Action': 'run', 'Package': 'example/app', 'Test': 'TestWitness'}))\n"
)
EMIT_SKIP = (
    "import json\n"
    "for event in (\n"
    "    {'Action': 'run', 'Package': 'example/app', 'Test': 'TestWitness'},\n"
    "    {'Action': 'pass', 'Package': 'example/app', 'Test': 'TestWitness'},\n"
    "    {'Action': 'skip', 'Package': 'example/app', 'Test': 'TestWitness'},\n"
    "):\n"
    "    print(json.dumps(event))\n"
)
EMIT_SKIPPED_DESCENDANT = (
    "import json\n"
    "for event in (\n"
    "    {'Action': 'run', 'Package': 'example/app', 'Test': 'TestWitness'},\n"
    "    {'Action': 'pass', 'Package': 'example/app', 'Test': 'TestWitness'},\n"
    "    {'Action': 'skip', 'Package': 'example/app', 'Test': 'TestWitness/sub'},\n"
    "):\n"
    "    print(json.dumps(event))\n"
)
EXIT_THREE = EMIT_PASS + "raise SystemExit(3)\n"
WRITE_DURING_GATE = "open('wrote_during_gate.txt', 'w').write('x')\n"

VITEST_NAMED_TESTS = (
    "renders one row per served boolean capability member",
    "never claims a capability the record does not carry",
)

FILES = {
    "app/main.go": "package app\n\nfunc Main() int { return 1 }\n",
    "app/main_test.go": "package app\n\nimport \"testing\"\n\nfunc TestBaseline(t *testing.T) {}\n",
    "app/app.go": "package app\n",
    "app/check_test.py": "def test_placeholder():\n    assert True\n",
    "web/thing.test.ts": "export const thing = 1;\n",
    "tests/sample.txt": "sample\n",
    "go.mod": "module example.com/fixture\n\ngo 1.21\n",
    ".github/workflows/ci.yml": "name: ci\n",
    "contracts/schema.json": "{}\n",
    "contracts/other.json": "{}\n",
    "AGENTS.md": "# Fixture instructions\n",
    "CLAUDE.md": "# Fixture instructions\n",
    ".agents/skills/demo/SKILL.md": "# Fixture skill\n",
    "docs/notes.md": "notes\n",
    "source.txt": "protected source\n",
    ".gitignore": "*.local.*\n",
}


class EvaluationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="model-routing-eval-")
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.repo = self.root / "repository"
        self.repo.mkdir()
        self.git("init", "--quiet")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "core.autocrlf", "false")
        for name, content in FILES.items():
            self.write(self.repo / name, content)
        self.git("add", "-A")
        self.git("commit", "--quiet", "-m", "fixture base")
        self.base = self.git_text("rev-parse", "HEAD")

    # -- environment helpers -------------------------------------------------

    def git(self, *arguments: str, cwd: pathlib.Path | None = None) -> bytes:
        return subprocess.check_output(
            ["git", "-C", str(cwd or self.repo), *arguments], stderr=subprocess.PIPE
        )

    def git_text(self, *arguments: str, cwd: pathlib.Path | None = None) -> str:
        return self.git(*arguments, cwd=cwd).decode("ascii").strip()

    def write(self, path: pathlib.Path, content: str) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content.encode("utf-8"))

    def write_json(self, path: pathlib.Path, value) -> pathlib.Path:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes((json.dumps(value, indent=2, sort_keys=True) + "\n").encode("utf-8"))
        return path

    def read(self, path: pathlib.Path) -> bytes:
        return path.read_bytes()

    def cli(self, *arguments: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, str(SCRIPT), *arguments],
            capture_output=True, text=True, cwd=str(self.root), check=False,
        )

    def assert_ok(self, result: subprocess.CompletedProcess):
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        return json.loads(result.stdout)

    def assert_refused(self, result: subprocess.CompletedProcess, needle: str | None = None):
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        combined = result.stderr + result.stdout
        self.assertIn("refused", combined)
        if needle is not None:
            self.assertIn(needle, combined, combined)

    # -- fixture builders ----------------------------------------------------

    def gate_events(self, gate_id="events", code=EMIT_PASS, tests=("TestWitness",), package="example/app"):
        gate = {"id": gate_id, "cwd": ".", "argv": [sys.executable, "-c", code]}
        if tests is not None:
            gate["go_tests"] = list(tests)
        if package is not None:
            gate["go_package"] = package
        return gate

    def gate_plain(self, gate_id="plain", code=EMIT_OK, cwd="."):
        return {"id": gate_id, "cwd": cwd, "argv": [sys.executable, "-c", code]}

    def task(self, *, gates=None, allowed_paths=None, protected_paths=None, **overrides):
        value = {
            "task_id": "fixture-task",
            "base": self.base,
            "language": "go",
            "risk_class": "bounded-fixture",
            "workflow": "reuse_existing",
            "outcome": "fixture outcome",
            "explanations": ["explain fixture one", "explain fixture two"],
            "allowed_paths": ["app/main.go", "app/new_test.go"] if allowed_paths is None else allowed_paths,
            "protected_paths": ["source.txt"] if protected_paths is None else protected_paths,
            "gates": [self.gate_events()] if gates is None else gates,
        }
        value.update(overrides)
        return value

    def routes(self, worker="worker-model", reviewer="reviewer-model"):
        def role(model):
            return {
                "model_id": model,
                "provenance": "fixture route preflight",
                "credential_route": "fixture-credential-route",
                "authorization_ref": "reuse_existing",
            }
        return {"worker": role(worker), "reviewer": role(reviewer)}

    def prepare(self, task=None, routes=None, run=None, source=None, environment_json=None) -> subprocess.CompletedProcess:
        inputs = self.root / "inputs"
        task_path = self.write_json(inputs / "task.json", self.task() if task is None else task)
        routes_path = self.write_json(inputs / "routes.json", self.routes() if routes is None else routes)
        command = [
            "prepare", "--run-dir", str(run or self.run_dir()), "--task", str(task_path),
            "--routes", str(routes_path), "--source", str(source or self.repo),
        ]
        if environment_json is not None:
            command += ["--environment-json", str(environment_json)]
        return self.cli(*command)

    def install_fake_mix(self, trace_lines=(), trace_exit=0, gate_exit=0):
        """Install an executable named `mix` that prints trace lines instead of running ExUnit.

        The runner only derives the ExUnit trace command for a gate whose argv[0] is
        named mix; the wrapper forwards to this interpreter so the fixture launches on
        POSIX and Windows alike. Non-trace invocations print a marker and exit with
        `gate_exit`, so a test can fail the gate command while the trace stays
        controllable.
        """
        directory = self.root / "fake-mix"
        directory.mkdir(exist_ok=True)
        body = directory / "mix_body.py"
        source = (
            "import sys\n"
            "if '--trace' in sys.argv:\n"
            + "".join(f"    print({line!r})\n" for line in trace_lines)
            + f"    raise SystemExit({trace_exit})\n"
            "print('mix test gate ran')\n"
            f"raise SystemExit({gate_exit})\n"
        )
        self.write(body, source)
        if os.name == "nt":
            wrapper = directory / "mix.bat"
            self.write(wrapper, f'@echo off\r\n"{sys.executable}" "{body}" %*\r\n')
        else:
            wrapper = directory / "mix"
            self.write(wrapper, f'#!/bin/sh\nexec "{sys.executable}" "{body}" "$@"\n')
            wrapper.chmod(0o755)
        probe = subprocess.run([str(wrapper), "test"], capture_output=True, text=True, check=False)
        if "mix test gate ran" not in probe.stdout:
            self.fail(f"the fake mix fixture did not launch: {probe.returncode} {probe.stdout} {probe.stderr}")
        return wrapper

    def exunit_gate(self, mix, *, gate_id="exunit-gate", tests, files=("test/witness_test.exs",), cwd=".", argv=None):
        gate = {
            "id": gate_id,
            "cwd": cwd,
            "argv": [str(mix), "test"] if argv is None else argv,
            "exunit_files": list(files),
            "exunit_tests": list(tests),
        }
        return gate

    def install_fake_vitest(self, report=None, report_exit=0, gate_exit=0, write_report=True):
        """Install a vitest entry point that writes a chosen JSON report instead of running tests.

        The runner derives the structured report command only for a gate whose argv
        names the vitest entry, so the fixture is a Node module exactly like the real
        entry point: the derived command launches it through the same interpreter and
        must find the report it wrote at `--outputFile`. A run without `--outputFile`
        is the gate command itself and only prints a marker, so a test can fail the
        gate command while keeping the derived report controllable.
        """
        directory = self.root / "fake-vitest"
        directory.mkdir(exist_ok=True)
        entry = directory / "vitest_entry.mjs"
        report_literal = "null" if report is None else json.dumps(report)
        source = (
            "import { writeFileSync } from 'node:fs';\n"
            "const args = process.argv.slice(2);\n"
            "const index = args.indexOf('--outputFile');\n"
            f"const report = {report_literal};\n"
            "if (args[0] === 'run' && index >= 0) {\n"
            + ("  if (report !== null) writeFileSync(args[index + 1], JSON.stringify(report));\n"
               if write_report else "")
            + f"  process.exit({report_exit});\n"
            "}\n"
            "console.log('vitest gate command ran');\n"
            f"process.exit({gate_exit});\n"
        )
        self.write(entry, source)
        probe = subprocess.run(["node", str(entry)], capture_output=True, text=True, check=False)
        if "vitest gate command ran" not in probe.stdout:
            self.fail(f"the fake vitest fixture did not launch: {probe.returncode} {probe.stdout} {probe.stderr}")
        return entry

    def vitest_gate(self, entry, *, gate_id="vitest-gate", tests, files=("src/witness.test.tsx",), cwd="."):
        return {
            "id": gate_id,
            "cwd": cwd,
            "argv": ["node", str(entry), "run"],
            "vitest_files": list(files),
            "vitest_tests": list(tests),
        }

    def bind_tree(self, run: pathlib.Path | None = None, *, tree: pathlib.Path | None = None):
        """Record the dependency tree a Node-tooling task resolves (required at check).

        The tree sits beside the run directory, as the runbook's hoisted install does,
        so the recorded route stays run-relative and the digest stays recomputable.
        """
        run_dir = run or self.run_dir()
        tree = tree or (self.root / "runs" / (run_dir.name + "-node_modules"))
        self.write(tree / "vitest" / "index.js", "module.exports = {};\n")
        self.write(tree / ".modules.yaml", "hoistPattern: []\n")
        log = self.root / "inputs" / (run_dir.name + "-pnpm-install.log")
        log.parent.mkdir(parents=True, exist_ok=True)
        log.write_bytes(b"Progress: resolved 1, reused 0, downloaded 1\nDone in 1.2s\n")
        return self.assert_ok(self.cli(
            "bind-dependencies", "--run-dir", str(run_dir), "--tree", str(tree), "--log", str(log)
        ))

    def run_dir(self) -> pathlib.Path:
        return self.root / "run"

    def checkout(self, run: pathlib.Path | None = None) -> pathlib.Path:
        return (run or self.run_dir()) / "checkout"

    def edit(self, relative: str, content: str, run: pathlib.Path | None = None) -> None:
        self.write(self.checkout(run) / relative, content)

    def stage_stale_mtime(self, relative: str, run: pathlib.Path | None = None) -> None:
        """Record an old mtime for a clean file so a later same-size edit hides from git.

        Refreshing the index while the content still matches stores the old mtime as
        cached stat data, which makes a same-size edit with a restored mtime invisible
        to plain `git diff` until the worktree mtimes are refreshed.
        """
        target = self.checkout(run) / relative
        os.utime(target, (1577836800, 1577836800))
        # An adversary with write access to the isolated checkout can disable Git's
        # ctime check, and the mtime-only bypass must reproduce on hosts that track
        # ctime, so this pins the same configuration the real bypass uses.
        self.git("config", "core.trustctime", "false", cwd=self.checkout(run))
        self.git("update-index", "--refresh", cwd=self.checkout(run))

    def install_base_echo_filter(self, relative: str, run: pathlib.Path | None = None) -> None:
        """Configure the checkout-local clean filter that echoes one path's base bytes.

        `.git/info/attributes` has the highest attribute precedence and `.git` is
        excluded from the Source manifest, so this is exactly what a worker with write
        access to the isolated checkout can install without touching any bound file.
        """
        checkout = self.checkout(run)
        attributes = checkout / ".git" / "info" / "attributes"
        attributes.parent.mkdir(parents=True, exist_ok=True)
        attributes.write_bytes(f"{relative} filter=hide\n".encode("utf-8"))
        self.git("config", "filter.hide.clean",
                 f"git cat-file blob {self.base}:{relative}", cwd=checkout)

    def remove_base_echo_filter(self, run: pathlib.Path | None = None) -> None:
        checkout = self.checkout(run)
        attributes = checkout / ".git" / "info" / "attributes"
        if attributes.exists():
            attributes.unlink()
        self.unset_base_echo_filter(run)

    def unset_base_echo_filter(self, run: pathlib.Path | None = None) -> None:
        """Drop only the clean-filter configuration, leaving any attribute file alone."""
        subprocess.run(["git", "-C", str(self.checkout(run)), "config", "--unset", "filter.hide.clean"],
                       capture_output=True, check=False)

    def seal(self, run: pathlib.Path | None = None) -> subprocess.CompletedProcess:
        return self.cli("seal", "--run-dir", str(run or self.run_dir()))

    def check(self, *extra: str, run: pathlib.Path | None = None) -> subprocess.CompletedProcess:
        return self.cli("check", "--run-dir", str(run or self.run_dir()), *extra)

    def import_review(self, record, run: pathlib.Path | None = None) -> subprocess.CompletedProcess:
        path = self.write_json(self.root / "inputs" / "review.json", record)
        return self.cli("review", "--run-dir", str(run or self.run_dir()), "--review", str(path))

    def receipt(self, usage, run: pathlib.Path | None = None) -> subprocess.CompletedProcess:
        path = self.write_json(self.root / "inputs" / "usage.json", usage)
        return self.cli("receipt", "--run-dir", str(run or self.run_dir()), "--usage", str(path))

    def accept(self, run: pathlib.Path | None = None, receipt_path: pathlib.Path | None = None):
        command = ["accept", "--run-dir", str(run or self.run_dir())]
        if receipt_path is not None:
            command += ["--receipt", str(receipt_path)]
        return self.cli(*command)

    def run_record(self, run: pathlib.Path | None = None) -> dict:
        return json.loads(((run or self.run_dir()) / "run.json").read_text(encoding="utf-8"))

    def frozen(self, name: str, run: pathlib.Path | None = None) -> dict:
        path = (run or self.run_dir()) / "frozen" / name
        return json.loads(path.read_text(encoding="utf-8"))

    def stored_receipt(self, run: pathlib.Path | None = None) -> dict:
        path = (run or self.run_dir()) / "receipt.json"
        return json.loads(path.read_text(encoding="utf-8"))

    def review_record(self, attempt, *, run=None, **overrides):
        run_path = run or self.run_dir()
        record = {
            "subject_hash": attempt["subject_hash"],
            "routes_sha256": self.run_record(run_path)["routes_sha256"],
            "check_attempt_id": attempt["attempt_id"],
            "check_evidence_sha256": attempt["attempt_sha256"],
            "reviewer": {"model_id": "reviewer-model", "session_ref": "reviewer-session-1"},
            "worker": {"model_id": "worker-model", "session_refs": ["worker-session-1"]},
            "verdict": "pass",
            "gate_weakening": False,
            "findings": [],
            "explanations": {
                text: "attested explanation for: " + text
                for text in self.frozen("task.json", run_path)["explanations"]
            },
        }
        record.update(overrides)
        return record

    def usage_rows(self, *, tokens=None, metadata=True, provenance="harness-estimate",
                   non_model_provenance="not-applicable", estimated_cost=1.5):
        tokens_value = tokens if tokens is not None else {"input": 10, "output": 2, "cached": 0}
        prompt = [{"kind": "task", "sha256": "ab" * 32, "index": 0}]

        def identity(model):
            return {"model": model, "provider": "fixture-provider", "api": "fixture-api", "assistant_messages": 1}

        def model_row(stage, session, model):
            row = {
                "stage": stage,
                "session_ref": session,
                "non_model_reason": None,
                "model_ids": [identity(model)],
                "prompt_hashes": list(prompt),
                "tokens": dict(tokens_value),
                "reported_cost": None,
                "estimated_cost": estimated_cost,
                "cost_provenance": provenance,
                "price_version": None,
                "wall_time": 3.0,
            }
            if metadata:
                row["metadata"] = {"accounting_complete": False}
                row["limitations"] = ["harness estimate without a price version"]
                known_subtotals = {
                    "assistant_messages": 1,
                    "assistant_messages_with_usage": 1,
                    "assistant_messages_with_cost_total": 1,
                }
                if estimated_cost is not None:
                    known_subtotals["cost"] = {"total": {"sum": estimated_cost, "messages": 1}}
                for field, value in tokens_value.items():
                    if value is not None:
                        known_subtotals[field] = {"sum": value, "messages": 1}
                row["known_subtotals"] = known_subtotals
            return row

        def non_model_row(stage, reason):
            return {
                "stage": stage,
                "session_ref": None,
                "non_model_reason": reason,
                "model_ids": [],
                "prompt_hashes": [],
                "tokens": {"input": None, "output": None, "cached": None},
                "reported_cost": None,
                "estimated_cost": None,
                "cost_provenance": non_model_provenance,
                "price_version": None,
                "wall_time": 1.0,
            }

        return {"repair_count": 0, "wall_time": 12.0, "phases": [
            model_row("exploration", "explorer-session-1", "worker-model"),
            model_row("implementation", "worker-session-1", "worker-model"),
            non_model_row("verification", "frozen gates executed without a model session"),
            model_row("review", "reviewer-session-1", "reviewer-model"),
            non_model_row("repair", "no repair was required"),
            non_model_row("failed_attempt", "no failed attempt occurred"),
        ]}

    def accepted_run(self, run: pathlib.Path | None = None, task=None, usage=None, environment_json=None):
        """Prepare, edit, seal, check, review and derive one accepted receipt."""
        run_path = run or self.run_dir()
        self.assert_ok(self.prepare(task=task, run=run_path, environment_json=environment_json))
        frozen_task = task if task is not None else self.task()
        if load_runner_module().requires_dependency_provisioning(frozen_task):
            self.bind_tree(run_path)
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run_path)
        self.edit("app/new_test.go", "package app\n\nimport \"testing\"\n\nfunc TestWitness2(t *testing.T) {}\n", run=run_path)
        sealed = self.assert_ok(self.seal(run=run_path))
        checked = self.assert_ok(self.check(run=run_path))
        self.assert_ok(self.import_review(self.review_record(checked, run=run_path), run=run_path))
        self.assert_ok(self.receipt(self.usage_rows() if usage is None else usage, run=run_path))
        return sealed, checked

    # -- tests ---------------------------------------------------------------

    def test_positive_flow_accepts_exact_derived_receipt(self) -> None:
        prepared = self.assert_ok(self.prepare())
        self.assertEqual(prepared["base"], self.base)
        self.assertEqual(prepared["head"], self.base)
        self.assertTrue(prepared["routes_sha256"].startswith("sha256:"))
        self.assertTrue(prepared["base_tree_digest"].startswith("sha256:"))

        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n")
        self.edit("app/new_test.go", "package app\n\nimport \"testing\"\n\nfunc TestWitness2(t *testing.T) {}\n")
        sealed = self.assert_ok(self.seal())
        self.assertEqual(sealed["head"], self.base)
        self.assertTrue(sealed["content_revision"].startswith("uncommitted:"))
        self.assertEqual(
            [change["path"] for change in sealed["changed_paths"]],
            ["app/main.go", "app/new_test.go"],
        )

        checked = self.assert_ok(self.check())
        self.assertTrue(checked["passed"])
        self.assertTrue(checked["complete"])
        self.assertTrue(checked["attempt_sha256"].startswith("sha256:"))

        self.assert_ok(self.import_review(self.review_record(checked)))
        summary = self.assert_ok(self.receipt(self.usage_rows()))
        self.assertTrue(summary["accepted"], summary["acceptance_blockers"])
        receipt = self.stored_receipt()
        self.assertEqual(receipt["acceptance_blockers"], [])
        self.assertEqual(receipt["tokens"], {
            "input": 30, "output": 6, "cached": 0,
            "known_input_subtotal": 30, "known_output_subtotal": 6, "known_cached_subtotal": 0,
        })
        self.assertIsNone(receipt["cost"]["reported_cost"])
        self.assertEqual(receipt["cost"]["estimated_cost"], 4.5)
        self.assertEqual(receipt["cost"]["known_estimated_cost_subtotal"], 4.5)
        self.assertFalse(receipt["cost"]["accounting_complete"])
        self.assertEqual(receipt["cost"]["price_versions"], [])
        self.assertEqual(receipt["subject_hash"], sealed["subject_hash"])
        self.assertEqual(receipt["reviewer_findings"], [])
        self.assertEqual(receipt["repair_count"], 0)
        self.assertEqual(receipt["explanations"], ["explain fixture one", "explain fixture two"])
        self.assertEqual(sorted(receipt["reviewed_explanations"]), sorted(receipt["explanations"]))
        self.assertEqual(
            receipt["reviewed_explanations"]["explain fixture one"],
            "attested explanation for: explain fixture one",
        )
        self.assertEqual(receipt["subject"]["changed_paths"], sealed["changed_paths"])
        self.assertEqual(receipt["checks"][0]["exit_code"], 0)
        self.assertEqual(receipt["review"]["verdict"], "pass")
        self.assert_ok(self.accept())

        fabricated = json.loads(json.dumps(receipt))
        fabricated["checks"][0]["exit_code"] = 7
        self.assert_refused(self.accept(receipt_path=self.write_json(self.root / "fabricated.json", fabricated)))
        extra = json.loads(json.dumps(receipt))
        extra["claimed_savings"] = 0.9
        self.assert_refused(self.accept(receipt_path=self.write_json(self.root / "extra.json", extra)))
        changed_cost = json.loads(json.dumps(receipt))
        changed_cost["cost"]["estimated_cost"] = 0.0
        self.assert_refused(self.accept(receipt_path=self.write_json(self.root / "cost.json", changed_cost)))
        missing = json.loads(json.dumps(receipt))
        missing.pop("cost")
        self.assert_refused(self.accept(receipt_path=self.write_json(self.root / "missing.json", missing)))

    def test_source_edit_after_checks_or_review_is_refused_and_preserved(self) -> None:
        run = self.root / "run-late"
        self.assert_ok(self.prepare(run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.assert_ok(self.check(run=run))
        self.assert_ok(self.import_review(self.review_record(checked, run=run), run=run))
        self.edit("app/late.go", "package app\n\nvar Late = 1\n", run=run)
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "does not match the sealed subject")
        self.assertTrue((self.checkout(run) / "app/late.go").exists())

        run = self.root / "run-review-late"
        self.assert_ok(self.prepare(run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 3 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.assert_ok(self.check(run=run))
        self.edit("app/late.go", "package app\n\nvar Late = 2\n", run=run)
        self.assert_refused(
            self.import_review(self.review_record(checked, run=run), run=run),
            "does not match the sealed subject",
        )
        self.assertTrue((self.checkout(run) / "app/late.go").exists())

    def test_go_event_gate_rejects_missing_pass_skips_and_failures(self) -> None:
        for label, code in (
            ("missing-pass", EMIT_RUN_ONLY),
            ("skipped", EMIT_SKIP),
            ("skipped-descendant", EMIT_SKIPPED_DESCENDANT),
            ("nonzero", EXIT_THREE),
        ):
            with self.subTest(gate=label):
                run = self.root / ("run-" + label)
                task = self.task(gates=[self.gate_events(code=code)])
                self.assert_ok(self.prepare(task=task, run=run))
                self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
                self.assert_ok(self.seal(run=run))
                checked = self.check(run=run)
                self.assertNotEqual(checked.returncode, 0, checked.stdout)
                summary = json.loads(checked.stdout)
                self.assertFalse(summary["passed"])
                self.assertTrue(summary["complete"])
                receipt_summary = self.assert_ok(self.receipt(self.usage_rows(), run=run))
                self.assertFalse(receipt_summary["accepted"])
                self.assertTrue(any("did not pass" in blocker for blocker in receipt_summary["acceptance_blockers"]))
                self.assert_refused(self.accept(run=run), "nonaccepted row")

    def test_missing_or_failing_gate_is_a_nonaccepted_row(self) -> None:
        run = self.root / "run-partial"
        task = self.task(gates=[self.gate_plain("first"), self.gate_plain("second")])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        partial = self.check("--gate", "first", run=run)
        summary = json.loads(partial.stdout)
        self.assertNotEqual(partial.returncode, 0)
        self.assertFalse(summary["complete"])
        self.assertFalse(summary["passed"])
        self.assert_refused(self.import_review(self.review_record(summary, run=run), run=run), "incomplete")
        receipt_summary = self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assertFalse(receipt_summary["accepted"])
        self.assertTrue(any("incomplete" in blocker for blocker in receipt_summary["acceptance_blockers"]))

        run = self.root / "run-broken-cwd"
        task = self.task(gates=[self.gate_plain("broken", cwd="missing-dir")])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        failed = self.check(run=run)
        self.assertNotEqual(failed.returncode, 0, failed.stdout)
        receipt_summary = self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assertFalse(receipt_summary["accepted"])
        self.assertTrue(any("gate broken" in blocker for blocker in receipt_summary["acceptance_blockers"]))

        attempt_path = run / "checks" / "attempt-0001" / "attempt.json"
        tampered = json.loads(attempt_path.read_text(encoding="utf-8"))
        tampered["gates"][0]["passed"] = True
        attempt_path.write_bytes((json.dumps(tampered, indent=2, sort_keys=True) + "\n").encode("utf-8"))
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "disagrees with its evidence")

    def test_protected_assets_cannot_be_weakened(self) -> None:
        task = self.task(allowed_paths=["**"])
        self.assert_ok(self.prepare(task=task))
        cases = (
            ("app/main_test.go", "package app\n\nimport \"testing\"\n\nfunc TestBaseline(t *testing.T) { t.Skip() }\n", "baseline test"),
            (".github/workflows/ci.yml", "name: weakened\n", "protected asset"),
            (".github/workflows/extra.yml", "name: extra\n", "protected asset"),
            ("AGENTS.md", "# weakened\n", "protected asset"),
            ("CLAUDE.md", "# weakened\n", "protected asset"),
            ("contracts/other.json", '{"weakened": true}\n', "protected asset"),
            (".agents/skills/demo/SKILL.md", "# weakened\n", "protected asset"),
            ("source.txt", "weakened\n", "protected asset"),
            ("app/check_test.py", "def test_placeholder():\n    assert False\n", "baseline test"),
            ("web/thing.test.ts", "export const thing = 2;\n", "baseline test"),
            ("tests/sample.txt", "weakened\n", "baseline test"),
        )
        for relative, content, needle in cases:
            with self.subTest(path=relative):
                self.edit(relative, content)
                self.assert_refused(self.seal(), needle)
                self.assertEqual(self.read(self.checkout() / relative).decode("utf-8"), content)
                if relative in FILES:
                    self.write(self.checkout() / relative, FILES[relative])
                else:
                    (self.checkout() / relative).unlink()

        (self.checkout() / "app/main_test.go").unlink()
        self.assert_refused(self.seal(), "baseline test")
        self.assertFalse((self.checkout() / "app/main_test.go").exists())
        self.write(self.checkout() / "app/main_test.go", FILES["app/main_test.go"])

    def test_new_tests_require_explicit_allowed_paths(self) -> None:
        new_test = "package app\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n"
        task = self.task(allowed_paths=["app/main.go"])
        self.assert_ok(self.prepare(task=task, run=self.root / "run-narrow"))
        self.edit("app/other_test.go", new_test, run=self.root / "run-narrow")
        self.assert_refused(self.seal(run=self.root / "run-narrow"), "new test")
        self.assertEqual(self.read(self.checkout(self.root / "run-narrow") / "app/other_test.go").decode("utf-8"), new_test)

        task = self.task(allowed_paths=["app/main.go", "app/other_test.go"])
        run = self.root / "run-scoped"
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/other_test.go", new_test, run=run)
        sealed = self.assert_ok(self.seal(run=run))
        self.assertEqual([change["path"] for change in sealed["changed_paths"]], ["app/other_test.go"])
        self.assert_ok(self.check(run=run))

    def test_changed_path_outside_allowed_paths_is_refused(self) -> None:
        self.assert_ok(self.prepare(task=self.task(allowed_paths=["app/main.go"])))
        self.edit("docs/notes.md", "changed\n")
        self.assert_refused(self.seal(), "outside allowed_paths")
        self.assertEqual(self.read(self.checkout() / "docs/notes.md").decode("utf-8"), "changed\n")

    def test_ignored_source_is_bound_and_refused_outside_allowed_paths(self) -> None:
        run = self.root / "run-ignored-denied"
        self.assert_ok(self.prepare(task=self.task(allowed_paths=["app/main.go"]), run=run))
        self.edit("app/extra.local.go", "package app\n", run=run)
        ignored = subprocess.run(
            ["git", "-C", str(self.checkout(run)), "check-ignore", "app/extra.local.go"],
            capture_output=True, check=False,
        )
        self.assertEqual(ignored.returncode, 0, "fixture premise: *.local.* must ignore the file")
        self.assert_refused(self.seal(run=run), "outside allowed_paths")

        run = self.root / "run-ignored-bound"
        task = self.task(allowed_paths=["app/main.go", "app/extra.local.go"])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/extra.local.go", "package app\n\nvar Ignored = 1\n", run=run)
        sealed = self.assert_ok(self.seal(run=run))
        self.assertIn("app/extra.local.go", [change["path"] for change in sealed["changed_paths"]])
        checked = self.assert_ok(self.check(run=run))
        self.assert_ok(self.import_review(self.review_record(checked, run=run), run=run))

        self.edit("app/late.local.go", "package app\n\nvar Late = 1\n", run=run)
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "does not match the sealed subject")
        self.assertTrue((self.checkout(run) / "app/late.local.go").exists())
        (self.checkout(run) / "app/late.local.go").unlink()

        self.edit("app/extra.local.go", "package app\n\nvar Ignored = 2\n", run=run)
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "does not match the sealed subject")

    def test_review_record_requires_independent_accepted_evidence(self) -> None:
        self.assert_ok(self.prepare())
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n")
        self.assert_ok(self.seal())
        checked = self.assert_ok(self.check())
        cases = (
            ("same-session", {"reviewer": {"model_id": "reviewer-model", "session_ref": "worker-session-1"}}, "not independent"),
            ("verdict-fail", {"verdict": "fail"}, "verdict is fail"),
            ("gate-weakening", {"gate_weakening": True}, "gate weakening"),
            ("open-finding", {"findings": [{"id": "F1", "severity": "major", "status": "open", "description": "open"}]}, "unresolved findings"),
            ("missing-explanation", {"explanations": {"explain fixture one": "x"}}, "does not attest"),
            ("empty-explanation", {"explanations": {"explain fixture one": "x", "explain fixture two": "   "}}, "empty explanation"),
            ("extra-explanation", {"explanations": {"explain fixture one": "x", "explain fixture two": "y", "extra": "z"}}, "not required"),
            ("reviewer-model", {"reviewer": {"model_id": "other-model", "session_ref": "reviewer-session-1"}}, "reviewer model"),
            ("worker-model", {"worker": {"model_id": "other-model", "session_refs": ["worker-session-1"]}}, "worker model"),
            ("subject", {"subject_hash": "sha256:" + "0" * 64}, "sealed subject"),
            ("attempt-id", {"check_attempt_id": "attempt-9999"}, "latest sealed-subject attempt"),
            ("evidence", {"check_evidence_sha256": "sha256:" + "1" * 64}, "check attempt evidence"),
        )
        for label, overrides, needle in cases:
            with self.subTest(case=label):
                self.assert_refused(self.import_review(self.review_record(checked, **overrides)), needle)
        extra = self.review_record(checked)
        extra["notes"] = "x"
        self.assert_refused(self.import_review(extra), "unexpected keys")
        self.assert_ok(self.import_review(self.review_record(checked)))

    def test_usage_rows_reject_incomplete_or_inconsistent_phases(self) -> None:
        self.accepted_run(run=self.root / "run-usage")

        def variant(mutate):
            usage = self.usage_rows()
            mutate(usage["phases"])
            return self.receipt(usage, run=self.root / "run-usage")

        def drop_repair(phases):
            phases[:] = [row for row in phases if row["stage"] != "repair"]

        def both_missing(phases):
            phases[0]["session_ref"] = None

        def both_set(phases):
            phases[0]["non_model_reason"] = "also non-model"

        def non_model_tokens(phases):
            phases[2]["tokens"] = {"input": 1, "output": 1, "cached": 0}

        def unknown_stage(phases):
            phases[5]["stage"] = "failed"

        def empty_models(phases):
            phases[0]["model_ids"] = []

        def extra_key(phases):
            phases[0]["notes"] = "x"

        def missing_key(phases):
            phases[0].pop("wall_time")

        def string_models(phases):
            phases[0]["model_ids"] = ["worker-model"]

        def bad_prompt_hash(phases):
            phases[0]["prompt_hashes"] = [{"kind": "task", "sha256": "nothex"}]

        def duplicate_session(phases):
            phases[0]["session_ref"] = phases[1]["session_ref"]

        def missing_worker_session(phases):
            phases[1]["session_ref"] = "unrelated-session"

        def empty_prompt_hashes(phases):
            phases[0]["prompt_hashes"] = []

        def unknown_subtotal_key(phases):
            phases[0]["known_subtotals"]["estimated_cost"] = 1.5

        def incomplete_subtotal_entry(phases):
            phases[0]["known_subtotals"]["input"] = {"sum": 10}

        def zero_message_subtotal(phases):
            phases[0]["known_subtotals"]["output"] = {"sum": 2, "messages": 0}

        def negative_subtotal(phases):
            phases[0]["known_subtotals"]["input"] = {"sum": -1, "messages": 1}

        def disagreeing_subtotal(phases):
            phases[0]["known_subtotals"]["input"] = {"sum": 99, "messages": 1}

        def non_model_subtotals(phases):
            phases[2]["known_subtotals"] = {"assistant_messages": 0}

        cases = (
            ("drop-repair", drop_repair, "cover every evaluation stage"),
            ("both-missing", both_missing, "exactly one of"),
            ("both-set", both_set, "exactly one of"),
            ("non-model-tokens", non_model_tokens, "must not report token usage"),
            ("unknown-stage", unknown_stage, "must be one of"),
            ("empty-models", empty_models, "must list the served model ids"),
            ("extra-key", extra_key, "unexpected keys"),
            ("missing-key", missing_key, "missing keys"),
            ("string-models", string_models, "served model identity"),
            ("bad-prompt-hash", bad_prompt_hash, "sha256 hex digest"),
            ("duplicate-session", duplicate_session, "duplicate model session"),
            ("missing-worker-session", missing_worker_session, "implementing worker session"),
            ("empty-prompt-hashes", empty_prompt_hashes, "prompt hash"),
            ("unknown-subtotal-key", unknown_subtotal_key, "unexpected subtotal key"),
            ("incomplete-subtotal-entry", incomplete_subtotal_entry, "missing keys"),
            ("zero-message-subtotal", zero_message_subtotal, "integer >= 1"),
            ("negative-subtotal", negative_subtotal, "finite number"),
            ("disagreeing-subtotal", disagreeing_subtotal, "disagrees with the complete session total"),
            ("non-model-subtotals", non_model_subtotals, "known subtotals without a session_ref"),
        )
        for label, mutate, needle in cases:
            with self.subTest(case=label):
                self.assert_refused(variant(mutate), needle)

        for count in (None, -1, True, 1.5):
            with self.subTest(repair_count=count):
                usage = self.usage_rows()
                usage["repair_count"] = count
                self.assert_refused(self.receipt(usage, run=self.root / "run-usage"), "must be an integer")
        missing_count = self.usage_rows()
        missing_count.pop("repair_count")
        self.assert_refused(self.receipt(missing_count, run=self.root / "run-usage"), "missing keys")

    def test_refused_usage_import_preserves_the_frozen_snapshot(self) -> None:
        run = self.root / "run-refused-import"
        self.accepted_run(run=run)
        self.assert_ok(self.accept(run=run))
        frozen_usage = self.read(run / "frozen" / "usage.json")
        run_record = self.read(run / "run.json")
        receipt_record = self.read(run / "receipt.json")
        accepted = self.stored_receipt(run)
        self.assertEqual(accepted["evidence"]["frozen_usage_sha256"],
                         "sha256:" + hashlib.sha256(frozen_usage).hexdigest())

        self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assertEqual(self.read(run / "frozen" / "usage.json"), frozen_usage)
        self.assertEqual(self.read(run / "run.json"), run_record)
        self.assertEqual(self.read(run / "receipt.json"), receipt_record)

        rejected = self.usage_rows()
        rejected["phases"][1]["session_ref"] = "unrelated-worker-session"
        self.assert_refused(self.receipt(rejected, run=run), "omit an implementing worker session")

        self.assertEqual(self.read(run / "frozen" / "usage.json"), frozen_usage)
        self.assertEqual(self.read(run / "run.json"), run_record)
        self.assertEqual(self.read(run / "receipt.json"), receipt_record)
        self.assertEqual(self.stored_receipt(run), accepted)
        summary = self.assert_ok(self.accept(run=run))
        self.assertTrue(summary["accepted"], summary)

    def test_repair_count_records_the_coordinator_ledger(self) -> None:
        usage = self.usage_rows()
        usage["repair_count"] = 2
        usage["wall_time"] = 5.0
        self.accepted_run(usage=usage)
        self.assertEqual(self.stored_receipt()["repair_count"], 2)
        self.assertEqual(self.stored_receipt()["wall_time"], 5.0)
        self.assert_ok(self.accept())

    def test_absent_model_evidence_has_unknown_totals(self) -> None:
        self.assert_ok(self.prepare())
        self.assert_ok(self.seal())
        self.assert_ok(self.check())
        usage = self.usage_rows()
        for row in usage["phases"]:
            row.pop("known_subtotals", None)
            row.update(session_ref=None, non_model_reason="No model usage evidence was recorded.",
                       model_ids=[], prompt_hashes=[], tokens={"input": None, "output": None, "cached": None},
                       reported_cost=None, estimated_cost=None, cost_provenance=None, price_version=None)
        self.assert_ok(self.receipt(usage))
        receipt = self.stored_receipt()
        self.assertFalse(receipt["accepted"])
        self.assertFalse(receipt["cost"]["accounting_complete"])
        self.assertIsNone(receipt["cost"]["reported_cost"])
        self.assertIsNone(receipt["cost"]["estimated_cost"])
        for field in ("input", "output", "cached"):
            self.assertIsNone(receipt["tokens"][field])
        self.assert_refused(self.accept(), "nonaccepted row")

    def test_later_incomplete_attempt_cannot_reuse_an_earlier_pass(self) -> None:
        task = self.task(gates=[self.gate_events(), self.gate_plain()])
        self.accepted_run(task=task)
        previous = self.write_json(self.root / "earlier-receipt.json", self.stored_receipt())
        gate_id = task["gates"][0]["id"]
        partial = self.cli("check", "--run-dir", str(self.run_dir()), "--gate", gate_id)
        self.assertNotEqual(partial.returncode, 0, partial.stdout + partial.stderr)
        self.assertFalse(json.loads(partial.stdout)["complete"])
        self.assert_refused(self.accept(receipt_path=previous), "does not match")

    def test_tooling_revision_change_invalidates_an_earlier_receipt(self) -> None:
        self.accepted_run()
        tools = self.root / "changed-tools"
        tools.mkdir()
        for name in (SCRIPT.name, "assert-go-test-json.py", "linux-v2-source-subject.py"):
            shutil.copyfile(SCRIPT.with_name(name), tools / name)
        changed = tools / SCRIPT.name
        changed.write_bytes(changed.read_bytes() + b"\n# Tooling revision control.\n")
        result = subprocess.run([sys.executable, str(changed), "accept", "--run-dir", str(self.run_dir())],
                                text=True, capture_output=True, check=False)
        self.assert_refused(result, "does not match")

    def test_unknown_usage_stays_null_not_zero(self) -> None:
        run = self.root / "run-unknown"
        self.accepted_run(run=run, usage=self.usage_rows(tokens={"input": None, "output": 5, "cached": None}))
        summary = json.loads(self.cli("accept", "--run-dir", str(run)).stdout)
        self.assertTrue(summary["accepted"])
        receipt = self.stored_receipt(run)
        self.assertIsNone(receipt["tokens"]["input"])
        self.assertIsNone(receipt["tokens"]["cached"])
        self.assertEqual(receipt["tokens"]["known_input_subtotal"], 0)
        self.assertEqual(receipt["tokens"]["output"], 15)
        self.assertEqual(receipt["tokens"]["known_output_subtotal"], 15)
        self.assertIsNone(receipt["cost"]["reported_cost"])
        self.assertEqual(receipt["cost"]["estimated_cost"], 4.5)
        self.assertFalse(receipt["cost"]["accounting_complete"])
        self.assertTrue(any("unknown token usage" in reason for reason in receipt["cost"]["incomplete_reasons"]))
        self.assertTrue(any("missing price version" in reason for reason in receipt["cost"]["incomplete_reasons"]))

        run = self.root / "run-null-provenance"
        self.accepted_run(run=run, usage=self.usage_rows(provenance=None, non_model_provenance=None))
        summary = json.loads(self.cli("accept", "--run-dir", str(run)).stdout)
        self.assertTrue(summary["accepted"])
        receipt = self.stored_receipt(run)
        self.assertFalse(receipt["cost"]["accounting_complete"])
        self.assertTrue(any("missing cost provenance" in reason for reason in receipt["cost"]["incomplete_reasons"]))
        self.assertEqual(receipt["cost"]["estimated_cost"], 4.5)
        self.assertEqual(
            [row["cost_provenance"] for row in receipt["phases"] if row["session_ref"] is not None],
            [None, None, None],
        )

    def test_duplicate_and_nonfinite_json_are_refused(self) -> None:
        self.assert_ok(self.prepare(run=self.root / "run-json"))
        duplicate_task = self.root / "duplicate-task.json"
        duplicate_task.write_bytes(
            b'{"task_id": "one", "task_id": "two", "base": "' + self.base.encode("ascii") + b'"}\n'
        )
        self.assert_refused(
            self.cli(
                "prepare", "--run-dir", str(self.root / "run-json-duplicate"), "--task", str(duplicate_task),
                "--routes", str(self.write_json(self.root / "routes.json", self.routes())),
                "--source", str(self.repo),
            ),
            "repeats the JSON key",
        )

        run = self.root / "run-nonfinite"
        self.assert_ok(self.prepare(run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.assert_ok(self.check(run=run))
        self.assert_ok(self.import_review(self.review_record(checked, run=run), run=run))
        usage_text = json.dumps(self.usage_rows(), indent=2, sort_keys=True)
        nan_usage = usage_text.replace('"wall_time": 3.0', '"wall_time": NaN', 1)
        nan_path = self.root / "nan-usage.json"
        nan_path.write_bytes(nan_usage.encode("utf-8"))
        self.assert_refused(
            self.cli("receipt", "--run-dir", str(run), "--usage", str(nan_path)),
            "non-JSON constant",
        )

        self.assert_ok(self.receipt(self.usage_rows(), run=run))
        receipt_text = (run / "receipt.json").read_text(encoding="utf-8")
        duplicate_receipt = self.root / "duplicate-receipt.json"
        duplicate_receipt.write_bytes(
            receipt_text.replace('"task_id": "fixture-task",', '"task_id": "fixture-task",\n  "task_id": "other",', 1).encode("utf-8")
        )
        self.assert_refused(
            self.accept(run=run, receipt_path=duplicate_receipt), "repeats the JSON key"
        )
        nan_receipt = self.root / "nan-receipt.json"
        nan_receipt.write_bytes(receipt_text.replace('"estimated_cost": 4.5', '"estimated_cost": NaN', 1).encode("utf-8"))
        self.assert_refused(self.accept(run=run, receipt_path=nan_receipt), "non-JSON constant")
        duplicate_review = self.root / "duplicate-review.json"
        duplicate_review.write_bytes(b'{"subject_hash": "sha256:00", "subject_hash": "sha256:11"}\n')
        self.assert_refused(
            self.cli("review", "--run-dir", str(run), "--review", str(duplicate_review)),
            "repeats the JSON key",
        )

    def test_prepare_validates_task_routes_and_base(self) -> None:
        cases = (
            ("base-length", self.task(base="0" * 39), None, 0, "40-hex commit id"),
            ("base-missing", self.task(base="0" * 40), None, 0, "failed"),
            ("absolute-allowed", self.task(allowed_paths=["/etc/passwd"]), None, 0, "must be relative"),
            ("parent-allowed", self.task(allowed_paths=["app/../etc"]), None, 0, "must not contain"),
            ("empty-argv", self.task(gates=[{"id": "bad", "cwd": ".", "argv": []}]), None, 0, "argv must be a non-empty list"),
            ("absolute-cwd", self.task(gates=[{"id": "bad", "cwd": "/tmp", "argv": [sys.executable, "-c", "pass"]}]), None, 0, "must be relative"),
            ("package-without-tests", self.task(gates=[{"id": "bad", "cwd": ".", "argv": [sys.executable, "-c", "pass"], "go_package": "x"}]), None, 0, "go_package requires go_tests"),
            ("routes-missing-role", None, {"worker": self.routes()["worker"]}, 0, "missing keys"),
            ("routes-secret", None, {"worker": self.routes()["worker"], "reviewer": {**self.routes()["reviewer"], "api_key": "x"}}, 0, "secret-like key"),
            ("routes-authorization", None, {"worker": self.routes()["worker"], "reviewer": {k: v for k, v in self.routes()["reviewer"].items() if k != "authorization_ref"}}, 0, "missing keys"),
        )
        for index, (label, task, routes, _unused, needle) in enumerate(cases):
            with self.subTest(case=label):
                run = self.root / f"run-validate-{index}"
                self.assert_refused(self.prepare(task=task, routes=routes, run=run), needle)
                self.assertFalse(run.exists(), "a refused prepare must not leave a run directory")

        occupied = self.root / "run-occupied"
        self.write(occupied / "existing.txt", "x\n")
        self.assert_refused(self.prepare(run=occupied), "must be empty")

    def test_source_change_during_a_gate_fails_the_attempt(self) -> None:
        task = self.task(gates=[self.gate_plain("writer", WRITE_DURING_GATE)])
        run = self.root / "run-writer"
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.check(run=run)
        self.assertNotEqual(checked.returncode, 0, checked.stdout)
        summary = json.loads(checked.stdout)
        self.assertFalse(summary["passed"])
        self.assertFalse(summary["complete"])
        written = self.checkout(run) / "wrote_during_gate.txt"
        self.assertTrue(written.exists())
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "does not match the sealed subject")
        written.unlink()
        receipt_summary = self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assertFalse(receipt_summary["accepted"])
        self.assertTrue(
            any("not bound to the sealed subject" in blocker for blocker in receipt_summary["acceptance_blockers"])
        )
        self.assert_refused(self.accept(run=run), "nonaccepted row")

    def test_timed_out_gate_terminates_its_descendants(self) -> None:
        # The descendant records survival through a handshake instead of a wall-clock
        # sleep: it waits for the release the test writes only after the timed-out
        # check returned, so a descendant the kill did reach can never write the
        # witness and a survivor records itself as soon as the release lands. The gate
        # and the descendant publish their own start markers outside the checkout so a
        # failure records what the fixture had established on that host. The handshake
        # directory is a sibling of the checkout because the gate runs with `cwd: "."`
        # in the checkout, so `..` is the run directory for both processes.
        descendant = (
            "import os, time\n"
            "from pathlib import Path\n"
            "handshake = Path('..', 'timeout-tree-handshake')\n"
            "handshake.joinpath('descendant-started.txt').write_text(str(os.getpid()), encoding='utf-8')\n"
            "release = handshake / 'descendant-release.txt'\n"
            "deadline = time.monotonic() + 300.0\n"
            "while not release.exists() and time.monotonic() < deadline:\n"
            "    time.sleep(0.02)\n"
            "Path('late-descendant.txt').write_text('late' if release.exists() else 'late-cap', encoding='utf-8')\n"
        )
        gate_code = (
            "import subprocess, sys, time\n"
            "from pathlib import Path\n"
            # DEVNULL so a descendant the kill missed cannot hold the gate's
            # inherited stdout/stderr pipes: on Windows that would keep the runner's
            # bounded reap waiting until the descendant's own wait cap expired,
            # delaying the release until the witness had already been missed.
            f"subprocess.Popen([sys.executable, '-c', {descendant!r}], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)\n"
            "Path('..', 'timeout-tree-handshake', 'gate-spawned-descendant.txt').write_text('spawned', encoding='utf-8')\n"
            "time.sleep(60)\n"
        )
        run = self.root / "run-timeout-tree"
        task = self.task(gates=[self.gate_plain("slow", gate_code)])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        handshake = run / "timeout-tree-handshake"
        handshake.mkdir(parents=True)
        checked = self.check("--timeout", "0.5", run=run)
        self.assertNotEqual(checked.returncode, 0, checked.stdout)
        summary = json.loads(checked.stdout)
        self.assertFalse(summary["passed"])
        result = json.loads(
            (run / "checks" / summary["attempt_id"] / "slow" / "result.json").read_text(encoding="utf-8")
        )
        self.assertTrue(result["timed_out"])
        release = handshake / "descendant-release.txt"
        release.write_text("release\n", encoding="utf-8")
        witness = self.checkout(run) / "late-descendant.txt"
        # The bound matches the runner's own reap window: a survivor the kill missed
        # writes as soon as the release lands, so the poll only has to outlast that.
        guard = load_runner_module().PROCESS_TREE_REAP_SECONDS
        deadline = time.monotonic() + guard
        while not witness.exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        text = witness.read_text(encoding="utf-8") if witness.exists() else None
        self.assertIsNone(
            text,
            "a gate descendant survived the per-gate timeout: "
            f"gate spawned descendant={(handshake / 'gate-spawned-descendant.txt').exists()}, "
            f"descendant started={(handshake / 'descendant-started.txt').exists()}, "
            f"witness={text!r}; the markers cannot distinguish a descendant the kill missed inside the "
            "tree from one created after the kill enumerated it (Windows `taskkill /T`) or after the "
            "process group was signalled (POSIX `killpg`): neither kill can reach a process it never saw",
        )

    def test_symlink_targets_are_bound(self) -> None:
        run = self.root / "run-symlink"
        self.assert_ok(self.prepare(task=self.task(allowed_paths=["link.txt"]), run=run))
        checkout = self.checkout(run)
        try:
            os.symlink("source.txt", checkout / "link.txt")
        except (OSError, NotImplementedError) as error:
            self.skipTest(f"symlink creation is unavailable: {error}")
        first = self.assert_ok(self.seal(run=run))
        self.assertEqual([change["path"] for change in first["changed_paths"]], ["link.txt"])
        os.remove(checkout / "link.txt")
        os.symlink("app/main.go", checkout / "link.txt")
        second = self.assert_ok(self.seal(run=run))
        self.assertNotEqual(first["subject_hash"], second["subject_hash"])

    def test_file_modes_are_bound_when_git_tracks_them(self) -> None:
        if os.name != "posix":
            self.skipTest("executable file modes are not tracked on this host")
        run = self.root / "run-mode"
        self.assert_ok(self.prepare(task=self.task(allowed_paths=["app/main.go"]), run=run))
        filemode = subprocess.run(
            ["git", "-C", str(self.checkout(run)), "config", "--bool", "core.filemode"],
            capture_output=True, text=True, check=False,
        )
        if filemode.stdout.strip() == "false":
            self.skipTest("core.filemode is disabled in the isolated checkout")
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        first = self.assert_ok(self.seal(run=run))
        os.chmod(self.checkout(run) / "app/main.go", 0o755)
        second = self.assert_ok(self.seal(run=run))
        self.assertNotEqual(first["subject_hash"], second["subject_hash"])

    def test_pristine_tracked_executables_report_no_mode_change(self) -> None:
        # A base that tracks an executable file must seal clean on hosts that cannot
        # represent the executable bit, exactly as `git diff` honors core.filemode.
        self.write(self.repo / "scripts/executable.sh", "#!/bin/sh\necho fixture\n")
        self.git("add", "scripts/executable.sh")
        self.git("update-index", "--chmod=+x", "scripts/executable.sh")
        self.git("commit", "--quiet", "-m", "add executable fixture")
        self.base = self.git_text("rev-parse", "HEAD")
        run = self.root / "run-executable-pristine"
        self.assert_ok(self.prepare(run=run))
        sealed = self.assert_ok(self.seal(run=run))
        self.assertEqual(sealed["changed_paths"], [])

    def test_content_derived_change_detection_hides_nothing(self) -> None:
        run = self.root / "run-content-derived"
        task = self.task(allowed_paths=["**"])
        self.assert_ok(self.prepare(task=task, run=run))
        checkout = self.checkout(run)

        # Case A: an assume-unchanged index entry hides a protected edit from git diff.
        self.edit("source.txt", "weakened source\n", run=run)
        self.git("update-index", "--assume-unchanged", "source.txt", cwd=checkout)
        self.assert_refused(self.seal(run=run), "assume-unchanged")
        self.git("update-index", "--no-assume-unchanged", "source.txt", cwd=checkout)
        self.write(checkout / "source.txt", FILES["source.txt"])
        self.git("update-index", "--refresh", cwd=checkout)

        # Case B: a same-size protected edit with a restored timestamp is invisible to
        # plain git diff until the runner refreshes the worktree mtimes.
        self.stage_stale_mtime("source.txt", run)
        self.write(checkout / "source.txt", "tampered content\n")
        os.utime(checkout / "source.txt", (1577836800, 1577836800))
        self.assertEqual(self.git_text("diff", "--name-status", cwd=checkout), "")
        self.assert_refused(self.seal(run=run), "protected asset")
        self.write(checkout / "source.txt", FILES["source.txt"])
        os.utime(checkout / "source.txt", (1577836800, 1577836800))
        self.git("update-index", "--refresh", cwd=checkout)

        # Case C: the same trick on an allowed file must still be reported.
        self.stage_stale_mtime("app/main.go", run)
        self.write(checkout / "app/main.go", "package app\n\nfunc Main() int { return 2 }\n")
        os.utime(checkout / "app/main.go", (1577836800, 1577836800))
        self.assertEqual(self.git_text("diff", "--name-status", cwd=checkout), "")
        sealed = self.assert_ok(self.seal(run=run))
        self.assertEqual([change["path"] for change in sealed["changed_paths"]], ["app/main.go"])

    def test_checkout_local_filter_cannot_hide_a_weakened_baseline_test(self) -> None:
        run = self.root / "run-filter-bypass"
        task = self.task(allowed_paths=["**"])
        self.assert_ok(self.prepare(task=task, run=run))
        checkout = self.checkout(run)
        self.install_base_echo_filter("app/main_test.go", run)
        weakened = "package app\n\nimport \"testing\"\n\nfunc TestBaseline(t *testing.T) { t.Skip() }\n"
        self.edit("app/main_test.go", weakened, run=run)

        # Bypass premise: the clean filter makes git diff report no change at all.
        self.assertEqual(self.git_text("diff", "--name-status", cwd=checkout), "")

        # The seal compares object-store bytes with worktree bytes, so the weakened
        # baseline test is still found and refused with the filter installed.
        self.assert_refused(self.seal(run=run), "baseline test")
        self.assertEqual(self.read(checkout / "app/main_test.go").decode("utf-8"), weakened)

        # Legitimate paths keep working: an unchanged baseline and a real edit.
        self.remove_base_echo_filter(run)
        self.write(checkout / "app/main_test.go", FILES["app/main_test.go"])
        sealed = self.assert_ok(self.seal(run=run))
        self.assertEqual(sealed["changed_paths"], [])
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        sealed = self.assert_ok(self.seal(run=run))
        self.assertEqual([change["path"] for change in sealed["changed_paths"]], ["app/main.go"])

    def test_checkout_local_attribute_machinery_is_refused_without_a_content_edit(self) -> None:
        run = self.root / "run-filter-machinery"
        task = self.task(allowed_paths=["**"])
        self.assert_ok(self.prepare(task=task, run=run))
        checkout = self.checkout(run)
        self.install_base_echo_filter("app/main_test.go", run)

        # Nothing is edited, so only fail-closed machinery detection can refuse this.
        self.assert_refused(self.seal(run=run), "checkout-local attribute or filter machinery")

        # The attribute file alone is machinery too, even without filter configuration.
        self.unset_base_echo_filter(run)
        self.assert_refused(self.seal(run=run), "checkout-local attribute or filter machinery")

        self.remove_base_echo_filter(run)
        self.assert_ok(self.seal(run=run))

    def test_replacement_objects_cannot_substitute_the_frozen_baseline(self) -> None:
        weakened = "package app\n\nimport \"testing\"\n\nfunc TestBaseline(t *testing.T) { t.Skip() }\n"

        run = self.root / "run-replace-blob"
        self.assert_ok(self.prepare(run=run))
        checkout = self.checkout(run)
        self.edit("app/main_test.go", weakened, run=run)
        base_object = self.git_text("rev-parse", f"{self.base}:app/main_test.go", cwd=checkout)
        replacement = subprocess.check_output(
            ["git", "-C", str(checkout), "hash-object", "-w", "--stdin"], input=weakened.encode("utf-8")
        ).decode("ascii").strip()
        self.git("replace", base_object, replacement, cwd=checkout)
        # Bypass premise: the replacement serves the weakened bytes under the frozen
        # blob id, so an unguarded seal sees the "frozen" baseline already matching.
        self.assertEqual(
            subprocess.check_output(["git", "-C", str(checkout), "cat-file", "blob", base_object]),
            weakened.encode("utf-8"),
        )
        self.assert_refused(self.seal(run=run), "replacement refs")

        # With the replacement removed the erased baseline test is refused for what it
        # is, and the legitimate clean seal still passes.
        self.git("replace", "-d", base_object, cwd=checkout)
        self.assert_refused(self.seal(run=run), "baseline test")
        self.write(checkout / "app/main_test.go", FILES["app/main_test.go"])
        self.assertEqual(self.assert_ok(self.seal(run=run))["changed_paths"], [])

        # The same bypass through a replaced base commit: ls-tree then serves a tree
        # whose baseline test already matches the weakened worktree.
        self.write(self.repo / "app/main_test.go", weakened)
        self.git("add", "-A")
        self.git("commit", "--quiet", "-m", "fixture weakened baseline")
        weakened_commit = self.git_text("rev-parse", "HEAD")
        run = self.root / "run-replace-commit"
        self.assert_ok(self.prepare(run=run))
        checkout = self.checkout(run)
        self.git("replace", self.base, weakened_commit, cwd=checkout)
        self.edit("app/main_test.go", weakened, run=run)
        self.assertEqual(
            self.git_text("ls-tree", "-r", self.base, "app/main_test.go", cwd=checkout).split()[2],
            self.git_text("rev-parse", f"{weakened_commit}:app/main_test.go", cwd=checkout),
        )
        self.assert_refused(self.seal(run=run), "replacement refs")

        # Grafted ancestry is the same class: refused while present.
        self.git("replace", "-d", self.base, cwd=checkout)
        self.git("replace", "--graft", self.base, weakened_commit, cwd=checkout)
        self.assert_refused(self.seal(run=run), "replacement refs")
        self.git("replace", "-d", self.base, cwd=checkout)
        grafts = checkout / ".git" / "info" / "grafts"
        grafts.parent.mkdir(parents=True, exist_ok=True)
        grafts.write_bytes((self.base + "\n").encode("ascii"))
        self.assert_refused(self.seal(run=run), "grafts")
        grafts.unlink()
        self.assert_refused(self.seal(run=run), "baseline test")
        self.write(checkout / "app/main_test.go", FILES["app/main_test.go"])
        self.assertEqual(self.assert_ok(self.seal(run=run))["changed_paths"], [])
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        sealed = self.assert_ok(self.seal(run=run))
        self.assertEqual([change["path"] for change in sealed["changed_paths"]], ["app/main.go"])

    def test_bound_symlink_targets_may_not_escape_the_manifest_scope(self) -> None:
        run = self.root / "run-symlink-escape"
        task = self.task(allowed_paths=["app/main.go", "app/app.go"])
        self.assert_ok(self.prepare(task=task, run=run))
        checkout = self.checkout(run)

        # Reproduced bypass: app/main.go was bound as link text, so a payload under
        # .git (excluded from the manifest) could change what the gates read without
        # changing any frozen record.
        (checkout / "app" / "main.go").unlink()
        (checkout / ".git" / "payload.go").write_bytes(b"package app\n\nvar Payload = 1\n")
        try:
            os.symlink("../.git/payload.go", checkout / "app" / "main.go")
        except (OSError, NotImplementedError) as error:
            self.skipTest(f"symlink creation is unavailable: {error}")
        self.assert_refused(self.seal(run=run), "points into .git")

        # A target outside the checkout is refused the same way.
        (checkout / "app" / "main.go").unlink()
        self.write(self.root / "outside-payload.go", "package app\n\nvar Outside = 1\n")
        os.symlink("../../outside-payload.go", checkout / "app" / "main.go")
        self.assert_refused(self.seal(run=run), "escapes the checkout")

        # Positive control: an in-tree symlink stays legitimate, its gates still run,
        # and the bytes behind it are bound through the target's own manifest entry.
        (checkout / "app" / "main.go").unlink()
        os.symlink("app.go", checkout / "app" / "main.go")
        first = self.assert_ok(self.seal(run=run))
        self.assertEqual([change["path"] for change in first["changed_paths"]], ["app/main.go"])
        checked = self.assert_ok(self.check(run=run))
        self.assertTrue(checked["passed"])
        self.edit("app/app.go", "package app\n\nvar Bound = 1\n", run=run)
        second = self.assert_ok(self.seal(run=run))
        self.assertNotEqual(first["subject_hash"], second["subject_hash"])
        self.assertIn("app/app.go", [change["path"] for change in second["changed_paths"]])

    def test_sealed_changed_paths_are_reverified(self) -> None:
        run = self.root / "run-reverify-paths"
        self.accepted_run(run=run)
        sealed_path = run / "sealed.json"
        sealed = json.loads(sealed_path.read_text(encoding="utf-8"))
        self.assertEqual(
            [change["path"] for change in sealed["changed_paths"]],
            ["app/main.go", "app/new_test.go"],
        )
        sealed["changed_paths"].append({"path": "docs/notes.md", "status": "M"})
        sealed_path.write_bytes((json.dumps(sealed, indent=2, sort_keys=True) + "\n").encode("utf-8"))
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "changed paths")
        self.assert_refused(self.accept(run=run), "changed paths")

        sealed["changed_paths"].pop()
        sealed_path.write_bytes((json.dumps(sealed, indent=2, sort_keys=True) + "\n").encode("utf-8"))
        self.assert_ok(self.accept(run=run))

    def test_go_event_gate_requires_the_package_to_list_named_tests(self) -> None:
        if shutil.which("go") is None:
            self.skipTest("the Go toolchain is unavailable")
        forged = (
            "package app\n\n"
            "import (\n"
            "\t\"fmt\"\n"
            "\t\"os\"\n"
            "\t\"testing\"\n"
            ")\n\n"
            "func TestMain(m *testing.M) {\n"
            "\tfmt.Println(\"=== RUN   TestWitnessGo\")\n"
            "\tfmt.Println(\"--- PASS: TestWitnessGo (0.00s)\")\n"
            "\tos.Exit(0)\n"
            "}\n\n"
            "func TestWitnessGo(t *testing.T) { t.Fatal(\"must not run\") }\n"
        )
        honest = "package app\n\nimport \"testing\"\n\nfunc TestWitnessGo(t *testing.T) {}\n"
        task = self.task(
            allowed_paths=["app/gate_test.go"],
            gates=[{
                "id": "go-gate",
                "cwd": ".",
                "argv": ["go", "test", "-json", "-count=1", "./app"],
                "go_tests": ["TestWitnessGo"],
                "go_package": "example.com/fixture/app",
            }],
        )

        run = self.root / "run-go-forged"
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/gate_test.go", forged, run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.check(run=run)
        self.assertNotEqual(checked.returncode, 0, checked.stdout + checked.stderr)
        summary = json.loads(checked.stdout)
        self.assertFalse(summary["passed"])
        result = json.loads(
            (run / "checks" / summary["attempt_id"] / "go-gate" / "result.json").read_text(encoding="utf-8")
        )
        self.assertIn("did not list the named Go tests", result["error"])
        receipt_summary = self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assertFalse(receipt_summary["accepted"])
        self.assertTrue(
            any("did not list the named Go tests" in blocker for blocker in receipt_summary["acceptance_blockers"]),
            receipt_summary["acceptance_blockers"],
        )

        run = self.root / "run-go-honest"
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/gate_test.go", honest, run=run)
        self.assert_ok(self.seal(run=run))
        self.assert_ok(self.check(run=run))

    def test_go_list_gate_that_writes_during_execution_stays_verifiable(self) -> None:
        if shutil.which("go") is None:
            self.skipTest("the Go toolchain is unavailable")
        writer = (
            "package app\n\n"
            "import (\n"
            "\t\"os\"\n"
            "\t\"testing\"\n"
            ")\n\n"
            "func TestWitnessGo(t *testing.T) {\n"
            "\tif err := os.WriteFile(\"wrote_during_gate.txt\", []byte(\"x\"), 0o644); err != nil {\n"
            "\t\tt.Fatal(err)\n"
            "\t}\n"
            "}\n"
        )
        task = self.task(
            allowed_paths=["app/gate_test.go"],
            gates=[{
                "id": "go-gate",
                "cwd": ".",
                "argv": ["go", "test", "-json", "-count=1", "./app"],
                "go_tests": ["TestWitnessGo"],
                "go_package": "example.com/fixture/app",
            }],
        )
        run = self.root / "run-go-writer"
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/gate_test.go", writer, run=run)
        self.assert_ok(self.seal(run=run))

        checked = self.check(run=run)
        self.assertNotEqual(checked.returncode, 0, checked.stdout + checked.stderr)
        summary = json.loads(checked.stdout)
        self.assertFalse(summary["passed"])
        written = self.checkout(run) / "app" / "wrote_during_gate.txt"
        self.assertTrue(written.exists(), "fixture premise: the gate wrote into the checkout")
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "does not match the sealed subject")

        written.unlink()
        receipt_summary = self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assertFalse(receipt_summary["accepted"])
        self.assertTrue(
            any("not bound to the sealed subject" in blocker for blocker in receipt_summary["acceptance_blockers"]),
            receipt_summary["acceptance_blockers"],
        )
        self.assert_refused(self.accept(run=run), "nonaccepted row")

    def test_mixed_known_and_unknown_costs_keep_totals_null(self) -> None:
        run = self.root / "run-mixed-costs"
        usage = self.usage_rows()
        usage["phases"][1].update(estimated_cost=None, reported_cost=None)
        usage["phases"][3].update(estimated_cost=None, reported_cost=None)
        self.accepted_run(run=run, usage=usage)
        cost = self.stored_receipt(run)["cost"]
        self.assertIsNone(cost["estimated_cost"])
        self.assertEqual(cost["known_estimated_cost_subtotal"], 4.5)
        self.assertIsNone(cost["reported_cost"])
        self.assertEqual(cost["known_reported_cost_subtotal"], 0)
        self.assertFalse(cost["accounting_complete"])
        self.assertEqual(
            [reason for reason in cost["incomplete_reasons"] if "estimated cost" in reason],
            ["unknown estimated cost in 2 model phase(s)"],
        )
        self.assertTrue(any("no provider-reported cost" in reason for reason in cost["incomplete_reasons"]))

        run = self.root / "run-known-subtotal"
        usage = self.usage_rows()
        usage["phases"][3].update(estimated_cost=None, reported_cost=None)
        self.accepted_run(run=run, usage=usage)
        cost = self.stored_receipt(run)["cost"]
        self.assertEqual(cost["known_estimated_cost_subtotal"], 4.5)
        self.assertIsNone(cost["estimated_cost"])
        self.assertFalse(cost["accounting_complete"])
        self.assertIn("unknown estimated cost in 1 model phase(s)", cost["incomplete_reasons"])

        run = self.root / "run-channel-mixed"
        usage = self.usage_rows()
        phases = usage["phases"]
        phases[0].update(reported_cost=1.0, estimated_cost=None)
        phases[1].update(reported_cost=None, estimated_cost=2.0)
        # One session keeps one observed cost subtotal: the complete 2.0 total and its
        # known subtotal must agree instead of surviving as two versions.
        phases[1]["known_subtotals"]["cost"]["total"]["sum"] = 2.0
        for row in (phases[0], phases[1], phases[3]):
            row["price_version"] = "fixture-price-v1"
        self.accepted_run(run=run, usage=usage)
        cost = self.stored_receipt(run)["cost"]
        self.assertIsNone(cost["reported_cost"])
        self.assertIsNone(cost["estimated_cost"])
        self.assertFalse(cost["accounting_complete"])
        self.assertIn("no provider-reported cost in 2 model phase(s)", cost["incomplete_reasons"])
        self.assertIn("unknown estimated cost in 1 model phase(s)", cost["incomplete_reasons"])

    def test_partial_session_known_subtotals_reach_the_receipt_totals(self) -> None:
        run = self.root / "run-partial-subtotals"
        usage = self.usage_rows()
        partial = usage["phases"][1]
        partial["tokens"] = {"input": None, "output": None, "cached": None}
        partial["estimated_cost"] = None
        partial["known_subtotals"] = {
            "assistant_messages": 2,
            "assistant_messages_with_usage": 1,
            "assistant_messages_with_cost_total": 1,
            "input": {"sum": 100, "messages": 1},
            "output": {"sum": 20, "messages": 1},
            "cached": {"sum": 0, "messages": 1},
            "cost": {"total": {"sum": 0.5, "messages": 1}},
        }
        self.accepted_run(run=run, usage=usage)
        self.assert_ok(self.accept(run=run))
        receipt = self.stored_receipt(run)
        self.assertIsNone(receipt["tokens"]["input"])
        self.assertIsNone(receipt["tokens"]["output"])
        self.assertIsNone(receipt["tokens"]["cached"])
        self.assertEqual(receipt["tokens"]["known_input_subtotal"], 120)
        self.assertEqual(receipt["tokens"]["known_output_subtotal"], 24)
        self.assertEqual(receipt["tokens"]["known_cached_subtotal"], 0)
        self.assertIsNone(receipt["cost"]["estimated_cost"])
        self.assertEqual(receipt["cost"]["known_estimated_cost_subtotal"], 3.5)
        self.assertFalse(receipt["cost"]["accounting_complete"])
        self.assertIn("unknown token usage in 1 model phase(s)", receipt["cost"]["incomplete_reasons"])
        self.assertIn("unknown estimated cost in 1 model phase(s)", receipt["cost"]["incomplete_reasons"])

    def test_usage_requires_the_review_record_identity(self) -> None:
        run = self.root / "run-review-identity"
        self.accepted_run(run=run)

        variant = self.usage_rows()
        variant["phases"][3]["session_ref"] = "other-review-session"
        variant["phases"][3]["model_ids"][0]["model"] = "other-reviewer-model"
        self.assert_refused(
            self.receipt(variant, run=run),
            "usage review phases do not include the independent review session and reviewer model",
        )

        variant = self.usage_rows()
        variant["phases"][1]["session_ref"] = "reviewer-session-1"
        self.assert_refused(self.receipt(variant, run=run))

    def test_frozen_inputs_are_byte_bound(self) -> None:
        run = self.root / "run-frozen-bytes"
        self.accepted_run(run=run)
        frozen = run / "frozen"
        for name, needle, command in (
            ("task.json", "frozen task bytes changed after prepare", "seal"),
            ("routes.json", "frozen routes bytes changed after prepare", "seal"),
            ("review.json", "frozen review record changed after it was imported", "receipt"),
            ("usage.json", "frozen usage rows changed after they were imported", "receipt"),
        ):
            with self.subTest(frozen=name):
                path = frozen / name
                original = self.read(path)
                path.write_bytes(b"{}")
                if command == "seal":
                    refused = self.seal(run=run)
                else:
                    refused = self.receipt(self.usage_rows(), run=run)
                path.write_bytes(original)
                self.assertEqual(self.read(path), original)
                self.assert_refused(refused, needle)
        self.assert_ok(self.accept(run=run))

    def test_protected_classes_cover_both_directions(self) -> None:
        run = self.root / "run-protected-slashless"
        task = self.task(allowed_paths=["docs/notes.md"], protected_paths=["notes.md"])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("docs/notes.md", "changed notes\n", run=run)
        self.assert_refused(self.seal(run=run), "protected asset")
        self.assertEqual(self.read(self.checkout(run) / "docs/notes.md").decode("utf-8"), "changed notes\n")

        run = self.root / "run-allowed-slashless"
        task = self.task(allowed_paths=["notes.md"], protected_paths=[])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("docs/notes.md", "changed notes\n", run=run)
        sealed = self.assert_ok(self.seal(run=run))
        self.assertEqual([change["path"] for change in sealed["changed_paths"]], ["docs/notes.md"])

    def test_serialization_refusals_are_not_tracebacks(self) -> None:
        run = self.root / "run-serialization"
        self.accepted_run(run=run)

        usage = self.usage_rows()
        usage["phases"][0]["metadata"] = {"note": "\ud800"}
        surrogate = self.root / "inputs" / "surrogate-usage.json"
        surrogate.write_bytes((json.dumps(usage, indent=2, sort_keys=True) + "\n").encode("utf-8"))
        refused = self.cli("receipt", "--run-dir", str(run), "--usage", str(surrogate))
        self.assert_refused(refused, "refused")
        self.assertNotIn("Traceback", refused.stderr + refused.stdout)

        refused = self.check("--timeout", "-1", run=run)
        self.assert_refused(refused, "--timeout")

    def test_serialization_refusal_publishes_nothing(self) -> None:
        run = self.root / "run-serialization-atomic"
        self.accepted_run(run=run)
        frozen_usage = self.read(run / "frozen" / "usage.json")
        run_record = self.read(run / "run.json")
        receipt_record = self.read(run / "receipt.json")
        accepted = self.stored_receipt(run)

        usage = self.usage_rows()
        usage["phases"][0]["metadata"] = {"note": "\ud800"}
        refused = self.receipt(usage, run=run)
        self.assert_refused(refused, "not valid UTF-8")
        self.assertNotIn("Traceback", refused.stderr + refused.stdout)

        # The candidate rows validated and the receipt derived, but the receipt could
        # not be serialized: nothing may have been published, or the frozen usage
        # bytes and run record would name the candidate while the old receipt stays
        # and no longer verifies against them.
        self.assertEqual(self.read(run / "frozen" / "usage.json"), frozen_usage)
        self.assertEqual(self.read(run / "run.json"), run_record)
        self.assertEqual(self.read(run / "receipt.json"), receipt_record)
        self.assertEqual(self.stored_receipt(run), accepted)
        summary = self.assert_ok(self.accept(run=run))
        self.assertTrue(summary["accepted"], summary)

    def test_non_utf8_checkout_paths_are_refused(self) -> None:
        if os.name != "posix":
            self.skipTest("non-UTF-8 file names are only constructible on POSIX hosts")
        run = self.root / "run-non-utf8-path"
        self.assert_ok(self.prepare(run=run))
        raw_target = os.path.join(os.fsencode(self.checkout(run)), b"bad-\xff-name.txt")
        try:
            with open(raw_target, "wb") as stream:
                stream.write(b"x\n")
        except OSError as error:
            self.skipTest(f"cannot create a non-UTF-8 path name: {error}")
        self.assert_refused(self.seal(run=run), "not valid UTF-8")

    def test_reparse_point_directories_are_refused(self) -> None:
        if os.name != "nt":
            self.skipTest("directory junctions are a Windows construction")
        run = self.root / "run-reparse"
        self.assert_ok(self.prepare(run=run))
        outside = self.root / "outside"
        outside.mkdir()
        (outside / "thing.txt").write_bytes(b"outside\n")
        junction = self.checkout(run) / "linked"
        created = subprocess.run(
            ["cmd", "/c", "mklink", "/J", str(junction), str(outside)],
            capture_output=True, text=True, check=False,
        )
        if created.returncode != 0:
            self.skipTest(f"cannot create a directory junction: {created.stdout}{created.stderr}")
        try:
            self.assert_refused(self.seal(run=run), "reparse")
        finally:
            try:
                os.rmdir(junction)
            except OSError:
                pass

    def test_exunit_trace_gate_requires_every_named_test_once(self) -> None:
        first = "committed harness session receipts recover through real Go HTTP after listener restarts"
        second = "a stale synchronization is reported as attention by the workspace read model"
        expected = (first, second)
        observations = (
            "\r\x1b[32m  * test " + first + " (0.4ms) [L#7]\x1b[0m",
            "\r  * test " + second + " (0.2ms) [L#12]",
        )
        summary = ("", "Finished in 0.02 seconds", "Result: 2 passed")

        mix = self.install_fake_mix(observations + summary)
        run = self.root / "run-exunit-passing"
        task = self.task(gates=[self.exunit_gate(mix, tests=expected)])
        self.accepted_run(task=task, run=run)
        receipt = self.stored_receipt(run)
        check = receipt["checks"][0]
        self.assertEqual(check["id"], "exunit-gate")
        self.assertEqual(check["exunit_tests"], list(expected))
        self.assertEqual(check["exunit_files"], ["test/witness_test.exs"])
        self.assertEqual(check["exunit_exit_code"], 0)
        self.assertIsNone(check["exunit_error"])
        self.assertEqual(check["env"], {})
        self.assertTrue(receipt["accepted"], receipt["acceptance_blockers"])

        variants = (
            ("missing", (observations[0], "", "Result: 1 passed"), 0, f"missing {second}"),
            ("duplicate", (observations[0], observations[0], observations[1], *summary), 0, f"{first} was observed 2 times"),
            ("skipped", (observations[0], observations[1].replace("(0.2ms)", "(skipped)"), "", "Result: 1 passed, 1 skipped"), 0, f"missing {second}"),
            ("excluded", (observations[0], observations[1].replace("(0.2ms)", "(excluded)"), "", "Result: 1 passed, 1 excluded"), 0, f"missing {second}"),
            (
                "describe-prefixed",
                (observations[0], "\r  * test a describe block " + second + " (0.2ms) [L#12]", "", "Result: 2 passed"),
                0,
                f"missing {second}",
            ),
            ("exited-non-zero", observations + summary, 1, "the traced run exited with 1"),
            ("reported-failures", (observations[0], observations[1], "", "Result: 1/2 passed", "Failed: 1 test"), 0, "the traced run reported 1 failures"),
            ("no-summary", observations, 0, "the traced run reported no ExUnit summary"),
        )
        for label, lines, trace_exit, needle in variants:
            with self.subTest(case=label):
                mix = self.install_fake_mix(lines, trace_exit=trace_exit)
                run = self.root / ("run-exunit-" + label)
                task = self.task(gates=[self.exunit_gate(mix, tests=expected)])
                self.assert_ok(self.prepare(task=task, run=run))
                self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
                self.assert_ok(self.seal(run=run))
                checked = self.check(run=run)
                self.assertNotEqual(checked.returncode, 0, checked.stdout + checked.stderr)
                summary_block = json.loads(checked.stdout)
                self.assertTrue(summary_block["complete"])
                self.assertFalse(summary_block["passed"])
                gate_dir = run / "checks" / summary_block["attempt_id"] / "exunit-gate"
                result = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
                self.assertEqual(
                    result["exunit_argv"],
                    [str(mix), "test", "--trace", "--seed", "0", "test/witness_test.exs"],
                )
                self.assertEqual(result["exunit_exit_code"], trace_exit)
                self.assertFalse(result["passed"])
                self.assertIsNone(result["error"])
                self.assertIn(needle, result["exunit_error"])
                self.assertIn(first, (gate_dir / "exunit.stdout.bin").read_bytes().decode("utf-8"))
                receipt_summary = self.assert_ok(self.receipt(self.usage_rows(), run=run))
                self.assertFalse(receipt_summary["accepted"])
                self.assertTrue(
                    any(result["exunit_error"] in blocker for blocker in receipt_summary["acceptance_blockers"]),
                    receipt_summary["acceptance_blockers"],
                )
                self.assert_refused(self.accept(run=run), "nonaccepted row")

    def test_prepare_validates_exunit_gate_and_environment_fields(self) -> None:
        runner = load_runner_module()
        plain = {"id": "gate", "cwd": ".", "argv": ["mix", "test"]}
        cases = (
            ("tests-without-files", {**plain, "exunit_tests": ["test one"]}, "exunit_tests requires exunit_files"),
            ("files-without-tests", {**plain, "exunit_files": ["test/x_test.exs"]}, "exunit_files requires exunit_tests"),
            (
                "non-mix-argv",
                {"id": "gate", "cwd": ".", "argv": [sys.executable, "-c", "pass"],
                 "exunit_files": ["test/x_test.exs"], "exunit_tests": ["test one"]},
                "requires a mix gate command",
            ),
            (
                "too-many-tests",
                {**plain, "exunit_files": ["test/x_test.exs"], "exunit_tests": [f"test {index}" for index in range(51)]},
                "at most 50 tests",
            ),
            (
                "too-many-files",
                {**plain, "exunit_files": [f"test/{index}_test.exs" for index in range(21)], "exunit_tests": ["test one"]},
                "at most 20 files",
            ),
            (
                "duplicate-tests",
                {**plain, "exunit_files": ["test/x_test.exs"], "exunit_tests": ["test one", "test one"]},
                "must be unique",
            ),
            (
                "nul-test-name",
                {**plain, "exunit_files": ["test/x_test.exs"], "exunit_tests": ["test one\0two"]},
                "must not contain NUL",
            ),
            (
                "empty-files",
                {**plain, "exunit_files": [], "exunit_tests": ["test one"]},
                "at least 1 file",
            ),
            (
                "glob-file",
                {**plain, "exunit_files": ["test/*_test.exs"], "exunit_tests": ["test one"]},
                "without glob",
            ),
            ("secret-env-name", {**plain, "env": {"SYMMETRY_OPERATOR_TOKEN": "x"}}, "secret"),
            ("lowercase-env-name", {**plain, "env": {"lower_case": "x"}}, "env names must match"),
            ("empty-env-value", {**plain, "env": {"SYMMETRY_MODE": ""}}, "non-empty string"),
            (
                "too-many-env-entries",
                {**plain, "env": {f"SYMMETRY_FLAG_{index}": "1" for index in range(9)}},
                "at most 8 entries",
            ),
            ("non-object-env", {**plain, "env": ["SYMMETRY_MODE"]}, "env must be a JSON object"),
        )
        for index, (label, gate, needle) in enumerate(cases):
            with self.subTest(case=label):
                with self.assertRaises(runner.Refused, msg=f"validate_gate accepted {label}") as refusal:
                    runner.validate_gate(gate, 0)
                self.assertIn(needle, str(refusal.exception))
                run = self.root / f"run-gate-validation-{index}"
                self.assert_refused(self.prepare(task=self.task(gates=[gate]), run=run), needle)
                self.assertFalse(run.exists(), "a refused prepare must not leave a run directory")
        # The whole-task secret scan and validate_gate are independent guards: the scan
        # refuses SYMMETRY_OPERATOR_TOKEN even before the gate's own allowlist applies.
        with self.assertRaises(runner.Refused) as refusal:
            runner.validate_gate({**plain, "env": {"SYMMETRY_OPERATOR_TOKEN": "x"}}, 0)
        self.assertIn("looks like a secret", str(refusal.exception))

    def test_gate_environment_is_applied_and_recorded(self) -> None:
        probe = "import os\nprint(os.environ.get('SYMMETRY_FIXTURE_MODE', 'missing'))\n"
        run = self.root / "run-gate-env"
        task = self.task(gates=[{
            "id": "env-gate",
            "cwd": ".",
            "argv": [sys.executable, "-c", probe],
            "env": {"SYMMETRY_FIXTURE_MODE": "measured-fixture"},
        }])
        self.accepted_run(task=task, run=run)
        gate_dir = run / "checks" / "attempt-0001" / "env-gate"
        self.assertEqual(
            (gate_dir / "stdout.bin").read_bytes().decode("utf-8").splitlines(),
            ["measured-fixture"],
        )
        result = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
        self.assertEqual(result["env"], {"SYMMETRY_FIXTURE_MODE": "measured-fixture"})
        receipt = self.stored_receipt(run)
        self.assertEqual(receipt["checks"][0]["id"], "env-gate")
        self.assertEqual(receipt["checks"][0]["env"], {"SYMMETRY_FIXTURE_MODE": "measured-fixture"})
        self.assert_ok(self.accept(run=run))

    def test_prepare_environment_json_records_the_measured_surface(self) -> None:
        measured = {"cpu_count": "8", "kernel": "6.8.0-fixture", "python_build": "fixture-build"}
        environment_path = self.write_json(self.root / "inputs" / "environment.json", measured)
        run = self.root / "run-measured"
        self.accepted_run(run=run, environment_json=environment_path)
        self.assertEqual(self.run_record(run)["environment"]["measured"], measured)
        self.assertEqual(
            sorted(self.run_record(run)["environment"]),
            ["git_version", "machine", "measured", "platform", "python_version"],
        )
        self.assertEqual(self.stored_receipt(run)["environment"]["measured"], measured)
        self.assert_ok(self.accept(run=run))

        cases = (
            ("secret-name", {"operator_token": "x"}, "looks like a secret"),
            ("bad-name", {"Kernel": "x"}, "names must match"),
            ("empty-value", {"kernel": ""}, "non-empty single-line"),
            ("multiline-value", {"kernel": "one\ntwo"}, "non-empty single-line"),
            ("too-many-entries", {f"field_{index}": "1" for index in range(25)}, "at most 24 entries"),
            ("not-an-object", ["kernel"], "must be a JSON object"),
        )
        for index, (label, value, needle) in enumerate(cases):
            with self.subTest(case=label):
                refused_run = self.root / f"run-measured-{index}"
                path = self.write_json(self.root / "inputs" / f"environment-{index}.json", value)
                self.assert_refused(self.prepare(run=refused_run, environment_json=path), needle)
                self.assertFalse(refused_run.exists())

        tampered = self.run_record(run)
        tampered["environment"]["measured"]["operator_token"] = "x"
        self.write_json(run / "run.json", tampered)
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "looks like a secret")

    def test_exunit_gate_evidence_is_reverified_from_stored_bytes(self) -> None:
        first = "committed harness session receipts recover through real Go HTTP after listener restarts"
        second = "a stale synchronization is reported as attention by the workspace read model"
        expected = (first, second)
        mix = self.install_fake_mix((
            "\r\x1b[32m  * test " + first + " (0.4ms) [L#7]\x1b[0m",
            "\r  * test " + second + " (0.2ms) [L#12]",
            "",
            "Result: 2 passed",
        ))
        run = self.root / "run-exunit-verify"
        task = self.task(gates=[self.exunit_gate(mix, tests=expected)])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        self.assert_ok(self.check(run=run))
        gate_dir = run / "checks" / "attempt-0001" / "exunit-gate"
        result_path = gate_dir / "result.json"
        original_raw = result_path.read_bytes()
        original_stdout = (gate_dir / "exunit.stdout.bin").read_bytes()
        self.assertEqual(sorted(entry.name for entry in gate_dir.iterdir()),
                         ["exunit.stderr.bin", "exunit.stdout.bin", "result.json", "stderr.bin", "stdout.bin"])

        def mutate_exit_code(result, directory):
            result["exunit_exit_code"] = 7

        def mutate_tests(result, directory):
            result["exunit_tests"] = ["test other"]

        def mutate_argv(result, directory):
            result["exunit_argv"] = ["mix", "test"]

        def mutate_env(result, directory):
            result["env"] = {"SYMMETRY_MODE": "1"}

        def mutate_digest(result, directory):
            result["exunit_stdout_sha256"] = "sha256:" + "0" * 64

        def mutate_timed_out(result, directory):
            result["exunit_timed_out"] = True

        def drop_stdout(result, directory):
            (directory / "exunit.stdout.bin").unlink()

        def add_stray(result, directory):
            (directory / "exunit-extra.bin").write_bytes(b"x")

        cases = (
            ("exit-code", mutate_exit_code, "claims passed"),
            ("tests", mutate_tests, "does not match the frozen gate exunit_tests"),
            ("argv", mutate_argv, "does not match the derived ExUnit trace command"),
            ("env", mutate_env, "does not match the frozen gate env"),
            ("stdout-digest", mutate_digest, "changed after it was captured"),
            ("timed-out", mutate_timed_out, "disagrees with its exit code"),
            ("missing-stdout", drop_stdout, "unexpected files"),
            ("stray-evidence", add_stray, "unexpected files"),
        )
        for label, mutate, needle in cases:
            with self.subTest(case=label):
                result = json.loads(original_raw.decode("utf-8"))
                mutate(result, gate_dir)
                self.write_json(result_path, result)
                refused = self.receipt(self.usage_rows(), run=run)
                result_path.write_bytes(original_raw)
                (gate_dir / "exunit.stdout.bin").write_bytes(original_stdout)
                stray = gate_dir / "exunit-extra.bin"
                if stray.exists():
                    stray.unlink()
                self.assert_refused(refused, needle)
        self.assert_ok(self.receipt(self.usage_rows(), run=run))

        failing = self.install_fake_mix((), gate_exit=3)
        run = self.root / "run-exunit-gate-failure"
        task = self.task(gates=[self.exunit_gate(failing, tests=expected)])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.check(run=run)
        self.assertNotEqual(checked.returncode, 0, checked.stdout)
        gate_dir = run / "checks" / "attempt-0001" / "exunit-gate"
        result = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
        self.assertEqual(result["exit_code"], 3)
        self.assertIsNone(result["exunit_error"])
        self.assertIsNone(result["exunit_exit_code"])
        self.assertFalse(result["exunit_timed_out"])
        self.assertEqual(
            result["exunit_argv"], [str(failing), "test", "--trace", "--seed", "0", "test/witness_test.exs"]
        )
        self.assertEqual(
            sorted(entry.name for entry in gate_dir.iterdir()),
            ["result.json", "stderr.bin", "stdout.bin"],
        )
        self.assert_ok(self.receipt(self.usage_rows(), run=run))

        run = self.root / "run-plain-verify"
        self.assert_ok(self.prepare(task=self.task(gates=[self.gate_plain()]), run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        self.assert_ok(self.check(run=run))
        gate_dir = run / "checks" / "attempt-0001" / "plain"
        original = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
        self.assertIsNone(original["exunit_argv"])
        self.assertIsNone(original["exunit_error"])
        (gate_dir / "exunit.stdout.bin").write_bytes(b"x")
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "unexpected files")
        (gate_dir / "exunit.stdout.bin").unlink()
        self.assert_ok(self.receipt(self.usage_rows(), run=run))
        receipt = self.stored_receipt(run)
        self.assertIsNone(receipt["checks"][0]["exunit_exit_code"])
        self.assertIsNone(receipt["checks"][0]["exunit_error"])

    def test_tool_cache_run_state_inside_the_checkout_is_refused(self) -> None:
        run = self.root / "run-tool-cache"
        self.assert_ok(self.prepare(run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        # vitest/vite run state is redirected outside a sealed checkout, so a cache
        # file that still appears inside one is ordinary source the manifest binds.
        self.edit("app/node_modules/.vite/vitest/results.json", '{"failed":[]}\n', run=run)
        self.edit("app/node_modules/.vitest-cache/ab/entry.json", "{}\n", run=run)
        self.edit("app/node_modules/.vite-temp/stale.tmp", "x\n", run=run)
        self.assert_refused(self.seal(run=run), "outside allowed_paths")

    def test_package_files_under_node_modules_stay_bound(self) -> None:
        run = self.root / "run-node-modules-package"
        self.assert_ok(self.prepare(run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.edit("app/node_modules/react/index.js", "module.exports = {};\n", run=run)
        self.assert_refused(self.seal(run=run), "outside allowed_paths")
        (self.checkout(run) / "app" / "node_modules" / "react" / "index.js").unlink()
        self.assert_ok(self.seal(run=run))

    def test_gate_writing_tool_cache_state_into_the_checkout_fails_closed(self) -> None:
        write_cache = (
            "import os\n"
            "os.makedirs('node_modules/.vite/vitest', exist_ok=True)\n"
            "open('node_modules/.vite/vitest/results.json', 'w').write('{\"failed\": []}')\n"
        )
        run = self.root / "run-tool-cache-gate"
        self.assert_ok(self.prepare(task=self.task(gates=[self.gate_plain(code=write_cache)]), run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.check(run=run)
        self.assertNotEqual(checked.returncode, 0, checked.stdout)
        result = json.loads(
            (run / "checks" / "attempt-0001" / "plain" / "result.json").read_text(encoding="utf-8")
        )
        self.assertIn("source changed while executing this gate", result["error"])
        self.assertFalse(result["passed"])

    def test_gate_environment_redirects_tool_state_outside_the_checkout(self) -> None:
        printer = (
            "import os\n"
            "print(os.environ['SYMMETRY_TOOL_CACHE_DIR'])\n"
            "print(os.environ['SYMMETRY_DISABLE_TOOL_CACHES'])\n"
        )
        run = self.root / "run-tool-state"
        self.assert_ok(self.prepare(task=self.task(gates=[self.gate_plain(gate_id="plain", code=printer)]), run=run))
        # prepare gives the run store the empty `node_modules` Vite's config loader
        # resolves `.vite-temp` against, outside every sealed checkout.
        self.assertTrue((run / "node_modules").is_dir())
        self.assertEqual(list((run / "node_modules").iterdir()), [])
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.assert_ok(self.check(run=run))
        gate_dir = run / "checks" / checked["attempt_id"] / "plain"
        printed = (gate_dir / "stdout.bin").read_text(encoding="utf-8").splitlines()
        # The state is per attempt, so nothing an earlier attempt cached can be read,
        # and the cache channels that execute earlier transformed code are disabled.
        self.assertEqual(printed[0].strip(), str(run / "tool-state" / checked["attempt_id"] / "plain"))
        self.assertEqual(printed[1].strip(), "1")
        self.assertFalse(pathlib.Path(printed[0].strip()).is_relative_to(self.checkout(run)))
        result = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
        self.assertEqual(result["tool_state_dir"], f"tool-state/{checked['attempt_id']}/plain")
        self.assert_ok(self.import_review(self.review_record(checked, run=run), run=run))
        self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assertEqual(
            self.stored_receipt(run)["checks"][0]["tool_state_dir"],
            f"tool-state/{checked['attempt_id']}/plain",
        )
        self.assert_ok(self.accept(run=run))

    @staticmethod
    def vitest_report(entries, *, success=True, total=None, failed=0, test_results=None):
        """A vitest JSON reporter document for `entries` = (full name, status, ancestors)."""
        assertions = [
            {
                "ancestorTitles": list(ancestors),
                "duration": 1.5,
                "failureMessages": [],
                "fullName": name,
                "status": status,
                "title": name.rsplit(" > ", 1)[-1],
            }
            for name, status, ancestors in entries
        ]
        document = {
            "numTotalTestSuites": 1,
            "numPassedTestSuites": 1 if success else 0,
            "numFailedTestSuites": 0 if success else 1,
            "numTotalTests": len(entries) if total is None else total,
            "numPassedTests": sum(1 for _name, status, _ancestors in entries if status == "passed"),
            "numFailedTests": failed,
            "success": success,
            "startTime": 1763420000000,
            "testResults": [
                {
                    "assertionResults": assertions,
                    "endTime": 1763420000123,
                    "message": "",
                    "name": "/fixture/src/witness.test.tsx",
                    "startTime": 1763420000000,
                    "status": "passed" if success else "failed",
                }
            ],
        }
        if test_results is not None:
            document["testResults"] = test_results
        return document

    def test_vitest_report_gate_requires_every_named_test_once(self) -> None:
        first = "renders one row per served boolean capability member"
        second = "never claims a capability the record does not carry"
        expected = (first, second)
        entry = self.install_fake_vitest(
            self.vitest_report(((first, "passed", ()), (second, "passed", ())))
        )
        run = self.root / "run-vitest-passing"
        task = self.task(gates=[self.vitest_gate(entry, tests=expected)])
        self.accepted_run(task=task, run=run)
        receipt = self.stored_receipt(run)
        check = receipt["checks"][0]
        self.assertEqual(check["id"], "vitest-gate")
        self.assertEqual(check["vitest_tests"], list(expected))
        self.assertEqual(check["vitest_files"], ["src/witness.test.tsx"])
        self.assertEqual(check["vitest_exit_code"], 0)
        self.assertIsNone(check["vitest_error"])
        self.assertEqual(check["env"], {})
        self.assertTrue(receipt["accepted"], receipt["acceptance_blockers"])
        gate_dir = run / "checks" / "attempt-0001" / "vitest-gate"
        result = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
        self.assertEqual(
            result["vitest_argv"],
            [
                "node", str(entry), "run", "--reporter=json", "--outputFile",
                str(gate_dir / "vitest-report.json"), "src/witness.test.tsx",
            ],
        )
        self.assertTrue((gate_dir / "vitest-report.json").is_file())

    @classmethod
    def vitest_refusal_variants(cls):
        """The malformed-report documents the structured-report refusals are pinned with.

        Each row is `(label, document, report_exit, write_report, expected refusal text)`.
        The gate test below runs every row through the real check, and
        `test_vitest_report_refusal_coverage_is_derived` measures which refusal shapes
        those same documents reach, so a record's covered/uncovered statement follows
        from the documents this suite executes.
        """
        first, second = VITEST_NAMED_TESTS
        return (
            (
                "missing",
                cls.vitest_report(((first, "passed", ()),)),
                0,
                True,
                f"missing {second}",
            ),
            (
                "duplicate",
                cls.vitest_report(((first, "passed", ()), (first, "passed", ()), (second, "passed", ()))),
                0,
                True,
                f"{first} was reported 2 times",
            ),
            (
                "nested",
                cls.vitest_report(((first, "passed", ()), (second, "passed", ("a describe block",)))),
                0,
                True,
                f"the named test {second!r} is declared inside a describe block",
            ),
            (
                "summary-failed",
                cls.vitest_report(
                    ((first, "passed", ()), (second, "failed", ())), success=False, failed=1
                ),
                0,
                True,
                "does not record a successful run",
            ),
            (
                "status-failed",
                cls.vitest_report(((first, "passed", ()), (second, "failed", ()))),
                0,
                True,
                f"the named test {second!r} finished with status 'failed'",
            ),
            (
                "too-few-total-tests",
                cls.vitest_report(((first, "passed", ()), (second, "passed", ())), total=1),
                0,
                True,
                "carries no complete test summary",
            ),
            (
                "no-results",
                cls.vitest_report((), test_results=[]),
                0,
                True,
                "carries no test results",
            ),
            (
                "no-summary",
                {"success": True, "testResults": [{"assertionResults": [], "name": "x", "status": "passed"}]},
                0,
                True,
                "carries no complete test summary",
            ),
            (
                "failed-count",
                cls.vitest_report(((first, "passed", ()), (second, "passed", ())), failed=1),
                0,
                True,
                "records failing tests",
            ),
            (
                "malformed-file-result",
                {"success": True, "numTotalTests": 2, "numFailedTests": 0, "testResults": ["not-a-file-result"]},
                0,
                True,
                "carries a malformed file result",
            ),
            (
                "assertions-not-a-list",
                {"success": True, "numTotalTests": 2, "numFailedTests": 0,
                 "testResults": [{"assertionResults": "nope"}]},
                0,
                True,
                "carries a file result without assertions",
            ),
            (
                "assertion-without-fields",
                {"success": True, "numTotalTests": 2, "numFailedTests": 0,
                 "testResults": [{"assertionResults": [{"fullName": 1}]}]},
                0,
                True,
                "carries an assertion without a name, status or ancestor list",
            ),
            ("no-report", cls.vitest_report(((first, "passed", ()), (second, "passed", ()))), 0, False,
             "wrote no readable JSON report"),
            (
                "report-exit-nonzero",
                cls.vitest_report(((first, "passed", ()), (second, "passed", ()))),
                1,
                True,
                "the derived vitest run exited with code 1",
            ),
            ("not-an-object", "text", 0, True, "is not a JSON object"),
        )

    def test_vitest_report_gate_refuses_missing_failing_or_nested_names(self) -> None:
        expected = VITEST_NAMED_TESTS
        for label, report, report_exit, write_report, needle in self.vitest_refusal_variants():
            with self.subTest(case=label):
                entry = self.install_fake_vitest(
                    report, report_exit=report_exit, write_report=write_report
                )
                run = self.root / ("run-vitest-" + label)
                task = self.task(gates=[self.vitest_gate(entry, tests=expected)])
                self.assert_ok(self.prepare(task=task, run=run))
                self.bind_tree(run)
                self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
                self.assert_ok(self.seal(run=run))
                checked = self.check(run=run)
                self.assertNotEqual(checked.returncode, 0, checked.stdout + checked.stderr)
                summary_block = json.loads(checked.stdout)
                self.assertTrue(summary_block["complete"])
                self.assertFalse(summary_block["passed"])
                gate_dir = run / "checks" / summary_block["attempt_id"] / "vitest-gate"
                result = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
                self.assertEqual(
                    result["vitest_argv"],
                    [
                        "node", str(entry), "run", "--reporter=json", "--outputFile",
                        str(gate_dir / "vitest-report.json"), "src/witness.test.tsx",
                    ],
                )
                self.assertEqual(result["vitest_exit_code"], report_exit)
                self.assertFalse(result["passed"])
                self.assertIsNone(result["error"])
                self.assertIn(needle, result["vitest_error"])
                self.assertTrue((gate_dir / "vitest-report.json").is_file())
                receipt_summary = self.assert_ok(self.receipt(self.usage_rows(), run=run))
                self.assertFalse(receipt_summary["accepted"])
                self.assertTrue(
                    any(result["vitest_error"] in blocker for blocker in receipt_summary["acceptance_blockers"]),
                    receipt_summary["acceptance_blockers"],
                )
                self.assert_refused(self.accept(run=run), "nonaccepted row")

    def test_vitest_report_refusal_coverage_is_derived(self) -> None:
        """Derive which refusal shapes of `vitest_report_detail` the suite reaches.

        The published records quote a classification - how many refusal shapes the
        function has and which of them no case reaches - so this test recomputes it from
        artifacts instead of trusting a hand-written list: the inventory is parsed out of
        the runner's own syntax tree, and coverage is measured by running the very
        documents `vitest_refusal_variants` feeds through the gate test above while
        tracing which of those returns executes. Adding or removing a refusal, or losing
        one of the cases, changes the derived classification and fails here, so a
        published count cannot go stale quietly. The printed line is the recomputable
        figure the records quote.
        """
        runner = load_runner_module()
        inventory = refusal_returns(SCRIPT.read_text(encoding="utf-8"), "vitest_report_detail")
        inventory_lines = {line for line, _kind, _text in inventory}
        self.assertEqual(len(inventory_lines), len(inventory), "two refusals share a source line")
        reached = {}
        for label, document, report_exit, write_report, needle in self.vitest_refusal_variants():
            with self.subTest(case=label):
                raw = b"" if not write_report else json.dumps(document).encode("utf-8")
                message, executed = exercise(
                    runner.vitest_report_detail,
                    lambda: runner.vitest_report_detail(VITEST_NAMED_TESTS, report_exit, raw),
                )
                self.assertIn(needle, message or "", label)
                hit = sorted(inventory_lines & set(executed))
                self.assertEqual(len(hit), 1, f"{label} reached {hit} of {sorted(inventory_lines)}")
                reached[hit[0]] = label
        uncovered = [(line, kind, text) for line, kind, text in inventory if line not in reached]
        print(
            f"vitest_report_detail refusal shapes: {len(inventory)} returns, "
            f"{len(reached)} reached by the suite's malformed-report cases, "
            f"{len(uncovered)} unreached: "
            + ", ".join(f"{text} (line {line})" for line, _kind, text in uncovered)
        )
        self.assertEqual(
            [(kind, text) for _line, kind, text in uncovered],
            [
                ("literal", '"the derived vitest run timed out"'),
                ("literal", '"the derived vitest report carries a malformed assertion"'),
            ],
        )
        self.assertEqual((len(inventory), len(reached), len(uncovered)), (17, 15, 2))

    def test_vitest_report_runs_only_when_the_gate_command_passes(self) -> None:
        first = "renders one row per served boolean capability member"
        second = "never claims a capability the record does not carry"
        expected = (first, second)
        entry = self.install_fake_vitest(
            self.vitest_report(((first, "passed", ()), (second, "passed", ()))), gate_exit=3
        )
        run = self.root / "run-vitest-gate-failure"
        task = self.task(gates=[self.vitest_gate(entry, tests=expected)])
        self.assert_ok(self.prepare(task=task, run=run))
        self.bind_tree(run)
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.check(run=run)
        self.assertNotEqual(checked.returncode, 0, checked.stdout)
        gate_dir = run / "checks" / "attempt-0001" / "vitest-gate"
        result = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
        self.assertEqual(result["exit_code"], 3)
        self.assertIsNone(result["vitest_error"])
        self.assertIsNone(result["vitest_exit_code"])
        self.assertFalse(result["vitest_timed_out"])
        self.assertEqual(
            sorted(entry_name.name for entry_name in gate_dir.iterdir()),
            ["result.json", "stderr.bin", "stdout.bin"],
        )
        self.assert_ok(self.receipt(self.usage_rows(), run=run))

    def test_if_absent_vitest_carrier_fields_stay_null(self) -> None:
        run = self.root / "run-vitest-plain-verify"
        self.assert_ok(self.prepare(task=self.task(gates=[self.gate_plain()]), run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        self.assert_ok(self.check(run=run))
        gate_dir = run / "checks" / "attempt-0001" / "plain"
        original = json.loads((gate_dir / "result.json").read_text(encoding="utf-8"))
        self.assertIsNone(original["vitest_argv"])
        self.assertIsNone(original["vitest_error"])
        (gate_dir / "vitest-report.json").write_bytes(b"{}")
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "unexpected files")
        (gate_dir / "vitest-report.json").unlink()
        self.assert_ok(self.receipt(self.usage_rows(), run=run))
        receipt = self.stored_receipt(run)
        self.assertIsNone(receipt["checks"][0]["vitest_exit_code"])
        self.assertIsNone(receipt["checks"][0]["vitest_error"])
        self.assertIsNone(receipt["checks"][0]["vitest_tests"])
        self.assertIsNone(receipt["checks"][0]["vitest_files"])

    def test_prepare_validates_vitest_gate_fields(self) -> None:
        runner = load_runner_module()
        plain = {"id": "gate", "cwd": ".", "argv": ["node", "vitest.mjs", "run"]}
        cases = (
            ("tests-without-files", {**plain, "vitest_tests": ["one"]}, "vitest_tests requires vitest_files"),
            (
                "files-without-tests",
                {**plain, "vitest_files": ["src/x.test.tsx"]},
                "vitest_files requires vitest_tests",
            ),
            (
                "non-vitest-argv",
                {"id": "gate", "cwd": ".", "argv": ["node", "eslint/bin/eslint.js", "."],
                 "vitest_files": ["src/x.test.tsx"], "vitest_tests": ["one"]},
                "requires a vitest entry-point gate command",
            ),
            (
                "too-many-tests",
                {**plain, "vitest_files": ["src/x.test.tsx"],
                 "vitest_tests": [f"case {index}" for index in range(51)]},
                "at most 50 tests",
            ),
            (
                "too-many-files",
                {**plain, "vitest_files": [f"src/{index}.test.tsx" for index in range(21)],
                 "vitest_tests": ["one"]},
                "at most 20 files",
            ),
            (
                "duplicate-tests",
                {**plain, "vitest_files": ["src/x.test.tsx"], "vitest_tests": ["one", "one"]},
                "must be unique",
            ),
            (
                "nul-test-name",
                {**plain, "vitest_files": ["src/x.test.tsx"], "vitest_tests": ["one\0two"]},
                "must not contain NUL",
            ),
            ("empty-files", {**plain, "vitest_files": [], "vitest_tests": ["one"]}, "at least 1 file"),
            (
                "glob-file",
                {**plain, "vitest_files": ["src/*.test.tsx"], "vitest_tests": ["one"]},
                "without glob",
            ),
        )
        for label, gate, needle in cases:
            with self.subTest(case=label):
                with self.assertRaises(runner.Refused, msg=f"validate_gate accepted {label}") as refusal:
                    runner.validate_gate(gate, 0)
                self.assertIn(needle, str(refusal.exception))

    def test_vitest_report_evidence_is_reverified_from_stored_bytes(self) -> None:
        first = "renders one row per served boolean capability member"
        second = "never claims a capability the record does not carry"
        expected = (first, second)
        entry = self.install_fake_vitest(
            self.vitest_report(((first, "passed", ()), (second, "passed", ())))
        )
        run = self.root / "run-vitest-verify"
        task = self.task(gates=[self.vitest_gate(entry, tests=expected)])
        self.accepted_run(task=task, run=run)
        gate_dir = run / "checks" / "attempt-0001" / "vitest-gate"
        result_path = gate_dir / "result.json"
        original_raw = result_path.read_bytes()
        report_raw = (gate_dir / "vitest-report.json").read_bytes()
        self.assertEqual(
            sorted(entry_name.name for entry_name in gate_dir.iterdir()),
            [
                "result.json", "stderr.bin", "stdout.bin",
                "vitest-report.json", "vitest.stderr.bin", "vitest.stdout.bin",
            ],
        )
        cases = (
            ("events", lambda value: value.update(vitest_tests=["other"]), "does not match the frozen gate vitest_tests"),
            ("files", lambda value: value.update(vitest_files=["src/other.test.tsx"]), "does not match the frozen gate vitest_files"),
            ("exit-code", lambda value: value.update(vitest_exit_code=7), "but its evidence says"),
            (
                "commands",
                lambda value: value.update(vitest_argv=["node", "vitest.mjs", "run"]),
                "does not match the derived vitest report command",
            ),
            (
                "report-digest",
                lambda value: value.update(vitest_report_sha256="sha256:" + "0" * 64),
                "changed after it was captured",
            ),
            (
                "stdout-digest",
                lambda value: value.update(vitest_stdout_sha256="sha256:" + "0" * 64),
                "changed after it was captured",
            ),
            ("timed-out", lambda value: value.update(vitest_timed_out=True), "disagrees with its exit code"),
        )
        for label, mutate, needle in cases:
            with self.subTest(case=label):
                result = json.loads(original_raw.decode("utf-8"))
                mutate(result)
                self.write_json(result_path, result)
                self.assert_refused(self.receipt(self.usage_rows(), run=run), needle)
                result_path.write_bytes(original_raw)

        (gate_dir / "vitest-report.json").unlink()
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "contains unexpected files")
        (gate_dir / "vitest-report.json").write_bytes(report_raw)
        stray = gate_dir / "vitest-extra.bin"
        stray.write_bytes(b"x")
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "contains unexpected files")
        stray.unlink()
        self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assert_ok(self.accept(run=run))

    def test_measured_surface_records_host_paths_as_hashed_tokens(self) -> None:
        windows_root = "C:/Users/fixture/AppData/Local/Temp/eval-run/build"
        posix_root = "/home/fixture/tmp/eval-run/deps"

        def token(raw: str) -> str:
            return "sha256:" + hashlib.sha256(raw.encode("utf-8")).hexdigest()

        measured = {
            "build_root": f"{windows_root} (outside the checkout)",
            "deps_root": f"{posix_root} (outside the checkout)",
            "kernel": "6.8.0-fixture",
        }
        run = self.root / "run-measured-redaction"
        path = self.write_json(self.root / "inputs" / "environment-redaction.json", measured)
        self.accepted_run(run=run, environment_json=path)
        stored = self.run_record(run)["environment"]["measured"]
        self.assertEqual(stored["build_root"], f"{token(windows_root)} (outside the checkout)")
        self.assertEqual(stored["deps_root"], f"{token(posix_root)} (outside the checkout)")
        self.assertEqual(stored["kernel"], "6.8.0-fixture")
        self.assertNotIn("fixture/AppData", json.dumps(stored))
        receipt_measured = self.stored_receipt(run)["environment"]["measured"]
        self.assertEqual(receipt_measured["build_root"], f"{token(windows_root)} (outside the checkout)")
        self.assertEqual(receipt_measured["deps_root"], f"{token(posix_root)} (outside the checkout)")

    def test_dependency_tree_digest_is_deterministic_and_content_bound(self) -> None:
        runner = load_runner_module()
        first = self.root / "tree-first"
        second = self.root / "tree-second"
        for tree in (first, second):
            self.write(tree / "z-package" / "index.js", "module.exports = 1\n")
            self.write(tree / "a-package" / "dist" / "index.js", "module.exports = 2\n")
            self.write(tree / "b-package" / "index.js", "third\n")
        digest_first, entries_first = runner.dependency_tree_digest(first)
        self.assertEqual((digest_first, entries_first), runner.dependency_tree_digest(second))
        self.assertEqual(entries_first, 3)
        self.assertTrue(digest_first.startswith("sha256:"))
        self.write(second / "b-package" / "index.js", "changed\n")
        self.assertNotEqual(runner.dependency_tree_digest(second)[0], digest_first)
        # An in-tree link is bound by its target text and pruned instead of followed.
        try:
            os.symlink("a-package", second / "linked-target")
        except (OSError, NotImplementedError) as error:
            self.skipTest(f"symlink creation is unavailable: {error}")
        with_link, link_entries = runner.dependency_tree_digest(second)
        self.assertEqual(link_entries, 4)
        self.assertNotEqual(with_link, digest_first)
        # A link whose target resolves outside the tree binds that target's content:
        # pnpm's workspace links (`@symmetry/contracts -> ../../contracts`) point at
        # source the gates execute, and the link text alone would not bind it.
        workspace_package = self.root / "workspace-package"
        self.write(workspace_package / "index.js", "module.exports = 1\n")
        os.symlink(workspace_package, second / "workspace-link")
        linked_digest, entries = runner.dependency_tree_digest(second)
        self.assertEqual(entries, 5)
        self.write(workspace_package / "index.js", "module.exports = 2\n")
        self.assertNotEqual(runner.dependency_tree_digest(second)[0], linked_digest)
        # Editing that target back reproduces the linked digest, and the digest stays
        # stable across repeated walks of an unchanged tree.
        self.write(workspace_package / "index.js", "module.exports = 1\n")
        self.assertEqual(runner.dependency_tree_digest(second), (linked_digest, 5))
        self.assertEqual(runner.dependency_tree_digest(second), (linked_digest, 5))

    def test_bind_dependencies_records_the_provisioning_step(self) -> None:
        run = self.run_dir()
        self.assert_ok(self.prepare(run=run))
        tree = self.root / "runs" / "node_modules"
        self.write(tree / "react" / "index.js", "module.exports = {}\n")
        self.write(tree / ".modules.yaml", "hoistPattern: []\n")
        log = self.root / "pnpm-install.log"
        log.write_bytes(b"Progress: resolved 1, reused 0, downloaded 1\nDone in 1.2s\n")
        bound = self.assert_ok(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree), "--log", str(log)
        ))
        self.assertEqual(bound["tree_entries"], 2)
        self.assertEqual((run / "provisioning" / "install.log").read_bytes(), log.read_bytes())
        provisioning = self.run_record(run)["environment"]["provisioning"]
        self.assertEqual(provisioning["tree_digest"], bound["tree_digest"])
        self.assertEqual(provisioning["tree_entries"], 2)
        # The route is run-relative, so the digest is recomputable from the stored run
        # directory alone (a redacted absolute route could not be re-verified).
        self.assertEqual(provisioning["tree"], "../runs/node_modules")
        self.assertEqual(provisioning["install_log"], "provisioning/install.log")
        self.assertEqual(provisioning["install_log_sha256"], bound["install_log_sha256"])
        self.assertEqual(provisioning["install_log_bytes"], len(log.read_bytes()))
        # The recorded digest re-verifies against the tree and refuses a changed tree.
        self.assert_ok(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree), "--check"
        ))
        self.write(tree / "react" / "index.js", "module.exports = { changed: true }\n")
        # Loading the run already recomputes the digest, before --check can compare it.
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "changed after it was recorded")
        self.write(tree / "react" / "index.js", "module.exports = {}\n")
        # Pointing --check at a different tree is a different refusal.
        other = self.root / "runs" / "other-node_modules"
        self.write(other / "react" / "index.js", "module.exports = {}\n")
        self.assert_refused(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(other), "--check"
        ), "tree no longer matches")
        self.assert_ok(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree), "--check"
        ))
        # One provisioning step per run, and the receipt states it.
        self.assert_refused(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree), "--log", str(log)
        ), "already records a provisioning step")
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.assert_ok(self.check(run=run))
        self.assert_ok(self.import_review(self.review_record(checked, run=run), run=run))
        self.assert_ok(self.receipt(self.usage_rows(), run=run))
        self.assertEqual(
            self.stored_receipt(run)["environment"]["provisioning"]["tree_digest"], bound["tree_digest"]
        )
        # The stored log copy is re-verified on every load.
        (run / "provisioning" / "install.log").write_bytes(b"tampered\n")
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "provisioning log changed")

    def test_bind_dependencies_refuses_incomplete_or_unsafe_provisioning(self) -> None:
        run = self.run_dir()
        self.assert_ok(self.prepare(run=run))
        tree = self.root / "runs" / "node_modules"
        self.write(tree / "react" / "index.js", "module.exports = {}\n")
        log = self.root / "pnpm-install.log"
        log.write_bytes(b"install output\n")
        # A tree inside the sealed checkout is refused: the point of the record is what
        # lives outside it.
        inside = self.checkout(run) / "node_modules"
        self.write(inside / "react" / "index.js", "module.exports = {}\n")
        self.assert_refused(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(inside), "--log", str(log)
        ), "outside the sealed checkout")
        self.assert_refused(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree)
        ), "requires --log")
        empty = self.root / "empty-install.log"
        empty.write_bytes(b"")
        self.assert_refused(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree), "--log", str(empty)
        ), "log is empty")
        self.assert_refused(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree),
            "--log", str(self.root / "missing.log"),
        ), "cannot read dependency provisioning log")
        # An unrecorded run cannot check a provisioning step, and a recorded block is
        # refused when its shape is tampered with.
        self.assert_refused(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree), "--check"
        ), "no provisioning step")
        self.assert_ok(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree), "--log", str(log)
        ))
        record = self.run_record(run)
        record["environment"]["provisioning"]["tree_digest"] = "sha256:" + "0" * 64
        self.write_json(run / "run.json", record)
        self.assert_refused(self.cli(
            "bind-dependencies", "--run-dir", str(run), "--tree", str(tree), "--check"
        ), "changed after it was recorded")

    def test_typescript_task_requires_a_bound_dependency_tree(self) -> None:
        run = self.root / "run-ts-requirement"
        task = self.task(language="typescript", gates=[self.gate_plain()])
        self.assert_ok(self.prepare(task=task, run=run))
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        self.assert_refused(self.check(run=run), "before check or receipt")
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "before check or receipt")
        self.bind_tree(run)
        self.assert_ok(self.check(run=run))

    def test_check_refuses_a_dependency_tree_that_changed_during_the_attempt(self) -> None:
        run = self.root / "run-tree-drift"
        tamper = (
            "import os\n"
            "os.makedirs('../../runs/run-tree-drift-node_modules/tampered', exist_ok=True)\n"
            "open('../../runs/run-tree-drift-node_modules/tampered/index.js', 'w').write('x')\n"
        )
        task = self.task(gates=[self.gate_plain(code=tamper)])
        self.assert_ok(self.prepare(task=task, run=run))
        self.bind_tree(run)
        self.edit("app/main.go", "package app\n\nfunc Main() int { return 2 }\n", run=run)
        self.assert_ok(self.seal(run=run))
        checked = self.check(run=run)
        self.assertNotEqual(checked.returncode, 0, checked.stdout)
        self.assertIn("changed after it was recorded", checked.stderr)
        # The refusal lands before the attempt record exists, so a drifted attempt is
        # preserved evidence that no receipt can read as a pass.
        self.assertFalse((run / "checks" / "attempt-0001" / "attempt.json").exists())
        self.assert_refused(self.receipt(self.usage_rows(), run=run), "changed after it was recorded")

    def test_fixture_probe_failure_fails_instead_of_skipping(self) -> None:
        interpreter = sys.executable
        sys.executable = str(self.root / "missing-interpreter")
        raised = None
        try:
            self.install_fake_mix(("Result: 1 passed",))
        except BaseException as error:
            raised = error
        finally:
            sys.executable = interpreter
        self.assertIsNotNone(raised, "the broken fixture probe did not fail the suite")
        self.assertNotIsInstance(raised, unittest.SkipTest, "a fixture failure must not be a skip")
        self.assertIsInstance(raised, AssertionError)
        self.assertIn("the fake mix fixture did not launch", str(raised))


if __name__ == "__main__":
    unittest.main()
