#!/usr/bin/env python3
"""Export only named, token-free Linux v2 witness summaries for CI review."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import pathlib
import re
import stat
import sys
import tempfile


MANUAL_WITNESSES = (
    ("pi_provider_action/TestProductionLinuxV2PiNativeProviderAction.json", "pi-native-provider-action.json"),
    ("opencode_native_health/TestProductionLinuxV2OpenCodeNativeHealth.json", "opencode-native-health.json"),
    ("helper_sigkill_detached_child/TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild.json", "pi-helper-sigkill-detached-child.json"),
    ("daemon_sigkill_helper_survives_detached_child/TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild.json", "pi-daemon-sigkill-helper-survives.json"),
    ("stale-owner-request-denials/TestProductionLinuxV2StaleOwnerRequestDenials_stale_journal_run_key.json", "stale-run-key-denial.json"),
    ("stale-owner-request-denials/TestProductionLinuxV2StaleOwnerRequestDenials_stale_journal_fence.json", "stale-fence-denial.json"),
    ("stale-owner-request-denials/TestProductionLinuxV2StaleOwnerRequestDenials_stale_journal_subject.json", "stale-subject-denial.json"),
    ("migration-denial/TestProductionLinuxV2MigrationDenialRetainsJournal.json", "migration-denial.json"),
    ("receipt-persist-failure/TestProductionLinuxV2StopReceiptPersistenceFailureRetainsJournal.json", "receipt-persistence-failure.json"),
    ("stale-leaf/TestProductionLinuxV2StaleLeafIdentityRetainsJournal.json", "stale-leaf-identity.json"),
    ("alternate-mount/TestProductionLinuxV2AlternateCgroupMountRefusal.json", "alternate-mount-refusal.json"),
    ("literal-pid-reuse/TestProductionLinuxV2LiteralPIDReuseAfterStopReceipt.json", "literal-pid-reuse.json"),
    ("helper-response-loss-stop/TestProductionLinuxV2HelperResponseLossReplayMatrix_stop.json", "helper-stop-response-loss.json"),
    ("helper-response-loss-ack_stop_receipt/TestProductionLinuxV2HelperResponseLossReplayMatrix_ack_stop_receipt.json", "helper-ack-response-loss.json"),
    ("helper-response-loss-launcher_ready/TestProductionLinuxV2HelperResponseLossReplayMatrix_launcher_ready.json", "helper-launcher-response-loss.json"),
    ("helper-exit-after-release-before-register/TestProductionLinuxV2HelperExitAfterReleaseBeforeNativeRegister.json", "helper-exit-after-release-before-register.json"),
    ("synthetic-goal-cleanup/TestProductionLinuxV2CloneCleanupAuthenticatedScratch.json", "synthetic-goal-cleanup.json"),
    ("unresolved-journal-guard/TestProductionLinuxV2UnresolvedJournalBlocksFreshRun.json", "unresolved-journal-guard.json"),
    ("unprovisioned/TestProductionLinuxV2UnprovisionedHostRefusal.json", "unprovisioned-owner-refusal.json"),
)
MANUAL_STAGE_HOLDS = (
    "reservation_durable", "create_started", "leaf_bound", "helper_bound", "launcher_bound", "gate_bound",
    "handoff_prepared", "handoff_committed", "lease_armed", "checkout_ready", "native_release",
    "stop_receipt_durable", "leaf_removed", "journal_cleared",
)
MANUAL_ACK_LOSS_STAGES = (
    "helper_bound", "launcher_bound", "gate_bound", "handoff_prepared", "handoff_committed",
    "lease_armed", "checkout_ready", "stop_receipt_durable", "native_release", "journal_cleared",
)
MANUAL_RECEIPTS = (
    ("control-provider-action-pi_provider_action.json", "control-provider-action-pi.json"),
    ("control-provider-action-helper_sigkill_detached_child.json", "control-provider-action-helper-crash.json"),
    ("control-provider-action-daemon_sigkill_helper_survives_detached_child.json", "control-provider-action-daemon-crash.json"),
)
COMPOSE_WITNESSES = (
    "rootless-compose-pi.json",
    "rootless-compose-opencode.json",
    "TestProductionLinuxV2UnprovisionedHostRefusal.json",
)
COMPOSE_DIAGNOSTICS = {
    f"rootless-compose-{case}-diagnostic.json": case
    for case in ("shared", "pi", "opencode", "unprovisioned")
}
COMPOSE_NATIVE_STAGES = {
    "case_inputs", "pinned_version", "probe", "probe_projection", "runtime_registration",
    "arm", "early_release", "work_item", "launch", "failed_run", "held_response",
    "held_binding", "journal_read", "claim_binding", "no_effects", "wrong_releases",
    "exact_release", "evidence_write", "complete",
}
COMPOSE_DIAGNOSTIC_STAGES = {
    "shared": {"rootless_guards", "portal", "transition_proxy", "native_cases", "complete"},
    "pi": COMPOSE_NATIVE_STAGES,
    "opencode": COMPOSE_NATIVE_STAGES,
    "unprovisioned": {
        "rootless_guards", "fixture_files", "config", "empty_store", "request_contract",
        "fresh_barrier", "binary_access", "command", "owner_refusal", "empty_response",
        "store_unchanged", "barrier_unchanged", "evidence_write", "complete",
    },
}
COMPOSE_DIAGNOSTIC_OBSERVATIONS = {
    "probe": {"native_unverified", "unsupported_version", "unavailable", "cancelled", "deadline", "other"},
    "probe_projection": {"transport_verified", "transport_unverified"},
    "failed_run": {
        "queued", "assigned", "claimed", "running", "paused", "waiting_for_input", "cancelling",
        "failed", "completed", "cancelled", "expired", "other", "runtime_mismatch",
        "assignment_missing", "refusal_mismatch",
    },
    "command": {"command_failed", "command_succeeded", "response_close_failed"},
    "owner_refusal": {"other_refusal"},
}
LEGACY_WITNESSES = (
    "bind-before-commit", "prepare-before-bind", "commit-before-resume", "lease-arm-before-resume",
    "daemon-after-resume", "daemon-after-resume-clean-seal-boundary", "helper-mirror",
    "wrong-hello-session", "setsid-escape", "scan-failure", "helper-scan-failure-crash",
    "helper-scan-pending-crash", "literal-pid-reuse", "stop-response-lost",
    "release-response-lost", "helper-dead-mirror",
)
LEGACY_HELPER_SCAN_CRASH_STAGES = {
    "helper_scan_failure_sigkill_before_cancel": {
        "daemon_task_setup", "running_journal", "descendant_marker", "trigger_fired_marker",
        "scan_failure_journal", "helper_exit", "cancel_request", "mirror_takeover",
        "mirror_authority", "mirror_stop_receipt_absent", "mirror_process_details",
        "mirror_scan_ledger", "mirror_unproven_scan", "post_takeover", "complete",
    },
    "helper_pending_scan_sigkill": {
        "daemon_task_setup", "running_journal", "descendant_marker", "trigger_fired_marker",
        "pending_scan_journal", "helper_exit", "cancel_request", "mirror_takeover",
        "mirror_authority", "mirror_stop_receipt_absent", "mirror_process_details",
        "mirror_scan_ledger", "mirror_pending_scan", "post_takeover", "complete",
    },
}
SAFE_STAGE_HOLDS = (
    "reservation_durable", "helper_bound", "gate_bound", "handoff_prepared", "handoff_committed",
    "lease_armed", "checkout_ready", "native_release", "stop_receipt_durable", "leaf_removed", "journal_cleared",
)
SAFE_ACK_LOSS_STAGES = (
    "helper_bound", "gate_bound", "handoff_prepared", "handoff_committed", "lease_armed",
    "checkout_ready", "stop_receipt_durable", "native_release", "journal_cleared",
)
MANUAL_CASE_SET_FILES = {
    "shared": {
        "pi-native-provider-action.json", "opencode-native-health.json",
        "pi-helper-sigkill-detached-child.json", "pi-daemon-sigkill-helper-survives.json",
        "receipt-persistence-failure.json", "alternate-mount-refusal.json", "literal-pid-reuse.json",
        "helper-stop-response-loss.json", "helper-ack-response-loss.json",
        "helper-launcher-response-loss.json", "helper-exit-after-release-before-register.json",
        "unprovisioned-owner-refusal.json", "control-provider-action-pi.json",
        "control-provider-action-helper-crash.json", "control-provider-action-daemon-crash.json",
        *(f"stage-crash-replay-{stage}.json" for stage in SAFE_STAGE_HOLDS),
        *(f"ack-loss-replay-{stage}.json" for stage in SAFE_ACK_LOSS_STAGES),
    },
    "stage_replay_create_started": {"stage-crash-replay-create_started.json"},
    "stage_replay_leaf_bound": {"stage-crash-replay-leaf_bound.json"},
    "stage_replay_launcher_bound": {"stage-crash-replay-launcher_bound.json"},
    "ack_loss_launcher_bound": {"ack-loss-replay-launcher_bound.json"},
    "stale_leaf": {"stale-leaf-identity.json"},
    "migration": {"migration-denial.json"},
    "stale_journal_run_key": {"unresolved-journal-guard.json"},
    "stale_journal_run_key_legacy": {"stale-run-key-denial.json"},
    "stale_journal_fence": {"stale-fence-denial.json"},
    "stale_journal_subject": {"stale-subject-denial.json"},
    "synthetic_goal_cleanup": {"synthetic-goal-cleanup.json"},
}
FORBIDDEN_KEYS = (
    "lease_token",
    "machine_token",
    "provider_token",
    "authorization",
    "api_key",
    "password",
    "secret",
    "raw_journal",
    "request_json",
)
MAX_EVIDENCE_SIZE = 2 << 20
SAFE_TEXT = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:/+@ -]{0,255}$")
SAFE_HASH = re.compile(r"^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$")

METADATA_KEYS = {
    "subject", "tree", "tree_sha256", "go_version", "kernel_release", "goos", "goarch",
    "source_path", "source_sha256", "binary_path", "binary_sha256", "tree_digest",
    "dirty_status_sha256",
}
EXIT_KEYS = {"requested_signal", "signaled", "signal", "exit_code", "error"}
PROCESS_KEYS = {"at", "role", "pid", "identity", "state", "pgrp", "session", "start_time", "exists", "error"}
RECEIPT_KEYS = {
    "version", "legacy_v1_no_v2_proof", "status", "owner_kind", "owner_context_sha256",
    "target_pid", "target_identity", "pipe_token_sha256", "job_id_sha256", "supervisor_pid",
    "supervisor_identity", "active_processes",
}
OWNER_KEYS = {
    "owner_kind", "owner_context_sha256", "target_pid", "target_identity", "supervisor_pid",
    "supervisor_identity", "job_id_sha256", "has_stop_receipt",
}
SCAN_PROGRESS_KEYS = {
    "pending_sequence", "clean_result_sequence", "completed_sequence", "sealed_sequence",
    "unproven_sequence", "failure_kind",
}
SCAN_KEYS = {"target_pid", "target_identity", "supervisor_pid", "supervisor_identity", "helper", "mirror"}
JOURNAL_KEYS = {
    "at", "path", "raw_sha256", "run_id", "generation", "local_state", "terminal_state",
    "terminal_verdict", "pid", "process_identity", "started_at", "lease_expires_at",
    "has_process_details", "containment_unproven", "retain_workspace", "pending_events",
    "pending_transitions", "containment_scans", "stop_receipt", "handoff", "authority",
}
BARRIER_KEYS = {"run_id", "generation", "pid", "identity", "receipt"}
HELLO_ANCHOR_KEYS = {"target_pid", "target_identity", "target_pgrp", "target_session", "target_start_time"}
HELLO_KEYS = {"actual", "transmitted", "supervisor_pid", "supervisor_identity"}
PID_IDENTITY_KEYS = {"pid", "identity", "pgrp", "session", "start_time"}
PID_REUSE_CRASH_KEYS = {"stage", "status"}
PID_REUSE_KEYS = {
    "task_id", "parent_pid_namespace_inode", "private_pid_namespace_inode",
    "parent_network_namespace_inode", "private_network_namespace_inode", "control_url_sha256",
    "namespace_creation_flags", "namespace_allocator_path", "namespace_allocator_before",
    "namespace_allocator_reset_to", "namespace_allocator_readback", "pidfd_group_signal_supported",
    "original_target", "helper", "replacement", "owner_stop_before_proof_reached_path",
    "owner_stop_before_proof_release_path", "owner_stop_proof_result_path",
    "recovery_authenticated_marker_path", "owner_stop_before_proof_reached",
    "owner_stop_before_proof_reached_sha256", "owner_stop_before_proof_release",
    "owner_stop_proof_result", "recovery_helper_scan_seal_eligible",
    "recovery_helper_scan_seal_eligible_after_proof", "recovery_stop_failure_observed",
    "recovery_stop_failure_event", "recovery_runtime_registered", "crash_statuses",
    "recovery_daemon_shutdown", "assertions", "output_digests", "notes",
}
LEGACY_COMMON_KEYS = {
    "version", "case", "metadata", "case_root", "crash", "journal_snapshots",
    "process_snapshots", "raw_output", "notes",
}
LEGACY_SPINE_KEYS = LEGACY_COMMON_KEYS | {
    "stage", "task_id", "run_id", "generation", "journal_key", "barrier_reached_path",
    "barrier_release_path", "barrier_reached_contents", "barrier_release_contents",
    "scan_trigger_path", "scan_reached_path", "scan_helper_reached", "scan_mirror_reached",
    "scan_release_path", "scan_release_contents", "stop_receipt_reached_path",
    "stop_receipt_reached", "stop_receipt_release_path", "stop_receipt_release",
    "crash_after_stop_receipt", "stop_receipt_before_release",
}
LEGACY_NEGATIVE_KEYS = LEGACY_COMMON_KEYS | {"status", "helper_hello", "pid_reuse", "assertions"}
LEGACY_REPLAY_KEYS = LEGACY_COMMON_KEYS | {
    "stage", "task_id", "run_id", "generation", "journal_key", "crash_statuses",
    "raw_journal_snapshots", "receipt_before_clear", "replay_count", "exact_receipt_replay",
    "clear_observed", "response_drop_operation", "response_drop_marker", "response_drop_fired",
    "barrier_reached_path", "barrier_release_path", "barrier_reached", "barrier_release",
    "stop_receipt_barrier_reached_path", "stop_receipt_barrier_release_path",
    "stop_receipt_barrier_reached", "stop_receipt_barrier_release",
    "replay_receipt_barrier_reached_path", "replay_receipt_barrier_release_path",
    "replay_receipt_barrier_reached", "replay_receipt_barrier_release", "scan_trigger_path",
    "scan_reached_path", "scan_release_path", "scan_helper_reached", "scan_mirror_reached",
    "scan_release", "duplicate_stop_marker", "duplicate_stop_seen",
}


def fail(message: str) -> None:
    raise SystemExit(f"redacted evidence export failed: {message}")


def reject_duplicate_keys(pairs):
    value = {}
    for key, child in pairs:
        if key in value:
            raise ValueError("duplicate JSON member")
        value[key] = child
    return value


def validate_document(data: bytes, secret_values: tuple[bytes, ...]) -> dict:
    try:
        document = json.loads(data.decode("utf-8", errors="strict"), object_pairs_hook=reject_duplicate_keys)
    except (UnicodeError, ValueError, json.JSONDecodeError):
        fail("allowlisted evidence is not strict JSON")
    if not isinstance(document, dict):
        fail("allowlisted evidence root is not an object")
    if any(secret and secret in data for secret in secret_values):
        fail("allowlisted evidence contains a configured credential value")
    try:
        decoded_secrets = tuple(secret.decode("utf-8", errors="strict") for secret in secret_values if secret)
    except UnicodeError:
        fail("configured credential values are not UTF-8")

    def inspect(value) -> None:
        if isinstance(value, dict):
            for key, child in value.items():
                inspect(key)
                normalized = re.sub(r"[^a-z0-9_]", "", key.lower())
                if any(forbidden in normalized for forbidden in FORBIDDEN_KEYS):
                    fail("allowlisted evidence contains a credential or raw-request field")
                inspect(child)
        elif isinstance(value, list):
            for child in value:
                inspect(child)
        elif isinstance(value, str):
            if any(secret in value for secret in decoded_secrets):
                fail("allowlisted evidence contains a configured credential value")

    inspect(document)
    return document


def reject_unknown(document: dict, allowed: set[str], context: str) -> None:
    unknown = set(document) - allowed
    if unknown:
        fail(f"legacy {context} contains unknown fields: {', '.join(sorted(unknown))}")


def validate_compose_diagnostic(document: dict, case: str) -> None:
    required = {"version", "case", "outcome", "stage"}
    if not required <= set(document) or set(document) - required - {"observation"}:
        fail("Compose diagnostic contains unexpected or missing fields")
    if type(document["version"]) is not int or document["version"] != 1 or document["case"] != case:
        fail("Compose diagnostic version or case does not match its fixed filename")
    stage = document["stage"]
    outcome = document["outcome"]
    if not isinstance(stage, str) or stage not in COMPOSE_DIAGNOSTIC_STAGES[case]:
        fail("Compose diagnostic stage is not allowlisted")
    if not isinstance(outcome, str) or outcome not in {"passed", "failed"} or (outcome == "passed") != (stage == "complete"):
        fail("Compose diagnostic outcome contradicts its stage")
    if "observation" in document:
        observation = document["observation"]
        if not isinstance(observation, str) or observation not in COMPOSE_DIAGNOSTIC_OBSERVATIONS.get(stage, set()):
            fail("Compose diagnostic observation is not allowlisted for its stage")


def require_object(value, context: str) -> dict:
    if not isinstance(value, dict):
        fail(f"legacy {context} must be an object")
    return value


def safe_text(value, context: str) -> str:
    if not isinstance(value, str) or not SAFE_TEXT.fullmatch(value):
        fail(f"legacy {context} is not a bounded safe value")
    return value


def safe_hash(value, context: str) -> str:
    if not isinstance(value, str) or not SAFE_HASH.fullmatch(value):
        fail(f"legacy {context} is not a source or binary digest")
    return value.lower()


def safe_int(value, context: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < -1 or value > (1 << 63) - 1:
        fail(f"legacy {context} is not a bounded integer")
    return value


def safe_bool(value, context: str) -> bool:
    if not isinstance(value, bool):
        fail(f"legacy {context} is not boolean")
    return value


def project_metadata(value) -> dict:
    source = require_object(value, "metadata")
    reject_unknown(source, METADATA_KEYS, "metadata")
    required = ("subject", "tree", "tree_sha256", "source_sha256", "binary_sha256")
    if any(key not in source for key in required):
        fail("legacy metadata omits source or binary identity")
    result = {key: safe_hash(source[key], f"metadata.{key}") for key in required}
    for key in ("go_version", "kernel_release", "goos", "goarch", "tree_digest", "dirty_status_sha256"):
        if key in source:
            result[key] = safe_hash(source[key], f"metadata.{key}") if key.endswith("sha256") or key == "tree_digest" else safe_text(source[key], f"metadata.{key}")
    return result


def project_exit(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, EXIT_KEYS, context)
    result = {}
    for key in ("requested_signal", "signal"):
        if key in source:
            result[key] = safe_text(source[key], f"{context}.{key}")
    for key in ("signaled",):
        if key in source:
            result[key] = safe_bool(source[key], f"{context}.{key}")
    if "exit_code" in source:
        result["exit_code"] = safe_int(source["exit_code"], f"{context}.exit_code")
    return result


def project_receipt(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, RECEIPT_KEYS, context)
    required = {
        "version", "legacy_v1_no_v2_proof", "status", "target_pid", "target_identity",
        "pipe_token_sha256", "supervisor_pid", "supervisor_identity", "active_processes",
    }
    missing = required - set(source)
    if missing:
        fail(f"legacy {context} omits required fields: {', '.join(sorted(missing))}")
    result = {}
    for key, child in source.items():
        if key in ("version", "target_pid", "supervisor_pid", "active_processes"):
            result[key] = safe_int(child, f"{context}.{key}")
        elif key == "legacy_v1_no_v2_proof":
            result[key] = safe_bool(child, f"{context}.{key}")
        elif key.endswith("sha256"):
            result[key] = safe_hash(child, f"{context}.{key}")
        else:
            result[key] = safe_text(child, f"{context}.{key}")
    return result


def project_owner(value, context: str, *, handoff: bool = False) -> dict:
    source = require_object(value, context)
    reject_unknown(source, OWNER_KEYS, context)
    required = {"target_pid", "target_identity", "supervisor_pid", "supervisor_identity", "has_stop_receipt"}
    missing = required - set(source)
    if missing:
        fail(f"legacy {context} omits required fields: {', '.join(sorted(missing))}")
    result = {}
    for key, child in source.items():
        if key in ("target_pid", "supervisor_pid"):
            result[key] = safe_int(child, f"{context}.{key}")
        elif key == "has_stop_receipt":
            result[key] = safe_bool(child, f"{context}.{key}")
        elif key.endswith("sha256"):
            result[key] = safe_hash(child, f"{context}.{key}")
        elif key == "supervisor_identity" and child == "" and handoff and source["supervisor_pid"] == 0 and source["has_stop_receipt"] is False:
            # A prepared handoff has not bound a supervisor; authority and receipts never use this shape.
            result[key] = ""
        else:
            result[key] = safe_text(child, f"{context}.{key}")
    return result


def project_scan_progress(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, SCAN_PROGRESS_KEYS, context)
    return {
        key: safe_text(child, f"{context}.{key}") if key == "failure_kind" else safe_int(child, f"{context}.{key}")
        for key, child in source.items()
    }


def project_scan(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, SCAN_KEYS, context)
    result = {}
    for key, child in source.items():
        if key in ("helper", "mirror"):
            result[key] = project_scan_progress(child, f"{context}.{key}")
        elif key in ("target_pid", "supervisor_pid"):
            result[key] = safe_int(child, f"{context}.{key}")
        else:
            result[key] = safe_text(child, f"{context}.{key}")
    return result


def project_journal(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, JOURNAL_KEYS, context)
    required = {
        "raw_sha256", "generation", "local_state", "has_process_details", "containment_unproven",
        "retain_workspace", "pending_events", "pending_transitions",
    }
    missing = required - set(source)
    if missing:
        fail(f"legacy {context} omits required fields: {', '.join(sorted(missing))}")
    handoff = source.get("handoff")
    if isinstance(handoff, dict) and handoff.get("supervisor_identity") == "":
        if any(key in source for key in ("stop_receipt", "authority", "containment_scans")) or source["has_process_details"] is not False or source.get("pid", 0) != 0 or source.get("process_identity", "") != "":
            fail(f"legacy {context} unbound handoff contradicts bound process, receipt, authority, or scan evidence")
    result = {}
    for key, child in source.items():
        if key in ("path", "run_id", "at", "started_at", "lease_expires_at"):
            continue
        if key in ("generation", "pid", "pending_events", "pending_transitions"):
            result[key] = safe_int(child, f"{context}.{key}")
        elif key in ("has_process_details", "containment_unproven", "retain_workspace"):
            result[key] = safe_bool(child, f"{context}.{key}")
        elif key == "raw_sha256":
            result[key] = safe_hash(child, f"{context}.{key}")
        elif key == "containment_scans":
            result[key] = project_scan(child, f"{context}.{key}")
        elif key == "stop_receipt":
            result[key] = project_receipt(child, f"{context}.{key}")
        elif key in ("handoff", "authority"):
            result[key] = project_owner(child, f"{context}.{key}", handoff=key == "handoff")
        else:
            result[key] = safe_text(child, f"{context}.{key}")
    return result


def project_process(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, PROCESS_KEYS, context)
    required = {"role", "pid", "exists"}
    missing = required - set(source)
    if missing:
        fail(f"legacy {context} omits required fields: {', '.join(sorted(missing))}")
    result = {}
    for key, child in source.items():
        if key in ("at", "error"):
            continue
        if key in ("pid", "pgrp", "session", "start_time"):
            result[key] = safe_int(child, f"{context}.{key}")
        elif key == "exists":
            result[key] = safe_bool(child, f"{context}.{key}")
        else:
            result[key] = safe_text(child, f"{context}.{key}")
    return result


def project_assertions(value, context: str) -> dict:
    source = require_object(value, context)
    result = {}
    for key, child in source.items():
        result[safe_text(key, f"{context} key")] = safe_bool(child, f"{context}.{key}")
    return result


def project_barrier(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, BARRIER_KEYS, context)
    required = {"generation", "pid", "identity", "receipt"}
    missing = required - set(source)
    if missing:
        fail(f"legacy {context} omits required fields: {', '.join(sorted(missing))}")
    result = {}
    for key, child in source.items():
        if key in ("run_id",):
            continue
        if key in ("generation", "pid"):
            result[key] = safe_int(child, f"{context}.{key}")
        elif key == "receipt":
            result[key] = project_receipt(child, f"{context}.receipt")
        else:
            result[key] = safe_text(child, f"{context}.{key}")
    return result


def project_hello_anchor(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, HELLO_ANCHOR_KEYS, context)
    return {key: safe_text(child, f"{context}.{key}") if key == "target_identity" else safe_int(child, f"{context}.{key}") for key, child in source.items()}


def project_hello(value) -> dict:
    source = require_object(value, "helper_hello")
    reject_unknown(source, HELLO_KEYS, "helper_hello")
    result = {}
    for key, child in source.items():
        if key in ("actual", "transmitted"):
            result[key] = project_hello_anchor(child, f"helper_hello.{key}")
        elif key == "supervisor_pid":
            result[key] = safe_int(child, f"helper_hello.{key}")
        else:
            result[key] = safe_text(child, f"helper_hello.{key}")
    return result


def project_pid_identity(value, context: str) -> dict:
    source = require_object(value, context)
    reject_unknown(source, PID_IDENTITY_KEYS, context)
    return {key: safe_text(child, f"{context}.{key}") if key == "identity" else safe_int(child, f"{context}.{key}") for key, child in source.items()}


def project_pid_reuse(value) -> dict:
    source = require_object(value, "pid_reuse")
    reject_unknown(source, PID_REUSE_KEYS, "pid_reuse")
    result = {}
    integer_fields = {
        "parent_pid_namespace_inode", "private_pid_namespace_inode", "parent_network_namespace_inode",
        "private_network_namespace_inode", "namespace_allocator_reset_to",
    }
    boolean_fields = {
        "pidfd_group_signal_supported", "owner_stop_before_proof_reached", "owner_stop_before_proof_release",
        "recovery_helper_scan_seal_eligible", "recovery_helper_scan_seal_eligible_after_proof",
        "recovery_stop_failure_observed", "recovery_runtime_registered",
    }
    for key, child in source.items():
        if key in integer_fields:
            result[key] = safe_int(child, f"pid_reuse.{key}")
        elif key in boolean_fields:
            result[key] = safe_bool(child, f"pid_reuse.{key}")
        elif key in ("control_url_sha256", "owner_stop_before_proof_reached_sha256"):
            result[key] = safe_hash(child, f"pid_reuse.{key}")
        elif key in ("original_target", "helper", "replacement"):
            result[key] = project_pid_identity(child, f"pid_reuse.{key}")
        elif key == "assertions":
            result[key] = project_assertions(child, "pid_reuse.assertions")
        elif key == "crash_statuses":
            if not isinstance(child, list):
                fail("legacy pid_reuse.crash_statuses must be a list")
            crashes = []
            for index, item in enumerate(child):
                item = require_object(item, f"pid_reuse.crash_statuses[{index}]")
                reject_unknown(item, PID_REUSE_CRASH_KEYS, f"pid_reuse.crash_statuses[{index}]")
                crashes.append({"stage": safe_text(item["stage"], "pid_reuse crash stage"), "status": project_exit(item["status"], "pid_reuse crash status")})
            result[key] = crashes
        elif key == "recovery_daemon_shutdown":
            result[key] = project_exit(child, "pid_reuse.recovery_daemon_shutdown")
        elif key == "output_digests":
            digests = require_object(child, "pid_reuse.output_digests")
            projected = {}
            for name, digest in digests.items():
                name = safe_text(name, "pid_reuse output digest key")
                if not isinstance(digest, str) or not re.fullmatch(r"[0-9]+ bytes sha256[:=][a-fA-F0-9]{64}", digest):
                    fail("legacy output digest is not a byte-count and SHA-256")
                projected[name] = digest.lower()
            result[key] = projected
        elif key in ("owner_stop_proof_result",):
            result[key] = safe_text(child, f"pid_reuse.{key}")
        # IDs, paths, allocator contents, event/log text, flags, and notes are intentionally private.
    return result


def project_legacy_document(document: dict) -> dict:
    if "raw_journal_snapshots" in document:
        family = "replay"
        allowed = LEGACY_REPLAY_KEYS
    elif "assertions" in document or "pid_reuse" in document or "helper_hello" in document:
        family = "negative"
        allowed = LEGACY_NEGATIVE_KEYS
        if isinstance(document.get("case"), str) and document["case"] in LEGACY_HELPER_SCAN_CRASH_STAGES:
            allowed = allowed | {"stage"}
    else:
        family = "spine"
        allowed = LEGACY_SPINE_KEYS
    reject_unknown(document, allowed, f"{family} evidence")
    required = ("version", "case", "metadata", "journal_snapshots", "process_snapshots")
    if any(key not in document for key in required):
        fail(f"legacy {family} evidence omits required proof fields")
    result = {
        "version": safe_int(document["version"], "version"),
        "family": family,
        "case": safe_text(document["case"], "case"),
        "metadata": project_metadata(document["metadata"]),
    }
    if "stage" in document:
        result["stage"] = safe_text(document["stage"], "stage")
        if result["case"] in LEGACY_HELPER_SCAN_CRASH_STAGES and result["stage"] not in LEGACY_HELPER_SCAN_CRASH_STAGES[result["case"]]:
            fail("legacy helper scan crash stage is not allowlisted")
    if "status" in document:
        result["status"] = safe_text(document["status"], "status")
    for key in ("crash", "crash_after_stop_receipt"):
        if key in document:
            result[key] = project_exit(document[key], key)
    if "crash_statuses" in document:
        if not isinstance(document["crash_statuses"], list):
            fail("legacy crash_statuses must be a list")
        result["crash_statuses"] = [project_exit(item, f"crash_statuses[{index}]") for index, item in enumerate(document["crash_statuses"])]
    journal_snapshots = document["journal_snapshots"]
    process_snapshots = document["process_snapshots"]
    if journal_snapshots is not None and not isinstance(journal_snapshots, list):
        fail("legacy journal_snapshots must be a list or null")
    if process_snapshots is not None and not isinstance(process_snapshots, list):
        fail("legacy process_snapshots must be a list or null")
    result["journal_snapshots"] = None if journal_snapshots is None else [
        project_journal(item, f"journal_snapshots[{index}]") for index, item in enumerate(journal_snapshots)
    ]
    result["process_snapshots"] = None if process_snapshots is None else [
        project_process(item, f"process_snapshots[{index}]") for index, item in enumerate(process_snapshots)
    ]
    for key in ("stop_receipt_before_release", "receipt_before_clear"):
        if key in document:
            result[key] = project_receipt(document[key], key)
    for key in ("stop_receipt_reached", "stop_receipt_barrier_reached", "replay_receipt_barrier_reached"):
        if key in document:
            result[key] = project_barrier(document[key], key)
    for key in ("replay_count",):
        if key in document:
            result[key] = safe_int(document[key], key)
    for key in ("exact_receipt_replay", "clear_observed", "response_drop_fired", "duplicate_stop_seen"):
        if key in document:
            result[key] = safe_bool(document[key], key)
    if "response_drop_operation" in document:
        result["response_drop_operation"] = safe_text(document["response_drop_operation"], "response_drop_operation")
    if "assertions" in document:
        result["assertions"] = project_assertions(document["assertions"], "assertions")
    if "helper_hello" in document:
        result["helper_hello"] = project_hello(document["helper_hello"])
    if "pid_reuse" in document:
        result["pid_reuse"] = project_pid_reuse(document["pid_reuse"])
    return result


def read_json_regular(path: pathlib.Path) -> dict:
    raw, _ = read_regular(path, (), validate=False)
    try:
        document = json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=reject_duplicate_keys)
    except (UnicodeError, ValueError, json.JSONDecodeError):
        fail("legacy evidence is not strict JSON")
    if not isinstance(document, dict):
        fail("legacy evidence root is not an object")
    # Legacy inputs intentionally contain private raw fields. Parse them strictly,
    # then project only the schema above rather than validating or copying raw bytes.
    return document


def read_regular(path: pathlib.Path, secret_values: tuple[bytes, ...], validate: bool = True,
                 diagnostic_owner: tuple[int, int] | None = None) -> tuple[bytes, dict | None]:
    maximum_size = 2048 if diagnostic_owner is not None else MAX_EVIDENCE_SIZE
    try:
        info = path.lstat()
    except FileNotFoundError:
        raise
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or info.st_size <= 0 or info.st_size > maximum_size:
        fail("allowlisted source must be a bounded regular file")
    if diagnostic_owner is not None and ((info.st_uid, info.st_gid) != diagnostic_owner or info.st_nlink != 1):
        fail("Compose diagnostic must be a bounded private singly-linked file")
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0)
    try:
        fd = os.open(path, flags)
        try:
            opened = os.fstat(fd)
            if not stat.S_ISREG(opened.st_mode) or opened.st_ino != info.st_ino or opened.st_dev != info.st_dev:
                fail("allowlisted source changed during open")
            if diagnostic_owner is not None and ((opened.st_uid, opened.st_gid) != diagnostic_owner or
                                                  opened.st_nlink != 1 or stat.S_IMODE(opened.st_mode) != 0o600 or
                                                  opened.st_size > maximum_size):
                fail("Compose diagnostic changed its private file boundary")
            data = bytearray()
            while len(data) <= maximum_size:
                chunk = os.read(fd, min(65536, maximum_size + 1 - len(data)))
                if not chunk:
                    break
                data.extend(chunk)
        finally:
            os.close(fd)
    except OSError:
        fail("allowlisted source could not be read")
    raw = bytes(data)
    if len(raw) != info.st_size:
        fail("allowlisted source changed while being read")
    return raw, validate_document(raw, secret_values) if validate else None


def atomic_write(directory: pathlib.Path, name: str, contents: bytes, uid: int, gid: int) -> None:
    temporary_fd, temporary_name = tempfile.mkstemp(prefix=".evidence-", dir=directory)
    try:
        os.fchmod(temporary_fd, 0o600)
        os.fchown(temporary_fd, uid, gid)
        view = memoryview(contents)
        while view:
            written = os.write(temporary_fd, view)
            if written <= 0:
                raise OSError("short write")
            view = view[written:]
        os.fsync(temporary_fd)
        os.close(temporary_fd)
        temporary_fd = -1
        os.replace(temporary_name, directory / name)
    finally:
        if temporary_fd >= 0:
            os.close(temporary_fd)
        try:
            os.unlink(temporary_name)
        except FileNotFoundError:
            pass


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("manual", "compose", "legacy"))
    parser.add_argument("output_directory")
    parser.add_argument("source_commit")
    parser.add_argument("run_status", choices=("passed", "failed"))
    parser.add_argument("--witness-root")
    parser.add_argument("--receipt-root")
    parser.add_argument("--secret-values-file")
    parser.add_argument("--runner-uid", type=int)
    parser.add_argument("--runner-gid", type=int)
    parser.add_argument("--case-set", choices=tuple(MANUAL_CASE_SET_FILES))
    arguments = parser.parse_args()

    output = pathlib.Path(arguments.output_directory)
    if not output.is_absolute() or output != pathlib.Path(os.path.normpath(str(output))):
        fail("output directory must be canonical and absolute")
    try:
        output_info = output.lstat()
    except OSError:
        fail("output directory is unavailable")
    if not stat.S_ISDIR(output_info.st_mode) or stat.S_ISLNK(output_info.st_mode):
        fail("output directory must be a real directory")
    runner_uid = os.getuid() if arguments.runner_uid is None else arguments.runner_uid
    runner_gid = os.getgid() if arguments.runner_gid is None else arguments.runner_gid
    if os.geteuid() != 0 and (runner_uid != os.getuid() or runner_gid != os.getgid()):
        fail("only root may export evidence on behalf of another user")
    if output_info.st_uid != runner_uid or stat.S_IMODE(output_info.st_mode) != 0o700:
        fail("output directory must be private to the runner")

    secret_values: tuple[bytes, ...] = ()
    if arguments.secret_values_file:
        try:
            secret_values = tuple(
                value for value in pathlib.Path(arguments.secret_values_file).read_bytes().split(b"\0") if value
            )
        except OSError:
            fail("private credential-scan file is unavailable")

    expected: list[tuple[pathlib.Path | None, str]] = []
    if arguments.mode == "manual":
        if not arguments.witness_root or not arguments.receipt_root or not arguments.case_set:
            fail("manual mode requires witness/Control receipt roots and one exact case set")
        witness_root = pathlib.Path(arguments.witness_root)
        receipt_root = pathlib.Path(arguments.receipt_root)
        expected.extend((witness_root / source, target) for source, target in MANUAL_WITNESSES)
        expected.extend(
            (
                witness_root / "stage-replay" / f"TestProductionLinuxV2StageCrashReplayMatrix_{stage}.json",
                f"stage-crash-replay-{stage}.json",
            )
            for stage in MANUAL_STAGE_HOLDS
        )
        expected.extend(
            (
                witness_root / "ack-loss-replay" / f"TestProductionLinuxV2AckLossReplayMatrix_{stage}.json",
                f"ack-loss-replay-{stage}.json",
            )
            for stage in MANUAL_ACK_LOSS_STAGES
        )
        expected.extend((receipt_root / source, target) for source, target in MANUAL_RECEIPTS)
        expected = [
            (source, target)
            for source, target in expected
            if target in MANUAL_CASE_SET_FILES[arguments.case_set]
        ]
    elif arguments.mode == "compose":
        if not arguments.witness_root:
            fail("Compose mode requires the private witness staging directory")
        witness_root = pathlib.Path(arguments.witness_root)
        expected.extend((witness_root / name, name) for name in COMPOSE_WITNESSES)
        expected.extend((witness_root / name, name) for name in COMPOSE_DIAGNOSTICS)
    else:
        if not arguments.witness_root:
            fail("legacy mode requires the private witness root")
        if arguments.receipt_root or arguments.case_set:
            fail("legacy mode accepts only --witness-root and optional credential scan input")
        witness_root = pathlib.Path(arguments.witness_root)
        expected.extend((witness_root / name / "witness-evidence.json", f"{name}.json") for name in LEGACY_WITNESSES)

    copied = []
    missing = []
    incomplete_files = []
    failed_diagnostics = []
    expected_destinations = {target for _, target in expected}
    for source, target in expected:
        if pathlib.Path(target).name != target or target == "manifest.json":
            fail("unsafe allowlist entry")
        destination = output / target
        try:
            if arguments.mode == "legacy":
                document = read_json_regular(source)
                projected = project_legacy_document(document)
                if projected["metadata"]["subject"] != arguments.source_commit.lower():
                    fail("legacy evidence subject does not match the requested source commit")
                if not projected["journal_snapshots"] or not projected["process_snapshots"]:
                    incomplete_files.append(target)
                data = json.dumps(projected, separators=(",", ":"), sort_keys=True).encode("utf-8") + b"\n"
                validate_document(data, secret_values)
            elif arguments.mode == "compose" and target in COMPOSE_DIAGNOSTICS:
                data, document = read_regular(source, secret_values, diagnostic_owner=(runner_uid, runner_gid))
                validate_compose_diagnostic(document, COMPOSE_DIAGNOSTICS[target])
                if document["outcome"] == "failed":
                    failed_diagnostics.append(target)
            else:
                data, _ = read_regular(source, secret_values)
        except FileNotFoundError:
            missing.append(target)
            continue
        if source != destination:
            if destination.exists() or destination.is_symlink():
                fail("evidence destination already exists")
            atomic_write(output, target, data, runner_uid, runner_gid)
        copied.append({"file": target, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()})

    for child in output.iterdir():
        if child.name not in expected_destinations and child.name != "manifest.json":
            fail("output directory contains a file outside the allowlist")

    if arguments.mode == "manual" and arguments.case_set == "synthetic_goal_cleanup":
        scope = "INSTALLED SYNTHETIC-GOAL CLEANUP; direct test-scoped Runner/Store/Workspace, not App end-to-end or Goal/Control acceptance"
    elif arguments.mode == "manual":
        scope = f"production Linux amd64 A/B owner witness case set: {arguments.case_set}"
    elif arguments.mode == "compose":
        scope = "rootless Compose native refusal and unprovisioned owner refusal"
    else:
        scope = "legacy Linux v1 production containment compatibility witnesses; sanitized summaries only"

    incomplete = bool(missing or incomplete_files or failed_diagnostics)
    effective_status = "failed" if arguments.mode in ("legacy", "compose") and incomplete else arguments.run_status
    manifest = {
        "version": 1,
        "source_commit": arguments.source_commit,
        "mode": arguments.mode,
        "status": effective_status,
        "scope": scope,
        "case_set": arguments.case_set if arguments.mode == "manual" else arguments.mode,
        "raw_logs_included": False,
        "raw_journals_included": False,
        "credentials_included": False,
        "files": copied,
        "missing_allowlisted_files": missing,
    }
    if arguments.mode == "legacy":
        manifest["complete"] = not incomplete
        manifest["incomplete_evidence_files"] = incomplete_files
    if arguments.mode == "compose":
        manifest["failed_diagnostic_files"] = failed_diagnostics
    encoded = json.dumps(manifest, separators=(",", ":"), sort_keys=True).encode("utf-8") + b"\n"
    atomic_write(output, "manifest.json", encoded, runner_uid, runner_gid)
    if arguments.run_status == "passed" and failed_diagnostics:
        fail("a successful witness run contains a failed Compose diagnostic")
    if arguments.run_status == "passed" and incomplete:
        fail("a successful witness run omitted required redacted receipts")
    print(f"allowlisted redacted evidence: {len(copied)} files; missing={len(missing)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
