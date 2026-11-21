import json
import os
import pathlib
import stat
import subprocess
import tempfile
import unittest
from unittest import mock

import citest

SCRIPT = pathlib.Path(__file__).with_name("assert-folded-gates.py")


class LoadGateTests(unittest.TestCase):
    def test_a_gate_loads_under_the_requested_module_name(self):
        module = citest.load_gate(SCRIPT, "folded_gate_under_test")
        self.assertEqual(module.__name__, "folded_gate_under_test")
        self.assertEqual(module.parse_gate("Gate one=success"), ("Gate one", "success"))

    def test_a_path_that_is_not_an_importable_module_is_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "not-a-module.txt")
            path.write_text("not python\n", encoding="utf-8")
            with self.assertRaisesRegex(citest.GateImportRefused, "is not an importable module"):
                citest.load_gate(path, "not_a_module")

    def test_a_missing_gate_raises_instead_of_returning_a_default(self):
        with tempfile.TemporaryDirectory() as directory, self.assertRaises(FileNotFoundError):
            citest.load_gate(pathlib.Path(directory, "absent.py"), "absent")


class RunGateTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = pathlib.Path(directory.name)
        self.script = self.root / "fixture-gate.py"
        self.script.write_text(
            "import json, os, pathlib, sys\n"
            "print(json.dumps({'argv': sys.argv[1:], 'cwd': pathlib.Path.cwd().name}))\n"
            "print('marker=' + os.environ.get('GATE_MARKER', 'unset'), file=sys.stderr)\n"
            "raise SystemExit(3)\n",
            encoding="utf-8",
        )

    def test_the_child_status_and_streams_are_returned_unchanged(self):
        result = citest.run_gate(self.script)
        self.assertEqual(result.returncode, 3)
        self.assertEqual(json.loads(result.stdout), {"argv": [], "cwd": pathlib.Path.cwd().name})
        self.assertEqual(result.stderr, "marker=unset\n")

    def test_arguments_reach_the_child_in_order(self):
        result = citest.run_gate(self.script, "--repo", "r", "positional")
        self.assertEqual(json.loads(result.stdout)["argv"], ["--repo", "r", "positional"])

    def test_cwd_and_env_replace_the_inherited_ones(self):
        environment = dict(os.environ, GATE_MARKER=str(self.root / "marker"))
        result = citest.run_gate(self.script, cwd=self.root.parent, env=environment)
        self.assertEqual(json.loads(result.stdout)["cwd"], self.root.parent.name)
        self.assertEqual(result.stderr, f"marker={self.root / 'marker'}\n")


class GitTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = pathlib.Path(directory.name)
        self.repo = self.root / "repository"
        self.repo.mkdir()
        citest.git(self.repo, "init", "--quiet")

    def test_git_text_strips_the_record_separator(self):
        self.assertEqual(citest.git_text(self.repo, "rev-parse", "--is-inside-work-tree"), "true")

    def test_output_bytes_are_returned_when_text_is_not_requested(self):
        self.assertEqual(citest.git(self.repo, "rev-parse", "--is-inside-work-tree").stdout, b"true\n")

    def test_a_failing_command_raises_instead_of_returning_a_default(self):
        with self.assertRaises(subprocess.CalledProcessError) as refusal:
            citest.git(self.repo, "rev-parse", "HEAD")
        self.assertEqual(refusal.exception.returncode, 128)

    def test_an_inherited_git_dir_does_not_redirect_the_command(self):
        other = self.root / "other"
        other.mkdir()
        citest.git(other, "init", "--quiet")
        with mock.patch.dict(os.environ, {"GIT_DIR": str(other / ".git")}):
            toplevel = pathlib.Path(citest.git_text(self.repo, "rev-parse", "--show-toplevel")).resolve()
        self.assertEqual(toplevel, self.repo.resolve())

    def test_an_input_is_written_to_the_child(self):
        completed = citest.git(self.repo, "hash-object", "--stdin", input=b"content\n")
        self.assertEqual(len(completed.stdout.strip()), 40)


class WriteJsonTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = pathlib.Path(directory.name)

    def test_the_written_bytes_are_canonical_and_returned(self):
        path = self.root / "nested" / "record.json"
        data = citest.write_json(path, {"b": 1, "a": [2, {"c": 3}]})
        self.assertEqual(data, b'{\n  "b": 1,\n  "a": [\n    2,\n    {\n      "c": 3\n    }\n  ]\n}\n')
        self.assertEqual(path.read_bytes(), data)

    def test_sort_keys_orders_the_members_by_key(self):
        path = self.root / "record.json"
        self.assertEqual(citest.write_json(path, {"b": 1, "a": 2}, sort_keys=True), b'{\n  "a": 2,\n  "b": 1\n}\n')

    def test_write_bytes_creates_parents_and_returns_the_input(self):
        path = self.root / "deep" / "raw.bin"
        self.assertEqual(citest.write_bytes(path, b"raw\n"), b"raw\n")
        self.assertEqual(path.read_bytes(), b"raw\n")

    @unittest.skipUnless(os.name == "posix", "the private mode is a POSIX permission bit")
    def test_mode_is_applied_after_the_write(self):
        path = self.root / "private.json"
        citest.write_json(path, {"a": 1}, mode=0o600)
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)


class StrictJsonTests(unittest.TestCase):
    def test_a_canonical_document_parses(self):
        value = citest.strict_json('{"a": [1, 2.5, "x", true, null], "b": {"c": -3}}', "record")
        self.assertEqual(value, {"a": [1, 2.5, "x", True, None], "b": {"c": -3}})

    def test_a_repeated_key_is_refused(self):
        with self.assertRaisesRegex(citest.StrictJsonRefused, "record repeats the JSON key 'a'"):
            citest.strict_json('{"a": 1, "a": 2}', "record")

    def test_a_repeated_key_inside_a_nested_object_is_refused(self):
        with self.assertRaisesRegex(citest.StrictJsonRefused, "record repeats the JSON key 'b'"):
            citest.strict_json('{"a": {"b": 1, "b": 2}}', "record")

    def test_a_nonfinite_number_is_refused(self):
        with self.assertRaisesRegex(citest.StrictJsonRefused, "record contains a nonfinite number"):
            citest.strict_json('{"a": 1e999}', "record")

    def test_a_non_json_constant_is_refused(self):
        with self.assertRaisesRegex(citest.StrictJsonRefused, "record contains the non-JSON constant NaN"):
            citest.strict_json('{"a": NaN}', "record")

    def test_invalid_json_is_refused_with_the_source_named(self):
        with self.assertRaisesRegex(citest.StrictJsonRefused, "run record is not valid JSON"):
            citest.strict_json("{", "run record")

    def test_the_refusal_names_the_source_and_not_only_the_document(self):
        with self.assertRaisesRegex(citest.StrictJsonRefused, "^usage record is not valid JSON: "):
            citest.strict_json("[1,", "usage record")


if __name__ == "__main__":
    unittest.main()
