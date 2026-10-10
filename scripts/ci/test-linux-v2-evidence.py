#!/usr/bin/env python3
"""Dependency-free regression tests for the Linux v2 evidence exporter."""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("export-linux-v2-redacted-evidence.py")
LEGACY_NAMES = (
    "bind-before-commit", "prepare-before-bind", "commit-before-resume", "lease-arm-before-resume",
    "daemon-after-resume", "daemon-after-resume-clean-seal-boundary", "helper-mirror",
    "wrong-hello-session", "setsid-escape", "scan-failure", "helper-scan-failure-crash",
    "helper-scan-pending-crash", "literal-pid-reuse", "stop-response-lost",
    "release-response-lost", "helper-dead-mirror",
)


def metadata(replay: bool = False) -> dict:
    value = {
        "subject": "1" * 40,
        "tree": "2" * 40,
        "tree_sha256": "3" * 64,
        "go_version": "go1.27.0",
        "kernel_release": "6.8.0-test",
        "goos": "linux",
        "goarch": "amd64",
        "source_path": "/private/source.go",
        "source_sha256": "4" * 64,
        "binary_path": "/private/symmetry-daemon",
        "binary_sha256": "5" * 64,
    }
    if replay:
        value.update({"tree_digest": "2" * 40, "dirty_status_sha256": "6" * 64})
    return value


def receipt() -> dict:
    return {
        "version": 1,
        "legacy_v1_no_v2_proof": True,
        "status": "stopped",
        "owner_kind": "linux_helper",
        "owner_context_sha256": "7" * 64,
        "target_pid": 2101,
        "target_identity": "2101:111",
        "pipe_token_sha256": "8" * 64,
        "job_id_sha256": "9" * 64,
        "supervisor_pid": 2102,
        "supervisor_identity": "2102:112",
        "active_processes": 0,
    }


def journal() -> dict:
    owner = {
        "owner_kind": "linux_helper", "owner_context_sha256": "a" * 64,
        "target_pid": 2101, "target_identity": "2101:111", "supervisor_pid": 2102,
        "supervisor_identity": "2102:112", "job_id_sha256": "b" * 64,
        "has_stop_receipt": True,
    }
    return {
        "at": "2026-09-29T12:00:00Z", "path": "/private/runs/claim.json",
        "raw_sha256": "c" * 64, "run_id": "private-run-id", "generation": 3,
        "local_state": "stopping", "terminal_state": "cancelled", "terminal_verdict": "accepted",
        "pid": 2101, "process_identity": "2101:111", "started_at": "2026-09-29T11:59:00Z",
        "lease_expires_at": "2026-09-29T12:01:00Z", "has_process_details": True,
        "containment_unproven": False, "retain_workspace": True, "pending_events": 1,
        "pending_transitions": 0,
        "containment_scans": {
            "target_pid": 2101, "target_identity": "2101:111", "supervisor_pid": 2102,
            "supervisor_identity": "2102:112",
            "helper": {"pending_sequence": 2, "clean_result_sequence": 2, "completed_sequence": 2,
                       "sealed_sequence": 2, "unproven_sequence": 0},
            "mirror": {"pending_sequence": 2, "clean_result_sequence": 2, "completed_sequence": 2,
                       "sealed_sequence": 2, "unproven_sequence": 0},
        },
        "stop_receipt": receipt(), "handoff": owner, "authority": owner,
    }


def process() -> dict:
    return {
        "at": "2026-09-29T12:00:00Z", "role": "target_after_replay", "pid": 2101,
        "identity": "2101:111", "state": "Z", "pgrp": 2101, "session": 2000,
        "start_time": 111, "exists": False, "error": "private diagnostic text",
    }


def exit_status() -> dict:
    return {"requested_signal": "SIGKILL", "signaled": True, "signal": "SIGKILL", "exit_code": -1, "error": "private error"}


def legacy_fixture(name: str, raw_secret: str = "RAW-PRIVATE-SECRET") -> dict:
    common = {
        "version": 1, "case": name.replace("-", "_"), "metadata": metadata(),
        "case_root": "/private/case/root", "crash": exit_status(),
        "journal_snapshots": [journal()], "process_snapshots": [process()],
        "raw_output": {"first.stdout.log": f"provider token={raw_secret}"},
        "notes": [f"private log text {raw_secret}"],
    }
    if name in LEGACY_NAMES[:7]:
        common.update({"stage": "recovered_and_cleared", "task_id": "private-task", "run_id": "private-run",
                       "generation": 3, "journal_key": "private-run/3", "stop_receipt_before_release": receipt()})
        return common
    if name in LEGACY_NAMES[7:13]:
        common.update({"status": "passed", "assertions": {"physical_stop_observed": True, "no_clear": False}})
        if name == "wrong-hello-session":
            anchor = {"target_pid": 2101, "target_identity": "2101:111", "target_pgrp": 2101,
                      "target_session": 2000, "target_start_time": 111}
            common["helper_hello"] = {"actual": anchor, "transmitted": anchor, "supervisor_pid": 2102,
                                      "supervisor_identity": "2102:112"}
        if name == "literal-pid-reuse":
            identity = {"pid": 2101, "identity": "2101:111", "pgrp": 2101, "session": 2000, "start_time": 111}
            common["pid_reuse"] = {
                "task_id": "private-task", "parent_pid_namespace_inode": 100,
                "private_pid_namespace_inode": 101, "parent_network_namespace_inode": 200,
                "private_network_namespace_inode": 200, "control_url_sha256": "d" * 64,
                "namespace_creation_flags": ["--user", "--pid"],
                "namespace_allocator_path": "/proc/sys/kernel/ns_last_pid",
                "namespace_allocator_before": "2100", "namespace_allocator_reset_to": 2100,
                "namespace_allocator_readback": "2100", "pidfd_group_signal_supported": True,
                "original_target": identity, "helper": identity,
                "replacement": {**identity, "identity": "2101:222", "start_time": 222},
                "owner_stop_before_proof_reached_path": "/private/reached",
                "owner_stop_before_proof_release_path": "/private/release",
                "owner_stop_proof_result_path": "/private/result",
                "recovery_authenticated_marker_path": "/private/authenticated",
                "owner_stop_before_proof_reached": True,
                "owner_stop_before_proof_reached_sha256": "e" * 64,
                "owner_stop_before_proof_release": True,
                "owner_stop_proof_result": "identity_or_anchor_mismatch",
                "recovery_helper_scan_seal_eligible": True,
                "recovery_helper_scan_seal_eligible_after_proof": True,
                "recovery_stop_failure_observed": True,
                "recovery_stop_failure_event": f"private log {raw_secret}",
                "recovery_runtime_registered": True,
                "crash_statuses": [{"stage": "owner_loss", "status": exit_status()}],
                "recovery_daemon_shutdown": exit_status(),
                "assertions": {"same_pid_and_pgrp_reused": True},
                "output_digests": {"first_daemon_stdout": "12 bytes sha256:" + "f" * 64},
                "notes": [f"private note {raw_secret}"],
            }
        return common
    common["metadata"] = metadata(replay=True)
    common.update({
        "stage": "replayed_receipt_released_and_cleared", "crash_statuses": [exit_status()],
        "raw_journal_snapshots": [{"at": "2026-09-29T12:00:00Z", "path": "/private/journal",
                                   "sha256": "d" * 64, "snapshot": {"lease_token": raw_secret}}],
        "receipt_before_clear": receipt(), "replay_count": 2, "exact_receipt_replay": True,
        "clear_observed": True, "response_drop_operation": "stop", "response_drop_fired": True,
        "duplicate_stop_seen": False,
    })
    return common


class ExporterTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.temporary.name)

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def private_dir(self, name: str) -> pathlib.Path:
        path = self.root / name
        path.mkdir(mode=0o700)
        os.chmod(path, 0o700)
        return path

    def write_json(self, path: pathlib.Path, value: dict) -> bytes:
        path.parent.mkdir(parents=True, exist_ok=True)
        data = json.dumps(value, indent=2).encode() + b"\n"
        path.write_bytes(data)
        os.chmod(path, 0o600)
        return data

    def run_export(self, *arguments: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run([sys.executable, str(SCRIPT), *arguments], text=True, capture_output=True, check=False)

    def populate_legacy(self, source: pathlib.Path, raw_secret: str = "RAW-PRIVATE-SECRET") -> dict[pathlib.Path, bytes]:
        originals = {}
        for name in LEGACY_NAMES:
            path = source / name / "witness-evidence.json"
            originals[path] = self.write_json(path, legacy_fixture(name, raw_secret))
        return originals

    def test_legacy_projects_actual_shapes_without_mutating_or_leaking_raw_inputs(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        originals = self.populate_legacy(source)
        result = self.run_export("legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual((manifest["status"], manifest["complete"], len(manifest["files"])), ("passed", True, 16))
        combined = b"".join(path.read_bytes() for path in output.iterdir())
        for forbidden in (b"RAW-PRIVATE-SECRET", b"raw_output", b"raw_journal_snapshots", b"lease_token",
                          b"private-task", b"private-run", b"/private/", b"notes", b"error"):
            self.assertNotIn(forbidden, combined)
        replay = json.loads((output / "stop-response-lost.json").read_text())
        self.assertTrue(replay["exact_receipt_replay"])
        self.assertTrue(replay["clear_observed"])
        self.assertEqual(replay["receipt_before_clear"]["status"], "stopped")
        self.assertFalse(replay["process_snapshots"][0]["exists"])
        self.assertEqual(replay["metadata"]["binary_sha256"], "5" * 64)
        self.assertEqual(replay["journal_snapshots"][0]["stop_receipt"]["status"], "stopped")
        pid_reuse = json.loads((output / "literal-pid-reuse.json").read_text())["pid_reuse"]
        self.assertTrue(pid_reuse["assertions"]["same_pid_and_pgrp_reused"])
        self.assertEqual(pid_reuse["replacement"]["start_time"], 222)
        for path, data in originals.items():
            self.assertEqual(path.read_bytes(), data)

    def test_legacy_exports_unbound_handoff_without_inventing_supervisor_identity(self) -> None:
        source = self.private_dir("unbound-source")
        output = self.private_dir("unbound-output")
        self.populate_legacy(source)
        path = source / "prepare-before-bind" / "witness-evidence.json"
        value = json.loads(path.read_text())
        snapshot = value["journal_snapshots"][0]
        snapshot.pop("authority")
        snapshot.pop("stop_receipt")
        snapshot.pop("containment_scans")
        snapshot["has_process_details"] = False
        snapshot.pop("pid")
        snapshot.pop("process_identity")
        snapshot["handoff"].update({"supervisor_pid": 0, "supervisor_identity": "", "has_stop_receipt": False})
        original = self.write_json(path, value)
        result = self.run_export("legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        handoff = json.loads((output / "prepare-before-bind.json").read_text())["journal_snapshots"][0]["handoff"]
        self.assertEqual((handoff["supervisor_pid"], handoff["supervisor_identity"], handoff["has_stop_receipt"]), (0, "", False))
        self.assertEqual(path.read_bytes(), original)

    def test_legacy_rejects_unbound_handoff_with_bound_journal_evidence(self) -> None:
        for index, contradiction in enumerate(("stop_receipt", "authority", "containment_scans", "has_process_details", "pid", "process_identity")):
            with self.subTest(contradiction=contradiction):
                source = self.private_dir(f"contradictory-unbound-source-{index}")
                output = self.private_dir(f"contradictory-unbound-output-{index}")
                self.populate_legacy(source)
                path = source / "prepare-before-bind" / "witness-evidence.json"
                value = json.loads(path.read_text())
                snapshot = value["journal_snapshots"][0]
                bound = dict(snapshot)
                for key in ("stop_receipt", "authority", "containment_scans"):
                    snapshot.pop(key)
                snapshot["has_process_details"] = False
                snapshot.pop("pid")
                snapshot.pop("process_identity")
                snapshot["handoff"].update({"supervisor_pid": 0, "supervisor_identity": "", "has_stop_receipt": False})
                snapshot[contradiction] = bound[contradiction]
                original = self.write_json(path, value)
                result = self.run_export("legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("unbound handoff contradicts", result.stderr)
                self.assertEqual(path.read_bytes(), original)

    def test_legacy_rejects_empty_bound_or_receipted_supervisor_identity(self) -> None:
        for index, (nested, pid, has_receipt) in enumerate((("handoff", 2102, False), ("handoff", 0, True), ("authority", 0, False))):
            with self.subTest(nested=nested, pid=pid, has_receipt=has_receipt):
                source = self.private_dir(f"invalid-owner-source-{index}")
                output = self.private_dir(f"invalid-owner-output-{index}")
                self.populate_legacy(source)
                path = source / "prepare-before-bind" / "witness-evidence.json"
                value = json.loads(path.read_text())
                if nested == "handoff":
                    for key in ("stop_receipt", "authority", "containment_scans"):
                        value["journal_snapshots"][0].pop(key)
                    value["journal_snapshots"][0]["has_process_details"] = False
                    value["journal_snapshots"][0].pop("pid")
                    value["journal_snapshots"][0].pop("process_identity")
                value["journal_snapshots"][0][nested].update({"supervisor_pid": pid, "supervisor_identity": "", "has_stop_receipt": has_receipt})
                self.write_json(path, value)
                result = self.run_export("legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("bounded safe value", result.stderr)

    def test_empty_legacy_proof_lists_write_failed_incomplete_manifest(self) -> None:
        source = self.private_dir("empty-source")
        output = self.private_dir("empty-output")
        originals = self.populate_legacy(source)
        for path in originals:
            value = json.loads(path.read_text())
            value["journal_snapshots"] = []
            value["process_snapshots"] = []
            originals[path] = self.write_json(path, value)
        result = self.run_export("legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["status"], "failed")
        self.assertFalse(manifest["complete"])
        self.assertEqual(set(manifest["incomplete_evidence_files"]), {f"{name}.json" for name in LEGACY_NAMES})
        for path, data in originals.items():
            self.assertEqual(path.read_bytes(), data)

    def test_null_legacy_proof_lists_write_failed_incomplete_manifest(self) -> None:
        source = self.private_dir("null-source")
        output = self.private_dir("null-output")
        self.populate_legacy(source)
        path = source / LEGACY_NAMES[0] / "witness-evidence.json"
        value = json.loads(path.read_text())
        value["journal_snapshots"] = None
        value["process_snapshots"] = None
        self.write_json(path, value)
        result = self.run_export("legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["status"], "failed")
        self.assertFalse(manifest["complete"])
        self.assertEqual(manifest["incomplete_evidence_files"], [f"{LEGACY_NAMES[0]}.json"])

    def test_legacy_rejects_truncated_nested_receipt_and_owner(self) -> None:
        for index, (nested, required_field) in enumerate((("stop_receipt", "status"), ("authority", "target_pid"))):
            with self.subTest(nested=nested, required_field=required_field):
                source = self.private_dir(f"truncated-source-{index}")
                output = self.private_dir(f"truncated-output-{index}")
                self.populate_legacy(source)
                path = source / LEGACY_NAMES[0] / "witness-evidence.json"
                value = json.loads(path.read_text())
                del value["journal_snapshots"][0][nested][required_field]
                original = self.write_json(path, value)
                result = self.run_export("legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
                self.assertNotEqual(result.returncode, 0)
                if (output / "manifest.json").exists():
                    self.assertNotEqual(json.loads((output / "manifest.json").read_text())["status"], "passed")
                self.assertEqual(path.read_bytes(), original)

    def test_legacy_rejects_unknown_credential_bearing_field(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        self.populate_legacy(source)
        path = source / LEGACY_NAMES[0] / "witness-evidence.json"
        value = json.loads(path.read_text())
        value["provider_token"] = "must-not-be-trusted"
        self.write_json(path, value)
        result = self.run_export("legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unknown fields", result.stderr)

    def test_legacy_credential_scan_applies_to_sanitized_safe_fields(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        self.populate_legacy(source)
        secret_file = self.root / "secrets"
        secret_file.write_bytes(b"recovered_and_cleared\0")
        result = self.run_export("legacy", str(output), "1" * 40, "failed", "--witness-root", str(source),
                                 "--secret-values-file", str(secret_file))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("configured credential value", result.stderr)

    def test_legacy_rejects_symlink_source(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        self.populate_legacy(source)
        victim = source / LEGACY_NAMES[0] / "real.json"
        self.write_json(victim, legacy_fixture(LEGACY_NAMES[0]))
        link = source / LEGACY_NAMES[0] / "witness-evidence.json"
        link.unlink()
        link.symlink_to(victim)
        result = self.run_export("legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("bounded regular file", result.stderr)

    def test_missing_legacy_evidence_writes_failed_incomplete_manifest(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        result = self.run_export("legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["status"], "failed")
        self.assertFalse(manifest["complete"])
        self.assertEqual(len(manifest["missing_allowlisted_files"]), 16)

    def test_compose_still_copies_allowlisted_document_exactly(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        expected = {}
        for name in ("rootless-compose-pi.json", "rootless-compose-opencode.json", "TestProductionLinuxV2UnprovisionedHostRefusal.json"):
            expected[name] = self.write_json(source / name, {"case": name, "status": "refused"})
        result = self.run_export("compose", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        for name, data in expected.items():
            self.assertEqual((output / name).read_bytes(), data)

    def assert_compose_rejects_credential(self, value: dict, secret: str, escaped: bool) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        path = source / "rootless-compose-pi.json"
        original = self.write_json(path, value)
        if escaped:
            encoded_secret = "".join(f"\\u{ord(character):04x}" for character in secret).encode()
            original = original.replace(secret.encode(), encoded_secret)
            path.write_bytes(original)
            self.assertNotIn(secret.encode(), original)
        secret_file = self.root / "secrets"
        secret_file.write_bytes(secret.encode() + b"\0")
        os.chmod(secret_file, 0o600)
        result = self.run_export("compose", str(output), "1" * 40, "passed", "--witness-root", str(source),
                                 "--secret-values-file", str(secret_file))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("configured credential value", result.stderr)
        self.assertNotIn(secret, result.stdout + result.stderr)
        self.assertEqual(list(output.iterdir()), [])
        self.assertEqual(path.read_bytes(), original)

    def test_compose_rejects_raw_credential_before_publication(self) -> None:
        secret = "OWNED-RAW-CREDENTIAL"
        self.assert_compose_rejects_credential({"case": "refusal", "details": ["prefix " + secret + " suffix"]},
                                             secret, escaped=False)

    def test_compose_rejects_escaped_credential_value_before_publication(self) -> None:
        secret = "OWNED-ESCAPED-CREDENTIAL"
        self.assert_compose_rejects_credential({"case": "refusal", "details": [{"note": "prefix " + secret + " suffix"}]},
                                             secret, escaped=True)

    def test_compose_rejects_escaped_credential_key_before_publication(self) -> None:
        secret = "OWNED-ESCAPED-CREDENTIAL"
        self.assert_compose_rejects_credential({"case": "refusal", "details": [{"prefix-" + secret + "-suffix": "refused"}]},
                                             secret, escaped=True)

    def test_manual_missing_case_set_remains_rejected(self) -> None:
        output = self.private_dir("output")
        result = self.run_export("manual", str(output), "1" * 40, "failed", "--witness-root", "/tmp/witness",
                                 "--receipt-root", "/tmp/receipt")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("one exact case set", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
