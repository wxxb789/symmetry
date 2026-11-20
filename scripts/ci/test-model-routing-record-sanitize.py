#!/usr/bin/env python3
"""Regression tests for the record path-sanitisation helper.

These tests are parser/renderer regression evidence: they exercise the
rendering of host-specific absolute paths in recorded text and never touch a
real evaluation record.
"""

import hashlib
import importlib.util
import json
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
    r"\\?\C:\Users\operator\run",
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

# The same host paths as they appear after JSON quoting: doubled backslashes, an
# escaped forward slash (`\/`) or a `file:` URL. A checker that misses these forms
# would pass a record whose published bytes still name a host location.
_BS, _FS = chr(92), chr(47)
QUOTED_HOST_PATHS = (
    '{"store":"' + _BS * 4 + "server" + _BS * 2 + "share" + _BS * 2 + "run" + '"}',
    "store //server/share/run here",
    "opened file:///C:/Users/operator/run today",
    "opened file:///home/operator/x today",
    "root " + _BS + _FS + "Users" + _BS + _FS + "operator" + _BS + _FS + "x kept",
    # A `file:` URL whose location is spelled with backslashes: single (a legitimate
    # file-URL form), JSON-escaped, and device-prefixed.
    "store file:" + _BS + "server" + _BS + "share" + _BS + "records",
    '{"src":"file:' + _BS * 2 + "server" + _BS + "share" + _BS + "0008d" + '"}',
    '{"src":"file:' + _BS * 4 + "server" + _BS * 2 + "share" + _BS * 2 + "run" + '"}',
    "store file:" + _BS + "tmp" + _BS + "x",
    "handle " + _BS * 2 + "?" + _BS + "C:" + _BS + "Users" + _BS + "operator" + _BS + "run",
    '{"handle":"file:' + _BS * 4 + "?" + _BS * 2 + "C:" + _BS * 2 + "Users" + _BS * 2 + "op" + '"}',
    # A location spelled after a key's colon, and a naive separator doubling.
    "root:/home/operator/state/run",
    "root:C:" + _BS + "Users" + _BS + "operator" + _BS + "run",
    "state under /tmp//symmetry-0008d-run and /home//operator/x",
    # A quoted path beside text: see `_render_match`'s docstring for the JSON-escape rule.
    '{"t":"see ' + _BS + '"C:' + _BS * 2 + "dir" + _BS * 2 + "file" + _BS + '", ok"}',
)

# The fragments that must not survive rendering of the prefixed/escaped forms above:
# a partial render (`path-sha256:<hex>:\Users\operator`) would still pass a
# "changed and contains a token" assertion, but it republishes part of the path.
QUOTED_HOST_PATH_FRAGMENTS = ("C:", "Users", "operator", "home", "server", "share", "tmp")


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

    def test_root_only_catches_quoted_and_url_host_paths(self):
        for text in QUOTED_HOST_PATHS:
            with self.subTest(text=text):
                rendered = MODULE.sanitize_text(text, [], root_only=True)
                self.assertNotEqual(rendered, text)
                self.assertIn("path-sha256:", rendered)
                for fragment in QUOTED_HOST_PATH_FRAGMENTS:
                    self.assertNotIn(fragment, rendered, text)
                # Rendering stays idempotent even with the quoted forms.
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)

    def test_json_escaped_quote_after_a_path_stays_out_of_the_token(self):
        # A path quoted inside a recorded JSON string is spelled `\"C:\\dir\\file\"`:
        # each backslash before a quote is that quote's escape, not a path byte.
        # Consuming the trailing one would leave a bare quote and break the record.
        raw_path = "C:" + _BS * 2 + "dir" + _BS * 2 + "file"
        text = '{"t": "see ' + _BS + '"' + raw_path + _BS + '", ok", "n": 1}'
        rendered = MODULE.sanitize_text(text, [], root_only=True)
        self.assertEqual(
            json.loads(rendered), {"t": 'see "' + digest_of(raw_path) + '", ok', "n": 1},
        )
        # An even trailing run is literal backslashes before the string's own bare
        # closing quote, so the whole run stays inside the token.
        tail_path = "C:" + _BS * 2 + "dir" + _BS * 2
        tail_rendered = MODULE.sanitize_text('{"t": "' + tail_path + '"}', [], root_only=True)
        self.assertEqual(json.loads(tail_rendered), {"t": digest_of(tail_path)})
        # Both shapes stay idempotent.
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        self.assertEqual(MODULE.sanitize_text(tail_rendered, [], root_only=True), tail_rendered)

    def test_json_escaped_quote_after_a_path_stays_out_of_the_token_for_odd_runs(self):
        # An odd run longer than one behaves the same way: a three-backslash run
        # before the recorded quote keeps its pair inside the token and leaves
        # the last backslash outside it as the quote's escape, so the quoted
        # string still decodes and no bare quote is left behind.
        run3_path = "C:" + _BS * 2 + "dir" + _BS * 2
        rendered = MODULE.sanitize_text(
            '{"t": "see ' + _BS + '"' + run3_path + _BS + '", ok"}', [], root_only=True,
        )
        self.assertEqual(json.loads(rendered), {"t": 'see "' + digest_of(run3_path) + '", ok'})
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)

    def test_root_only_keeps_https_urls_literal(self):
        text = "https://github.com/wxxb789/symmetry/blob/main/x and https://x/home/y"
        self.assertEqual(MODULE.sanitize_text(text, [], root_only=True), text)

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

    def test_cli_refuses_a_record_that_does_not_decode_as_utf8(self):
        # Checked as mojibake, either spelling of a UTF-16 record's host path would pass:
        # its ASCII characters interleave NULs (valid UTF-8, invisible to a contiguous
        # pattern) and a BOM-led record does not decode at all. The tool refuses the
        # bytes instead, in both the check and the render path.
        with tempfile.TemporaryDirectory() as tmp:
            body = '{"root":"/tmp/symmetry-0008d-eval"}\n'
            for name, payload in (("nul-laden", body.encode("utf-16-le")),
                                  ("bom-led", b"\xff\xfe" + body.encode("utf-16-le"))):
                path = pathlib.Path(tmp) / f"recorded-{name}.json"
                path.write_bytes(payload)
                for arguments in (["--check", "--root-only"], ["--check"], []):
                    with self.subTest(payload=name, arguments=arguments):
                        result = subprocess.run(
                            [sys.executable, str(SCRIPT), *arguments, str(path)],
                            capture_output=True, check=False,
                        )
                        self.assertEqual(result.returncode, 2, result.stderr)
                        self.assertEqual(result.stdout, b"")
                        self.assertIn("refused", result.stderr.decode("utf-8"))

    def test_cli_root_only_check_refuses_quoted_host_paths(self):
        for text in QUOTED_HOST_PATHS:
            result = self.run_cli(text + "\n", "--check", "--root-only")
            self.assertEqual(result.returncode, 1, text)
            self.assertEqual(result.stdout, "", text)
            self.assertIn("refused", result.stderr)

    def test_cli_root_only_renders_stdin_with_routes_kept(self):
        result = self.run_cli("see /portal/runtimes under " + HOST_PATHS[4] + "\n",
                              "--root-only", "--root", ROOT)
        self.assertEqual(result.returncode, 0)
        self.assertIn("/portal/runtimes", result.stdout)
        self.assertIn(digest_of(HOST_PATHS[4]), result.stdout)


if __name__ == "__main__":
    unittest.main()
