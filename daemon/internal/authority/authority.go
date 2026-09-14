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
	// SecretBytes is the entropy carried by a supervisor authority secret.
	SecretBytes = 32
	// TokenBytes is the entropy used for a pipe address and Job label.
	TokenBytes = 16
)

// Supervisor binds one launched target to the exact helper that owns its
// inherited containment handle. Secret is machine-local durable authority; it
// must never be placed in argv, environment, pipe names, or logs.
type Supervisor struct {
	Version            int          `json:"version"`
	Secret             string       `json:"secret"`
	TargetPID          int          `json:"target_pid"`
	TargetIdentity     string       `json:"target_identity"`
	PipeToken          string       `json:"pipe_token"`
	JobID              string       `json:"job_id"`
	SupervisorPID      int          `json:"supervisor_pid"`
	SupervisorIdentity string       `json:"supervisor_identity"`
	StopReceipt        *StopReceipt `json:"stop_receipt,omitempty"`
}

// StopReceipt is an exact, replayable terminal witness. It intentionally does
// not contain Secret: the authority binding is supplied by the enclosing
// Supervisor record and remains the only credential-bearing value.
type StopReceipt struct {
	Version            int    `json:"version"`
	Status             string `json:"status"`
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
	if !validHex(value.Secret, SecretBytes*2) || !validHex(value.PipeToken, TokenBytes*2) || !validHex(value.JobID, TokenBytes*2) {
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
		receipt.TargetPID == value.TargetPID && receipt.TargetIdentity == value.TargetIdentity &&
		receipt.PipeToken == value.PipeToken && receipt.JobID == value.JobID &&
		receipt.SupervisorPID == value.SupervisorPID && receipt.SupervisorIdentity == value.SupervisorIdentity
}

// Clone returns an independent copy suitable for handing across a callback
// boundary. Secret is copied as an ordinary string and is never formatted.
func (value Supervisor) Clone() Supervisor {
	cloned := value
	if value.StopReceipt != nil {
		receipt := *value.StopReceipt
		cloned.StopReceipt = &receipt
	}
	return cloned
}

// Equal compares all durable authority fields, including the secret and any
// terminal receipt. It is used by compare-and-set journal mutations.
func (value Supervisor) Equal(other Supervisor) bool {
	if value.Version != other.Version || value.Secret != other.Secret || value.TargetPID != other.TargetPID ||
		value.TargetIdentity != other.TargetIdentity || value.PipeToken != other.PipeToken || value.JobID != other.JobID ||
		value.SupervisorPID != other.SupervisorPID || value.SupervisorIdentity != other.SupervisorIdentity {
		return false
	}
	if value.StopReceipt == nil || other.StopReceipt == nil {
		return value.StopReceipt == nil && other.StopReceipt == nil
	}
	return *value.StopReceipt == *other.StopReceipt
}

func validBounded(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= max
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
