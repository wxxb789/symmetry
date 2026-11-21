#!/usr/bin/env python3

import importlib.util
import json
import pathlib
import subprocess
import sys
import unittest
import unittest.mock

# The sibling raw-reading module's name carries dashes, so it is loaded by path; the
# fixtures (the loaded tool, the disposable root and `digest_of`) live there.
_SIBLING = pathlib.Path(__file__).with_name("test-model-routing-record-sanitize.py")
_spec = importlib.util.spec_from_file_location("test_model_routing_record_sanitize", _SIBLING)
sibling = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(sibling)
MODULE, ROOT, SCRIPT, digest_of = sibling.MODULE, sibling.ROOT, sibling.SCRIPT, sibling.digest_of

_BS = chr(92)

ESCAPE_N, ESCAPE_T = _BS + "n", _BS + "t"
ESCAPE_U0020 = _BS + "u0020"
ESCAPE_SPELLINGS = (ESCAPE_N, ESCAPE_T, _BS + "r", _BS + "b", _BS + "f", ESCAPE_U0020)
HOST_RUNNER_WORK, HOST_TMP_RUN = "/home/runner/work/x", "/tmp/symmetry-run"
HOST_ROOT_LOCATIONS = (
    HOST_RUNNER_WORK, "/Users/op/run", "/root/native", HOST_TMP_RUN,
    "/var/folders/ab/T", "/mnt/q/repos/symmetry", "/private/var/tmp/symmetry",
)
# What a writer's `json.dumps` spells for `C:\Users\op\run`.
ESCAPED_DRIVE_RUN = "C:" + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run"
# The digest the TODO entry records for the path text alone; the entry's own input
# must render exactly it.
ENTRY_PATH_DIGEST = "path-sha256:26e7b5b42607730bbb3c0fdb4d10159a3c52a1c4004d4cb4fc281175bc3f9fb8"


def run_cli(text, *arguments):

    return subprocess.run(
        [sys.executable, str(SCRIPT), *arguments],
        input=text, text=True, capture_output=True, check=False,
    )


class JsonReadingTests(unittest.TestCase):
    def test_a_json_escape_after_a_posix_path_survives_the_render(self):
        # STD-A07: a bare backslash in the segment classes would take the JSON `\n` escape and
        # the evidence line into the token; the decoded reading stops the tail at the newline.
        for escape in (ESCAPE_N, _BS + "u000a"):
            text = '{"out":"working directory: /tmp/run' + escape + 'FAIL: verifier refused"}'
            for root_only in (True, False):
                with self.subTest(escape=escape, root_only=root_only):
                    rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                    self.assertEqual(
                        rendered,
                        '{"out":"working directory: ' + digest_of("/tmp/run")
                        + escape + 'FAIL: verifier refused"}',
                    )
                    self.assertEqual(
                        json.loads(rendered),
                        {"out": "working directory: " + digest_of("/tmp/run")
                         + "\nFAIL: verifier refused"},
                    )
                    self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)

    def test_a_json_escape_after_a_windows_path_survives_the_render(self):
        # The entry's own reproduction: the decoded reading binds the location's recorded bytes
        # (`path-sha256:26e7b5b4…`, the digest the entry records for the path alone).
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(
                    '{"output":"wrote ' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED"}', [],
                    root_only=root_only,
                )
                self.assertEqual(
                    rendered, '{"output":"wrote ' + digest_of(ESCAPED_DRIVE_RUN) + _BS + 'nFAILED"}',
                )
                self.assertIn(ENTRY_PATH_DIGEST, rendered)
                self.assertEqual(digest_of(ESCAPED_DRIVE_RUN), ENTRY_PATH_DIGEST)
                self.assertEqual(
                    json.loads(rendered)["output"], "wrote " + digest_of(ESCAPED_DRIVE_RUN) + "\nFAILED",
                )
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)

    def test_a_json_escape_after_every_windows_family_survives_the_render(self):
        cases = (
            _BS * 4 + "server" + _BS * 2 + "share" + _BS * 2 + "run",     # UNC, doubly escaped leader
            _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run",         # root-relative spelling
            "file:" + _BS * 2 + "server" + _BS * 2 + "share" + _BS * 2 + "run",
        )
        for path in cases:
            for root_only in (True, False):
                with self.subTest(path=path, root_only=root_only):
                    text = '{"p":"' + path + _BS + 'nFAILED"}'
                    rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                    self.assertEqual(rendered, '{"p":"' + digest_of(path) + _BS + 'nFAILED"}')
                    self.assertEqual(json.loads(rendered)["p"], digest_of(path) + "\nFAILED")
                    self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        # A forward-slash spelling carries no escaped pair, so the raw look union absorbs its
        # whole span: the escape and the word behind it go into the token (fail-closed).
        absorbed = (
            "//server/share/run",              # slash-UNC authority
            "file:///C:/Users/op/run",         # file: URL
        )
        for path in absorbed:
            whole = path + _BS + "nFAILED"
            for root_only in (True, False):
                with self.subTest(path=path, shape="absorbed", root_only=root_only):
                    rendered = MODULE.sanitize_text('{"p":"' + whole + '"}', [], root_only=root_only)
                    self.assertEqual(rendered, '{"p":"' + digest_of(whole) + '"}')
                    self.assertEqual(json.loads(rendered)["p"], digest_of(whole))
                    self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        # A JSON-escaped backslash stays a path byte: the record's `\\` decodes to two literal
        # backslashes, so the whole spelling is one token, per the JSON grammar.
        pair = "C:" + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run" + _BS * 4 + "n"
        for root_only in (True, False):
            with self.subTest(shape="escaped-backslash", root_only=root_only):
                rendered = MODULE.sanitize_text('{"p":"' + pair + '"}', [], root_only=root_only)
                self.assertEqual(rendered, '{"p":"' + digest_of(pair) + '"}')
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        # The accepted boundary, pinned rather than silent: a mixed spelling (`C:\\a\repo`) is
        # read as JSON, so no path byte is left in front of the residue and rendering stays idempotent.
        mixed = "C:" + _BS * 2 + "a" + _BS + "repo"
        for root_only in (True, False):
            with self.subTest(shape="mixed-spelling", root_only=root_only):
                rendered = MODULE.sanitize_text('{"p":"' + mixed + '"}', [], root_only=root_only)
                self.assertEqual(
                    rendered, '{"p":"' + digest_of("C:" + _BS * 2 + "a") + _BS + 'repo"}',
                )
                self.assertEqual(
                    json.loads(rendered)["p"], digest_of("C:" + _BS * 2 + "a") + "\r" + "epo",
                )
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)

    def test_an_escaped_space_nbsp_and_apostrophe_stay_inside_the_location(self):
        # A space, NBSP or U+3000 a JSON writer escaped (`json.dumps` spells non-ASCII as
        # `\uXXXX`) is the path character it decodes to; a partial render republishes the tail.
        cases = (
            '{"p":"C:' + _BS * 2 + "Program" + _BS + "u0020Files" + _BS * 2 + "op" + _BS * 2 + "secret" + '"}',
            '{"p": "C:' + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "my" + _BS + 'u00a0project"}',
            '{"p": "C:' + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "my" + _BS + 'u3000project"}',
            '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "O" + _BS + "u0027Brien" + _BS * 2 + "secret" + '"}',
            '{"p": "C:' + _BS * 2 + "Users" + _BS * 2 + "First Last" + _BS * 2 + "my" + _BS + 'u00a0project"}',
        )
        for text in cases:
            with self.subTest(text=text):
                json.loads(text)  # the document parses: the decoded reading owns it
                spelling = text.split('"', 3)[3][:-2]
                for root_only in (True, False):
                    with self.subTest(text=text, root_only=root_only):
                        rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                        self.assertEqual(rendered, text.replace(spelling, digest_of(spelling)), text)
                        self.assertEqual(
                            MODULE.sanitize_text(rendered, [], root_only=root_only), rendered
                        )

    def test_an_escaped_quote_and_surrogate_pair_keep_the_mapping(self):
        raw_path = "C:" + _BS * 2 + "dir" + _BS * 2 + "file"
        text = '{"t": "see ' + _BS + '"' + raw_path + _BS + '", ok", "n": 1}'
        rendered = MODULE.sanitize_text(text, [], root_only=True)
        self.assertEqual(
            json.loads(rendered), {"t": 'see "' + digest_of(raw_path) + '", ok', "n": 1},
        )
        tail_path = "C:" + _BS * 2 + "dir" + _BS * 2
        tail_rendered = MODULE.sanitize_text('{"t": "' + tail_path + '"}', [], root_only=True)
        self.assertEqual(json.loads(tail_rendered), {"t": digest_of(tail_path)})
        run3 = MODULE.sanitize_text(
            '{"t": "see ' + _BS + '"' + tail_path + _BS + '", ok"}', [], root_only=True,
        )
        self.assertEqual(json.loads(run3), {"t": 'see "' + digest_of(tail_path) + '", ok'})
        # An odd number of `\"` escapes before the path keeps one literal; an escaped quote
        # ending it would read the path raw and absorb the following escape and line.
        odd = ('{"out":"see ' + _BS + '"' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED"}')
        odd_rendered = MODULE.sanitize_text(odd, [], root_only=True)
        self.assertEqual(
            odd_rendered,
            '{"out":"see ' + _BS + '"' + digest_of(ESCAPED_DRIVE_RUN) + _BS + 'nFAILED"}',
        )
        self.assertEqual(json.loads(odd_rendered)["out"],
                         'see "' + digest_of(ESCAPED_DRIVE_RUN) + "\nFAILED")
        # A surrogate pair stays its two decoded units, six recorded bytes each: the pairing
        # step changed no match, mapping or token, so the location still renders whole.
        pair_path = "C:" + _BS * 2 + "Users" + _BS * 2 + "x" + _BS + "ud83d" + _BS + "ude00" \
            + _BS * 2 + "run"
        pair_rendered = MODULE.sanitize_text('{"p":"' + pair_path + '"}', [], root_only=True)
        self.assertEqual(pair_rendered, '{"p":"' + digest_of(pair_path) + '"}')
        for shape in (rendered, tail_rendered, run3, odd_rendered, pair_rendered):
            self.assertEqual(MODULE.sanitize_text(shape, [], root_only=True), shape)

    def test_the_root_matchers_escape_their_own_recorded_bytes(self):
        root, text = "/srv/store)", '{"out":"/srv/store)/x' + _BS + 'n"}'
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [root], root_only=root_only)
                self.assertEqual(rendered, '{"out":"<disposable-root>/x' + _BS + 'n"}')
                self.assertEqual(MODULE.sanitize_text(rendered, [root], root_only=root_only), rendered)
        masked = '{"out":"/srv/store' + _BS + 'n"}'
        self.assertEqual(MODULE.sanitize_text(masked, [root], root_only=True), masked)
        self.assertEqual(
            MODULE.sanitize_text(masked, [root], root_only=False),
            '{"out":"' + digest_of("/srv/store") + _BS + 'n"}',
        )
        drive_root = "C:/a))b/run"
        drive_text = '{"p":"C:' + _BS * 2 + "a))b" + _BS * 2 + "run" + _BS + 'n"}'
        self.assertEqual(
            MODULE.sanitize_text(drive_text, [drive_root], root_only=True),
            '{"p":"<disposable-root>' + _BS + 'n"}',
        )
        once = MODULE.sanitize_text('{"p":"/srv/store/srv/store"}', ["/srv/store"], root_only=True)
        self.assertEqual(once, '{"p":"<disposable-root>/srv/store"}')
        self.assertEqual(
            MODULE.sanitize_text(once, ["/srv/store"], root_only=True), once,
        )
        self.assertEqual(
            MODULE.sanitize_text('{"p":"C:/a/b"}', ["C:/a"], root_only=True),
            '{"p":"<disposable-root>/b"}',
        )
        raw_root = "C:" + _BS + "repo"
        self.assertEqual(
            MODULE.sanitize_text('{"p":"' + raw_root + '"}', ["C:/repo"], root_only=True),
            '{"p":"' + digest_of(raw_root) + '"}',
        )

    def test_a_token_never_binds_another_tokens_text(self):
        # A later matcher must not touch bytes an earlier replacement wrote: a raw match is
        # never dropped for overlapping a claim, so every digest binds recorded bytes alone.
        text = '{"out":"' + _BS + 'u0008//r' + _BS + 'bfile:C:/"}'
        whole = "//r" + _BS + "bfile:C:/"
        expected = '{"out":"' + _BS + 'u0008' + digest_of(whole) + '"}'
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                self.assertEqual(rendered, expected)
                self.assertEqual(json.loads(rendered)["out"], "\b" + digest_of(whole))
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        prose = "worktree " + _BS + "Users" + _BS + "zzalpha" + _BS + "zzbeta" + _BS + "n/child"
        whole = _BS + "Users" + _BS + "zzalpha" + _BS + "zzbeta" + _BS + "n/child"
        for root_only in (True, False):
            with self.subTest(shape="prose", root_only=root_only):
                rendered = MODULE.sanitize_text(prose, [], root_only=root_only)
                self.assertEqual(rendered, "worktree " + digest_of(whole))
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        token_tail = "//r" + _BS + "u005c/srv/store"
        self.assertEqual(
            MODULE.sanitize_text('{"p":"' + token_tail + '"}', ["/srv/store"], root_only=True),
            '{"p":"' + digest_of(token_tail) + '"}',
        )
        # The filter that removes a rendered token's own text from the raw look's spans: without
        # it a raw spelling glued in front of a token binds that token's text into a second digest.
        existing = "path-sha256:" + "a" * 64
        glued = "C:" + _BS + "temp" + _BS + "run" + existing
        for root_only in (True, False):
            with self.subTest(shape="glued-token", root_only=root_only):
                rendered = MODULE.sanitize_text('{"k":"' + glued + '"}', [], root_only=root_only)
                self.assertEqual(
                    rendered, '{"k":"' + digest_of(glued[:-len(existing)]) + existing + '"}',
                )
                self.assertEqual(
                    json.loads(rendered)["k"], digest_of(glued[:-len(existing)]) + existing,
                )
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)

    def test_the_reading_is_decided_at_the_document_level(self):
        jsonl = (
            '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run" + _BS + 'nFAILED"}\n'
            '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run2" + _BS + 'nFAILED"}\n'
        )
        rendered = MODULE.sanitize_text(jsonl, [], root_only=True)
        self.assertEqual(
            rendered,
            '{"p":"' + digest_of("C:" + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run")
            + _BS + 'nFAILED"}\n'
            + '{"p":"' + digest_of("C:" + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run2")
            + _BS + 'nFAILED"}\n',
        )
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        # One prose line makes the whole document prose: a stray quote's mispairing cannot reach
        # into the JSON line, and the raw reading absorbs the escape and line with the location.
        mixed = "the 3\" bolt\n" + '{"output":"wrote ' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED"}'
        absorbed = ESCAPED_DRIVE_RUN + _BS + "nFAILED"
        rendered = MODULE.sanitize_text(mixed, [], root_only=True)
        self.assertEqual(rendered, 'the 3" bolt\n{"output":"wrote ' + digest_of(absorbed) + '"}')
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        markdown = "## Run\n\n```json\n{\"p\":\"" + ESCAPED_DRIVE_RUN + "\"}\n```\n"
        self.assertEqual(
            MODULE.sanitize_text(markdown, [], root_only=True),
            '## Run\n\n```json\n{"p":"' + digest_of(ESCAPED_DRIVE_RUN) + '"}\n```\n',
        )

    def test_a_raw_single_backslash_spelling_inside_json_is_rendered(self):
        # The decoded reading stays primary; the raw pass also covers the recorded bytes, else a
        # `C:\repo\tmp` spelling (its separators are JSON escapes) stays unrendered - fail-open.
        cases = (
            "C:" + _BS + "repo" + _BS + "tmp",    # `\r` then `\t`
            "C:" + _BS + "temp" + _BS + "run",    # `\t` then `\r`
            "C:" + _BS + "name" + _BS + "foo",    # `\n` then `\f`
            "C:" + _BS + "foo" + _BS + "bar",     # `\f` then `\b`
            "C:" + _BS + "bar" + _BS + "run",     # `\b` then `\r`
        )
        for path in cases:
            text = '{"p":"' + path + '"}'
            json.loads(text)  # the document parses: the decoded reading owns the span
            for root_only in (True, False):
                with self.subTest(path=path, root_only=root_only):
                    rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                    self.assertEqual(rendered, '{"p":"' + digest_of(path) + '"}')
                    self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        # A recorded `\n` reads as a separator and a name, so the raw match renders the location
        # plus the escape plus the line whole - the prose reading's disclosed over-approximation.
        sealed = "C:" + _BS + "temp" + _BS + "nFAILED"
        for root_only in (True, False):
            with self.subTest(shape="escape-followed", root_only=root_only):
                rendered = MODULE.sanitize_text('{"p":"' + sealed + '"}', [], root_only=root_only)
                self.assertEqual(rendered, '{"p":"' + digest_of(sealed) + '"}')
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)

    def test_a_raw_location_inside_an_earlier_match_is_still_rendered(self):
        # The raw look rescans from each match's first byte plus one: a greedy match tail can
        # swallow the start of the location behind it, which must render as one union token.
        cases = (
            (
                "C:" + _BS + "fC:" + _BS + "tsrv" + _BS * 2 + "xfile",
                '{"k":"C:' + _BS + 'fC:' + _BS + 'tsrv' + _BS * 2 + 'xfile:"}',
            ),
            (
                "C:" + _BS + "tC:" + _BS * 3 + "r " + _BS + "n",
                '{"k":" C:' + _BS + 'tC:' + _BS * 3 + 'r ' + _BS + 'n"}',
            ),
        )
        for bound, text in cases:
            digest = digest_of(bound)
            expected = text.replace(bound, digest)
            with self.subTest(text=text, root_only=True):
                rendered = MODULE.sanitize_text(text, [], root_only=True)
                self.assertEqual(rendered, expected)
                self.assertIn(digest, json.loads(rendered)["k"])
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        # The resume window is `_DEVICE_PREFIX`'s width plus one: a narrower window skips the
        # second location's start and leaves its raw separators literal behind the token.
        straddled = ("D:" + _BS + "bx" + _BS + "n" + _BS * 4 + "?" + _BS * 4
                     + "C:" + _BS + "temp" + _BS + "run")
        text = '{"k":"' + straddled + '"}'
        expected = '{"k":"' + digest_of(straddled) + '"}'
        for root_only in (True, False):
            with self.subTest(shape="device-prefix-straddle", root_only=root_only):
                rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                self.assertEqual(rendered, expected)
                self.assertEqual(json.loads(rendered)["k"], digest_of(straddled))
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        # The resume window's other side, pinned rather than silent: a start deeper than
        # `_CONTAINED_RESUME` is not read, so the pair-bearing rule governs the nested spelling.
        nested = "//tserver" + _BS * 6 + "-" + _BS + "//run" + _BS + "u0020x/rootx" + _BS + "/opop/tmp"
        escape = _BS + "u000a"
        text = '{"p":"' + nested + escape + '"}'
        rendered = MODULE.sanitize_text(text, [], root_only=True)
        self.assertEqual(rendered, '{"p":"' + digest_of(nested) + escape + '"}')
        self.assertEqual(json.loads(rendered)["p"], digest_of(nested) + "\n")
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)

    def test_a_host_path_after_a_json_escape_is_caught(self):
        # #119: the escape's letter must be a boundary, not a name character glued in front of
        # the location; else the recorded host path passes `--check --root-only` unrendered.
        for escape in ESCAPE_SPELLINGS:
            for path in HOST_ROOT_LOCATIONS:
                with self.subTest(escape=escape, path=path):
                    text = '{"out":"ok' + escape + path + ', failed"}'
                    rendered = MODULE.sanitize_text(text, [], root_only=True)
                    self.assertEqual(rendered, '{"out":"ok' + escape + digest_of(path) + ', failed"}')
                    self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        for path in ("C:" + _BS * 2 + "Users" + _BS * 2 + "op", _BS * 4 + "server" + _BS * 2 + "share",
                     "//server/share", "file:///C:/Users/op"):
            with self.subTest(path=path):
                text = '{"out":"ok' + _BS + 'n' + path + ', failed"}'
                self.assertEqual(
                    MODULE.sanitize_text(text, [], root_only=True),
                    '{"out":"ok' + _BS + 'n' + digest_of(path) + ', failed"}',
                )
        cmd = '{"cmd":"go test' + _BS + 'n/etc/hosts"}'
        self.assertEqual(MODULE.sanitize_text(cmd, []),
                         '{"cmd":"go test' + _BS + 'n' + digest_of("/etc/hosts") + '"}')
        self.assertEqual(MODULE.sanitize_text(cmd, [], root_only=True), cmd)
        # The boundary negatives stay: the escape allowance does not weaken the name rejection,
        # so a literal name character glued in front is still not this location.
        for text in ('{"out":"ok' + _BS + 'naa/home/op/run"}',
                     '{"out":"ok' + _BS + 'n../home/op/run"}',
                     '{"out":"ok' + _BS + 'nx_/home/op/run"}',
                     '{"out":"oka/srv/store/x"}'):
            with self.subTest(text=text):
                self.assertEqual(
                    MODULE.sanitize_text(text, ["/srv/store"], root_only=True), text
                )

    def test_a_host_path_in_a_json_key_is_rendered(self):
        paths = (ESCAPED_DRIVE_RUN, HOST_RUNNER_WORK)
        cases = (
            ("document", '{"' + paths[0] + '": 1, "p": "' + paths[1] + '"}'),
            ("jsonl", '{"' + paths[0] + '": 1}\n{"' + paths[1] + '": 2}\n'),
        )
        for name, text in cases:
            expected = text.replace(paths[0], digest_of(paths[0])).replace(
                paths[1], digest_of(paths[1]),
            )
            for root_only in (True, False):
                with self.subTest(shape=name, root_only=root_only):
                    rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                    self.assertEqual(rendered, expected)
                    self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
                    if name == "document":
                        self.assertEqual(json.loads(rendered), json.loads(expected))

    def test_a_raw_match_without_a_backslash_is_left_to_the_decoded_reading(self):
        # The raw look's no-backslash filter: a Windows-shaped match with no backslash is bytes
        # the decoded reading owns; dropping the filter swallows the DEL byte and what follows.
        text = '{"p":"C:/x' + chr(0x7F) + 'y) ok' + _BS + 'nFAILED"}'
        rendered = MODULE.sanitize_text(text, [], root_only=True)
        self.assertEqual(
            rendered, '{"p":"' + digest_of("C:/x") + chr(0x7F) + 'y) ok' + _BS + 'nFAILED"}',
        )
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)

    def test_the_fail_closed_property_holds_for_every_repro_shape(self):
        # The invariant, as a property over the shapes both reviews reproduced: no decoded string
        # keeps a host-path segment, the render passes the check the input fails, and it is a fixpoint.
        cases = (
            ("escaped-space", '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "my" + _BS + 'u0020project"}',
             [], ("Users", "project"), True),
            ("nbspan", json.dumps({"p": "C:" + _BS + "Users" + _BS + "op" + _BS + "my\u00a0project"}),
             [], ("Users", "project"), True),
            ("ideographic-space", json.dumps({"p": "C:" + _BS + "Users" + _BS + "op" + _BS + "my\u3000project"}),
             [], ("Users", "project"), True),
            ("escaped-apostrophe",
             '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "O" + _BS + "u0027Brien" + _BS * 2 + "secret" + '"}',
             [], ("Users", "Brien"), True),
            ("space-continued",
             '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "run name" + _BS + 'nFAILED"}',
             [], ("Users", "run"), True),
            ("root-space-continued", '{"out":"/srv/store name' + _BS + 'nFAILED"}',
             ["/srv/store"], ("srv", "store"), True),
            ("token-prefix", '{"out":"' + _BS + 'u0008//r' + _BS + 'bfile:C:/"}',
             [], ("file:", "C:/"), True),
            ("stray-quote",
             "the 3\" bolt\n" + '{"output":"wrote ' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED"}',
             [], ("Users", "FAILED"), False),
            ("root-metacharacter", '{"out":"/srv/store)/x' + _BS + 'n"}',
             ["/srv/store)"], ("srv", "store"), True),
            ("entry-windows", '{"output":"wrote ' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED"}',
             [], ("Users", "run"), True),
            ("entry-temp", '{"p":"C:' + _BS * 2 + 'temp' + _BS + 'nFAILED"}',
             [], ("temp",), True),
            ("round5-posix", '{"out":"ok' + _BS + 'n/home/runner/work/x failed"}',
             [], ("runner", "work"), True),
            ("round5-root", '{"out":"ok' + _BS + 'n/srv/store/x failed"}',
             ["/srv/store"], ("srv", "store"), True),
            ("surrogate-pair",
             json.dumps({"p": "C:" + _BS + "Users" + _BS + "op" + _BS + "x\U0001F600" + _BS + "run"}),
             [], ("Users", "run"), True),
            ("doubled-backslash",
             '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run" + _BS * 4 + 'n"}',
             [], ("Users", "run"), True),
            # The r2 raw look: a spelling whose separators are `\r`, `\t`, `\n`, `\f` and `\b`
            # decodes into control characters, so only the raw reading sees the location.
            ("raw-repo-tmp", '{"p":"C:' + _BS + "repo" + _BS + 'tmp"}', [], ("repo", "tmp"), True),
            ("raw-temp-run", '{"p":"C:' + _BS + "temp" + _BS + 'run"}', [], ("temp", "run"), True),
            ("raw-name-foo", '{"p":"C:' + _BS + "name" + _BS + 'foo"}', [], ("name", "foo"), True),
            ("raw-foo-bar", '{"p":"C:' + _BS + "foo" + _BS + 'bar"}', [], ("foo", "bar"), True),
            ("raw-bar-run", '{"p":"C:' + _BS + "bar" + _BS + 'run"}', [], ("bar", "run"), True),
            ("raw-escape-followed", '{"p":"C:' + _BS + "temp" + _BS + 'nFAILED"}',
             [], ("temp", "FAILED"), True),
            # A raw UNC spelling: no single backslash spells a leader in a valid literal, so the
            # recorded bytes are the location's only spelling and both dialects render.
            ("unc-raw-separator",
             '{"p":"' + _BS * 2 + "server" + _BS * 2 + "share" + _BS + 'nFAILED"}',
             [], ("server", "share", "FAILED"), True),
            ("slash-unc-raw-separator", '{"p":"//server' + _BS + 'tmp"}',
             [], ("server", "tmp"), True),
            # The forward-slash spelling: its raw match carries no escaped pair, so the union
            # absorbs its whole span - the accepted over-redaction, fail-closed.
            ("slash-unc-escape-absorbed", '{"p":"//server/share/run' + _BS + 'nFAILED"}',
             [], ("server", "share", "run", "FAILED"), True),
            ("slash-prefix-raw-suffix", '{"p":"C:/Users' + _BS + 'nop' + _BS + 'repo"}',
             [], ("Users", "op", "repo"), True),
            ("slash-prefix-raw-suffix-jsonl",
             '{"p":"C:/Users' + _BS + 'nop' + _BS + 'repo"}\n'
             '{"p":"C:/Users' + _BS + 'nop' + _BS + 'repo2"}\n',
             [], ("Users", "op", "repo"), False),
            ("escaped-prefix-raw-suffix",
             '{"p":"C:' + _BS * 2 + "Users" + _BS + 'nop' + _BS + 'repo"}',
             [], ("Users", "op", "repo"), True),
            ("odd-run-raw-suffix", '{"p":"C:' + _BS * 3 + "temp" + _BS * 2 + 'op"}',
             [], ("temp", "op"), True),
            ("key-carrying", '{"' + ESCAPED_DRIVE_RUN + '":1}',
             [], ("Users", "run"), True),
            # A match starting after an odd backslash run: without span snapping the token lands
            # behind a lone backslash (`\p`), so the rendered literal stops parsing as JSON.
            ("split-run-boundary", '{"p":"' + _BS + 'file:///C:/x' + _BS * 2 + '"}',
             [], ("file:", "C:/"), True),
            ("raw-root-relative", '{"p":"' + _BS + "root" + _BS + 'run"}',
             [], ("root", "run"), True),
            ("raw-file-url", '{"p":"file:' + _BS + "temp" + _BS + 'run"}',
             [], ("temp", "run"), True),
            ("mixed-literal",
             '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "op and D:" + _BS + 'repo"}',
             [], ("Users", "repo"), True),
        )
        for name, text, roots, fragments, json_expected in cases:
            with self.subTest(name=name):
                arguments = []
                for root in roots:
                    arguments += ["--root", root]
                self.assertEqual(run_cli(text, "--check", "--root-only", *arguments).returncode, 1, name)
                rendered = MODULE.sanitize_text(text, roots, root_only=True)
                self.assertNotEqual(rendered, text, name)
                self.assertEqual(MODULE.sanitize_text(rendered, roots, root_only=True), rendered, name)
                self.assertEqual(
                    run_cli(rendered, "--check", "--root-only", *arguments).returncode, 0, name,
                )
                if json_expected:
                    decoded = json.loads(rendered)
                    body = json.dumps(decoded, ensure_ascii=False)
                else:
                    body = rendered
                for fragment in fragments:
                    self.assertNotIn(fragment, body, f"{name}: {rendered!r}")

    def test_a_wide_record_is_rebuilt_once_per_pass_not_once_per_match(self):
        # One pass is one rebuild (`_rebuild_with_groups`): the pin counts its calls, so the count
        # must not grow with the number of matches, and the rendered bytes are checked too.
        def rebuilds(text):
            calls = []
            original = MODULE._rebuild_with_groups

            def counted(*arguments):
                calls.append(1)
                return original(*arguments)

            with unittest.mock.patch.object(MODULE, "_rebuild_with_groups", counted):
                rendered = MODULE.sanitize_text(text, [], root_only=True)
            return len(calls), rendered

        literal_counts = []
        for paths in (1000, 8000):
            content = " ".join("C:" + _BS * 2 + "Users" + _BS * 2 + ("p%d" % index)
                               for index in range(paths))
            calls, rendered = rebuilds('{"p":"' + content + '"}')
            literal_counts.append(calls)
            self.assertEqual(rendered.count("path-sha256:"), paths)
            self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        prose_counts = []
        for lines in (1000, 8000):
            text = "\n".join("line %d wrote C:" % index + _BS + "temp" + _BS + ("run%d" % index)
                             for index in range(lines))
            calls, rendered = rebuilds(text)
            prose_counts.append(calls)
            self.assertEqual(rendered.count("path-sha256:"), lines)
            self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        # 8x the matches, the same number of rebuilds: one per matching pass (the pass
        # sequence is at most the nine families plus two matchers per `--root`).
        self.assertEqual(literal_counts[0], literal_counts[1])
        self.assertEqual(prose_counts[0], prose_counts[1])
        self.assertLessEqual(max(literal_counts + prose_counts), 12)

    def test_a_wide_raw_listing_is_scanned_once_per_location(self):
        # The raw look re-enters only a small window behind each kept match: the pin counts the
        # characters its searches match, which grow with the listing, not the lines times it.
        class CountingPattern:
            def __init__(self, pattern):
                self.pattern = pattern
                self.calls = 0
                self.matched = 0
                self.anchor_calls = 0
                self.anchor_matched = 0

            def search(self, *arguments):
                match = self.pattern.search(*arguments)
                self.calls += 1
                if match is not None:
                    self.matched += match.end() - match.start()
                return match

            def match(self, *arguments):
                # The anchored pass (`_raw_literal_spans`' docstring): one bounded attempt per
                # claim end per family, counted apart from the scan's own searches.
                match = self.pattern.match(*arguments)
                self.anchor_calls += 1
                if match is not None:
                    self.anchor_matched += match.end() - match.start()
                return match

        def scanned(text, roots=()):
            patterns = tuple(CountingPattern(pattern) for pattern in MODULE._RAW_READING.windows)
            with unittest.mock.patch.object(
                MODULE, "_RAW_READING", unittest.mock.Mock(windows=patterns),
            ):
                rendered = MODULE.sanitize_text(text, list(roots), root_only=True)
            return (
                sum(p.matched for p in patterns), sum(p.calls for p in patterns),
                sum(p.anchor_calls for p in patterns), sum(p.anchor_matched for p in patterns),
                rendered,
            )

        listing_counts = []
        for lines in (1000, 8000):
            paths = [_BS * 2 + "srv" + _BS + "share" + _BS + ("dir%d" % index)
                     for index in range(lines)]
            text = json.dumps({"stdout": "\n".join(paths)})
            matched, calls, anchor_calls, anchor_matched, rendered = scanned(text)
            listing_counts.append((matched, calls, anchor_calls, anchor_matched, len(text)))
            self.assertEqual(rendered.count("path-sha256:"), lines)
            self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        matched_1000, calls_1000, anchor_calls_1000, anchor_matched_1000, length_1000 = listing_counts[0]
        matched_8000, calls_8000, anchor_calls_8000, anchor_matched_8000, length_8000 = listing_counts[1]
        # 8x the lines: a linear scan's covered characters grow ~8x, the scan that re-ran a tail per line grew ~64x.
        self.assertLess(matched_8000, matched_1000 * 16)
        # An absolute bound: one listing's own bytes, not its bytes times its lines.
        self.assertLess(matched_1000, length_1000 * 4)
        self.assertLess(matched_8000, length_8000 * 4)
        # The scan finds the listing's first location and re-enters only its own last
        # bytes: a handful of searches for the whole listing, not one per line.
        self.assertLessEqual(calls_1000, 64)
        self.assertLessEqual(calls_8000, 64)
        # The anchored pass attempts one bounded match per claim end per family, so its attempts
        # grow with the listing's lines; no claim end here starts a family body, so it reads nothing.
        self.assertLessEqual(anchor_calls_1000, 8 * 1000)
        self.assertLessEqual(anchor_calls_8000, 8 * 8000)
        self.assertEqual(anchor_matched_1000, 0)
        self.assertEqual(anchor_matched_8000, 0)
        # With `--root`, each line's root token ends where a family body starts - the shape the counters above miss.
        rooted_matched = []
        for lines in (1000, 8000):
            text = json.dumps({"o": "\n".join("/srv/store//run%d" % index for index in range(lines))})
            _, _, root_calls, root_matched, rendered = scanned(text, ["/srv/store"])
            rooted_matched.append(root_matched)
            self.assertEqual(rendered.count("path-sha256:"), 1)  # one union token for the listing
            self.assertLessEqual(root_calls, 8 * lines)
            self.assertLess(root_matched, len(text) * 4)
        # 8x the lines: the same linear-growth bound as above, over the rooted shape.
        self.assertLess(rooted_matched[1], rooted_matched[0] * 16)

    def test_cli_root_only_check_refuses_a_root_after_a_json_escape(self):
        # Inside a quoted record a root follows an escape (`\n`, `\t`); the escape is a boundary,
        # else the root bytes pass `--check --root-only`, while the name rejection itself stays.
        posix_root = "/srv/store"
        cases = (
            '{"out":"ok\\n' + posix_root + '/x failed"}\n',
            '{"out":"a\\t' + posix_root + '/x"}\n',
            '{"out":"ok' + _BS + 'u000a' + posix_root + '/x failed"}\n',
        )
        for text in cases:
            with self.subTest(text=text):
                result = run_cli(text, "--check", "--root-only", "--root", posix_root)
                self.assertEqual(result.returncode, 1, text)
                self.assertEqual(result.stdout, "", text)
                self.assertIn("refused", result.stderr)
        # Boundary negative: a literal name character glued in front is still not
        # this root.
        passed = run_cli("aa" + posix_root + "/x\n", "--check", "--root-only", "--root", posix_root)
        self.assertEqual(passed.returncode, 0, passed.stderr)
        self.assertEqual(passed.stdout, "")

    def test_cli_root_only_check_refuses_a_host_path_after_a_json_escape(self):
        # #119: the host-path patterns read the escape's letter as a glued name character, so
        # recorded bytes still naming a host path passed `--check --root-only` unrendered.
        cases = (
            '{"out":"ok' + ESCAPE_N + HOST_RUNNER_WORK + ' failed"}\n',
            '{"out":"ok' + ESCAPE_T + HOST_RUNNER_WORK + '"}\n',
            '{"out":"ok' + ESCAPE_U0020 + HOST_TMP_RUN + '"}\n',
            '{"p":"wrote ' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED"}\n',
            '{"p":"wrote //server/share/run' + _BS + 'nFAILED"}\n',
            '{"p":"wrote file:///C:/Users/op/run' + _BS + 'nFAILED"}\n',
        )
        for text in cases:
            with self.subTest(text=text):
                result = run_cli(text, "--check", "--root-only")
                self.assertEqual(result.returncode, 1, text)
                self.assertEqual(result.stdout, "", text)
                self.assertIn("refused", result.stderr)
        for text in cases:
            with self.subTest(text=text, scope="default"):
                self.assertEqual(run_cli(text, "--check").returncode, 1, text)
            with self.subTest(text=text, scope="rerender"):
                rendered = MODULE.sanitize_text(text, [], root_only=True)
                recheck = run_cli(rendered, "--check", "--root-only")
                self.assertEqual(recheck.returncode, 0, recheck.stderr)
                again = run_cli(rendered, "--root-only")
                self.assertEqual(again.stdout, rendered)
        # Boundary negative: the escape allowance does not weaken the name rejection, so
        # a literal name character glued in front still passes.
        passed = run_cli("aa/home/runner/x\n", "--check", "--root-only")
        self.assertEqual(passed.returncode, 0, passed.stderr)
        self.assertEqual(passed.stdout, "")

    def test_cli_renders_json_records_to_bytes_its_own_check_accepts(self):
        # The publishing round trip: the check the input fails must pass the render, or a
        # publisher that renders before writing publishes bytes its own gate refuses.
        text = (
            '{"out":"wrote ' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED",'
            '"note":"C:' + _BS * 2 + "Users" + _BS * 2 + "O" + _BS + 'u0027Brien"}\n'
        )
        self.assertEqual(run_cli(text, "--check", "--root-only").returncode, 1)
        rendered = run_cli(text, "--root-only")
        self.assertEqual(rendered.returncode, 0, rendered.stderr)
        self.assertEqual(run_cli(rendered.stdout, "--check", "--root-only").returncode, 0)
        self.assertEqual(
            json.loads(rendered.stdout),
            {"out": "wrote " + digest_of(ESCAPED_DRIVE_RUN) + "\nFAILED",
             "note": digest_of("C:" + _BS * 2 + "Users" + _BS * 2 + "O" + _BS + "u0027Brien")},
        )

    def test_cli_root_render_then_check_is_a_fixpoint(self):
        # The documented publish flow (`--root --root-only`, then the gate's `--check --root-only`):
        # the first render writes one token binding the whole location - fail-closed, byte-stable.
        posix_root = "/srv/store"
        drive_root = "C:" + _BS + "Work" + _BS + "run"
        drive_spelling = "C:" + _BS * 2 + "Work" + _BS * 2 + "run"
        cases = (
            (posix_root, posix_root + "//run" + _BS + "nFAILED", ("srv", "store")),
            (drive_root, drive_spelling + "//child" + _BS + "nFAILED", ("Work",)),
        )
        for root, whole, fragments in cases:
            text = '{"out":"' + whole + '"}'
            for scope in (["--root-only"], []):
                with self.subTest(root=root, scope=scope):
                    refused = run_cli(text, "--check", *scope, "--root", root)
                    self.assertEqual(refused.returncode, 1, text)
                    self.assertEqual(refused.stdout, "")
                    self.assertIn("refused", refused.stderr)
                    rendered = run_cli(text, *scope, "--root", root)
                    self.assertEqual(rendered.returncode, 0, rendered.stderr)
                    self.assertEqual(rendered.stdout, '{"out":"' + digest_of(whole) + '"}')
                    for fragment in fragments:
                        self.assertNotIn(fragment, rendered.stdout)
                    # The gate exactly as the cleanliness job runs it: `--check --root-only`,
                    # with and without `--root`.
                    for arguments in (["--check", "--root-only"],
                                      ["--check", "--root-only", "--root", root]):
                        gated = run_cli(rendered.stdout, *arguments)
                        self.assertEqual(gated.returncode, 0, gated.stderr)
                        self.assertEqual(gated.stdout, "")
                    again = run_cli(rendered.stdout, *scope, "--root", root)
                    self.assertEqual(again.returncode, 0, again.stderr)
                    self.assertEqual(again.stdout, rendered.stdout)


if __name__ == "__main__":
    unittest.main()
