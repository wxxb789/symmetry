#!/usr/bin/env python3
"""Exercise the evaluation CLI through real Git, subprocess and file surfaces."""

import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("model-routing-evaluation.py")

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

    def prepare(self, task=None, routes=None, run=None, source=None) -> subprocess.CompletedProcess:
        inputs = self.root / "inputs"
        task_path = self.write_json(inputs / "task.json", self.task() if task is None else task)
        routes_path = self.write_json(inputs / "routes.json", self.routes() if routes is None else routes)
        return self.cli(
            "prepare", "--run-dir", str(run or self.run_dir()), "--task", str(task_path),
            "--routes", str(routes_path), "--source", str(source or self.repo),
        )

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
                row["known_subtotals"] = {"estimated_cost": estimated_cost}
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

    def accepted_run(self, run: pathlib.Path | None = None, task=None, usage=None):
        """Prepare, edit, seal, check, review and derive one accepted receipt."""
        run_path = run or self.run_dir()
        self.assert_ok(self.prepare(task=task, run=run_path))
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
        self.assertEqual(cost["known_estimated_cost_subtotal"], 1.5)
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
        self.assertEqual(cost["known_estimated_cost_subtotal"], 3.0)
        self.assertIsNone(cost["estimated_cost"])
        self.assertFalse(cost["accounting_complete"])
        self.assertIn("unknown estimated cost in 1 model phase(s)", cost["incomplete_reasons"])

        run = self.root / "run-channel-mixed"
        usage = self.usage_rows()
        phases = usage["phases"]
        phases[0].update(reported_cost=1.0, estimated_cost=None)
        phases[1].update(reported_cost=None, estimated_cost=2.0)
        for row in (phases[0], phases[1], phases[3]):
            row["price_version"] = "fixture-price-v1"
        self.accepted_run(run=run, usage=usage)
        cost = self.stored_receipt(run)["cost"]
        self.assertIsNone(cost["reported_cost"])
        self.assertIsNone(cost["estimated_cost"])
        self.assertFalse(cost["accounting_complete"])
        self.assertIn("no provider-reported cost in 2 model phase(s)", cost["incomplete_reasons"])
        self.assertIn("unknown estimated cost in 1 model phase(s)", cost["incomplete_reasons"])

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


if __name__ == "__main__":
    unittest.main()
