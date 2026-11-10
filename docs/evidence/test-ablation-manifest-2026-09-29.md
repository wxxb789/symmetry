# Project test inventory manifest

Captured 2026-09-29 from Git tracked and non-ignored untracked paths on the dirty working tree at HEAD `d14debbf6ae3ded09e5013ef90952d347c01474d`. Session artifacts are excluded. This is a coverage manifest, not proof that a test ran or that a file is redundant. Workers must reconcile their discovered test declarations and entrypoints with this list.

## Test files (243)

| Path | Inventory owner |
|---|---|
| `browser/tests/portal-chat-live.spec.mjs` | Tooling |
| `browser/tests/portal-chat.spec.mjs` | Tooling |
| `browser/tests/portal-compose-daemon.spec.mjs` | Tooling |
| `browser/tests/portal-compose-restart.spec.mjs` | Tooling |
| `browser/tests/portal-connections.spec.mjs` | Tooling |
| `browser/tests/portal-live-daemon.spec.mjs` | Tooling |
| `browser/tests/portal.spec.mjs` | Tooling |
| `contracts/scripts/boundary.test.mjs` | Tooling |
| `contracts/scripts/generate-effect.test.mjs` | Tooling |
| `control/test/symmetry_control/chat_test.exs` | Control |
| `control/test/symmetry_control/config/runtime_test.exs` | Control |
| `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs` | Control |
| `control/test/symmetry_control/goals/contract_validation_test.exs` | Control |
| `control/test/symmetry_control/goals/dispatch_notification_test.exs` | Control |
| `control/test/symmetry_control/goals/validation_profiles_test.exs` | Control |
| `control/test/symmetry_control/goals/workers_test.exs` | Control |
| `control/test/symmetry_control/goals_external_wait_schema_test.exs` | Control |
| `control/test/symmetry_control/goals_read_model_test.exs` | Control |
| `control/test/symmetry_control/goals_schema_test.exs` | Control |
| `control/test/symmetry_control/goals_test.exs` | Control |
| `control/test/symmetry_control/health_test.exs` | Control |
| `control/test/symmetry_control/integrations/auth_test.exs` | Control |
| `control/test/symmetry_control/integrations/azure_devops_test.exs` | Control |
| `control/test/symmetry_control/integrations/change_action_test.exs` | Control |
| `control/test/symmetry_control/integrations/command_test.exs` | Control |
| `control/test/symmetry_control/integrations/github_test.exs` | Control |
| `control/test/symmetry_control/integrations/syncer_test.exs` | Control |
| `control/test/symmetry_control/integrations_test.exs` | Control |
| `control/test/symmetry_control/migrations/add_history_lookup_indexes_test.exs` | Control |
| `control/test/symmetry_control/migrations/add_retry_commands_test.exs` | Control |
| `control/test/symmetry_control/migrations/add_task_command_ownership_test.exs` | Control |
| `control/test/symmetry_control/migrations/goal_0006_migration_test.exs` | Control |
| `control/test/symmetry_control/migrations/goal_external_wait_migration_test.exs` | Control |
| `control/test/symmetry_control/migrations/version_request_hashes_test.exs` | Control |
| `control/test/symmetry_control/orchestration/capability_scheduling_test.exs` | Control |
| `control/test/symmetry_control/orchestration/scheduler_test.exs` | Control |
| `control/test/symmetry_control/orchestration_goal_admission_test.exs` | Control |
| `control/test/symmetry_control/orchestration_request_hash_test.exs` | Control |
| `control/test/symmetry_control/orchestration_retry_test.exs` | Control |
| `control/test/symmetry_control/orchestration_review_test.exs` | Control |
| `control/test/symmetry_control/orchestration_supervision_test.exs` | Control |
| `control/test/symmetry_control/orchestration_telemetry_test.exs` | Control |
| `control/test/symmetry_control/orchestration_test.exs` | Control |
| `control/test/symmetry_control/pi_control_e2e_test.exs` | Control |
| `control/test/symmetry_control/provider_broker_e2e_test.exs` | Control |
| `control/test/symmetry_control/request_hash_test.exs` | Control |
| `control/test/symmetry_control/workspaces_completion_test.exs` | Control |
| `control/test/symmetry_control/workspaces_read_model_test.exs` | Control |
| `control/test/symmetry_control/workspaces_test.exs` | Control |
| `control/test/symmetry_control_web/channels/daemon_channel_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/chat_controller_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/error_json_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/goal_controller_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/goal_machine_controller_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/health_controller_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/operator_read_controller_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/portal_controller_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/portal_integrations_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/portal_resource_identity_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/portal_workflow_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/protocol_controller_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/provider_action_controller_test.exs` | Control |
| `control/test/symmetry_control_web/controllers/task_goal_guard_controller_test.exs` | Control |
| `control/test/symmetry_control_web/plugs/strict_evidence_json_test.exs` | Control |
| `control/test/symmetry_control_web/portal_html_test.exs` | Control |
| `control/test/symmetry_control_web/portal_session_test.exs` | Control |
| `daemon/cmd/symmetry-daemon/main_test.go` | Go surfaces |
| `daemon/cmd/symmetry-fake-agent/autonomous_test.go` | Go surfaces |
| `daemon/cmd/symmetry-fake-agent/main_test.go` | Go surfaces |
| `daemon/cmd/symmetry-linux-launcher/main_test.go` | Go surfaces |
| `daemon/e2e/core_test.go` | Go surfaces |
| `daemon/e2e/production_daemon_crash_witness_test.go` | Go surfaces |
| `daemon/e2e/production_linux_containment_negative_witness_test.go` | Go surfaces |
| `daemon/e2e/production_linux_containment_replay_witness_test.go` | Go surfaces |
| `daemon/e2e/production_linux_containment_witness_test.go` | Go surfaces |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | Go surfaces |
| `daemon/e2e/production_linux_v2_helper_exit_witness_test.go` | Go surfaces |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | Go surfaces |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | Go surfaces |
| `daemon/e2e/production_linux_v2_rootless_compose_witness_test.go` | Go surfaces |
| `daemon/e2e/production_pre_authority_crash_witness_test.go` | Go surfaces |
| `daemon/internal/app/app_test.go` | Go core |
| `daemon/internal/app/app_unsupported_test.go` | Go core |
| `daemon/internal/app/atomic_terminal_ack_interleaving_test.go` | Go core |
| `daemon/internal/app/cleanup_test.go` | Go core |
| `daemon/internal/app/control_failure_test.go` | Go core |
| `daemon/internal/app/decision_liveness_test.go` | Go core |
| `daemon/internal/app/decision_requirement_test.go` | Go core |
| `daemon/internal/app/deterministic_validation_test.go` | Go core |
| `daemon/internal/app/generic_process_stop_test.go` | Go core |
| `daemon/internal/app/goal_delivery_test.go` | Go core |
| `daemon/internal/app/harness_test.go` | Go core |
| `daemon/internal/app/lease_renewal_test.go` | Go core |
| `daemon/internal/app/lease_stale_persistence_test.go` | Go core |
| `daemon/internal/app/linux_owner_preflight_linux_test.go` | Go core |
| `daemon/internal/app/linux_owner_v2_app_test.go` | Go core |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | Go core |
| `daemon/internal/app/linux_owner_witness_unsupported_test.go` | Go core |
| `daemon/internal/app/native_lease_settlement_test.go` | Go core |
| `daemon/internal/app/native_process_stop_test.go` | Go core |
| `daemon/internal/app/native_settlement_final_regression_test.go` | Go core |
| `daemon/internal/app/native_settlement_regression_test.go` | Go core |
| `daemon/internal/app/native_start_attachment_barrier_test.go` | Go core |
| `daemon/internal/app/output_settlement_test.go` | Go core |
| `daemon/internal/app/pi_provider_bridge_durable_test.go` | Go core |
| `daemon/internal/app/pi_provider_bridge_test.go` | Go core |
| `daemon/internal/app/prelaunch_recovery_fault_test.go` | Go core |
| `daemon/internal/app/process_cleanup_test.go` | Go core |
| `daemon/internal/app/process_finalization_test.go` | Go core |
| `daemon/internal/app/process_persistence_test.go` | Go core |
| `daemon/internal/app/recovered_stop_witness_test.go` | Go core |
| `daemon/internal/app/recovery_handoff_test.go` | Go core |
| `daemon/internal/app/recovery_isolation_test.go` | Go core |
| `daemon/internal/app/recovery_linux_test.go` | Go core |
| `daemon/internal/app/recovery_receipt_release_test.go` | Go core |
| `daemon/internal/app/recovery_registration_test.go` | Go core |
| `daemon/internal/app/retention_failure_test.go` | Go core |
| `daemon/internal/app/stale_ack_settlement_test.go` | Go core |
| `daemon/internal/app/supervisory_test.go` | Go core |
| `daemon/internal/app/terminal_settlement_race_test.go` | Go core |
| `daemon/internal/app/terminate_linux_test.go` | Go core |
| `daemon/internal/app/terminate_windows_test.go` | Go core |
| `daemon/internal/app/timestamp_test.go` | Go core |
| `daemon/internal/authority/authority_linux_clone_test.go` | Go surfaces |
| `daemon/internal/authority/authority_test.go` | Go surfaces |
| `daemon/internal/authority/authority_v2_test.go` | Go surfaces |
| `daemon/internal/config/config_harness_test.go` | Go surfaces |
| `daemon/internal/config/config_test.go` | Go surfaces |
| `daemon/internal/config/supervisory_test.go` | Go surfaces |
| `daemon/internal/contracts/schema_test.go` | Go surfaces |
| `daemon/internal/control/client_review_test.go` | Go surfaces |
| `daemon/internal/control/client_test.go` | Go surfaces |
| `daemon/internal/control/goal_client_test.go` | Go surfaces |
| `daemon/internal/control/goal_receipt_e2e_test.go` | Go surfaces |
| `daemon/internal/control/provider_action_client_test.go` | Go surfaces |
| `daemon/internal/control/supervisory_validation_test.go` | Go surfaces |
| `daemon/internal/control/validation_test.go` | Go surfaces |
| `daemon/internal/execution/containment_receipt_test.go` | Go surfaces |
| `daemon/internal/execution/identity_linux_test.go` | Go surfaces |
| `daemon/internal/execution/identity_windows_test.go` | Go surfaces |
| `daemon/internal/execution/linux_owner_v2_test.go` | Go surfaces |
| `daemon/internal/execution/linux_owner_v2_unsupported_test.go` | Go surfaces |
| `daemon/internal/execution/probe_process_exists_unix_test.go` | Go surfaces |
| `daemon/internal/execution/probe_process_exists_windows_test.go` | Go surfaces |
| `daemon/internal/execution/probe_test.go` | Go surfaces |
| `daemon/internal/execution/runner_lifecycle_test.go` | Go surfaces |
| `daemon/internal/execution/runner_output_truncation_test.go` | Go surfaces |
| `daemon/internal/execution/runner_result_race_test.go` | Go surfaces |
| `daemon/internal/execution/runner_stop_receipt_p1_test.go` | Go surfaces |
| `daemon/internal/execution/runner_termination_order_windows_test.go` | Go surfaces |
| `daemon/internal/execution/runner_test.go` | Go surfaces |
| `daemon/internal/execution/runner_unix_test.go` | Go surfaces |
| `daemon/internal/execution/runner_windows_test.go` | Go surfaces |
| `daemon/internal/harness/claude/stream_test.go` | Go surfaces |
| `daemon/internal/harness/claude_candidate_adapter_test.go` | Go surfaces |
| `daemon/internal/harness/claude_candidate_additional_test.go` | Go surfaces |
| `daemon/internal/harness/claude_candidate_cmdline_windows_test.go` | Go surfaces |
| `daemon/internal/harness/claude_candidate_native_test.go` | Go surfaces |
| `daemon/internal/harness/claude_candidate_structured_output_test.go` | Go surfaces |
| `daemon/internal/harness/claude_probe_test.go` | Go surfaces |
| `daemon/internal/harness/codex/adapter_native_test.go` | Go surfaces |
| `daemon/internal/harness/codex/adapter_test.go` | Go surfaces |
| `daemon/internal/harness/codex/framing_test.go` | Go surfaces |
| `daemon/internal/harness/codex/probe_test.go` | Go surfaces |
| `daemon/internal/harness/codex/rpc_sandbox_test.go` | Go surfaces |
| `daemon/internal/harness/codex_probe_test.go` | Go surfaces |
| `daemon/internal/harness/harness_test.go` | Go surfaces |
| `daemon/internal/harness/opencode/adapter_native_terminal_candidate_test.go` | Go surfaces |
| `daemon/internal/harness/opencode/adapter_native_test.go` | Go surfaces |
| `daemon/internal/harness/opencode/adapter_native_work_test.go` | Go surfaces |
| `daemon/internal/harness/opencode/adapter_test.go` | Go surfaces |
| `daemon/internal/harness/opencode/client_test.go` | Go surfaces |
| `daemon/internal/harness/opencode/peer_linux_test.go` | Go surfaces |
| `daemon/internal/harness/opencode/peer_test.go` | Go surfaces |
| `daemon/internal/harness/opencode/sse_test.go` | Go surfaces |
| `daemon/internal/harness/pi/adapter_native_resume_test.go` | Go surfaces |
| `daemon/internal/harness/pi/adapter_native_test.go` | Go surfaces |
| `daemon/internal/harness/pi/adapter_native_work_test.go` | Go surfaces |
| `daemon/internal/harness/pi/adapter_test.go` | Go surfaces |
| `daemon/internal/harness/pi/argv_test.go` | Go surfaces |
| `daemon/internal/harness/pi/probe_test.go` | Go surfaces |
| `daemon/internal/harness/pi/provider_bridge_peer_linux_test.go` | Go surfaces |
| `daemon/internal/harness/pi/provider_bridge_peer_test.go` | Go surfaces |
| `daemon/internal/harness/pi/provider_bridge_test.go` | Go surfaces |
| `daemon/internal/harness/pi/provider_extension_native_test.go` | Go surfaces |
| `daemon/internal/harness/pi/provider_extension_test.go` | Go surfaces |
| `daemon/internal/harness/pi/rpc_test.go` | Go surfaces |
| `daemon/internal/notification/client_test.go` | Go surfaces |
| `daemon/internal/platform/containment_supervisor_linux_test.go` | Go surfaces |
| `daemon/internal/platform/containment_supervisor_production_windows_test.go` | Go surfaces |
| `daemon/internal/platform/containment_supervisor_receipt_windows_test.go` | Go surfaces |
| `daemon/internal/platform/containment_supervisor_windows_test.go` | Go surfaces |
| `daemon/internal/platform/directory_path_other_test.go` | Go surfaces |
| `daemon/internal/platform/directory_path_test.go` | Go surfaces |
| `daemon/internal/platform/directory_path_windows_test.go` | Go surfaces |
| `daemon/internal/platform/identity_linux_test.go` | Go surfaces |
| `daemon/internal/platform/linux_cgroup_v2_linux_test.go` | Go surfaces |
| `daemon/internal/platform/linux_owner_anchor_linux_test.go` | Go surfaces |
| `daemon/internal/platform/linux_owner_helper_v2_linux_test.go` | Go surfaces |
| `daemon/internal/platform/linux_ptrace_launch_linux_test.go` | Go surfaces |
| `daemon/internal/platform/linux_supervisor_protocol_linux_test.go` | Go surfaces |
| `daemon/internal/platform/native_launch_windows_test.go` | Go surfaces |
| `daemon/internal/platform/process_unix_linux_test.go` | Go surfaces |
| `daemon/internal/platform/process_windows_test.go` | Go surfaces |
| `daemon/internal/platform/tcp_peer_linux_test.go` | Go surfaces |
| `daemon/internal/platform/tcp_peer_linux_v2_test.go` | Go surfaces |
| `daemon/internal/platform/tcp_peer_test.go` | Go surfaces |
| `daemon/internal/platform/tcp_peer_windows_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_adapter_operations_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_admission_input_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_contract_fixture_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_contract_semantics_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_evidence_batch_response_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_evidence_batch_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_subject_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_transport_test.go` | Go surfaces |
| `daemon/internal/protocol/goal_validation_test.go` | Go surfaces |
| `daemon/internal/protocol/registration_capabilities_test.go` | Go surfaces |
| `daemon/internal/protocol/work_capabilities_test.go` | Go surfaces |
| `daemon/internal/state/control_test.go` | Go core |
| `daemon/internal/state/event_sequence_test.go` | Go core |
| `daemon/internal/state/goal_delivery_test.go` | Go core |
| `daemon/internal/state/goal_session_test.go` | Go core |
| `daemon/internal/state/linux_cgroup_v2_test.go` | Go core |
| `daemon/internal/state/linux_clone_journal_test.go` | Go core |
| `daemon/internal/state/process_cleanup_test.go` | Go core |
| `daemon/internal/state/provide_input_idempotence_test.go` | Go core |
| `daemon/internal/state/provider_action_test.go` | Go core |
| `daemon/internal/state/state_test.go` | Go core |
| `daemon/internal/state/state_windows_test.go` | Go core |
| `daemon/internal/state/supervisor_handoff_test.go` | Go core |
| `daemon/internal/workspace/artifact_test.go` | Go core |
| `daemon/internal/workspace/fingerprint_test.go` | Go core |
| `daemon/internal/workspace/linux_clone_linux_test.go` | Go core |
| `daemon/internal/workspace/subject_test.go` | Go core |
| `daemon/internal/workspace/workspace_test.go` | Go core |
| `daemon/native/linux-gate/protocol/clone_cleanup_linux_test.go` | Go surfaces |
| `daemon/native/linux-gate/protocol/clone_integration_linux_test.go` | Go surfaces |
| `daemon/native/linux-gate/protocol/launcher_integration_linux_test.go` | Go surfaces |
| `scripts/ci/test-linux-v2-evidence.py` | Tooling |
| `scripts/ci/test-linux-v2-go-events.py` | Tooling |
| `scripts/ci/test-linux-v2-source-subject.py` | Tooling |
| `scripts/claude-code-local-transport-smoke.tests.ps1` | Tooling |

## Additional fixture and execution-entrypoint candidates (290)

These paths need responsibility classification; an operational script is not automatically a removable test harness. Generated fixtures and support modules remain linked to their actual consumers. Historical provenance remains evidence, not executable coverage or a deletion candidate.

| Path | Inventory owner |
|---|---|
| `.github/workflows/ci.yml` | Tooling |
| `browser/package.json` | Tooling |
| `browser/playwright.config.mjs` | Tooling |
| `browser/scripts/run-chat-daemon-tests.ps1` | Tooling |
| `browser/scripts/run-compose-daemon-tests.ps1` | Tooling |
| `browser/scripts/run-live-daemon-tests.ps1` | Tooling |
| `browser/tests/fixtures.mjs` | Tooling |
| `compose.production.yaml` | Tooling |
| `compose.yaml` | Tooling |
| `contracts/fixtures/invalid/adapter.missing-handoff.json` | Tooling |
| `contracts/fixtures/invalid/adapter.unknown-enum.json` | Tooling |
| `contracts/fixtures/invalid/admission.extra-control-field.json` | Tooling |
| `contracts/fixtures/invalid/admission.fresh-source-run.json` | Tooling |
| `contracts/fixtures/invalid/admission.handoff-missing-source-run.json` | Tooling |
| `contracts/fixtures/invalid/admission.handoff-observe.json` | Tooling |
| `contracts/fixtures/invalid/admission.handoff-session-id.json` | Tooling |
| `contracts/fixtures/invalid/admission.implement-validation-id.json` | Tooling |
| `contracts/fixtures/invalid/admission.missing-max-cost.json` | Tooling |
| `contracts/fixtures/invalid/admission.missing-tree-digest.json` | Tooling |
| `contracts/fixtures/invalid/admission.non-implement-provider-scope.json` | Tooling |
| `contracts/fixtures/invalid/admission.plan-handoff.json` | Tooling |
| `contracts/fixtures/invalid/admission.plan-work-item.json` | Tooling |
| `contracts/fixtures/invalid/admission.provider-scope-branches-resource-sync.json` | Tooling |
| `contracts/fixtures/invalid/admission.provider-scope-credential.json` | Tooling |
| `contracts/fixtures/invalid/admission.provider-scope-equal-branches.json` | Tooling |
| `contracts/fixtures/invalid/admission.provider-scope-null-target-change-upsert.json` | Tooling |
| `contracts/fixtures/invalid/admission.provider-scope-pull-request-change-upsert.json` | Tooling |
| `contracts/fixtures/invalid/admission.provider-scope-resource-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/admission.resume-null-session-id.json` | Tooling |
| `contracts/fixtures/invalid/admission.resume-source-run.json` | Tooling |
| `contracts/fixtures/invalid/admission.unsupported-uuid-variant.json` | Tooling |
| `contracts/fixtures/invalid/admission.unsupported-uuid-version.json` | Tooling |
| `contracts/fixtures/invalid/admission.uppercase-uuid.json` | Tooling |
| `contracts/fixtures/invalid/admission.validate-null-validation-id.json` | Tooling |
| `contracts/fixtures/invalid/admission.version-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/admission.wrong-identity.json` | Tooling |
| `contracts/fixtures/invalid/context-snapshot.content-hash-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/context-snapshot.duplicate-predicate-id.json` | Tooling |
| `contracts/fixtures/invalid/context-snapshot.missing-sources.json` | Tooling |
| `contracts/fixtures/invalid/context-snapshot.plan-work-item.json` | Tooling |
| `contracts/fixtures/invalid/context-snapshot.same-change-branch.json` | Tooling |
| `contracts/fixtures/invalid/context-snapshot.validation-binding-extra-field.json` | Tooling |
| `contracts/fixtures/invalid/decision.completion-missing-subject.json` | Tooling |
| `contracts/fixtures/invalid/decision.completion-work-item.json` | Tooling |
| `contracts/fixtures/invalid/decision.duplicate-option.json` | Tooling |
| `contracts/fixtures/invalid/decision.missing-resolution.json` | Tooling |
| `contracts/fixtures/invalid/decision.open-with-resolution.json` | Tooling |
| `contracts/fixtures/invalid/decision.plan-subject.json` | Tooling |
| `contracts/fixtures/invalid/decision.plan-work-item.json` | Tooling |
| `contracts/fixtures/invalid/decision.review-missing-subject.json` | Tooling |
| `contracts/fixtures/invalid/decision.review-missing-work-item.json` | Tooling |
| `contracts/fixtures/invalid/decision.scope-missing-work-item.json` | Tooling |
| `contracts/fixtures/invalid/decision.scope-subject.json` | Tooling |
| `contracts/fixtures/invalid/decision.superseded-with-resolution.json` | Tooling |
| `contracts/fixtures/invalid/decision.unknown-option.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch-conflict-details.empty-items.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch-conflict-details.extra-field.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch-conflict-details.index-out-of-range.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch-response.empty-receipts.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch-response.extra-field.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch-response.invalid-disposition.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch.duplicate-key.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch.empty-items.json` | Tooling |
| `contracts/fixtures/invalid/evidence-batch.extra-control-field.json` | Tooling |
| `contracts/fixtures/invalid/evidence.artifact-subject-commit-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/evidence.artifact-subject-resource-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/evidence.kind-payload-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/evidence.review-verdict-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/evidence.source-ref-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/evidence.subject-hash-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.admit-handoff-chat.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.admit-implement-validation-id.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.admit-plan.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.admit-validate-null-validation-id.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.amend-deterministic-review.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.decision-plan-same-change-branch.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.derived-admission-field.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.expected-version-unsafe-integer.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.extra-control-field.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.fresh-session-id.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.fresh-source-run.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.handoff-missing-source-run.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.handoff-session-id.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.kind-payload-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.nul-text.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.plan-same-change-branch.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.proposal-hash-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.request-decision-caller-options.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.request-decision-unsupported-kind.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.request-plan-derived-field.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.request-plan-fresh-session-id.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.request-plan-handoff.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.request-plan-subject-resource-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.resume-null-session-id.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.unpaired-surrogate.json` | Tooling |
| `contracts/fixtures/invalid/goal-command.version-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/goal-create.deterministic-review.json` | Tooling |
| `contracts/fixtures/invalid/goal-revision.automatic-unlimited-budget.json` | Tooling |
| `contracts/fixtures/invalid/goal-revision.deterministic-review.json` | Tooling |
| `contracts/fixtures/invalid/goal-revision.duplicate-predicate-id.json` | Tooling |
| `contracts/fixtures/invalid/goal-revision.extra-field.json` | Tooling |
| `contracts/fixtures/invalid/goal-revision.null-actor-ref.json` | Tooling |
| `contracts/fixtures/invalid/goal-revision.null-reason.json` | Tooling |
| `contracts/fixtures/invalid/goal-revision.safe-integer-overflow.json` | Tooling |
| `contracts/fixtures/invalid/goal-revision.strict-budget-without-hard-limit.json` | Tooling |
| `contracts/fixtures/invalid/plan-proposal.duplicate-predicate-id.json` | Tooling |
| `contracts/fixtures/invalid/plan-proposal.missing-baseline.json` | Tooling |
| `contracts/fixtures/invalid/plan-proposal.missing-change-target.json` | Tooling |
| `contracts/fixtures/invalid/plan-proposal.same-change-branch.json` | Tooling |
| `contracts/fixtures/invalid/task-result.automatic-baseline-blocker.json` | Tooling |
| `contracts/fixtures/invalid/task-result.candidate-proposal.json` | Tooling |
| `contracts/fixtures/invalid/task-result.failed-nonterminal-reason.json` | Tooling |
| `contracts/fixtures/invalid/task-result.failed-null-reason.json` | Tooling |
| `contracts/fixtures/invalid/task-result.missing-blocker.json` | Tooling |
| `contracts/fixtures/invalid/task-result.missing-proposal.json` | Tooling |
| `contracts/fixtures/invalid/task-result.missing-subject.json` | Tooling |
| `contracts/fixtures/invalid/task-result.plan-proposed-null-proposal.json` | Tooling |
| `contracts/fixtures/invalid/task-result.progress-reason.json` | Tooling |
| `contracts/fixtures/invalid/task-result.proposal-duplicate-predicate.json` | Tooling |
| `contracts/fixtures/invalid/task-result.proposal-same-change-branch.json` | Tooling |
| `contracts/fixtures/invalid/task-result.subject-hash-mismatch.json` | Tooling |
| `contracts/fixtures/invalid/task-result.unexpected-blocker.json` | Tooling |
| `contracts/fixtures/invalid/task-result.unknown-kind.json` | Tooling |
| `contracts/fixtures/invalid/usage.estimated-cost-null.json` | Tooling |
| `contracts/fixtures/invalid/usage.input-tokens-fraction.json` | Tooling |
| `contracts/fixtures/invalid/usage.input-tokens-unsafe-integer.json` | Tooling |
| `contracts/fixtures/invalid/usage.microusd-overflow.json` | Tooling |
| `contracts/fixtures/invalid/usage.reported-cost-null.json` | Tooling |
| `contracts/fixtures/invalid/usage.unknown-cost-non-null.json` | Tooling |
| `contracts/fixtures/invalid/usage.wrong-nullable-type.json` | Tooling |
| `contracts/fixtures/manifest.json` | Tooling |
| `contracts/fixtures/valid/adapter-capabilities.basic.json` | Tooling |
| `contracts/fixtures/valid/admission.basic.json` | Tooling |
| `contracts/fixtures/valid/admission.handoff-validate.json` | Tooling |
| `contracts/fixtures/valid/admission.handoff.json` | Tooling |
| `contracts/fixtures/valid/admission.plan.json` | Tooling |
| `contracts/fixtures/valid/admission.provider-scope-branches-upsert.json` | Tooling |
| `contracts/fixtures/valid/admission.provider-scope-null-target-sync.json` | Tooling |
| `contracts/fixtures/valid/admission.provider-scope-pull-request-update.json` | Tooling |
| `contracts/fixtures/valid/admission.provider-scope.json` | Tooling |
| `contracts/fixtures/valid/admission.validate.json` | Tooling |
| `contracts/fixtures/valid/context-snapshot.basic.json` | Tooling |
| `contracts/fixtures/valid/context-snapshot.plan.json` | Tooling |
| `contracts/fixtures/valid/decision.completion.json` | Tooling |
| `contracts/fixtures/valid/decision.open.json` | Tooling |
| `contracts/fixtures/valid/decision.resolved.json` | Tooling |
| `contracts/fixtures/valid/decision.superseded.json` | Tooling |
| `contracts/fixtures/valid/evidence-batch-conflict-details.basic.json` | Tooling |
| `contracts/fixtures/valid/evidence-batch-response.basic.json` | Tooling |
| `contracts/fixtures/valid/evidence-batch.basic.json` | Tooling |
| `contracts/fixtures/valid/evidence.artifact.json` | Tooling |
| `contracts/fixtures/valid/evidence.check.json` | Tooling |
| `contracts/fixtures/valid/evidence.observation.json` | Tooling |
| `contracts/fixtures/valid/evidence.review.json` | Tooling |
| `contracts/fixtures/valid/goal-command.accept-plan.json` | Tooling |
| `contracts/fixtures/valid/goal-command.achieve.json` | Tooling |
| `contracts/fixtures/valid/goal-command.activate.json` | Tooling |
| `contracts/fixtures/valid/goal-command.add-dependency.json` | Tooling |
| `contracts/fixtures/valid/goal-command.admit-fresh.json` | Tooling |
| `contracts/fixtures/valid/goal-command.admit-handoff-validate.json` | Tooling |
| `contracts/fixtures/valid/goal-command.admit-handoff.json` | Tooling |
| `contracts/fixtures/valid/goal-command.admit-resume.json` | Tooling |
| `contracts/fixtures/valid/goal-command.admit-validate-fresh.json` | Tooling |
| `contracts/fixtures/valid/goal-command.amend-deterministic-operator-override.json` | Tooling |
| `contracts/fixtures/valid/goal-command.amend.json` | Tooling |
| `contracts/fixtures/valid/goal-command.cancel.json` | Tooling |
| `contracts/fixtures/valid/goal-command.pause.json` | Tooling |
| `contracts/fixtures/valid/goal-command.remove-dependency.json` | Tooling |
| `contracts/fixtures/valid/goal-command.request-completion-decision.json` | Tooling |
| `contracts/fixtures/valid/goal-command.request-plan-decision.json` | Tooling |
| `contracts/fixtures/valid/goal-command.request-plan-fresh.json` | Tooling |
| `contracts/fixtures/valid/goal-command.request-plan-resume.json` | Tooling |
| `contracts/fixtures/valid/goal-command.request-review-decision.json` | Tooling |
| `contracts/fixtures/valid/goal-command.request-scope-decision.json` | Tooling |
| `contracts/fixtures/valid/goal-command.resolve-decision.json` | Tooling |
| `contracts/fixtures/valid/goal-command.resume.json` | Tooling |
| `contracts/fixtures/valid/goal-create.basic.json` | Tooling |
| `contracts/fixtures/valid/goal-create.deterministic-operator-override.json` | Tooling |
| `contracts/fixtures/valid/goal-revision.automatic-soft-budget.json` | Tooling |
| `contracts/fixtures/valid/goal-revision.basic.json` | Tooling |
| `contracts/fixtures/valid/goal-revision.deterministic-machine-only.json` | Tooling |
| `contracts/fixtures/valid/goal-revision.deterministic-operator-override.json` | Tooling |
| `contracts/fixtures/valid/goal-revision.strict-budget.json` | Tooling |
| `contracts/fixtures/valid/plan-proposal.basic.json` | Tooling |
| `contracts/fixtures/valid/plan-proposal.integration-omitted.json` | Tooling |
| `contracts/fixtures/valid/task-result.blocked.json` | Tooling |
| `contracts/fixtures/valid/task-result.candidate.json` | Tooling |
| `contracts/fixtures/valid/task-result.failed.json` | Tooling |
| `contracts/fixtures/valid/task-result.plan-proposed.json` | Tooling |
| `contracts/fixtures/valid/usage.estimated-cost.json` | Tooling |
| `contracts/fixtures/valid/usage.microusd-9200000000000000000.json` | Tooling |
| `contracts/fixtures/valid/usage.microusd-max-int64.json` | Tooling |
| `contracts/fixtures/valid/usage.nullable.json` | Tooling |
| `contracts/fixtures/valid/usage.safe-integer-limit.json` | Tooling |
| `contracts/package.json` | Tooling |
| `contracts/scripts/check-effect.mjs` | Tooling |
| `contracts/scripts/check.mjs` | Tooling |
| `contracts/scripts/generate-effect.mjs` | Tooling |
| `contracts/scripts/generate.mjs` | Tooling |
| `contracts/ts/decoder.type-test.ts` | Tooling |
| `control/mix.exs` | Control |
| `control/test/support/conn_case.ex` | Control |
| `control/test/support/data_case.ex` | Control |
| `control/test/support/integration_auth_stub.ex` | Control |
| `control/test/support/integration_http_stub.ex` | Control |
| `control/test/support/integration_provider_stub.ex` | Control |
| `control/test/test_helper.exs` | Control |
| `daemon/cmd/symmetry-daemon/testdata/valid-config.json` | Go surfaces |
| `daemon/cmd/symmetry-linux-launcher/c-wrapper/wrapper.c` | Go surfaces |
| `daemon/e2e/pi-control-witness/main.go` | Go surfaces |
| `daemon/e2e/preauthority-sentinel/main.go` | Go surfaces |
| `daemon/internal/app/pi_control_e2e_options.go` | Go core |
| `daemon/internal/contracts/schema_bundle.json` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.259/README.md` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.259/version.txt` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.281/README.md` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.281/native-structured-output-windows-2026-09-24.md` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.281/stream-json.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.281/structured-output-missing-after-text-success.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.281/structured-output-retry-exhausted.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.281/structured-output-task-result-success.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/claude/2.1.281/version.txt` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.153.4/README.md` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.153.4/app-server-help.txt` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.153.4/app-server-start-smoke.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.153.4/native-smoke.md` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.153.4/schema-manifest.json` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.156.1/README.md` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.156.1/app-server-help.txt` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.156.1/app-server-lifecycle.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.156.1/app-server-start-smoke.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.156.1/frames.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.156.1/native-smoke.md` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.156.1/schema-manifest.json` | Go surfaces |
| `daemon/internal/harness/testdata/codex/0.156.1/version.txt` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.30/README.md` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.30/native-synthetic-tool-current-run-2026-09-13.md` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.30/version.txt` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.32/README.md` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.32/lifecycle-terminal.sse` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.32/lifecycle-tool-retry.sse` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.32/missing-result.sse` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.32/native-windows-2026-09-24.md` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.32/session-events.sse` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.32/unknown-then-terminal.sse` | Go surfaces |
| `daemon/internal/harness/testdata/opencode/1.18.32/version.txt` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.85.1/README.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.85.1/native-linux-cancellation-current-run-2026-09-13.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.85.1/native-linux-current-run-2026-09-13.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.85.1/native-repository-work.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.85.1/native-retained-resume.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.85.1/native-smoke.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.85.1/native-windows-current-run-2026-09-13.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.85.1/version.txt` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.87.1/README.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.87.1/compaction-continuation.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.87.1/lifecycle.jsonl` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.87.1/native-windows-2026-09-24.md` | Go surfaces |
| `daemon/internal/harness/testdata/pi/0.87.1/version.txt` | Go surfaces |
| `daemon/internal/protocol/testdata/evidence-batch.duplicate-item.json` | Go surfaces |
| `daemon/internal/protocol/testdata/evidence-batch.duplicate-outer.json` | Go surfaces |
| `daemon/native/linux-gate/c-gate/gate.c` | Go surfaces |
| `package.json` | Tooling |
| `pnpm-workspace.yaml` | Tooling |
| `scripts/audit-no-legacy-routes.sh` | Tooling |
| `scripts/ci/assert-go-test-json.py` | Tooling |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | Tooling |
| `scripts/ci/linux-v2-host-peer-probe.go` | Tooling |
| `scripts/ci/linux-v2-host-prerequisite-probe.sh` | Tooling |
| `scripts/ci/linux-v2-native-acceptance.sh` | Tooling |
| `scripts/ci/linux-v2-native-tools.sh` | Tooling |
| `scripts/ci/linux-v2-source-subject.py` | Tooling |
| `scripts/ci/literal-pid-reuse-helper.c` | Tooling |
| `scripts/ci/prepare-rootless-compose-owner-claim.py` | Tooling |
| `scripts/ci/provision-linux-v2-control-claims.exs` | Tooling |
| `scripts/ci/publish-linux-v2-control-receipt.py` | Tooling |
| `scripts/ci/register-linux-v2-claim-machine.py` | Tooling |
| `scripts/ci/rootless-compose-native-acceptance.sh` | Tooling |
| `scripts/ci/rootless-compose-native-daemon.Dockerfile` | Tooling |
| `scripts/ci/rootless-compose-transition-proxy.py` | Tooling |
| `scripts/ci/verify-linux-v2-helper-exit-checkpoint.py` | Tooling |
| `scripts/ci/write-linux-v2-owner-request.py` | Tooling |
| `scripts/ci/wsl2-v2-denial-witness.sh` | Tooling |
| `scripts/claude-code-local-transport-smoke.helpers.psm1` | Tooling |
| `scripts/claude-code-local-transport-smoke.ps1` | Tooling |
| `scripts/cleanup-linux-pi-processes.sh` | Tooling |
| `scripts/install-linux-owner.sh` | Tooling |
| `scripts/render-seo-assets.mjs` | Tooling |
| `scripts/systemd/symmetry-daemon.service.in` | Tooling |
| `scripts/systemd/symmetry-owner.service.in` | Tooling |

