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
ACCEPTANCE_SCRIPT = pathlib.Path(__file__).with_name("linux-v2-native-acceptance.sh")
NATIVE_WITNESS_SOURCE = (
    pathlib.Path(__file__).resolve().parents[2] / "daemon" / "e2e" / "production_linux_v2_native_witness_test.go"
)
REPORT_TAIL_BUDGET = re.compile(r"linuxV2OwnerWitnessStderrLogLines\s*=\s*(\d+)")
WITNESS_OUTPUT_WINDOW = 10
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


CREDENTIAL_GATED_LEGS = (
    (
        "daemon_sigkill_helper_survives_detached_child",
        "TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild",
        "credential_gated_daemon_sigkill_helper_survives_detached_child",
        "pi-daemon-sigkill-helper-survives.json",
    ),
    (
        "helper_sigkill_detached_child",
        "TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild",
        "credential_gated_helper_sigkill_detached_child",
        "pi-helper-sigkill-detached-child.json",
    ),
    (
        "pi_provider_action",
        "TestProductionLinuxV2PiNativeProviderAction",
        "credential_gated_pi_provider_action",
        "pi-native-provider-action.json",
    ),
)
CREDENTIAL_GATED_TARGETS = frozenset(leg[3] for leg in CREDENTIAL_GATED_LEGS)


def credential_gated_declaration() -> dict:
    return {
        "version": 1,
        "mode": "placeholder_provider_credential",
        "credential_environment_variable": "SYMMETRY_OPENAI_API_KEY",
        "harness": "pi",
        "source_tree_digest": PI_PINS["tree_digest"],
        "legs": [
            {"case": case, "test": test, "outcome": outcome, "test_result": result}
            for (case, test, outcome, _), result in zip(CREDENTIAL_GATED_LEGS, ("not_run", "not_run", "failed"))
        ],
    }


def write_declaration(path: pathlib.Path, document: dict, mode: int = 0o600) -> None:
    citest.write_json(path, document)
    os.chmod(path, mode)


def parse_case_arms(lines: list[str]) -> dict[str, str]:
    arms: dict[str, str] = {}
    pattern = None
    current: list[str] = []
    for line in lines:
        stripped = line.strip()
        if pattern is None:
            if stripped == "esac":
                break
            if not re.fullmatch(r"[A-Za-z0-9_|*]+\)", stripped):
                continue
            pattern, _, tail = stripped.partition(")")
            pattern, current = pattern.strip(), [tail.strip()]
        else:
            current.append(stripped)
        if current[-1].endswith(";;"):
            arms[pattern] = "\n".join(current)
            pattern = None
    return arms


def bash_case_arms(text: str, function: str, header: str) -> dict[str, str]:
    lines = text[text.index(f"{function}() {{") :].splitlines()
    index = next(i for i, line in enumerate(lines) if line.strip() == header)
    return parse_case_arms(lines[index + 1 :])


def acceptance_dispatch_arms(text: str) -> dict[str, str]:
    lines = text.splitlines()
    index = max(i for i, line in enumerate(lines) if line.strip() == 'case "$fixture_case_set" in')
    return parse_case_arms(lines[index + 1 :])


def acceptance_case_set_allowlist(source: str) -> tuple[str, ...]:
    listed = [
        line.strip()
        for line in source.splitlines()
        if line.strip().startswith("shared|codex_containment|")
    ]
    if len(listed) != 1:
        return ()
    return tuple(listed[0].removesuffix(") ;;").split("|"))


def acceptance_case_set_failures(text: str, exporter) -> list[str]:
    case_sets = set(acceptance_case_set_allowlist(text))
    if not case_sets:
        return ["the acceptance script must carry exactly one case-set allowlist line"]
    failures = []
    if case_sets != set(exporter.PI_DURABILITY_CASE_SETS) | {"codex_containment", "claude_containment"}:
        failures.append("the script allowlist and the exporter pi-durability sets diverged")
    if case_sets != set(exporter.MANUAL_CASE_SET_FILES):
        failures.append("the script allowlist and the exporter case sets diverged")
    targets = [target for allowlist in exporter.MANUAL_CASE_SET_FILES.values() for target in allowlist]
    if len(targets) != len(set(targets)):
        failures.append("case-set witness allowlists are not disjoint")
    arms = acceptance_dispatch_arms(text)
    if {name for pattern in arms for name in pattern.split("|")} != case_sets:
        failures.append("the dispatch arms and the case-set allowlist diverged")
    # The mount refusal retains a claim-only RunJournal no product path retires; its leg must
    # stay in its own case set (own guest and Store), never in shared.
    if [
        pattern for pattern, body in arms.items() if "run_alternate_mount_refusal_witness" in body
    ] != ["alternate_mount_refusal"]:
        failures.append("the mount-refusal leg must run only in its own case set")
    return failures


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

    def test_manual_containment_refuses_the_owner_witness_stderr_capture(self) -> None:
        source = self.private_dir("stderr-capture-source")
        receipts = self.private_dir("stderr-capture-receipts")
        output = self.private_dir("stderr-capture-output")
        self.populate_containment(source, "codex")
        self.write_log(source / "owner-witness.stderr", [b"the refused owner witness printed a diagnostic"])
        result = self.run_containment_gate("codex", source, receipts, output)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("owner-witness.stderr", result.stderr)
        self.assertFalse((output / "manifest.json").exists())
        self.assertEqual(list(output.iterdir()), [])

    def test_excluded_capture_in_a_computed_source_path_refuses(self) -> None:
        exporter = load_exporter()
        captured = pathlib.Path("stage-replay") / "owner-witness.stderr"
        with self.assertRaises(SystemExit) as refusal:
            exporter.require_no_excluded_witness_file((), (captured,))
        self.assertIn("owner-witness.stderr", str(refusal.exception))

    def test_compose_refuses_the_owner_witness_stderr_capture(self) -> None:
        source = self.private_dir("compose-capture-source")
        output = self.private_dir("compose-capture-output")
        self.write_log(source / "owner-witness.stderr", [b"the refused owner witness printed a diagnostic"])
        result = citest.run_gate(SCRIPT, "compose", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("owner-witness.stderr", result.stderr)
        self.assertFalse((output / "manifest.json").exists())
        self.assertEqual(list(output.iterdir()), [])

    def test_legacy_refuses_the_owner_witness_stderr_capture(self) -> None:
        source = self.private_dir("legacy-capture-source")
        output = self.private_dir("legacy-capture-output")
        self.write_log(source / "owner-witness.stderr", [b"the refused owner witness printed a diagnostic"])
        result = citest.run_gate(SCRIPT, "legacy", str(output), "1" * 40, "passed", "--witness-root", str(source))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("owner-witness.stderr", result.stderr)
        self.assertFalse((output / "manifest.json").exists())
        self.assertEqual(list(output.iterdir()), [])

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

    def test_manual_pi_durability_shared_requires_every_pi_witness(self) -> None:
        source = self.private_dir("pi-shared-source")
        receipts = self.private_dir("pi-shared-receipts")
        output = self.private_dir("pi-shared-output")
        result = self.run_pi_durability_gate(source, receipts, output, case_set="shared")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("pi durability witness is unavailable: ack-loss-replay-checkout_ready.json", result.stderr)
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

    def test_pi_durability_checks_every_allowlisted_witness_except_the_named_non_pi_documents(self) -> None:
        exporter = load_exporter()
        receipt_targets = {target for _, target in exporter.MANUAL_RECEIPTS}
        self.assertEqual(
            exporter.PI_VEHICLE_INDEPENDENT_WITNESSES,
            receipt_targets | {"opencode-native-health.json", "unprovisioned-owner-refusal.json"},
        )
        witness_targets = (
            {target for _, target in exporter.MANUAL_WITNESSES}
            | {f"stage-crash-replay-{stage}.json" for stage in exporter.MANUAL_STAGE_HOLDS}
            | {f"ack-loss-replay-{stage}.json" for stage in exporter.MANUAL_ACK_LOSS_STAGES}
        )
        shared_allowlist = exporter.MANUAL_CASE_SET_FILES["shared"]
        self.assertEqual(
            (witness_targets & shared_allowlist) - (shared_allowlist - exporter.PI_VEHICLE_INDEPENDENT_WITNESSES),
            {"opencode-native-health.json", "unprovisioned-owner-refusal.json"},
        )
        for case_set in exporter.PI_DURABILITY_CASE_SETS:
            checked = exporter.MANUAL_CASE_SET_FILES[case_set] - exporter.PI_VEHICLE_INDEPENDENT_WITNESSES
            self.assertTrue(checked, case_set)
            if case_set != "shared":
                self.assertEqual(checked, exporter.MANUAL_CASE_SET_FILES[case_set], case_set)

    def test_acceptance_case_set_lists_match_the_exporter_case_sets_and_dispatch(self) -> None:
        self.assertEqual(
            acceptance_case_set_failures(ACCEPTANCE_SCRIPT.read_text(encoding="utf-8"), load_exporter()),
            [],
        )
        arms = bash_case_arms(
            ACCEPTANCE_SCRIPT.read_text(encoding="utf-8"), "export_native_evidence", 'case "$fixture_case_set" in'
        )
        self.assertEqual(set(arms), {"codex_containment", "claude_containment", "*"})
        self.assertIn("native_kind=pi", arms["*"])

    def test_mutations_dropping_the_mount_case_set_or_rejoining_shared_redden_the_case_set_pins(self) -> None:
        exporter = load_exporter()
        text = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        dropped = text.replace("migration|alternate_mount_refusal|", "migration|", 1)
        self.assertNotEqual(dropped, text, "the mutation must drop the mount set from the allowlist")
        self.assertTrue(acceptance_case_set_failures(dropped, exporter))
        rejoined = text.replace(
            "  shared)\n",
            '  shared)\n    run_alternate_mount_refusal_witness "$v2_alternate_mount_events"\n',
            1,
        )
        self.assertNotEqual(rejoined, text, "the mutation must return the mount leg to the shared arm")
        self.assertTrue(acceptance_case_set_failures(rejoined, exporter))

    def credential_gated_source(self) -> tuple[pathlib.Path, pathlib.Path]:
        exporter = load_exporter()
        source = self.private_dir("credential-gated-witness")
        receipts = self.private_dir("credential-gated-receipts")
        shared = exporter.MANUAL_CASE_SET_FILES["shared"]
        for name, target in exporter.MANUAL_WITNESSES:
            if target in shared and target not in CREDENTIAL_GATED_TARGETS:
                self.write_json(source / name, pi_durability_witness())
        for stage in exporter.MANUAL_STAGE_HOLDS:
            self.write_json(
                source / "stage-replay" / f"TestProductionLinuxV2StageCrashReplayMatrix_{stage}.json",
                pi_durability_witness(),
            )
        for stage in exporter.MANUAL_ACK_LOSS_STAGES:
            self.write_json(
                source / "ack-loss-replay" / f"TestProductionLinuxV2AckLossReplayMatrix_{stage}.json",
                pi_durability_witness(),
            )
        self.write_json(
            source / "pi_provider_action" / "TestProductionLinuxV2PiNativeProviderAction.json",
            {
                "version": 1,
                "case": "TestProductionLinuxV2PiNativeProviderAction",
                "harness": "pi",
                "provider_action_succeeded": False,
                "subject": {"tree_digest": PI_PINS["tree_digest"]},
            },
        )
        return source, receipts

    def run_credential_gated_gate(
        self,
        source: pathlib.Path,
        receipts: pathlib.Path,
        output: pathlib.Path,
        *,
        declaration: pathlib.Path | None = None,
        run_status: str = "failed",
        case_set: str = "shared",
        native_pin: bool = True,
    ) -> subprocess.CompletedProcess:
        arguments = [
            "manual",
            str(output),
            "1" * 40,
            run_status,
            "--witness-root",
            str(source),
            "--receipt-root",
            str(receipts),
            "--case-set",
            case_set,
        ]
        if native_pin:
            arguments += [
                "--native-kind", "pi",
                "--native-binary", PI_PINS["binary"],
                "--native-binary-sha256", "sha256:" + PI_PINS["binary_sha256"],
                "--native-archive-sha256", "sha256:" + PI_PINS["archive_sha256"],
                "--native-version-line", PI_PINS["version_line"],
                "--source-tree-digest", PI_PINS["tree_digest"],
            ]
        if declaration is not None:
            arguments += ["--credential-gated-declaration", str(declaration)]
        return citest.run_gate(SCRIPT, *arguments)

    def test_credential_gated_declaration_publishes_named_unproven_legs(self) -> None:
        source, receipts = self.credential_gated_source()
        declaration = self.root / "credential-gated-legs.json"
        write_declaration(declaration, credential_gated_declaration())
        without = self.run_credential_gated_gate(source, receipts, self.private_dir("gated-without"))
        self.assertNotEqual(without.returncode, 0)
        self.assertIn(
            "pi durability witness is unavailable: pi-daemon-sigkill-helper-survives.json", without.stderr
        )
        output = self.private_dir("gated-output")
        result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((output / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["status"], "failed")
        self.assertEqual(
            manifest["credential_gated"],
            {
                "mode": "placeholder_provider_credential",
                "declaration": "credential-gated-legs.json",
                "legs": [case for case, _, _, _ in CREDENTIAL_GATED_LEGS],
            },
        )
        self.assertEqual(
            sorted(manifest["missing_allowlisted_files"]),
            [
                "control-provider-action-daemon-crash.json",
                "control-provider-action-helper-crash.json",
                "control-provider-action-pi.json",
                "pi-daemon-sigkill-helper-survives.json",
                "pi-helper-sigkill-detached-child.json",
            ],
        )
        self.assertEqual(
            json.loads((output / "credential-gated-legs.json").read_text(encoding="utf-8")),
            credential_gated_declaration(),
        )
        self.assertIn("credential-gated-legs.json", {entry["file"] for entry in manifest["files"]})
        self.assertTrue((output / "pi-native-provider-action.json").exists())

    def test_partial_export_keeps_the_declaration_without_waiving_durability(self) -> None:
        source, receipts = self.credential_gated_source()
        (source / "ack-loss-replay" / "TestProductionLinuxV2AckLossReplayMatrix_checkout_ready.json").unlink()
        (source / "pi_provider_action" / "TestProductionLinuxV2PiNativeProviderAction.json").unlink()
        output = self.private_dir("partial-output")
        declaration = self.root / "credential-gated-legs.json"
        write_declaration(declaration, credential_gated_declaration())
        result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("pi durability witness is unavailable: ack-loss-replay-checkout_ready.json", result.stderr)
        self.assertFalse((output / "manifest.json").exists())
        self.assertEqual((output / declaration.name).read_bytes(), declaration.read_bytes())

    def test_credential_gated_declaration_refuses_wrong_source(self) -> None:
        source, receipts = self.credential_gated_source()
        declaration = self.root / "wrong-source.json"
        write_declaration(declaration, credential_gated_declaration())
        output = self.private_dir("missing-native-pin")
        result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration, native_pin=False)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("requires --native-kind pi", result.stderr)
        self.assertFalse((output / "credential-gated-legs.json").exists())
        write_declaration(declaration, {**credential_gated_declaration(), "source_tree_digest": "sha256:" + "e" * 64})
        output = self.private_dir("wrong-source")
        result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("credential-gated declaration does not bind --source-tree-digest", result.stderr)
        self.assertFalse((output / "credential-gated-legs.json").exists())

    def test_credential_gated_declaration_refuses_misbound_witnesses(self) -> None:
        source, receipts = self.credential_gated_source()
        declaration = self.root / "credential-gated-legs.json"
        write_declaration(declaration, credential_gated_declaration())
        provider = source / "pi_provider_action" / "TestProductionLinuxV2PiNativeProviderAction.json"
        failure = json.loads(provider.read_text(encoding="utf-8"))
        for index, (field, value) in enumerate((
            ("harness", "wrong"),
            ("case", "TestOther"),
            ("subject", {"tree_digest": "sha256:" + "e" * 64}),
            ("subject", None),
            ("provider_action_succeeded", True),
            ("provider_action_succeeded", 0),
        )):
            with self.subTest(field=field, value=value):
                self.write_json(provider, {**failure, field: value})
                output = self.private_dir(f"misbound-{index}")
                result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("does not bind the declared pi provider failure", result.stderr)
                self.assertFalse((output / "pi-native-provider-action.json").exists())
                self.assertFalse((output / "manifest.json").exists())
        self.write_json(provider, failure)
        for case, test, _, target in CREDENTIAL_GATED_LEGS:
            if case == "pi_provider_action":
                continue
            with self.subTest(case=case):
                crash = source / case / (test + ".json")
                self.write_json(crash, {**failure, "case": test})
                output = self.private_dir(case)
                result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("was not run in the credential-gated case set", result.stderr)
                self.assertFalse((output / target).exists())
                self.assertFalse((output / "manifest.json").exists())
                crash.unlink()

    def test_credential_gated_export_refuses_positive_owner_outcomes(self) -> None:
        source, receipts = self.credential_gated_source()
        declaration = self.root / "credential-gated-legs.json"
        write_declaration(declaration, credential_gated_declaration())
        provider = source / "pi_provider_action" / "TestProductionLinuxV2PiNativeProviderAction.json"
        failure = json.loads(provider.read_text(encoding="utf-8"))
        for index, positive in enumerate((
            {"owner_witness_outcome": "completed"},
            {"owner_witness_response": {"outcome": "completed"}},
            {"owner_witness_response": {"provider_action": {"outcome": "succeeded"}}},
        )):
            with self.subTest(positive=positive):
                self.write_json(provider, {**failure, **positive})
                output = self.private_dir(f"positive-{index}")
                result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("pi-native-provider-action.json contradicts the declared pi provider failure", result.stderr)
                self.assertFalse((output / "pi-native-provider-action.json").exists())
                self.assertFalse((output / "manifest.json").exists())

    def test_credential_gated_export_refuses_source_provider_receipts(self) -> None:
        source, receipts = self.credential_gated_source()
        declaration = self.root / "credential-gated-legs.json"
        write_declaration(declaration, credential_gated_declaration())
        for name, target in load_exporter().MANUAL_RECEIPTS:
            with self.subTest(target=target):
                receipt = receipts / name
                self.write_json(receipt, {"outcome": "succeeded", "run_id": "stale-example"})
                output = self.private_dir(f"source-{target}")
                result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(f"Control provider receipt was not produced by the credential-gated case set: {target}", result.stderr)
                self.assertFalse((output / target).exists())
                self.assertFalse((output / "manifest.json").exists())
                receipt.unlink()

    def test_credential_gated_export_refuses_prefilled_receipt_and_witness_targets(self) -> None:
        source, receipts = self.credential_gated_source()
        (source / "pi_provider_action" / "TestProductionLinuxV2PiNativeProviderAction.json").unlink()
        declaration = self.root / "credential-gated-legs.json"
        write_declaration(declaration, credential_gated_declaration())
        receipt_targets = {target for _, target in load_exporter().MANUAL_RECEIPTS}
        for target in sorted(receipt_targets | CREDENTIAL_GATED_TARGETS):
            with self.subTest(target=target):
                output = self.private_dir(f"prefilled-{target}")
                self.write_json(output / target, {"outcome": "succeeded", "run_id": "stale-example"})
                result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(f"credential-gated evidence destination already exists: {target}", result.stderr)
                self.assertFalse((output / "manifest.json").exists())

    def test_credential_gated_declaration_refuses_a_passed_run_or_another_case_set(self) -> None:
        source, receipts = self.credential_gated_source()
        declaration = self.root / "refusal-declaration.json"
        write_declaration(declaration, credential_gated_declaration())
        for name, options, expected in (
            ("passed status", {"run_status": "passed"}, "can never be exported as a passed run"),
            ("other case set", {"case_set": "stale_leaf"}, "only defined for the manual shared case set"),
        ):
            with self.subTest(name=name):
                output = self.private_dir(f"refusal-{name.split()[0]}")
                result = self.run_credential_gated_gate(
                    source, receipts, output, declaration=declaration, **options
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)
                self.assertFalse((output / "manifest.json").exists())

    def test_credential_gated_declaration_shape_refusals(self) -> None:
        source, receipts = self.credential_gated_source()

        def missing_leg(document: dict) -> None:
            document["legs"] = document["legs"][:-1]

        def wrong_outcome(document: dict) -> None:
            document["legs"][0]["outcome"] = "credential_gated_wrong_case"

        def wrong_test(document: dict) -> None:
            document["legs"][0]["test"] = "TestSomethingElse"

        def skipped_pi(document: dict) -> None:
            document["legs"][2]["test_result"] = "not_run"

        def failed_crash(document: dict) -> None:
            document["legs"][0]["test_result"] = "failed"

        def missing_test_result(document: dict) -> None:
            del document["legs"][2]["test_result"]

        def extra_leg_field(document: dict) -> None:
            document["legs"][0]["note"] = "extra"

        def duplicate_leg(document: dict) -> None:
            document["legs"][1] = dict(document["legs"][0])

        def wrong_version(document: dict) -> None:
            document["version"] = 2

        def wrong_mode(document: dict) -> None:
            document["mode"] = "credentialed"

        def wrong_variable(document: dict) -> None:
            document["credential_environment_variable"] = "OPENAI_API_KEY"

        def extra_field(document: dict) -> None:
            document["extra"] = 1

        rows = (
            ("missing leg", missing_leg, "must name exactly the pi authentic-provider legs"),
            ("wrong outcome", wrong_outcome, "do not match the exact pi authentic-provider legs"),
            ("wrong test", wrong_test, "do not match the exact pi authentic-provider legs"),
            ("unrun provider action", skipped_pi, "does not bind the exact failed and unrun tests"),
            ("failed crash leg", failed_crash, "does not bind the exact failed and unrun tests"),
            ("missing test result", missing_test_result, "leg has unexpected or missing fields"),
            ("extra leg field", extra_leg_field, "leg has unexpected or missing fields"),
            ("duplicate leg", duplicate_leg, "do not match the exact pi authentic-provider legs"),
            ("wrong version", wrong_version, "does not match its fixed shape"),
            ("bool version", lambda doc: doc.update(version=True), "does not match its fixed shape"),
            ("float version", lambda doc: doc.update(version=1.0), "does not match its fixed shape"),
            ("wrong harness", lambda doc: doc.update(harness="opencode"), "must bind the pi harness"),
            ("wrong mode", wrong_mode, "does not match its fixed shape"),
            ("wrong variable", wrong_variable, "does not match its fixed shape"),
            ("extra field", extra_field, "unexpected or missing fields"),
        )
        for index, (name, mutate, expected) in enumerate(rows):
            with self.subTest(name=name):
                document = credential_gated_declaration()
                mutate(document)
                declaration = self.root / f"shape-{index}.json"
                write_declaration(declaration, document)
                output = self.private_dir(f"shape-output-{index}")
                result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)
                self.assertFalse((output / "manifest.json").exists())
        with self.subTest(name="missing declaration file"):
            output = self.private_dir("shape-missing")
            result = self.run_credential_gated_gate(
                source, receipts, output, declaration=self.root / "absent.json"
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("credential-gated declaration is unavailable", result.stderr)

    def test_credential_gated_declaration_refuses_non_private_mode(self) -> None:
        source, receipts = self.credential_gated_source()
        declaration = self.root / "shape-public.json"
        write_declaration(declaration, credential_gated_declaration(), mode=0o644)
        output = self.private_dir("shape-public-output")
        result = self.run_credential_gated_gate(source, receipts, output, declaration=declaration)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("allowlisted source must be a bounded regular file", result.stderr)

    def test_credential_gated_helpers_match_the_exporter_constants(self) -> None:
        exporter = load_exporter()
        self.assertEqual(exporter.CREDENTIAL_GATED_LEGS, CREDENTIAL_GATED_LEGS)
        self.assertEqual(exporter.CREDENTIAL_GATED_DECLARATION_TARGET, "credential-gated-legs.json")
        self.assertEqual(exporter.CREDENTIAL_GATED_MODE, "placeholder_provider_credential")
        self.assertEqual(exporter.CREDENTIAL_GATED_CREDENTIAL_ENVIRONMENT_VARIABLE, "SYMMETRY_OPENAI_API_KEY")
        self.assertEqual(
            CREDENTIAL_GATED_TARGETS & {target for _, target in exporter.MANUAL_WITNESSES},
            CREDENTIAL_GATED_TARGETS,
        )
        self.assertTrue(CREDENTIAL_GATED_TARGETS <= exporter.MANUAL_CASE_SET_FILES["shared"])
        self.assertEqual(
            exporter.validate_credential_gated_declaration(credential_gated_declaration(), PI_PINS["tree_digest"]),
            tuple(case for case, _, _, _ in CREDENTIAL_GATED_LEGS),
        )

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
            ("claude_containment", ("--native-kind", "opencode", *complete[2:]), "not a supported native kind"),
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


def extract_witness_output_printer() -> str:
    source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
    start = source.index("print_witness_output_lines() {\n")
    end = source.index("\n}\n", start)
    return source[start : end + len("\n}\n")]


def synthetic_cleanup_blocks(source: str) -> tuple[str, str, str]:
    fail_start = source.index("\nfail() {\n")
    fail_end = source.index("\n}\n", fail_start) + len("\n}\n")
    setup_start = source.index('sudo install -d -o root -g root -m 0755 "$rootfs$OWNER_TEST_ROOT"')
    setup_end = source.index('sudo install -d -o root -g "$A_GID" -m 2730', setup_start)
    config = source.index('    "$fixture_root/synthetic-goal-cleanup.json" \\\n')
    guard_start = source.rindex("\ndone\n", 0, config) + len("\ndone\n")
    guard_end = source.index('for hold_after in "${OWNER_STAGE_REPLAY_HOLDS[@]}"', config)
    return source[fail_start:fail_end], source[setup_start:setup_end], source[guard_start:guard_end]


class SyntheticCleanupGuardTest(unittest.TestCase):
    def test_scoped_grant_and_permission_refusals(self) -> None:
        fail, setup, guard = synthetic_cleanup_blocks(ACCEPTANCE_SCRIPT.read_text(encoding="utf-8"))
        harness = r'''
set -Eeuo pipefail
fixture_case_set=$1
shape=$2
A_USER=symmetry-a A_UID=52000 A_GID=52000
B_USER=symmetry-b B_UID=52001 B_GID=52001
rootfs=/rootfs fixture_root=/fixture
OWNER_TEST_ROOT=/owner PRODUCTION_ROOT=/owner/production
SYNTHETIC_GOAL_CLEANUP_ROOT=/owner/synthetic
SYNTHETIC_GOAL_CLEANUP_SOURCE=/owner/synthetic/source
SYNTHETIC_GOAL_CLEANUP_SEEDS=/owner/synthetic/seeds
SYNTHETIC_GOAL_CLEANUP_CLONES=/owner/synthetic/clones
sudo() {
  case "$1:${@: -1}" in
    install:*|setfacl:/rootfs/owner/synthetic/seeds) ;;
    setfacl:/rootfs/owner/synthetic)
      [[ "$2:$3" == '-m:u:52001:r-x,m::r-x' ]] || return 96
      printf 'case root granted\n'
      ;;
    setfacl:/rootfs/owner/synthetic/clones)
      [[ "$2:$3" == '-m:u:52000:r-x,m::r-x,d:u:52000:r-x,d:m::r-x' ]] || return 96
      ;;
    stat:/rootfs/owner/synthetic/config.json) printf '0:52000:640:1\n' ;;
    stat:/rootfs/owner/synthetic)
      if [[ "$shape" == mode ]]; then printf '0:52000:755\n'; else printf '0:52000:751\n'; fi
      ;;
    getfacl:/rootfs/owner/synthetic)
      printf 'user::rwx\nuser:52001:r-x\n'
      if [[ "$shape" == acl ]]; then printf 'user:52002:r-x\n'; fi
      printf 'group::r-x\nmask::r-x\nother::--x\n'
      ;;
    stat:/rootfs/owner/synthetic/clones)
      case "$shape" in
        clone-mode) printf '52001:52001:770\n' ;;
        clone-uid) printf '52002:52001:750\n' ;;
        clone-gid) printf '52001:52002:750\n' ;;
        *) printf '52001:52001:750\n' ;;
      esac
      ;;
    getfacl:/rootfs/owner/synthetic/clones)
      printf 'user::rwx\nuser:52000:r-x\ngroup::---\nmask::r-x\nother::---\ndefault:user::rwx\ndefault:user:52000:r-x\n'
      if [[ "$shape" == clone-default-reader ]]; then printf 'default:user:52002:r-x\n'; fi
      printf 'default:group::---\n'
      if [[ "$shape" == clone-acl ]]; then printf 'default:mask::rwx\n'; else printf 'default:mask::r-x\n'; fi
      printf 'default:other::---\n'
      ;;
    *) return 96 ;;
  esac
}
machine_run_as() {
  case "$*" in
    'symmetry-a symmetry-a /usr/bin/test -r /owner/synthetic/config.json') return 0 ;;
    'symmetry-b symmetry-b /usr/bin/test -r /owner/synthetic/config.json') return 1 ;;
    'symmetry-b symmetry-b /usr/bin/bash -c test -r "$1" bash /owner/synthetic') [[ "$shape" != case-read ]] ;;
    'symmetry-b symmetry-b /usr/bin/bash -c test -r "$1" bash /owner/synthetic/seeds') [[ "$shape" != seed-read ]] ;;
    'symmetry-a symmetry-a /usr/bin/bash -c ! test -w "$1" bash /owner/synthetic') [[ "$shape" != case-write ]] ;;
    *) return 97 ;;
  esac
}
'''
        clone_stat_error = "synthetic cleanup clone root must stay B-owned mode 0750"
        clone_acl_error = "synthetic cleanup clone root must keep a non-writable default ACL mask so B-created binding/run/generation directories stay non-writable"
        cases = (
            ("synthetic_goal_cleanup", "delivered", ""),
            ("shared", "case-read", ""),
            ("synthetic_goal_cleanup", "case-read", "native B cannot read the synthetic cleanup case root the clone seed path sits under"),
            ("synthetic_goal_cleanup", "seed-read", "native B cannot read the synthetic cleanup seed root"),
            ("synthetic_goal_cleanup", "case-write", "native A unexpectedly gained write access to the root-controlled synthetic cleanup case root"),
            ("synthetic_goal_cleanup", "mode", "synthetic cleanup case root must stay root:A mode 0751"),
            ("synthetic_goal_cleanup", "acl", "synthetic cleanup case root must have exactly the fixture B-read ACL"),
            ("synthetic_goal_cleanup", "clone-mode", clone_stat_error),
            ("synthetic_goal_cleanup", "clone-acl", clone_acl_error),
            ("synthetic_goal_cleanup", "clone-uid", clone_stat_error),
            ("synthetic_goal_cleanup", "clone-gid", clone_stat_error),
            ("synthetic_goal_cleanup", "clone-default-reader", clone_acl_error),
        )
        for case_set, shape, error in cases:
            with self.subTest(case_set=case_set, shape=shape):
                result = subprocess.run(
                    ["bash", "-c", harness + fail + setup + guard, "bash", case_set, shape],
                    capture_output=True, text=True, check=False,
                )
                self.assertEqual((result.returncode, result.stderr), (
                    (1, f"Linux v2 native acceptance setup failed: {error}\n") if error else (0, "")
                ))
                self.assertEqual(result.stdout, "case root granted\n" if case_set == "synthetic_goal_cleanup" else "")


def report_tail_budget() -> int:
    match = REPORT_TAIL_BUDGET.search(NATIVE_WITNESS_SOURCE.read_text(encoding="utf-8"))
    if match is None:
        raise AssertionError("the native witness report no longer declares its tail line budget")
    return int(match.group(1))


class WitnessOutputWindowTest(unittest.TestCase):
    """The report's line budget must keep its status line inside the printer's last window."""

    def printer_events(self, tail_lines: list[str]) -> list[dict]:
        name = "TestWitness"
        lines = [
            "=== RUN   " + name,
            "    production_linux_v2_native_witness_test.go:101: owner-witness command exit status: 1",
            *(f"    production_linux_v2_native_witness_test.go:101: owner-witness stderr: {line}" for line in tail_lines),
            "    production_linux_v2_native_witness_test.go:101: wait for requested production owner-witness barrier",
            "    production_linux_v2_native_witness_test.go:1838: production Linux v2 StateDir fixture cleanup",
            "--- FAIL: " + name,
        ]
        return [{"Action": "output", "Test": name, "Output": line + "\n"} for line in lines]

    def run_printer(self, events: list[dict]) -> str:
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            root.joinpath("events.jsonl").write_text(
                "".join(json.dumps(event) + "\n" for event in events), encoding="utf-8"
            )
            root.joinpath("secrets.bin").write_bytes(b"")
            root.joinpath("printer.sh").write_text(
                'set -u\nevidence_secret_values_file=$2\n'
                + extract_witness_output_printer()
                + '\nprint_witness_output_lines "$1" TestWitness\n',
                encoding="utf-8",
            )
            result = subprocess.run(
                ["bash", "printer.sh", "events.jsonl", "secrets.bin"],
                cwd=directory,
                capture_output=True,
                text=True,
                check=False,
            )
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stderr

    def test_report_budget_keeps_its_status_line_and_every_tail_line(self) -> None:
        budget = report_tail_budget()
        tail = [f"witness diagnostic line {index}" for index in range(budget)]
        printed = self.run_printer(self.printer_events(tail))
        self.assertIn("owner-witness command exit status: 1", printed)
        self.assertIn(tail[0], printed)
        self.assertIn(tail[-1], printed)
        self.assertEqual(printed.count("witness output: "), WITNESS_OUTPUT_WINDOW)

    def test_a_full_window_tail_evicts_the_status_line(self) -> None:
        tail = [f"witness diagnostic line {index}" for index in range(WITNESS_OUTPUT_WINDOW)]
        printed = self.run_printer(self.printer_events(tail))
        self.assertNotIn("owner-witness command exit status: 1", printed)
        self.assertIn(tail[-1], printed)


def extracted_bash_function(source: str, name: str) -> str:
    start = source.index(f"{name}() {{\n")
    end = source.index("\n}\n", start)
    return source[start : end + len("\n}\n")]


CREDENTIAL_GATED_FIXTURE_VARIABLES = (
    "pi_legacy_refusal_events",
    "opencode_legacy_refusal_events",
    "v2_b_cleanup_denial_events",
    "v2_cgroup_mount_cleanup_events",
    "v2_unexpected_cgroup_procs_fd_events",
    "owner_events",
    "v2_marker_reader_events",
    "v2_pi_events",
    "v2_peer_negative_pi_events",
    "v2_crash_events",
    "v2_daemon_crash_events",
    "v2_stage_replay_events",
    "v2_ack_loss_events",
    "v2_helper_exit_before_register_events",
    "v2_helper_stop_loss_events",
    "v2_helper_ack_loss_events",
    "v2_helper_launcher_loss_events",
    "v2_receipt_persist_events",
    "v2_pid_reuse_events",
    "v2_v1_replay_events",
    "v2_opencode_events",
    "v2_peer_negative_opencode_events",
)


def shared_arm_harness(source: str) -> str:
    arm = acceptance_dispatch_arms(source)["shared"]
    preamble = [
        "set -Eeuo pipefail",
        "fixture_case_set=shared",
        "call_log=$2",
        "placeholder_provider_credential=$1",
        "replay_status=$3",
        "A_USER=a",
        "OWNER_TEST_LEAF=leaf",
        "credential_gated_cases=()",
        "credential_gated_tests=()",
        extracted_bash_function(source, "owner_witness_case_identity"),
        extracted_bash_function(source, "owner_witness_case_test_name"),
        *(f"{name}={name}" for name in CREDENTIAL_GATED_FIXTURE_VARIABLES),
        'run_go_test() { printf "go:%s\\n" "$1" >> "$call_log"; }',
        'run_unexpected_cgroup_procs_fd_witness() { printf "owner-fd-census\\n" >> "$call_log"; }',
        'run_native_owner_witness() { printf "owner:%s\\n" "$1" >> "$call_log"; }',
        'run_stage_replay_cases() { printf "stage-replay\\n" >> "$call_log"; return "$replay_status"; }',
        'run_ack_loss_replay_cases() { printf "ack-loss\\n" >> "$call_log"; }',
        'run_helper_exit_after_release_before_native_register_witness() { printf "helper-exit\\n" >> "$call_log"; }',
        'run_helper_response_loss_witnesses() { printf "response-loss\\n" >> "$call_log"; }',
        'run_receipt_persist_failure_witness() { printf "receipt-persist\\n" >> "$call_log"; }',
        'run_literal_pid_reuse_witness() { printf "pid-reuse\\n" >> "$call_log"; }',
        'run_legacy_v1_replay_witness() { printf "v1-replay\\n" >> "$call_log"; }',
        'record_credential_gated_leg() { printf "record:%s %s\\n" "$1" "$2" >> "$call_log"; credential_gated_cases+=("$1"); }',
        'run_credential_gated_pi_provider_leg() { printf "gated-pi-run:%s\\n" "$1" >> "$call_log"; }',
        'stop_readback_reader() { printf "stop-reader\\n" >> "$call_log"; }',
        'machine_run_as() { printf "machine:%s\\n" "$3" >> "$call_log"; }',
        'write_credential_gated_declaration() { printf "write-declaration\\n" >> "$call_log"; }',
        'case "$fixture_case_set" in',
        "shared)",
    ]
    return "\n".join(preamble) + "\n" + arm + "\nesac\n" + source[source.rindex("\nstop_readback_reader\n") :]


CONTROL_CLAIMS_SCRIPT = ACCEPTANCE_SCRIPT.with_name("provision-linux-v2-control-claims.exs")
CONTROL_PI_PROVIDER_ACTION_DESCRIPTION = re.compile(
    r'"pi_provider_action", "pi-native", :pi, :pi_provider,\n\s*"(?P<description>[^"]*)"\},'
)
FIXTURE_HOME_INVOCATION_MARKER = 'python3 - "$fixture_root" "$A_HOME" "$A_SOURCE" \\\n'
FIXED_B_HOME = "/opt/symmetry-owner-test/fixed-b-home"
FIXED_B_HOME_TASK_TEXT = f"{FIXED_B_HOME}/.pi/agent/APPEND_SYSTEM.md"
FIXED_SCOPE_PARAGRAPH = (
    "This leg is fixed-scope: do not explore the repository or run its test suites or gates, "
    "and return the required final TaskResult JSON object as your only final answer."
)
STAGED_TASK_TEXT_PATH = "b-home/.pi/agent/APPEND_SYSTEM.md"
STAGED_SETTINGS_PATH = "b-home/.pi/agent/settings.json"
STAGED_PI_HOME_PATH = "b-home/.pi"
ACCEPTANCE_WORKFLOW = ACCEPTANCE_SCRIPT.parents[2] / ".github/workflows/linux-native-v2-acceptance.yml"


def extracted_authenticated_pi_task_block(source: str) -> str:
    start = source.index("authenticated_pi_task_text=''")
    return source[start : source.index("\nfi\n", start) + len("\nfi\n")]


def extracted_authenticated_pi_install_block(source: str) -> str:
    start = source.find('if [[ -n "$authenticated_pi_task_text" ]]; then')
    if start == -1:
        return ""
    return source[start : source.index("\nfi\n", start) + len("\nfi\n")]


def extracted_fixture_home_invocation(source: str) -> str:
    start = source.index(FIXTURE_HOME_INVOCATION_MARKER)
    return source[start : source.index("\nPY\n", start) + len("\nPY\n")]


def staging_script(source: str, root: pathlib.Path, case_set: str) -> pathlib.Path:
    api_key = root / f"{case_set}-api-key"
    api_key.write_text("sk-fixture-provider-key\n", encoding="utf-8")
    script = root / f"{case_set}-staging.sh"
    script.write_text(
        "set -Eeuo pipefail\n"
        "fixture_root=$1\n"
        f"fixture_case_set={case_set}\n"
        "fixture_identity_suffix=${fixture_case_set//_/-}\n"
        "A_HOME=/home/a\n"
        "A_SOURCE=/a/source\n"
        "B_CLONE_ROOT=/clones\n"
        "SEED_ROOT=/seeds\n"
        "DAEMON_SERVICE_STATE_DIR=/state\n"
        "PI_SHARE=/share\n"
        "SYMMETRY_PI_MODEL=pi-model\n"
        "SYMMETRY_OPENCODE_MODEL=opencode-model\n"
        "SYMMETRY_OPENAI_BASE_URL=https://provider.invalid/v1\n"
        f"native_api_key_file={api_key}\n"
        "pi_repository_id=11111111-1111-1111-1111-111111111111\n"
        "opencode_repository_id=22222222-2222-2222-2222-222222222222\n"
        "pi_durability_repository_id=33333333-3333-3333-3333-333333333333\n"
        "SYNTHETIC_GOAL_CLEANUP_SOURCE=/synthetic/source\n"
        "SYNTHETIC_GOAL_CLEANUP_SEEDS=/synthetic/seeds\n"
        "SYNTHETIC_GOAL_CLEANUP_CLONES=/synthetic/clones\n"
        + extracted_authenticated_pi_task_block(source)
        + extracted_fixture_home_invocation(source)
        + f"B_HOME={FIXED_B_HOME}\n"
        'b_native_credential_files=("$B_HOME/.pi/agent/settings.json" '
        '"$B_HOME/.pi/agent/auth.json" "$B_HOME/.pi/agent/models.json")\n'
        + extracted_authenticated_pi_install_block(source)
        + '\nprintf "%s\\n" "${b_native_credential_files[@]}"\n',
        encoding="utf-8",
    )
    return script


def staged_home_run(source: str, root: pathlib.Path, case_set: str) -> tuple[subprocess.CompletedProcess, pathlib.Path]:
    fixture_root = root / f"{case_set}-fixture"
    fixture_root.mkdir(parents=True)
    result = subprocess.run(
        ["bash", str(staging_script(source, root, case_set)), str(fixture_root)],
        capture_output=True,
        text=True,
        check=False,
    )
    return result, fixture_root


def staged_thinking_level(fixture_root: pathlib.Path) -> object:
    settings_file = fixture_root / STAGED_SETTINGS_PATH
    if not settings_file.exists():
        return None
    return json.loads(settings_file.read_text(encoding="utf-8")).get("defaultThinkingLevel")


def acceptance_containment_case_sets(source: str) -> tuple[str, ...]:
    return tuple(re.findall(r"^\s*([a-z_]+)\) native_containment_kind=", source, re.M))


def writer_containment_suffixes(source: str) -> tuple[str, ...]:
    match = re.search(r"containment_kind = \{(?P<keys>[^}]*)\}\.get\(", source)
    return () if match is None else tuple(re.findall(r'"([^"]+)":', match.group("keys")))


def workflow_case_sets(text: str) -> tuple[str, ...]:
    lines = text.splitlines()
    start = next(i for i, line in enumerate(lines) if line.strip() == "case_sets=(")
    names = []
    for line in lines[start + 1 :]:
        stripped = line.strip()
        if stripped == ")":
            break
        names.append(stripped)
    return tuple(names)


def acceptance_case_set_derivation_failures(source: str, workflow: str) -> list[str]:
    allowlist = acceptance_case_set_allowlist(source)
    if not allowlist:
        return ["the acceptance script no longer carries one case-set allowlist"]
    failures = []
    dispatched = {
        name for pattern in acceptance_dispatch_arms(source) for name in pattern.split("|")
    }
    if set(allowlist) != dispatched:
        failures.append("the case-set allowlist and the script's own dispatch arms diverged")
    if set(allowlist) != set(workflow_case_sets(workflow)):
        failures.append("the case-set allowlist and the workflow case-set matrix diverged")
    containment = {name.replace("_", "-") for name in acceptance_containment_case_sets(source)}
    if containment != set(writer_containment_suffixes(source)):
        failures.append("the containment case sets and the writer's containment kinds diverged")
    return failures


def authenticated_pi_task_failures(source: str, claims: str, root: pathlib.Path) -> list[str]:
    failures: list[str] = []
    description = CONTROL_PI_PROVIDER_ACTION_DESCRIPTION.search(claims)
    if description is None:
        failures.append("the Control claim fixture no longer carries the pi_provider_action description")
    expected = None if description is None else f"{description.group('description')}\n\n{FIXED_SCOPE_PARAGRAPH}\n"

    case_sets = acceptance_case_set_allowlist(source)
    containment_sets = acceptance_containment_case_sets(source)
    if not case_sets:
        failures.append("the acceptance script no longer carries one case-set allowlist")
    runs = {case_set: staged_home_run(source, root, case_set) for case_set in case_sets}
    for case_set in case_sets:
        result, fixture_root = runs[case_set]
        if result.returncode != 0:
            failures.append(f"the fixture staging script for {case_set} exited {result.returncode}")
            continue
        listed = FIXED_B_HOME_TASK_TEXT in result.stdout.splitlines()
        if case_set == "shared":
            staged_file = fixture_root / STAGED_TASK_TEXT_PATH
            staged = staged_file.read_text(encoding="utf-8") if staged_file.exists() else None
            if expected is not None and staged != expected:
                failures.append("the shared case set does not stage the exact authenticated Pi task text")
            level = staged_thinking_level(fixture_root)
            if level != "off":
                failures.append(f"the staged pi home no longer sets the thinking level off: {level!r}")
            if not listed:
                failures.append(f"the {case_set} case set's fixed B home install list omits the staged task text")
        elif case_set in containment_sets:
            # A containment guest writes its own kind directory and no Pi home, so its only
            # observable of a leaked task-text guard is the install list.
            if (fixture_root / STAGED_PI_HOME_PATH).exists():
                failures.append(f"the {case_set} case set is not the containment fixture: it staged a Pi home")
            if listed:
                failures.append(
                    f"the {case_set} case set's fixed B home install list stages the authenticated Pi task "
                    "text without a Pi home"
                )
        else:
            if (fixture_root / STAGED_TASK_TEXT_PATH).exists():
                failures.append(f"the {case_set} case set stages the authenticated Pi task text")
            if listed:
                failures.append(
                    f"the {case_set} case set's fixed B home install list stages the authenticated Pi task text"
                )
            level = staged_thinking_level(fixture_root)
            if level != "minimal":
                failures.append(
                    f"the {case_set} case set stages the authenticated thinking level {level!r}"
                )
    return failures


class CredentialGatedFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.temporary.name)

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def run_script(self, name: str, body: str, *arguments: str) -> subprocess.CompletedProcess:
        path = self.root / name
        path.write_text(body, encoding="utf-8")
        return subprocess.run(
            ["bash", str(path), *arguments], capture_output=True, text=True, check=False
        )

    def test_mode_detection_matches_only_the_ratified_placeholder(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        start = source.index("detect_placeholder_provider_credential() {\n")
        end = source.index('printf \'%s\' "$GH_TOKEN" > "$control_github_token_file"', start)
        body = (
            "set -Eeuo pipefail\n"
            "placeholder_provider_credential=0\n"
            'SYMMETRY_OPENAI_API_KEY=$1 native_api_key_file=$2 native_containment_kind=\n'
            + source[start:end]
            + '\n'
            'printf "%s" "$placeholder_provider_credential"\n'
        )
        for value, expected in (
            ("sk-scratch-dummy", "1"),
            ("sk-live-provider-credential-000000", "0"),
            ("sk-scratch-dummy-extra", "0"),
            ("", "0"),
        ):
            with self.subTest(value=value):
                staged_key = self.root / "staged-key"
                result = self.run_script("detect.sh", body, value, str(staged_key))
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, expected)
                self.assertEqual(staged_key.read_text(encoding="utf-8"), value)

    def test_export_passes_only_the_existing_declaration_to_the_exporter(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        body = (
            'set -Eeuo pipefail\nfixture_root=$1 rootfs=$1/rootfs evidence_directory=$1/output\n'
            'repo=$2 source_tree_digest=$3 fixture_case_set=shared GITHUB_SHA=fixture-commit\n'
            'evidence_secret_values_file=$1/scan\n'
            'sudo() { printf "%s\\n" "$@"; }\n'
            + extracted_bash_function(source, "export_native_evidence")
            + '\nexport_native_evidence 42\n'
        )
        declaration = self.root / "credential-gated-legs.json"
        for declared in (False, True):
            with self.subTest(declared=declared):
                if declared:
                    write_declaration(declaration, credential_gated_declaration())
                repo = ACCEPTANCE_SCRIPT.parents[2]
                result = self.run_script("export.sh", body, str(self.root), str(repo), PI_PINS["tree_digest"])
                self.assertEqual(result.returncode, 0, result.stderr)
                arguments = result.stdout.splitlines()
                self.assertEqual(arguments[:6], [
                    "python3", str(repo / "scripts/ci/export-linux-v2-redacted-evidence.py"),
                    "manual", str(self.root / "output"), "fixture-commit", "failed",
                ])
                self.assertEqual(arguments.count("--credential-gated-declaration"), int(declared))
                if declared:
                    self.assertEqual(arguments[arguments.index("--credential-gated-declaration") + 1], str(declaration))

    def test_declaration_helper_records_the_named_outcomes(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        body = (
            "set -Eeuo pipefail\n"
            "umask 022\n"
            "fixture_root=$1\n"
            "source_tree_digest=$2\n"
            "credential_gated_cases=()\n"
            "credential_gated_tests=()\n"
            + extracted_bash_function(source, "record_credential_gated_leg")
            + "\n"
            + extracted_bash_function(source, "write_credential_gated_declaration")
            + "\n"
            "record_credential_gated_leg pi_provider_action TestProductionLinuxV2PiNativeProviderAction\n"
            "record_credential_gated_leg daemon_sigkill_helper_survives_detached_child TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild\n"
            "record_credential_gated_leg helper_sigkill_detached_child TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild\n"
            "write_credential_gated_declaration\n"
        )
        result = self.run_script("declaration.sh", body, str(self.root), PI_PINS["tree_digest"])
        self.assertEqual(result.returncode, 0, result.stderr)
        exporter = load_exporter()
        declaration_path = self.root / "credential-gated-legs.json"
        document = json.loads(declaration_path.read_text(encoding="utf-8"))
        self.assertEqual(document, credential_gated_declaration())
        self.assertEqual(
            exporter.validate_credential_gated_declaration(document, PI_PINS["tree_digest"]),
            tuple(case for case, _, _, _ in CREDENTIAL_GATED_LEGS),
        )
        self.assertEqual(oct(declaration_path.stat().st_mode & 0o777), oct(0o600))
        for case, _, outcome, _ in CREDENTIAL_GATED_LEGS:
            self.assertIn(f"{case} ({outcome}) is NOT proven", result.stderr)

    def test_gated_go_runner_accepts_only_the_named_nonpass(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        body = (
            "set -Eeuo pipefail\n"
            "repo=$1\nmode=$2\nstatus_to_emit=$3\naction=$4\nevent_file=$5\n"
            "machine=fixture machine_run_count=0 A_HOME=a A_USER=a READBACK_CHANNEL=reader\n"
            "NATIVE_HOOK_MARKER_ROOT=markers OWNER_CONFIG_ROOT=config\n"
            "OWNER_CONTROL_RECEIPT_ROOT=receipt OWNER_CONTROL_RECEIPT_CONTEXT_ROOT=context\n"
            "OWNER_REQUEST_ROOT=requests OWNER_BARRIER_ROOT=barriers fixture_root=fixture\n"
            "credential_gated_cases=() credential_gated_tests=()\n"
            "witness=TestProductionLinuxV2PiNativeProviderAction\n"
            'fail() { printf "%s\\n" "$1" >&2; exit 1; }\n'
            "machine_run_as() { :; }\n"
            'prepare_owner_request() { printf "prepare:%s\\n" "$*"; }\n'
            'machine_run() { printf "cleanup:%s\\n" "$*"; }\n'
            "print_filtered_output_lines() { :; }\n"
            "print_witness_output_lines() { :; }\n"
            "print_witness_failure_context() { :; }\n"
            "sudo() {\n"
            '  local package=github.com/wxxb789/symmetry/daemon/e2e\n'
            '  printf \'{"Action":"%s","Package":"%s","Test":"%s"}\\n\' run "$package" "$witness" "$action" "$package" "$witness"\n'
            '  printf \'{"Action":"%s","Package":"%s"}\\n\' "$action" "$package"\n'
            '  return "$status_to_emit"\n'
            "}\n"
            + "\n".join(extracted_bash_function(source, name) for name in (
                "run_go_test", "owner_witness_case_identity", "record_credential_gated_leg",
                "run_credential_gated_pi_provider_leg",
            ))
            + '\nif [[ "$mode" == gated-leg ]]; then\n'
            '  run_credential_gated_pi_provider_leg "$event_file"\n'
            'else\n'
            '  run_go_test "$event_file" ./e2e "^${witness}$" "$mode" REQUEST request output -- "$witness"\n'
            'fi\n'
        )
        witness = "TestProductionLinuxV2PiNativeProviderAction"
        for mode, status, action, want, diagnostic in (
            ("gated-leg", "1", "fail", 0, f"credential-gated {witness}:"),
            ("gated-leg", "0", "pass", 1, "did not produce the expected placeholder-credential stop"),
            ("production", "0", "pass", 0, f"verified {witness}:"),
            ("production", "7", "pass", 7, "Required native Go witness exited with status 7"),
        ):
            with self.subTest(mode=mode, status=status, action=action):
                result = self.run_script(
                    "go-runner.sh", body, str(ACCEPTANCE_SCRIPT.parents[2]), mode, status, action,
                    str(self.root / "go-events.jsonl"),
                )
                self.assertEqual(result.returncode, want, result.stderr)
                self.assertIn(diagnostic, result.stdout + result.stderr)
                if mode == "gated-leg":
                    self.assertIn("prepare:pi_provider_action pi_provider_action", result.stdout)
                    self.assertEqual("cleanup:/usr/bin/rm -f -- requests/pi_provider_action.json" in result.stdout, want == 0)
                    self.assertEqual("(credential_gated_pi_provider_action) is NOT proven" in result.stderr, want == 0)

    def test_shared_arm_gates_only_the_provider_legs(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        for mode, replay_status in (("0", "0"), ("1", "0"), ("1", "42")):
            with self.subTest(placeholder=mode, replay_status=replay_status):
                call_log = self.root / f"calls-{mode}.txt"
                call_log.write_text("", encoding="utf-8")
                result = self.run_script(
                    "arm.sh", shared_arm_harness(source), mode, str(call_log), replay_status
                )
                calls = call_log.read_text(encoding="utf-8").splitlines()
                if mode == "0":
                    self.assertEqual([entry for entry in calls if entry.startswith("record:")], [])
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(calls[-2:], ["stop-reader", "machine:/usr/bin/rmdir"])
                    self.assertNotIn("gated-pi-run:v2_pi_events", calls)
                    self.assertNotIn("write-declaration", calls)
                    positions = [
                        calls.index(entry)
                        for entry in (
                            "owner:pi_provider_action",
                            "go:v2_peer_negative_pi_events",
                            "owner:helper_sigkill_detached_child",
                            "owner:daemon_sigkill_helper_survives_detached_child",
                            "owner:opencode_native_health",
                        )
                    ]
                    self.assertEqual(positions, sorted(positions))
                else:
                    self.assertEqual(result.returncode, int(replay_status) or 1, result.stderr)
                    self.assertNotIn("owner:pi_provider_action", calls)
                    self.assertNotIn("owner:helper_sigkill_detached_child", calls)
                    self.assertNotIn("owner:daemon_sigkill_helper_survives_detached_child", calls)
                    first = calls.index("go:v2_marker_reader_events") + 1
                    self.assertEqual(calls[first:calls.index("stage-replay") + 1], [
                        "gated-pi-run:v2_pi_events",
                        "go:v2_peer_negative_pi_events",
                        "record:helper_sigkill_detached_child TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild",
                        "record:daemon_sigkill_helper_survives_detached_child TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild",
                        "write-declaration",
                        "stage-replay",
                    ])
                    if replay_status == "0":
                        self.assertEqual(calls[-2:], ["stop-reader", "machine:/usr/bin/rmdir"])
                        self.assertIn("this case set is not passed", result.stderr)
                    else:
                        self.assertEqual(calls[-1], "stage-replay")

    def test_authenticated_pi_task_staging_rows(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        claims = CONTROL_CLAIMS_SCRIPT.read_text(encoding="utf-8")
        self.assertEqual(authenticated_pi_task_failures(source, claims, self.root), [])

    def test_case_set_coverage_is_derived_from_the_script_and_the_workflow(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        workflow = ACCEPTANCE_WORKFLOW.read_text(encoding="utf-8")
        self.assertEqual(acceptance_case_set_derivation_failures(source, workflow), [])

    def test_mutations_of_the_case_set_lists_redden_the_derivation_checks(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        workflow = ACCEPTANCE_WORKFLOW.read_text(encoding="utf-8")
        mutations = (
            (
                "allowlist-extra-set",
                source.replace(
                    "|synthetic_goal_cleanup) ;;", "|synthetic_goal_cleanup|x_probe) ;;", 1
                ),
                workflow,
                (
                    "the case-set allowlist and the script's own dispatch arms diverged",
                    "the case-set allowlist and the workflow case-set matrix diverged",
                ),
            ),
            (
                "workflow-extra-set",
                source,
                workflow.replace(
                    "            stale_journal_subject\n",
                    "            stale_journal_subject\n            x_probe\n",
                    1,
                ),
                ("the case-set allowlist and the workflow case-set matrix diverged",),
            ),
            (
                "containment-arm-dropped",
                source.replace("  codex_containment) native_containment_kind=codex ;;\n", "", 1),
                workflow,
                ("the containment case sets and the writer's containment kinds diverged",),
            ),
        )
        for name, mutant_source, mutant_workflow, expected in mutations:
            with self.subTest(mutation=name):
                self.assertNotEqual(
                    (mutant_source, mutant_workflow),
                    (source, workflow),
                    "the mutation must change an input",
                )
                self.assertEqual(
                    acceptance_case_set_derivation_failures(mutant_source, mutant_workflow),
                    list(expected),
                    name,
                )

    def test_mutations_of_the_authenticated_pi_task_staging_redden_the_rows(self) -> None:
        source = ACCEPTANCE_SCRIPT.read_text(encoding="utf-8")
        claims = CONTROL_CLAIMS_SCRIPT.read_text(encoding="utf-8")
        mutations = (
            (
                "unconditional-staging",
                source.replace(
                    'if [[ "$fixture_case_set" == shared ]]; then\n  authenticated_pi_task_text=',
                    "if true; then\n  authenticated_pi_task_text=",
                    1,
                ),
                claims,
                (
                    *(
                        f"the {case_set} case set's fixed B home install list stages the "
                        "authenticated Pi task text without a Pi home"
                        for case_set in acceptance_containment_case_sets(source)
                    ),
                    *(
                        row
                        for case_set in acceptance_case_set_allowlist(source)
                        if case_set != "shared" and case_set not in acceptance_containment_case_sets(source)
                        for row in (
                            f"the {case_set} case set stages the authenticated Pi task text",
                            f"the {case_set} case set's fixed B home install list stages the authenticated "
                            "Pi task text",
                        )
                    ),
                ),
            ),
            (
                "raised-thinking-level",
                source.replace(
                    'authenticated_pi_thinking_level = "off" if fixture_identity_suffix == "shared" else "minimal"',
                    'authenticated_pi_thinking_level = "minimal"',
                    1,
                ),
                claims,
                ("the staged pi home no longer sets the thinking level off: 'minimal'",),
            ),
            (
                "negative-writer-unconditional",
                source.replace("    if authenticated_pi_task_text:\n", "    if True:\n", 1),
                claims,
                (
                    "the stage_replay_create_started case set stages the authenticated Pi task text",
                    "the stage_replay_leaf_bound case set stages the authenticated Pi task text",
                    "the stage_replay_launcher_bound case set stages the authenticated Pi task text",
                    "the ack_loss_launcher_bound case set stages the authenticated Pi task text",
                    "the stale_leaf case set stages the authenticated Pi task text",
                    "the migration case set stages the authenticated Pi task text",
                    "the alternate_mount_refusal case set stages the authenticated Pi task text",
                    "the stale_journal_run_key case set stages the authenticated Pi task text",
                    "the stale_journal_run_key_legacy case set stages the authenticated Pi task text",
                    "the stale_journal_fence case set stages the authenticated Pi task text",
                    "the stale_journal_subject case set stages the authenticated Pi task text",
                    "the synthetic_goal_cleanup case set stages the authenticated Pi task text",
                ),
            ),
            (
                "migration-leak",
                source.replace(
                    'if [[ "$fixture_case_set" == shared ]]; then',
                    'if [[ "$fixture_case_set" == shared || "$fixture_case_set" == migration ]]; then',
                    1,
                ),
                claims,
                (
                    "the migration case set stages the authenticated Pi task text",
                    "the migration case set's fixed B home install list stages the authenticated Pi task text",
                ),
            ),
            (
                "newly-covered-set-leak",
                source.replace(
                    'if [[ "$fixture_case_set" == shared ]]; then',
                    'if [[ "$fixture_case_set" == shared || "$fixture_case_set" == synthetic_goal_cleanup ]]; then',
                    1,
                ),
                claims,
                (
                    "the synthetic_goal_cleanup case set stages the authenticated Pi task text",
                    "the synthetic_goal_cleanup case set's fixed B home install list stages the "
                    "authenticated Pi task text",
                ),
            ),
            (
                "containment-guard-leak",
                source.replace(
                    'if [[ "$fixture_case_set" == shared ]]; then',
                    'if [[ "$fixture_case_set" == shared || "$fixture_case_set" == codex_containment ]]; then',
                    1,
                ),
                claims,
                (
                    "the codex_containment case set's fixed B home install list stages the "
                    "authenticated Pi task text without a Pi home",
                ),
            ),
            (
                "containment-branch-loss",
                source.replace(
                    'containment_kind = {"codex-containment": "codex", "claude-containment": "claude_code"}.get(',
                    'containment_kind = {"claude-containment": "claude_code"}.get(',
                    1,
                ),
                claims,
                ("the codex_containment case set is not the containment fixture: it staged a Pi home",),
            ),
            (
                "unconditional-off-thinking-level",
                source.replace(
                    'authenticated_pi_thinking_level = "off" if fixture_identity_suffix == "shared" else "minimal"',
                    'authenticated_pi_thinking_level = "off"',
                    1,
                ),
                claims,
                (
                    "the stage_replay_create_started case set stages the authenticated thinking level 'off'",
                    "the stage_replay_leaf_bound case set stages the authenticated thinking level 'off'",
                    "the stage_replay_launcher_bound case set stages the authenticated thinking level 'off'",
                    "the ack_loss_launcher_bound case set stages the authenticated thinking level 'off'",
                    "the stale_leaf case set stages the authenticated thinking level 'off'",
                    "the migration case set stages the authenticated thinking level 'off'",
                    "the alternate_mount_refusal case set stages the authenticated thinking level 'off'",
                    "the stale_journal_run_key case set stages the authenticated thinking level 'off'",
                    "the stale_journal_run_key_legacy case set stages the authenticated thinking level 'off'",
                    "the stale_journal_fence case set stages the authenticated thinking level 'off'",
                    "the stale_journal_subject case set stages the authenticated thinking level 'off'",
                    "the synthetic_goal_cleanup case set stages the authenticated thinking level 'off'",
                ),
            ),
            (
                "reworded-task-text",
                source.replace("for the configured GitHub repository", "for the configured resource", 1),
                claims,
                ("the shared case set does not stage the exact authenticated Pi task text",),
            ),
            (
                "deleted-fixed-scope-paragraph",
                source.replace(f"\n\n{FIXED_SCOPE_PARAGRAPH}", "", 1),
                claims,
                ("the shared case set does not stage the exact authenticated Pi task text",),
            ),
            (
                "blank-staging-argument",
                source.replace(
                    '"$SYNTHETIC_GOAL_CLEANUP_CLONES" "$authenticated_pi_task_text" <<\'PY\'',
                    '"$SYNTHETIC_GOAL_CLEANUP_CLONES" "" <<\'PY\'',
                    1,
                ),
                claims,
                ("the shared case set does not stage the exact authenticated Pi task text",),
            ),
            (
                "no-fixture-write",
                source.replace("    if authenticated_pi_task_text:\n", "    if False:\n", 1),
                claims,
                ("the shared case set does not stage the exact authenticated Pi task text",),
            ),
            (
                "misplaced-install-target",
                source.replace(
                    'b_native_credential_files+=("$B_HOME/.pi/agent/APPEND_SYSTEM.md")',
                    'b_native_credential_files+=("$B_HOME/.pi/agent/APPEND_SYSTEM.txt")',
                    1,
                ),
                claims,
                ("the shared case set's fixed B home install list omits the staged task text",),
            ),
            (
                "removed-install-row",
                source.replace(extracted_authenticated_pi_install_block(source), "", 1),
                claims,
                ("the shared case set's fixed B home install list omits the staged task text",),
            ),
            (
                "dropped-admitted-description",
                source,
                claims.replace(
                    "Use the real symmetry_resource_sync tool for the configured GitHub repository, "
                    "then create and commit one small file. Do not publish.",
                    "Use the configured provider tool, then create and commit one small file.",
                    1,
                ),
                ("the shared case set does not stage the exact authenticated Pi task text",),
            ),
            (
                "removed-control-claim-row",
                source,
                claims.replace(
                    '"pi_provider_action", "pi-native", :pi, :pi_provider,',
                    '"_removed_", "pi-native", :pi, :pi_provider,',
                    1,
                ),
                ("the Control claim fixture no longer carries the pi_provider_action description",),
            ),
            (
                "dropped-staging-argument",
                source.replace(
                    '"$SYNTHETIC_GOAL_CLEANUP_CLONES" "$authenticated_pi_task_text" <<\'PY\'',
                    '"$SYNTHETIC_GOAL_CLEANUP_CLONES" <<\'PY\'',
                    1,
                ),
                claims,
                (
                    "the fixture staging script for shared exited 1",
                    "the fixture staging script for codex_containment exited 1",
                    "the fixture staging script for claude_containment exited 1",
                    "the fixture staging script for stage_replay_create_started exited 1",
                    "the fixture staging script for stage_replay_leaf_bound exited 1",
                    "the fixture staging script for stage_replay_launcher_bound exited 1",
                    "the fixture staging script for ack_loss_launcher_bound exited 1",
                    "the fixture staging script for stale_leaf exited 1",
                    "the fixture staging script for migration exited 1",
                    "the fixture staging script for alternate_mount_refusal exited 1",
                    "the fixture staging script for stale_journal_run_key exited 1",
                    "the fixture staging script for stale_journal_run_key_legacy exited 1",
                    "the fixture staging script for stale_journal_fence exited 1",
                    "the fixture staging script for stale_journal_subject exited 1",
                    "the fixture staging script for synthetic_goal_cleanup exited 1",
                ),
            ),
        )
        for name, mutant_source, mutant_claims, expected in mutations:
            with self.subTest(mutation=name):
                self.assertNotEqual(
                    (mutant_source, mutant_claims),
                    (source, claims),
                    "the mutation must change the fixture",
                )
                root = self.root / name
                root.mkdir()
                self.assertEqual(
                    authenticated_pi_task_failures(mutant_source, mutant_claims, root), list(expected), name
                )


if __name__ == "__main__":
    unittest.main(verbosity=2)
