// Package authority contains the small durable binding used to recover a
// machine-local process containment boundary. It is deliberately independent
// of platform and state packages so the journal can store the exact authority
// without making either package own the other's policy.
package authority

import (
	"encoding/hex"
	"errors"
	"strings"
)

const (
	// SupervisorVersion is the first durable supervisor-authority schema.
	SupervisorVersion = 1
	// SupervisorHandoffVersion is the first durable pre-authority handoff schema.
	SupervisorHandoffVersion = 1
	// SupervisorHandoffAbortProofVersion is the first pre-authority abort-proof
	// schema. It is independent from both the handoff and stop-receipt schemas.
	SupervisorHandoffAbortProofVersion = 1
	// SupervisorHandoffAbortDisposition identifies a proof that the launch was
	// definitively aborted before supervisor authority was committed.
	SupervisorHandoffAbortDisposition = "preauth_aborted"
	// SecretBytes is the entropy carried by a supervisor authority secret.
	SecretBytes = 32
	// TokenBytes is the entropy used for a pipe address and Job label.
	TokenBytes = 16
	// OwnerKindLinuxHelper identifies the platform-neutral Linux helper owner.
	OwnerKindLinuxHelper = "linux_helper"
	// OwnerKindLegacyWindows identifies the existing Windows owner/session
	// binding. Empty owner fields remain valid for legacy v1 records.
	OwnerKindLegacyWindows = "legacy_windows"
)

// Supervisor binds one launched target to the exact helper that owns its
// inherited containment handle. Secret is machine-local durable authority; it
// must never be placed in argv, environment, pipe names, or logs.
type Supervisor struct {
	Version int    `json:"version"`
	Secret  string `json:"secret"`
	// LaunchToken binds the committed authority to the helper's exact durable
	// bootstrap endpoint. It is optional only for legacy records created before
	// committed handoff recovery carried this identity.
	LaunchToken        string `json:"launch_token,omitempty"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	// CreatorSessionID is optional for legacy authority records. A present
	// pointer distinguishes Windows session 0 from an unavailable field.
	CreatorSessionID *uint32      `json:"creator_session_id,omitempty"`
	StopReceipt      *StopReceipt `json:"stop_receipt,omitempty"`
}

// SupervisorHandoff is the durable launch record written before an
// independent containment helper can outlive the daemon that launched it.
// SupervisorPID and SupervisorIdentity are an optional pair until the helper
// creation identity has been observed. StopReceipt is written only by the
// dedicated receipt mutation after a positive stop witness exists.
type SupervisorHandoff struct {
	Version            int    `json:"version"`
	LaunchToken        string `json:"launch_token"`
	Secret             string `json:"secret"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	SupervisorPID      int    `json:"supervisor_pid,omitempty"`
	SupervisorIdentity string `json:"supervisor_identity,omitempty"`
	// CreatorSessionID is optional only for legacy handoffs. A present pointer
	// distinguishes Windows session 0 from a missing legacy field.
	CreatorSessionID *uint32      `json:"creator_session_id,omitempty"`
	StopReceipt      *StopReceipt `json:"stop_receipt,omitempty"`
}

// SupervisorHandoffReleaseProof is a narrow, non-durable proof supplied by
// the platform after it has released the helper. It deliberately carries the
// exact handoff identity instead of allowing a caller to clear by PID alone.
// The platform may adopt this shape later; this package does not implement
// platform release behavior.
type SupervisorHandoffReleaseProof struct {
	Version            int     `json:"version"`
	Status             string  `json:"status"`
	LaunchToken        string  `json:"launch_token"`
	OwnerKind          string  `json:"owner_kind,omitempty"`
	OwnerContext       string  `json:"owner_context,omitempty"`
	TargetPID          int     `json:"target_pid"`
	TargetIdentity     string  `json:"target_identity"`
	PipeToken          string  `json:"pipe_token"`
	JobID              string  `json:"job_id"`
	SupervisorPID      int     `json:"supervisor_pid"`
	SupervisorIdentity string  `json:"supervisor_identity"`
	CreatorSessionID   *uint32 `json:"creator_session_id,omitempty"`
}

// SupervisorHandoffAbortProof is a non-durable platform proof that a prepared
// handoff was definitively aborted before authority commit. It deliberately
// contains no StopReceipt: an abort proof cannot be used as evidence that a
// target or Job was stopped. The optional helper identity is retained when a
// helper was created so the release boundary remains identity-bound.
type SupervisorHandoffAbortProof struct {
	Version            int     `json:"version"`
	Disposition        string  `json:"disposition"`
	LaunchToken        string  `json:"launch_token"`
	JobID              string  `json:"job_id"`
	OwnerKind          string  `json:"owner_kind,omitempty"`
	OwnerContext       string  `json:"owner_context,omitempty"`
	TargetPID          int     `json:"target_pid"`
	TargetIdentity     string  `json:"target_identity"`
	SupervisorPID      int     `json:"supervisor_pid,omitempty"`
	SupervisorIdentity string  `json:"supervisor_identity,omitempty"`
	CreatorSessionID   *uint32 `json:"creator_session_id,omitempty"`
}

// StopReceipt is an exact, replayable terminal witness. It intentionally does
// not contain Secret: the authority binding is supplied by the enclosing
// Supervisor record and remains the only credential-bearing value.
type StopReceipt struct {
	Version            int    `json:"version"`
	Status             string `json:"status"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	ActiveProcesses    uint32 `json:"active_processes"`
}

// Validate checks the durable authority shape without interpreting a target's
// platform-specific process identity.
func (value Supervisor) Validate() error {
	if value.Version != SupervisorVersion || value.TargetPID <= 0 || value.SupervisorPID <= 0 {
		return errors.New("supervisor authority is invalid")
	}
	if !validOwner(value.OwnerKind, value.OwnerContext) {
		return errors.New("supervisor authority owner is invalid")
	}
	if value.OwnerKind == OwnerKindLinuxHelper && value.CreatorSessionID != nil {
		return errors.New("Linux supervisor authority cannot carry a Windows creator session")
	}
	if !validHex(value.Secret, SecretBytes*2) || !validOptionalHex(value.LaunchToken, TokenBytes*2) || !validHex(value.PipeToken, TokenBytes*2) || !validHex(value.JobID, TokenBytes*2) {
		return errors.New("supervisor authority token is invalid")
	}
	if !validBounded(value.TargetIdentity, 4096) || !validBounded(value.SupervisorIdentity, 4096) {
		return errors.New("supervisor authority process identity is invalid")
	}
	if value.StopReceipt != nil && !value.StopReceipt.ValidFor(value) {
		return errors.New("supervisor authority stop receipt is invalid")
	}
	return nil
}

// ValidFor reports whether a receipt is a positive stop witness for value.
func (receipt StopReceipt) ValidFor(value Supervisor) bool {
	return receipt.Version == SupervisorVersion && receipt.Status == "stopped" && receipt.ActiveProcesses == 0 &&
		receipt.OwnerKind == value.OwnerKind && receipt.OwnerContext == value.OwnerContext &&
		receipt.TargetPID == value.TargetPID && receipt.TargetIdentity == value.TargetIdentity &&
		receipt.PipeToken == value.PipeToken && receipt.JobID == value.JobID &&
		receipt.SupervisorPID == value.SupervisorPID && receipt.SupervisorIdentity == value.SupervisorIdentity
}

// Clone returns an independent copy suitable for handing across a callback
// boundary. Secret is copied as an ordinary string and is never formatted.
func (value Supervisor) Clone() Supervisor {
	cloned := value
	cloned.CreatorSessionID = cloneUint32(value.CreatorSessionID)
	if value.StopReceipt != nil {
		receipt := *value.StopReceipt
		cloned.StopReceipt = &receipt
	}
	return cloned
}

// Equal compares all durable authority fields, including the secret and any
// terminal receipt. It is used by compare-and-set journal mutations.
func (value Supervisor) Equal(other Supervisor) bool {
	if value.Version != other.Version || value.Secret != other.Secret || value.LaunchToken != other.LaunchToken || value.OwnerKind != other.OwnerKind || value.OwnerContext != other.OwnerContext || value.TargetPID != other.TargetPID ||
		value.TargetIdentity != other.TargetIdentity || value.PipeToken != other.PipeToken || value.JobID != other.JobID ||
		value.SupervisorPID != other.SupervisorPID || value.SupervisorIdentity != other.SupervisorIdentity ||
		!equalUint32(value.CreatorSessionID, other.CreatorSessionID) {
		return false
	}
	if value.StopReceipt == nil || other.StopReceipt == nil {
		return value.StopReceipt == nil && other.StopReceipt == nil
	}
	return *value.StopReceipt == *other.StopReceipt
}

// Validate checks the durable pre-authority handoff shape. A helper identity
// is either completely absent or completely present; partial state is unsafe
// because it cannot identify the retained helper after a daemon restart.
func (value SupervisorHandoff) Validate() error {
	if value.Version != SupervisorHandoffVersion || !validHex(value.LaunchToken, TokenBytes*2) || value.TargetPID <= 0 {
		return errors.New("supervisor handoff is invalid")
	}
	if !validHex(value.Secret, SecretBytes*2) || !validHex(value.PipeToken, TokenBytes*2) || !validHex(value.JobID, TokenBytes*2) {
		return errors.New("supervisor handoff token is invalid")
	}
	if !validBounded(value.TargetIdentity, 4096) {
		return errors.New("supervisor handoff target identity is invalid")
	}
	if !validOwner(value.OwnerKind, value.OwnerContext) {
		return errors.New("supervisor handoff owner is invalid")
	}
	if value.OwnerKind == OwnerKindLinuxHelper && value.CreatorSessionID != nil {
		return errors.New("Linux supervisor handoff cannot carry a Windows creator session")
	}
	if (value.SupervisorPID == 0) != (strings.TrimSpace(value.SupervisorIdentity) == "") || value.SupervisorPID < 0 || !validOptionalBounded(value.SupervisorIdentity, 4096) {
		return errors.New("supervisor handoff helper identity is invalid")
	}
	if value.StopReceipt != nil {
		if value.SupervisorPID <= 0 || !value.StopReceipt.ValidForHandoff(value) {
			return errors.New("supervisor handoff stop receipt is invalid")
		}
	}
	return nil
}

// ToSupervisor converts a fully bound handoff into the existing durable
// authority type. The helper identity is required at the commit boundary.
func (value SupervisorHandoff) ToSupervisor() (Supervisor, error) {
	if err := value.Validate(); err != nil {
		return Supervisor{}, err
	}
	if value.SupervisorPID <= 0 || strings.TrimSpace(value.SupervisorIdentity) == "" {
		return Supervisor{}, errors.New("supervisor handoff helper identity is not bound")
	}
	result := Supervisor{
		Version:            SupervisorVersion,
		Secret:             value.Secret,
		LaunchToken:        value.LaunchToken,
		OwnerKind:          value.OwnerKind,
		OwnerContext:       value.OwnerContext,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		CreatorSessionID:   cloneUint32(value.CreatorSessionID),
	}
	if value.StopReceipt != nil {
		receipt := *value.StopReceipt
		result.StopReceipt = &receipt
	}
	if err := result.Validate(); err != nil {
		return Supervisor{}, err
	}
	return result, nil
}

// Clone returns an independent copy suitable for a journal mutation.
func (value SupervisorHandoff) Clone() SupervisorHandoff {
	cloned := value
	cloned.CreatorSessionID = cloneUint32(value.CreatorSessionID)
	if value.StopReceipt != nil {
		receipt := *value.StopReceipt
		cloned.StopReceipt = &receipt
	}
	return cloned
}

// Equal compares every durable handoff field, including helper identity and
// stop receipt. It is used by exact compare-and-set mutations.
func (value SupervisorHandoff) Equal(other SupervisorHandoff) bool {
	if value.Version != other.Version || value.LaunchToken != other.LaunchToken || value.Secret != other.Secret || value.OwnerKind != other.OwnerKind || value.OwnerContext != other.OwnerContext || value.TargetPID != other.TargetPID ||
		value.TargetIdentity != other.TargetIdentity || value.PipeToken != other.PipeToken || value.JobID != other.JobID ||
		value.SupervisorPID != other.SupervisorPID || value.SupervisorIdentity != other.SupervisorIdentity ||
		!equalUint32(value.CreatorSessionID, other.CreatorSessionID) {
		return false
	}
	if value.StopReceipt == nil || other.StopReceipt == nil {
		return value.StopReceipt == nil && other.StopReceipt == nil
	}
	return *value.StopReceipt == *other.StopReceipt
}

// SameLaunch compares the immutable launch binding while ignoring the helper
// binding and terminal receipt, which may be added by later dedicated phases.
func (value SupervisorHandoff) SameLaunch(other SupervisorHandoff) bool {
	return value.Version == other.Version && value.LaunchToken == other.LaunchToken && value.Secret == other.Secret &&
		value.OwnerKind == other.OwnerKind && value.OwnerContext == other.OwnerContext && value.TargetPID == other.TargetPID && value.TargetIdentity == other.TargetIdentity && value.PipeToken == other.PipeToken && value.JobID == other.JobID &&
		equalUint32(value.CreatorSessionID, other.CreatorSessionID)
}

// ValidForHandoff reports whether a stop receipt exactly identifies a bound
// handoff. The receipt schema remains shared with the existing Supervisor
// authority for backwards compatibility.
func (receipt StopReceipt) ValidForHandoff(value SupervisorHandoff) bool {
	return receipt.Version == SupervisorVersion && receipt.Status == "stopped" && receipt.ActiveProcesses == 0 &&
		receipt.OwnerKind == value.OwnerKind && receipt.OwnerContext == value.OwnerContext &&
		receipt.TargetPID == value.TargetPID && receipt.TargetIdentity == value.TargetIdentity &&
		receipt.PipeToken == value.PipeToken && receipt.JobID == value.JobID &&
		receipt.SupervisorPID == value.SupervisorPID && receipt.SupervisorIdentity == value.SupervisorIdentity
}

// ReleaseProof returns an independent proof for the exact handoff identity.
// CreatorSessionID is copied so the proof cannot mutate the handoff through
// its optional session pointer.
func (value SupervisorHandoff) ReleaseProof() SupervisorHandoffReleaseProof {
	return SupervisorHandoffReleaseProof{
		Version:            SupervisorHandoffVersion,
		Status:             "released",
		LaunchToken:        value.LaunchToken,
		OwnerKind:          value.OwnerKind,
		OwnerContext:       value.OwnerContext,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		CreatorSessionID:   cloneUint32(value.CreatorSessionID),
	}
}

// Validate checks a release proof before it can clear a pending handoff.
func (proof SupervisorHandoffReleaseProof) Validate() error {
	if proof.Version != SupervisorHandoffVersion || proof.Status != "released" || !validHex(proof.LaunchToken, TokenBytes*2) || proof.TargetPID <= 0 || proof.SupervisorPID < 0 || !validOwner(proof.OwnerKind, proof.OwnerContext) {
		return errors.New("supervisor handoff release proof is invalid")
	}
	if !validHex(proof.PipeToken, TokenBytes*2) || !validHex(proof.JobID, TokenBytes*2) || !validBounded(proof.TargetIdentity, 4096) || (proof.SupervisorPID == 0) != (strings.TrimSpace(proof.SupervisorIdentity) == "") || !validOptionalBounded(proof.SupervisorIdentity, 4096) {
		return errors.New("supervisor handoff release proof is invalid")
	}
	if proof.OwnerKind == OwnerKindLinuxHelper && proof.CreatorSessionID != nil {
		return errors.New("Linux supervisor release proof cannot carry a Windows creator session")
	}
	return nil
}

// ValidFor reports whether proof identifies the exact handoff it claims to
// release.
func (proof SupervisorHandoffReleaseProof) ValidFor(value SupervisorHandoff) bool {
	return proof.Version == SupervisorHandoffVersion && proof.Status == "released" &&
		proof.LaunchToken == value.LaunchToken && proof.OwnerKind == value.OwnerKind && proof.OwnerContext == value.OwnerContext && proof.TargetPID == value.TargetPID && proof.TargetIdentity == value.TargetIdentity &&
		proof.PipeToken == value.PipeToken && proof.JobID == value.JobID &&
		proof.SupervisorPID == value.SupervisorPID && proof.SupervisorIdentity == value.SupervisorIdentity &&
		equalUint32(proof.CreatorSessionID, value.CreatorSessionID)
}

// Validate checks the exact pre-authority abort proof shape. Windows legacy
// proofs retain the CreatorSessionID requirement; Linux helper proofs bind
// owner kind/context instead and must not synthesize a Windows session.
func (proof SupervisorHandoffAbortProof) Validate() error {
	if proof.Version != SupervisorHandoffAbortProofVersion || proof.Disposition != SupervisorHandoffAbortDisposition || !validHex(proof.LaunchToken, TokenBytes*2) || !validHex(proof.JobID, TokenBytes*2) || proof.TargetPID <= 0 || proof.SupervisorPID < 0 || !validOwner(proof.OwnerKind, proof.OwnerContext) {
		return errors.New("supervisor handoff abort proof is invalid")
	}
	if !validBounded(proof.TargetIdentity, 4096) || (proof.SupervisorPID == 0) != (strings.TrimSpace(proof.SupervisorIdentity) == "") || !validOptionalBounded(proof.SupervisorIdentity, 4096) {
		return errors.New("supervisor handoff abort proof is invalid")
	}
	if proof.OwnerKind == OwnerKindLinuxHelper && proof.CreatorSessionID != nil {
		return errors.New("Linux supervisor abort proof cannot carry a Windows creator session")
	}
	if proof.OwnerKind != OwnerKindLinuxHelper && proof.CreatorSessionID == nil {
		return errors.New("supervisor handoff abort proof is invalid")
	}
	return nil
}

// ValidFor reports whether proof identifies the exact launch that was
// definitively aborted before authority commit.
func (proof SupervisorHandoffAbortProof) ValidFor(value SupervisorHandoff) bool {
	return proof.Version == SupervisorHandoffAbortProofVersion && proof.Disposition == SupervisorHandoffAbortDisposition &&
		proof.LaunchToken == value.LaunchToken && proof.JobID == value.JobID && proof.OwnerKind == value.OwnerKind && proof.OwnerContext == value.OwnerContext &&
		proof.TargetPID == value.TargetPID && proof.TargetIdentity == value.TargetIdentity &&
		proof.SupervisorPID == value.SupervisorPID && proof.SupervisorIdentity == value.SupervisorIdentity &&
		(proof.OwnerKind == OwnerKindLinuxHelper || (proof.CreatorSessionID != nil && value.CreatorSessionID != nil)) && equalUint32(proof.CreatorSessionID, value.CreatorSessionID)
}

func validOwner(kind, context string) bool {
	if kind == "" {
		return context == ""
	}
	if kind != OwnerKindLinuxHelper && kind != OwnerKindLegacyWindows {
		return false
	}
	return validBounded(context, 4096)
}

func cloneUint32(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalUint32(left, right *uint32) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validBounded(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= max
}

func validOptionalBounded(value string, max int) bool {
	return value == "" || validBounded(value, max)
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validOptionalHex(value string, length int) bool {
	return value == "" || validHex(value, length)
}
