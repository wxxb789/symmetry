#!/usr/bin/env python3
"""Keep failed native executions and no-op interrupts out of success evidence."""

import copy
import importlib.util
import http.client
import json
import os
import pathlib
import socket
import subprocess
import sys
import threading
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location("witness", pathlib.Path(__file__).with_name("opencode-v2-support-witness.py"))
witness = importlib.util.module_from_spec(spec)
spec.loader.exec_module(witness)


class ClassificationTests(unittest.TestCase):
    def receipt(self, terminal_type):
        terminal = {"id": "evt_terminal", "type": terminal_type, "data": {"sessionID": "ses_native", "error": {"type": "provider.auth", "status": 401}}}
        return {"operations": [], "events": [terminal], "classes": {
            "success": {"status": "observed", "terminal": terminal},
            "interruption": {"status": "observed", "first_token": None, "response": {"interrupted": False}, "terminal": terminal},
        }}

    def test_native_provider_auth_failure_is_not_success(self):
        receipt = witness.classify(self.receipt("session.execution.failed"))
        self.assertEqual(receipt["classes"]["success"]["status"], "run and failed")
        self.assertEqual(receipt["classes"]["provider_failure"]["terminals"], receipt["events"])
        self.assertEqual(receipt["classes"]["interruption"]["status"], "run and failed")
        self.assertEqual(receipt["classes"]["usage"]["status"], "unavailable")

    def test_readiness_failure_without_requests_preserves_unavailable_blocker(self):
        receipt = {"failure": "RuntimeError", "operations": [], "events": [], "classes": {
            name: {"status": "unavailable", "blocker": "RuntimeError"}
            for name in ("success", "interruption")
        }}
        classified = witness.classify(receipt)
        for name in ("success", "interruption"):
            with self.subTest(name=name):
                self.assertEqual(classified["classes"][name], {"status": "unavailable", "blocker": "RuntimeError"})

    def test_requests_without_terminals_are_failed_observations(self):
        for name, route in (("success", "prompt"), ("interruption", "interrupt")):
            with self.subTest(name=name):
                receipt = {"operations": [{"method": "POST", "path": "/api/session/ses_native/" + route}],
                           "events": [], "classes": {}}
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

    def test_successful_execution_without_tool_continuation_is_not_proof(self):
        receipt = self.receipt("session.execution.succeeded")
        receipt["classes"]["continuation_after_step"] = {"event_start": 0, "terminal": receipt["events"][0]}
        self.assertEqual(witness.classify(receipt)["classes"]["continuation_after_step"]["status"], "run and failed")

    def test_continuation_must_precede_the_selected_terminal(self):
        receipt = self.receipt("session.execution.succeeded")
        terminal = receipt["events"][0]
        def event(kind):
            return {"type": kind, "data": {"sessionID": "ses_native"}}
        steps = [event(kind) for kind in ("session.tool.success", "session.step.ended", "session.step.started")]
        receipt["classes"]["continuation_after_step"] = {"event_start": 0, "terminal": terminal}
        receipt["events"] = [*steps, terminal]
        self.assertEqual(witness.classify(copy.deepcopy(receipt))["classes"]["continuation_after_step"]["status"], "observed")
        receipt["events"] = [*steps[:2], terminal, steps[2]]
        self.assertEqual(witness.classify(receipt)["classes"]["continuation_after_step"]["status"], "run and failed")

    def test_cleanup_requires_tree_and_reader_barriers(self):
        receipt = self.receipt("session.execution.failed")
        cleanup = {"exit_code": 0, "server_exit_code": 1, "stream_reader_stopped": True, "output_readers_stopped": True}
        receipt["classes"]["cleanup"] = cleanup
        self.assertEqual(witness.classify(copy.deepcopy(receipt))["classes"]["cleanup"]["status"], "observed")
        for name, value in (("exit_code", 1), ("stream_reader_stopped", False), ("output_readers_stopped", False), ("server_exit_code", None)):
            with self.subTest(name=name):
                failed = copy.deepcopy(receipt)
                failed["classes"]["cleanup"][name] = value
                self.assertEqual(witness.classify(failed)["classes"]["cleanup"]["status"], "run and failed")

    def test_tree_kill_timeout_still_closes_real_process_socket_and_readers(self):
        child = subprocess.Popen([sys.executable, "-c", "import sys; sys.stdin.buffer.read()"],
                                 stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                 env={name: value for name, value in os.environ.items() if name.upper() in {"SYSTEMROOT", "WINDIR"}})
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


class ChunkedSSETests(unittest.TestCase):
    def capture(self, chunks):
        local, peer = socket.socketpair()
        response = http.client.HTTPResponse(local)
        try:
            local.settimeout(5)
            peer.sendall(b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n"
                         + b"".join(f"{len(chunk):x}\r\n".encode() + chunk + b"\r\n" for chunk in chunks)
                         + b"0\r\n\r\n")
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


if __name__ == "__main__":
    unittest.main()
