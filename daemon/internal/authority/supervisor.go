package authority

import (
	"encoding/hex"
	"errors"
	"strings"
)

const (
	SupervisorVersion = 1

	SupervisorHandoffVersion = 1

	SupervisorHandoffAbortProofVersion = 1

	SupervisorVersionV2 = 2

	SupervisorHandoffVersionV2 = 2

	LinuxCgroupIdentityVersion = 2
	// Durable disposition for a launch definitively aborted before authority commit.
	SupervisorHandoffAbortDisposition = "preauth_aborted"
	// The same-PID trusted A gate that exists before the one-shot release to native UID B.
	LinuxCgroupTargetBirthTrustedAGate = "trusted_a_gate"

	LinuxCgroupStopProofHelper = "helper"
	LinuxCgroupStopProofMirror = "mirror"
	// Emitted by the authenticated root launcher only when clone3 created no A gate.
	LinuxCgroupGateOutcomeNeverCreated = "never_created"
	// Distinguishes a verified removal from completing a prior, receipt-authorized removal.
	LinuxCgroupLeafRemoved            = "removed"
	LinuxCgroupLeafAbsentAfterReceipt = "absent_after_receipt"

	LinuxCgroupAbortProofVersion         = 2
	LinuxCgroupAbortLeafNeverCreated     = "never_created"
	LinuxCgroupAbortLeafRemovedEmpty     = "removed_empty"
	LinuxCgroupAbortLeafAbsentAfterRmdir = "absent_after_rmdir"

	SecretBytes = 32

	TokenBytes = 16

	OwnerKindLinuxHelper = "linux_helper"

	OwnerKindLegacyWindows             = "legacy_windows"
	SupervisorScanOwnerHelper          = "helper"
	SupervisorScanOwnerMirror          = "mirror"
	SupervisorScanPhaseBegin           = "begin"
	SupervisorScanPhaseInitialize      = "initialize"
	SupervisorScanPhaseClean           = "clean"
	SupervisorScanPhaseRetire          = "retire"
	SupervisorScanPhaseSeal            = "seal"
	SupervisorScanPhaseUnproven        = "unproven"
	SupervisorScanPhaseAssertClean     = "assert_clean"
	SupervisorScanPhaseAssertSealed    = "assert_sealed"
	SupervisorScanPhaseAssertAllSealed = "assert_all_sealed"

	LinuxCloneVersion = 2

	LinuxCloneNonceBytes = 32

	LinuxCloneMaxSeedBytes int64 = 512 << 20

	LinuxCloneTargetDispositionRemoved       = "removed"
	LinuxCloneTargetDispositionAlreadyAbsent = "already_absent"
)

func equalSupervisorHandoffReleaseProof(left, right *SupervisorHandoffReleaseProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

// SupervisorScanUpdate is a request-only in-process envelope carried by the handoff callback:
// never serialized and never authority by itself; the Store binds it to the run key and exact helper.
type SupervisorScanUpdate struct {
	SupervisorPID      int
	SupervisorIdentity string
	MirrorIdentity     string
	Owner              string
	Sequence           uint64
	Phase              string
	FailureKind        string
}

// Secret is machine-local durable authority: never place it in argv, environment, pipe names, or logs.
type Supervisor struct {
	Version int    `json:"version"`
	Secret  string `json:"secret"`

	LaunchToken        string `json:"launch_token,omitempty"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	OwnerUID           uint32 `json:"owner_uid,omitempty"`
	LauncherUID        uint32 `json:"launcher_uid,omitempty"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`

	TargetBirthKind  string                      `json:"target_birth_kind,omitempty"`
	CgroupIdentity   *LinuxCgroupIdentity        `json:"cgroup_identity,omitempty"`
	AnchorPID        int                         `json:"anchor_pid,omitempty"`
	AnchorIdentity   string                      `json:"anchor_identity,omitempty"`
	LauncherPID      int                         `json:"launcher_pid,omitempty"`
	LauncherIdentity string                      `json:"launcher_identity,omitempty"`
	LauncherExit     *LinuxCgroupExitProof       `json:"launcher_exit,omitempty"`
	Mirror           *LinuxCgroupProcessIdentity `json:"mirror,omitempty"`
	// A present pointer distinguishes Windows session 0 from an unavailable legacy field.
	CreatorSessionID *uint32      `json:"creator_session_id,omitempty"`
	StopReceipt      *StopReceipt `json:"stop_receipt,omitempty"`
}

// Durable launch record written before an independent containment helper can outlive the daemon:
// SupervisorPID/Identity are an optional pair until observed, and StopReceipt only follows a positive stop witness.
type SupervisorHandoff struct {
	Version            int    `json:"version"`
	LaunchToken        string `json:"launch_token"`
	Secret             string `json:"secret"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	OwnerUID           uint32 `json:"owner_uid,omitempty"`
	LauncherUID        uint32 `json:"launcher_uid,omitempty"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	SupervisorPID      int    `json:"supervisor_pid,omitempty"`
	SupervisorIdentity string `json:"supervisor_identity,omitempty"`

	TargetBirthKind  string                      `json:"target_birth_kind,omitempty"`
	CgroupIdentity   *LinuxCgroupIdentity        `json:"cgroup_identity,omitempty"`
	AnchorPID        int                         `json:"anchor_pid,omitempty"`
	AnchorIdentity   string                      `json:"anchor_identity,omitempty"`
	LauncherPID      int                         `json:"launcher_pid,omitempty"`
	LauncherIdentity string                      `json:"launcher_identity,omitempty"`
	LauncherExit     *LinuxCgroupExitProof       `json:"launcher_exit,omitempty"`
	Mirror           *LinuxCgroupProcessIdentity `json:"mirror,omitempty"`
	// A present pointer distinguishes Windows session 0 from a missing legacy field.
	CreatorSessionID *uint32      `json:"creator_session_id,omitempty"`
	StopReceipt      *StopReceipt `json:"stop_receipt,omitempty"`
	// Request-only envelope for the Store callback: excluded from journal encoding and handoff equality.
	ScanUpdate *SupervisorScanUpdate `json:"-"`
}

// Non-durable platform proof carrying the exact handoff identity: a caller cannot clear by PID alone.
type SupervisorHandoffReleaseProof struct {
	Version            int                         `json:"version"`
	Status             string                      `json:"status"`
	LaunchToken        string                      `json:"launch_token"`
	OwnerKind          string                      `json:"owner_kind,omitempty"`
	OwnerContext       string                      `json:"owner_context,omitempty"`
	TargetPID          int                         `json:"target_pid"`
	TargetIdentity     string                      `json:"target_identity"`
	OwnerUID           uint32                      `json:"owner_uid,omitempty"`
	LauncherUID        uint32                      `json:"launcher_uid,omitempty"`
	PipeToken          string                      `json:"pipe_token"`
	JobID              string                      `json:"job_id"`
	SupervisorPID      int                         `json:"supervisor_pid"`
	SupervisorIdentity string                      `json:"supervisor_identity"`
	TargetBirthKind    string                      `json:"target_birth_kind,omitempty"`
	CgroupIdentity     *LinuxCgroupIdentity        `json:"cgroup_identity,omitempty"`
	AnchorPID          int                         `json:"anchor_pid,omitempty"`
	AnchorIdentity     string                      `json:"anchor_identity,omitempty"`
	LauncherPID        int                         `json:"launcher_pid,omitempty"`
	LauncherIdentity   string                      `json:"launcher_identity,omitempty"`
	LauncherExit       *LinuxCgroupExitProof       `json:"launcher_exit,omitempty"`
	Mirror             *LinuxCgroupProcessIdentity `json:"mirror,omitempty"`
	LeafDisposition    string                      `json:"leaf_disposition,omitempty"`
	StopReceipt        *StopReceipt                `json:"stop_receipt,omitempty"`
	CreatorSessionID   *uint32                     `json:"creator_session_id,omitempty"`
}

// Non-durable abort proof: it carries no StopReceipt and can never serve as evidence that a target or Job was stopped.
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

// Exact, replayable terminal witness; it intentionally carries no Secret, which only the
// enclosing Supervisor record holds.
type StopReceipt struct {
	Version            int                   `json:"version"`
	Status             string                `json:"status"`
	OwnerKind          string                `json:"owner_kind,omitempty"`
	OwnerContext       string                `json:"owner_context,omitempty"`
	TargetPID          int                   `json:"target_pid"`
	TargetIdentity     string                `json:"target_identity"`
	OwnerUID           uint32                `json:"owner_uid,omitempty"`
	LauncherUID        uint32                `json:"launcher_uid,omitempty"`
	PipeToken          string                `json:"pipe_token"`
	JobID              string                `json:"job_id"`
	SupervisorPID      int                   `json:"supervisor_pid"`
	SupervisorIdentity string                `json:"supervisor_identity"`
	ActiveProcesses    uint32                `json:"active_processes"`
	TargetBirthKind    string                `json:"target_birth_kind,omitempty"`
	CgroupIdentity     *LinuxCgroupIdentity  `json:"cgroup_identity,omitempty"`
	LinuxProof         *LinuxCgroupStopProof `json:"linux_proof,omitempty"`
}

type supervisorCore struct {
	Version            int
	Secret             string
	LaunchToken        string
	OwnerKind          string
	OwnerContext       string
	TargetPID          int
	TargetIdentity     string
	OwnerUID           uint32
	LauncherUID        uint32
	PipeToken          string
	JobID              string
	SupervisorPID      int
	SupervisorIdentity string
	TargetBirthKind    string
	CgroupIdentity     *LinuxCgroupIdentity
	AnchorPID          int
	AnchorIdentity     string
	LauncherPID        int
	LauncherIdentity   string
	LauncherExit       *LinuxCgroupExitProof
	Mirror             *LinuxCgroupProcessIdentity
	CreatorSessionID   *uint32
	StopReceipt        *StopReceipt
}

func (value Supervisor) core() supervisorCore {
	return supervisorCore(value)
}

func (value SupervisorHandoff) core() supervisorCore {
	return supervisorCore{
		Version: value.Version, Secret: value.Secret, LaunchToken: value.LaunchToken, OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext,
		TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, OwnerUID: value.OwnerUID, LauncherUID: value.LauncherUID,
		PipeToken: value.PipeToken, JobID: value.JobID, SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity,
		TargetBirthKind: value.TargetBirthKind, CgroupIdentity: value.CgroupIdentity, AnchorPID: value.AnchorPID, AnchorIdentity: value.AnchorIdentity,
		LauncherPID: value.LauncherPID, LauncherIdentity: value.LauncherIdentity, LauncherExit: value.LauncherExit, Mirror: value.Mirror,
		CreatorSessionID: value.CreatorSessionID, StopReceipt: value.StopReceipt,
	}
}

func (proof SupervisorHandoffReleaseProof) core() supervisorCore {
	return supervisorCore{
		Version: proof.Version, LaunchToken: proof.LaunchToken, OwnerKind: proof.OwnerKind, OwnerContext: proof.OwnerContext,
		TargetPID: proof.TargetPID, TargetIdentity: proof.TargetIdentity, OwnerUID: proof.OwnerUID, LauncherUID: proof.LauncherUID,
		PipeToken: proof.PipeToken, JobID: proof.JobID, SupervisorPID: proof.SupervisorPID, SupervisorIdentity: proof.SupervisorIdentity,
		TargetBirthKind: proof.TargetBirthKind, CgroupIdentity: proof.CgroupIdentity, AnchorPID: proof.AnchorPID, AnchorIdentity: proof.AnchorIdentity,
		LauncherPID: proof.LauncherPID, LauncherIdentity: proof.LauncherIdentity, LauncherExit: proof.LauncherExit, Mirror: proof.Mirror,
		CreatorSessionID: proof.CreatorSessionID, StopReceipt: proof.StopReceipt,
	}
}

func supervisorFromCore(core supervisorCore) Supervisor {
	return Supervisor{
		Version: core.Version, Secret: core.Secret, LaunchToken: core.LaunchToken, OwnerKind: core.OwnerKind, OwnerContext: core.OwnerContext,
		TargetPID: core.TargetPID, TargetIdentity: core.TargetIdentity, OwnerUID: core.OwnerUID, LauncherUID: core.LauncherUID,
		PipeToken: core.PipeToken, JobID: core.JobID, SupervisorPID: core.SupervisorPID, SupervisorIdentity: core.SupervisorIdentity,
		TargetBirthKind: core.TargetBirthKind, CgroupIdentity: cloneLinuxCgroupIdentity(core.CgroupIdentity), AnchorPID: core.AnchorPID,
		AnchorIdentity: core.AnchorIdentity, LauncherPID: core.LauncherPID, LauncherIdentity: core.LauncherIdentity,
		LauncherExit: cloneLinuxCgroupExitProof(core.LauncherExit), Mirror: cloneLinuxCgroupProcessIdentity(core.Mirror),
		CreatorSessionID: cloneUint32(core.CreatorSessionID), StopReceipt: cloneStopReceipt(core.StopReceipt),
	}
}

func cloneCorePointers(core supervisorCore) (*LinuxCgroupIdentity, *LinuxCgroupExitProof, *LinuxCgroupProcessIdentity, *uint32, *StopReceipt) {
	return cloneLinuxCgroupIdentity(core.CgroupIdentity), cloneLinuxCgroupExitProof(core.LauncherExit),
		cloneLinuxCgroupProcessIdentity(core.Mirror), cloneUint32(core.CreatorSessionID), cloneStopReceipt(core.StopReceipt)
}

func (core supervisorCore) equal(other supervisorCore) bool {
	if core.Version != other.Version || core.Secret != other.Secret || core.LaunchToken != other.LaunchToken || core.OwnerKind != other.OwnerKind || core.OwnerContext != other.OwnerContext || core.TargetPID != other.TargetPID ||
		core.TargetIdentity != other.TargetIdentity || core.PipeToken != other.PipeToken || core.JobID != other.JobID ||
		core.SupervisorPID != other.SupervisorPID || core.SupervisorIdentity != other.SupervisorIdentity ||
		core.TargetBirthKind != other.TargetBirthKind || core.OwnerUID != other.OwnerUID || core.LauncherUID != other.LauncherUID ||
		core.AnchorPID != other.AnchorPID || core.AnchorIdentity != other.AnchorIdentity ||
		core.LauncherPID != other.LauncherPID || core.LauncherIdentity != other.LauncherIdentity ||
		!equalLinuxCgroupIdentity(core.CgroupIdentity, other.CgroupIdentity) ||
		!equalLinuxCgroupExitProof(core.LauncherExit, other.LauncherExit) || !equalLinuxCgroupProcessIdentity(core.Mirror, other.Mirror) ||
		!equalUint32(core.CreatorSessionID, other.CreatorSessionID) {
		return false
	}
	return equalStopReceiptPointer(core.StopReceipt, other.StopReceipt)
}

// Immutable launch binding: the helper binding, mirror identity and terminal receipt are
// cleared because later phases may add them.
func (core supervisorCore) sameLaunch(other supervisorCore) bool {
	core.SupervisorPID, core.SupervisorIdentity, core.Mirror, core.StopReceipt = 0, "", nil, nil
	other.SupervisorPID, other.SupervisorIdentity, other.Mirror, other.StopReceipt = 0, "", nil, nil
	return core.equal(other)
}

func (core supervisorCore) matchesStoppedReceipt(receipt StopReceipt) bool {
	return receipt.Version == SupervisorVersionV2 && receipt.Status == "stopped" && receipt.ActiveProcesses == 0 &&
		receipt.OwnerKind == core.OwnerKind && receipt.OwnerContext == core.OwnerContext &&
		receipt.TargetPID == core.TargetPID && receipt.TargetIdentity == core.TargetIdentity &&
		receipt.OwnerUID == core.OwnerUID && receipt.LauncherUID == core.LauncherUID &&
		receipt.PipeToken == core.PipeToken && receipt.JobID == core.JobID &&
		receipt.SupervisorPID == core.SupervisorPID && receipt.SupervisorIdentity == core.SupervisorIdentity &&
		receipt.TargetBirthKind == core.TargetBirthKind && equalLinuxCgroupIdentity(receipt.CgroupIdentity, core.CgroupIdentity)
}

// CreatorSessionID is deliberately outside the release identity: v2 validation rejects
// a non-nil session on both records.
func (core supervisorCore) matchesReleaseProof(proof SupervisorHandoffReleaseProof) bool {
	return proof.Version == SupervisorHandoffVersionV2 && proof.Status == "released" &&
		proof.LaunchToken == core.LaunchToken && proof.OwnerKind == core.OwnerKind && proof.OwnerContext == core.OwnerContext &&
		proof.TargetPID == core.TargetPID && proof.TargetIdentity == core.TargetIdentity &&
		proof.OwnerUID == core.OwnerUID && proof.LauncherUID == core.LauncherUID &&
		proof.PipeToken == core.PipeToken && proof.JobID == core.JobID &&
		proof.SupervisorPID == core.SupervisorPID && proof.SupervisorIdentity == core.SupervisorIdentity &&
		proof.TargetBirthKind == core.TargetBirthKind && equalLinuxCgroupIdentity(proof.CgroupIdentity, core.CgroupIdentity) &&
		proof.AnchorPID == core.AnchorPID && proof.AnchorIdentity == core.AnchorIdentity &&
		proof.LauncherPID == core.LauncherPID && proof.LauncherIdentity == core.LauncherIdentity &&
		equalLinuxCgroupExitProof(proof.LauncherExit, core.LauncherExit) &&
		equalLinuxCgroupProcessIdentity(proof.Mirror, core.Mirror) &&
		(proof.LeafDisposition == LinuxCgroupLeafRemoved || proof.LeafDisposition == LinuxCgroupLeafAbsentAfterReceipt) &&
		proof.StopReceipt != nil && core.StopReceipt != nil && proof.StopReceipt.Equal(*core.StopReceipt)
}

func (core supervisorCore) validateV2(requireHelper bool) error {
	if !validLowerHex(core.LaunchToken, TokenBytes*2) || core.OwnerKind != OwnerKindLinuxHelper || !validBounded(core.OwnerContext, 4096) ||
		core.TargetPID <= 0 || !validBounded(core.TargetIdentity, 4096) || core.TargetBirthKind != LinuxCgroupTargetBirthTrustedAGate ||
		core.OwnerUID == 0 || core.LauncherUID != 0 || core.CgroupIdentity == nil || core.CgroupIdentity.Validate() != nil ||
		core.AnchorPID <= 0 || !validBounded(core.AnchorIdentity, 4096) ||
		core.LauncherPID <= 0 || !validBounded(core.LauncherIdentity, 4096) || core.LauncherExit == nil || core.CreatorSessionID != nil {
		return errors.New("Linux cgroup supervisor identity is invalid")
	}
	launcher := LinuxCgroupProcessIdentity{PID: core.LauncherPID, Identity: core.LauncherIdentity}
	anchor := LinuxCgroupProcessIdentity{PID: core.AnchorPID, Identity: core.AnchorIdentity}
	target := LinuxCgroupProcessIdentity{PID: core.TargetPID, Identity: core.TargetIdentity}
	if core.LauncherExit.Validate() != nil || !core.LauncherExit.Process.Equal(launcher) {
		return errors.New("Linux cgroup launcher exit identity is invalid")
	}
	if core.LauncherExit.Witness.Equal(anchor) {
		if core.LauncherExit.HelperExit == nil || !core.LauncherExit.HelperExit.ValidFor(core.LauncherExit.HelperExit.Process, anchor) {
			return errors.New("Linux launcher anchor wait lacks exact helper-death adoption proof")
		}
	} else if core.LauncherExit.HelperExit != nil {
		return errors.New("Linux launcher helper wait unexpectedly carries helper-death proof")
	}
	if target.Equal(anchor) || target.Equal(launcher) || anchor.Equal(launcher) {
		return errors.New("Linux cgroup producer roles alias one process identity")
	}
	if core.SupervisorPID < 0 || (core.SupervisorPID == 0) != (strings.TrimSpace(core.SupervisorIdentity) == "") ||
		!validOptionalBounded(core.SupervisorIdentity, 4096) || (requireHelper && core.SupervisorPID == 0) {
		return errors.New("Linux cgroup helper identity is invalid")
	}
	if core.SupervisorPID == 0 {
		if core.Mirror != nil {
			return errors.New("Linux cgroup mirror cannot precede helper binding")
		}
		return nil
	}
	helper := LinuxCgroupProcessIdentity{PID: core.SupervisorPID, Identity: core.SupervisorIdentity}
	if helper.Validate() != nil || core.Mirror == nil || core.Mirror.Validate() != nil {
		return errors.New("Linux cgroup mirror identity is invalid")
	}
	if core.LauncherExit.Witness.Equal(anchor) && (core.LauncherExit.HelperExit == nil || !core.LauncherExit.HelperExit.ValidFor(helper, anchor)) {
		return errors.New("Linux launcher anchor witness does not identify the exact helper exit")
	}
	if !core.LauncherExit.ValidForOwnedDescendant(launcher, helper, anchor, core.LauncherExit.HelperExit) {
		return errors.New("Linux launcher exit witness does not match the exact helper or adopted child")
	}
	if target.Equal(helper) || target.Equal(*core.Mirror) || anchor.Equal(helper) || anchor.Equal(*core.Mirror) ||
		helper.Equal(launcher) || helper.Equal(*core.Mirror) || launcher.Equal(*core.Mirror) {
		return errors.New("Linux cgroup producer roles alias one process identity")
	}
	return nil
}

func (core supervisorCore) hasV2Fields() bool {
	return core.TargetBirthKind != "" || core.CgroupIdentity != nil || core.AnchorPID != 0 || core.AnchorIdentity != "" ||
		core.LauncherPID != 0 || core.LauncherIdentity != "" || core.LauncherExit != nil || core.Mirror != nil
}

func (proof SupervisorHandoffReleaseProof) Clone() SupervisorHandoffReleaseProof {
	cloned := proof
	cloned.CgroupIdentity, cloned.LauncherExit, cloned.Mirror, cloned.CreatorSessionID, cloned.StopReceipt = cloneCorePointers(proof.core())
	return cloned
}

func (proof SupervisorHandoffReleaseProof) Equal(other SupervisorHandoffReleaseProof) bool {
	return proof.Status == other.Status && proof.LeafDisposition == other.LeafDisposition && proof.core().equal(other.core())
}

func cloneStopReceipt(value *StopReceipt) *StopReceipt {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.CgroupIdentity = cloneLinuxCgroupIdentity(value.CgroupIdentity)
	cloned.LinuxProof = cloneLinuxCgroupStopProof(value.LinuxProof)
	return &cloned
}

func equalStopReceiptPointer(left, right *StopReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return equalStopReceipt(*left, *right)
}

func (value Supervisor) Validate() error {
	if value.Version == SupervisorVersionV2 {
		if err := value.core().validateV2(true); err != nil {
			return errors.New("supervisor authority is invalid")
		}
		if !validLowerHex(value.Secret, SecretBytes*2) || !validLowerHex(value.PipeToken, TokenBytes*2) || !validLowerHex(value.JobID, TokenBytes*2) {
			return errors.New("supervisor authority token is invalid")
		}
		if value.StopReceipt != nil && !value.StopReceipt.ValidFor(value) {
			return errors.New("supervisor authority stop receipt is invalid")
		}
		return nil
	}
	if value.Version != SupervisorVersion || value.TargetPID <= 0 || value.SupervisorPID <= 0 || value.OwnerUID != 0 || value.LauncherUID != 0 || value.core().hasV2Fields() {
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

func (receipt StopReceipt) ValidFor(value Supervisor) bool {
	if value.Version == SupervisorVersionV2 {
		return value.core().matchesStoppedReceipt(receipt) &&
			receipt.LinuxProof != nil && receipt.LinuxProof.ValidateFor(value) == nil
	}
	return receipt.Version == SupervisorVersion && receipt.Status == "stopped" && receipt.ActiveProcesses == 0 &&
		receipt.OwnerKind == value.OwnerKind && receipt.OwnerContext == value.OwnerContext &&
		receipt.TargetPID == value.TargetPID && receipt.TargetIdentity == value.TargetIdentity &&
		receipt.OwnerUID == 0 && receipt.LauncherUID == 0 &&
		receipt.PipeToken == value.PipeToken && receipt.JobID == value.JobID &&
		receipt.SupervisorPID == value.SupervisorPID && receipt.SupervisorIdentity == value.SupervisorIdentity &&
		receipt.TargetBirthKind == "" && receipt.CgroupIdentity == nil && receipt.LinuxProof == nil
}

func (receipt StopReceipt) Equal(other StopReceipt) bool {
	return equalStopReceipt(receipt, other)
}

// Secret is copied as an ordinary string and is never formatted.
func (value Supervisor) Clone() Supervisor {
	cloned := value
	cloned.CgroupIdentity, cloned.LauncherExit, cloned.Mirror, cloned.CreatorSessionID, cloned.StopReceipt = cloneCorePointers(value.core())
	return cloned
}

func (value Supervisor) Equal(other Supervisor) bool {
	return value.core().equal(other.core())
}

// The helper identity is either completely absent or completely present; partial state
// cannot identify the retained helper after a restart.
func (value SupervisorHandoff) Validate() error {
	if value.Version == SupervisorHandoffVersionV2 {
		if value.ScanUpdate != nil || value.core().validateV2(false) != nil {
			return errors.New("supervisor handoff is invalid")
		}
		if !validLowerHex(value.Secret, SecretBytes*2) || !validLowerHex(value.PipeToken, TokenBytes*2) || !validLowerHex(value.JobID, TokenBytes*2) {
			return errors.New("supervisor handoff token is invalid")
		}
		if value.StopReceipt != nil && (value.SupervisorPID <= 0 || value.Mirror == nil || !value.StopReceipt.ValidForHandoff(value)) {
			return errors.New("supervisor handoff stop receipt is invalid")
		}
		return nil
	}
	if value.Version != SupervisorHandoffVersion || !validHex(value.LaunchToken, TokenBytes*2) || value.TargetPID <= 0 {
		return errors.New("supervisor handoff is invalid")
	}
	if value.OwnerUID != 0 || value.LauncherUID != 0 || value.core().hasV2Fields() {
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
	if update := value.ScanUpdate; update != nil {
		if value.OwnerKind != OwnerKindLinuxHelper || value.SupervisorPID != 0 || value.SupervisorIdentity != "" ||
			len(update.MirrorIdentity) != TokenBytes*2 || !validHex(update.MirrorIdentity, TokenBytes*2) || update.Sequence == 0 {
			return errors.New("supervisor scan update identity is invalid")
		}
		if update.Phase == SupervisorScanPhaseInitialize {
			if update.Owner != SupervisorScanOwnerMirror || update.Sequence != 1 || update.SupervisorPID != 0 || update.SupervisorIdentity != "" || update.FailureKind != "" {
				return errors.New("supervisor mirror scan initialization is invalid")
			}
		} else {
			if update.SupervisorPID <= 0 || !validBounded(update.SupervisorIdentity, 4096) ||
				(update.Owner != SupervisorScanOwnerHelper && update.Owner != SupervisorScanOwnerMirror) {
				return errors.New("supervisor scan update owner identity is invalid")
			}
			switch update.Phase {
			case SupervisorScanPhaseBegin, SupervisorScanPhaseClean, SupervisorScanPhaseRetire,
				SupervisorScanPhaseSeal, SupervisorScanPhaseAssertClean, SupervisorScanPhaseAssertSealed,
				SupervisorScanPhaseAssertAllSealed:
				if update.FailureKind != "" {
					return errors.New("supervisor scan update has an unexpected failure kind")
				}
			case SupervisorScanPhaseUnproven:
				if !validBounded(update.FailureKind, 128) {
					return errors.New("supervisor scan update failure kind is invalid")
				}
			default:
				return errors.New("supervisor scan update phase is invalid")
			}
		}
	}
	return nil
}

func (value SupervisorHandoff) ToSupervisor() (Supervisor, error) {
	if err := value.Validate(); err != nil {
		return Supervisor{}, err
	}
	if value.ScanUpdate != nil {
		return Supervisor{}, errors.New("supervisor scan update is not a durable authority")
	}
	if value.SupervisorPID <= 0 || strings.TrimSpace(value.SupervisorIdentity) == "" {
		return Supervisor{}, errors.New("supervisor handoff helper identity is not bound")
	}
	result := supervisorFromCore(value.core())
	if err := result.Validate(); err != nil {
		return Supervisor{}, err
	}
	return result, nil
}

func (value SupervisorHandoff) Clone() SupervisorHandoff {
	cloned := value
	cloned.CgroupIdentity, cloned.LauncherExit, cloned.Mirror, cloned.CreatorSessionID, cloned.StopReceipt = cloneCorePointers(value.core())
	if value.ScanUpdate != nil {
		update := *value.ScanUpdate
		cloned.ScanUpdate = &update
	}
	return cloned
}

func (value SupervisorHandoff) Equal(other SupervisorHandoff) bool {
	return value.core().equal(other.core())
}

func (value SupervisorHandoff) SameLaunch(other SupervisorHandoff) bool {
	return value.core().sameLaunch(other.core())
}

func (receipt StopReceipt) ValidForHandoff(value SupervisorHandoff) bool {
	if value.Version == SupervisorHandoffVersionV2 {
		return value.SupervisorPID > 0 && value.Mirror != nil && value.core().matchesStoppedReceipt(receipt) &&
			receipt.LinuxProof != nil && receipt.LinuxProof.ValidateFor(supervisorFromHandoff(value)) == nil
	}
	return receipt.Version == SupervisorVersion && receipt.Status == "stopped" && receipt.ActiveProcesses == 0 &&
		receipt.OwnerKind == value.OwnerKind && receipt.OwnerContext == value.OwnerContext &&
		receipt.TargetPID == value.TargetPID && receipt.TargetIdentity == value.TargetIdentity &&
		receipt.OwnerUID == 0 && receipt.LauncherUID == 0 &&
		receipt.PipeToken == value.PipeToken && receipt.JobID == value.JobID &&
		receipt.SupervisorPID == value.SupervisorPID && receipt.SupervisorIdentity == value.SupervisorIdentity &&
		receipt.TargetBirthKind == "" && receipt.CgroupIdentity == nil && receipt.LinuxProof == nil
}

// Legacy v1 constructor: v2 Linux release evidence is minted only by the platform after pinned cgroup removal,
// so this returns a zero value for v2. CreatorSessionID is copied so a v1 proof cannot mutate the handoff.
func (value SupervisorHandoff) ReleaseProof() SupervisorHandoffReleaseProof {
	if value.Version != SupervisorHandoffVersion {
		return SupervisorHandoffReleaseProof{}
	}
	proof := SupervisorHandoffReleaseProof{
		Version:            SupervisorHandoffVersion,
		Status:             "released",
		LaunchToken:        value.LaunchToken,
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
		CreatorSessionID:   cloneUint32(value.CreatorSessionID),
	}
	return proof
}

func (proof SupervisorHandoffReleaseProof) Validate() error {
	if proof.Version == SupervisorHandoffVersionV2 {
		if proof.Status != "released" || !validLowerHex(proof.LaunchToken, TokenBytes*2) ||
			proof.core().validateV2(true) != nil ||
			(proof.LeafDisposition != LinuxCgroupLeafRemoved && proof.LeafDisposition != LinuxCgroupLeafAbsentAfterReceipt) {
			return errors.New("supervisor handoff release proof is invalid")
		}
		if !validLowerHex(proof.PipeToken, TokenBytes*2) || !validLowerHex(proof.JobID, TokenBytes*2) {
			return errors.New("supervisor handoff release proof is invalid")
		}
		if proof.StopReceipt == nil || !proof.StopReceipt.ValidFor(supervisorFromReleaseProof(proof)) {
			return errors.New("supervisor handoff release proof lacks its exact durable stop receipt")
		}
		return nil
	}
	if proof.Version != SupervisorHandoffVersion || proof.Status != "released" || !validHex(proof.LaunchToken, TokenBytes*2) || proof.TargetPID <= 0 || proof.SupervisorPID < 0 || !validOwner(proof.OwnerKind, proof.OwnerContext) {
		return errors.New("supervisor handoff release proof is invalid")
	}
	if proof.OwnerUID != 0 || proof.LauncherUID != 0 || proof.StopReceipt != nil || proof.core().hasV2Fields() || proof.LeafDisposition != "" {
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

func (proof SupervisorHandoffReleaseProof) ValidFor(value SupervisorHandoff) bool {
	if value.Version == SupervisorHandoffVersionV2 {
		return value.core().matchesReleaseProof(proof)
	}
	return proof.Version == SupervisorHandoffVersion && proof.Status == "released" &&
		proof.LaunchToken == value.LaunchToken && proof.OwnerKind == value.OwnerKind && proof.OwnerContext == value.OwnerContext && proof.TargetPID == value.TargetPID && proof.TargetIdentity == value.TargetIdentity &&
		proof.PipeToken == value.PipeToken && proof.JobID == value.JobID &&
		proof.SupervisorPID == value.SupervisorPID && proof.SupervisorIdentity == value.SupervisorIdentity &&
		equalUint32(proof.CreatorSessionID, value.CreatorSessionID) && proof.OwnerUID == 0 && proof.LauncherUID == 0 && proof.LeafDisposition == "" && proof.StopReceipt == nil &&
		!proof.core().hasV2Fields()
}

func (proof SupervisorHandoffReleaseProof) ValidForSupervisor(value Supervisor) bool {
	return value.Version == SupervisorVersionV2 && value.core().matchesReleaseProof(proof)
}

// Windows legacy proofs require CreatorSessionID; Linux helper proofs bind owner
// kind/context and must not synthesize a Windows session.
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

func (proof SupervisorHandoffAbortProof) ValidFor(value SupervisorHandoff) bool {
	return value.Version == SupervisorHandoffVersion && proof.Version == SupervisorHandoffAbortProofVersion && proof.Disposition == SupervisorHandoffAbortDisposition &&
		proof.LaunchToken == value.LaunchToken && proof.JobID == value.JobID && proof.OwnerKind == value.OwnerKind && proof.OwnerContext == value.OwnerContext &&
		proof.TargetPID == value.TargetPID && proof.TargetIdentity == value.TargetIdentity &&
		proof.SupervisorPID == value.SupervisorPID && proof.SupervisorIdentity == value.SupervisorIdentity &&
		(proof.OwnerKind == OwnerKindLinuxHelper || (proof.CreatorSessionID != nil && value.CreatorSessionID != nil)) && equalUint32(proof.CreatorSessionID, value.CreatorSessionID)
}

// Compare-only projection: it carries no terminal receipt, which is not part of the
// launch identity these checks resolve.
func supervisorFromHandoff(value SupervisorHandoff) Supervisor {
	core := value.core()
	core.StopReceipt = nil
	return supervisorFromCore(core)
}

// V2 comparison subject: forces the supervisor schema and omits the creator session, which
// v2 validation rejects and the release comparison never reads.
func supervisorFromReleaseProof(proof SupervisorHandoffReleaseProof) Supervisor {
	core := proof.core()
	core.Version = SupervisorVersionV2
	core.CreatorSessionID = nil
	return supervisorFromCore(core)
}

func equalStopReceipt(left, right StopReceipt) bool {
	return left.Version == right.Version && left.Status == right.Status && left.OwnerKind == right.OwnerKind &&
		left.OwnerContext == right.OwnerContext && left.TargetPID == right.TargetPID &&
		left.TargetIdentity == right.TargetIdentity && left.PipeToken == right.PipeToken && left.JobID == right.JobID &&
		left.SupervisorPID == right.SupervisorPID && left.SupervisorIdentity == right.SupervisorIdentity &&
		left.ActiveProcesses == right.ActiveProcesses && left.TargetBirthKind == right.TargetBirthKind &&
		left.OwnerUID == right.OwnerUID && left.LauncherUID == right.LauncherUID &&
		equalLinuxCgroupIdentity(left.CgroupIdentity, right.CgroupIdentity) &&
		equalLinuxCgroupStopProof(left.LinuxProof, right.LinuxProof)
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

func validLowerHex(value string, length int) bool {
	return value == strings.ToLower(value) && validHex(value, length)
}

func validOptionalHex(value string, length int) bool {
	return value == "" || validHex(value, length)
}
