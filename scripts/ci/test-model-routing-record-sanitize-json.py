#!/usr/bin/env python3
"""Regression tests for the record sanitizer's JSON reading.

These tests are the JSON half of the sanitizer's regression evidence: a record whose
document parses as JSON is read through its string literals (decoded, matched, and
re-spelled), and this module pins that reading's escapes, offsets, digests, token
opacity and fail-closed property. The raw/prose reading lives in
``test-model-routing-record-sanitize.py``, whose fixtures this module shares.
"""

import importlib.util
import json
import pathlib
import subprocess
import sys
import unittest
import unittest.mock

# The sibling raw-reading module's name carries dashes, so it is loaded by path: the
# shared fixtures (the loaded tool, the disposable root and `digest_of`) live there,
# and the two suites must agree on them.
_SIBLING = pathlib.Path(__file__).with_name("test-model-routing-record-sanitize.py")
_spec = importlib.util.spec_from_file_location("test_model_routing_record_sanitize", _SIBLING)
sibling = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(sibling)
MODULE, ROOT, SCRIPT, digest_of = sibling.MODULE, sibling.ROOT, sibling.SCRIPT, sibling.digest_of

_BS = chr(92)

# The escape spellings the `ok` + escape + host-path shape pins: every two-character
# escape the grammar spells and the `\\uXXXX` spelling of a space, and the host-rooted
# POSIX locations they precede, so one loop crosses every spelling with every root.
ESCAPE_N, ESCAPE_T = _BS + "n", _BS + "t"
ESCAPE_U0020 = _BS + "u0020"
ESCAPE_SPELLINGS = (ESCAPE_N, ESCAPE_T, _BS + "r", _BS + "b", _BS + "f", ESCAPE_U0020)
HOST_RUNNER_WORK, HOST_TMP_RUN = "/home/runner/work/x", "/tmp/symmetry-run"
HOST_ROOT_LOCATIONS = (
    HOST_RUNNER_WORK, "/Users/op/run", "/root/native", HOST_TMP_RUN,
    "/var/folders/ab/T", "/mnt/q/repos/symmetry", "/private/var/tmp/symmetry",
)
# The JSON-escaped drive spelling the Windows half of the shape follows an escape
# with: what a writer's `json.dumps` spells for `C:\Users\op\run`.
ESCAPED_DRIVE_RUN = "C:" + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run"
# The digest the TODO entry records for the path text alone; the entry's own input
# must render exactly it (the pre-fix tools bound the path plus the escape plus
# `FAILED` as `path-sha256:a3a19f2b…`).
ENTRY_PATH_DIGEST = "path-sha256:26e7b5b42607730bbb3c0fdb4d10159a3c52a1c4004d4cb4fc281175bc3f9fb8"


def run_cli(text, *arguments):
    """Run the sanitizer CLI over ``text`` on standard input."""

    return subprocess.run(
        [sys.executable, str(SCRIPT), *arguments],
        input=text, text=True, capture_output=True, check=False,
    )


class JsonReadingTests(unittest.TestCase):
    def test_a_json_escape_after_a_posix_path_survives_the_render(self):
        # STD-A07: the segment classes used to accept a bare backslash, so the render
        # took the JSON `\n` escape and the evidence line behind it into the token
        # (`FAIL: verifier refused` vanished) while the record stayed valid JSON. The
        # decoded reading stops the tail at the escape's newline, so the token binds
        # the location's recorded bytes alone, whichever spelling the writer used.
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
        # The entry's own reproduction: the location is recorded with the JSON-escaped
        # doubled separators and the two-character `\n` escape follows its last segment,
        # with an evidence line behind the escape. The pre-fix tail took the escape and
        # `FAILED` into the token (the token bound path + escape + `FAILED`, the
        # entry's `path-sha256:a3a19f2b…`); the decoded reading binds the location's
        # recorded bytes (`path-sha256:26e7b5b4…`, the digest the entry records for the
        # path alone) and leaves the escape and the line where they were.
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
        # The same shape on the families the entry lists: the location is spelled with
        # its JSON-escaped separators and the escape follows the last segment. Every
        # properly escaped family stops at the decoded newline in both scopes, and the
        # token binds exactly the location's recorded bytes - the UNC leader's escaped
        # run included.
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
        # A forward-slash spelling carries no escaped pair, so the raw look union
        # absorbs its whole span: the newline escape and the word behind it go into
        # the token with the location. That is round 3's accepted over-redaction - the
        # fail-closed direction of the union rule - and the visible opposite of the
        # properly escaped cases above, which keep the escape and `FAILED` outside.
        # The guard pin that used to require the escape outside the token for the
        # slash-UNC shape is this test's absorbed case now.
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
        # A JSON-escaped backslash stays a path byte: the record's `\\\\` decodes to two
        # literal backslashes, so the segment behind the pair is part of the location
        # (`...\\n` is not an escape there) and the whole spelling is one token. The
        # reading is the JSON grammar's, not a shape guess that would cut a location at
        # its own last separator.
        pair = "C:" + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "run" + _BS * 4 + "n"
        for root_only in (True, False):
            with self.subTest(shape="escaped-backslash", root_only=root_only):
                rendered = MODULE.sanitize_text('{"p":"' + pair + '"}', [], root_only=root_only)
                self.assertEqual(rendered, '{"p":"' + digest_of(pair) + '"}')
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        # The accepted boundary, pinned rather than silent: a literal that mixes an
        # escaped separator with an escape-shaped one - a spelling only a producer bug
        # writes, `C:\\a\repo` - is read as JSON, where that backslash is a carriage
        # return and not a separator. The location renders up to the escape, and the
        # escape's own spelling plus the text behind it stay outside the token: no path
        # byte is left in front of the residue, the record is still valid JSON, and
        # rendering stays idempotent. A later narrowing must change this case visibly.
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
        # The decoded reading's byte classes, pinned on the shapes a valid producer
        # writes: a space, NBSP or U+3000 a JSON writer escaped is the path character
        # it decodes to (Python's `json.dumps` default spells a non-ASCII space as a
        # `\\uXXXX` escape), and an apostrophe spelled the same way is part of the
        # segment name (`O'Brien`). A partial render here would republish the tail of
        # the location, so each case asserts the exact one-token render.
        cases = (
            # An escaped plain space between two segments.
            '{"p":"C:' + _BS * 2 + "Program" + _BS + "u0020Files" + _BS * 2 + "op" + _BS * 2 + "secret" + '"}',
            # NBSP and U+3000 as Python's `json.dumps` default writes them.
            '{"p": "C:' + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "my" + _BS + 'u00a0project"}',
            '{"p": "C:' + _BS * 2 + "Users" + _BS * 2 + "op" + _BS * 2 + "my" + _BS + 'u3000project"}',
            # The apostrophe-in-profile-name case.
            '{"p":"C:' + _BS * 2 + "Users" + _BS * 2 + "O" + _BS + "u0027Brien" + _BS * 2 + "secret" + '"}',
            # A literal space and NBSP beside a segment that continues to the line end.
            '{"p": "C:' + _BS * 2 + "Users" + _BS * 2 + "First Last" + _BS * 2 + "my" + _BS + 'u00a0project"}',
        )
        for text in cases:
            with self.subTest(text=text):
                json.loads(text)  # the document parses: the decoded reading owns it
                # The recorded spelling between the literal's quotes; the render
                # replaces exactly it with the token its digest binds.
                spelling = text.split('"', 3)[3][:-2]
                for root_only in (True, False):
                    with self.subTest(text=text, root_only=root_only):
                        rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                        self.assertEqual(rendered, text.replace(spelling, digest_of(spelling)), text)
                        self.assertEqual(
                            MODULE.sanitize_text(rendered, [], root_only=root_only), rendered
                        )

    def test_an_escaped_quote_and_surrogate_pair_keep_the_mapping(self):
        # A decoded quote ends the location and stays outside the token: the drive
        # match stops at it, so the token binds the recorded bytes before the quote.
        raw_path = "C:" + _BS * 2 + "dir" + _BS * 2 + "file"
        text = '{"t": "see ' + _BS + '"' + raw_path + _BS + '", ok", "n": 1}'
        rendered = MODULE.sanitize_text(text, [], root_only=True)
        self.assertEqual(
            json.loads(rendered), {"t": 'see "' + digest_of(raw_path) + '", ok', "n": 1},
        )
        # An even trailing run of recorded backslashes is the value's own backslashes
        # before the string's closing quote, so the whole run stays inside the token.
        tail_path = "C:" + _BS * 2 + "dir" + _BS * 2
        tail_rendered = MODULE.sanitize_text('{"t": "' + tail_path + '"}', [], root_only=True)
        self.assertEqual(json.loads(tail_rendered), {"t": digest_of(tail_path)})
        # An odd run longer than one behaves the same way: the escape pair stays inside
        # the token and the quote's own escape outside it, so the quoted string still
        # decodes and no bare quote is left behind.
        run3 = MODULE.sanitize_text(
            '{"t": "see ' + _BS + '"' + tail_path + _BS + '", ok"}', [], root_only=True,
        )
        self.assertEqual(json.loads(run3), {"t": 'see "' + digest_of(tail_path) + '", ok'})
        # The escaped-quote parity: a literal carrying an odd number of `\"` escapes
        # before the path is still one literal (a scan that let an escaped quote end
        # it would read the path raw and absorb the following escape and line).
        odd = ('{"out":"see ' + _BS + '"' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED"}')
        odd_rendered = MODULE.sanitize_text(odd, [], root_only=True)
        self.assertEqual(
            odd_rendered,
            '{"out":"see ' + _BS + '"' + digest_of(ESCAPED_DRIVE_RUN) + _BS + 'nFAILED"}',
        )
        self.assertEqual(json.loads(odd_rendered)["out"],
                         'see "' + digest_of(ESCAPED_DRIVE_RUN) + "\nFAILED")
        # A surrogate pair stays its two decoded units, each covering its own six
        # recorded bytes: round 3 deleted the pairing step, because the pair's
        # character and either lone unit are characters no byte class refuses and no
        # pattern atom reads, so the pairing changed no match, no mapping and no token
        # (the differential fuzz over generated documents carrying pairs, lone halves
        # and mixed spellings found no output difference). A location that contains
        # the spelling still renders as one token binding the whole recorded spelling.
        pair_path = "C:" + _BS * 2 + "Users" + _BS * 2 + "x" + _BS + "ud83d" + _BS + "ude00" \
            + _BS * 2 + "run"
        pair_rendered = MODULE.sanitize_text('{"p":"' + pair_path + '"}', [], root_only=True)
        self.assertEqual(pair_rendered, '{"p":"' + digest_of(pair_path) + '"}')
        for shape in (rendered, tail_rendered, run3, odd_rendered, pair_rendered):
            self.assertEqual(MODULE.sanitize_text(shape, [], root_only=True), shape)

    def test_the_root_matchers_escape_their_own_recorded_bytes(self):
        # A `--root` whose bytes contain a regex metacharacter (`)`) must match its own
        # recorded spelling and nothing else: `re.escape` keeps `store)` a literal
        # `store` + `)`, and the decoded reading never lets a substituted byte satisfy
        # a root.
        root, text = "/srv/store)", '{"out":"/srv/store)/x' + _BS + 'n"}'
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [root], root_only=root_only)
                self.assertEqual(rendered, '{"out":"<disposable-root>/x' + _BS + 'n"}')
                self.assertEqual(MODULE.sanitize_text(rendered, [root], root_only=root_only), rendered)
        # The same root on a record that does not spell the root: the record's `\n`
        # escape is not a `)` for the root to match. `--root-only` leaves the record
        # alone, and the default scope renders the bare `/srv/store` as the absolute
        # path it is - never through the root.
        masked = '{"out":"/srv/store' + _BS + 'n"}'
        self.assertEqual(MODULE.sanitize_text(masked, [root], root_only=True), masked)
        self.assertEqual(
            MODULE.sanitize_text(masked, [root], root_only=False),
            '{"out":"' + digest_of("/srv/store") + _BS + 'n"}',
        )
        # A drive root with the metacharacter in a middle segment.
        drive_root = "C:/a))b/run"
        drive_text = '{"p":"C:' + _BS * 2 + "a))b" + _BS * 2 + "run" + _BS + 'n"}'
        self.assertEqual(
            MODULE.sanitize_text(drive_text, [drive_root], root_only=True),
            '{"p":"<disposable-root>' + _BS + 'n"}',
        )
        # Rendering rendered text stays a no-op even when the token is spelled right
        # before the root's own bytes.
        once = MODULE.sanitize_text('{"p":"/srv/store/srv/store"}', ["/srv/store"], root_only=True)
        self.assertEqual(once, '{"p":"<disposable-root>/srv/store"}')
        self.assertEqual(
            MODULE.sanitize_text(once, ["/srv/store"], root_only=True), once,
        )
        # A drive root spelled with forward slashes is the decoded reading's own span:
        # it carries no single backslash, so the raw look leaves it alone and the root
        # token keeps its trailing structure.
        self.assertEqual(
            MODULE.sanitize_text('{"p":"C:/a/b"}', ["C:/a"], root_only=True),
            '{"p":"<disposable-root>/b"}',
        )
        # A drive root spelled with single backslashes is a raw pronunciation the decoded
        # reading cannot see, and the drive family renders it - the root's own bytes are
        # removed, as a hashed token binding the raw spelling rather than the root token
        # (the raw look runs the path families, not the decoded root matchers).
        raw_root = "C:" + _BS + "repo"
        self.assertEqual(
            MODULE.sanitize_text('{"p":"' + raw_root + '"}', ["C:/repo"], root_only=True),
            '{"p":"' + digest_of(raw_root) + '"}',
        )

    def test_a_token_never_binds_another_tokens_text(self):
        # A later matcher must not touch bytes an earlier replacement wrote: the pre-fix
        # render took `//r` + the escape spelling up to the just-written token's own
        # `path-sha256` text into a second digest and left the first token as an
        # orphaned `:<hex>` fragment. Every digest here binds recorded bytes alone. The
        # raw look cannot bind a token's own text either, on this render or the next:
        # it renders only the parts of a match no rendered token's text covers, so the
        # `//r\b` slash-UNC prefix beside the claim is one union span with it (round 3's
        # rule: a raw match is never dropped for overlapping a claim) and a re-render
        # changes nothing.
        text = '{"out":"' + _BS + 'u0008//r' + _BS + 'bfile:C:/"}'
        whole = "//r" + _BS + "bfile:C:/"
        expected = '{"out":"' + _BS + 'u0008' + digest_of(whole) + '"}'
        for root_only in (True, False):
            with self.subTest(root_only=root_only):
                rendered = MODULE.sanitize_text(text, [], root_only=root_only)
                self.assertEqual(rendered, expected)
                self.assertEqual(json.loads(rendered)["out"], "\b" + digest_of(whole))
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        # The prose shape of the same defect: the escape arm's added match (`/child`
        # behind a recorded `\n`) used to be swallowed by the root-relative matcher's
        # next pass, which hashed the first token's own text and left an orphan. The
        # overlapping matches now merge into one span, so the whole location renders as
        # one token - the base tool's own render.
        prose = "worktree " + _BS + "Users" + _BS + "zzalpha" + _BS + "zzbeta" + _BS + "n/child"
        whole = _BS + "Users" + _BS + "zzalpha" + _BS + "zzbeta" + _BS + "n/child"
        for root_only in (True, False):
            with self.subTest(shape="prose", root_only=root_only):
                rendered = MODULE.sanitize_text(prose, [], root_only=root_only)
                self.assertEqual(rendered, "worktree " + digest_of(whole))
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)
        # A root spelling behind a rendered `<disposable-root>` is the token's own
        # trailing structure, not a new location.
        token_tail = "//r" + _BS + "u005c/srv/store"
        self.assertEqual(
            MODULE.sanitize_text('{"p":"' + token_tail + '"}', ["/srv/store"], root_only=True),
            '{"p":"' + digest_of(token_tail) + '"}',
        )

    def test_the_reading_is_decided_at_the_document_level(self):
        # A JSONL record decodes each line's literals: the escape stays outside the
        # token on both lines and the whole file renders to bytes its own check
        # accepts.
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
        # One prose line makes the whole document prose: a stray quote's mispairing
        # cannot reach into the JSON line, and the raw reading owns the document - the
        # same render the base tool produced (the escape and the line behind it are
        # absorbed with the location).
        mixed = "the 3\" bolt\n" + '{"output":"wrote ' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED"}'
        absorbed = ESCAPED_DRIVE_RUN + _BS + "nFAILED"
        rendered = MODULE.sanitize_text(mixed, [], root_only=True)
        self.assertEqual(rendered, 'the 3" bolt\n{"output":"wrote ' + digest_of(absorbed) + '"}')
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)
        # A Markdown record carrying a fenced JSON block stays prose in both scopes.
        markdown = "## Run\n\n```json\n{\"p\":\"" + ESCAPED_DRIVE_RUN + "\"}\n```\n"
        self.assertEqual(
            MODULE.sanitize_text(markdown, [], root_only=True),
            '## Run\n\n```json\n{"p":"' + digest_of(ESCAPED_DRIVE_RUN) + '"}\n```\n',
        )

    def test_a_raw_single_backslash_spelling_inside_json_is_rendered(self):
        # The r2 rule, on the pin the replaced D2b carve-out used to carry: the decoded
        # reading stays primary, and the Windows-shaped families run over the literal's
        # recorded bytes as well, because a raw spelling's separators are the JSON
        # escapes `\r`, `\t`, `\n`, `\f` and `\b` - `C:\repo\tmp` decodes to `C:`, a
        # carriage return, `epo`, a tab and `mp`, so the decoded value names no location
        # while the recorded bytes spell one. A span the decoded reading reads no
        # location in is such a raw spelling and renders whole: the pre-r2 bytes left
        # every record here unchanged (fail-open on the recorded bytes).
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
        # A raw spelling followed by an escaped newline: to the decoded reading the
        # escape is a boundary and the line behind it is text, but the decoded reading
        # reads no location at all here, so the raw reading's own match - the location
        # plus the escape plus the line, because a recorded `\n` reads as a separator and
        # a name - renders whole. That is the prose reading's disclosed
        # over-approximation, and the visible opposite of the entry's properly escaped
        # case above, where the decoded reading reads the location and keeps the escape
        # and `FAILED` outside the token.
        sealed = "C:" + _BS + "temp" + _BS + "nFAILED"
        for root_only in (True, False):
            with self.subTest(shape="escape-followed", root_only=root_only):
                rendered = MODULE.sanitize_text('{"p":"' + sealed + '"}', [], root_only=root_only)
                self.assertEqual(rendered, '{"p":"' + digest_of(sealed) + '"}')
                self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=root_only), rendered)

    def test_a_host_path_after_a_json_escape_is_caught(self):
        # The #119 round-5 follow-up: the POSIX families' leading boundary read the
        # escape's own letter as a name character glued in front of the location, so
        # `{"out":"ok\n/home/…"}` passed `--check --root-only` and the render left the
        # host path literal. The decoded escape is a boundary, and the locations behind
        # it render with the escape outside the token - the host-rooted POSIX roots
        # first, then the other families.
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
        # The general POSIX family behind the escape renders in the default scope, and
        # `--root-only` still spares a route it does not recognise.
        cmd = '{"cmd":"go test' + _BS + 'n/etc/hosts"}'
        self.assertEqual(MODULE.sanitize_text(cmd, []),
                         '{"cmd":"go test' + _BS + 'n' + digest_of("/etc/hosts") + '"}')
        self.assertEqual(MODULE.sanitize_text(cmd, [], root_only=True), cmd)
        # The boundary negatives stay: the escape allowance does not weaken the name
        # rejection, so a literal name character glued in front is still not this
        # location.
        for text in ('{"out":"ok' + _BS + 'naa/home/op/run"}',
                     '{"out":"ok' + _BS + 'n../home/op/run"}',
                     '{"out":"ok' + _BS + 'nx_/home/op/run"}',
                     '{"out":"oka/srv/store/x"}'):
            with self.subTest(text=text):
                self.assertEqual(
                    MODULE.sanitize_text(text, ["/srv/store"], root_only=True), text
                )

    def test_a_host_path_in_a_json_key_is_rendered(self):
        # P3 (round 3): the reading is per literal, keys included. The review's
        # mutation N11 - leave a literal untouched when the next non-space character is
        # `:` - kept both suites green, so nothing pinned a key. A key renders like a
        # value, in a whole-document record and in each JSONL line, and the rendered
        # bytes still parse.
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
        # P3 (round 3): the raw look's no-backslash filter had no discriminating case
        # (the review's mutation N13 stayed green). A Windows-shaped raw match whose
        # span carries no backslash at all is bytes the decoded reading reads as
        # written, so its own boundary decisions govern: the raw tail admits the
        # recorded DEL byte while the decoded reading refuses it, so the decoded
        # reading's token stops where it did and the match's span is not unioned. With
        # the filter dropped, the match's uncovered part would merge with the claim and
        # the token would swallow the DEL byte and everything behind it.
        text = '{"p":"C:/x' + chr(0x7F) + 'y) ok' + _BS + 'nFAILED"}'
        rendered = MODULE.sanitize_text(text, [], root_only=True)
        self.assertEqual(
            rendered, '{"p":"' + digest_of("C:/x") + chr(0x7F) + 'y) ok' + _BS + 'nFAILED"}',
        )
        self.assertEqual(MODULE.sanitize_text(rendered, [], root_only=True), rendered)

    def test_the_fail_closed_property_holds_for_every_repro_shape(self):
        # The invariant, as a property over the shapes both reviews reproduced: after a
        # render no decoded string carries a host-path segment from the input, the
        # rendered bytes pass the same root-only check the input fails, and rendering
        # is a fixpoint. The corpus: the four truncated-render shapes (an escaped
        # space, NBSP, U+3000 and an escaped apostrophe), the space fail-open shapes,
        # the token-prefix shape, the stray-quote document, the root-metacharacter
        # shapes, the entry's own input, the round-5 POSIX path after an escape, a
        # surrogate pair, a doubled escaped backslash, the r2 raw Windows spellings
        # (drive, both UNC dialects, root-relative and `file:` URL), and round 3's
        # union shapes: a forward-slash prefix continued by raw separators (whole
        # document and JSONL), an escaped-pair prefix continued by a raw segment, an
        # odd-run raw suffix, a key-carrying path, and the forward-slash spelling
        # whose escape and evidence line the union absorbs (the accepted
        # over-redaction).
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
            # The r2 raw look: a Windows spelling whose separators are the escapes `\r`,
            # `\t`, `\n`, `\f` and `\b` decodes into control characters, so the decoded
            # value names no location and only the raw reading sees the one the recorded
            # bytes spell. Each span renders whole - the escape and the evidence line
            # behind a raw spelling included, where the raw tail cannot tell them apart.
            ("raw-repo-tmp", '{"p":"C:' + _BS + "repo" + _BS + 'tmp"}', [], ("repo", "tmp"), True),
            ("raw-temp-run", '{"p":"C:' + _BS + "temp" + _BS + 'run"}', [], ("temp", "run"), True),
            ("raw-name-foo", '{"p":"C:' + _BS + "name" + _BS + 'foo"}', [], ("name", "foo"), True),
            ("raw-foo-bar", '{"p":"C:' + _BS + "foo" + _BS + 'bar"}', [], ("foo", "bar"), True),
            ("raw-bar-run", '{"p":"C:' + _BS + "bar" + _BS + 'run"}', [], ("bar", "run"), True),
            ("raw-escape-followed", '{"p":"C:' + _BS + "temp" + _BS + 'nFAILED"}',
             [], ("temp", "FAILED"), True),
            # A raw UNC spelling: no single backslash spells a UNC leader in a valid
            # literal, so its leader is a pair and its separator raw - the decoded value
            # is `\server`, a tab and `mp`, and the recorded bytes are the location's
            # only spelling. Both dialects render, the escape and the line included.
            ("unc-raw-separator",
             '{"p":"' + _BS * 2 + "server" + _BS * 2 + "share" + _BS + 'nFAILED"}',
             [], ("server", "share", "FAILED"), True),
            ("slash-unc-raw-separator", '{"p":"//server' + _BS + 'tmp"}',
             [], ("server", "tmp"), True),
            # The forward-slash spelling that used to be the guard pin: its raw match
            # carries no escaped pair, so round 3's union absorbs its whole span - the
            # escape and `FAILED` go into the token with the location, which is the
            # accepted over-redaction (the properly escaped spellings keep the escape
            # outside; see `test_a_json_escape_after_every_windows_family_survives_the_render`).
            ("slash-unc-escape-absorbed", '{"p":"//server/share/run' + _BS + 'nFAILED"}',
             [], ("server", "share", "run", "FAILED"), True),
            # Round 3's union shapes, and the leak the review's table reproduced: a
            # forward-slash prefix the decoded reading reads alone renders whole once
            # the raw match is unioned instead of dropped, so the segment names behind
            # the escape never stay literal.
            ("slash-prefix-raw-suffix", '{"p":"C:/Users' + _BS + 'nop' + _BS + 'repo"}',
             [], ("Users", "op", "repo"), True),
            ("slash-prefix-raw-suffix-jsonl",
             '{"p":"C:/Users' + _BS + 'nop' + _BS + 'repo"}\n'
             '{"p":"C:/Users' + _BS + 'nop' + _BS + 'repo2"}\n',
             [], ("Users", "op", "repo"), False),
            # The escaped-pair prefix continued by raw segments: the union renders the
            # parts past the claim's own spelling whole, so the segments render too.
            ("escaped-prefix-raw-suffix",
             '{"p":"C:' + _BS * 2 + "Users" + _BS + 'nop' + _BS + 'repo"}',
             [], ("Users", "op", "repo"), True),
            # An odd backslash run's last backslash is the escape `_RAW_SPELLING_ESCAPE`
            # fires on (the guard used to see only runs of one), so the literal is
            # scanned and the whole spelling renders.
            ("odd-run-raw-suffix", '{"p":"C:' + _BS * 3 + "temp" + _BS * 2 + 'op"}',
             [], ("temp", "op"), True),
            # A host path in a JSON key renders like one in a value (the review's N11
            # mutation - skipping a literal whose next non-space character is `:` -
            # stayed green without this shape).
            ("key-carrying", '{"' + ESCAPED_DRIVE_RUN + '":1}',
             [], ("Users", "run"), True),
            # A match starting right after an odd backslash run: without the span
            # snapping the token would land behind a lone backslash and the literal
            # would spell `\p` - no valid escape - so the render stopped parsing as
            # JSON. The span absorbs the prefix and binds the recorded spelling whole.
            ("split-run-boundary", '{"p":"' + _BS + 'file:///C:/x' + _BS * 2 + '"}',
             [], ("file:", "C:/"), True),
            # The remaining Windows-shaped families whose separators the decoded reading
            # can mis-read the same way: a raw root-relative spelling and a raw `file:`
            # URL.
            ("raw-root-relative", '{"p":"' + _BS + "root" + _BS + 'run"}',
             [], ("root", "run"), True),
            ("raw-file-url", '{"p":"file:' + _BS + "temp" + _BS + 'run"}',
             [], ("temp", "run"), True),
            # A mixed literal: one properly escaped location (the decoded reading reads
            # it and renders its recorded bytes) beside one raw spelling the raw look
            # renders - one token each, no fragment of either.
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
        # P2 (round 3): every match rebuilt the whole text and its mapping, so a
        # literal with thousands of paths was quadratic (2,000 paths: 4.3 s; 8,000:
        # 115 s; a 8,000-line prose record: 41.7 s). One pass is now one rebuild, and
        # `_rebuild_with_groups` is that one rebuild: the pin counts its calls, so the
        # count must not grow with the number of matches. The rendered bytes are
        # checked too, so a rebuild that skipped work would fail the count's premise.
        # The pass machinery is shared, so the prose shape is pinned beside the
        # literal: prose renders through the same rebuilds, one pass over the
        # recorded text.
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

    def test_cli_root_only_check_refuses_a_root_after_a_json_escape(self):
        # Opus P2 (round 4): inside a quoted record, a line's root follows an
        # escape sequence (`\n`, `\t`, ...), and the leading name lookbehind read
        # the escape's letter as a name character glued to the root, so the root
        # bytes passed `--check --root-only`. The decoded escape is a boundary; the
        # name-character rejection itself stays.
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
        # The #119 round-5 follow-up reproduction: the host-path patterns read the
        # escape's letter as a name character glued in front of the location, so the
        # recorded bytes still naming a host path passed `--check --root-only`. The
        # decoded escape is a boundary, and a location behind it refuses the input;
        # the shape also renders to bytes the check accepts, so a re-rendered record
        # is clean.
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
        # The default scope refuses the same bytes, and every shape's render passes the
        # root-only check it just refused, with the rendered bytes left alone by the
        # render that produced them.
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
        # The publishing round trip: render a JSON record through the CLI, then run the
        # root-only check over the rendered bytes. The check the input fails must pass
        # the output, or a publisher that renders before writing would publish bytes
        # its own gate refuses.
        text = (
            '{"out":"wrote ' + ESCAPED_DRIVE_RUN + _BS + 'nFAILED",'
            '"note":"C:' + _BS * 2 + "Users" + _BS * 2 + "O" + _BS + 'u0027Brien"}\n'
        )
        self.assertEqual(run_cli(text, "--check", "--root-only").returncode, 1)
        rendered = run_cli(text, "--root-only")
        self.assertEqual(rendered.returncode, 0, rendered.stderr)
        self.assertEqual(run_cli(rendered.stdout, "--check", "--root-only").returncode, 0)
        # The rendered record is still a JSON document carrying no host path.
        self.assertEqual(
            json.loads(rendered.stdout),
            {"out": "wrote " + digest_of(ESCAPED_DRIVE_RUN) + "\nFAILED",
             "note": digest_of("C:" + _BS * 2 + "Users" + _BS * 2 + "O" + _BS + "u0027Brien")},
        )


if __name__ == "__main__":
    unittest.main()
