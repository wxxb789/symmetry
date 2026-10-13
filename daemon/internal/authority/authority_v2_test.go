package authority

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAddingLinuxCgroupV2KeepsLegacyAuthorityJSONByteShape(t *testing.T) {
	legacyJSON := `{"version":1,"secret":"` + strings.Repeat("a", SecretBytes*2) + `","target_pid":42,"target_identity":"windows:42:created-at","pipe_token":"` + strings.Repeat("b", TokenBytes*2) + `","job_id":"` + strings.Repeat("c", TokenBytes*2) + `","supervisor_pid":99,"supervisor_identity":"windows:99:created-at"}`
	var value Supervisor
	if err := json.Unmarshal([]byte(legacyJSON), &value); err != nil {
		t.Fatalf("legacy Unmarshal() error = %v", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("legacy Marshal() error = %v", err)
	}
	if string(encoded) != legacyJSON {
		t.Fatalf("legacy record changed byte shape:\n got: %s\nwant: %s", encoded, legacyJSON)
	}
}

func TestLinuxCgroupV2AuthorityAndProofsBindExactLeafAndActors(t *testing.T) {
	handoff := testLinuxCgroupV2Handoff()
	if err := handoff.Validate(); err != nil {
		t.Fatalf("v2 handoff Validate() error = %v", err)
	}
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("v2 ToSupervisor() error = %v", err)
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("v2 authority Validate() error = %v", err)
	}

	receipt := testLinuxCgroupV2Receipt(value)
	if !receipt.ValidFor(value) || !receipt.ValidForHandoff(handoff) {
		t.Fatal("exact v2 helper stop receipt did not validate for its authority and handoff")
	}
	value.StopReceipt = &receipt
	if err := value.Validate(); err != nil {
		t.Fatalf("v2 authority with stop receipt Validate() error = %v", err)
	}
	if proof := handoff.ReleaseProof(); proof.Validate() == nil || proof.Version == SupervisorHandoffVersionV2 {
		t.Fatal("authority handed out a synthetic v2 release proof without platform removal")
	}

	cloned := value.Clone()
	if !value.Equal(cloned) {
		t.Fatal("v2 authority Clone() did not preserve deep equality")
	}
	cloned.CgroupIdentity.LeafInode++
	if value.Equal(cloned) || value.CgroupIdentity.LeafInode == cloned.CgroupIdentity.LeafInode {
		t.Fatal("v2 authority clone shared mutable cgroup identity")
	}

	handoffWithReceipt := handoff.Clone()
	handoffWithReceipt.StopReceipt = &receipt
	clonedHandoff := handoffWithReceipt.Clone()
	if !handoffWithReceipt.Equal(clonedHandoff) {
		t.Fatal("v2 handoff Clone() did not preserve deep equality")
	}
	clonedHandoff.StopReceipt.LinuxProof.GateExit.Process.Identity = "replaced"
	if handoffWithReceipt.StopReceipt.LinuxProof.GateExit.Process.Identity == "replaced" || handoffWithReceipt.Equal(clonedHandoff) {
		t.Fatal("v2 handoff clone shared nested stop-proof state")
	}
}

func TestLinuxCgroupV2AuthorityRejectsIncompleteOrMismatchedPhysicalProof(t *testing.T) {
	handoff := testLinuxCgroupV2Handoff()
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	receipt := testLinuxCgroupV2Receipt(value)
	if !receipt.ValidFor(value) {
		t.Fatal("fixture receipt is invalid")
	}

	tests := []struct {
		name   string
		mutate func(*Supervisor, *StopReceipt)
	}{
		{
			name: "wrong leaf object",
			mutate: func(_ *Supervisor, receipt *StopReceipt) {
				receipt.CgroupIdentity.LeafInode++
			},
		},
		{
			name: "missing pinned descriptor proof",
			mutate: func(_ *Supervisor, receipt *StopReceipt) {
				receipt.LinuxProof.PinnedLeafFD = false
			},
		},
		{
			name: "unknown producer migration",
			mutate: func(_ *Supervisor, receipt *StopReceipt) {
				receipt.LinuxProof.NoMigrationObserved = false
			},
		},
		{
			name: "wrong exact gate wait",
			mutate: func(_ *Supervisor, receipt *StopReceipt) {
				receipt.LinuxProof.GateExit.Process.PID++
			},
		},
		{
			name: "v1 receipt schema",
			mutate: func(_ *Supervisor, receipt *StopReceipt) {
				receipt.Version = SupervisorVersion
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateValue := value.Clone()
			candidateReceipt := receipt
			candidateReceipt.CgroupIdentity = cloneLinuxCgroupIdentity(receipt.CgroupIdentity)
			candidateReceipt.LinuxProof = cloneLinuxCgroupStopProof(receipt.LinuxProof)
			test.mutate(&candidateValue, &candidateReceipt)
			if candidateReceipt.ValidFor(candidateValue) {
				t.Fatal("ValidFor() accepted mismatched v2 stop evidence")
			}
			candidateValue.StopReceipt = &candidateReceipt
			if candidateValue.Validate() == nil {
				t.Fatal("Validate() accepted mismatched v2 stop evidence")
			}
		})
	}

	wrongOwnerUID := handoff.Clone()
	wrongOwnerUID.OwnerUID = 0
	if wrongOwnerUID.Validate() == nil {
		t.Fatal("v2 handoff accepted root or missing UID-A identity")
	}
	wrongLauncherUID := handoff.Clone()
	wrongLauncherUID.LauncherUID = 1000
	if wrongLauncherUID.Validate() == nil {
		t.Fatal("v2 handoff accepted a non-root transient launcher identity")
	}

	mirrorProof := *receipt.LinuxProof
	mirrorProof.Kind = LinuxCgroupStopProofMirror
	mirrorProof.Issuer = *value.Mirror
	mirrorProof.Reaper = LinuxCgroupProcessIdentity{PID: value.AnchorPID, Identity: value.AnchorIdentity}
	mirrorProof.GateExit.Witness = mirrorProof.Reaper
	helper := LinuxCgroupProcessIdentity{PID: value.SupervisorPID, Identity: value.SupervisorIdentity}
	mirrorProof.HelperExit = &LinuxCgroupExitProof{
		Version: LinuxCgroupIdentityVersion,
		Process: helper,
		Witness: mirrorProof.Reaper,
		Waited:  true,
		Reaped:  true,
	}
	mirrorProof.GateExit.HelperExit = cloneLinuxCgroupExitProof(mirrorProof.HelperExit)
	receipt.LinuxProof = &mirrorProof
	if !receipt.ValidFor(value) {
		t.Fatal("exact mirror stop proof did not validate")
	}
	receipt.LinuxProof.HelperExit.Witness.Identity = "other-anchor"
	if receipt.ValidFor(value) {
		t.Fatal("mirror stop proof accepted a different reaper identity")
	}
}

func TestLinuxCgroupV1AndV2ProofsRemainDisjoint(t *testing.T) {
	v1Handoff := testAuthoritySupervisorHandoffFixture()
	v1Handoff.SupervisorPID = 21
	v1Handoff.SupervisorIdentity = "legacy-helper:21:start"
	v1, err := v1Handoff.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	v1Receipt := StopReceipt{
		Version:            SupervisorVersion,
		Status:             "stopped",
		OwnerKind:          v1.OwnerKind,
		OwnerContext:       v1.OwnerContext,
		TargetPID:          v1.TargetPID,
		TargetIdentity:     v1.TargetIdentity,
		PipeToken:          v1.PipeToken,
		JobID:              v1.JobID,
		SupervisorPID:      v1.SupervisorPID,
		SupervisorIdentity: v1.SupervisorIdentity,
	}
	if !v1Receipt.ValidFor(v1) {
		t.Fatal("valid v1 receipt fixture was rejected")
	}

	v2Handoff := testLinuxCgroupV2Handoff()
	v2, err := v2Handoff.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	v2Receipt := testLinuxCgroupV2Receipt(v2)
	if v1Receipt.ValidFor(v2) || v2Receipt.ValidFor(v1) || v1Receipt.ValidForHandoff(v2Handoff) {
		t.Fatal("a version-mismatched receipt crossed the v1/v2 authority boundary")
	}
	if v1Handoff.ReleaseProof().ValidFor(v2Handoff) {
		t.Fatal("v1 release proof validated for v2 handoff")
	}
}

func TestLinuxCgroupReservationAbortProofRequiresExactPreEffectEvidence(t *testing.T) {
	parent := LinuxCgroupParentIdentity{
		BootID:  "00000000-0000-4000-8000-000000000001",
		MountID: 7,
		Device:  11,
		Inode:   13,
		Path:    "/system.slice/symmetry.slice/delegated",
	}
	anchor := LinuxCgroupProcessIdentity{PID: 9, Identity: "anchor:9:start"}
	mirror := LinuxCgroupProcessIdentity{PID: 8, Identity: "daemon:8:start"}
	proof := LinuxCgroupReservationAbortProof{
		Version:         LinuxCgroupAbortProofVersion,
		LaunchToken:     strings.Repeat("a", TokenBytes*2),
		Parent:          parent,
		LeafName:        strings.Repeat("b", TokenBytes*2),
		LeafDisposition: LinuxCgroupAbortLeafNeverCreated,
		Anchor:          anchor,
		OwnerUID:        1000, Mirror: mirror,
		Witness:          anchor,
		CreatorFenced:    true,
		NoProducerCanAct: true,
		ParentUnchanged:  true,
		LeafAbsent:       true,
	}
	if err := proof.Validate(); err != nil || !proof.ValidFor(proof.LaunchToken, parent, proof.LeafName, false, 1000, anchor, mirror, nil, nil) {
		t.Fatalf("pre-mkdir abort proof invalid: %v", err)
	}
	proof.CreateStarted = true
	if proof.Validate() == nil {
		t.Fatal("post-create-intent proof inferred never-created from path absence alone")
	}
	proof.LeafNeverAppeared = true
	if err := proof.Validate(); err != nil {
		t.Fatalf("exact never-appeared proof invalid: %v", err)
	}
	if proof.ValidFor(proof.LaunchToken, parent, proof.LeafName, false, 1000, anchor, mirror, nil, nil) {
		t.Fatal("abort proof validated for another creation-intent state")
	}
}

func testLinuxCgroupV2Handoff() SupervisorHandoff {
	parent := LinuxCgroupParentIdentity{
		BootID:  "00000000-0000-4000-8000-000000000001",
		MountID: 42,
		Device:  84,
		Inode:   126,
		Path:    "/system.slice/symmetry.slice/delegated",
	}
	leaf := LinuxCgroupIdentity{
		Version:    LinuxCgroupIdentityVersion,
		Parent:     parent,
		LeafName:   strings.Repeat("e", TokenBytes*2),
		LeafDevice: 168,
		LeafInode:  210,
	}
	anchor := LinuxCgroupProcessIdentity{PID: 10, Identity: "anchor:10:start"}
	helper := LinuxCgroupProcessIdentity{PID: 20, Identity: "helper:20:start"}
	launcher := LinuxCgroupProcessIdentity{PID: 30, Identity: "launcher:30:start"}
	gate := LinuxCgroupProcessIdentity{PID: 40, Identity: "gate:40:start"}
	mirror := LinuxCgroupProcessIdentity{PID: 50, Identity: "daemon:50:start"}
	launcherExit := &LinuxCgroupExitProof{
		Version: LinuxCgroupIdentityVersion,
		Process: launcher,
		Witness: helper,
		Waited:  true,
		Reaped:  true,
	}
	return SupervisorHandoff{
		Version:            SupervisorHandoffVersionV2,
		LaunchToken:        strings.Repeat("a", TokenBytes*2),
		Secret:             strings.Repeat("b", SecretBytes*2),
		OwnerKind:          OwnerKindLinuxHelper,
		OwnerContext:       "/run/symmetry/recovery.sock",
		TargetPID:          gate.PID,
		TargetIdentity:     gate.Identity,
		OwnerUID:           1000,
		LauncherUID:        0,
		PipeToken:          strings.Repeat("c", TokenBytes*2),
		JobID:              strings.Repeat("d", TokenBytes*2),
		TargetBirthKind:    LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity:     &leaf,
		AnchorPID:          anchor.PID,
		AnchorIdentity:     anchor.Identity,
		LauncherPID:        launcher.PID,
		LauncherIdentity:   launcher.Identity,
		LauncherExit:       launcherExit,
		SupervisorPID:      helper.PID,
		SupervisorIdentity: helper.Identity,
		Mirror:             &mirror,
	}
}

func testLinuxCgroupV2Receipt(value Supervisor) StopReceipt {
	helper := LinuxCgroupProcessIdentity{PID: value.SupervisorPID, Identity: value.SupervisorIdentity}
	proof := LinuxCgroupStopProof{
		Version:                LinuxCgroupIdentityVersion,
		Kind:                   LinuxCgroupStopProofHelper,
		Issuer:                 helper,
		Reaper:                 helper,
		PinnedLeafFD:           true,
		CgroupKillAcknowledged: true,
		CgroupPopulatedZero:    true,
		NoMigrationObserved:    true,
		ProducerFenced:         true,
		DescendantsReaped:      true,
		LauncherExit:           *value.LauncherExit,
		GateExit: LinuxCgroupExitProof{
			Version: LinuxCgroupIdentityVersion,
			Process: LinuxCgroupProcessIdentity{PID: value.TargetPID, Identity: value.TargetIdentity},
			Witness: helper,
			Waited:  true,
			Reaped:  true,
		},
	}
	return StopReceipt{
		Version:            SupervisorVersionV2,
		Status:             "stopped",
		OwnerKind:          value.OwnerKind,
		OwnerContext:       value.OwnerContext,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		OwnerUID:           value.OwnerUID,
		LauncherUID:        value.LauncherUID,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		ActiveProcesses:    0,
		TargetBirthKind:    value.TargetBirthKind,
		CgroupIdentity:     cloneLinuxCgroupIdentity(value.CgroupIdentity),
		LinuxProof:         &proof,
	}
}
