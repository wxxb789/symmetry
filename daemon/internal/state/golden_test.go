package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func goldenRunJournalMinimal() RunJournal {
	return RunJournal{
		RunID:               "00000000-0000-4000-8000-00000000a001",
		Generation:          3,
		RuntimeKey:          "local",
		RuntimeID:           "00000000-0000-4000-8000-00000000a002",
		ClaimedRuntimeEpoch: 7,
		ClaimID:             "00000000-0000-4000-8000-00000000a003",
		LeaseToken:          "lease-token-3",
		LeaseExpiresAt:      time.Date(2026, 11, 22, 4, 5, 6, 0, time.UTC),
		LocalState:          "claimed",
		Work: protocol.Work{
			Goal:         "implement",
			AgentProfile: "codex",
			Workspace:    "isolated",
			Input:        json.RawMessage(`{"goal":"implement"}`),
		},
	}
}

func goldenRunJournalClone() RunJournal {
	value := goldenRunJournalMinimal()
	value.WorkspacePath = "/workspace/clone-3"
	value.WorkspaceBindingKey = "00000000-0000-4000-8000-00000000a004"
	value.WorkspaceRecoveryRequired = true
	value.RetainWorkspace = true
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: value.RunID, Generation: value.Generation,
		BindingKey: value.WorkspaceBindingKey, ResourceID: "repo-one", BaseCommit: strings.Repeat("a", 40),
		BaseTreeDigest: "sha256:" + strings.Repeat("b", 64), Nonce: "nonce-1",
		SeedPath: "/seed/clone-3", Target: value.WorkspacePath,
	}
	value.LinuxCloneIntent = &intent
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("c", 64), Size: 4096, Device: 8, Inode: 12,
	}
	value.LinuxCloneSeedReceipt = &seed
	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: value.RunID, Generation: value.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target,
		Nonce: intent.Nonce, SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit,
		TargetDevice: 18, TargetInode: 24,
	}
	value.LinuxCloneCheckoutReceipt = &checkout
	grant := authority.LinuxCloneReuseGrant{
		Version: authority.LinuxCloneVersion, Source: authority.LinuxCloneGrant{Intent: intent, Seed: seed},
		SourceCheckout: checkout, RunID: value.RunID, Generation: value.Generation,
		WorkspaceFingerprint: "fingerprint-1",
	}
	value.LinuxCloneReuseGrant = &grant
	value.LinuxCloneReuseRetired = true
	value.LinuxCloneReuseAbortedBeforeNativeStart = true
	promotionIntent := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
		Commit: strings.Repeat("d", 40), TreeDigest: "sha256:" + strings.Repeat("e", 64),
	}
	value.LinuxClonePromotionIntent = &promotionIntent
	promotionReceipt := authority.LinuxClonePromotionReceipt{
		Version: authority.LinuxCloneVersion, Commit: promotionIntent.Commit,
		TreeDigest: promotionIntent.TreeDigest, Ref: "refs/symmetry/clone/" + value.RunID,
	}
	value.LinuxClonePromotionReceipt = &promotionReceipt
	value.LinuxCloneAbortPending = true
	abortProof := authority.LinuxCloneAbortProof{
		Version: authority.LinuxCloneVersion, Intent: intent, SeedReceipt: &seed,
		BaseRef: "refs/base", PromotionRef: promotionReceipt.Ref, TargetAbsent: true,
		SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
	}
	value.LinuxCloneAbortProof = &abortProof
	value.LinuxCloneCleanupPending = true
	cleanupIntent := authority.LinuxCloneCleanupIntent{
		Version: authority.LinuxCloneVersion, Intent: intent,
		FenceDigest: "sha256:" + strings.Repeat("f", 64), RootPath: "/root/clone-3",
		BaseRef: "refs/base", SeedReceipt: seed, CheckoutReceipt: checkout,
		PromotionIntent: promotionIntent, PromotionReceipt: promotionReceipt,
		NormalReleaseProof: authority.SupervisorHandoffReleaseProof{
			Version: authority.SupervisorHandoffVersionV2, Status: "released", LaunchToken: "launch-1",
			TargetPID: 4242, TargetIdentity: "target:4242:1", PipeToken: "pipe-1", JobID: "job-1",
			SupervisorPID: 4243, SupervisorIdentity: "supervisor:4243:1",
		},
		ArtifactEvidenceID: "evidence-1",
	}
	value.LinuxCloneCleanupIntent = &cleanupIntent
	removal := authority.LinuxCloneTargetRemovalProof{
		Version: authority.LinuxCloneVersion, Intent: cleanupIntent,
		RootDevice: 9, RootInode: 11, TargetDevice: checkout.TargetDevice, TargetInode: checkout.TargetInode,
		Disposition: authority.LinuxCloneTargetDispositionRemoved, PathRewalkVerified: true,
		BOwnedMountConfined: true, TargetNlinkZero: true,
	}
	value.LinuxCloneTargetRemovalProof = &removal
	cleanupProof := authority.LinuxCloneCleanupProof{
		Version: authority.LinuxCloneVersion, Intent: cleanupIntent, TargetRemovalProof: removal,
		SeedAbsent: true, BaseRefAbsent: true, ScratchAbsent: true, PromotionRefPresent: true,
	}
	value.LinuxCloneCleanupProof = &cleanupProof
	return value
}

func goldenRunJournalContainment() RunJournal {
	value := goldenRunJournalMinimal()
	value.PID = 4242
	value.ProcessIdentity = "target:4242:1"
	value.StartedAt = time.Date(2026, 11, 22, 4, 5, 7, 0, time.UTC)
	owner := authority.Supervisor{
		Version: authority.SupervisorVersionV2, Secret: "secret-1", LaunchToken: "launch-1",
		OwnerKind: authority.OwnerKindLinuxHelper, TargetPID: 4242, TargetIdentity: value.ProcessIdentity,
		PipeToken: "pipe-1", JobID: "job-1", SupervisorPID: 4243, SupervisorIdentity: "supervisor:4243:1",
	}
	value.ContainmentAuthority = &owner
	handoff := authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersionV2, LaunchToken: "launch-1", Secret: "secret-1",
		OwnerKind: authority.OwnerKindLinuxHelper, TargetPID: 4242, TargetIdentity: value.ProcessIdentity,
		PipeToken: "pipe-1", JobID: "job-1", SupervisorPID: 4243, SupervisorIdentity: "supervisor:4243:1",
	}
	value.ContainmentHandoff = &handoff
	reservation := LinuxCgroupReservation{
		Version: LinuxCgroupReservationVersion, JobID: "job-1", LaunchToken: "launch-1",
		Anchor:    authority.LinuxCgroupProcessIdentity{PID: 4241, Identity: "anchor:4241:1"},
		Parent:    authority.LinuxCgroupParentIdentity{BootID: "boot-1", MountID: 5, Device: 6, Inode: 7, Path: "/sys/fs/cgroup/symmetry"},
		LeafName:  strings.Repeat("1", 64),
		PipeToken: "pipe-1", RecoveryEndpoint: "endpoint-1", Secret: "secret-1",
	}
	value.ContainmentCgroup = &reservation
	release := authority.SupervisorHandoffReleaseProof{
		Version: authority.SupervisorHandoffVersionV2, Status: "released", LaunchToken: "launch-1",
		TargetPID: 4242, TargetIdentity: value.ProcessIdentity, PipeToken: "pipe-1", JobID: "job-1",
		SupervisorPID: 4243, SupervisorIdentity: "supervisor:4243:1", LeafDisposition: "removed",
	}
	value.ContainmentCgroupReleaseProof = &release
	preGate := authority.LinuxCgroupPreGateReleaseProof{
		Version: authority.SupervisorHandoffVersionV2, LeafDisposition: "removed",
	}
	value.ContainmentCgroupPreGateReleaseProof = &preGate
	scans := ContainmentScanJournal{
		Version: 1, LaunchToken: "launch-1", TargetPID: 4242, TargetIdentity: value.ProcessIdentity,
		SupervisorPID: 4243, SupervisorIdentity: "supervisor:4243:1", MirrorIdentity: strings.Repeat("2", 64),
		Helper: ContainmentScanProgress{CompletedSequence: 2, CleanResultSequence: 2, SealedSequence: 2},
		Mirror: ContainmentScanProgress{CompletedSequence: 2, CleanResultSequence: 2, SealedSequence: 2},
	}
	value.ContainmentScans = &scans
	value.ContainmentUnproven = true
	return value
}

func goldenRunJournalOutbox() RunJournal {
	value := goldenRunJournalMinimal()
	value.LastEventSequence = 5
	value.PendingEvents = []protocol.RunEvent{{
		EventID: "event-1", Sequence: 4, Kind: "output", OccurredAt: time.Date(2026, 11, 22, 4, 5, 8, 0, time.UTC),
		Payload: json.RawMessage(`{"chunk":"one"}`),
	}}
	value.DroppedOutputChunks = 2
	value.DroppedOutputBytes = 128
	value.PendingTransitions = []protocol.StateTransitionRequest{{
		Fence: value.Fence(), TransitionID: "transition-1", State: "running",
		Payload: json.RawMessage(`{"state":"running"}`),
	}}
	value.AttemptedTransitionIDs = []string{"transition-0"}
	value.PendingCommandAcknowledgements = []protocol.CommandAcknowledgement{{
		Fence: value.Fence(), RunID: value.RunID, CommandID: "command-1", Outcome: "accepted", AckID: "ack-1",
	}}
	return value
}

func goldenRunJournalGoalDelivery() RunJournal {
	value := goldenRunJournalMinimal()
	value.PendingGoalDeliveries = []GoalDelivery{{
		Kind: GoalDeliveryEvidence, DeliveryID: "delivery-1", PayloadDigest: "sha256:" + strings.Repeat("3", 64),
		Fence: value.Fence(), Ready: true,
	}}
	value.DeliveredGoalDeliveries = []GoalDelivery{{
		Kind: GoalDeliveryUsage, DeliveryID: "delivery-2", PayloadDigest: "sha256:" + strings.Repeat("4", 64),
		Fence: value.Fence(),
	}}
	value.RetiredGoalDeliveries = []goalDeliveryRetirement{{
		Delivery: GoalDelivery{
			Kind: GoalDeliveryEvidence, DeliveryID: "delivery-3", PayloadDigest: "sha256:" + strings.Repeat("5", 64),
			Fence: value.Fence(),
		},
		StatusCode: 409, Code: "conflict", Message: "already delivered",
		RetiredAt: time.Date(2026, 11, 22, 4, 5, 9, 0, time.UTC),
	}}
	value.GoalDeliveryEnabled = true
	value.NativeUsageObservation = &NativeUsageObservation{
		SchemaVersion: NativeUsageObservationSchemaVersion, InputTokens: 11, OutputTokens: 7,
		CachedInputTokens: 3, ReasoningOutputTokens: 2, TotalTokens: 18,
		ObservedAt: time.Date(2026, 11, 22, 4, 5, 10, 0, time.UTC),
	}
	value.NativeUsageRecoveryRequired = true
	value.NativeUsageTerminalRecovery = &NativeUsageTerminalRecovery{
		State: "completed", Payload: json.RawMessage(`{"task_result":{"kind":"candidate_completion"}}`),
	}
	value.InputCommandIntent = &InputCommandIntent{
		CommandID: "command-1", PayloadDigest: "sha256:" + strings.Repeat("6", 64),
		RunningTransitionID: "transition-1", AckID: "ack-1", EventSequenceBarrier: 5, Outcome: "accepted",
	}
	value.ControlCommandIntents = []ControlCommandIntent{{
		CommandID: "command-2", Kind: "cancel", PayloadDigest: "sha256:" + strings.Repeat("7", 64),
		AckID: "ack-2",
	}}
	value.ProviderActionIntents = []ProviderActionIntent{{
		ActionID: "action-1", ActionKey: "key-1", RequestDigest: "sha256:" + strings.Repeat("8", 64),
		ResourceID: "repo-one", Operation: "fetch",
	}}
	return value
}

func goldenGoalSessionJournal() GoalSessionJournal {
	return GoalSessionJournal{
		GoalSessionLaunchIntent: GoalSessionLaunchIntent{
			LaunchIntentID: "00000000-0000-4000-8000-00000000b001",
			GoalID:         "00000000-0000-4000-8000-00000000b002",
			GoalRevision:   2,
			TaskID:         "00000000-0000-4000-8000-00000000b003",
			RunID:          "00000000-0000-4000-8000-00000000b004",
			Generation:     1,
			LocalHandleID:  "00000000-0000-4000-8000-00000000b005",
			HarnessKind:    "codex",
			HarnessVersion: "0.99.0", AdapterVersion: "adapter-1", AdapterProtocolVersion: 1,
			WorkspaceFingerprint: "fingerprint-1",
		},
		SchemaVersion: 1,
		SessionState:  "running",
		LaunchState:   "started",
		CreatedAt:     time.Date(2026, 11, 22, 4, 6, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 11, 22, 4, 6, 1, 0, time.UTC),
	}
}

func goldenLateGoalUsage() lateGoalUsageLedger {
	return lateGoalUsageLedger{
		SchemaVersion: lateGoalUsageLedgerSchemaVersion,
		RunID:         "00000000-0000-4000-8000-00000000c001",
		Generation:    2,
		Fence: protocol.Fence{
			RuntimeID: "00000000-0000-4000-8000-00000000c002", RuntimeEpoch: 5, Generation: 2,
			ClaimID: "00000000-0000-4000-8000-00000000c003", LeaseToken: "lease-token-2",
		},
		PendingUsageDeliveries: []GoalDelivery{{
			Kind: GoalDeliveryUsage, DeliveryID: "delivery-1", PayloadDigest: "sha256:" + strings.Repeat("9", 64),
			Fence: protocol.Fence{
				RuntimeID: "00000000-0000-4000-8000-00000000c002", RuntimeEpoch: 5, Generation: 2,
				ClaimID: "00000000-0000-4000-8000-00000000c003", LeaseToken: "lease-token-2",
			},
		}},
		CreatedAt: time.Date(2026, 11, 22, 4, 7, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 11, 22, 4, 7, 1, 0, time.UTC),
	}
}

func goldenMachineIdentity() MachineIdentity {
	return MachineIdentity{MachineID: "00000000-0000-4000-8000-00000000d001", MachineToken: "machine-token-1"}
}

func goldenEnrollmentIntent() EnrollmentIntent {
	return EnrollmentIntent{MachineName: "workstation-1", MachineToken: "machine-token-1", IdempotencyKey: "idempotency-1"}
}

func goldenFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "goldens", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return data
}

func decodeGoldenRunJournal(t *testing.T, name string) RunJournal {
	t.Helper()
	var journal RunJournal
	if err := decodeJSONWithLimitAndTags(goldenFile(t, name), "run journal", &journal, maxJournalFileBytes, reflect.TypeOf(RunJournal{})); err != nil {
		t.Fatalf("decode golden %s: %v", name, err)
	}
	return journal
}

func assertGoldenBytes(t *testing.T, name string, got []byte) {
	t.Helper()
	want := goldenFile(t, name)
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s differs\nwant %s\n got %s", name, want, got)
	}
}

func TestRunJournalGoldensDecodeAndReencodeIdentically(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func() RunJournal{
		"run-journal-minimal.json":       goldenRunJournalMinimal,
		"run-journal-clone.json":         goldenRunJournalClone,
		"run-journal-containment.json":   goldenRunJournalContainment,
		"run-journal-outbox.json":        goldenRunJournalOutbox,
		"run-journal-goal-delivery.json": goldenRunJournalGoalDelivery,
	} {
		encoded, err := serializeRunJournal(decodeGoldenRunJournal(t, name))
		if err != nil {
			t.Fatalf("re-encode golden %s: %v", name, err)
		}
		assertGoldenBytes(t, name, encoded)
		built, err := serializeRunJournal(build())
		if err != nil {
			t.Fatalf("encode built value for %s: %v", name, err)
		}
		assertGoldenBytes(t, name, built)
	}
}

func TestPersistedJournalFamilyGoldensRoundTrip(t *testing.T) {
	t.Parallel()
	for name, family := range map[string]struct {
		value    any
		resource string
		limit    int
	}{
		"goal-session-journal.json": {value: goldenGoalSessionJournal(), resource: "goal session journal", limit: maxJournalFileBytes},
		"late-goal-usage.json":      {value: goldenLateGoalUsage(), resource: "late Goal usage ledger", limit: maxJournalFileBytes},
		"machine-identity.json":     {value: goldenMachineIdentity(), resource: "identity", limit: maxStateFileBytes},
		"enrollment-intent.json":    {value: goldenEnrollmentIntent(), resource: "enrollment intent", limit: maxStateFileBytes},
	} {
		want := goldenFile(t, name)
		encoded, err := json.Marshal(family.value)
		if err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
		assertGoldenBytes(t, name, encoded)
		decoded := reflect.New(reflect.TypeOf(family.value))
		if err := decodeJSONWithLimitAndTags(want, family.resource, decoded.Interface(), family.limit, nil); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		reencoded, err := json.Marshal(decoded.Elem().Interface())
		if err != nil {
			t.Fatalf("re-encode %s: %v", name, err)
		}
		assertGoldenBytes(t, name, reencoded)
	}
}

func TestRunJournalGoldenFieldKeys(t *testing.T) {
	t.Parallel()
	type keyCase struct {
		golden   string
		key      string
		mutate   func(*RunJournal)
		value    any
		contains string
		absent   bool
	}
	sentinelTime := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []keyCase{
		{golden: "run-journal-clone.json", key: "workspace_path", mutate: func(journal *RunJournal) { journal.WorkspacePath = "/sentinel/workspace" }, value: "/sentinel/workspace"},
		{golden: "run-journal-clone.json", key: "workspace_recovery_required", mutate: func(journal *RunJournal) { journal.WorkspaceRecoveryRequired = false }, absent: true},
		{golden: "run-journal-clone.json", key: "retain_workspace", mutate: func(journal *RunJournal) { journal.RetainWorkspace = false }, absent: true},
		{golden: "run-journal-clone.json", key: "workspace_binding_key", mutate: func(journal *RunJournal) { journal.WorkspaceBindingKey = "sentinel-binding" }, value: "sentinel-binding"},
		{golden: "run-journal-clone.json", key: "linux_clone_intent", mutate: func(journal *RunJournal) { journal.LinuxCloneIntent = &authority.LinuxCloneIntent{Version: 4242} }, contains: `"version":4242`},
		{golden: "run-journal-clone.json", key: "linux_clone_seed_receipt", mutate: func(journal *RunJournal) {
			journal.LinuxCloneSeedReceipt = &authority.LinuxCloneSeedReceipt{Version: 4243}
		}, contains: `"version":4243`},
		{golden: "run-journal-clone.json", key: "linux_clone_checkout_receipt", mutate: func(journal *RunJournal) {
			journal.LinuxCloneCheckoutReceipt = &authority.LinuxCloneCheckoutReceipt{Version: 4244}
		}, contains: `"version":4244`},
		{golden: "run-journal-clone.json", key: "linux_clone_reuse_grant", mutate: func(journal *RunJournal) {
			journal.LinuxCloneReuseGrant = &authority.LinuxCloneReuseGrant{Version: 4245}
		}, contains: `"version":4245`},
		{golden: "run-journal-clone.json", key: "linux_clone_reuse_retired", mutate: func(journal *RunJournal) { journal.LinuxCloneReuseRetired = false }, absent: true},
		{golden: "run-journal-clone.json", key: "linux_clone_reuse_aborted_before_native_start", mutate: func(journal *RunJournal) { journal.LinuxCloneReuseAbortedBeforeNativeStart = false }, absent: true},
		{golden: "run-journal-clone.json", key: "linux_clone_promotion_intent", mutate: func(journal *RunJournal) {
			journal.LinuxClonePromotionIntent = &authority.LinuxClonePromotionIntent{Version: 4246}
		}, contains: `"version":4246`},
		{golden: "run-journal-clone.json", key: "linux_clone_promotion_receipt", mutate: func(journal *RunJournal) {
			journal.LinuxClonePromotionReceipt = &authority.LinuxClonePromotionReceipt{Version: 4247}
		}, contains: `"version":4247`},
		{golden: "run-journal-clone.json", key: "linux_clone_abort_pending", mutate: func(journal *RunJournal) { journal.LinuxCloneAbortPending = false }, absent: true},
		{golden: "run-journal-clone.json", key: "linux_clone_abort_proof", mutate: func(journal *RunJournal) {
			journal.LinuxCloneAbortProof = &authority.LinuxCloneAbortProof{Version: 4248}
		}, contains: `"version":4248`},
		{golden: "run-journal-clone.json", key: "linux_clone_cleanup_pending", mutate: func(journal *RunJournal) { journal.LinuxCloneCleanupPending = false }, absent: true},
		{golden: "run-journal-clone.json", key: "linux_clone_cleanup_intent", mutate: func(journal *RunJournal) {
			journal.LinuxCloneCleanupIntent = &authority.LinuxCloneCleanupIntent{Version: 4249}
		}, contains: `"version":4249`},
		{golden: "run-journal-clone.json", key: "linux_clone_target_removal_proof", mutate: func(journal *RunJournal) {
			journal.LinuxCloneTargetRemovalProof = &authority.LinuxCloneTargetRemovalProof{Version: 4250}
		}, contains: `"version":4250`},
		{golden: "run-journal-clone.json", key: "linux_clone_cleanup_proof", mutate: func(journal *RunJournal) {
			journal.LinuxCloneCleanupProof = &authority.LinuxCloneCleanupProof{Version: 4251}
		}, contains: `"version":4251`},
		{golden: "run-journal-containment.json", key: "pid", mutate: func(journal *RunJournal) { journal.PID = 5151 }, value: 5151},
		{golden: "run-journal-containment.json", key: "process_identity", mutate: func(journal *RunJournal) { journal.ProcessIdentity = "sentinel:none" }, value: "sentinel:none"},
		{golden: "run-journal-containment.json", key: "started_at", mutate: func(journal *RunJournal) { journal.StartedAt = sentinelTime }, value: sentinelTime},
		{golden: "run-journal-containment.json", key: "containment_authority", mutate: func(journal *RunJournal) { journal.ContainmentAuthority = &authority.Supervisor{Version: 4252} }, contains: `"version":4252`},
		{golden: "run-journal-containment.json", key: "containment_handoff", mutate: func(journal *RunJournal) { journal.ContainmentHandoff = &authority.SupervisorHandoff{Version: 4253} }, contains: `"version":4253`},
		{golden: "run-journal-containment.json", key: "containment_cgroup", mutate: func(journal *RunJournal) { journal.ContainmentCgroup = &LinuxCgroupReservation{Version: 4254} }, contains: `"version":4254`},
		{golden: "run-journal-containment.json", key: "containment_cgroup_release_proof", mutate: func(journal *RunJournal) {
			journal.ContainmentCgroupReleaseProof = &authority.SupervisorHandoffReleaseProof{Version: 4255}
		}, contains: `"version":4255`},
		{golden: "run-journal-containment.json", key: "containment_cgroup_pre_gate_release_proof", mutate: func(journal *RunJournal) {
			journal.ContainmentCgroupPreGateReleaseProof = &authority.LinuxCgroupPreGateReleaseProof{Version: 4256}
		}, contains: `"version":4256`},
		{golden: "run-journal-containment.json", key: "containment_scans", mutate: func(journal *RunJournal) { journal.ContainmentScans = &ContainmentScanJournal{Version: 4257} }, contains: `"version":4257`},
		{golden: "run-journal-containment.json", key: "containment_unproven", mutate: func(journal *RunJournal) { journal.ContainmentUnproven = false }, absent: true},
		{golden: "run-journal-outbox.json", key: "last_event_sequence", mutate: func(journal *RunJournal) { journal.LastEventSequence = 5151 }, value: 5151},
		{golden: "run-journal-outbox.json", key: "pending_events", mutate: func(journal *RunJournal) { journal.PendingEvents = []protocol.RunEvent{{EventID: "sentinel-event"}} }, contains: `"sentinel-event"`},
		{golden: "run-journal-outbox.json", key: "dropped_output_chunks", mutate: func(journal *RunJournal) { journal.DroppedOutputChunks = 5151 }, value: 5151},
		{golden: "run-journal-outbox.json", key: "dropped_output_bytes", mutate: func(journal *RunJournal) { journal.DroppedOutputBytes = 5151 }, value: 5151},
		{golden: "run-journal-outbox.json", key: "pending_transitions", mutate: func(journal *RunJournal) {
			journal.PendingTransitions = []protocol.StateTransitionRequest{{TransitionID: "sentinel-transition"}}
		}, contains: `"sentinel-transition"`},
		{golden: "run-journal-outbox.json", key: "attempted_transition_ids", mutate: func(journal *RunJournal) { journal.AttemptedTransitionIDs = []string{"sentinel-attempted"} }, contains: `"sentinel-attempted"`},
		{golden: "run-journal-outbox.json", key: "pending_command_acknowledgements", mutate: func(journal *RunJournal) {
			journal.PendingCommandAcknowledgements = []protocol.CommandAcknowledgement{{AckID: "sentinel-ack"}}
		}, contains: `"sentinel-ack"`},
		{golden: "run-journal-goal-delivery.json", key: "pending_goal_deliveries", mutate: func(journal *RunJournal) {
			journal.PendingGoalDeliveries = []GoalDelivery{{DeliveryID: "sentinel-pending"}}
		}, contains: `"sentinel-pending"`},
		{golden: "run-journal-goal-delivery.json", key: "delivered_goal_deliveries", mutate: func(journal *RunJournal) {
			journal.DeliveredGoalDeliveries = []GoalDelivery{{DeliveryID: "sentinel-delivered"}}
		}, contains: `"sentinel-delivered"`},
		{golden: "run-journal-goal-delivery.json", key: "retired_goal_deliveries", mutate: func(journal *RunJournal) {
			journal.RetiredGoalDeliveries = []goalDeliveryRetirement{{StatusCode: 5151}}
		}, contains: `"status_code":5151`},
		{golden: "run-journal-goal-delivery.json", key: "goal_delivery_enabled", mutate: func(journal *RunJournal) { journal.GoalDeliveryEnabled = false }, absent: true},
	}
	for _, testCase := range cases {
		journal := decodeGoldenRunJournal(t, testCase.golden)
		testCase.mutate(&journal)
		encoded, err := serializeRunJournal(journal)
		if err != nil {
			t.Fatalf("%s: encode: %v", testCase.key, err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatalf("%s: decode map: %v", testCase.key, err)
		}
		raw, present := fields[testCase.key]
		if testCase.absent {
			if present {
				t.Fatalf("%s: key %q unexpectedly present after clearing", testCase.golden, testCase.key)
			}
			continue
		}
		if !present {
			t.Fatalf("%s: key %q missing after mutation", testCase.golden, testCase.key)
		}
		if testCase.contains != "" {
			if !bytes.Contains(raw, []byte(testCase.contains)) {
				t.Fatalf("%s: key %q = %s, missing %s", testCase.golden, testCase.key, raw, testCase.contains)
			}
			continue
		}
		want, err := json.Marshal(testCase.value)
		if err != nil {
			t.Fatalf("%s: marshal sentinel: %v", testCase.key, err)
		}
		if !bytes.Equal(raw, want) {
			t.Fatalf("%s: key %q = %s, want %s", testCase.golden, testCase.key, raw, want)
		}
	}
}
