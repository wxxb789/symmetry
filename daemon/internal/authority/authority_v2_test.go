package authority

import (
	"encoding/json"
	"reflect"
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

type durableRecord struct {
	name  string
	value any
	equal func(left, right any) bool
	clone func(value any) any
}

func durableSupervisorRecords(t *testing.T) []durableRecord {
	t.Helper()
	return []durableRecord{
		{
			name:  "Supervisor",
			value: durableSupervisorFixture(t),
			equal: func(left, right any) bool { return left.(Supervisor).Equal(right.(Supervisor)) },
			clone: func(value any) any { return value.(Supervisor).Clone() },
		},
		{
			name:  "SupervisorHandoff",
			value: durableHandoffFixture(t),
			equal: func(left, right any) bool { return left.(SupervisorHandoff).Equal(right.(SupervisorHandoff)) },
			clone: func(value any) any { return value.(SupervisorHandoff).Clone() },
		},
		{
			name:  "SupervisorHandoffReleaseProof",
			value: durableReleaseProofFixture(t),
			equal: func(left, right any) bool {
				return left.(SupervisorHandoffReleaseProof).Equal(right.(SupervisorHandoffReleaseProof))
			},
			clone: func(value any) any { return value.(SupervisorHandoffReleaseProof).Clone() },
		},
		{
			name:  "StopReceipt",
			value: durableStopReceiptFixture(),
			equal: func(left, right any) bool { return left.(StopReceipt).Equal(right.(StopReceipt)) },
		},
	}
}

func durableStopReceiptFixture() StopReceipt {
	anchor := LinuxCgroupProcessIdentity{PID: 10, Identity: "anchor:10:start"}
	helper := LinuxCgroupProcessIdentity{PID: 20, Identity: "helper:20:start"}
	launcher := LinuxCgroupProcessIdentity{PID: 30, Identity: "launcher:30:start"}
	mirror := LinuxCgroupProcessIdentity{PID: 50, Identity: "daemon:50:start"}
	launcherExit := LinuxCgroupExitProof{
		Version: LinuxCgroupIdentityVersion,
		Process: launcher,
		Witness: helper,
		Waited:  true,
		Reaped:  true,
	}
	gateExit := LinuxCgroupExitProof{
		Version: LinuxCgroupIdentityVersion,
		Process: LinuxCgroupProcessIdentity{PID: 40, Identity: "gate:40:start"},
		Witness: helper,
		Waited:  true,
		Reaped:  true,
	}
	adoptedHelperExit := LinuxCgroupExitProof{
		Version: LinuxCgroupIdentityVersion,
		Process: helper,
		Witness: anchor,
		Waited:  true,
		Reaped:  true,
	}
	mirrorGateExit := gateExit
	mirrorGateExit.Witness = anchor
	mirrorGateExit.HelperExit = &adoptedHelperExit
	cgroup := LinuxCgroupIdentity{
		Version: LinuxCgroupIdentityVersion,
		Parent: LinuxCgroupParentIdentity{
			BootID: "00000000-0000-4000-8000-000000000001", MountID: 42, Device: 84, Inode: 126,
			Path: "/system.slice/symmetry.slice/delegated",
		},
		LeafName: strings.Repeat("e", TokenBytes*2), LeafDevice: 168, LeafInode: 210,
	}
	return StopReceipt{
		Version:            SupervisorVersionV2,
		Status:             "stopped",
		OwnerKind:          OwnerKindLinuxHelper,
		OwnerContext:       "/run/symmetry/recovery.sock",
		TargetPID:          gateExit.Process.PID,
		TargetIdentity:     gateExit.Process.Identity,
		OwnerUID:           1000,
		PipeToken:          strings.Repeat("c", TokenBytes*2),
		JobID:              strings.Repeat("d", TokenBytes*2),
		SupervisorPID:      helper.PID,
		SupervisorIdentity: helper.Identity,
		ActiveProcesses:    0,
		TargetBirthKind:    LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity:     &cgroup,
		LinuxProof: &LinuxCgroupStopProof{
			Version:                LinuxCgroupIdentityVersion,
			Kind:                   LinuxCgroupStopProofMirror,
			Issuer:                 mirror,
			Reaper:                 anchor,
			PinnedLeafFD:           true,
			CgroupKillAcknowledged: true,
			CgroupPopulatedZero:    true,
			NoMigrationObserved:    true,
			ProducerFenced:         true,
			DescendantsReaped:      true,
			LauncherExit:           launcherExit,
			GateExit:               mirrorGateExit,
			HelperExit:             &adoptedHelperExit,
		},
	}
}

func durableValidHandoff(t *testing.T) SupervisorHandoff {
	t.Helper()
	handoff := testLinuxCgroupV2Handoff()
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("v2 fixture failed ToSupervisor(): %v", err)
	}
	receipt := testLinuxCgroupV2Receipt(value)
	handoff.StopReceipt = &receipt
	if err := handoff.Validate(); err != nil {
		t.Fatalf("v2 fixture failed Validate(): %v", err)
	}
	return handoff
}

func durableHandoffFixture(t *testing.T) SupervisorHandoff {
	t.Helper()
	handoff := durableValidHandoff(t)
	receipt := durableStopReceiptFixture()
	handoff.StopReceipt = &receipt
	update := SupervisorScanUpdate{
		SupervisorPID: 20, SupervisorIdentity: "helper:20:start", MirrorIdentity: strings.Repeat("f", TokenBytes*2),
		Owner: SupervisorScanOwnerHelper, Sequence: 3, Phase: SupervisorScanPhaseClean,
	}
	handoff.ScanUpdate = &update
	return handoff
}

func durableSupervisorFixture(t *testing.T) Supervisor {
	t.Helper()
	value, err := durableValidHandoff(t).ToSupervisor()
	if err != nil {
		t.Fatalf("v2 fixture failed ToSupervisor(): %v", err)
	}
	creatorSessionID := uint32(0)
	value.CreatorSessionID = &creatorSessionID
	return value
}

func durableReleaseProofFixture(t *testing.T) SupervisorHandoffReleaseProof {
	t.Helper()
	return releaseProofForHandoff(durableValidHandoff(t))
}

func releaseProofForHandoff(handoff SupervisorHandoff) SupervisorHandoffReleaseProof {
	return SupervisorHandoffReleaseProof{
		Version: SupervisorHandoffVersionV2, Status: "released",
		LaunchToken: handoff.LaunchToken, OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext,
		TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity,
		OwnerUID: handoff.OwnerUID, LauncherUID: handoff.LauncherUID,
		PipeToken: handoff.PipeToken, JobID: handoff.JobID,
		SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
		TargetBirthKind: handoff.TargetBirthKind,
		CgroupIdentity:  cloneLinuxCgroupIdentity(handoff.CgroupIdentity),
		AnchorPID:       handoff.AnchorPID, AnchorIdentity: handoff.AnchorIdentity,
		LauncherPID: handoff.LauncherPID, LauncherIdentity: handoff.LauncherIdentity,
		LauncherExit:    cloneLinuxCgroupExitProof(handoff.LauncherExit),
		Mirror:          cloneLinuxCgroupProcessIdentity(handoff.Mirror),
		LeafDisposition: LinuxCgroupLeafRemoved, StopReceipt: cloneStopReceipt(handoff.StopReceipt),
	}
}

func TestSupervisorRecordsEqualSensitivityPerField(t *testing.T) {
	for _, record := range durableSupervisorRecords(t) {
		t.Run(record.name, func(t *testing.T) {
			for _, path := range durableFieldPaths(reflect.ValueOf(record.value)) {
				if strings.HasPrefix(path, "ScanUpdate") {
					continue // request-only envelope; TestSupervisorHandoffScanUpdateIsRequestOnly covers it
				}
				if record.equal(record.value, mutateDurablePath(t, record.value, path)) {
					t.Errorf("Equal() ignored changed durable field %s", path)
				}
			}
		})
	}
}

func TestSupervisorRecordsCloneIndependence(t *testing.T) {
	for _, record := range durableSupervisorRecords(t) {
		if record.clone == nil {
			continue // StopReceipt has no Clone method; Supervisor.Clone carries it
		}
		t.Run(record.name, func(t *testing.T) {
			snapshot := deepCopyDurableValue(reflect.ValueOf(record.value)).Interface()
			cloned := record.clone(record.value)
			assertDurablePointersIndependent(t, record.value, cloned)
			mutateDurableSharedState(reflect.ValueOf(record.value))
			if !record.equal(cloned, snapshot) {
				t.Error("Clone() shares mutable state with the original")
			}
			if record.equal(record.value, snapshot) {
				t.Fatal("fixture mutation did not change the original")
			}
		})
	}
}

func TestSupervisorHandoffScanUpdateIsRequestOnly(t *testing.T) {
	handoff := durableHandoffFixture(t)
	if handoff.ScanUpdate == nil {
		t.Fatal("fixture is missing the request-only envelope")
	}
	plain := handoff
	plain.ScanUpdate = nil
	if !handoff.Equal(plain) {
		t.Fatal("Equal() compared the request-only ScanUpdate envelope")
	}
	if !handoff.SameLaunch(plain) {
		t.Fatal("SameLaunch() compared the request-only ScanUpdate envelope")
	}
	cloned := handoff.Clone()
	if cloned.ScanUpdate == handoff.ScanUpdate || cloned.ScanUpdate == nil || *cloned.ScanUpdate != *handoff.ScanUpdate {
		t.Fatal("Clone() did not copy the request-only ScanUpdate envelope independently")
	}
	handoff.ScanUpdate.Phase = SupervisorScanPhaseUnproven
	if cloned.ScanUpdate.Phase != SupervisorScanPhaseClean {
		t.Fatal("Clone() shared the request-only ScanUpdate envelope")
	}

	if err := plain.Validate(); err != nil {
		t.Fatalf("fixture without the envelope failed Validate(): %v", err)
	}
	if _, err := plain.ToSupervisor(); err != nil {
		t.Fatalf("fixture without the envelope failed ToSupervisor(): %v", err)
	}
	if _, err := handoff.ToSupervisor(); err == nil || err.Error() != "supervisor handoff is invalid" {
		t.Fatalf("ToSupervisor() on a v2 handoff holding the envelope = %v, want the handoff-shape rejection", err)
	}

	legacy := SupervisorHandoff{
		Version:        SupervisorHandoffVersion,
		LaunchToken:    strings.Repeat("a", TokenBytes*2),
		Secret:         strings.Repeat("b", SecretBytes*2),
		TargetPID:      40,
		TargetIdentity: "gate:40:start",
		OwnerKind:      OwnerKindLinuxHelper,
		OwnerContext:   "/run/symmetry/recovery.sock",
		PipeToken:      strings.Repeat("c", TokenBytes*2),
		JobID:          strings.Repeat("d", TokenBytes*2),
		ScanUpdate: &SupervisorScanUpdate{
			SupervisorPID: 20, SupervisorIdentity: "helper:20:start",
			MirrorIdentity: strings.Repeat("e", TokenBytes*2),
			Owner:          SupervisorScanOwnerHelper, Sequence: 3, Phase: SupervisorScanPhaseClean,
		},
	}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("v1 handoff with a request-only envelope failed Validate(): %v", err)
	}
	if _, err := legacy.ToSupervisor(); err == nil || err.Error() != "supervisor scan update is not a durable authority" {
		t.Fatalf("ToSupervisor() on a v1 request-only envelope = %v, want the scan-update rejection", err)
	}

	encoded, err := json.Marshal(handoff)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "ScanUpdate") || strings.Contains(string(encoded), "scan_update") {
		t.Fatalf("handoff JSON emitted the request-only envelope: %s", encoded)
	}
}

func TestSupervisorHandoffSameLaunchCoversLaunchBindingOnly(t *testing.T) {
	handoff := durableHandoffFixture(t)
	ignored := map[string]bool{"SupervisorPID": true, "SupervisorIdentity": true, "Mirror": true, "StopReceipt": true, "ScanUpdate": true}
	for _, path := range durableFieldPaths(reflect.ValueOf(handoff)) {
		mutated := mutateDurablePath(t, handoff, path).(SupervisorHandoff)

		want := ignored[strings.SplitN(path, ".", 2)[0]]
		if got := handoff.SameLaunch(mutated); got != want {
			t.Errorf("SameLaunch() after changing %s = %v, want %v", path, got, want)
		}
	}
}

func TestSupervisorConversionParityAgainstBaseFieldSet(t *testing.T) {
	handoff := durableValidHandoff(t)
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("ToSupervisor() error = %v", err)
	}
	assertDurableFieldParityByName(t, "ToSupervisor", value, handoff, nil)
	assertDurablePointersIndependent(t, handoff, value)
	if value.StopReceipt == nil || !value.StopReceipt.Equal(*handoff.StopReceipt) {
		t.Fatal("ToSupervisor() lost the terminal stop receipt")
	}

	if got := supervisorFromCore(supervisorCore{LauncherUID: 7}).LauncherUID; got != 7 {
		t.Fatalf("supervisorFromCore() dropped the reserved-zero LauncherUID mapping: got %d", got)
	}

	projection := supervisorFromHandoff(handoff)
	assertDurableFieldParityByName(t, "supervisorFromHandoff", projection, handoff, map[string]bool{"StopReceipt": true})
	if projection.StopReceipt != nil {
		t.Fatal("supervisorFromHandoff() gained a stop receipt; it is the compare-only projection")
	}

	proof := releaseProofForHandoff(handoff)
	if err := proof.Validate(); err != nil {
		t.Fatalf("release proof fixture Validate() error = %v", err)
	}
	converted := supervisorFromReleaseProof(proof)
	assertDurableFieldParityByName(t, "supervisorFromReleaseProof", converted, proof,
		map[string]bool{"Secret": true, "CreatorSessionID": true})
	assertDurablePointersIndependent(t, proof, converted)
	if converted.Version != SupervisorVersionV2 || converted.Secret != "" || converted.CreatorSessionID != nil {
		t.Fatalf("supervisorFromReleaseProof() shape = version %d, secret %q, creator session %v",
			converted.Version, converted.Secret, converted.CreatorSessionID)
	}
	if converted.StopReceipt == nil || !converted.StopReceipt.Equal(*proof.StopReceipt) {
		t.Fatal("supervisorFromReleaseProof() lost the terminal stop receipt")
	}

	withSession := proof
	creatorSessionID := uint32(0)
	withSession.CreatorSessionID = &creatorSessionID
	if subject := supervisorFromReleaseProof(withSession); subject.CreatorSessionID != nil {
		t.Fatal("supervisorFromReleaseProof() copied the creator session")
	}
}

func TestStopReceiptValidForComparesEveryReceiptField(t *testing.T) {
	handoff := durableValidHandoff(t)
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("ToSupervisor() error = %v", err)
	}
	receipt := testLinuxCgroupV2Receipt(value)
	if !receipt.ValidFor(value) || !receipt.ValidForHandoff(handoff) {
		t.Fatal("fixture receipt does not validate for its exact authority")
	}
	for _, path := range durableFieldPaths(reflect.ValueOf(receipt)) {
		mutated := mutateDurablePath(t, receipt, path).(StopReceipt)
		if mutated.ValidFor(value) {
			t.Errorf("ValidFor() ignored changed receipt field %s", path)
		}
		if mutated.ValidForHandoff(handoff) {
			t.Errorf("ValidForHandoff() ignored changed receipt field %s", path)
		}
	}
}

func TestStopReceiptValidForComparesExactAuthorityIdentity(t *testing.T) {
	handoff := durableValidHandoff(t)
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("ToSupervisor() error = %v", err)
	}
	receipt := testLinuxCgroupV2Receipt(value)
	if !receipt.ValidFor(value) || !receipt.ValidForHandoff(handoff) {
		t.Fatal("fixture receipt does not validate for its exact authority")
	}

	ignored := map[string]bool{
		"Secret": true, "LaunchToken": true, "CreatorSessionID": true, "StopReceipt": true,
		"ScanUpdate": true, "Mirror": true, "AnchorPID": true, "AnchorIdentity": true,
	}
	for _, path := range durableFieldPaths(reflect.ValueOf(value)) {
		mutated := mutateDurablePath(t, value, path).(Supervisor)

		want := ignored[strings.SplitN(path, ".", 2)[0]]
		if got := receipt.ValidFor(mutated); got != want {
			t.Errorf("ValidFor() after changing Supervisor.%s = %v, want %v", path, got, want)
		}
	}
	for _, path := range durableFieldPaths(reflect.ValueOf(handoff)) {
		mutated := mutateDurablePath(t, handoff, path).(SupervisorHandoff)
		want := ignored[strings.SplitN(path, ".", 2)[0]]
		if got := receipt.ValidForHandoff(mutated); got != want {
			t.Errorf("ValidForHandoff() after changing SupervisorHandoff.%s = %v, want %v", path, got, want)
		}
	}
}

func TestReleaseProofValidForComparesExactIdentity(t *testing.T) {
	handoff := durableValidHandoff(t)
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("ToSupervisor() error = %v", err)
	}
	proof := releaseProofForHandoff(handoff)
	if err := proof.Validate(); err != nil {
		t.Fatalf("release proof Validate() error = %v", err)
	}
	if !proof.ValidFor(handoff) || !proof.ValidForSupervisor(value) {
		t.Fatal("release proof does not validate for its exact handoff and authority")
	}

	for _, path := range durableFieldPaths(reflect.ValueOf(proof)) {
		mutated := mutateDurablePath(t, proof, path).(SupervisorHandoffReleaseProof)

		want := strings.SplitN(path, ".", 2)[0] == "CreatorSessionID"
		if got := mutated.ValidFor(handoff); got != want {
			t.Errorf("ValidFor() after changing SupervisorHandoffReleaseProof.%s = %v, want %v", path, got, want)
		}
		if got := mutated.ValidForSupervisor(value); got != want {
			t.Errorf("ValidForSupervisor() after changing SupervisorHandoffReleaseProof.%s = %v, want %v", path, got, want)
		}
	}

	recordIgnored := map[string]bool{"Secret": true, "CreatorSessionID": true, "ScanUpdate": true}

	withSession := proof
	creatorSessionID := uint32(0)
	withSession.CreatorSessionID = &creatorSessionID
	if !withSession.ValidFor(handoff) || !withSession.ValidForSupervisor(value) {
		t.Fatal("release proof comparison rejected a proof-side creator session")
	}
	for _, path := range durableFieldPaths(reflect.ValueOf(handoff)) {
		mutated := mutateDurablePath(t, handoff, path).(SupervisorHandoff)
		want := recordIgnored[strings.SplitN(path, ".", 2)[0]]
		if got := proof.ValidFor(mutated); got != want {
			t.Errorf("ValidFor() after changing SupervisorHandoff.%s = %v, want %v", path, got, want)
		}
	}
	for _, path := range durableFieldPaths(reflect.ValueOf(value)) {
		mutated := mutateDurablePath(t, value, path).(Supervisor)
		want := recordIgnored[strings.SplitN(path, ".", 2)[0]]
		if got := proof.ValidForSupervisor(mutated); got != want {
			t.Errorf("ValidForSupervisor() after changing Supervisor.%s = %v, want %v", path, got, want)
		}
	}
}

func TestValidatePinsV2HelperBindingAndMirrorOnlyV1Records(t *testing.T) {
	bound := durableValidHandoff(t)
	value, err := bound.ToSupervisor()
	if err != nil {
		t.Fatalf("ToSupervisor() error = %v", err)
	}
	proof := releaseProofForHandoff(bound)

	legacyHandoff := testAuthoritySupervisorHandoffFixture()
	legacyHandoff.SupervisorPID = 21
	legacyHandoff.SupervisorIdentity = "legacy-helper:21:start"
	legacyValue, err := legacyHandoff.ToSupervisor()
	if err != nil {
		t.Fatalf("legacy ToSupervisor() error = %v", err)
	}
	legacyProof := legacyHandoff.ReleaseProof()

	if err := value.Validate(); err != nil {
		t.Fatalf("bound v2 authority failed Validate(): %v", err)
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("bound v2 release proof failed Validate(): %v", err)
	}
	if err := legacyValue.Validate(); err != nil {
		t.Fatalf("bound v1 authority failed Validate(): %v", err)
	}
	if err := legacyHandoff.Validate(); err != nil {
		t.Fatalf("bound v1 handoff failed Validate(): %v", err)
	}
	if err := legacyProof.Validate(); err != nil {
		t.Fatalf("bound v1 release proof failed Validate(): %v", err)
	}

	unboundValue := value
	unboundValue.SupervisorPID, unboundValue.SupervisorIdentity, unboundValue.Mirror, unboundValue.StopReceipt = 0, "", nil, nil
	unboundProof := proof
	unboundProof.SupervisorPID, unboundProof.SupervisorIdentity, unboundProof.Mirror, unboundProof.StopReceipt = 0, "", nil, nil

	mirrorOnly := LinuxCgroupProcessIdentity{PID: 51, Identity: "daemon:51:start"}
	legacyValue.Mirror = &mirrorOnly
	legacyHandoff.Mirror = &mirrorOnly
	legacyProof.Mirror = &mirrorOnly

	cases := []struct {
		name     string
		validate func() error
		want     string
	}{
		{"v2 supervisor without a bound helper", unboundValue.Validate, "supervisor authority is invalid"},
		{"v2 release proof without a bound helper", unboundProof.Validate, "supervisor handoff release proof is invalid"},
		{"v1 supervisor carrying only Mirror", legacyValue.Validate, "supervisor authority is invalid"},
		{"v1 handoff carrying only Mirror", legacyHandoff.Validate, "supervisor handoff is invalid"},
		{"v1 release proof carrying only Mirror", legacyProof.Validate, "supervisor handoff release proof is invalid"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.validate()
			if err == nil {
				t.Fatalf("Validate() accepted %s", testCase.name)
			}
			if err.Error() != testCase.want {
				t.Fatalf("Validate() error = %q, want %q", err, testCase.want)
			}
		})
	}
}

func durableFieldPaths(value reflect.Value) []string {
	var paths []string
	collectDurableFieldPaths(value, "", &paths)
	return paths
}

func collectDurableFieldPaths(value reflect.Value, prefix string, paths *[]string) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		collectDurableFieldPaths(value.Elem(), prefix, paths)
		return
	}
	if value.Kind() != reflect.Struct {
		panic("durableFieldPaths: want struct or pointer to struct, got " + value.Kind().String())
	}
	for index := range value.NumField() {
		field := value.Type().Field(index)
		if field.PkgPath != "" {
			panic("durableFieldPaths: unexported field " + field.Name)
		}
		path := field.Name
		if prefix != "" {
			path = prefix + "." + field.Name
		}
		*paths = append(*paths, path)
		if field.Tag.Get("json") == "-" {
			continue
		}
		switch field.Type.Kind() {
		case reflect.Pointer:
			if field.Type.Elem().Kind() == reflect.Struct {
				collectDurableFieldPaths(value.Field(index), path, paths)
			}
		case reflect.Struct:
			collectDurableFieldPaths(value.Field(index), path, paths)
		}
	}
}

func mutateDurablePath(t *testing.T, value any, path string) any {
	t.Helper()
	copied := deepCopyDurableValue(reflect.ValueOf(value))
	target := copied
	for _, part := range strings.Split(path, ".") {
		if target.Kind() == reflect.Pointer {
			target = target.Elem()
		}
		target = target.FieldByName(part)
		if !target.IsValid() {
			t.Fatalf("mutateDurablePath: no field %s in %s", part, path)
		}
	}
	mutateDurableField(target, path)
	return copied.Interface()
}

func mutateDurableField(target reflect.Value, path string) {
	switch target.Kind() {
	case reflect.String:
		target.SetString(target.String() + "-mutated")
	case reflect.Bool:
		target.SetBool(!target.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		target.SetInt(target.Int() + 7919)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		target.SetUint(target.Uint() + 7919)
	case reflect.Pointer:
		replacement := reflect.New(target.Type().Elem())
		populateDurableValue(replacement.Elem(), path, map[reflect.Type]bool{})
		target.Set(replacement)
	case reflect.Struct:
		for index := range target.NumField() {
			if target.Field(index).CanSet() {
				mutateDurableField(target.Field(index), path)
				return
			}
		}
		panic("mutateDurableField: no settable field under " + path)
	default:
		panic("mutateDurableField: unsupported kind " + target.Kind().String() + " at " + path)
	}
}

func populateDurableValue(value reflect.Value, prefix string, ancestors map[reflect.Type]bool) {
	switch value.Kind() {
	case reflect.Pointer:
		element := value.Type().Elem()
		if ancestors[element] {
			value.Set(reflect.Zero(value.Type()))
			return
		}
		value.Set(reflect.New(element))
		populateDurableValue(value.Elem(), prefix, ancestors)
	case reflect.Struct:
		ancestors[value.Type()] = true
		for index := range value.NumField() {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				panic("populateDurableValue: unexported field " + field.Name)
			}
			populateDurableValue(value.Field(index), prefix+"."+field.Name, ancestors)
		}
		delete(ancestors, value.Type())
	case reflect.String:
		value.SetString("durable" + prefix)
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(int64(len(prefix)) + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(uint64(len(prefix)) + 1)
	default:
		panic("populateDurableValue: unsupported kind " + value.Kind().String())
	}
}

func deepCopyDurableValue(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		copied := reflect.New(value.Type().Elem())
		copied.Elem().Set(deepCopyDurableValue(value.Elem()))
		return copied
	case reflect.Struct:
		copied := reflect.New(value.Type()).Elem()
		for index := range value.NumField() {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				panic("deepCopyDurableValue: unexported field " + field.Name)
			}
			copied.Field(index).Set(deepCopyDurableValue(value.Field(index)))
		}
		return copied
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value
	default:
		panic("deepCopyDurableValue: durable records must not hold " + value.Kind().String() + " fields")
	}
}

func mutateDurableSharedState(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return
		}
		mutateDurablePointerTargets(value.Elem())
	case reflect.Struct:
		for index := range value.NumField() {
			field := value.Field(index)
			switch field.Kind() {
			case reflect.Pointer, reflect.Struct:
				mutateDurableSharedState(field)
			}
		}
	default:
		panic("mutateDurableSharedState: want struct or pointer, got " + value.Kind().String())
	}
}

func mutateDurablePointerTargets(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return
		}
		mutateDurablePointerTargets(value.Elem())
	case reflect.Struct:
		for index := range value.NumField() {
			mutateDurablePointerTargets(value.Field(index))
		}
	case reflect.String:
		value.SetString(value.String() + "-shared")
	case reflect.Bool:
		value.SetBool(!value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(value.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(value.Uint() + 1)
	default:
		panic("mutateDurablePointerTargets: unsupported kind " + value.Kind().String())
	}
}

func assertDurablePointersIndependent(t *testing.T, original, cloned any) {
	t.Helper()
	assertPointersIndependent(t, "", reflect.ValueOf(original), reflect.ValueOf(cloned))
}

func assertPointersIndependent(t *testing.T, path string, original, clone reflect.Value) {
	t.Helper()
	if original.Kind() != clone.Kind() {
		t.Fatalf("%s: compared %s with %s", path, original.Kind(), clone.Kind())
	}
	switch original.Kind() {
	case reflect.Pointer:
		if original.IsNil() {
			if !clone.IsNil() {
				t.Errorf("%s: clone gained a pointer", path)
			}
			return
		}
		if clone.IsNil() {
			t.Errorf("%s: clone dropped a pointer", path)
			return
		}
		if original.Pointer() == clone.Pointer() {
			t.Errorf("%s: clone shares the original pointer", path)
		}
		assertPointersIndependent(t, path, original.Elem(), clone.Elem())
	case reflect.Struct:
		for index := range original.NumField() {
			name := original.Type().Field(index).Name
			child := name
			if path != "" {
				child = path + "." + name
			}
			mirror := clone
			if original.Type() != clone.Type() {

				mirror = clone.FieldByName(name)
				if !mirror.IsValid() {
					continue
				}
			} else {
				mirror = clone.Field(index)
			}
			assertPointersIndependent(t, child, original.Field(index), mirror)
		}
	}
}

func assertDurableFieldParityByName(t *testing.T, label string, got any, source any, skip map[string]bool) {
	t.Helper()
	gotValue := reflect.ValueOf(got)
	sourceValue := reflect.ValueOf(source)
	for index := range gotValue.NumField() {
		field := gotValue.Type().Field(index)
		if skip[field.Name] {
			continue
		}
		mirror := sourceValue.FieldByName(field.Name)
		if !mirror.IsValid() {
			t.Fatalf("%s: source %s has no field %s", label, sourceValue.Type(), field.Name)
		}
		if !reflect.DeepEqual(gotValue.Field(index).Interface(), mirror.Interface()) {
			t.Errorf("%s: %s = %#v, want the source value %#v", label, field.Name,
				gotValue.Field(index).Interface(), mirror.Interface())
		}
	}
}
