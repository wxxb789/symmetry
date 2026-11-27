#!/usr/bin/env python3
"""Export only named, token-free Linux v2 witness summaries for CI review."""

from __future__ import annotations

import argparse
import contextlib
import hashlib
import json
import os
import pathlib
import re
import stat
import tempfile

MANUAL_WITNESSES = (
    ("pi_provider_action/TestProductionLinuxV2PiNativeProviderAction.json", "pi-native-provider-action.json"),
    ("opencode_native_health/TestProductionLinuxV2OpenCodeNativeHealth.json", "opencode-native-health.json"),
    (
        "helper_sigkill_detached_child/TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild.json",
        "pi-helper-sigkill-detached-child.json",
    ),
    (
        "daemon_sigkill_helper_survives_detached_child/TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild.json",
        "pi-daemon-sigkill-helper-survives.json",
    ),
    (
        "stale-owner-request-denials/TestProductionLinuxV2StaleOwnerRequestDenials_stale_journal_run_key.json",
        "stale-run-key-denial.json",
    ),
    (
        "stale-owner-request-denials/TestProductionLinuxV2StaleOwnerRequestDenials_stale_journal_fence.json",
        "stale-fence-denial.json",
    ),
    (
        "stale-owner-request-denials/TestProductionLinuxV2StaleOwnerRequestDenials_stale_journal_subject.json",
        "stale-subject-denial.json",
    ),
    ("migration-denial/TestProductionLinuxV2MigrationDenialRetainsJournal.json", "migration-denial.json"),
    (
        "receipt-persist-failure/TestProductionLinuxV2StopReceiptPersistenceFailureRetainsJournal.json",
        "receipt-persistence-failure.json",
    ),
    ("stale-leaf/TestProductionLinuxV2StaleLeafIdentityRetainsJournal.json", "stale-leaf-identity.json"),
    ("alternate-mount/TestProductionLinuxV2AlternateCgroupMountRefusal.json", "alternate-mount-refusal.json"),
    ("literal-pid-reuse/TestProductionLinuxV2LiteralPIDReuseAfterStopReceipt.json", "literal-pid-reuse.json"),
    (
        "helper-response-loss-stop/TestProductionLinuxV2HelperResponseLossReplayMatrix_stop.json",
        "helper-stop-response-loss.json",
    ),
    (
        "helper-response-loss-ack_stop_receipt/TestProductionLinuxV2HelperResponseLossReplayMatrix_ack_stop_receipt.json",
        "helper-ack-response-loss.json",
    ),
    (
        "helper-response-loss-launcher_ready/TestProductionLinuxV2HelperResponseLossReplayMatrix_launcher_ready.json",
        "helper-launcher-response-loss.json",
    ),
    (
        "helper-exit-after-release-before-register/TestProductionLinuxV2HelperExitAfterReleaseBeforeNativeRegister.json",
        "helper-exit-after-release-before-register.json",
    ),
    (
        "synthetic-goal-cleanup/TestProductionLinuxV2CloneCleanupAuthenticatedScratch.json",
        "synthetic-goal-cleanup.json",
    ),
    (
        "unresolved-journal-guard/TestProductionLinuxV2UnresolvedJournalBlocksFreshRun.json",
        "unresolved-journal-guard.json",
    ),
    ("unprovisioned/TestProductionLinuxV2UnprovisionedHostRefusal.json", "unprovisioned-owner-refusal.json"),
    ("codex_containment/TestProductionLinuxV2CodexNativeContainment.json", "codex-native-containment.json"),
    ("claude_containment/TestProductionLinuxV2ClaudeNativeContainment.json", "claude-native-containment.json"),
    (
        "stage-replay/TestProductionLinuxV2StageCrashReplayMatrix_native_release_codex.json",
        "codex-stage-crash-replay-native-release.json",
    ),
    (
        "stage-replay/TestProductionLinuxV2StageCrashReplayMatrix_native_release_claude.json",
        "claude-stage-crash-replay-native-release.json",
    ),
)
MANUAL_STAGE_HOLDS = (
    "reservation_durable",
    "create_started",
    "leaf_bound",
    "helper_bound",
    "launcher_bound",
    "gate_bound",
    "handoff_prepared",
    "handoff_committed",
    "lease_armed",
    "checkout_ready",
    "native_release",
    "stop_receipt_durable",
    "leaf_removed",
    "journal_cleared",
)
MANUAL_ACK_LOSS_STAGES = (
    "helper_bound",
    "launcher_bound",
    "gate_bound",
    "handoff_prepared",
    "handoff_committed",
    "lease_armed",
    "checkout_ready",
    "stop_receipt_durable",
    "native_release",
    "journal_cleared",
)
MANUAL_RECEIPTS = (
    ("control-provider-action-pi_provider_action.json", "control-provider-action-pi.json"),
    ("control-provider-action-helper_sigkill_detached_child.json", "control-provider-action-helper-crash.json"),
    (
        "control-provider-action-daemon_sigkill_helper_survives_detached_child.json",
        "control-provider-action-daemon-crash.json",
    ),
)
COMPOSE_WITNESSES = (
    "rootless-compose-pi.json",
    "rootless-compose-opencode.json",
    "TestProductionLinuxV2UnprovisionedHostRefusal.json",
)
COMPOSE_DIAGNOSTICS = {
    f"rootless-compose-{case}-diagnostic.json": case for case in ("shared", "pi", "opencode", "unprovisioned")
}
COMPOSE_NATIVE_STAGES = {
    "case_inputs",
    "pinned_version",
    "probe",
    "probe_projection",
    "runtime_registration",
    "arm",
    "early_release",
    "work_item",
    "launch",
    "failed_run",
    "held_response",
    "held_binding",
    "journal_read",
    "claim_binding",
    "no_effects",
    "wrong_releases",
    "exact_release",
    "evidence_write",
    "complete",
}
COMPOSE_DIAGNOSTIC_STAGES = {
    "shared": {"rootless_guards", "portal", "transition_proxy", "native_cases", "complete"},
    "pi": COMPOSE_NATIVE_STAGES,
    "opencode": COMPOSE_NATIVE_STAGES,
    "unprovisioned": {
        "rootless_guards",
        "fixture_files",
        "config",
        "empty_store",
        "request_contract",
        "fresh_barrier",
        "binary_access",
        "command",
        "owner_refusal",
        "empty_response",
        "store_unchanged",
        "barrier_unchanged",
        "evidence_write",
        "complete",
    },
}
COMPOSE_DIAGNOSTIC_OBSERVATIONS = {
    "probe": {"native_unverified", "unsupported_version", "unavailable", "cancelled", "deadline", "other"},
    "probe_projection": {"transport_verified", "transport_unverified"},
    "runtime_registration": {
        "portal_unavailable",
        "not_observed",
        "offline",
        "native_version_mismatch",
        "other_projection",
    },
    "failed_run": {
        "queued",
        "assigned",
        "claimed",
        "running",
        "paused",
        "waiting_for_input",
        "cancelling",
        "failed",
        "completed",
        "cancelled",
        "expired",
        "other",
        "runtime_mismatch",
        "assignment_missing",
        "refusal_mismatch",
    },
    "command": {"command_failed", "command_succeeded", "response_close_failed"},
    "owner_refusal": {"other_refusal"},
}
LEGACY_WITNESSES = (
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
LEGACY_HELPER_SCAN_CRASH_STAGES = {
    "helper_scan_failure_sigkill_before_cancel": {
        "daemon_task_setup",
        "running_journal",
        "descendant_marker",
        "trigger_fired_marker",
        "scan_failure_journal",
        "helper_exit",
        "cancel_request",
        "mirror_takeover",
        "cancel_api_409_state_conflict",
        "cancel_api_409_failed_run",
        "cancel_api_409_other",
        "cancel_api_other",
        "cancel_invalid_response",
        "cancel_timeout",
        "cancel_transport",
        "cancel_other_error",
        "mirror_authority",
        "mirror_stop_receipt_absent",
        "mirror_process_details",
        "mirror_scan_ledger",
        "mirror_unproven_scan",
        "post_takeover",
        "complete",
    },
    "helper_pending_scan_sigkill": {
        "daemon_task_setup",
        "running_journal",
        "descendant_marker",
        "trigger_fired_marker",
        "pending_scan_journal",
        "helper_exit",
        "cancel_request",
        "mirror_takeover",
        "cancel_api_409_state_conflict",
        "cancel_api_409_failed_run",
        "cancel_api_409_other",
        "cancel_api_other",
        "cancel_invalid_response",
        "cancel_timeout",
        "cancel_transport",
        "cancel_other_error",
        "mirror_authority",
        "mirror_stop_receipt_absent",
        "mirror_process_details",
        "mirror_scan_ledger",
        "mirror_pending_scan",
        "post_takeover",
        "complete",
    },
}
SAFE_STAGE_HOLDS = (
    "reservation_durable",
    "helper_bound",
    "gate_bound",
    "handoff_prepared",
    "handoff_committed",
    "lease_armed",
    "checkout_ready",
    "native_release",
    "stop_receipt_durable",
    "leaf_removed",
    "journal_cleared",
)
SAFE_ACK_LOSS_STAGES = (
    "helper_bound",
    "gate_bound",
    "handoff_prepared",
    "handoff_committed",
    "lease_armed",
    "checkout_ready",
    "stop_receipt_durable",
    "native_release",
    "journal_cleared",
)
MANUAL_CASE_SET_FILES = {
    "shared": {
        "pi-native-provider-action.json",
        "opencode-native-health.json",
        "pi-helper-sigkill-detached-child.json",
        "pi-daemon-sigkill-helper-survives.json",
        "receipt-persistence-failure.json",
        "literal-pid-reuse.json",
        "helper-stop-response-loss.json",
        "helper-ack-response-loss.json",
        "helper-launcher-response-loss.json",
        "helper-exit-after-release-before-register.json",
        "unprovisioned-owner-refusal.json",
        "control-provider-action-pi.json",
        "control-provider-action-helper-crash.json",
        "control-provider-action-daemon-crash.json",
        *(f"stage-crash-replay-{stage}.json" for stage in SAFE_STAGE_HOLDS),
        *(f"ack-loss-replay-{stage}.json" for stage in SAFE_ACK_LOSS_STAGES),
    },
    "stage_replay_create_started": {"stage-crash-replay-create_started.json"},
    "stage_replay_leaf_bound": {"stage-crash-replay-leaf_bound.json"},
    "stage_replay_launcher_bound": {"stage-crash-replay-launcher_bound.json"},
    "ack_loss_launcher_bound": {"ack-loss-replay-launcher_bound.json"},
    "stale_leaf": {"stale-leaf-identity.json"},
    "migration": {"migration-denial.json"},
    "alternate_mount_refusal": {"alternate-mount-refusal.json"},
    "stale_journal_run_key": {"unresolved-journal-guard.json"},
    "stale_journal_run_key_legacy": {"stale-run-key-denial.json"},
    "stale_journal_fence": {"stale-fence-denial.json"},
    "stale_journal_subject": {"stale-subject-denial.json"},
    "synthetic_goal_cleanup": {"synthetic-goal-cleanup.json"},
    "codex_containment": {"codex-native-containment.json", "codex-stage-crash-replay-native-release.json"},
    "claude_containment": {"claude-native-containment.json", "claude-stage-crash-replay-native-release.json"},
}
CONTAINMENT_KINDS = {
    "codex": ("codex_containment", "codex-native-containment.json"),
    "claude_code": ("claude_containment", "claude-native-containment.json"),
}
# The retargeted durability sets accept --native-kind pi; the cleanup case records the vehicle
# (fixture.runtime_kind) because no harness turn ever launches.
PI_DURABILITY_CASE_SETS = (
    "shared",
    "stage_replay_create_started",
    "stage_replay_leaf_bound",
    "stage_replay_launcher_bound",
    "ack_loss_launcher_bound",
    "stale_leaf",
    "migration",
    "alternate_mount_refusal",
    "stale_journal_run_key",
    "stale_journal_run_key_legacy",
    "stale_journal_fence",
    "stale_journal_subject",
    "synthetic_goal_cleanup",
)
# Five names in `shared`'s allowlist stay outside the pi durability binding: the three pi
# provider-action receipts and the two OpenCode-vehicle witnesses.
PI_VEHICLE_INDEPENDENT_WITNESSES = {
    "control-provider-action-pi.json",
    "control-provider-action-helper-crash.json",
    "control-provider-action-daemon-crash.json",
    "opencode-native-health.json",
    "unprovisioned-owner-refusal.json",
}
# The fixture records this declaration only when the ratified placeholder provider credential was
# staged into B; the named legs cannot run an authentic provider turn, so they are not proofs.
CREDENTIAL_GATED_DECLARATION_TARGET = "credential-gated-legs.json"
CREDENTIAL_GATED_MODE = "placeholder_provider_credential"
CREDENTIAL_GATED_CREDENTIAL_ENVIRONMENT_VARIABLE = "SYMMETRY_OPENAI_API_KEY"
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
CLEANUP_VEHICLE_WITNESS = "synthetic-goal-cleanup.json"
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
# The owner-witness harness keeps the App's bounded stderr beside its A-private artifacts; it is
# never evidence, so an input root that carries it refuses instead of risking publication.
EXCLUDED_WITNESS_NAMES = ("owner-witness.stderr",)
MAX_EVIDENCE_SIZE = 2 << 20
SAFE_TEXT = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:/+@ -]{0,255}$")
SAFE_HASH = re.compile(r"^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$")

METADATA_KEYS = {
    "subject",
    "tree",
    "tree_sha256",
    "go_version",
    "kernel_release",
    "goos",
    "goarch",
    "source_path",
    "source_sha256",
    "binary_path",
    "binary_sha256",
    "tree_digest",
    "dirty_status_sha256",
}
EXIT_KEYS = {"requested_signal", "signaled", "signal", "exit_code", "error"}
PROCESS_KEYS = {"at", "role", "pid", "identity", "state", "pgrp", "session", "start_time", "exists", "error"}
RECEIPT_KEYS = {
    "version",
    "legacy_v1_no_v2_proof",
    "status",
    "owner_kind",
    "owner_context_sha256",
    "target_pid",
    "target_identity",
    "pipe_token_sha256",
    "job_id_sha256",
    "supervisor_pid",
    "supervisor_identity",
    "active_processes",
}
OWNER_KEYS = {
    "owner_kind",
    "owner_context_sha256",
    "target_pid",
    "target_identity",
    "supervisor_pid",
    "supervisor_identity",
    "job_id_sha256",
    "has_stop_receipt",
}
SCAN_PROGRESS_KEYS = {
    "pending_sequence",
    "clean_result_sequence",
    "completed_sequence",
    "sealed_sequence",
    "unproven_sequence",
    "failure_kind",
}
SCAN_KEYS = {"target_pid", "target_identity", "supervisor_pid", "supervisor_identity", "helper", "mirror"}
JOURNAL_KEYS = {
    "at",
    "path",
    "raw_sha256",
    "run_id",
    "generation",
    "local_state",
    "terminal_state",
    "terminal_verdict",
    "pid",
    "process_identity",
    "started_at",
    "lease_expires_at",
    "has_process_details",
    "containment_unproven",
    "retain_workspace",
    "pending_events",
    "pending_transitions",
    "containment_scans",
    "stop_receipt",
    "handoff",
    "authority",
}
BARRIER_KEYS = {"run_id", "generation", "pid", "identity", "receipt"}
HELLO_ANCHOR_KEYS = {"target_pid", "target_identity", "target_pgrp", "target_session", "target_start_time"}
HELLO_KEYS = {"actual", "transmitted", "supervisor_pid", "supervisor_identity"}
PID_IDENTITY_KEYS = {"pid", "identity", "pgrp", "session", "start_time"}
PID_REUSE_CRASH_KEYS = {"stage", "status"}
PID_REUSE_KEYS = {
    "task_id",
    "parent_pid_namespace_inode",
    "private_pid_namespace_inode",
    "parent_network_namespace_inode",
    "private_network_namespace_inode",
    "control_url_sha256",
    "namespace_creation_flags",
    "namespace_allocator_path",
    "namespace_allocator_before",
    "namespace_allocator_reset_to",
    "namespace_allocator_readback",
    "pidfd_group_signal_supported",
    "original_target",
    "helper",
    "replacement",
    "owner_stop_before_proof_reached_path",
    "owner_stop_before_proof_release_path",
    "owner_stop_proof_result_path",
    "recovery_authenticated_marker_path",
    "owner_stop_before_proof_reached",
    "owner_stop_before_proof_reached_sha256",
    "owner_stop_before_proof_release",
    "owner_stop_proof_result",
    "recovery_helper_scan_seal_eligible",
    "recovery_helper_scan_seal_eligible_after_proof",
    "recovery_stop_failure_observed",
    "recovery_stop_failure_event",
    "recovery_runtime_registered",
    "crash_statuses",
    "recovery_daemon_shutdown",
    "assertions",
    "output_digests",
    "notes",
}
LEGACY_COMMON_KEYS = {
    "version",
    "case",
    "metadata",
    "case_root",
    "crash",
    "journal_snapshots",
    "process_snapshots",
    "raw_output",
    "notes",
}
LEGACY_SPINE_KEYS = LEGACY_COMMON_KEYS | {
    "stage",
    "task_id",
    "run_id",
    "generation",
    "journal_key",
    "barrier_reached_path",
    "barrier_release_path",
    "barrier_reached_contents",
    "barrier_release_contents",
    "scan_trigger_path",
    "scan_reached_path",
    "scan_helper_reached",
    "scan_mirror_reached",
    "scan_release_path",
    "scan_release_contents",
    "stop_receipt_reached_path",
    "stop_receipt_reached",
    "stop_receipt_release_path",
    "stop_receipt_release",
    "crash_after_stop_receipt",
    "stop_receipt_before_release",
}
LEGACY_NEGATIVE_KEYS = LEGACY_COMMON_KEYS | {"status", "helper_hello", "pid_reuse", "assertions"}
LEGACY_REPLAY_KEYS = LEGACY_COMMON_KEYS | {
    "stage",
    "task_id",
    "run_id",
    "generation",
    "journal_key",
    "crash_statuses",
    "raw_journal_snapshots",
    "receipt_before_clear",
    "replay_count",
    "exact_receipt_replay",
    "clear_observed",
    "response_drop_operation",
    "response_drop_marker",
    "response_drop_fired",
    "barrier_reached_path",
    "barrier_release_path",
    "barrier_reached",
    "barrier_release",
    "stop_receipt_barrier_reached_path",
    "stop_receipt_barrier_release_path",
    "stop_receipt_barrier_reached",
    "stop_receipt_barrier_release",
    "replay_receipt_barrier_reached_path",
    "replay_receipt_barrier_release_path",
    "replay_receipt_barrier_reached",
    "replay_receipt_barrier_release",
    "scan_trigger_path",
    "scan_reached_path",
    "scan_release_path",
    "scan_helper_reached",
    "scan_mirror_reached",
    "scan_release",
    "duplicate_stop_marker",
    "duplicate_stop_seen",
}

# The restarted daemon's stdout/stderr logs, written into each case root beside
# witness-evidence.json by every restarted-daemon start at
# daemon/e2e/production_linux_containment_witness_test.go:1723-1754
# (startLinuxResumeBarrierDaemonWitness),
# daemon/e2e/production_daemon_crash_witness_test.go:386-394
# (startProductionDaemonWitness) and
# daemon/e2e/production_linux_containment_replay_witness_test.go:754-762
# (startLinuxReplayDaemonWitness); the case exports a tail exactly when such a
# log exists, so a case that never restarted the daemon publishes nothing. Only
# the tail of a failed run is exported, as a sanitized projection of the
# daemon's own slog JSON records; the sources are never copied.
LEGACY_DAEMON_LOG_TAIL_BYTES = 64 << 10
LEGACY_DAEMON_LOG_LABEL = "second"
LEGACY_DAEMON_LOG_STREAMS = ("stdout", "stderr")
# The daemon's own slog.NewJSONHandler writer (daemon/internal/app/app.go:310)
# emits its JSON on stdout (daemon/cmd/symmetry-daemon/main.go:89). The
# projection publishes a text value only when it is one of the exact literals
# the daemon code can write into that field, so a credential-shaped value in
# one of these generic keys is dropped before publication, not masked:
#   msg   -- every daemon.log.<Level>("...") message literal under
#           daemon/internal/app (the files that carry daemon.log call sites).
#   state -- the terminal transition states (daemon/internal/state/state.go
#           isTerminalTransitionState) logged as stateName by
#           queueTerminalTransitionWithRetry / queueTerminalTransitionAndAcknowledgement
#           (outbox.go:166, 246); nativeTerminalPayload (goal_delivery.go:357) returns the
#           same three.
# Maintenance rule: a change that adds a producer or literal for one of these
# fields must extend the set in the same change; test-linux-v2-evidence.py
# scans the log call sites and the known producer call sites and fails when a
# literal is missing from its set. Values reaching these fields through a
# variable (for example a stateName passed down a helper, or the outcome values
# assigned from a literal at native_run.go:1584-1620, 2180-2210; commands.go:721
# and supervisory.go:140) are covered by this rule, not by that scan.
DAEMON_LOG_TIME = re.compile(
    r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-][0-9]{2}:[0-9]{2})$"
)
DAEMON_LOG_LEVELS = ("DEBUG", "INFO", "WARN", "ERROR")
DAEMON_LOG_MESSAGES = frozenset(
    {
        "advance_lease_expiry_failed",
        "claim_failed",
        "cleanup_recovered_workspace_failed",
        "cleanup_terminal_workspace_failed",
        "clear_native_usage_recovery_failed",
        "complete_provide_input_failed",
        "control_clock_skew_cleared",
        "control_clock_skew_detected",
        "delete_enrollment_intent_failed",
        "delete_stale_enrollment_intent_failed",
        "delete_terminal_journal_failed",
        "discard_unready_goal_session_attach_failed",
        "generate_cancelled_receipt_id_failed",
        "generate_claim_id_failed",
        "generate_command_acknowledgement_id_failed",
        "input_write_timeout",
        "linux_v2_native_assignment_blocked_by_owner_recovery",
        "list_cleanup_journals_failed",
        "list_journals_failed",
        "load_claim_intent_failed",
        "load_recovered_goal_session_failed",
        "load_unready_pi_resume_barrier_failed",
        "machine_enrolled",
        "mark_lease_expired_stale_failed",
        "mark_native_start_lease_expired_stale_failed",
        "mark_recovered_journal_stale_failed",
        "mark_workspace_recovery_required_failed",
        "native_goal_usage_retry_exhausted",
        "notification_client_stopped",
        "output_dropped_pending_budget",
        "persist_native_goal_usage_recovery_failed",
        "persist_native_terminal_stale_failed",
        "persist_prepared_startup_stale_failed",
        "persist_stale_process_exit_failed",
        "persist_startup_stale_failed",
        "queue_cancelled_transition_failed",
        "queue_command_acknowledgement_failed",
        "queue_completed_transition_failed",
        "queue_failed_transition_failed",
        "queue_native_goal_usage_before_cancel_failed",
        "queue_native_goal_usage_before_terminal_cancel_failed",
        "queue_native_goal_usage_failed",
        "queue_native_terminal_transition_failed",
        "queue_recovered_cancel_transition_failed",
        "queue_running_failed",
        "queue_terminal_transition_and_acknowledgement_failed",
        "queue_terminal_transition_failed",
        "record_recovered_cancel_process_stop_failed",
        "record_recovered_process_stop_failed",
        "record_workspace_failed",
        "recover_goal_session_attachment_failed",
        "recover_input_command_intents_failed",
        "recover_unclosed_goal_sessions_failed",
        "recover_unclosed_goal_sessions_pending",
        "recovered_cancel_process_stop_blocked_by_terminal",
        "recovered_input_process_stop_pending",
        "recovered_journal_stopped",
        "recovered_process_stop_blocked_by_terminal",
        "resolve_recovered_goal_session_stop_failed",
        "retain_recovered_cancel_workspace_failed",
        "retain_supervised_workspace_failed",
        "retain_unknown_goal_workspace_failed",
        "retry_native_goal_session_close_failed",
        "retry_native_goal_usage_failed",
        "runtime_heartbeat_failed",
        "runtime_poll_failed",
        "runtime_reconcile_failed",
        "runtime_registered",
        "save_claim_grant_failed",
        "save_claim_intent_failed",
        "schedule_process_exit_cleanup_failed",
        "stop_recovered_cancel_goal_session_unproven",
        "stop_recovered_cancel_process_failed",
        "stop_recovered_cancel_process_unavailable",
        "stop_recovered_goal_session_unproven",
        "stop_recovered_process_failed",
        "stop_recovered_process_unavailable",
    }
)
DAEMON_LOG_TEXT_VALUES = {
    "state": frozenset({"cancelled", "completed", "failed"}),
    "stage": frozenset(
        {
            "build_environment",
            "encode_input",
            "goal_admission",
            "goal_admission_required",
            "harness_capability",
            "initial_lease_deadline",
            "invalid_admission",
            "linux_v2_required",
            "mark_workspace_recovery_required",
            "prepare_workspace",
            "process_exit",
            "queue_running",
            "read_process_identity",
            "record_process",
            "start_agent",
        }
    ),
    "reason": frozenset(
        {
            "ack ownership lost",
            "cancel",
            "claim ownership lost",
            "continue",
            "event ownership lost",
            "lease expired",
            "lease expired during renewal",
            "lease fence is incomplete",
            "lease renewal failed",
            "local lease watchdog renewal failed",
            "not eligible for reconcile",
            "relative lease deadline elapsed",
            "relative lease deadline elapsed during renewal",
            "renewed lease has no safe local deadline",
            "renewed lease is already unsafe",
            "stale_stop",
            "terminal",
            "transition ownership lost",
            "unknown_stop",
        }
    ),
    "outcome": frozenset({"applied", "failed", "rejected"}),
    "kind": frozenset({"acknowledgement", "control_failure_event", "terminal transition", "transition"}),
}
DAEMON_LOG_INT_KEYS = ("attempt", "bytes", "generation", "lease_safety_margin_ms", "runtime_epoch", "skew_ms")
DAEMON_LOG_INT_LIMIT = 1 << 31


def fail(message: str) -> None:
    raise SystemExit(f"redacted evidence export failed: {message}")


def require_no_excluded_witness_file(
    roots: tuple[pathlib.Path, ...], sources: tuple[pathlib.Path, ...] = ()
) -> None:
    for root in roots:
        for name in EXCLUDED_WITNESS_NAMES:
            candidate = root / name
            if candidate.is_symlink() or candidate.exists():
                fail(f"{name} is a harness-private App stderr capture and is never exported")
    for source in sources:
        for name in EXCLUDED_WITNESS_NAMES:
            if name in source.parts:
                fail(f"{name} is a harness-private App stderr capture and is never exported")


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
        elif isinstance(value, str) and any(secret in value for secret in decoded_secrets):
            fail("allowlisted evidence contains a configured credential value")

    inspect(document)
    return document


def reject_unknown(document: dict, allowed: set[str], context: str) -> None:
    unknown = set(document) - allowed
    if unknown:
        fail(f"legacy {context} contains unknown fields: {', '.join(sorted(unknown))}")


COMPOSE_REFUSAL_PREDICATES = (
    "claim_fence_observed",
    "no_native_task_process",
    "no_task_result",
    "no_process_authority",
    "no_stop_receipt",
    "no_provider_access",
    "non_goal_work_item",
)
COMPOSE_REFUSAL_HASHES = ("work_item_id_sha256", "task_id_sha256", "run_id_sha256", "runtime_id_sha256")


def validate_compose_refusal(document: dict, harness: str) -> None:
    required = {
        "version",
        "case",
        "harness",
        "native_version",
        "generation",
        "run_state",
        "failure_stage",
        "probe_returned_err_native_unverified",
        "probe_returned_err_harness_unavailable",
        *COMPOSE_REFUSAL_PREDICATES,
        *COMPOSE_REFUSAL_HASHES,
    }
    if set(document) != required:
        fail("Compose refusal contains unexpected or missing fields")
    if (
        type(document["version"]) is not int
        or document["version"] != 1
        or document["case"] != "rootless_compose_native_work_item_refusal"
        or document["harness"] != harness
    ):
        fail("Compose refusal version, case or harness does not match its fixed filename")
    native_version = "1.5.2" if harness == "pi" else ""
    if (
        document["native_version"] != native_version
        or document["probe_returned_err_native_unverified"] is not (harness == "pi")
        or document["probe_returned_err_harness_unavailable"] is not (harness == "opencode")
    ):
        fail("Compose refusal does not match the production adapter availability projection")
    if (
        type(document["generation"]) is not int
        or not 0 < document["generation"] <= (1 << 63) - 1
        or document["run_state"] != "failed"
        or document["failure_stage"] != "linux_v2_required"
    ):
        fail("Compose refusal does not retain the exact durable failed Run boundary")
    for key in COMPOSE_REFUSAL_HASHES:
        if not isinstance(document[key], str) or not re.fullmatch(r"[a-f0-9]{64}", document[key]):
            fail("Compose refusal does not retain a redacted exact WorkItem/Run/runtime identity")
    if any(document[key] is not True for key in COMPOSE_REFUSAL_PREDICATES):
        fail("Compose refusal is missing a durable claim or no-process-authority predicate")


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
    if (
        not isinstance(outcome, str)
        or outcome not in {"passed", "failed"}
        or (outcome == "passed") != (stage == "complete")
    ):
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
            result[key] = (
                safe_hash(source[key], f"metadata.{key}")
                if key.endswith("sha256") or key == "tree_digest"
                else safe_text(source[key], f"metadata.{key}")
            )
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
        "version",
        "legacy_v1_no_v2_proof",
        "status",
        "target_pid",
        "target_identity",
        "pipe_token_sha256",
        "supervisor_pid",
        "supervisor_identity",
        "active_processes",
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
        elif (
            key == "supervisor_identity"
            and child == ""
            and handoff
            and source["supervisor_pid"] == 0
            and source["has_stop_receipt"] is False
        ):
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
        "raw_sha256",
        "generation",
        "local_state",
        "has_process_details",
        "containment_unproven",
        "retain_workspace",
        "pending_events",
        "pending_transitions",
    }
    missing = required - set(source)
    if missing:
        fail(f"legacy {context} omits required fields: {', '.join(sorted(missing))}")
    handoff = source.get("handoff")
    if (
        isinstance(handoff, dict)
        and handoff.get("supervisor_identity") == ""
        and (
            any(key in source for key in ("stop_receipt", "authority", "containment_scans"))
            or source["has_process_details"] is not False
            or source.get("pid", 0) != 0
            or source.get("process_identity", "") != ""
        )
    ):
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
    return {
        key: safe_text(child, f"{context}.{key}") if key == "target_identity" else safe_int(child, f"{context}.{key}")
        for key, child in source.items()
    }


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
    return {
        key: safe_text(child, f"{context}.{key}") if key == "identity" else safe_int(child, f"{context}.{key}")
        for key, child in source.items()
    }


def project_pid_reuse(value) -> dict:
    source = require_object(value, "pid_reuse")
    reject_unknown(source, PID_REUSE_KEYS, "pid_reuse")
    result = {}
    integer_fields = {
        "parent_pid_namespace_inode",
        "private_pid_namespace_inode",
        "parent_network_namespace_inode",
        "private_network_namespace_inode",
        "namespace_allocator_reset_to",
    }
    boolean_fields = {
        "pidfd_group_signal_supported",
        "owner_stop_before_proof_reached",
        "owner_stop_before_proof_release",
        "recovery_helper_scan_seal_eligible",
        "recovery_helper_scan_seal_eligible_after_proof",
        "recovery_stop_failure_observed",
        "recovery_runtime_registered",
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
                projected_item = require_object(item, f"pid_reuse.crash_statuses[{index}]")
                reject_unknown(projected_item, PID_REUSE_CRASH_KEYS, f"pid_reuse.crash_statuses[{index}]")
                crashes.append(
                    {
                        "stage": safe_text(projected_item["stage"], "pid_reuse crash stage"),
                        "status": project_exit(projected_item["status"], "pid_reuse crash status"),
                    }
                )
            result[key] = crashes
        elif key == "recovery_daemon_shutdown":
            result[key] = project_exit(child, "pid_reuse.recovery_daemon_shutdown")
        elif key == "output_digests":
            digests = require_object(child, "pid_reuse.output_digests")
            projected = {}
            for name, digest in digests.items():
                safe_name = safe_text(name, "pid_reuse output digest key")
                if not isinstance(digest, str) or not re.fullmatch(r"[0-9]+ bytes sha256[:=][a-fA-F0-9]{64}", digest):
                    fail("legacy output digest is not a byte-count and SHA-256")
                projected[safe_name] = digest.lower()
            result[key] = projected
        elif key in ("owner_stop_proof_result",):
            result[key] = safe_text(child, f"pid_reuse.{key}")
        # IDs, paths, allocator contents, event/log text, flags, and notes are intentionally private.
    return result


def project_daemon_log_record(value) -> dict | None:
    """Project one daemon slog line to its allowlisted, private-free shape.

    The record keeps its time and level whenever both match the shapes the
    daemon's own JSON log handler emits, so the daemon's activity timeline
    survives even when its message text cannot be published. Every other field
    is dropped: identities, paths, error text and any unknown attribute. `msg`
    and the five text fields survive only as literals the daemon code writes.
    """
    if not isinstance(value, dict):
        return None
    time_value = value.get("time")
    level = value.get("level")
    if not (
        isinstance(level, str)
        and level in DAEMON_LOG_LEVELS
        and isinstance(time_value, str)
        and DAEMON_LOG_TIME.fullmatch(time_value)
    ):
        return None
    record = {"time": time_value, "level": level}
    message = value.get("msg")
    if isinstance(message, str) and message in DAEMON_LOG_MESSAGES:
        record["msg"] = message
    for key, allowed in DAEMON_LOG_TEXT_VALUES.items():
        child = value.get(key)
        if isinstance(child, str) and child in allowed:
            record[key] = child
    for key in DAEMON_LOG_INT_KEYS:
        child = value.get(key)
        if (
            isinstance(child, int)
            and not isinstance(child, bool)
            and -DAEMON_LOG_INT_LIMIT < child < DAEMON_LOG_INT_LIMIT
        ):
            record[key] = child
    return record


def sanitize_daemon_log_tail(raw: bytes) -> tuple[list[dict], int]:
    messages = []
    dropped = 0
    for line in raw.split(b"\n"):
        if not line:
            continue
        try:
            value = json.loads(line.decode("utf-8", errors="strict"), object_pairs_hook=reject_duplicate_keys)
        except (UnicodeError, ValueError, json.JSONDecodeError):
            value = None
        record = None if value is None else project_daemon_log_record(value)
        if record is None:
            dropped += 1
            continue
        messages.append(record)
    return messages, dropped


def build_daemon_log_document(case: str, stream: str, raw: bytes, truncated: bool) -> bytes:
    messages, dropped = sanitize_daemon_log_tail(raw)
    document = {
        "version": 1,
        "case": case,
        "daemon": LEGACY_DAEMON_LOG_LABEL,
        "stream": stream,
        "truncated": truncated,
        "dropped_lines": dropped,
        "messages": messages,
    }
    return json.dumps(document, separators=(",", ":"), sort_keys=True).encode("utf-8") + b"\n"


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
        if (
            result["case"] in LEGACY_HELPER_SCAN_CRASH_STAGES
            and result["stage"] not in LEGACY_HELPER_SCAN_CRASH_STAGES[result["case"]]
        ):
            fail("legacy helper scan crash stage is not allowlisted")
    if "status" in document:
        result["status"] = safe_text(document["status"], "status")
    for key in ("crash", "crash_after_stop_receipt"):
        if key in document:
            result[key] = project_exit(document[key], key)
    if "crash_statuses" in document:
        if not isinstance(document["crash_statuses"], list):
            fail("legacy crash_statuses must be a list")
        result["crash_statuses"] = [
            project_exit(item, f"crash_statuses[{index}]") for index, item in enumerate(document["crash_statuses"])
        ]
    journal_snapshots = document["journal_snapshots"]
    process_snapshots = document["process_snapshots"]
    if journal_snapshots is not None and not isinstance(journal_snapshots, list):
        fail("legacy journal_snapshots must be a list or null")
    if process_snapshots is not None and not isinstance(process_snapshots, list):
        fail("legacy process_snapshots must be a list or null")
    result["journal_snapshots"] = (
        None
        if journal_snapshots is None
        else [project_journal(item, f"journal_snapshots[{index}]") for index, item in enumerate(journal_snapshots)]
    )
    result["process_snapshots"] = (
        None
        if process_snapshots is None
        else [project_process(item, f"process_snapshots[{index}]") for index, item in enumerate(process_snapshots)]
    )
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


def read_regular(
    path: pathlib.Path,
    secret_values: tuple[bytes, ...],
    validate: bool = True,
    diagnostic_owner: tuple[int, int] | None = None,
) -> tuple[bytes, dict | None]:
    maximum_size = 2048 if diagnostic_owner is not None else MAX_EVIDENCE_SIZE
    info = path.lstat()
    if (
        not stat.S_ISREG(info.st_mode)
        or stat.S_IMODE(info.st_mode) != 0o600
        or info.st_size <= 0
        or info.st_size > maximum_size
    ):
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
            if diagnostic_owner is not None and (
                (opened.st_uid, opened.st_gid) != diagnostic_owner
                or opened.st_nlink != 1
                or stat.S_IMODE(opened.st_mode) != 0o600
                or opened.st_size > maximum_size
            ):
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


def read_daemon_log_tail(path: pathlib.Path, maximum: int) -> tuple[bytes, bool]:
    """Read at most the last `maximum` bytes of one private daemon log.

    Unlike read_regular this file may be empty and its total size is not
    capped: only the tail is published, and the daemon that wrote it may still
    be appending. The identity re-check keeps the same symlink/short-write
    boundary every other allowlisted source passes; when the read starts
    mid-line, the partial first line is discarded before sanitization.
    """
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600:
        fail("allowlisted daemon log must be a private regular file")
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0)
    try:
        fd = os.open(path, flags)
        try:
            opened = os.fstat(fd)
            if not stat.S_ISREG(opened.st_mode) or opened.st_ino != info.st_ino or opened.st_dev != info.st_dev:
                fail("allowlisted daemon log changed during open")
            truncated = opened.st_size > maximum
            os.lseek(fd, max(opened.st_size - maximum, 0), os.SEEK_SET)
            data = bytearray()
            while len(data) < maximum:
                chunk = os.read(fd, maximum - len(data))
                if not chunk:
                    break
                data.extend(chunk)
        finally:
            os.close(fd)
    except OSError:
        fail("allowlisted daemon log could not be read")
    raw = bytes(data)
    if truncated:
        boundary = raw.find(b"\n")
        raw = raw[boundary + 1 :] if boundary >= 0 else b""
    return raw, truncated


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
        with contextlib.suppress(FileNotFoundError):
            os.unlink(temporary_name)


def validate_pi_durability_witnesses(
    case_set: str,
    documents: dict,
    binary: str,
    binary_sha256: str,
    version_line: str,
    source_tree_digest: str,
    credential_gated_targets: frozenset = frozenset(),
) -> None:
    if not credential_gated_targets <= MANUAL_CASE_SET_FILES[case_set]:
        fail("credential-gated declaration names a witness outside its case set")
    # A declared credential-gated witness is a recorded unproven outcome, never a staged-pi proof.
    expected = MANUAL_CASE_SET_FILES[case_set] - PI_VEHICLE_INDEPENDENT_WITNESSES - credential_gated_targets
    for target in sorted(expected):
        document = documents.get(target)
        if document is None:
            fail(f"pi durability witness is unavailable: {target}")
        if target == CLEANUP_VEHICLE_WITNESS:
            fixture = document.get("fixture")
            if not isinstance(fixture, dict) or fixture.get("runtime_kind") != "pi":
                fail(f"{target} does not record the pi vehicle runtime_kind")
            continue
        subject = document.get("subject")
        if (
            document.get("harness") != "pi"
            or document.get("native_binary") != binary
            or document.get("native_binary_sha256") != binary_sha256
            or document.get("native_version_line") != version_line
        ):
            fail(f"{target} pi durability witness does not bind the staged pi executable")
        if not isinstance(subject, dict) or subject.get("tree_digest") != source_tree_digest:
            fail(f"{target} pi durability witness does not bind --source-tree-digest")


def validate_credential_gated_declaration(document: dict, source_tree_digest: str) -> tuple[str, ...]:
    if set(document) != {"version", "mode", "credential_environment_variable", "source_tree_digest", "legs"}:
        fail("credential-gated declaration has unexpected or missing fields")
    if (
        document["version"] != 1
        or document["mode"] != CREDENTIAL_GATED_MODE
        or document["credential_environment_variable"] != CREDENTIAL_GATED_CREDENTIAL_ENVIRONMENT_VARIABLE
    ):
        fail("credential-gated declaration version, mode or credential variable does not match its fixed shape")
    if document["source_tree_digest"] != source_tree_digest:
        fail("credential-gated declaration does not bind --source-tree-digest")
    legs = document["legs"]
    if not isinstance(legs, list) or len(legs) != len(CREDENTIAL_GATED_LEGS):
        fail("credential-gated declaration must name exactly the pi authentic-provider legs")
    recorded = []
    for leg in legs:
        if not isinstance(leg, dict) or set(leg) != {"case", "test", "outcome"}:
            fail("credential-gated declaration leg has unexpected or missing fields")
        if not all(isinstance(leg[field], str) for field in ("case", "test", "outcome")):
            fail("credential-gated declaration leg fields must be strings")
        recorded.append((leg["case"], leg["test"], leg["outcome"]))
    if sorted(recorded) != sorted((case, test, outcome) for case, test, outcome, _ in CREDENTIAL_GATED_LEGS):
        fail("credential-gated declaration legs do not match the exact pi authentic-provider legs")
    return tuple(sorted(case for case, _, _ in recorded))


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
    parser.add_argument("--native-kind")
    parser.add_argument("--native-binary")
    parser.add_argument("--native-binary-sha256")
    parser.add_argument("--native-archive-sha256")
    parser.add_argument("--native-version-line")
    parser.add_argument("--source-tree-digest")
    parser.add_argument("--credential-gated-declaration")
    arguments = parser.parse_args()

    if arguments.credential_gated_declaration is not None:
        if arguments.mode != "manual" or arguments.case_set != "shared":
            fail("a credential-gated declaration is only defined for the manual shared case set")
        if arguments.run_status != "failed":
            fail("a credential-gated case set can never be exported as a passed run")
        if arguments.native_kind != "pi":
            fail("a credential-gated declaration requires --native-kind pi")

    native_kind = arguments.native_kind
    native_binary = arguments.native_binary
    native_binary_sha256 = arguments.native_binary_sha256
    native_archive_sha256 = arguments.native_archive_sha256
    native_version_line = arguments.native_version_line
    native_values = (native_kind, native_binary, native_binary_sha256, native_archive_sha256, native_version_line)
    if any(value is not None for value in native_values) and not all(value is not None for value in native_values):
        fail(
            "native containment export requires --native-kind, --native-binary, --native-binary-sha256, "
            "--native-archive-sha256 and --native-version-line together"
        )
    if native_kind is not None and native_kind not in CONTAINMENT_KINDS and native_kind != "pi":
        fail(f"--native-kind {native_kind} is not a supported native kind")
    if native_kind in CONTAINMENT_KINDS and arguments.case_set != CONTAINMENT_KINDS[native_kind][0]:
        fail(f"--native-kind {native_kind} requires --case-set {CONTAINMENT_KINDS[native_kind][0]}")
    if native_kind == "pi" and arguments.case_set not in PI_DURABILITY_CASE_SETS:
        fail(f"--native-kind pi requires a retargeted durability case set, not {arguments.case_set}")
    if native_kind is not None and arguments.mode != "manual":
        fail("native containment export is available only in manual mode")
    if arguments.source_tree_digest is not None and not re.fullmatch(
        r"sha256:[0-9a-f]{64}", arguments.source_tree_digest
    ):
        fail("--source-tree-digest must be a sha256:<64 hex> tree digest")
    if native_kind is not None and arguments.source_tree_digest is None:
        fail("native containment export requires --source-tree-digest")

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
            (source, target) for source, target in expected if target in MANUAL_CASE_SET_FILES[arguments.case_set]
        ]
        require_no_excluded_witness_file((witness_root, receipt_root), tuple(source for source, _ in expected))
    elif arguments.mode == "compose":
        if not arguments.witness_root:
            fail("Compose mode requires the private witness staging directory")
        witness_root = pathlib.Path(arguments.witness_root)
        expected.extend((witness_root / name, name) for name in COMPOSE_WITNESSES)
        expected.extend((witness_root / name, name) for name in COMPOSE_DIAGNOSTICS)
        require_no_excluded_witness_file((witness_root,), tuple(source for source, _ in expected))
    else:
        if not arguments.witness_root:
            fail("legacy mode requires the private witness root")
        if arguments.receipt_root or arguments.case_set:
            fail("legacy mode accepts only --witness-root and optional credential scan input")
        witness_root = pathlib.Path(arguments.witness_root)
        expected.extend((witness_root / name / "witness-evidence.json", f"{name}.json") for name in LEGACY_WITNESSES)
        require_no_excluded_witness_file((witness_root,), tuple(source for source, _ in expected))

    credential_gated_cases: tuple[str, ...] = ()
    credential_gated_targets = {}
    if arguments.credential_gated_declaration is not None:
        try:
            declaration_data, declaration = read_regular(
                pathlib.Path(arguments.credential_gated_declaration), secret_values
            )
        except FileNotFoundError:
            fail("credential-gated declaration is unavailable")
        credential_gated_cases = validate_credential_gated_declaration(declaration, arguments.source_tree_digest)
        credential_gated_targets = {target: (case, test) for case, test, _, target in CREDENTIAL_GATED_LEGS}

    copied = []
    missing = []
    incomplete_files = []
    failed_diagnostics = []
    published_documents = {}
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
            elif arguments.mode == "compose" and target in (
                "rootless-compose-pi.json",
                "rootless-compose-opencode.json",
            ):
                data, document = read_regular(source, secret_values)
                validate_compose_refusal(document, "pi" if target == "rootless-compose-pi.json" else "opencode")
            else:
                data, document = read_regular(source, secret_values)
                if target in credential_gated_targets:
                    case, test = credential_gated_targets[target]
                    if case != "pi_provider_action":
                        fail(f"{target} was not run in the credential-gated case set")
                    subject = document.get("subject")
                    if (
                        document.get("harness") != "pi"
                        or document.get("case") != test
                        or not isinstance(subject, dict)
                        or subject.get("tree_digest") != arguments.source_tree_digest
                        or document.get("provider_action_succeeded") is not False
                    ):
                        fail(f"{target} does not bind the declared pi provider failure")
                if native_kind is not None:
                    published_documents[target] = document
        except FileNotFoundError:
            missing.append(target)
            continue
        if source != destination:
            if destination.exists() or destination.is_symlink():
                fail("evidence destination already exists")
            atomic_write(output, target, data, runner_uid, runner_gid)
        copied.append({"file": target, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()})

    if credential_gated_cases:
        destination = output / CREDENTIAL_GATED_DECLARATION_TARGET
        if destination.exists() or destination.is_symlink():
            fail("evidence destination already exists")
        atomic_write(output, CREDENTIAL_GATED_DECLARATION_TARGET, declaration_data, runner_uid, runner_gid)
        copied.append(
            {
                "file": CREDENTIAL_GATED_DECLARATION_TARGET,
                "bytes": len(declaration_data),
                "sha256": hashlib.sha256(declaration_data).hexdigest(),
            }
        )
        expected_destinations.add(CREDENTIAL_GATED_DECLARATION_TARGET)

    if native_kind in CONTAINMENT_KINDS:
        document = published_documents.get(CONTAINMENT_KINDS[native_kind][1])
        if document is None:
            fail("containment witness is unavailable")
        containment = document.get("containment")
        if document.get("native_binary") != native_binary:
            fail("containment witness native_binary does not match --native-binary")
        if document.get("native_binary_sha256") != native_binary_sha256:
            fail("containment witness native_binary_sha256 does not match --native-binary-sha256")
        if document.get("native_version_line") != native_version_line:
            fail("containment witness native_version_line does not match --native-version-line")
        if document.get("subject", {}).get("tree_digest") != arguments.source_tree_digest:
            fail("containment witness subject.tree_digest does not match --source-tree-digest")
        if not isinstance(containment, dict) or not isinstance(containment.get("native_open_reached"), bool):
            fail("containment witness does not record a boolean containment.native_open_reached")
        replay_names = MANUAL_CASE_SET_FILES[arguments.case_set] - {CONTAINMENT_KINDS[native_kind][1]}
        if len(replay_names) != 1:
            fail("containment case set does not publish exactly one per-kind replay witness")
        replay_document = published_documents.get(next(iter(replay_names)))
        if replay_document is None:
            fail("containment replay witness is unavailable")
        if (
            replay_document.get("harness") != native_kind
            or replay_document.get("native_version_line") != native_version_line
        ):
            fail("containment replay witness does not bind the pinned kind and version line")
    elif native_kind == "pi":
        validate_pi_durability_witnesses(
            arguments.case_set,
            published_documents,
            native_binary,
            native_binary_sha256,
            native_version_line,
            arguments.source_tree_digest,
            frozenset(credential_gated_targets),
        )

    # A failed witness run is the one case whose daemon-side stall the uploaded
    # evidence must explain, so the restarted daemon's log tails are published
    # as sanitized projections. Per-case verdicts are not visible here (the
    # exporter reads files, not the go-test event stream), so the CI's run
    # verdict is the trigger and every case that wrote a restarted-daemon log
    # gets its tail exported.
    if arguments.mode == "legacy" and arguments.run_status == "failed":
        for name in LEGACY_WITNESSES:
            for stream in LEGACY_DAEMON_LOG_STREAMS:
                try:
                    source = witness_root / name / f"{LEGACY_DAEMON_LOG_LABEL}-daemon.{stream}.log"
                    raw, truncated = read_daemon_log_tail(source, LEGACY_DAEMON_LOG_TAIL_BYTES)
                except FileNotFoundError:
                    continue
                target = f"{name}-{LEGACY_DAEMON_LOG_LABEL}-daemon-{stream}.json"
                data = build_daemon_log_document(name, stream, raw, truncated)
                document = validate_document(data, secret_values)
                destination = output / target
                if destination.exists() or destination.is_symlink():
                    fail("evidence destination already exists")
                atomic_write(output, target, data, runner_uid, runner_gid)
                copied.append(
                    {
                        "file": target,
                        "bytes": len(data),
                        "sha256": hashlib.sha256(data).hexdigest(),
                        "kind": "daemon_log_tail",
                        "daemon": document["daemon"],
                        "stream": document["stream"],
                        "truncated": document["truncated"],
                        "records": len(document["messages"]),
                        "dropped_lines": document["dropped_lines"],
                    }
                )
                expected_destinations.add(target)

    for child in output.iterdir():
        if child.name not in expected_destinations and child.name != "manifest.json":
            fail("output directory contains a file outside the allowlist")

    if arguments.mode == "manual" and arguments.case_set == "synthetic_goal_cleanup":
        scope = (
            "INSTALLED SYNTHETIC-GOAL CLEANUP; direct test-scoped Runner/Store/Workspace, "
            "not App end-to-end or Goal/Control acceptance"
        )
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
    if credential_gated_cases:
        manifest["credential_gated"] = {
            "mode": CREDENTIAL_GATED_MODE,
            "declaration": CREDENTIAL_GATED_DECLARATION_TARGET,
            "legs": list(credential_gated_cases),
        }
    if native_kind is not None:
        manifest["native_binary"] = {
            "kind": native_kind,
            "path": native_binary,
            "sha256": native_binary_sha256,
            "archive_sha256": native_archive_sha256,
            "version": native_version_line,
        }
        manifest["source_tree_digest"] = arguments.source_tree_digest
    if native_kind in CONTAINMENT_KINDS:
        manifest["containment"] = {"native_open_reached": document["containment"]["native_open_reached"]}
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
