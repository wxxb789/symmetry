#!/usr/bin/env python3
"""Verify native capture preserves valid bytes and fails closed while draining."""

import importlib.util
import json
import os
import pathlib
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

SCRIPT = pathlib.Path(__file__).with_name("omo-native-evidence.py")
SPEC = importlib.util.spec_from_file_location("omo_native_evidence", SCRIPT)
WITNESS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WITNESS)


class OmONativeEvidenceTest(unittest.TestCase):
    def start_process(self, receipt, root, code, arguments, rpc=True):
        allowed = {"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT"}
        environment = {key: value for key, value in os.environ.items() if os.name == "nt" and key.upper() in allowed}
        process = WITNESS.NativeProcess(
            receipt,
            "isolated-python-capture",
            [sys.executable, "-I", "-u", "-c", code, *arguments],
            root,
            environment,
            rpc=rpc,
        )
        self.addCleanup(process.stop)
        return process

    def test_invalid_utf8_fails_consumer_and_archive_while_both_readers_drain(self):
        writer = r"""
import os
import sys
fd = int(sys.argv[1])
os.write(fd, bytes.fromhex(sys.argv[2]))
tail = bytes.fromhex(sys.argv[3])
for _ in range(128):
    os.write(fd, tail)
os.write(3 - fd, bytes.fromhex(sys.argv[4]))
"""
        for label, rpc in (("stdout", True), ("stderr", True), ("stdout", False), ("stderr", False)):
            with self.subTest(stream=label, rpc=rpc), tempfile.TemporaryDirectory() as directory:
                root = pathlib.Path(directory)
                receipt = WITNESS.Receipt("")
                invalid = b'{"type":"agent_idle","note":"private-undecodable-output\xff"}\n'
                tail_frame = {"type": "drain_marker", "note": "x" * 2048}
                tail_text = json.dumps(tail_frame, ensure_ascii=False)
                tail = (tail_text + "\n").encode("utf-8")
                other = b'{"type":"other_stream_drained"}\n'
                process = self.start_process(
                    receipt,
                    root,
                    writer,
                    ["1" if label == "stdout" else "2", invalid.hex(), tail.hex(), other.hex()],
                    rpc,
                )
                try:
                    # The tail exceeds a pipe buffer: a reader that stops at the
                    # invalid line prevents this child from reaching normal exit.
                    self.assertEqual(process.process.wait(timeout=10), 0)
                finally:
                    process.stop()
                self.assertTrue(all(not thread.is_alive() for thread in process.threads))
                self.assertTrue(receipt.failure.is_set())
                expected_reason = "native " + label + " output is not valid UTF-8"
                self.assertEqual(receipt.failure_reason, expected_reason)
                with self.assertRaisesRegex(WITNESS.WitnessError, expected_reason):
                    if rpc:
                        process.next(time.monotonic() + 1)
                    else:
                        process.finish_cli()

                output = root / "receipt.json"
                receipt.save(output)
                archived = json.loads(output.read_text(encoding="utf-8"))
                observations = archived["observations"]
                native = [item for item in observations if item["operation"] == "native_output"]
                failed = [item for item in native if item["classification"] == "failed"]
                self.assertEqual(len(failed), 1)
                self.assertEqual(failed[0]["stream"], label)
                self.assertEqual(failed[0]["error"], "invalid UTF-8")
                self.assertNotIn("raw", failed[0])
                self.assertNotIn("frame", failed[0])
                self.assertNotIn("\ufffd", json.dumps(native, ensure_ascii=False))
                self.assertNotIn("private-undecodable-output", json.dumps(native))
                tail_rows = [item for item in native if item["stream"] == label and item.get("raw") == tail_text]
                self.assertEqual(len(tail_rows), 128)
                other_rows = [item for item in native if item["stream"] != label]
                self.assertEqual(len(other_rows), 1)
                self.assertEqual(other_rows[0]["raw"], other.decode("utf-8").rstrip("\n"))
                stops = [item for item in observations if item["operation"] == "process_stop"]
                self.assertEqual(len(stops), 1)
                self.assertTrue(stops[0]["readersDrained"])
                self.assertEqual(stops[0]["classification"], "passed")

    def test_split_multibyte_utf8_preserves_rpc_cli_and_stderr_through_drain(self):
        writer = r"""
import os
import sys
stdout = bytes.fromhex(sys.argv[1])
stderr = bytes.fromhex(sys.argv[2])
split = int(sys.argv[3])
os.write(1, stdout[:split])
os.write(2, b'split_ready\n')
sys.stdin.buffer.readline()
os.write(1, stdout[split:])
for byte in stderr:
    os.write(2, bytes([byte]))
"""
        for rpc in (True, False):
            with self.subTest(rpc=rpc), tempfile.TemporaryDirectory() as directory:
                root = pathlib.Path(directory)
                ready = threading.Event()

                class SignallingReceipt(WITNESS.Receipt):
                    def record(self, operation, classification="passed", *, ready=ready, **data):
                        observation = super().record(operation, classification, **data)
                        if (
                            operation == "native_output"
                            and data.get("stream") == "stderr"
                            and data.get("raw") == "split_ready"
                        ):
                            ready.set()
                        return observation

                receipt = SignallingReceipt("")
                frame = {"type": "agent_idle", "note": "\u6c49\U0001f642\u00e9"}
                text = json.dumps(frame, ensure_ascii=False)
                stdout = (text + "\n").encode("utf-8")
                stderr_text = "diagnostic \u6c49\U0001f642\u00e9"
                stderr = stderr_text.encode("utf-8")
                split = stdout.index("\u6c49".encode("utf-8")) + 1
                process = self.start_process(receipt, root, writer, [stdout.hex(), stderr.hex(), str(split)], rpc)
                try:
                    self.assertTrue(ready.wait(timeout=10), "split-byte writer did not reach its barrier")
                    process.send("continue")
                    self.assertEqual(process.process.wait(timeout=10), 0)
                finally:
                    process.stop()
                receipt.check()
                if rpc:
                    _, actual = process.next(time.monotonic() + 1)
                    self.assertEqual(actual, frame)
                    self.assertTrue(process.queue.empty())
                else:
                    self.assertEqual(process.finish_cli(), text)

                output = root / "receipt.json"
                receipt.save(output)
                archived = json.loads(output.read_text(encoding="utf-8"))
                native = [item for item in archived["observations"] if item["operation"] == "native_output"]
                self.assertEqual([item["raw"] for item in native if item["stream"] == "stdout"], [text])
                self.assertEqual(
                    [item["raw"] for item in native if item["stream"] == "stderr"], ["split_ready", stderr_text]
                )
                self.assertTrue(all(item["classification"] == "passed" for item in native))
                self.assertTrue(all(not thread.is_alive() for thread in process.threads))


class PinnedBinaryTest(unittest.TestCase):
    """The operator names the pinned executable; an unset variable or a changed blob is refused."""

    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = pathlib.Path(directory.name)
        self.binary = self.root / "omo.exe"
        self.binary.write_bytes(b"pinned omo native evidence\n")
        self.pin = {"size": self.binary.stat().st_size, "sha256": WITNESS.digest(self.binary)}

    def resolve(self, system="Windows", pins=None, configured=None, path=None):
        """Call the pin with this host's PINS and environment patched out for the duration."""
        environment = {} if configured is None else {WITNESS.BINARY_ENVIRONMENT: configured}
        pins = {system: self.pin} if pins is None else pins
        with (
            mock.patch.dict(os.environ),
            mock.patch.dict(WITNESS.PINS, pins, clear=True),
        ):
            os.environ.pop(WITNESS.BINARY_ENVIRONMENT, None)
            os.environ.update(environment)
            return WITNESS.pinned_binary(system, path or self.binary)

    def test_unset_environment_is_refused_by_name(self):
        with self.assertRaisesRegex(WITNESS.WitnessError, WITNESS.BINARY_ENVIRONMENT):
            self.resolve()

    def test_platform_without_a_committed_pin_is_refused(self):
        with self.assertRaisesRegex(WITNESS.WitnessError, "unsupported operating system"):
            self.resolve(system="Plan9", pins={"Windows": self.pin}, configured=str(self.binary))

    def test_missing_declared_file_is_refused(self):
        with self.assertRaisesRegex(WITNESS.WitnessError, "must name an existing file"):
            self.resolve(configured=str(self.root / "absent.exe"))

    def test_wrong_digest_is_refused_at_the_pinned_size(self):
        pins = {"Windows": {"size": self.pin["size"], "sha256": "0" * 64}}
        with self.assertRaisesRegex(WITNESS.WitnessError, "exact platform binary pin mismatch"):
            self.resolve(pins=pins, configured=str(self.binary))

    def test_wrong_size_is_refused_at_the_pinned_digest(self):
        pins = {"Windows": {"size": self.pin["size"] + 1, "sha256": self.pin["sha256"]}}
        with self.assertRaisesRegex(WITNESS.WitnessError, "exact platform binary pin mismatch"):
            self.resolve(pins=pins, configured=str(self.binary))

    def test_binary_argument_must_name_the_declared_file(self):
        other = self.root / "other.exe"
        other.write_bytes(b"not the pinned blob\n")
        with self.assertRaisesRegex(WITNESS.WitnessError, "must name the file declared by"):
            self.resolve(configured=str(self.binary), path=other)

    def test_matching_pin_records_the_declared_executable(self):
        receipt = self.resolve(configured=str(self.binary))
        self.assertEqual(receipt["path"], str(self.binary))
        self.assertEqual(receipt["size"], self.pin["size"])
        self.assertEqual(receipt["sha256"], self.pin["sha256"])
        self.assertEqual(receipt["independentPlatformPin"], self.pin)


class UrlUserinfoRedactionTest(unittest.TestCase):
    """A hostile URL in captured output must not leak its password tail."""

    PASSWORD_IN_USERINFO = "https://alice:p@s:w0rd@control.internal/x"

    def sanitize(self, text):
        return WITNESS.Receipt("").sanitize(text)

    def test_password_containing_at_sign_is_redacted_whole(self):
        sanitized = self.sanitize(f'{{"url": "{self.PASSWORD_IN_USERINFO}"}}')
        self.assertEqual(sanitized, '{"url": "https://[REDACTED]@control.internal/x"}')
        for fragment in ("alice", "p@s:w0rd", "s:w0rd"):
            self.assertNotIn(fragment, sanitized)

    def test_userinfo_is_redacted_through_the_module_helper(self):
        self.assertEqual(
            WITNESS.redact_url_userinfo(self.PASSWORD_IN_USERINFO),
            "https://[REDACTED]@control.internal/x",
        )

    def test_authority_without_a_password_is_still_redacted(self):
        self.assertEqual(
            WITNESS.redact_url_userinfo("https://token@host:8443/p?q=1#f"),
            "https://[REDACTED]@host:8443/p?q=1#f",
        )

    def test_credential_free_urls_are_left_alone(self):
        for url in ("https://control.internal/x", "http://127.0.0.1:4000/api/v1/runtimes"):
            self.assertEqual(WITNESS.redact_url_userinfo(url), url)

    def test_ipv6_authority_keeps_its_brackets(self):
        self.assertEqual(
            WITNESS.redact_url_userinfo("https://u:p@[::1]:8080/x"),
            "https://[REDACTED]@[::1]:8080/x",
        )

    def test_unparsable_authority_is_dropped_rather_than_printed(self):
        self.assertEqual(
            WITNESS.redact_url_userinfo("see https://u:p@[::1/x for details"),
            "see [REDACTED] for details",
        )


if __name__ == "__main__":
    unittest.main()
