#!/usr/bin/env python3
"""Dependency-free regression tests for the Linux v2 evidence exporter."""

from __future__ import annotations

import hashlib
import itertools
import json
import os
import pathlib
import re
import subprocess
import tempfile
import unittest

import citest

SCRIPT = pathlib.Path(__file__).with_name("export-linux-v2-redacted-evidence.py")
DAEMON_LOG_SCAN_DIRECTORIES = ("daemon/internal/app", "daemon/internal/platform", "daemon/cmd")
DAEMON_LOG_SCANNED_PRODUCERS = (
    # call, zero-based argument index, allowlist field
    ("queueFailure", 2, "stage"),
    ("queueTerminalTransitionWithRetry", 2, "state"),
    ("queueTerminalTransitionAndAcknowledgementWithContext", 2, "state"),
    ("queueTerminalTransitionAndAcknowledgementWithContext", 5, "outcome"),
    ("stopRecoveredJournal", 1, "reason"),
    ("terminateForLease", 1, "reason"),
    ("handleOrdinaryDeliveryError", 3, "reason"),
    ("newIDWithRetry", 3, "kind"),
)
LEGACY_NAMES = (
    "bind-before-commit",
    "prepare-before-bind",
    "commit-before-resume",
    "lease-arm-before-resume",
    "daemon-after-resume",
    "daemon-after-resume-clean-seal-boundary",
    "helper-mirror",
    "wrong-hello-session",
    "setsid-escape",
    "scan-failure",
    "helper-scan-failure-crash",
    "helper-scan-pending-crash",
    "literal-pid-reuse",
    "stop-response-lost",
    "release-response-lost",
    "helper-dead-mirror",
)


def load_exporter():
    return citest.load_gate(SCRIPT, "export_linux_v2_redacted_evidence")


def go_call_argument_lists(text: str, call_pattern: str) -> list[list[str]]:
    # Top-level arguments of every call matching call_pattern, which must end
    # at the opening parenthesis. String literals and nested parentheses or
    # composite literals stay inside their argument.
    found = []
    for match in re.finditer(call_pattern, text):
        depth = 1
        index = match.end()
        arguments, current, in_string, escaped = [], [], False, False
        while index < len(text) and depth > 0:
            character = text[index]
            index += 1
            if in_string:
                current.append(character)
                if escaped:
                    escaped = False
                elif character == "\\":
                    escaped = True
                elif character == '"':
                    in_string = False
                continue
            if character == '"':
                in_string = True
                current.append(character)
            elif character in "([{":
                depth += 1
                current.append(character)
            elif character in ")]}":
                depth -= 1
                if depth:
                    current.append(character)
            elif character == "," and depth == 1:
                arguments.append("".join(current).strip())
                current = []
            else:
                current.append(character)
        arguments.append("".join(current).strip())
        found.append(arguments)
    return found


def go_string_literal(argument: str) -> str | None:
    if len(argument) >= 2 and argument.startswith('"') and argument.endswith('"'):
        return argument[1:-1]
    return None


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
        "owner_kind": "linux_helper",
        "owner_context_sha256": "a" * 64,
        "target_pid": 2101,
        "target_identity": "2101:111",
        "supervisor_pid": 2102,
        "supervisor_identity": "2102:112",
        "job_id_sha256": "b" * 64,
        "has_stop_receipt": True,
    }
    return {
        "at": "2026-09-29T12:00:00Z",
        "path": "/private/runs/claim.json",
        "raw_sha256": "c" * 64,
        "run_id": "private-run-id",
        "generation": 3,
        "local_state": "stopping",
        "terminal_state": "cancelled",
        "terminal_verdict": "accepted",
        "pid": 2101,
        "process_identity": "2101:111",
        "started_at": "2026-09-29T11:59:00Z",
        "lease_expires_at": "2026-09-29T12:01:00Z",
        "has_process_details": True,
        "containment_unproven": False,
        "retain_workspace": True,
        "pending_events": 1,
        "pending_transitions": 0,
        "containment_scans": {
            "target_pid": 2101,
            "target_identity": "2101:111",
            "supervisor_pid": 2102,
            "supervisor_identity": "2102:112",
            "helper": {
                "pending_sequence": 2,
                "clean_result_sequence": 2,
                "completed_sequence": 2,
                "sealed_sequence": 2,
                "unproven_sequence": 0,
            },
            "mirror": {
                "pending_sequence": 2,
                "clean_result_sequence": 2,
                "completed_sequence": 2,
                "sealed_sequence": 2,
                "unproven_sequence": 0,
            },
        },
        "stop_receipt": receipt(),
        "handoff": owner,
        "authority": owner,
    }


def process() -> dict:
    return {
        "at": "2026-09-29T12:00:00Z",
        "role": "target_after_replay",
        "pid": 2101,
        "identity": "2101:111",
        "state": "Z",
        "pgrp": 2101,
        "session": 2000,
        "start_time": 111,
        "exists": False,
        "error": "private diagnostic text",
    }


def exit_status() -> dict:
    return {
        "requested_signal": "SIGKILL",
        "signaled": True,
        "signal": "SIGKILL",
        "exit_code": -1,
        "error": "private error",
    }


def legacy_fixture(name: str, raw_secret: str = "RAW-PRIVATE-SECRET") -> dict:
    common = {
        "version": 1,
        "case": name.replace("-", "_"),
        "metadata": metadata(),
        "case_root": "/private/case/root",
        "crash": exit_status(),
        "journal_snapshots": [journal()],
        "process_snapshots": [process()],
        "raw_output": {"first.stdout.log": f"provider token={raw_secret}"},
        "notes": [f"private log text {raw_secret}"],
    }
    if name in LEGACY_NAMES[:7]:
        common.update(
            {
                "stage": "recovered_and_cleared",
                "task_id": "private-task",
                "run_id": "private-run",
                "generation": 3,
                "journal_key": "private-run/3",
                "stop_receipt_before_release": receipt(),
            }
        )
        return common
    if name in LEGACY_NAMES[7:13]:
        common.update({"status": "passed", "assertions": {"physical_stop_observed": True, "no_clear": False}})
        if name == "wrong-hello-session":
            anchor = {
                "target_pid": 2101,
                "target_identity": "2101:111",
                "target_pgrp": 2101,
                "target_session": 2000,
                "target_start_time": 111,
            }
            common["helper_hello"] = {
                "actual": anchor,
                "transmitted": anchor,
                "supervisor_pid": 2102,
                "supervisor_identity": "2102:112",
            }
        if name == "literal-pid-reuse":
            identity = {"pid": 2101, "identity": "2101:111", "pgrp": 2101, "session": 2000, "start_time": 111}
            common["pid_reuse"] = {
                "task_id": "private-task",
                "parent_pid_namespace_inode": 100,
                "private_pid_namespace_inode": 101,
                "parent_network_namespace_inode": 200,
                "private_network_namespace_inode": 200,
                "control_url_sha256": "d" * 64,
                "namespace_creation_flags": ["--user", "--pid"],
                "namespace_allocator_path": "/proc/sys/kernel/ns_last_pid",
                "namespace_allocator_before": "2100",
                "namespace_allocator_reset_to": 2100,
                "namespace_allocator_readback": "2100",
                "pidfd_group_signal_supported": True,
                "original_target": identity,
                "helper": identity,
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
    common.update(
        {
            "stage": "replayed_receipt_released_and_cleared",
            "crash_statuses": [exit_status()],
            "raw_journal_snapshots": [
                {
                    "at": "2026-09-29T12:00:00Z",
                    "path": "/private/journal",
                    "sha256": "d" * 64,
                    "snapshot": {"lease_token": raw_secret},
                }
            ],
            "receipt_before_clear": receipt(),
            "replay_count": 2,
            "exact_receipt_replay": True,
            "clear_observed": True,
            "response_drop_operation": "stop",
            "response_drop_fired": True,
            "duplicate_stop_seen": False,
        }
    )
    return common


def compose_refusal(harness: str) -> dict:
    return {
        "version": 1,
        "case": "rootless_compose_native_work_item_refusal",
        "harness": harness,
        "native_version": "1.5.2" if harness == "pi" else "",
        "generation": 1,
        "probe_returned_err_native_unverified": harness == "pi",
        "probe_returned_err_harness_unavailable": harness == "opencode",
        "work_item_id_sha256": "1" * 64,
        "task_id_sha256": "2" * 64,
        "run_id_sha256": "3" * 64,
        "runtime_id_sha256": "4" * 64,
        "run_state": "failed",
        "failure_stage": "linux_v2_required",
        "claim_fence_observed": True,
        "no_native_task_process": True,
        "no_task_result": True,
        "no_process_authority": True,
        "no_stop_receipt": True,
        "no_provider_access": True,
        "non_goal_work_item": True,
    }


CONTAINMENT_PINS = {
    "codex": {
        "case_set": "codex_containment",
        "harness": "codex",
        "native_version": "0.168.0",
        "native_version_line": "codex-cli 0.168.0",
        "tree_digest": "sha256:" + "2" * 64,
        "binary": "/usr/local/bin/codex",
        "binary_sha256": "a8cb58c528c76e7313d4a70294ec614757b0f4c7bf9683e11c88ecad8c3d421b",
        "archive_sha256": "3acbff91c0a8e30767d47c8b6941279a603ab4087280aa3e546537cef3622cb8",
        "witness": "codex_containment/TestProductionLinuxV2CodexNativeContainment.json",
        "replay": "stage-replay/TestProductionLinuxV2StageCrashReplayMatrix_native_release_codex.json",
        "exported": ("codex-native-containment.json", "codex-stage-crash-replay-native-release.json"),
    },
    "claude_code": {
        "case_set": "claude_containment",
        "harness": "claude_code",
        "native_version": "2.1.323",
        "native_version_line": "2.1.323 (Claude Code)",
        "tree_digest": "sha256:" + "3" * 64,
        "binary": "/usr/local/bin/claude",
        "binary_sha256": "604d29a6869409106b6a1110bdadcc8b72e8654a5a865d23258f938a696e348e",
        "archive_sha256": "522255678548859b9799b7b098c03a3aa286c9a9ede01957e35f4713ccc43631",
        "witness": "claude_containment/TestProductionLinuxV2ClaudeNativeContainment.json",
        "replay": "stage-replay/TestProductionLinuxV2StageCrashReplayMatrix_native_release_claude.json",
        "exported": ("claude-native-containment.json", "claude-stage-crash-replay-native-release.json"),
    },
}


def containment_witness(kind: str, native_open_reached: bool = True) -> dict:
    pin = CONTAINMENT_PINS[kind]
    return {
        "version": 1,
        "case": pin["case_set"],
        "harness": pin["harness"],
        "native_version": pin["native_version"],
        "installed_owner_verified": True,
        "native_binary": pin["binary"],
        "native_binary_sha256": "sha256:" + pin["binary_sha256"],
        "native_version_line": pin["native_version_line"],
        "subject": {"tree_digest": pin["tree_digest"]},
        "containment": {"native_open_reached": native_open_reached},
        "no_native_fallback": False,
        "owner_witness_outcome": "passed",
    }


PI_PINS = {
    "case_set": "stale_leaf",
    "binary": "/usr/local/lib/symmetry-pi/pi",
    "binary_sha256": "6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57",
    "archive_sha256": "811c6f2c9e7aaee252b5404252e556fb713aa209c1f7f924226aa77d4ef70cb5",
    "version_line": "1.5.2",
    "tree_digest": "sha256:" + "4" * 64,
    "witness": "stale-leaf/TestProductionLinuxV2StaleLeafIdentityRetainsJournal.json",
    "exported": ("stale-leaf-identity.json",),
}


def pi_durability_witness(pin: dict = PI_PINS) -> dict:
    return {
        "version": 1,
        "case": "stage_replay",
        "harness": "pi",
        "native_version": "1.5.2",
        "native_binary": pin["binary"],
        "native_binary_sha256": "sha256:" + pin["binary_sha256"],
        "native_version_line": pin["version_line"],
        "subject": {"tree_digest": pin["tree_digest"]},
        "owner_witness_outcome": "recovered",
    }


def pi_cleanup_witness(runtime_kind: str = "pi") -> dict:
    return {
        "version": 1,
        "test": "TestProductionLinuxV2CloneCleanupAuthenticatedScratch",
        "status": "passed",
        "fixture": {"runtime_kind": runtime_kind},
    }


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
        return citest.write_json(path, value, mode=0o600)

    def write_log(self, path: pathlib.Path, lines: list[bytes]) -> bytes:
        return citest.write_bytes(path, b"\n".join(lines) + b"\n", mode=0o600)

    def populate_legacy(
        self, source: pathlib.Path, raw_secret: str = "RAW-PRIVATE-SECRET"
    ) -> dict[pathlib.Path, bytes]:
        originals = {}
        for name in LEGACY_NAMES:
            path = source / name / "witness-evidence.json"
            originals[path] = self.write_json(path, legacy_fixture(name, raw_secret))
        return originals

    def populate_containment(self, source: pathlib.Path, kind: str, native_open_reached: bool = True) -> None:
        pin = CONTAINMENT_PINS[kind]
        self.write_json(source / pin["witness"], containment_witness(kind, native_open_reached))
        self.write_json(
            source / pin["replay"],
            {
                "version": 1,
                "case": "stage_replay",
                "harness": pin["harness"],
                "native_version_line": pin["native_version_line"],
                "stage": "native_release",
                "status": "passed",
            },
        )

    def run_containment_gate(
        self, kind: str, source: pathlib.Path, receipts: pathlib.Path, output: pathlib.Path
    ) -> subprocess.CompletedProcess:
        pin = CONTAINMENT_PINS[kind]
        return citest.run_gate(
            SCRIPT,
            "manual",
            str(output),
            "1" * 40,
            "passed",
            "--witness-root",
            str(source),
            "--receipt-root",
            str(receipts),
            "--case-set",
            pin["case_set"],
            "--native-kind",
            kind,
            "--native-binary",
            pin["binary"],
            "--native-binary-sha256",
            "sha256:" + pin["binary_sha256"],
            "--native-archive-sha256",
            "sha256:" + pin["archive_sha256"],
            "--native-version-line",
            pin["native_version_line"],
            "--source-tree-digest",
            pin["tree_digest"],
        )

    def populate_pi_durability(self, source: pathlib.Path, witness: dict | None = None) -> None:
        self.write_json(source / PI_PINS["witness"], pi_durability_witness() if witness is None else witness)

    def run_pi_durability_gate(self, source, receipts, output, *, binary_sha256=None, case_set=None):
        return citest.run_gate(
            SCRIPT,
            "manual",
            str(output),
            "1" * 40,
            "passed",
            "--witness-root",
            str(source),
            "--receipt-root",
            str(receipts),
            "--case-set",
            case_set or PI_PINS["case_set"],
            "--native-kind",
            "pi",
            "--native-binary",
            PI_PINS["binary"],
            "--native-binary-sha256",
            binary_sha256 or "sha256:" + PI_PINS["binary_sha256"],
            "--native-archive-sha256",
            "sha256:" + PI_PINS["archive_sha256"],
            "--native-version-line",
            PI_PINS["version_line"],
            "--source-tree-digest",
            PI_PINS["tree_digest"],
        )

    def test_legacy_projects_actual_shapes_without_mutating_or_leaking_raw_inputs(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        originals = self.populate_legacy(source)
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual((manifest["status"], manifest["complete"], len(manifest["files"])), ("passed", True, 16))
        combined = b"".join(path.read_bytes() for path in output.iterdir())
        for forbidden in (
            b"RAW-PRIVATE-SECRET",
            b"raw_output",
            b"raw_journal_snapshots",
            b"lease_token",
            b"private-task",
            b"private-run",
            b"/private/",
            b"notes",
            b"error",
        ):
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
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        handoff = json.loads((output / "prepare-before-bind.json").read_text())["journal_snapshots"][0]["handoff"]
        self.assertEqual(
            (handoff["supervisor_pid"], handoff["supervisor_identity"], handoff["has_stop_receipt"]), (0, "", False)
        )
        self.assertEqual(path.read_bytes(), original)

    def test_legacy_rejects_unbound_handoff_with_bound_journal_evidence(self) -> None:
        for index, contradiction in enumerate(
            ("stop_receipt", "authority", "containment_scans", "has_process_details", "pid", "process_identity")
        ):
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
                result = citest.run_gate(
                    SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source)
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("unbound handoff contradicts", result.stderr)
                self.assertEqual(path.read_bytes(), original)

    def test_legacy_rejects_empty_bound_or_receipted_supervisor_identity(self) -> None:
        for index, (nested, pid, has_receipt) in enumerate(
            (("handoff", 2102, False), ("handoff", 0, True), ("authority", 0, False))
        ):
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
                value["journal_snapshots"][0][nested].update(
                    {"supervisor_pid": pid, "supervisor_identity": "", "has_stop_receipt": has_receipt}
                )
                self.write_json(path, value)
                result = citest.run_gate(
                    SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source)
                )
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
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
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
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["status"], "failed")
        self.assertFalse(manifest["complete"])
        self.assertEqual(manifest["incomplete_evidence_files"], [f"{LEGACY_NAMES[0]}.json"])

    def test_helper_scan_crash_stages_preserve_initialized_incomplete_proof(self) -> None:
        for name, case, scan_stage in (
            ("helper-scan-failure-crash", "helper_scan_failure_sigkill_before_cancel", "mirror_unproven_scan"),
            ("helper-scan-pending-crash", "helper_pending_scan_sigkill", "mirror_pending_scan"),
        ):
            stages = (None, scan_stage, "cancel_api_409_failed_run", "complete")
            for index, stage in enumerate(stages):
                with self.subTest(case=case, stage=stage):
                    source = self.private_dir(f"{name}-source-{index}")
                    self.populate_legacy(source)
                    path = source / name / "witness-evidence.json"
                    value = {
                        "version": 1,
                        "case": case,
                        "status": "initialized",
                        "metadata": metadata(),
                        "case_root": "/private/case/root",
                        "assertions": {},
                        "journal_snapshots": None,
                        "process_snapshots": None,
                    }
                    if stage is not None:
                        value["stage"] = stage
                    original = self.write_json(path, value)
                    for run_status in (
                        ("failed", "passed") if stage in ("cancel_api_409_failed_run", "complete") else ("failed",)
                    ):
                        output = self.private_dir(f"{name}-output-{index}-{run_status}")
                        result = citest.run_gate(
                            SCRIPT, "legacy", str(output), "1" * 40, run_status, "--witness-root", str(source)
                        )
                        if run_status == "failed":
                            self.assertEqual(result.returncode, 0, result.stderr)
                        else:
                            self.assertNotEqual(result.returncode, 0)
                        manifest = json.loads((output / "manifest.json").read_text())
                        self.assertEqual(manifest["status"], "failed")
                        self.assertFalse(manifest["complete"])
                        self.assertEqual(manifest["incomplete_evidence_files"], [f"{name}.json"])
                        projected = json.loads((output / f"{name}.json").read_text())
                        self.assertEqual(projected["status"], "initialized")
                        self.assertEqual(projected["assertions"], {})
                        self.assertIsNone(projected["journal_snapshots"])
                        self.assertIsNone(projected["process_snapshots"])
                        if stage is None:
                            self.assertNotIn("stage", projected)
                        else:
                            self.assertEqual(projected["stage"], stage)
                        self.assertNotIn(b"/private/", (output / f"{name}.json").read_bytes())
                    self.assertEqual(path.read_bytes(), original)

    def test_helper_scan_crash_stage_rejects_unknown_and_credential_canaries(self) -> None:
        for name, case, other_scan_stage in (
            ("helper-scan-failure-crash", "helper_scan_failure_sigkill_before_cancel", "mirror_pending_scan"),
            ("helper-scan-pending-crash", "helper_pending_scan_sigkill", "mirror_unproven_scan"),
        ):
            for index, (stage, credential) in enumerate(
                (
                    ("CREDENTIAL-CANARY-NOT-A-STAGE", False),
                    (other_scan_stage, False),
                    ("running_journal", True),
                    ("cancel_api_409_private_error", False),
                    ("offline", False),
                    ("cancel_api_409_failed_run", True),
                )
            ):
                with self.subTest(case=case, stage=stage, credential=credential):
                    source = self.private_dir(f"{name}-rejected-source-{index}")
                    output = self.private_dir(f"{name}-rejected-output-{index}")
                    self.populate_legacy(source)
                    path = source / name / "witness-evidence.json"
                    value = {
                        "version": 1,
                        "case": case,
                        "status": "initialized",
                        "stage": stage,
                        "metadata": metadata(),
                        "case_root": "/private/case/root",
                        "assertions": {},
                        "journal_snapshots": None,
                        "process_snapshots": None,
                    }
                    original = self.write_json(path, value)
                    arguments = ["legacy", str(output), "1" * 40, "failed", "--witness-root", str(source)]
                    if credential:
                        secret_file = self.root / f"{name}-stage-secrets"
                        secret_file.write_bytes(stage.encode() + b"\0")
                        arguments.extend(("--secret-values-file", str(secret_file)))
                    result = citest.run_gate(SCRIPT, *arguments)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(
                        "configured credential value" if credential else "stage is not allowlisted", result.stderr
                    )
                    self.assertNotIn(stage, result.stdout + result.stderr)
                    self.assertFalse((output / f"{name}.json").exists())
                    self.assertFalse((output / "manifest.json").exists())
                    self.assertNotIn(stage.encode(), b"".join(child.read_bytes() for child in output.iterdir()))
                    self.assertEqual(path.read_bytes(), original)

    def test_legacy_cancel_diagnostic_stage_rejects_unrelated_negative_case(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        self.populate_legacy(source)
        path = source / "scan-failure" / "witness-evidence.json"
        value = legacy_fixture("scan-failure")
        value["stage"] = "cancel_api_409_failed_run"
        original = self.write_json(path, value)
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("negative evidence contains unknown fields: stage", result.stderr)
        self.assertNotIn(value["stage"], result.stdout + result.stderr)
        self.assertFalse((output / "scan-failure.json").exists())
        self.assertFalse((output / "manifest.json").exists())
        self.assertEqual(path.read_bytes(), original)

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
                result = citest.run_gate(
                    SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source)
                )
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
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unknown fields", result.stderr)

    def test_legacy_credential_scan_applies_to_sanitized_safe_fields(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        self.populate_legacy(source)
        secret_file = self.root / "secrets"
        secret_file.write_bytes(b"recovered_and_cleared\0")
        result = citest.run_gate(
            SCRIPT,
            "legacy",
            str(output),
            "1" * 40,
            "failed",
            "--witness-root",
            str(source),
            "--secret-values-file",
            str(secret_file),
        )
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
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("bounded regular file", result.stderr)

    def test_legacy_failed_run_exports_sanitized_restarted_daemon_log_tails(self) -> None:
        source = self.private_dir("log-source")
        output = self.private_dir("log-output")
        originals = self.populate_legacy(source)
        case = "lease-arm-before-resume"
        stdout_lines = [
            json.dumps(
                {
                    "time": "2026-11-20T18:42:24.695900552Z",
                    "level": "WARN",
                    "msg": "stop_recovered_process_failed",
                    "run_id": "LEASE-TOKEN-abc123def456",
                    "generation": 1,
                    "stage": "process_exit",
                    "state": "failed",
                    "reason": "lease renewal failed",
                    "outcome": "failed",
                    "kind": "terminal transition",
                    "attempt": 2,
                    "runtime_epoch": 7,
                    "skew_ms": -250,
                    "lease_safety_margin_ms": 1500,
                    "command_id": "private-command",
                    "error": "open /home/runner/work/symmetry/daemon.json: permission denied",
                    "workspace": "/home/runner/work/_temp/symmetry-linux-containment-witness/lease-arm-before-resume",
                }
            ).encode(),
            json.dumps(
                {
                    "time": "2026-11-20T18:42:24Z",
                    "level": "WARN",
                    "msg": "recovered_journal_stopped",
                    "reason": "stale_stop",
                    "kind": "terminal transition",
                }
            ).encode(),
            json.dumps(
                {
                    "time": "2026-11-20T18:42:25Z",
                    "level": "INFO",
                    "msg": "/home/runner/work/_temp/symmetry-linux-containment-witness",
                    "error": "JOINED-TOKEN-77ff",
                }
            ).encode(),
            # Negative boundaries: a bad time, a non-ASCII digit in the time, a
            # bad level, made-up text values, an out-of-range integer and a
            # boolean integer must be dropped without losing the records.
            json.dumps({"time": "2026-11-20 18:42:25Z", "level": "INFO", "msg": "runtime_registered"}).encode(),
            json.dumps({"time": "\u0662026-11-20T18:42:25Z", "level": "INFO", "msg": "runtime_registered"}).encode(),
            json.dumps({"time": "2026-11-20T18:42:25Z", "level": "NOTICE", "msg": "runtime_registered"}).encode(),
            json.dumps(
                {
                    "time": "2026-11-20T18:42:25Z",
                    "level": "INFO",
                    "msg": "runtime_registered",
                    "state": "stopping",
                    "reason": "owner_loss",
                    "outcome": "lost",
                    "kind": "stop",
                    "bytes": 2**31,
                    "attempt": True,
                    "generation": 3,
                }
            ).encode(),
            b"goroutine 1 [running]: /home/runner/work/symmetry/daemon/internal/app/app.go:2148",
            b'{"time":"2026-11-20T18:42:26Z","level":"INFO","msg":"runtime_registered","msg":"duplicated"}',
        ]
        stdout_bytes = self.write_log(source / case / "second-daemon.stdout.log", stdout_lines)
        self.write_log(
            source / case / "second-daemon.stderr.log", [b"run daemon: linux child subreaper: operation not permitted"]
        )
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads((output / f"{case}-second-daemon-stdout.json").read_text())
        self.assertEqual(
            (document["version"], document["case"], document["daemon"], document["stream"]),
            (1, case, "second", "stdout"),
        )
        self.assertFalse(document["truncated"])
        self.assertEqual(document["dropped_lines"], 5)
        self.assertEqual(
            document["messages"],
            [
                {
                    "time": "2026-11-20T18:42:24.695900552Z",
                    "level": "WARN",
                    "msg": "stop_recovered_process_failed",
                    "attempt": 2,
                    "generation": 1,
                    "lease_safety_margin_ms": 1500,
                    "runtime_epoch": 7,
                    "skew_ms": -250,
                    "kind": "terminal transition",
                    "outcome": "failed",
                    "reason": "lease renewal failed",
                    "stage": "process_exit",
                    "state": "failed",
                },
                {
                    "time": "2026-11-20T18:42:24Z",
                    "level": "WARN",
                    "msg": "recovered_journal_stopped",
                    "kind": "terminal transition",
                    "reason": "stale_stop",
                },
                {"time": "2026-11-20T18:42:25Z", "level": "INFO"},
                {"time": "2026-11-20T18:42:25Z", "level": "INFO", "msg": "runtime_registered", "generation": 3},
            ],
        )
        stderr_document = json.loads((output / f"{case}-second-daemon-stderr.json").read_text())
        self.assertEqual((stderr_document["messages"], stderr_document["dropped_lines"]), ([], 1))
        published = (output / f"{case}-second-daemon-stdout.json").read_bytes() + (
            output / f"{case}-second-daemon-stderr.json"
        ).read_bytes()
        for forbidden in (
            b"/home/runner",
            b"LEASE-TOKEN-abc123def456",
            b"JOINED-TOKEN-77ff",
            b"permission denied",
            b"private-command",
            b"goroutine",
            b"duplicated",
            b"operation not permitted",
            b'"error"',
            b'"run_id"',
            b'"workspace"',
            b"owner_loss",
            b"NOTICE",
        ):
            self.assertNotIn(forbidden, published)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(len(manifest["files"]), 18)
        self.assertFalse(manifest["raw_logs_included"])
        entries = {entry["file"]: entry for entry in manifest["files"]}
        self.assertEqual(
            entries[f"{case}-second-daemon-stdout.json"],
            {
                "file": f"{case}-second-daemon-stdout.json",
                "bytes": len((output / f"{case}-second-daemon-stdout.json").read_bytes()),
                "sha256": hashlib.sha256((output / f"{case}-second-daemon-stdout.json").read_bytes()).hexdigest(),
                "kind": "daemon_log_tail",
                "daemon": "second",
                "stream": "stdout",
                "truncated": False,
                "records": 4,
                "dropped_lines": 5,
            },
        )
        self.assertEqual(entries[f"{case}-second-daemon-stderr.json"]["records"], 0)
        for path, data in originals.items():
            self.assertEqual(path.read_bytes(), data)
        self.assertEqual((source / case / "second-daemon.stdout.log").read_bytes(), stdout_bytes)

    def test_legacy_passed_run_omits_daemon_log_tails(self) -> None:
        source = self.private_dir("log-passed-source")
        output = self.private_dir("log-passed-output")
        self.populate_legacy(source)
        case = "lease-arm-before-resume"
        self.write_log(
            source / case / "second-daemon.stdout.log",
            [json.dumps({"time": "2026-11-20T18:42:24Z", "level": "INFO", "msg": "runtime_registered"}).encode()],
        )
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(len(manifest["files"]), 16)
        self.assertEqual(
            sorted(path.name for path in output.iterdir()),
            sorted(["manifest.json"] + [f"{name}.json" for name in LEGACY_NAMES]),
        )
        self.assertEqual([entry for entry in manifest["files"] if entry.get("kind") == "daemon_log_tail"], [])

    def test_legacy_daemon_log_tail_is_bounded_to_its_last_bytes(self) -> None:
        source = self.private_dir("tail-source")
        output = self.private_dir("tail-output")
        self.populate_legacy(source)
        case = "lease-arm-before-resume"
        lines = [
            json.dumps(
                {"time": "2026-11-20T18:42:24Z", "level": "INFO", "msg": "runtime_registered", "generation": index}
            ).encode()
            for index in range(2400)
        ]
        raw = self.write_log(source / case / "second-daemon.stdout.log", lines)
        self.assertGreater(len(raw), 64 << 10)
        unterminated = source / "daemon-after-resume" / "second-daemon.stdout.log"
        unterminated.write_bytes(b"x" * (70 << 10))
        os.chmod(unterminated, 0o600)
        self.assertNotIn(b"\n", unterminated.read_bytes())
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads((output / f"{case}-second-daemon-stdout.json").read_text())
        self.assertTrue(document["truncated"])
        boundary = raw.find(b"\n", len(raw) - (64 << 10)) + 1
        expected = [json.loads(line) for line in raw[boundary:].split(b"\n") if line]
        self.assertEqual(document["messages"], expected)
        self.assertEqual(document["dropped_lines"], 0)
        self.assertLess(len(document["messages"]), len(lines))
        self.assertEqual(document["messages"][-1]["generation"], len(lines) - 1)
        # No newline anywhere in the tail window: the window is discarded whole
        # rather than kept as one unprojectable partial line (dropped_lines 0).
        cut_window = json.loads((output / "daemon-after-resume-second-daemon-stdout.json").read_text())
        self.assertEqual((cut_window["messages"], cut_window["dropped_lines"], cut_window["truncated"]), ([], 0, True))

    def test_legacy_daemon_log_tail_accepts_empty_restarted_log(self) -> None:
        # The witness O_TRUNCs second-daemon.stdout/stderr.log before starting
        # the daemon, so a zero-byte log (usually stderr) is a legitimate
        # failed-run export input; the tail reader must not fail closed on it.
        source = self.private_dir("empty-log-source")
        output = self.private_dir("empty-log-output")
        self.populate_legacy(source)
        case = "lease-arm-before-resume"
        path = source / case / "second-daemon.stderr.log"
        path.write_bytes(b"")
        os.chmod(path, 0o600)
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads((output / f"{case}-second-daemon-stderr.json").read_text())
        self.assertEqual(
            (
                document["daemon"],
                document["stream"],
                document["truncated"],
                document["messages"],
                document["dropped_lines"],
            ),
            ("second", "stderr", False, [], 0),
        )
        manifest = json.loads((output / "manifest.json").read_text())
        entries = {entry["file"]: entry for entry in manifest["files"]}
        published = (output / f"{case}-second-daemon-stderr.json").read_bytes()
        self.assertEqual(
            entries[f"{case}-second-daemon-stderr.json"],
            {
                "file": f"{case}-second-daemon-stderr.json",
                "bytes": len(published),
                "sha256": hashlib.sha256(published).hexdigest(),
                "kind": "daemon_log_tail",
                "daemon": "second",
                "stream": "stderr",
                "truncated": False,
                "records": 0,
                "dropped_lines": 0,
            },
        )

    def test_legacy_daemon_log_tail_source_size_is_not_capped(self) -> None:
        # The 2 MiB allowlisted-evidence bound must not apply to daemon logs: a
        # long-lived daemon's log is read from its tail, not rejected whole.
        source = self.private_dir("large-log-source")
        output = self.private_dir("large-log-output")
        self.populate_legacy(source)
        case = "lease-arm-before-resume"
        lines = [
            json.dumps(
                {"time": "2026-11-20T18:42:24Z", "level": "INFO", "msg": "runtime_registered", "generation": index}
            ).encode()
            for index in range(34000)
        ]
        raw = self.write_log(source / case / "second-daemon.stdout.log", lines)
        self.assertGreater(len(raw), 2 << 20)
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads((output / f"{case}-second-daemon-stdout.json").read_text())
        self.assertTrue(document["truncated"])
        self.assertEqual(document["dropped_lines"], 0)
        self.assertLess(len(document["messages"]), len(lines))
        self.assertEqual(document["messages"][-1]["generation"], len(lines) - 1)

    def test_legacy_daemon_log_tail_rejects_nonprivate_or_linked_log(self) -> None:
        for index, kind in enumerate(("permissions", "symlink", "directory")):
            with self.subTest(kind=kind):
                source = self.private_dir(f"log-boundary-source-{index}")
                output = self.private_dir(f"log-boundary-output-{index}")
                self.populate_legacy(source)
                case = "lease-arm-before-resume"
                path = source / case / "second-daemon.stdout.log"
                self.write_log(
                    path,
                    [
                        json.dumps(
                            {"time": "2026-11-20T18:42:24Z", "level": "INFO", "msg": "runtime_registered"}
                        ).encode()
                    ],
                )
                if kind == "permissions":
                    os.chmod(path, 0o644)
                elif kind == "symlink":
                    target = path.with_name("private.log")
                    path.rename(target)
                    path.symlink_to(target)
                else:
                    path.unlink()
                    path.mkdir()
                result = citest.run_gate(
                    SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source)
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("private regular file", result.stderr)
                self.assertFalse((output / f"{case}-second-daemon-stdout.json").exists())
                self.assertFalse((output / "manifest.json").exists())

    def test_legacy_daemon_log_tail_credential_scan_fails_closed(self) -> None:
        # The value allowlists close every shape-based channel, so the only way
        # a configured credential can reach a tail document is by literally
        # being one of the daemon's own emitted values; that must still fail
        # closed through the configured-credential scan before publication.
        source = self.private_dir("log-secret-source")
        output = self.private_dir("log-secret-output")
        self.populate_legacy(source)
        case = "lease-arm-before-resume"
        secret = "lease renewal failed"
        self.write_log(
            source / case / "second-daemon.stdout.log",
            [
                json.dumps(
                    {
                        "time": "2026-11-20T18:42:24Z",
                        "level": "WARN",
                        "msg": "claim_failed",
                        "reason": secret,
                    }
                ).encode()
            ],
        )
        secret_file = self.root / "log-secrets"
        secret_file.write_bytes(secret.encode() + b"\0")
        os.chmod(secret_file, 0o600)
        result = citest.run_gate(
            SCRIPT,
            "legacy",
            str(output),
            "1" * 40,
            "failed",
            "--witness-root",
            str(source),
            "--secret-values-file",
            str(secret_file),
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("configured credential value", result.stderr)
        self.assertNotIn(secret, result.stdout + result.stderr)
        self.assertTrue((output / "bind-before-commit.json").exists())
        self.assertFalse((output / f"{case}-second-daemon-stdout.json").exists())
        self.assertFalse((output / "manifest.json").exists())
        self.assertNotIn(secret.encode(), b"".join(path.read_bytes() for path in output.iterdir()))

    def test_legacy_daemon_log_tail_drops_credential_shaped_values_without_secret_scan(self) -> None:
        # No --secret-values-file: the projection itself is the leak barrier.
        # The fixture values are built at runtime so no credential-shaped
        # literal sits in this file.
        token = "".join(("probe", "-", "0" * 20, "-", "f" * 32))
        hex_digest = "d" * 63
        source = self.private_dir("log-shape-source")
        output = self.private_dir("log-shape-output")
        self.populate_legacy(source)
        case = "lease-arm-before-resume"
        self.write_log(
            source / case / "second-daemon.stdout.log",
            [
                json.dumps(
                    {
                        "time": "2026-11-20T18:42:24Z",
                        "level": "WARN",
                        "msg": hex_digest,
                        "reason": token,
                    }
                ).encode()
            ],
        )
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "failed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads((output / f"{case}-second-daemon-stdout.json").read_text())
        self.assertEqual(document["messages"], [{"time": "2026-11-20T18:42:24Z", "level": "WARN"}])
        self.assertEqual(document["dropped_lines"], 0)
        published = (output / f"{case}-second-daemon-stdout.json").read_bytes()
        self.assertNotIn(token.encode(), published)
        self.assertNotIn(hex_digest.encode(), published)

    def test_daemon_log_allowlists_match_daemon_producers(self) -> None:
        # The projection's value sets are derived from the daemon source. This
        # scan fails when a daemon log call site or known producer introduces a
        # literal that is missing from its set, so the allowlists cannot drift
        # silently; values reaching a field through a variable stay under the
        # maintenance rule documented beside DAEMON_LOG_TEXT_VALUES.
        repository = SCRIPT.resolve().parents[2]
        if not (repository / DAEMON_LOG_SCAN_DIRECTORIES[0]).is_dir():
            self.skipTest(f"daemon sources are not present beside {SCRIPT}")
        exporter = load_exporter()
        messages = set()
        observed = {key: set() for key in exporter.DAEMON_LOG_TEXT_VALUES}
        log_calls = 0
        for directory in DAEMON_LOG_SCAN_DIRECTORIES:
            for path in sorted((repository / directory).rglob("*.go")):
                if path.name.endswith("_test.go"):
                    continue
                text = path.read_text(encoding="utf-8")
                for arguments in go_call_argument_lists(text, r"\.log\.(?:Info|Warn|Error|Debug)\("):
                    log_calls += 1
                    message = go_string_literal(arguments[0]) if arguments else None
                    self.assertIsNotNone(message, f"{path}: daemon log call without a literal message")
                    if message is not None:
                        messages.add(message)
                    for key, value in itertools.pairwise(arguments):
                        field = go_string_literal(key)
                        literal = go_string_literal(value)
                        if field in observed and literal is not None:
                            observed[field].add(literal)
                for name, position, field in DAEMON_LOG_SCANNED_PRODUCERS:
                    for arguments in go_call_argument_lists(text, r"\b" + name + r"\s*\("):
                        if position < len(arguments):
                            literal = go_string_literal(arguments[position])
                            if literal is not None:
                                observed[field].add(literal)
        protocol = repository / "daemon/internal/protocol/protocol.go"
        if protocol.is_file():
            observed["reason"].update(re.findall(r'Reconcile\w+Kind = "([^"]*)"', protocol.read_text(encoding="utf-8")))
        self.assertGreater(log_calls, 0, "the daemon log call scan found no call sites")
        self.assertEqual(
            sorted(messages - set(exporter.DAEMON_LOG_MESSAGES)),
            [],
            "daemon log messages missing from DAEMON_LOG_MESSAGES",
        )
        for field, allowed in exporter.DAEMON_LOG_TEXT_VALUES.items():
            self.assertEqual(
                sorted(observed[field] - set(allowed)),
                [],
                f"daemon {field} literals missing from DAEMON_LOG_TEXT_VALUES",
            )
        self.assertIn("process_exit", observed["stage"])
        self.assertIn("terminal transition", observed["kind"])
        self.assertIn("lease renewal failed", observed["reason"])
        self.assertIn("not eligible for reconcile", observed["reason"])

    def test_missing_legacy_evidence_writes_failed_incomplete_manifest(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["status"], "failed")
        self.assertFalse(manifest["complete"])
        self.assertEqual(len(manifest["missing_allowlisted_files"]), 16)

    def test_compose_still_copies_allowlisted_document_exactly(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        expected = {}
        for harness in ("pi", "opencode"):
            name = f"rootless-compose-{harness}.json"
            expected[name] = self.write_json(source / name, compose_refusal(harness))
        name = "TestProductionLinuxV2UnprovisionedHostRefusal.json"
        expected[name] = self.write_json(source / name, {"case": name, "status": "refused"})
        for case in ("shared", "pi", "opencode", "unprovisioned"):
            self.write_json(
                source / f"rootless-compose-{case}-diagnostic.json",
                {"version": 1, "case": case, "outcome": "passed", "stage": "complete"},
            )
        result = citest.run_gate(SCRIPT, "compose", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)
        for name, data in expected.items():
            self.assertEqual((output / name).read_bytes(), data)

    def test_compose_opencode_refusal_rejects_native_promotion_or_missing_durable_proof(self) -> None:
        changes = [
            {"native_version": "1.18.32"},
            {"native_version": "2.3.0"},
            {"native_version": "unavailable"},
            {"probe_returned_err_native_unverified": True},
            {"probe_returned_err_harness_unavailable": False},
            {"probe_returned_err_harness_unavailable": 1},
            {"transport_verified": True},
            {"run_state": "completed"},
            {"failure_stage": "capability_rejection"},
            {"generation": 0},
            {"generation": True},
            {"run_id_sha256": "private-run"},
        ]
        for key in (
            "claim_fence_observed",
            "no_native_task_process",
            "no_task_result",
            "no_process_authority",
            "no_stop_receipt",
            "no_provider_access",
            "non_goal_work_item",
        ):
            changes.extend(({key: False}, {key: 1}))
        for index, change in enumerate(changes):
            with self.subTest(change=change):
                source = self.private_dir(f"source-{index}")
                output = self.private_dir(f"output-{index}")
                path = source / "rootless-compose-opencode.json"
                original = self.write_json(path, compose_refusal("opencode") | change)
                result = citest.run_gate(
                    SCRIPT, "compose", str(output), "1" * 40, "failed", "--witness-root", str(source)
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((output / path.name).exists())
                self.assertEqual(path.read_bytes(), original)

    def test_compose_refusal_requires_exact_fields_and_keeps_pi_transport_evidence(self) -> None:
        for index, (harness, change, omit) in enumerate(
            (
                ("pi", {"native_version": ""}, None),
                ("pi", {"probe_returned_err_native_unverified": False}, None),
                ("pi", {"probe_returned_err_harness_unavailable": True}, None),
                ("opencode", {}, "no_process_authority"),
                ("opencode", {}, "probe_returned_err_harness_unavailable"),
                ("opencode", {"harness": "pi"}, None),
                ("opencode", {"status": "accepted"}, None),
            )
        ):
            with self.subTest(harness=harness, change=change, omit=omit):
                source = self.private_dir(f"source-{index}")
                output = self.private_dir(f"output-{index}")
                value = compose_refusal(harness) | change
                if omit:
                    del value[omit]
                path = source / f"rootless-compose-{harness}.json"
                self.write_json(path, value)
                result = citest.run_gate(
                    SCRIPT, "compose", str(output), "1" * 40, "failed", "--witness-root", str(source)
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((output / path.name).exists())

    def test_compose_failure_diagnostic_exports_without_success_receipts(self) -> None:
        for index, (case, stage, observation) in enumerate(
            (
                ("pi", "probe_projection", "transport_unverified"),
                ("pi", "runtime_registration", "offline"),
                ("opencode", "runtime_registration", "portal_unavailable"),
            )
        ):
            with self.subTest(case=case, stage=stage, observation=observation):
                source = self.private_dir(f"source-{index}")
                diagnostic = {
                    "version": 1,
                    "case": case,
                    "outcome": "failed",
                    "stage": stage,
                    "observation": observation,
                }
                name = f"rootless-compose-{case}-diagnostic.json"
                original = self.write_json(source / name, diagnostic)
                for run_status in ("failed", "passed"):
                    output = self.private_dir(f"output-{index}-{run_status}")
                    result = citest.run_gate(
                        SCRIPT, "compose", str(output), "1" * 40, run_status, "--witness-root", str(source)
                    )
                    if run_status == "failed":
                        self.assertEqual(result.returncode, 0, result.stderr)
                    else:
                        self.assertNotEqual(result.returncode, 0)
                    manifest = json.loads((output / "manifest.json").read_text())
                    self.assertEqual(manifest["status"], "failed")
                    self.assertEqual(manifest["failed_diagnostic_files"], [name])
                    self.assertIn(f"rootless-compose-{case}.json", manifest["missing_allowlisted_files"])
                    self.assertFalse(manifest["credentials_included"])
                    self.assertEqual((output / name).read_bytes(), original)
                self.assertEqual((source / name).read_bytes(), original)

    def test_compose_diagnostics_cannot_substitute_for_acceptance_receipts(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        for case in ("shared", "pi", "opencode", "unprovisioned"):
            self.write_json(
                source / f"rootless-compose-{case}-diagnostic.json",
                {"version": 1, "case": case, "outcome": "passed", "stage": "complete"},
            )
        result = citest.run_gate(SCRIPT, "compose", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["status"], "failed")
        self.assertEqual(
            set(manifest["missing_allowlisted_files"]),
            {
                "rootless-compose-pi.json",
                "rootless-compose-opencode.json",
                "TestProductionLinuxV2UnprovisionedHostRefusal.json",
            },
        )

    def test_compose_diagnostic_rejects_private_or_contradictory_values(self) -> None:
        base = {"version": 1, "case": "pi", "outcome": "failed", "stage": "probe"}
        changes = (
            ("pi", {"stage": "private-error-with-credential"}),
            ("pi", {"observation": "private-error-with-credential"}),
            ("pi", {"details": "private-error-with-credential"}),
            ("pi", {"case": "opencode"}),
            ("pi", {"version": True}),
            ("pi", {"outcome": "passed"}),
            ("pi", {"stage": "complete"}),
            ("pi", {"stage": "runtime_registration", "observation": "private-error-with-credential"}),
            ("pi", {"stage": "runtime_registration", "observation": "transport_unverified"}),
            ("pi", {"observation": "offline"}),
            ("pi", {"stage": "runtime_registration", "observation": "cancel_api_409_failed_run"}),
            ("shared", {"case": "shared", "stage": "runtime_registration", "observation": "offline"}),
            ("unprovisioned", {"case": "unprovisioned", "stage": "runtime_registration", "observation": "offline"}),
        )
        for index, (case, change) in enumerate(changes):
            with self.subTest(case=case, change=change):
                source = self.private_dir(f"source-{index}")
                output = self.private_dir(f"output-{index}")
                name = f"rootless-compose-{case}-diagnostic.json"
                original = self.write_json(source / name, base | change)
                result = citest.run_gate(
                    SCRIPT, "compose", str(output), "1" * 40, "failed", "--witness-root", str(source)
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("private-error-with-credential", result.stdout + result.stderr)
                self.assertEqual(list(output.iterdir()), [])
                self.assertEqual((source / name).read_bytes(), original)

    def test_compose_failed_diagnostic_prevents_passed_manifest(self) -> None:
        source = self.private_dir("source")
        output = self.private_dir("output")
        for harness in ("pi", "opencode"):
            self.write_json(source / f"rootless-compose-{harness}.json", compose_refusal(harness))
        name = "TestProductionLinuxV2UnprovisionedHostRefusal.json"
        self.write_json(source / name, {"case": name, "status": "refused"})
        for case in ("shared", "pi", "opencode", "unprovisioned"):
            value = {"version": 1, "case": case, "outcome": "passed", "stage": "complete"}
            if case == "pi":
                value.update({"outcome": "failed", "stage": "claim_binding"})
            self.write_json(source / f"rootless-compose-{case}-diagnostic.json", value)
        result = citest.run_gate(SCRIPT, "compose", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(json.loads((output / "manifest.json").read_text())["status"], "failed")

    def test_compose_diagnostic_preserves_credential_scan_for_allowlisted_values(self) -> None:
        for index, (stage, observation) in enumerate(
            (
                ("probe_projection", "transport_unverified"),
                ("runtime_registration", "offline"),
            )
        ):
            with self.subTest(stage=stage, observation=observation):
                source = self.private_dir(f"source-{index}")
                output = self.private_dir(f"output-{index}")
                path = source / "rootless-compose-pi-diagnostic.json"
                original = self.write_json(
                    path,
                    {
                        "version": 1,
                        "case": "pi",
                        "outcome": "failed",
                        "stage": stage,
                        "observation": observation,
                    },
                )
                secret_file = self.root / f"secrets-{index}"
                secret_file.write_bytes(observation.encode() + b"\0")
                result = citest.run_gate(
                    SCRIPT,
                    "compose",
                    str(output),
                    "1" * 40,
                    "failed",
                    "--witness-root",
                    str(source),
                    "--secret-values-file",
                    str(secret_file),
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("configured credential value", result.stderr)
                self.assertNotIn(observation, result.stdout + result.stderr)
                self.assertEqual(list(output.iterdir()), [])
                self.assertEqual(path.read_bytes(), original)

    def test_compose_diagnostic_rejects_nonprivate_or_linked_source(self) -> None:
        for index, kind in enumerate(("permissions", "symlink", "hardlink", "oversized")):
            with self.subTest(kind=kind):
                source = self.private_dir(f"source-{index}")
                output = self.private_dir(f"output-{index}")
                path = source / "rootless-compose-pi-diagnostic.json"
                self.write_json(path, {"version": 1, "case": "pi", "outcome": "failed", "stage": "probe"})
                if kind == "permissions":
                    os.chmod(path, 0o644)
                elif kind == "symlink":
                    target = source / "private.json"
                    path.rename(target)
                    path.symlink_to(target)
                elif kind == "hardlink":
                    os.link(path, source / "private.json")
                else:
                    path.write_bytes(path.read_bytes() + b" " * 2048)
                result = citest.run_gate(
                    SCRIPT, "compose", str(output), "1" * 40, "failed", "--witness-root", str(source)
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((output / "rootless-compose-pi-diagnostic.json").exists())

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
        result = citest.run_gate(
            SCRIPT,
            "compose",
            str(output),
            "1" * 40,
            "passed",
            "--witness-root",
            str(source),
            "--secret-values-file",
            str(secret_file),
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("configured credential value", result.stderr)
        self.assertNotIn(secret, result.stdout + result.stderr)
        self.assertEqual(list(output.iterdir()), [])
        self.assertEqual(path.read_bytes(), original)

    def test_compose_rejects_raw_credential_before_publication(self) -> None:
        secret = "OWNED-RAW-CREDENTIAL"
        self.assert_compose_rejects_credential(
            {"case": "refusal", "details": ["prefix " + secret + " suffix"]}, secret, escaped=False
        )

    def test_compose_rejects_escaped_credential_value_before_publication(self) -> None:
        secret = "OWNED-ESCAPED-CREDENTIAL"
        self.assert_compose_rejects_credential(
            {"case": "refusal", "details": [{"note": "prefix " + secret + " suffix"}]}, secret, escaped=True
        )

    def test_compose_rejects_escaped_credential_key_before_publication(self) -> None:
        secret = "OWNED-ESCAPED-CREDENTIAL"
        self.assert_compose_rejects_credential(
            {"case": "refusal", "details": [{"prefix-" + secret + "-suffix": "refused"}]}, secret, escaped=True
        )

    def test_manual_missing_case_set_remains_rejected(self) -> None:
        output = self.private_dir("output")
        result = citest.run_gate(
            SCRIPT,
            "manual",
            str(output),
            "1" * 40,
            "failed",
            "--witness-root",
            "/tmp/witness",
            "--receipt-root",
            "/tmp/receipt",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("one exact case set", result.stderr)

    def test_manual_containment_export_records_pinned_native_binary(self) -> None:
        for index, kind in enumerate(("codex", "claude_code")):
            with self.subTest(kind=kind):
                pin = CONTAINMENT_PINS[kind]
                source = self.private_dir(f"containment-source-{index}")
                receipts = self.private_dir(f"containment-receipts-{index}")
                output = self.private_dir(f"containment-output-{index}")
                self.populate_containment(source, kind)
                result = citest.run_gate(
                    SCRIPT,
                    "manual",
                    str(output),
                    "1" * 40,
                    "passed",
                    "--witness-root",
                    str(source),
                    "--receipt-root",
                    str(receipts),
                    "--case-set",
                    pin["case_set"],
                    "--native-kind",
                    kind,
                    "--native-binary",
                    pin["binary"],
                    "--native-binary-sha256",
                    "sha256:" + pin["binary_sha256"],
                    "--native-archive-sha256",
                    "sha256:" + pin["archive_sha256"],
                    "--native-version-line",
                    pin["native_version_line"],
                    "--source-tree-digest",
                    pin["tree_digest"],
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                manifest = json.loads((output / "manifest.json").read_text())
                self.assertEqual(manifest["case_set"], pin["case_set"])
                self.assertEqual(manifest["status"], "passed")
                self.assertEqual(
                    manifest["native_binary"],
                    {
                        "kind": kind,
                        "path": pin["binary"],
                        "sha256": "sha256:" + pin["binary_sha256"],
                        "archive_sha256": "sha256:" + pin["archive_sha256"],
                        "version": pin["native_version_line"],
                    },
                )
                self.assertEqual(manifest["containment"], {"native_open_reached": True})
                self.assertEqual(manifest["source_tree_digest"], pin["tree_digest"])
                self.assertEqual({entry["file"] for entry in manifest["files"]}, set(pin["exported"]))
                for name in pin["exported"]:
                    self.assertTrue((output / name).is_file())

    def test_manual_containment_digest_mismatch_refuses_publication(self) -> None:
        pin = CONTAINMENT_PINS["codex"]
        source = self.private_dir("mismatch-source")
        receipts = self.private_dir("mismatch-receipts")
        output = self.private_dir("mismatch-output")
        self.populate_containment(source, "codex")
        result = citest.run_gate(
            SCRIPT,
            "manual",
            str(output),
            "1" * 40,
            "passed",
            "--witness-root",
            str(source),
            "--receipt-root",
            str(receipts),
            "--case-set",
            pin["case_set"],
            "--native-kind",
            "codex",
            "--native-binary",
            pin["binary"],
            "--native-binary-sha256",
            pin["binary_sha256"],
            "--native-archive-sha256",
            "sha256:" + pin["archive_sha256"],
            "--native-version-line",
            pin["native_version_line"],
            "--source-tree-digest",
            pin["tree_digest"],
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--native-binary-sha256", result.stderr)
        self.assertFalse((output / "manifest.json").exists())

    def test_manual_containment_manifest_copies_the_witness_boundary_flag(self) -> None:
        for index, kind in enumerate(("codex", "claude_code")):
            with self.subTest(kind=kind):
                source = self.private_dir(f"boundary-source-{index}")
                receipts = self.private_dir(f"boundary-receipts-{index}")
                output = self.private_dir(f"boundary-output-{index}")
                self.populate_containment(source, kind, native_open_reached=False)
                result = self.run_containment_gate(kind, source, receipts, output)
                self.assertEqual(result.returncode, 0, result.stderr)
                manifest = json.loads((output / "manifest.json").read_text())
                self.assertEqual(manifest["containment"], {"native_open_reached": False})

    def test_manual_containment_replay_witness_must_bind_the_pinned_kind(self) -> None:
        for index, (kind, mutation) in enumerate(
            (
                ("codex", {"harness": "opencode"}),
                ("claude_code", {"native_version_line": "0.0.0"}),
            )
        ):
            with self.subTest(kind=kind, mutation=mutation):
                pin = CONTAINMENT_PINS[kind]
                source = self.private_dir(f"replay-source-{index}")
                receipts = self.private_dir(f"replay-receipts-{index}")
                output = self.private_dir(f"replay-output-{index}")
                self.populate_containment(source, kind)
                replay_path = source / pin["replay"]
                replay = json.loads(replay_path.read_text())
                replay.update(mutation)
                self.write_json(replay_path, replay)
                result = self.run_containment_gate(kind, source, receipts, output)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("containment replay witness", result.stderr)
                self.assertFalse((output / "manifest.json").exists())

    def test_manual_containment_witness_mutations_refuse_publication(self) -> None:
        base = containment_witness("codex")
        mutations = (
            (
                "native_binary",
                base | {"native_binary": "/usr/local/bin/not-the-pinned-codex"},
                "native_binary does not match --native-binary",
            ),
            (
                "native_version_line",
                base | {"native_version_line": "0.0.0"},
                "native_version_line does not match --native-version-line",
            ),
            (
                "subject.tree_digest",
                base | {"subject": {"tree_digest": "sha256:" + "f" * 64}},
                "subject.tree_digest does not match --source-tree-digest",
            ),
            (
                "containment absent",
                {key: value for key, value in base.items() if key != "containment"},
                "does not record a boolean containment.native_open_reached",
            ),
            (
                "containment empty",
                base | {"containment": {}},
                "does not record a boolean containment.native_open_reached",
            ),
            (
                "containment non-boolean",
                base | {"containment": {"native_open_reached": "true"}},
                "does not record a boolean containment.native_open_reached",
            ),
        )
        for index, (name, document, expected) in enumerate(mutations):
            with self.subTest(mutation=name):
                source = self.private_dir(f"witness-mutation-source-{index}")
                receipts = self.private_dir(f"witness-mutation-receipts-{index}")
                output = self.private_dir(f"witness-mutation-output-{index}")
                self.populate_containment(source, "codex")
                self.write_json(source / CONTAINMENT_PINS["codex"]["witness"], document)
                result = self.run_containment_gate("codex", source, receipts, output)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)
                self.assertFalse((output / "manifest.json").exists())

    def test_manual_pi_durability_export_records_pinned_native_binary(self) -> None:
        source = self.private_dir("pi-source")
        receipts = self.private_dir("pi-receipts")
        output = self.private_dir("pi-output")
        self.populate_pi_durability(source)
        result = self.run_pi_durability_gate(source, receipts, output)
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["case_set"], PI_PINS["case_set"])
        self.assertEqual(manifest["status"], "passed")
        self.assertNotIn("containment", manifest)
        self.assertEqual(
            manifest["native_binary"],
            {
                "kind": "pi",
                "path": PI_PINS["binary"],
                "sha256": "sha256:" + PI_PINS["binary_sha256"],
                "archive_sha256": "sha256:" + PI_PINS["archive_sha256"],
                "version": PI_PINS["version_line"],
            },
        )
        self.assertEqual(manifest["source_tree_digest"], PI_PINS["tree_digest"])
        self.assertEqual({entry["file"] for entry in manifest["files"]}, set(PI_PINS["exported"]))

    def test_manual_pi_durability_digest_or_binding_mismatch_refuses_publication(self) -> None:
        mutations = (
            (
                "argument digest",
                pi_durability_witness(),
                "sha256:" + "0" * 64,
                "does not bind the staged pi executable",
            ),
            (
                "witness digest",
                pi_durability_witness() | {"native_binary_sha256": "sha256:" + "1" * 64},
                None,
                "does not bind the staged pi executable",
            ),
            (
                "witness harness",
                pi_durability_witness() | {"harness": "opencode"},
                None,
                "does not bind the staged pi executable",
            ),
            (
                "witness binary",
                {key: value for key, value in pi_durability_witness().items() if key != "native_binary"},
                None,
                "does not bind the staged pi executable",
            ),
            (
                "source tree digest",
                pi_durability_witness() | {"subject": {"tree_digest": "sha256:" + "f" * 64}},
                None,
                "does not bind --source-tree-digest",
            ),
        )
        for index, (name, witness, binary_sha256, expected) in enumerate(mutations):
            with self.subTest(mutation=name):
                source = self.private_dir(f"pi-mutation-source-{index}")
                receipts = self.private_dir(f"pi-mutation-receipts-{index}")
                output = self.private_dir(f"pi-mutation-output-{index}")
                self.populate_pi_durability(source, witness)
                result = self.run_pi_durability_gate(source, receipts, output, binary_sha256=binary_sha256)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)
                self.assertFalse((output / "manifest.json").exists())

    def test_manual_pi_kind_refuses_non_retargeted_case_sets(self) -> None:
        source = self.private_dir("pi-kind-source")
        receipts = self.private_dir("pi-kind-receipts")
        output = self.private_dir("pi-kind-output")
        result = self.run_pi_durability_gate(source, receipts, output, case_set="codex_containment")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("retargeted durability case set", result.stderr)
        self.assertEqual(list(output.iterdir()), [])

    def test_manual_pi_cleanup_witness_binds_the_vehicle_runtime_kind(self) -> None:
        for index, runtime_kind in enumerate(("pi", "opencode")):
            with self.subTest(runtime_kind=runtime_kind):
                source = self.private_dir(f"pi-cleanup-source-{index}")
                receipts = self.private_dir(f"pi-cleanup-receipts-{index}")
                output = self.private_dir(f"pi-cleanup-output-{index}")
                self.write_json(
                    source / "synthetic-goal-cleanup/TestProductionLinuxV2CloneCleanupAuthenticatedScratch.json",
                    pi_cleanup_witness(runtime_kind),
                )
                result = self.run_pi_durability_gate(source, receipts, output, case_set="synthetic_goal_cleanup")
                if runtime_kind == "pi":
                    self.assertEqual(result.returncode, 0, result.stderr)
                    manifest = json.loads((output / "manifest.json").read_text())
                    self.assertEqual(manifest["case_set"], "synthetic_goal_cleanup")
                    self.assertEqual(manifest["native_binary"]["kind"], "pi")
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("pi vehicle runtime_kind", result.stderr)
                    self.assertFalse((output / "manifest.json").exists())

    def test_manual_native_arguments_require_all_five_and_matching_case_set(self) -> None:
        pin = CONTAINMENT_PINS["codex"]
        source = self.private_dir("native-args-source")
        receipts = self.private_dir("native-args-receipts")
        complete = (
            "--native-kind",
            "codex",
            "--native-binary",
            pin["binary"],
            "--native-binary-sha256",
            "sha256:" + pin["binary_sha256"],
            "--native-archive-sha256",
            "sha256:" + pin["archive_sha256"],
            "--native-version-line",
            pin["native_version_line"],
        )
        cases = (
            ("codex_containment", ("--native-kind", "codex"), "requires --native-kind"),
            ("claude_containment", complete, "requires --case-set codex_containment"),
            ("claude_containment", ("--native-kind", "opencode", *complete[2:]), "not a containment-capable"),
            ("codex_containment", complete, "requires --source-tree-digest"),
            (
                "codex_containment",
                (*complete, "--source-tree-digest", "sha256:short"),
                "must be a sha256:<64 hex> tree digest",
            ),
        )
        for index, (case_set, native_arguments, expected) in enumerate(cases):
            with self.subTest(case_set=case_set, native_arguments=native_arguments):
                output = self.private_dir(f"native-args-output-{index}")
                result = citest.run_gate(
                    SCRIPT,
                    "manual",
                    str(output),
                    "1" * 40,
                    "passed",
                    "--witness-root",
                    str(source),
                    "--receipt-root",
                    str(receipts),
                    "--case-set",
                    case_set,
                    *native_arguments,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)
                self.assertEqual(list(output.iterdir()), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
