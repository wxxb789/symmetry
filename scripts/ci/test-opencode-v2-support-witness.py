#!/usr/bin/env python3
"""Keep failed native executions, no-op interrupts and unapproved tool continuations out of success evidence."""

import copy
import hashlib
import http.client
import json
import os
import pathlib
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

import citest
from testsupport.gitfixture import FixtureRepo

ROOT = pathlib.Path(__file__).resolve().parents[2]
witness = citest.load_gate(pathlib.Path(__file__).with_name("opencode-v2-support-witness.py"), "witness")


class ClassificationTests(unittest.TestCase):
    @staticmethod
    def receipt(terminal_type):
        terminal = {
            "id": "evt_terminal",
            "type": terminal_type,
            "data": {"sessionID": "ses_native", "error": {"type": "provider.auth", "status": 401}},
        }
        return {
            "operations": [],
            "events": [terminal],
            "classes": {
                "success": {"status": "observed", "terminal": terminal},
                "interruption": {
                    "status": "observed",
                    "first_token": None,
                    "response": {"interrupted": False},
                    "terminal": terminal,
                },
            },
        }

    def test_native_provider_auth_failure_is_not_success(self):
        receipt = witness.classify(self.receipt("session.execution.failed"))
        self.assertEqual(receipt["classes"]["success"]["status"], "run and failed")
        self.assertEqual(receipt["classes"]["provider_failure"]["terminals"], receipt["events"])
        self.assertEqual(receipt["classes"]["interruption"]["status"], "run and failed")
        self.assertEqual(receipt["classes"]["usage"]["status"], "unavailable")

    def test_readiness_failure_without_requests_preserves_unavailable_blocker(self):
        receipt = {
            "failure": "RuntimeError",
            "operations": [],
            "events": [],
            "classes": {
                name: {"status": "unavailable", "blocker": "RuntimeError"} for name in ("success", "interruption")
            },
        }
        classified = witness.classify(receipt)
        for name in ("success", "interruption"):
            with self.subTest(name=name):
                self.assertEqual(classified["classes"][name], {"status": "unavailable", "blocker": "RuntimeError"})

    def test_requests_without_terminals_are_failed_observations(self):
        for name, route in (("success", "prompt"), ("interruption", "interrupt")):
            with self.subTest(name=name):
                receipt = {
                    "operations": [{"method": "POST", "path": "/api/session/ses_native/" + route}],
                    "events": [],
                    "classes": {},
                }
                self.assertEqual(witness.classify(receipt)["classes"][name]["status"], "run and failed")

    def test_step_idle_and_missing_terminal_do_not_establish_success(self):
        for kind in ("session.step.ended", "session.idle", None):
            with self.subTest(kind=kind):
                receipt = self.receipt(kind)
                if kind is None:
                    receipt["classes"]["success"]["terminal"] = None
                self.assertNotEqual(witness.classify(receipt)["classes"]["success"]["status"], "observed")

    def test_interruption_requires_active_provider_and_native_terminal(self):
        receipt = self.receipt("session.execution.interrupted")
        interruption = receipt["classes"]["interruption"]
        interruption.update(first_token={"type": "session.text.delta"}, response={"interrupted": True})
        self.assertEqual(witness.classify(copy.deepcopy(receipt))["classes"]["interruption"]["status"], "observed")
        interruption["response"]["interrupted"] = False
        self.assertEqual(witness.classify(receipt)["classes"]["interruption"]["status"], "run and failed")

    @staticmethod
    def continuation_event(kind, **data):
        return {"type": kind, "data": {"sessionID": "ses_native", **data}}

    @classmethod
    def continuation_receipt(cls, events):
        terminal = {"id": "evt_terminal", "type": "session.execution.succeeded", "data": {"sessionID": "ses_native"}}
        return {
            "operations": [],
            "events": [*events, terminal],
            "classes": {
                "continuation_after_step": {
                    "event_start": 0,
                    "request_id": "per_native",
                    "terminal": terminal,
                    "reply": {"status": 204},
                }
            },
        }

    def continuation_steps(self):
        return [
            self.continuation_event("permission.replied", requestID="per_native", reply="once"),
            self.continuation_event("session.tool.success"),
            self.continuation_event("session.step.ended"),
            self.continuation_event("session.step.started"),
        ]

    def test_successful_execution_without_tool_continuation_is_not_proof(self):
        receipt = self.continuation_receipt([])
        self.assertEqual(witness.classify(receipt)["classes"]["continuation_after_step"]["status"], "run and failed")

    def test_continuation_must_precede_the_selected_terminal(self):
        steps = self.continuation_steps()
        receipt = self.continuation_receipt(steps)
        self.assertEqual(
            witness.classify(copy.deepcopy(receipt))["classes"]["continuation_after_step"]["status"], "observed"
        )
        reordered = copy.deepcopy(receipt)
        reordered["events"] = [*steps[:3], reordered["events"][-1], steps[3]]
        self.assertEqual(
            witness.classify(reordered)["classes"]["continuation_after_step"]["status"], "run and failed"
        )

    def test_continuation_requires_a_recorded_accepted_reply(self):
        receipt = self.continuation_receipt(self.continuation_steps())
        unrecorded = copy.deepcopy(receipt)
        del unrecorded["classes"]["continuation_after_step"]["reply"]
        rejected = copy.deepcopy(receipt)
        rejected["classes"]["continuation_after_step"]["reply"]["status"] = 403
        self.assertEqual(witness.classify(unrecorded)["classes"]["continuation_after_step"]["status"], "run and failed")
        self.assertEqual(witness.classify(rejected)["classes"]["continuation_after_step"]["status"], "run and failed")
        self.assertEqual(
            witness.classify(copy.deepcopy(receipt))["classes"]["continuation_after_step"]["status"], "observed"
        )

    def test_continuation_requires_the_ordered_permission_reply_event(self):
        steps = self.continuation_steps()
        receipt = self.continuation_receipt(steps)
        self.assertEqual(
            witness.classify(copy.deepcopy(receipt))["classes"]["continuation_after_step"]["status"], "observed"
        )
        variants = {
            "deleted": [event for event in steps if event["type"] != "permission.replied"],
            "other_request": [
                self.continuation_event("permission.replied", requestID="per_other", reply="once"),
                *steps[1:],
            ],
            "not_once": [
                self.continuation_event("permission.replied", requestID="per_native", reply="always"),
                *steps[1:],
            ],
            "after_tool_success": [steps[1], steps[0], *steps[2:]],
        }
        for name, events in variants.items():
            with self.subTest(name=name):
                self.assertEqual(
                    witness.classify(self.continuation_receipt(events))["classes"]["continuation_after_step"]["status"],
                    "run and failed",
                )

    def test_cleanup_requires_tree_and_reader_barriers(self):
        receipt = self.receipt("session.execution.failed")
        cleanup = {"exit_code": 0, "server_exit_code": 1, "stream_reader_stopped": True, "output_readers_stopped": True}
        receipt["classes"]["cleanup"] = cleanup
        self.assertEqual(witness.classify(copy.deepcopy(receipt))["classes"]["cleanup"]["status"], "observed")
        for name, value in (
            ("exit_code", 1),
            ("stream_reader_stopped", False),
            ("output_readers_stopped", False),
            ("server_exit_code", None),
        ):
            with self.subTest(name=name):
                failed = copy.deepcopy(receipt)
                failed["classes"]["cleanup"][name] = value
                self.assertEqual(witness.classify(failed)["classes"]["cleanup"]["status"], "run and failed")

    def test_a_recorded_failure_survives_the_cleanup_grade(self):
        recorded = {"classes": {"cleanup": {"status": "run and failed"}}, "failure": "RuntimeError"}
        self.assertEqual(witness.cleanup_failure(recorded)["failure"], "RuntimeError")
        unnamed = witness.cleanup_failure({"classes": {"cleanup": {"status": "run and failed"}}})
        self.assertEqual(unnamed["failure"], "CleanupIncomplete")
        self.assertNotIn("failure", witness.cleanup_failure({"classes": {"cleanup": {"status": "observed"}}}))

    @unittest.skipUnless(os.name == "nt", "the Windows tree-kill arm")
    def test_tree_kill_timeout_still_closes_real_process_socket_and_readers(self):
        child = subprocess.Popen(
            [sys.executable, "-c", "import sys; sys.stdin.buffer.read()"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env={name: value for name, value in os.environ.items() if name.upper() in {"SYSTEMROOT", "WINDIR"}},
        )
        local, peer = socket.socketpair()
        connection = http.client.HTTPConnection("127.0.0.1")
        connection.sock = local
        stream_reader = threading.Thread(target=lambda: peer.recv(1))
        output_readers = [threading.Thread(target=stream.read) for stream in (child.stdout, child.stderr)]
        stream_reader.start()
        for reader in output_readers:
            reader.start()

        def stalled_tree_kill(*args, **kwargs):
            child.stdin.close()
            raise subprocess.TimeoutExpired("taskkill", 15)

        try:
            with mock.patch.object(witness.subprocess, "run", side_effect=stalled_tree_kill):
                cleanup = witness.close_native(child, connection, stream_reader, output_readers)
            self.assertEqual(cleanup["tree_kill_error"], "TimeoutExpired")
            self.assertIsNone(cleanup["exit_code"])
            self.assertEqual(child.poll(), 0)
            self.assertTrue(cleanup["stream_reader_stopped"])
            self.assertTrue(cleanup["output_readers_stopped"])
            receipt = self.receipt("session.execution.failed")
            receipt["classes"]["cleanup"] = cleanup
            self.assertEqual(witness.classify(receipt)["classes"]["cleanup"]["status"], "run and failed")
        finally:
            connection.close()
            peer.close()
            if child.poll() is None:
                child.kill()
            child.wait(timeout=5)
            for reader in [stream_reader, *output_readers]:
                reader.join(timeout=5)
            child.stdout.close()
            child.stderr.close()


@unittest.skipUnless(os.name == "posix", "the POSIX signal arm")
class PosixCleanupTests(unittest.TestCase):
    """The POSIX arm signals the process group and still reaches every barrier."""

    def child(self, code, **kwargs):
        child = subprocess.Popen(
            [sys.executable, "-c", code],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env={name: value for name, value in os.environ.items() if name.upper() in {"PATH", "LANG"}},
            **kwargs,
        )
        self.addCleanup(self.reap, child)
        return child

    def reap(self, child):
        if child.poll() is None:
            child.kill()
        child.wait(timeout=5)
        child.stdout.close()
        child.stderr.close()

    def classify_row(self, cleanup):
        receipt = ClassificationTests.receipt("session.execution.failed")
        receipt["classes"]["cleanup"] = cleanup
        return receipt

    def test_posix_cleanup_terminates_the_group_without_escalation(self):
        child = self.child("import time\ntime.sleep(60)\n", start_new_session=True)
        cleanup = witness.close_native(child, None, None, [])
        self.assertEqual(cleanup["exit_code"], 0)
        self.assertNotIn("escalated", cleanup)
        self.assertEqual(cleanup["server_exit_code"], -15)
        self.assertTrue(cleanup["output_readers_stopped"])
        self.assertEqual(witness.classify(self.classify_row(cleanup))["classes"]["cleanup"]["status"], "observed")

    def test_posix_cleanup_treats_a_poll_reaped_child_as_group_gone(self):
        child = self.child("pass", start_new_session=True)
        deadline = time.monotonic() + 5
        while child.poll() is None and time.monotonic() < deadline:
            time.sleep(0.01)
        self.assertEqual(child.poll(), 0)
        cleanup = witness.close_native(child, None, None, [])
        self.assertEqual(cleanup["exit_code"], 0)
        self.assertTrue(cleanup["group_gone"])
        self.assertNotIn("signal_error", cleanup)
        self.assertEqual(cleanup["server_exit_code"], 0)
        self.assertEqual(witness.classify(self.classify_row(cleanup))["classes"]["cleanup"]["status"], "observed")

    def test_posix_cleanup_escalates_once_when_sigterm_is_ignored(self):
        child = self.child(
            "import signal,time\nsignal.signal(signal.SIGTERM, signal.SIG_IGN)\nprint('armed', flush=True)\ntime.sleep(60)\n",
            start_new_session=True,
        )
        self.assertEqual(child.stdout.readline(), b"armed\n")
        with (
            mock.patch.object(witness, "POSIX_SIGTERM_SECONDS", 1),
            mock.patch.object(witness, "POSIX_SIGKILL_SECONDS", 1),
        ):
            cleanup = witness.close_native(child, None, None, [])
        self.assertEqual(cleanup["exit_code"], 0)
        self.assertTrue(cleanup.get("escalated"))
        self.assertIn("SIGKILL", cleanup["command"])
        self.assertEqual(cleanup["server_exit_code"], -9)
        self.assertEqual(witness.classify(self.classify_row(cleanup))["classes"]["cleanup"]["status"], "observed")

    def test_posix_cleanup_reports_a_live_reader_as_not_stopped(self):
        child = self.child("import time\ntime.sleep(60)\n", start_new_session=True)
        reader = threading.Thread(target=lambda: time.sleep(30), daemon=True)
        reader.start()
        cleanup = witness.close_native(child, None, None, [reader])
        self.assertFalse(cleanup["output_readers_stopped"])
        self.assertEqual(witness.classify(self.classify_row(cleanup))["classes"]["cleanup"]["status"], "run and failed")

    def test_posix_cleanup_joins_a_reader_blocked_on_the_stream(self):
        child = self.child("import time\ntime.sleep(60)\n", start_new_session=True)
        local, peer = socket.socketpair()
        self.addCleanup(peer.close)
        connection = http.client.HTTPConnection("127.0.0.1")
        connection.sock = local
        # The reader lingers past the shutdown, so only the join can settle its barrier.
        stream_reader = threading.Thread(target=lambda: (peer.recv(1), time.sleep(2)))
        stream_reader.start()
        self.addCleanup(stream_reader.join, 5)
        self.addCleanup(connection.close)
        cleanup = witness.close_native(child, connection, stream_reader, [])
        self.assertTrue(cleanup["stream_reader_stopped"])
        self.assertTrue(cleanup["output_readers_stopped"])
        self.assertEqual(witness.classify(self.classify_row(cleanup))["classes"]["cleanup"]["status"], "observed")


class ChunkedSSETests(unittest.TestCase):
    def capture(self, chunks):
        local, peer = socket.socketpair()
        response = http.client.HTTPResponse(local)
        try:
            local.settimeout(5)
            peer.sendall(
                b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n"
                + b"".join(f"{len(chunk):x}\r\n".encode() + chunk + b"\r\n" for chunk in chunks)
                + b"0\r\n\r\n"
            )
            peer.shutdown(socket.SHUT_WR)
            response.begin()
            receipt = {"events": []}
            witness.capture_events(response, "test-credential-not-in-events", receipt, threading.Condition())
            self.assertNotIn("stream_end", receipt)
            return receipt["events"]
        finally:
            response.close()
            local.close()
            peer.close()

    def test_event_split_inside_json_across_http_chunks(self):
        event = {"type": "session.execution.succeeded", "data": {"sessionID": "ses_native"}}
        body = b"data: " + json.dumps(event).encode() + b"\n\n"
        split = body.index(b"succeeded") + 3
        self.assertEqual(self.capture([body[:split], body[split:]]), [event])

    def test_http_chunk_boundary_immediately_before_data(self):
        event = {"type": "session.step.first-token", "data": {"sessionID": "ses_native"}}
        body = b"data: " + json.dumps(event).encode() + b"\n\n"
        self.assertEqual(self.capture([b": keepalive\n\n", body]), [event])


class ReadinessTests(unittest.TestCase):
    """The readiness route must report this server's own pid and the pinned version exactly."""

    def test_exact_pid_and_version_is_the_only_accepted_identity(self):
        self.assertTrue(witness.readiness_identity({"pid": 42, "version": "2.4.1"}, 42))
        for info, pid in (
            ({"pid": 42, "version": "2.4"}, 42),
            ({"pid": 42, "version": "2.4.10"}, 42),
            ({"pid": 42, "version": "2.4.1 "}, 42),
            ({"pid": 41, "version": "2.4.1"}, 42),
            ({"version": "2.4.1"}, 42),
            (None, 42),
        ):
            with self.subTest(info=info, pid=pid):
                self.assertFalse(witness.readiness_identity(info, pid))


class InfoRecordTests(unittest.TestCase):
    """The recorded /api/info body keeps identity keys only; host metadata is never recorded."""

    def test_the_recorded_info_body_drops_host_metadata(self):
        raw = json.dumps(
            {
                "version": "2.4.1",
                "pid": 4242,
                "hostname": "fixture-host-name",
                "urls": ["http://127.0.0.1:51844"],
                "paths": {"tmp": "path-sha256:" + "0" * 64},
            }
        )
        recorded = witness.recorded_info_body("/api/info", 200, raw)
        self.assertEqual(json.loads(recorded), {"pid": 4242, "version": "2.4.1", "status": 200})
        self.assertNotIn("hostname", recorded)
        self.assertNotIn("fixture-host-name", recorded)
        refusal = witness.recorded_info_body("/api/info", 401, '{"_tag":"UnauthorizedError","message":"Authentication required"}')
        self.assertEqual(json.loads(refusal), {"pid": None, "version": None, "status": 401})
        self.assertEqual(witness.recorded_info_body("/api/health", 404, raw), raw)


class RedactionTests(unittest.TestCase):
    """The credential and the relay origin never reach a record, whatever the carrier."""

    def test_relay_base_url_and_host_are_replaced_with_counts(self):
        replacements = witness.relay_replacements("https://relay.example.invalid/v1")
        text = "base https://relay.example.invalid/v1 host relay.example.invalid twice relay.example.invalid"
        redacted, counts = witness.redact(text, replacements)
        self.assertNotIn("relay.example.invalid", redacted)
        self.assertEqual(counts[witness.RELAY_BASE_TOKEN], 1)
        self.assertEqual(counts[witness.RELAY_HOST_TOKEN], 2)

    def test_credential_is_replaced_inside_a_route_body(self):
        body = json.dumps({"data": {"apiKey": "fixture-credential-value"}, "location": {"directory": "/run"}})
        redacted, counts = witness.redact(body, {"fixture-credential-value": witness.CREDENTIAL_TOKEN})
        self.assertNotIn("fixture-credential-value", redacted)
        self.assertIn(witness.CREDENTIAL_TOKEN, redacted)
        self.assertEqual(counts[witness.CREDENTIAL_TOKEN], 1)

    def test_an_unset_credential_matches_nothing(self):
        self.assertFalse(witness.carries_credential(b'{"text": "reply with PROBE"}', ""))
        self.assertFalse(witness.carries_credential('{"text": "reply with PROBE"}', ""))
        self.assertTrue(witness.carries_credential(b"carrier fixture-secret", "fixture-secret"))
        self.assertFalse(witness.carries_credential(b"carrier fixture-secret", "fixture-other"))

    def test_the_written_receipt_applies_the_replacements_and_refuses_a_secret(self):
        replacements = witness.relay_replacements("https://relay.example.invalid/v1")
        receipt = {"logs": {"stdout": "relay relay.example.invalid twice relay.example.invalid\n"}}
        data = witness.receipt_bytes(receipt, replacements, ("",))
        self.assertNotIn(b"relay.example.invalid", data)
        self.assertEqual(data.count(witness.RELAY_HOST_TOKEN.encode()), 2)
        with self.assertRaisesRegex(RuntimeError, "credential locality evidence refused"):
            witness.receipt_bytes({"logs": {"stdout": "fixture-credential-value\n"}}, replacements, ("fixture-credential-value",))
        with self.assertRaisesRegex(RuntimeError, "credential locality evidence refused"):
            witness.receipt_bytes(
                {"logs": {"stdout": "https://relay.example.invalid/v1\n"}},
                replacements,
                ("https://relay.example.invalid/v1",),
            )

    def test_every_write_refusal_carrier_is_refused(self):
        secrets = ("fixture-credential-value", "fixture-password-value", "Basic Zm...", "https://relay.example.invalid/v1")
        for carrier in secrets:
            with self.subTest(carrier=carrier):
                with self.assertRaisesRegex(RuntimeError, "credential locality evidence refused"):
                    witness.receipt_bytes({"logs": {"stdout": carrier}}, {}, secrets)

    def test_a_quote_or_non_ascii_secret_is_refused_and_replaced_in_its_escaped_spelling(self):
        for secret in ('fixture-"quoted"-\\secret', "fixture-\u00e9\u20ac-secret"):
            with self.subTest(secret=secret):
                with self.assertRaisesRegex(RuntimeError, "credential locality evidence refused"):
                    witness.receipt_bytes({"logs": {"stdout": secret}}, {secret: witness.CREDENTIAL_TOKEN}, (secret,))
                text, counts = witness.redact(json.dumps({"log": secret}), {secret: witness.CREDENTIAL_TOKEN})
                self.assertEqual(counts[witness.CREDENTIAL_TOKEN], 1)
                self.assertEqual(json.loads(text), {"log": witness.CREDENTIAL_TOKEN})
                self.assertTrue(witness.carries_credential(json.dumps({"apiKey": secret}), secret))


class WriteReceiptTests(unittest.TestCase):
    """The receipt's only write path: the run-root scan, the refusal and the replaced bytes."""

    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = pathlib.Path(directory.name)
        self.out = self.root / "receipt.json"
        self.credential = "fixture-credential-value"
        self.replacements = witness.relay_replacements("https://relay.example.invalid/v1")
        self.secrets = (self.credential, "fixture-password-value", "Basic Zm...", "https://relay.example.invalid/v1")

    def write(self, receipt):
        return witness.write_receipt(self.out, receipt, self.replacements, self.secrets, self.credential, self.root)

    def test_the_write_tail_refuses_a_credential_bearing_receipt(self):
        with self.assertRaisesRegex(RuntimeError, "credential locality evidence refused"):
            self.write({"logs": {"stdout": "carrier " + self.credential}})
        self.assertFalse(self.out.exists())

    def test_the_write_tail_refuses_a_relay_origin_bearing_receipt(self):
        with self.assertRaisesRegex(RuntimeError, "credential locality evidence refused"):
            self.write({"logs": {"stdout": "origin https://relay.example.invalid/v1"}})
        self.assertFalse(self.out.exists())

    def test_the_write_tail_refuses_a_run_root_scan_match(self):
        (self.root / "stray.txt").write_text("carrier " + self.credential)
        with self.assertRaisesRegex(RuntimeError, "credential locality evidence refused"):
            self.write({"logs": {"stdout": "clean\n"}})
        self.assertFalse(self.out.exists())

    def test_the_write_tail_writes_the_replaced_bytes(self):
        receipt = {"logs": {"stdout": "twice relay.example.invalid relay.example.invalid\n"}}
        self.assertIs(self.write(receipt), receipt)
        data = self.out.read_bytes()
        self.assertNotIn(b"relay.example.invalid", data)
        written = json.loads(self.out.read_text())
        self.assertEqual(written["logs"]["stdout"].count(witness.RELAY_HOST_TOKEN), 2)
        self.assertEqual(written["redaction"]["occurrences"], {witness.RELAY_HOST_TOKEN: 2})
        self.assertEqual(written["credential_scan"], {"files_scanned": 0, "matches": []})


class SchemaTests(unittest.TestCase):
    """The truncation attempt is decided from the live prompt schema, never guessed."""

    def operation(self, properties, required=("text",)):
        return {
            "paths": {
                "/api/session/{sessionID}/prompt": {
                    "post": {
                        "requestBody": {
                            "content": {
                                "application/json": {
                                    "schema": {"properties": {name: {"type": "string"} for name in properties}, "required": list(required)}
                                }
                            }
                        }
                    }
                }
            }
        }

    def test_a_bounding_property_is_found_whatever_its_spelling(self):
        self.assertEqual(witness.bounding_field(["text", "maxOutputTokens", "id"]), "maxOutputTokens")
        self.assertEqual(witness.bounding_field(["text", "max_tokens"]), "max_tokens")
        self.assertIsNone(witness.bounding_field(["id", "text", "delivery", "resume"]))

    def test_resolve_prompt_schema_reads_the_live_shape(self):
        bound = witness.resolve_prompt_schema(self.operation(["id", "text", "maxOutputTokens"]))
        self.assertEqual(bound["properties"], ["id", "maxOutputTokens", "text"])
        self.assertEqual(bound["required"], ["text"])
        self.assertEqual(bound["bounding_field"], "maxOutputTokens")
        unbound = witness.resolve_prompt_schema(self.operation(["id", "text", "delivery", "resume"]))
        self.assertEqual(unbound["properties"], ["delivery", "id", "resume", "text"])
        self.assertIsNone(unbound["bounding_field"])

    def test_resolve_prompt_schema_survives_an_absent_operation(self):
        resolved = witness.resolve_prompt_schema({})
        self.assertEqual(resolved, {"properties": [], "required": [], "bounding_field": None})

    def test_a_truncated_response_needs_a_native_bound(self):
        self.assertTrue(witness.truncation_marker([{"data": {"metadata": {"truncated": True}}}]))
        self.assertTrue(witness.truncation_marker([{"data": {"finish_reason": "length"}}]))
        self.assertFalse(witness.truncation_marker([{"data": {"metadata": {"truncated": False}}}]))
        self.assertFalse(witness.truncation_marker([{"type": "session.execution.succeeded", "data": {"sessionID": "s"}}]))
        self.assertFalse(witness.truncation_marker([]))


class ChildEnvironmentTests(unittest.TestCase):
    """The child environment keeps its shape per platform and drops everything else."""

    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = pathlib.Path(directory.name)

    def test_windows_shape_keeps_the_allowlist_and_redirects_private_roots(self):
        environment = witness.child_environment(
            self.root,
            "nt",
            {
                "PATH": "C:\\bin",
                "SystemRoot": "C:\\Windows",
                "WINDIR": "C:\\W",
                "COMSPEC": "C:\\cmd.exe",
                "PATHEXT": ".EXE",
                "SYSTEMDRIVE": "C:",
                "TENCENT_GLM_API_KEY": "fixture-credential-value",
                "SESSIONNAME": "Console",
                "HOME": "C:\\Users\\operator",
                "USERPROFILE": "C:\\Users\\operator",
                "APPDATA": "C:\\Users\\operator\\AppData",
                "LOCALAPPDATA": "C:\\Users\\operator\\Local",
                "TMP": "C:\\Temp",
                "TEMP": "C:\\Temp",
                "XDG_CONFIG_HOME": "C:\\xdg\\config",
                "XDG_CACHE_HOME": "C:\\xdg\\cache",
                "XDG_DATA_HOME": "C:\\xdg\\data",
                "XDG_STATE_HOME": "C:\\xdg\\state",
            },
        )
        self.assertEqual(
            sorted(environment),
            sorted(
                [
                    "PATH",
                    "SystemRoot",
                    "WINDIR",
                    "COMSPEC",
                    "PATHEXT",
                    "SYSTEMDRIVE",
                    "HOME",
                    "USERPROFILE",
                    "APPDATA",
                    "LOCALAPPDATA",
                    "TMP",
                    "TEMP",
                    "XDG_CONFIG_HOME",
                    "XDG_CACHE_HOME",
                    "XDG_DATA_HOME",
                    "XDG_STATE_HOME",
                ]
            ),
        )
        for name in ("HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TMP", "TEMP", "XDG_CONFIG_HOME"):
            self.assertTrue(environment[name].startswith(str(self.root)), name)
        self.assertEqual(environment["HOME"], environment["USERPROFILE"])
        self.assertEqual(environment["TMP"], environment["TEMP"])

    def test_posix_shape_keeps_the_allowlist_and_redirects_private_roots(self):
        environment = witness.child_environment(
            self.root,
            "posix",
            {
                "PATH": "/usr/bin",
                "HOME": "/root",
                "TMPDIR": "/tmp",
                "XDG_CONFIG_HOME": "/root/.config",
                "XDG_CACHE_HOME": "/root/.cache",
                "XDG_DATA_HOME": "/root/.local/share",
                "XDG_STATE_HOME": "/root/.local/state",
                "TENCENT_GLM_API_KEY": "fixture-credential-value",
                "GIT_DIR": "/mnt/q/repos/symmetry/.git/worktrees/symmetry4",
                "WSLENV": "TENCENT_GLM_API_KEY",
            },
        )
        self.assertEqual(
            sorted(environment),
            sorted(["PATH", "HOME", "TMPDIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"]),
        )
        for name in ("HOME", "TMPDIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"):
            self.assertTrue(environment[name].startswith(str(self.root)), name)


class PinnedBlobTest(unittest.TestCase):
    """The blob path is operator-supplied; an unset variable, a changed blob or a wrong magic is refused."""

    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.blob = pathlib.Path(directory.name) / "opencode"
        self.content = b"MZ" + b"owner-pinned opencode blob stand-in\n"
        self.blob.write_bytes(self.content)
        self.digest = hashlib.sha256(self.content).hexdigest()
        self.pin = {
            "label": "Windows PE",
            "asset": "opencode-windows-x64.zip",
            "magic": b"MZ",
            "size": len(self.content),
            "sha256": self.digest,
        }

    def resolve(self, configured=None, pins=None, platform_key="nt"):
        environment = {} if configured is None else {witness.BINARY_ENVIRONMENT: configured}
        with (
            mock.patch.dict(os.environ),
            mock.patch.dict(witness.PINS, pins or {"nt": self.pin}),
            mock.patch.object(witness, "running_platform", return_value=platform_key),
        ):
            os.environ.pop(witness.BINARY_ENVIRONMENT, None)
            os.environ.update(environment)
            return witness.pinned_binary()

    def test_unset_environment_is_refused_by_name(self):
        with self.assertRaisesRegex(
            RuntimeError,
            "SYMMETRY_OPENCODE_BINARY must name the owner-pinned Windows PE executable for platform nt",
        ):
            self.resolve()

    def test_missing_declared_blob_is_refused(self):
        with self.assertRaisesRegex(RuntimeError, "SYMMETRY_OPENCODE_BINARY must name an existing file"):
            self.resolve(str(pathlib.Path(self.blob.parent) / "absent.exe"))

    def test_wrong_digest_is_refused_at_the_pinned_size(self):
        with self.assertRaisesRegex(RuntimeError, "owner-pinned executable identity mismatch for platform nt"):
            self.resolve(str(self.blob), pins={"nt": {**self.pin, "sha256": "0" * 64}})

    def test_wrong_size_is_refused_at_the_pinned_digest(self):
        with self.assertRaisesRegex(RuntimeError, "owner-pinned executable identity mismatch for platform nt"):
            self.resolve(str(self.blob), pins={"nt": {**self.pin, "size": len(self.content) + 1}})

    def test_matching_pin_returns_the_declared_blob(self):
        self.assertEqual(self.resolve(str(self.blob)), self.blob.resolve())

    def test_wrong_magic_is_refused_before_launch(self):
        elf_pin = {**self.pin, "label": "Linux ELF", "magic": b"\x7fELF"}
        with self.assertRaisesRegex(RuntimeError, "owner-pinned executable magic mismatch for platform nt"):
            self.resolve(str(self.blob), pins={"nt": elf_pin})

    def test_the_running_platform_selects_its_own_pin(self):
        elf = b"\x7fELF" + b"owner-pinned opencode blob stand-in\n"
        elf_path = pathlib.Path(self.blob.parent) / "elf-opencode"
        elf_path.write_bytes(elf)
        elf_pin = {
            "label": "Linux ELF",
            "asset": "opencode-linux-x64.tar.gz",
            "magic": b"\x7fELF",
            "size": len(elf),
            "sha256": hashlib.sha256(elf).hexdigest(),
        }
        self.assertEqual(
            self.resolve(str(elf_path), pins={"posix": elf_pin}, platform_key="posix"), elf_path.resolve()
        )
        with self.assertRaisesRegex(
            RuntimeError, "must name the owner-pinned Linux ELF executable for platform posix"
        ):
            self.resolve(pins={"posix": elf_pin}, platform_key="posix")

    def test_posix_pin_refuses_a_pe_magic(self):
        elf_pin = {**self.pin, "label": "Linux ELF", "magic": b"\x7fELF"}
        with self.assertRaisesRegex(RuntimeError, "owner-pinned executable magic mismatch for platform posix"):
            self.resolve(str(self.blob), pins={"posix": elf_pin}, platform_key="posix")


class CommittedReceiptTests(unittest.TestCase):
    """The shipped classifier reproduces every committed receipt's stored class verdicts; no re-render."""

    def test_committed_receipts_reproduce_their_stored_class_statuses(self):
        receipts = sorted((ROOT / "daemon/internal/harness/testdata/opencode/2.4.1").glob("*.json"))
        self.assertTrue(receipts)
        for path in receipts:
            with self.subTest(receipt=path.name):
                stored = json.loads(path.read_text())["classes"]
                recomputed = witness.classify(copy.deepcopy(json.loads(path.read_text())))["classes"]
                for name, value in stored.items():
                    self.assertEqual(recomputed[name]["status"], value["status"])


class BindingTest(FixtureRepo, unittest.TestCase):
    REPOSITORY_PREFIX = "opencode-witness-binding-"
    REPOSITORY_DIRECTORY = None

    def test_the_binding_covers_the_tool_and_the_shared_module(self):
        binding = witness.binding_sha256(ROOT)
        self.assertEqual(
            sorted(binding),
            ["scripts/ci/opencode-v2-support-witness.py", "scripts/ci/source_subject.py"],
        )
        for relative, digest in binding.items():
            self.assertEqual(digest, hashlib.sha256((ROOT / relative).read_bytes()).hexdigest())

    def test_head_revision_reads_the_checkout_head(self):
        self.write("source.txt", "reviewed source\n")
        commit = self.commit("fixture")
        revision = witness.source_subject.head_revision(
            lambda *arguments: subprocess.check_output(["git", *arguments], cwd=self.repo)
        )
        self.assertEqual(revision, commit)


if __name__ == "__main__":
    unittest.main()
