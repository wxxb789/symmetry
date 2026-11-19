#!/usr/bin/env python3
"""Regression tests for the record path-sanitisation helper.

These tests are parser/renderer regression evidence: they exercise the
rendering of host-specific absolute paths in recorded text and never touch a
real evaluation record.
"""

import hashlib
import importlib.util
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("model-routing-record-sanitize.py")


def load_module():
    spec = importlib.util.spec_from_file_location("model_routing_record_sanitize", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


MODULE = load_module()
ROOT = r"C:\Users\operator\AppData\Local\Temp\symmetry-0008b-7f3m2q"

HOST_PATHS = (
    r"C:\Users\operator\AppData\Local\Temp\symmetry-0008b-probe",
    "C:/Users/operator/project",
    r"\\server\share\records",
    "/home/operator/state/run-7f3m2q",
    "/Users/operator/Library/Caches/tmp",
    "/root/omp-native-acceptance",
    "/tmp/symmetry-0008b-linux",
    "/var/folders/xy/ab/T",
    "/mnt/q/repos/symmetry/daemon",
    "/private/var/tmp/symmetry",
)

PRODUCT_ROUTES = (
    "/portal/runtimes",
    "/portal/api/runtime-capabilities",
    "/socket",
    "/assets/login.css",
)


def digest_of(path):
    return "path-sha256:" + hashlib.sha256(path.encode("utf-8")).hexdigest()


class SanitizeTextTests(unittest.TestCase):
    def test_disposable_root_renders_to_token_with_trailing_structure(self):
        text = ROOT + r"\controls\baseline-protocol"
        self.assertEqual(MODULE.sanitize_text(text, [ROOT]), "<disposable-root>\\controls\\baseline-protocol")

    def test_disposable_root_forward_slash_variant_renders_to_token(self):
        text = ROOT.replace("\\", "/") + "/controls/baseline-protocol"
        self.assertEqual(MODULE.sanitize_text(text, [ROOT]), "<disposable-root>/controls/baseline-protocol")

    def test_other_windows_path_renders_to_hashed_token(self):
        path = r"C:\Users\operator\AppData\Local\Temp\symmetry-0008b-probe"
        self.assertEqual(MODULE.sanitize_text("worktree " + path, []), "worktree " + digest_of(path))

    def test_unc_and_posix_paths_render_to_hashed_tokens(self):
        unc = r"\\server\share\records"
        posix = "/home/operator/state/run-7f3m2q"
        rendered = MODULE.sanitize_text(f"unc {unc} posix {posix}", [])
        self.assertNotIn(unc, rendered)
        self.assertNotIn(posix, rendered)
        self.assertIn(digest_of(unc), rendered)
        self.assertIn(digest_of(posix), rendered)

    def test_relative_paths_urls_and_digests_pass_through(self):
        text = ("docs/evaluations/model-routing/0008b-policy.json "
                "https://github.com/wxxb789/symmetry/blob/main/README.md "
                "sha256:9a9ff8eb65bda21e584c5fef9642f86569b46c8c8da16d3e216443e702425c56")
        self.assertEqual(MODULE.sanitize_text(text, [ROOT]), text)

    def test_rendering_is_idempotent(self):
        text = f"root {ROOT}\\controls\\x and other {ROOT[:-6]}a\\b"
        once = MODULE.sanitize_text(text, [ROOT])
        self.assertEqual(MODULE.sanitize_text(once, [ROOT]), once)

    def test_no_personal_path_survives_rendering(self):
        text = (ROOT + r"\checks\attempt-0001 and C:\Users\operator\.local\state and "
                "/home/operator/.local/state")
        rendered = MODULE.sanitize_text(text, [ROOT])
        for marker in ("C:", "/home", "Users", "operator"):
            self.assertNotIn(marker, rendered)

    def test_root_only_keeps_product_routes_literal(self):
        text = "The frozen claim keeps " + " and ".join(PRODUCT_ROUTES) + " readable."
        self.assertEqual(MODULE.sanitize_text(text, [], root_only=True), text)

    def test_root_only_still_hashes_host_paths(self):
        for path in HOST_PATHS:
            rendered = MODULE.sanitize_text(f"ran under {path} today", [], root_only=True)
            self.assertNotIn(path, rendered, path)
            self.assertIn(digest_of(path), rendered, path)

    def test_root_only_leaves_relative_paths_urls_and_digests_alone(self):
        text = ("docs/evaluations/model-routing/0008d-a-mixed.json "
                "https://github.com/wxxb789/symmetry/blob/main/README.md "
                "sha256:9a9ff8eb65bda21e584c5fef9642f86569b46c8c8da16d3e216443e702425c56")
        self.assertEqual(MODULE.sanitize_text(text, [ROOT], root_only=True), text)

    def test_root_only_leaves_a_bare_root_token_literal(self):
        text = "the render step was blocked by `/tmp` and `/home` alone, never by a path"
        self.assertEqual(MODULE.sanitize_text(text, [], root_only=True), text)
        # The default scope still renders them, so this is a `--root-only` boundary.
        self.assertNotEqual(MODULE.sanitize_text(text, [], root_only=False), text)

    def test_root_only_still_renders_a_disposable_root(self):
        text = ROOT + r"\controls\baseline-protocol"
        rendered = MODULE.sanitize_text(text, [ROOT], root_only=True)
        self.assertEqual(rendered, "<disposable-root>\\controls\\baseline-protocol")

    def test_root_only_rendering_is_idempotent(self):
        text = ("root " + ROOT + r"\controls\x and " + HOST_PATHS[3] + " beside "
                + PRODUCT_ROUTES[0])
        once = MODULE.sanitize_text(text, [ROOT], root_only=True)
        self.assertIn(PRODUCT_ROUTES[0], once)
        self.assertEqual(MODULE.sanitize_text(once, [ROOT], root_only=True), once)

    def test_default_mode_still_renders_product_routes(self):
        # The default renderer's documented boundary is every absolute path, so
        # argv/output capture keeps hashing routes; only --root-only spares them.
        for route in PRODUCT_ROUTES:
            rendered = MODULE.sanitize_text(f"served {route} twice", [])
            self.assertNotIn(route, rendered)
            self.assertIn(digest_of(route), rendered)


class SanitizeCliTests(unittest.TestCase):
    def run_cli(self, text, *arguments):
        return subprocess.run(
            [sys.executable, str(SCRIPT), *arguments],
            input=text, text=True, capture_output=True, check=False,
        )

    def test_cli_renders_stdin_to_stdout(self):
        result = self.run_cli(ROOT + r"\controls\baseline-protocol", "--root", ROOT)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "<disposable-root>\\controls\\baseline-protocol")

    def test_cli_check_mode_refuses_a_host_path_without_writing(self):
        result = self.run_cli(r"see C:\Users\operator\x", "--check")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")
        self.assertIn("refused", result.stderr)

    def test_cli_check_mode_passes_clean_text(self):
        result = self.run_cli("docs/evaluations/model-routing/README.md", "--check")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_cli_preserves_recorded_crlf_line_endings(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "recorded.txt"
            path.write_bytes(f"first\r\n{ROOT}\\controls\\baseline-protocol\r\n".encode("utf-8"))
            result = subprocess.run(
                [sys.executable, str(SCRIPT), "--root", ROOT, str(path)],
                capture_output=True, check=False,
            )
            self.assertEqual(result.returncode, 0)
            self.assertEqual(result.stdout, b"first\r\n<disposable-root>\\controls\\baseline-protocol\r\n")

    def test_cli_leaves_clean_utf8_text_byte_identical(self):
        text = (
            "# Goal 0008b: evidence-based Go model routing — docs/evaluations/model-routing/README.md\n"
            "The binding contract is [`docs/goals/archive/0008b-go-model-routing.md`](../goals/archive/0008b-go-model-routing.md),\n"
            "| Bytes / SHA-256 | 1660 / `sha256:d3422209a7ece34e235e0d28a52d4dd0af17037b96513bd86d64385190d99d86` |\n"
            "https://github.com/wxxb789/symmetry/blob/main/docs/evidence/goal-0008b-go-model-routing-2026-11-17.md\n"
        )
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "clean.md"
            path.write_bytes(text.encode("utf-8"))
            result = subprocess.run(
                [sys.executable, str(SCRIPT), str(path)], capture_output=True, check=False,
            )
            self.assertEqual(result.returncode, 0)
            self.assertEqual(result.stdout, text.encode("utf-8"))

    def test_cli_root_only_check_passes_product_routes(self):
        text = "The page at /portal/runtimes renders /portal/api rows beside /socket and /assets/login.css.\n"
        result = self.run_cli(text, "--check", "--root-only")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")

    def test_cli_root_only_check_refuses_every_host_path_shape(self):
        for path in HOST_PATHS:
            result = self.run_cli(f"ran under {path}\n", "--check", "--root-only")
            self.assertEqual(result.returncode, 1, path)
            self.assertEqual(result.stdout, "", path)
            self.assertIn("refused", result.stderr)

    def test_cli_root_only_renders_stdin_with_routes_kept(self):
        result = self.run_cli("see /portal/runtimes under " + HOST_PATHS[4] + "\n",
                              "--root-only", "--root", ROOT)
        self.assertEqual(result.returncode, 0)
        self.assertIn("/portal/runtimes", result.stdout)
        self.assertIn(digest_of(HOST_PATHS[4]), result.stdout)


if __name__ == "__main__":
    unittest.main()
