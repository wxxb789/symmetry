#!/usr/bin/env python3
"""Regression tests for the record path-sanitisation helper.

These tests are parser/renderer regression evidence: they exercise the
rendering of host-specific absolute paths in recorded text and never touch a
real evaluation record.
"""

import hashlib
import importlib.util
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import time
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
    r"\\.\C:\Users\operator\secret",
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
    # A `.` device namespace, JSON-escaped, and a UNC escaped one level deeper than the
    # single JSON escape above (the raw doubly escaped spelling of `\\server\share`):
    # the device prefix must take `.` as well as `?`, and the UNC leader/inner
    # separator any even backslash run, not just two or four.
    '{"handle":"' + _BS * 4 + "." + _BS * 2 + "C:" + _BS * 2 + "Users" + _BS * 2 + "op" + '"}',
    '{"p":"' + _BS * 8 + "server" + _BS * 4 + "share" + _BS * 4 + "run" + '"}',
    # A location spelled after a key's colon, and a naive separator doubling.
    "root:/home/operator/state/run",
    "root:C:" + _BS + "Users" + _BS + "operator" + _BS + "run",
    "state under /tmp//symmetry-0008d-run and /home//operator/x",
    # Mixed-separator UNC spellings (Windows reads both slash directions as the same
    # location), their JSON-escaped inner separator, a root-relative Windows
    # spelling, a space-containing path, and a `file:` URL with no segment after the
    # host root.
    "store " + _BS * 2 + "server/share/sub as a mixed-separator UNC",
    "store //server" + _BS + "share" + _BS + "sub as a mixed-separator UNC",
    "store " + _BS * 2 + "server" + _BS + _FS + "share/sub as an escaped-inner UNC",
    "store " + _BS + "Users" + _BS + "operator" + _BS + "run as a root-relative spelling",
    "worktree C:" + _BS + "Users" + _BS + "First Last" + _BS + "repo today",
    "opened file:///tmp today",
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

    def test_disposable_root_matches_escaped_spellings_and_keeps_structure(self):
        # A root inside a quoted record is spelled with doubled separators (the JSON
        # escape of each backslash run); the token binds the root alone, so the
        # separators and segments behind it stay in place.
        root = r"C:\Work\run"
        escaped = _BS * 2
        text = '{"store":"C:' + escaped + "Work" + escaped + "run" + escaped + "x" + escaped + 'y"}'
        rendered = MODULE.sanitize_text(text, [root])
        self.assertEqual(
            rendered, '{"store":"<disposable-root>' + escaped + "x" + escaped + 'y"}',
        )
        self.assertEqual(MODULE.sanitize_text(rendered, [root]), rendered)
        # A separator may also be spelled as the `\/` escape, and the spellings mix:
        # the root still matches, and the run after it stays outside the token.
        mixed = "C:" + _BS + "/Work" + escaped + "run/x"
        self.assertEqual(MODULE.sanitize_text(mixed, [root]), "<disposable-root>/x")

    def test_disposable_root_does_not_absorb_a_sibling_path(self):
        # A sibling directory whose name merely starts with the root's last segment
        # is a different location: tokenising `C:/Work/run-user` as
        # `<disposable-root>-user` would republish path bytes under a token.
        sibling = "C:/Work/run-user/private"
        self.assertEqual(
            MODULE.sanitize_text("worktree " + sibling, [r"C:\Work\run"]),
            "worktree " + digest_of(sibling),
        )
        # The same spelling in `--root-only` scope: a drive path hashes whole, and
        # the root's own replacement does not claim its first segments.
        self.assertEqual(
            MODULE.sanitize_text("worktree " + sibling, [r"C:\Work\run"], root_only=True),
            "worktree " + digest_of(sibling),
        )

    def test_disposable_root_does_not_absorb_a_space_continued_sibling(self):
        # STD-B10: `C:/Work/run user/private` names the sibling `run user`, one
        # space-continued path token; pressing the root's token over its prefix left
        # `user/private` standing as if it were a child of the token. The root is a
        # whole-name boundary, and a space-continued chunk continues the name.
        root = r"C:\Work\run"
        sibling = "C:/Work/run user/private"
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(sibling, [root], root_only=root_only)
                self.assertEqual(rendered, digest_of(sibling))
                self.assertEqual(MODULE.sanitize_text(rendered, [root], root_only=root_only), rendered)
        # A prose word after the root is not a continuation: the root still renders.
        self.assertEqual(
            MODULE.sanitize_text(root + " and the run continues", [root], root_only=True),
            "<disposable-root> and the run continues",
        )
        # A terminal chunk is the same rule: `run user` may be one sibling name, so
        # the root does not claim the token's prefix either - the whole token renders
        # (the drive-path renderer hashes it), never a token plus a residue.
        terminal = "C:/Work/run user"
        for root_only in (True, False):
            with self.subTest(shape="terminal", root_only=root_only):
                self.assertEqual(
                    MODULE.sanitize_text(terminal, [root], root_only=root_only),
                    digest_of(terminal),
                )

    def test_a_root_only_claims_its_own_separator_kind(self):
        # STD-B10: a root's leading separator run is kind-specific. The UNC root
        # `\\work\run` must not tokenize `/work/run/child` - a POSIX location that
        # merely spells the same segments - and a slash-led root must not claim the
        # backslash-led spelling either.
        unc_root = _BS * 2 + "work" + _BS + "run"
        self.assertEqual(
            MODULE.sanitize_text("/work/run/child", [unc_root], root_only=True),
            "/work/run/child",
        )
        record_spelling = _BS * 2 + "work" + _BS + "run" + _BS + "child"
        self.assertEqual(
            MODULE.sanitize_text(record_spelling, [unc_root], root_only=True),
            "<disposable-root>" + _BS + "child",
        )
        # The JSON-escaped run of the same UNC root is its own spelling too.
        escaped = _BS * 4 + "work" + _BS * 2 + "run" + _BS * 2 + "child"
        self.assertEqual(
            MODULE.sanitize_text(escaped, [unc_root], root_only=True),
            "<disposable-root>" + _BS * 2 + "child",
        )
        # The symmetric case: a POSIX root is not the UNC spelling of its segments,
        # and its own escaped spelling still renders.
        posix_root = "/work/run"
        self.assertEqual(
            MODULE.sanitize_text(record_spelling, [posix_root], root_only=True),
            digest_of(record_spelling),
        )
        self.assertEqual(
            MODULE.sanitize_text("/work/run/child", [posix_root], root_only=True),
            "<disposable-root>/child",
        )

    def test_a_rendered_root_keeps_its_escaped_descendant_suffix(self):
        # STD-B10: the default scope's escaped-POSIX pattern matched the `\/` a
        # rendered token leaves behind and hashed the short suffix, so the token's
        # own trailing structure came back as a second digest instead of staying in
        # place.
        root = r"C:\Work\run"
        text = root + _BS + _FS + "child"
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [root], root_only=root_only)
                self.assertEqual(rendered, "<disposable-root>" + _BS + _FS + "child")
                self.assertEqual(MODULE.sanitize_text(rendered, [root], root_only=root_only), rendered)

    def test_root_match_over_a_long_separator_run_is_linear(self):
        # STD-B02: the root matcher's separator used to be an overlapping
        # alternation repeated as a group (`(?:\\+|/|\\/)+`), which parses one run
        # of N backslashes 2**(N-1) ways: `C:` + N backslashes + `Worker/run` took
        # 0.017s / 0.14s / 1.1s for N = 18 / 21 / 24, doubling per backslash. One
        # character class matches the run once, so the failed match stays linear.
        root = r"C:\Work\run"
        text = "C:" + _BS * 4000 + "Worker/run"
        started = time.monotonic()
        rendered = MODULE.sanitize_text(text, [root], root_only=True)
        self.assertLess(time.monotonic() - started, 5.0)
        # The root does not match (`Worker` is not the root's segment); the
        # drive-path renderer still owns the whole token.
        self.assertEqual(rendered, digest_of(text))

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
        # The path is closed by punctuation before the prose word: a bare word at
        # the end of the line is a terminal space chunk the tail absorbs (the
        # documented over-approximation, pinned by its own cases below), and this
        # case pins the exact digest of the path itself.
        for path in HOST_PATHS:
            rendered = MODULE.sanitize_text(f"ran under {path}, today", [], root_only=True)
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

    def test_https_urls_with_a_host_root_segment_stay_literal_verbatim(self):
        # F12: the host-rooted POSIX pattern must not see a `/tmp` or `/home`
        # authority segment behind a URL's own separators as a location. This holds
        # in both modes, because the measured-value redaction shares the root-only
        # pattern set.
        text = ("https://tmp/vendor/doc and https://home/vendor/doc beside "
                "https://x/home/y and https://github.com/wxxb789/symmetry/blob/main/x")
        self.assertEqual(MODULE.sanitize_text(text, [], root_only=True), text)
        self.assertEqual(MODULE.sanitize_text(text, []), text)

    def test_root_only_catches_space_continued_path_tails(self):
        # F06: a path whose segment contains a space rendered only its prefix, and the
        # residual tail (`First`, `Last`, `repo`) no longer looked like a path, so the
        # rendered text re-checked clean. The tail continues across one space exactly
        # while the chunk after it reaches another separator or closes the location at
        # the end of the line; a word followed by another word is not a continuation.
        windows = "C:" + _BS + "Users" + _BS + "First Last" + _BS + "repo"
        posix = "/home/first last/repo"
        for text in (f"worktree {windows} today", f"state {posix} today",
                     json.dumps({"path": windows}), json.dumps({"path": posix})):
            with self.subTest(text=text):
                rendered = MODULE.sanitize_text(text, [], root_only=True)
                for fragment in ("First", "Last", "first", "last", "repo"):
                    self.assertNotIn(fragment, rendered, text)
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        # A prose word after the path is not a continuation: the space ends the match.
        prose = f"worktree {windows} and the run continues"
        rendered = MODULE.sanitize_text(prose, [], root_only=True)
        self.assertIn("and the run continues", rendered)

    def test_space_continued_paths_render_whole_on_every_family(self):
        # Sol P2: the F06 continuation covered only a chunk that reaches another
        # separator, only the drive and host-POSIX families, and only when the
        # host-POSIX pattern won in root-only mode: `\\server\share\My Folder\sub\x`
        # rendered as its prefix plus ` Folder\sub\x`, and a terminal chunk
        # (`my project`, `my folder`) stayed literal in both scopes - all of them
        # re-checking clean because the residue no longer looked like a path.
        cases = (
            _BS * 2 + "server" + _BS + "share" + _BS + "My Folder" + _BS + "sub" + _BS + "x",
            "C:" + _BS + "Users" + _BS + "op" + _BS + "my project",
            "/home/op/my project",
            _BS * 2 + "srv" + _BS + "sh" + _BS + "my folder",
            "/home/first last/repo",
        )
        for path in cases:
            for root_only in (True, False):
                # The raw spelling at the end of a line renders as one token whose
                # digest is the path itself (the final chunk is closed by the line).
                with self.subTest(path=path, root_only=root_only, boundary="eol"):
                    rendered = MODULE.sanitize_text(f"worktree {path}", [], root_only=root_only)
                    self.assertEqual(rendered, f"worktree {digest_of(path)}")
                    self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
                # A quoted spelling closes the same chunks, and the whole location
                # (escaping included) is one token - the digest binds the matched
                # text, so only the fragment absence is asserted here.
                quoted = json.dumps({"path": path})
                with self.subTest(path=path, root_only=root_only, boundary="quote"):
                    rendered = MODULE.sanitize_text(quoted, [], root_only=root_only)
                    self.assertEqual(rendered.count("path-sha256:"), 1, quoted)
                    for fragment in ("Folder", "folder", "project", "First", "Last"):
                        self.assertNotIn(fragment, rendered, quoted)
                    self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)

    def test_a_json_escape_after_a_path_survives_the_render(self):
        # STD-A07: the segment classes used to accept a bare backslash, so the
        # render took the JSON `\n` escape and the evidence line behind it into the
        # token (`FAIL: verifier refused` vanished) while the record stayed valid
        # JSON. A POSIX segment never contains a bare backslash, so the escape and
        # everything after it stay outside the token.
        text = '{"out":"working directory: /tmp/run' + _BS + 'nFAIL: verifier refused"}'
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                self.assertEqual(
                    json.loads(rendered),
                    {"out": "working directory: " + digest_of("/tmp/run") + "\nFAIL: verifier refused"},
                )
                self.assertEqual(
                    MODULE.sanitize_text(rendered, [], root_only=root_only), rendered
                )

    def test_file_scheme_spellings_are_case_insensitive(self):
        # STD-A05: `FILE:` and `File:` are the same URL scheme as `file:`, and the
        # lowercase-only scheme let a record carrying either spelling pass the
        # root-only check unchanged (the lookbehind also blocked the bare drive
        # letter behind the slashes).
        cases = (
            "FILE:///C:/Work/Personal",
            "File:///C:/Work/Personal",
            "FILE:///home/operator/run",
        )
        for text in cases:
            for root_only in (True, False):
                with self.subTest(text=text, root_only=root_only):
                    rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                    self.assertEqual(rendered, digest_of(text))
                    self.assertEqual(
                        MODULE.sanitize_text(rendered, [], root_only=root_only), rendered
                    )

    def test_file_url_authority_with_a_drive_location_is_one_token(self):
        # STD-A06: `file://localhost/C:/Work/Personal` is one absolute location, but
        # the authority arm stopped at the drive letter's colon: the render left
        # `/Work/Personal` literal and the residue re-checked clean, so a published
        # record could still name the operator's directories.
        text = "opened file://localhost/C:/Work/Personal, today"
        whole = "file://localhost/C:/Work/Personal"
        rendered = MODULE.sanitize_text(text, [], root_only=True)
        self.assertEqual(rendered, f"opened {digest_of(whole)}, today")
        self.assertEqual(MODULE.sanitize_text(text, []), rendered)
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)

    def test_a_terminal_prose_word_is_the_documented_over_approximation(self):
        # Sol P3: the space continuation cannot tell a space-containing segment
        # (`my project`) from a path followed by a prose word (`x and/or`), so a
        # terminal chunk is absorbed into the token and the correlated digest is the
        # path plus that word. The over-approximation is deliberate - the narrower
        # rule leaked the path's own bytes - and this fixture pins it so a later
        # narrowing is a visible decision, not a silent drift.
        path, tail = "C:" + _BS + "Users" + _BS + "x", " and/or"
        text = path + tail + " the repo"
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                self.assertEqual(rendered, digest_of(path + tail) + " the repo")
                self.assertEqual(
                    MODULE.sanitize_text(rendered, [], root_only=root_only), rendered
                )

    def test_urls_stay_literal_in_both_scopes(self):
        # The URL battery: the lookbehinds keep every URL spelling literal in both
        # modes, including authorities whose first segment is a host root.
        urls = (
            "https://github.com/wxxb789/symmetry/blob/main/README.md",
            "https://tmp/vendor/doc",
            "https://home/vendor/doc",
            "https://x/home/y",
            "https://example.invalid/a/b",
            "http://127.0.0.1:8080/portal/runtimes",
            "http://localhost:55444/socket",
            "git+ssh://git@example.invalid/tmp/repo",
            "https:\\/\\/tmp/vendor/doc",
            "<https://10.0.0.1/home/x>",
            "https://xn--80ak6aa92e.invalid/Users/op",
        )
        for url in urls:
            for root_only in (True, False):
                with self.subTest(url=url, root_only=root_only):
                    text = f"fetched {url} unchanged"
                    self.assertEqual(
                        MODULE.sanitize_text(text, [], root_only=root_only), text, url
                    )
        # A doubled-slash authority spelling is a location, not a URL to spare, in
        # both scopes; the spelled route behind it renders with it.
        authority = "//example.invalid/portal/runtimes"
        text = f"fetched {authority}, unchanged"
        for root_only in (True, False):
            self.assertEqual(
                MODULE.sanitize_text(text, [], root_only=root_only),
                "fetched " + digest_of(authority) + ", unchanged",
            )

    def test_root_only_catches_mixed_separator_unc_spellings(self):
        # F07: Windows reads `\\server/share/sub` and `//server\share\sub` as the same
        # UNC location, and a checker that missed a mixed spelling would pass a record
        # that still names it.
        cases = (
            _BS * 2 + "server/share/sub",
            "//server" + _BS + "share" + _BS + "sub",
            _BS * 2 + "server" + _BS + _FS + "share/sub",
        )
        for path in cases:
            for root_only in (True, False):
                with self.subTest(path=path, root_only=root_only):
                    rendered = MODULE.sanitize_text(f"store {path}, here", [], root_only=root_only)
                    self.assertNotIn("server", rendered, path)
                    self.assertNotIn("share", rendered, path)
                    self.assertIn(digest_of(path), rendered, path)
                    self.assertEqual(
                        MODULE.sanitize_text(rendered, [], root_only=root_only), rendered
                    )

    def test_default_mode_renders_colon_adjacent_and_escaped_posix_paths(self):
        # F10: the default scope's documented boundary is every absolute path, and a
        # path after a key's colon or in its JSON-escaped spelling is one; a URL must
        # stay literal in the same pass.
        text = ("PATH=/usr/bin:/usr/local/bin:/bin and "
                + '{"p":"' + _BS + '/etc' + _BS + '/hosts"} and https://tmp/vendor/doc')
        rendered = MODULE.sanitize_text(text, [])
        for fragment in ("/usr/local/bin", "/bin", "etc", "hosts"):
            self.assertNotIn(fragment, rendered, text)
        self.assertIn("https://tmp/vendor/doc", rendered)
        self.assertEqual(MODULE.sanitize_text(rendered, []), rendered)
        # The host-rooted spelling after a colon is a host path in both modes.
        for root_only in (True, False):
            host_rooted = "store:" + "/home/operator/x"
            self.assertNotIn(
                "/home/operator/x", MODULE.sanitize_text(host_rooted, [], root_only=root_only)
            )

    def test_root_only_catches_a_file_url_with_no_segment_after_the_root(self):
        # F13: a `file:` URL names an absolute location whether or not a segment
        # follows the host root; only the bare root token in prose stays literal.
        for text in ("opened file:///tmp today", "opened file:///tmp/run today"):
            with self.subTest(text=text):
                rendered = MODULE.sanitize_text(text, [], root_only=True)
                self.assertNotIn("/tmp", rendered, text)
                self.assertIn("path-sha256:", rendered)
        for root_only in (True, False):
            rendered = MODULE.sanitize_text("opened file:///tmp today", [], root_only=root_only)
            self.assertNotIn("file:", rendered)
            self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        self.assertEqual(MODULE.sanitize_text("blocked by `/tmp` alone", [], root_only=True),
                         "blocked by `/tmp` alone")

    def test_root_only_catches_a_root_relative_backslash_spelling(self):
        # F23: classic Windows resolves a leading single backslash against the current
        # drive root, so `\Users\op\run` names the same profile location as
        # `C:\Users\op\run`; the bare root token keeps its `/tmp`-style boundary.
        text = "store " + _BS + "Users" + _BS + "operator" + _BS + "run today"
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                self.assertNotIn("operator", rendered, text)
                self.assertIn("path-sha256:", rendered)
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        self.assertEqual(MODULE.sanitize_text("blocked by `" + _BS + "tmp` alone", [], root_only=True),
                         "blocked by `" + _BS + "tmp` alone")

    def test_a_deep_escaped_file_url_renders_the_whole_location(self):
        # Sol F2: the `file:` head is followed by any run of separators, so an escaped
        # run past the old cap no longer leaves the drive head (`file:\\\\C:`) behind.
        text = json.dumps({"cmd": "file:" + _BS * 4 + "C:" + _BS * 4 + "Users"
                          + _BS * 4 + "operator" + _BS * 4 + "secret"})
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                for fragment in ("C:", "Users", "operator", "secret"):
                    self.assertNotIn(fragment, rendered, text)
                self.assertIn("path-sha256:", rendered)
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)

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
        # The route is closed by punctuation because a bare end-of-line word is a
        # terminal chunk the space continuation absorbs.
        for route in PRODUCT_ROUTES:
            rendered = MODULE.sanitize_text(f"served {route}, twice", [])
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

    def test_cli_check_mode_names_the_scope_it_refused(self):
        # The default scope renders every absolute location, so a product route
        # refuses too: the message must name the scope that refused instead of
        # claiming a host path in a run that only saw a route.
        route = "see /portal/runtimes, twice\n"
        result = self.run_cli(route, "--check")
        self.assertEqual(result.returncode, 1)
        self.assertIn("an absolute location would be rendered", result.stderr)
        self.assertNotIn("a host absolute path", result.stderr)
        # A host path still refuses in the default scope, and the route still passes
        # the scope that only renders host paths.
        self.assertEqual(self.run_cli(r"see C:\Users\operator\x" + "\n", "--check").returncode, 1)
        passed = self.run_cli(route, "--check", "--root-only")
        self.assertEqual(passed.returncode, 0)
        self.assertEqual(passed.stdout, "")

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

    def test_cli_root_only_check_refuses_boundary_spelled_roots(self):
        # STD-B01/B10: a root beside a non-name character - `>` glue, trailing
        # punctuation, a shell `|` or a URL query/fragment - is still the root, and
        # `--check --root-only` must refuse the input.
        drive_root = "C:" + _BS + "Work" + _BS + "run"
        posix_root = "/srv/store"
        cases = (
            ("go test ./... 2>" + drive_root + _BS + "stderr.txt\n", drive_root),
            ("<b>" + drive_root + _BS + "x\n", drive_root),
            (drive_root + ".\n", drive_root),
            (drive_root + "!\n", drive_root),
            (drive_root + "|x\n", drive_root),
            ("stored under " + posix_root + ".\n", posix_root),
            ("see " + posix_root + "!\n", posix_root),
            ("path=" + posix_root + "|x\n", posix_root),
            ("fetch " + posix_root + "?x=1\n", posix_root),
            ("anchor " + posix_root + "#frag\n", posix_root),
        )
        for text, root in cases:
            with self.subTest(text=text):
                result = self.run_cli(text, "--check", "--root-only", "--root", root)
                self.assertEqual(result.returncode, 1, text)
                self.assertEqual(result.stdout, "", text)
                self.assertIn("refused", result.stderr)

    def test_cli_root_only_leaves_name_continued_spellings_literal(self):
        # The boundary negatives: a longer name (`-user`, `.bak`), a name glued in
        # front (`aaC:\...`) or a relative spelling (`../srv/store`) is not this
        # root, so the root-only check refuses nothing.
        posix_root = "/srv/store"
        drive_root = "C:" + _BS + "Work" + _BS + "run"
        for text, root in (
            (posix_root + "-user/x\n", posix_root),
            (posix_root + ".bak/x\n", posix_root),
            (posix_root + "-user\n", posix_root),
            (".." + posix_root + "/x\n", posix_root),
            ("aa" + drive_root + _BS + "x\n", drive_root),
        ):
            with self.subTest(text=text):
                result = self.run_cli(text, "--check", "--root-only", "--root", root)
                self.assertEqual(result.returncode, 0, text)
                self.assertEqual(result.stdout, "", text)

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

    def test_cli_refuses_mixed_case_file_scheme_spellings(self):
        # STD-A05: the check must flag the scheme however it is spelled.
        for text in ("FILE:///C:/Work/Personal", "File:///C:/Work/Personal",
                     "FILE:///home/operator/run"):
            with self.subTest(text=text):
                result = self.run_cli(text + "\n", "--check", "--root-only")
                self.assertEqual(result.returncode, 1, text)
                self.assertEqual(result.stdout, "", text)
                self.assertIn("refused", result.stderr)

    def test_cli_refuses_a_file_authority_with_a_drive_location(self):
        # STD-A06: the original bytes refuse in both scopes, the render replaces the
        # whole location, and the rendered text passes the root-only check.
        text = "opened file://localhost/C:/Work/Personal, today"
        for arguments in (["--check", "--root-only"], ["--check"]):
            with self.subTest(arguments=arguments):
                result = self.run_cli(text + "\n", *arguments)
                self.assertEqual(result.returncode, 1, text)
                self.assertIn("refused", result.stderr)
        rendered = MODULE.sanitize_text(text, [], root_only=True)
        recheck = self.run_cli(rendered + "\n", "--check", "--root-only")
        self.assertEqual(recheck.returncode, 0, recheck.stderr)

    def test_cli_root_only_renders_stdin_with_routes_kept(self):
        result = self.run_cli("see /portal/runtimes under " + HOST_PATHS[4] + "\n",
                              "--root-only", "--root", ROOT)
        self.assertEqual(result.returncode, 0)
        self.assertIn("/portal/runtimes", result.stdout)
        self.assertIn(digest_of(HOST_PATHS[4]), result.stdout)

    def test_cli_refuses_an_existing_but_unreadable_file(self):
        # F22: a file the checker cannot read is not a verdict about recorded text -
        # an uncaught traceback exits 1, which `--check --root-only` reads as "still
        # carries a host absolute path". The tool refuses it as unusable (exit 2),
        # like every other input it cannot inspect.
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "unreadable.txt"
            path.write_text("clean text\n", encoding="utf-8")
            restore = None
            if os.name == "nt":
                user = os.environ.get("USERNAME", "")
                denied = subprocess.run(
                    ["icacls", str(path), "/deny", f"{user}:(R)"],
                    capture_output=True, text=True, check=False,
                )
                if denied.returncode != 0:
                    self.skipTest(f"cannot deny read access: {denied.stdout}{denied.stderr}")
                restore = lambda: subprocess.run(
                    ["icacls", str(path), "/remove:d", user], capture_output=True
                )
            else:
                try:
                    path.chmod(0)
                except OSError as error:
                    self.skipTest(f"cannot remove read permission: {error}")
                restore = lambda: path.chmod(0o600)
            try:
                try:
                    with path.open("rb"):
                        self.skipTest("this host can still read the restricted file")
                except OSError:
                    pass
                result = subprocess.run(
                    [sys.executable, str(SCRIPT), "--check", "--root-only", str(path)],
                    capture_output=True, check=False,
                )
            finally:
                # The mode is restored inside the TemporaryDirectory block, before the
                # context manager deletes the directory: a unittest `addCleanup` runs
                # after that deletion and on POSIX raised FileNotFoundError from the
                # chmod, turning the hosted daemon job red (Sol P1).
                if restore is not None:
                    restore()
            self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
            self.assertEqual(result.stdout, b"")
            self.assertIn("refused", result.stderr.decode("utf-8"))


if __name__ == "__main__":
    unittest.main()
