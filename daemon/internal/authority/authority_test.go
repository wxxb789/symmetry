package authority

import (
	"encoding/json"
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

func TestSupervisorHandoffLinuxAbortProofDoesNotRequireCreatorSession(t *testing.T) {
	handoff := testAuthoritySupervisorHandoffFixture()
	handoff.OwnerKind = OwnerKindLinuxHelper
	handoff.OwnerContext = "helper-endpoint:linux:run-1"
	if err := handoff.Validate(); err != nil {
		t.Fatalf("Linux handoff Validate() error = %v", err)
	}
	proof := SupervisorHandoffAbortProof{
		Version:        SupervisorHandoffAbortProofVersion,
		Disposition:    SupervisorHandoffAbortDisposition,
		LaunchToken:    handoff.LaunchToken,
		JobID:          handoff.JobID,
		OwnerKind:      handoff.OwnerKind,
		OwnerContext:   handoff.OwnerContext,
		TargetPID:      handoff.TargetPID,
		TargetIdentity: handoff.TargetIdentity,
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("Linux abort proof Validate() error = %v", err)
	}
	if !proof.ValidFor(handoff) {
		t.Fatal("Linux abort proof did not match exact owner/target binding")
	}

	proof.OwnerContext = "helper-endpoint:linux:other"
	if proof.ValidFor(handoff) {
		t.Fatal("Linux abort proof matched a different owner context")
	}
	proof.OwnerContext = handoff.OwnerContext
	proof.TargetIdentity = "linux:other"
	if proof.ValidFor(handoff) {
		t.Fatal("Linux abort proof matched a different target identity")
	}
}

func TestSupervisorHandoffOwnerBindingPropagatesThroughCloneConversionAndProofs(t *testing.T) {
	handoff := testAuthoritySupervisorHandoffFixture()
	handoff.OwnerKind = OwnerKindLinuxHelper
	handoff.OwnerContext = "helper-endpoint:linux:run-1"
	handoff.SupervisorPID = 99
	handoff.SupervisorIdentity = "linux:99:start-time"
	cloned := handoff.Clone()
	if cloned.OwnerKind != handoff.OwnerKind || cloned.OwnerContext != handoff.OwnerContext {
		t.Fatalf("Clone() lost owner binding: %#v", cloned)
	}
	if !cloned.Equal(handoff) || !cloned.SameLaunch(handoff) {
		t.Fatal("Clone() changed owner-bound handoff equality")
	}

	converted, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("ToSupervisor() error = %v", err)
	}
	if converted.OwnerKind != handoff.OwnerKind || converted.OwnerContext != handoff.OwnerContext {
		t.Fatalf("ToSupervisor() lost owner binding: %#v", converted)
	}

	release := handoff.ReleaseProof()
	if err := release.Validate(); err != nil {
		t.Fatalf("ReleaseProof Validate() error = %v", err)
	}
	if !release.ValidFor(handoff) {
		t.Fatal("ReleaseProof did not match exact owner binding")
	}
	release.OwnerKind = OwnerKindLegacyWindows
	if release.ValidFor(handoff) {
		t.Fatal("ReleaseProof matched a different owner kind")
	}
}

func TestSupervisorHandoffOwnerValidationRejectsUnknownOrPartialFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SupervisorHandoff)
	}{
		{name: "context without kind", mutate: func(value *SupervisorHandoff) { value.OwnerContext = "context" }},
		{name: "kind without context", mutate: func(value *SupervisorHandoff) { value.OwnerKind = OwnerKindLinuxHelper }},
		{name: "unknown kind", mutate: func(value *SupervisorHandoff) { value.OwnerKind = "other"; value.OwnerContext = "context" }},
		{name: "legacy abort session missing", mutate: func(value *SupervisorHandoff) {
			value.OwnerKind = OwnerKindLegacyWindows
			value.OwnerContext = "windows-session"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := testAuthoritySupervisorHandoffFixture()
			test.mutate(&value)
			if err := value.Validate(); err == nil && test.name != "legacy abort session missing" {
				t.Fatal("Validate() accepted an invalid owner binding")
			}
		})
	}

	legacy := testAuthoritySupervisorHandoffFixture()
	legacy.OwnerKind = OwnerKindLegacyWindows
	legacy.OwnerContext = "windows-session"
	proof := legacy.ReleaseProof()
	if err := proof.Validate(); err != nil {
		t.Fatalf("legacy release proof Validate() error = %v", err)
	}
	abort := SupervisorHandoffAbortProof{
		Version:        SupervisorHandoffAbortProofVersion,
		Disposition:    SupervisorHandoffAbortDisposition,
		LaunchToken:    legacy.LaunchToken,
		JobID:          legacy.JobID,
		OwnerKind:      legacy.OwnerKind,
		OwnerContext:   legacy.OwnerContext,
		TargetPID:      legacy.TargetPID,
		TargetIdentity: legacy.TargetIdentity,
	}
	if err := abort.Validate(); err == nil {
		t.Fatal("legacy Windows abort proof accepted a missing creator session ID")
	}
}

func TestStopReceiptBindsOwnerAndPreservesLegacyJSON(t *testing.T) {
	handoff := testAuthoritySupervisorHandoffFixture()
	handoff.SupervisorPID = 99
	handoff.SupervisorIdentity = "windows:99:created-at"
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("ToSupervisor() error = %v", err)
	}
	receipt := StopReceipt{
		Version:            SupervisorVersion,
		Status:             "stopped",
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
	}
	if !receipt.ValidFor(value) {
		t.Fatal("stop receipt did not match legacy authority")
	}
	value.OwnerKind = OwnerKindLinuxHelper
	value.OwnerContext = "helper-endpoint:linux:run-1"
	if receipt.ValidFor(value) {
		t.Fatal("legacy stop receipt matched owner-bound authority")
	}
	receipt.OwnerKind = value.OwnerKind
	receipt.OwnerContext = value.OwnerContext
	if !receipt.ValidFor(value) {
		t.Fatal("owner-bound stop receipt did not match authority")
	}

	legacyJSON := `{"version":1,"secret":"` + strings.Repeat("a", SecretBytes*2) + `","target_pid":42,"target_identity":"windows:42:created-at","pipe_token":"` + strings.Repeat("b", TokenBytes*2) + `","job_id":"` + strings.Repeat("c", TokenBytes*2) + `","supervisor_pid":99,"supervisor_identity":"windows:99:created-at"}`
	var legacy Supervisor
	if err := json.Unmarshal([]byte(legacyJSON), &legacy); err != nil {
		t.Fatalf("legacy JSON unmarshal error = %v", err)
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("legacy JSON marshal error = %v", err)
	}
	if strings.Contains(string(encoded), "owner_kind") || strings.Contains(string(encoded), "owner_context") {
		t.Fatalf("legacy JSON unexpectedly emitted additive owner fields: %s", encoded)
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
