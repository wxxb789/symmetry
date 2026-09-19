package authority

import (
	"strings"
	"testing"
)

func TestSupervisorHandoffValidationAndConversion(t *testing.T) {
	handoff := testAuthoritySupervisorHandoffFixture()
	if err := handoff.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if _, err := handoff.ToSupervisor(); err == nil {
		t.Fatal("ToSupervisor() accepted an unbound helper")
	}

	handoff.SupervisorPID = 99
	handoff.SupervisorIdentity = "windows:99:created-at"
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("ToSupervisor() error = %v", err)
	}
	if value.TargetPID != handoff.TargetPID || value.SupervisorPID != handoff.SupervisorPID || value.Secret != handoff.Secret || value.LaunchToken != handoff.LaunchToken {
		t.Fatalf("converted authority = %#v, want target/helper binding", value)
	}
}

func TestSupervisorHandoffRejectsPartialAndConflictingFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SupervisorHandoff)
	}{
		{name: "helper pid only", mutate: func(value *SupervisorHandoff) { value.SupervisorPID = 99 }},
		{name: "helper identity only", mutate: func(value *SupervisorHandoff) { value.SupervisorIdentity = "windows:99:created-at" }},
		{name: "receipt without helper", mutate: func(value *SupervisorHandoff) {
			value.StopReceipt = &StopReceipt{Version: SupervisorVersion, Status: "stopped"}
		}},
		{name: "invalid launch token", mutate: func(value *SupervisorHandoff) { value.LaunchToken = "launch" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := testAuthoritySupervisorHandoffFixture()
			test.mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("Validate() accepted an unsafe handoff")
			}
		})
	}
}

func TestSupervisorHandoffReleaseProofBindsExactIdentity(t *testing.T) {
	handoff := testAuthoritySupervisorHandoffFixture()
	handoff.SupervisorPID = 99
	handoff.SupervisorIdentity = "windows:99:created-at"
	creatorSessionID := uint32(0)
	handoff.CreatorSessionID = &creatorSessionID
	proof := handoff.ReleaseProof()
	if proof.CreatorSessionID == handoff.CreatorSessionID || proof.CreatorSessionID == nil || *proof.CreatorSessionID != 0 {
		t.Fatalf("ReleaseProof() CreatorSessionID = %v, want independent session 0 pointer", proof.CreatorSessionID)
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("release proof Validate() error = %v", err)
	}
	if !proof.ValidFor(handoff) {
		t.Fatal("release proof did not match exact handoff")
	}
	proof.TargetIdentity = "windows:other"
	if proof.ValidFor(handoff) {
		t.Fatal("release proof matched a different target identity")
	}
}

func TestSupervisorHandoffAbortProofBindsExactPreAuthorityIdentity(t *testing.T) {
	handoff := testAuthoritySupervisorHandoffFixture()
	creatorSessionID := uint32(0)
	handoff.CreatorSessionID = &creatorSessionID
	proof := SupervisorHandoffAbortProof{
		Version: SupervisorHandoffAbortProofVersion, Disposition: SupervisorHandoffAbortDisposition,
		LaunchToken: handoff.LaunchToken, JobID: handoff.JobID, TargetPID: handoff.TargetPID,
		TargetIdentity:   handoff.TargetIdentity,
		CreatorSessionID: &creatorSessionID,
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("abort proof Validate() error = %v", err)
	}
	if !proof.ValidFor(handoff) {
		t.Fatal("abort proof did not match exact unbound handoff")
	}

	handoff.SupervisorPID = 99
	handoff.SupervisorIdentity = "windows:99:created-at"
	proof.SupervisorPID = handoff.SupervisorPID
	proof.SupervisorIdentity = handoff.SupervisorIdentity
	if !proof.ValidFor(handoff) {
		t.Fatal("abort proof did not match exact bound handoff")
	}
	proof.JobID = strings.Repeat("e", TokenBytes*2)
	if proof.ValidFor(handoff) {
		t.Fatal("abort proof matched a different Job identity")
	}
	proof.JobID = handoff.JobID
	proof.CreatorSessionID = nil
	if err := proof.Validate(); err == nil {
		t.Fatal("abort proof accepted a missing creator session ID")
	}
	proof.CreatorSessionID = &creatorSessionID
	otherSessionID := uint32(1)
	proof.CreatorSessionID = &otherSessionID
	if proof.ValidFor(handoff) {
		t.Fatal("abort proof matched a different creator session ID")
	}
	proof.CreatorSessionID = &creatorSessionID
	proof.Disposition = "released"
	if err := proof.Validate(); err == nil {
		t.Fatal("abort proof accepted a non-abort disposition")
	}
}

func TestSupervisorHandoffCreatorSessionPresenceDistinguishesMissingAndZero(t *testing.T) {
	missing := testAuthoritySupervisorHandoffFixture()
	missing.SupervisorPID = 99
	missing.SupervisorIdentity = "windows:99:created-at"
	zero := missing.Clone()
	creatorSessionID := uint32(0)
	zero.CreatorSessionID = &creatorSessionID
	if missing.Equal(zero) || missing.SameLaunch(zero) {
		t.Fatal("missing creator session ID was treated as session 0")
	}
	cloned := zero.Clone()
	if cloned.CreatorSessionID == nil || cloned.CreatorSessionID == zero.CreatorSessionID || *cloned.CreatorSessionID != 0 {
		t.Fatalf("Clone() did not preserve independent session 0 pointer: %#v", cloned.CreatorSessionID)
	}
	converted, err := zero.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	if converted.CreatorSessionID == nil || *converted.CreatorSessionID != 0 {
		t.Fatalf("ToSupervisor() lost creator session 0: %#v", converted.CreatorSessionID)
	}
	legacy, err := missing.ToSupervisor()
	if err != nil {
		t.Fatalf("legacy handoff failed ordinary authority conversion: %v", err)
	}
	if legacy.CreatorSessionID != nil {
		t.Fatalf("legacy conversion synthesized creator session ID: %#v", legacy.CreatorSessionID)
	}
}

func testAuthoritySupervisorHandoffFixture() SupervisorHandoff {
	return SupervisorHandoff{
		Version:        SupervisorHandoffVersion,
		LaunchToken:    strings.Repeat("d", TokenBytes*2),
		Secret:         strings.Repeat("a", SecretBytes*2),
		TargetPID:      42,
		TargetIdentity: "windows:42:created-at",
		PipeToken:      strings.Repeat("b", TokenBytes*2),
		JobID:          strings.Repeat("c", TokenBytes*2),
	}
}
