package authority

import (
	"errors"
	"path"
	"strings"
	"time"
)

func equalLinuxCgroupPreGateReleaseProof(left, right *LinuxCgroupPreGateReleaseProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalLinuxCgroupReservationAbortProof(left, right *LinuxCgroupReservationAbortProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

// Path is a canonical absolute cgroup path, not a host filesystem path: the identity binds one boot and cgroup2 mount.
type LinuxCgroupParentIdentity struct {
	BootID  string `json:"boot_id"`
	MountID uint64 `json:"mount_id"`
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	Path    string `json:"path"`
}

// Identity match after the live pins are lost does not re-establish authority to reopen or act on the leaf.
type LinuxCgroupIdentity struct {
	Version    int                       `json:"version"`
	Parent     LinuxCgroupParentIdentity `json:"parent"`
	LeafName   string                    `json:"leaf_name"`
	LeafDevice uint64                    `json:"leaf_device"`
	LeafInode  uint64                    `json:"leaf_inode"`
}

// Exact PID plus a platform-supplied creation identity: opaque to this package, never PID-only evidence.
type LinuxCgroupProcessIdentity struct {
	PID      int    `json:"pid"`
	Identity string `json:"identity"`
}

// Structural caller attestation of an exact wait/reap: State validates shape and identity, it cannot observe kernel wait state.
type LinuxCgroupExitProof struct {
	Version int                        `json:"version"`
	Process LinuxCgroupProcessIdentity `json:"process"`
	Witness LinuxCgroupProcessIdentity `json:"witness"`
	Waited  bool                       `json:"waited"`
	Reaped  bool                       `json:"reaped"`
	// Present only when the anchor adopted this child after the helper was independently waited/reaped; a helper-owned wait leaves it nil.
	HelperExit *LinuxCgroupExitProof `json:"helper_exit,omitempty"`
}

// Authenticated root-launcher outcome for the narrow pre-gate stop path: the platform verifies the MAC
// against the per-run launch secret; State checks shape and identity only, not clone3 or kernel state.
type LinuxCgroupGateProductionResult struct {
	Version       int                        `json:"version"`
	LaunchToken   string                     `json:"launch_token"`
	Launcher      LinuxCgroupProcessIdentity `json:"launcher"`
	Outcome       string                     `json:"outcome"`
	Authenticator string                     `json:"authenticator"`
}

// Physical stop attestation for a reserved leaf where the launcher grant may have been armed
// but no gate was produced; separate from a gate-bound StopReceipt (no target PID or native gate).
type LinuxCgroupPreGateStopProof struct {
	Version                int                             `json:"version"`
	Kind                   string                          `json:"kind"`
	Issuer                 LinuxCgroupProcessIdentity      `json:"issuer"`
	Reaper                 LinuxCgroupProcessIdentity      `json:"reaper"`
	PinnedLeafFD           bool                            `json:"pinned_leaf_fd"`
	CgroupKillAcknowledged bool                            `json:"cgroup_kill_acknowledged"`
	CgroupPopulatedZero    bool                            `json:"cgroup_populated_zero"`
	NoMigrationObserved    bool                            `json:"no_migration_observed"`
	ProducerFenced         bool                            `json:"producer_fenced"`
	DescendantsReaped      bool                            `json:"descendants_reaped"`
	LauncherExit           LinuxCgroupExitProof            `json:"launcher_exit"`
	HelperExit             *LinuxCgroupExitProof           `json:"helper_exit,omitempty"`
	GateOutcome            LinuxCgroupGateProductionResult `json:"gate_outcome"`
}

type LinuxCgroupPreGateStopReceipt struct {
	Version        int                         `json:"version"`
	LaunchToken    string                      `json:"launch_token"`
	CgroupIdentity LinuxCgroupIdentity         `json:"cgroup_identity"`
	OwnerUID       uint32                      `json:"owner_uid"`
	LauncherUID    uint32                      `json:"launcher_uid"`
	Anchor         LinuxCgroupProcessIdentity  `json:"anchor"`
	Helper         LinuxCgroupProcessIdentity  `json:"helper"`
	Launcher       LinuxCgroupProcessIdentity  `json:"launcher"`
	Mirror         LinuxCgroupProcessIdentity  `json:"mirror"`
	Proof          LinuxCgroupPreGateStopProof `json:"proof"`
}

// The Linux platform seals this only after a pinned exact rmdir or a receipt-backed absence check under the same parent pin.
type LinuxCgroupPreGateReleaseProof struct {
	Version         int                           `json:"version"`
	Receipt         LinuxCgroupPreGateStopReceipt `json:"receipt"`
	LeafDisposition string                        `json:"leaf_disposition"`
}

// Platform attestations (not facts proved by this package) binding the trusted A gate at clone birth to the measured run leaf.
type LinuxCgroupGateIdentity struct {
	Process      LinuxCgroupProcessIdentity `json:"process"`
	HostUID      uint32                     `json:"host_uid"`
	ObservedAt   time.Time                  `json:"observed_at"`
	BirthKind    string                     `json:"birth_kind"`
	Cgroup       LinuxCgroupIdentity        `json:"cgroup"`
	BornInLeaf   bool                       `json:"born_in_leaf"`
	TrustedImage bool                       `json:"trusted_image"`
}

// Issuer names the helper or mirror that issues the receipt, not the kill writer;
// the caller validates the anchor's kill acknowledgement, empty-leaf evidence, wait/reap and producer fencing against pinned FDs.
type LinuxCgroupStopProof struct {
	Version                int                        `json:"version"`
	Kind                   string                     `json:"kind"`
	Issuer                 LinuxCgroupProcessIdentity `json:"issuer"`
	Reaper                 LinuxCgroupProcessIdentity `json:"reaper"`
	PinnedLeafFD           bool                       `json:"pinned_leaf_fd"`
	CgroupKillAcknowledged bool                       `json:"cgroup_kill_acknowledged"`
	CgroupPopulatedZero    bool                       `json:"cgroup_populated_zero"`
	NoMigrationObserved    bool                       `json:"no_migration_observed"`
	ProducerFenced         bool                       `json:"producer_fenced"`
	DescendantsReaped      bool                       `json:"descendants_reaped"`
	LauncherExit           LinuxCgroupExitProof       `json:"launcher_exit"`
	GateExit               LinuxCgroupExitProof       `json:"gate_exit"`
	HelperExit             *LinuxCgroupExitProof      `json:"helper_exit,omitempty"`
}

// For LeafNeverCreated with CreateStarted=false, State may issue the tombstone from its exclusive durable
// creator fence; it is not a physical stop receipt and does not claim to inspect the cgroup filesystem.
type LinuxCgroupReservationAbortProof struct {
	Version               int                         `json:"version"`
	LaunchToken           string                      `json:"launch_token"`
	Parent                LinuxCgroupParentIdentity   `json:"parent"`
	LeafName              string                      `json:"leaf_name"`
	CreateStarted         bool                        `json:"create_started"`
	LauncherGrantPossible bool                        `json:"launcher_grant_possible"`
	LeafIdentity          *LinuxCgroupIdentity        `json:"leaf_identity,omitempty"`
	LeafDisposition       string                      `json:"leaf_disposition"`
	OwnerUID              uint32                      `json:"owner_uid"`
	Anchor                LinuxCgroupProcessIdentity  `json:"anchor"`
	Mirror                LinuxCgroupProcessIdentity  `json:"mirror"`
	Helper                *LinuxCgroupProcessIdentity `json:"helper,omitempty"`
	Launcher              *LinuxCgroupProcessIdentity `json:"launcher,omitempty"`
	Witness               LinuxCgroupProcessIdentity  `json:"witness"`
	HelperExit            *LinuxCgroupExitProof       `json:"helper_exit,omitempty"`
	LauncherExit          *LinuxCgroupExitProof       `json:"launcher_exit,omitempty"`
	CreatorFenced         bool                        `json:"creator_fenced"`
	NoProducerCanAct      bool                        `json:"no_producer_can_act"`
	ParentUnchanged       bool                        `json:"parent_unchanged"`
	LeafEmpty             bool                        `json:"leaf_empty"`
	LeafAbsent            bool                        `json:"leaf_absent"`
	LeafNeverAppeared     bool                        `json:"leaf_never_appeared"`
	RmdirSucceeded        bool                        `json:"rmdir_succeeded"`
	RmdirOutcomeUnknown   bool                        `json:"rmdir_outcome_unknown"`
}

func (value LinuxCgroupParentIdentity) Validate() error {
	if !validBootID(value.BootID) || value.MountID == 0 || value.Device == 0 || value.Inode == 0 ||
		!validCanonicalCgroupPath(value.Path) {
		return errors.New("Linux cgroup parent identity is invalid")
	}
	return nil
}

func (value LinuxCgroupParentIdentity) Equal(other LinuxCgroupParentIdentity) bool {
	return value == other
}

func (value LinuxCgroupIdentity) Validate() error {
	if value.Version != LinuxCgroupIdentityVersion || value.Parent.Validate() != nil ||
		!validRandomLeafName(value.LeafName) || len(value.Parent.Path)+1+len(value.LeafName) > 4096 ||
		value.LeafDevice == 0 || value.LeafInode == 0 {
		return errors.New("Linux cgroup leaf identity is invalid")
	}
	return nil
}

func (value LinuxCgroupIdentity) Equal(other LinuxCgroupIdentity) bool {
	return value == other
}

func (value LinuxCgroupProcessIdentity) Validate() error {
	if value.PID <= 0 || !validBounded(value.Identity, 4096) {
		return errors.New("Linux process identity is invalid")
	}
	return nil
}

func (value LinuxCgroupProcessIdentity) Equal(other LinuxCgroupProcessIdentity) bool {
	return value == other
}

func (value LinuxCgroupExitProof) Validate() error {
	if value.Version != LinuxCgroupIdentityVersion || value.Process.Validate() != nil ||
		value.Witness.Validate() != nil || !value.Waited || !value.Reaped {
		return errors.New("Linux exact wait proof is invalid")
	}
	if value.HelperExit != nil && (value.HelperExit.HelperExit != nil || value.HelperExit.Validate() != nil) {
		return errors.New("Linux adopted-child helper exit proof is invalid")
	}
	return nil
}

func (value LinuxCgroupExitProof) ValidFor(process, witness LinuxCgroupProcessIdentity) bool {
	return value.Validate() == nil && value.HelperExit == nil && value.Process.Equal(process) && value.Witness.Equal(witness)
}

func (value LinuxCgroupExitProof) ValidForOwnedDescendant(process, helper, anchor LinuxCgroupProcessIdentity, helperExit *LinuxCgroupExitProof) bool {
	if value.Validate() != nil || !value.Process.Equal(process) {
		return false
	}
	if value.Witness.Equal(helper) {
		return value.HelperExit == nil
	}
	return value.Witness.Equal(anchor) && helperExit != nil && value.HelperExit != nil &&
		helperExit.ValidFor(helper, anchor) && value.HelperExit.Equal(*helperExit)
}

func (value LinuxCgroupExitProof) Equal(other LinuxCgroupExitProof) bool {
	return value.Version == other.Version && value.Process.Equal(other.Process) &&
		value.Witness.Equal(other.Witness) && value.Waited == other.Waited && value.Reaped == other.Reaped &&
		equalLinuxCgroupExitProof(value.HelperExit, other.HelperExit)
}

func (value LinuxCgroupExitProof) Clone() LinuxCgroupExitProof {
	cloned := value
	if value.HelperExit != nil {
		helperExit := value.HelperExit.Clone()
		cloned.HelperExit = &helperExit
	}
	return cloned
}

func (value LinuxCgroupGateProductionResult) ValidateFor(launchToken string, launcher LinuxCgroupProcessIdentity) error {
	if value.Version != LinuxCgroupIdentityVersion || value.LaunchToken != launchToken ||
		!validLowerHex(value.LaunchToken, TokenBytes*2) || !value.Launcher.Equal(launcher) ||
		value.Launcher.Validate() != nil || value.Outcome != LinuxCgroupGateOutcomeNeverCreated ||
		!validLowerHex(value.Authenticator, 64) {
		return errors.New("Linux cgroup no-gate launcher result is invalid")
	}
	return nil
}

func (value LinuxCgroupGateProductionResult) Equal(other LinuxCgroupGateProductionResult) bool {
	return value.Version == other.Version && value.LaunchToken == other.LaunchToken &&
		value.Launcher.Equal(other.Launcher) && value.Outcome == other.Outcome &&
		value.Authenticator == other.Authenticator
}

func (proof LinuxCgroupPreGateStopProof) ValidateFor(receipt LinuxCgroupPreGateStopReceipt) error {
	helperExit := proof.HelperExit
	if proof.Version != LinuxCgroupIdentityVersion ||
		(proof.Kind != LinuxCgroupStopProofHelper && proof.Kind != LinuxCgroupStopProofMirror) ||
		proof.Issuer.Validate() != nil || proof.Reaper.Validate() != nil ||
		!proof.PinnedLeafFD || !proof.CgroupKillAcknowledged || !proof.CgroupPopulatedZero ||
		!proof.NoMigrationObserved || !proof.ProducerFenced || !proof.DescendantsReaped ||
		proof.GateOutcome.ValidateFor(receipt.LaunchToken, receipt.Launcher) != nil ||
		!proof.LauncherExit.ValidForOwnedDescendant(receipt.Launcher, receipt.Helper, receipt.Anchor, helperExit) {
		return errors.New("Linux pre-gate cgroup stop proof is invalid")
	}
	switch proof.Kind {
	case LinuxCgroupStopProofHelper:
		if !proof.Issuer.Equal(receipt.Helper) || !proof.Reaper.Equal(receipt.Helper) ||
			proof.HelperExit != nil || !proof.LauncherExit.ValidForOwnedDescendant(receipt.Launcher, receipt.Helper, receipt.Anchor, nil) {
			return errors.New("Linux pre-gate helper stop proof identity is invalid")
		}
	case LinuxCgroupStopProofMirror:
		if !proof.Issuer.Equal(receipt.Mirror) || !proof.Reaper.Equal(receipt.Anchor) ||
			proof.HelperExit == nil || !proof.HelperExit.ValidFor(receipt.Helper, receipt.Anchor) {
			return errors.New("Linux pre-gate mirror stop proof identity is invalid")
		}
	}
	return nil
}

func (proof LinuxCgroupPreGateStopProof) Equal(other LinuxCgroupPreGateStopProof) bool {
	return proof.Version == other.Version && proof.Kind == other.Kind && proof.Issuer.Equal(other.Issuer) &&
		proof.Reaper.Equal(other.Reaper) && proof.PinnedLeafFD == other.PinnedLeafFD &&
		proof.CgroupKillAcknowledged == other.CgroupKillAcknowledged &&
		proof.CgroupPopulatedZero == other.CgroupPopulatedZero && proof.NoMigrationObserved == other.NoMigrationObserved &&
		proof.ProducerFenced == other.ProducerFenced && proof.DescendantsReaped == other.DescendantsReaped &&
		proof.LauncherExit.Equal(other.LauncherExit) && equalLinuxCgroupExitProof(proof.HelperExit, other.HelperExit) &&
		proof.GateOutcome.Equal(other.GateOutcome)
}

func (receipt LinuxCgroupPreGateStopReceipt) Validate() error {
	if receipt.Version != LinuxCgroupIdentityVersion || !validLowerHex(receipt.LaunchToken, TokenBytes*2) ||
		receipt.CgroupIdentity.Validate() != nil || receipt.OwnerUID == 0 || receipt.LauncherUID != 0 ||
		receipt.Anchor.Validate() != nil || receipt.Helper.Validate() != nil || receipt.Launcher.Validate() != nil ||
		receipt.Mirror.Validate() != nil || receipt.CgroupIdentity.LeafName == "" ||
		receipt.Proof.ValidateFor(receipt) != nil {
		return errors.New("Linux pre-gate cgroup stop receipt is invalid")
	}
	if receipt.Anchor.Equal(receipt.Helper) || receipt.Anchor.Equal(receipt.Launcher) ||
		receipt.Anchor.Equal(receipt.Mirror) || receipt.Helper.Equal(receipt.Launcher) ||
		receipt.Helper.Equal(receipt.Mirror) || receipt.Launcher.Equal(receipt.Mirror) {
		return errors.New("Linux pre-gate cgroup producer roles alias")
	}
	return nil
}

func (receipt LinuxCgroupPreGateStopReceipt) ValidFor(launchToken string, identity LinuxCgroupIdentity, ownerUID, launcherUID uint32,
	anchor, helper, launcher, mirror LinuxCgroupProcessIdentity, launcherExit LinuxCgroupExitProof, helperExit *LinuxCgroupExitProof) bool {
	return receipt.Validate() == nil && receipt.LaunchToken == launchToken && receipt.CgroupIdentity.Equal(identity) &&
		receipt.OwnerUID == ownerUID && receipt.LauncherUID == launcherUID && receipt.Anchor.Equal(anchor) &&
		receipt.Helper.Equal(helper) && receipt.Launcher.Equal(launcher) && receipt.Mirror.Equal(mirror) &&
		receipt.Proof.LauncherExit.Equal(launcherExit) && equalLinuxCgroupExitProof(receipt.Proof.HelperExit, helperExit)
}

func (receipt LinuxCgroupPreGateStopReceipt) Clone() LinuxCgroupPreGateStopReceipt {
	cloned := receipt
	cloned.Proof.LauncherExit.HelperExit = cloneLinuxCgroupExitProof(receipt.Proof.LauncherExit.HelperExit)
	cloned.Proof.HelperExit = cloneLinuxCgroupExitProof(receipt.Proof.HelperExit)
	return cloned
}

func (receipt LinuxCgroupPreGateStopReceipt) Equal(other LinuxCgroupPreGateStopReceipt) bool {
	return receipt.Version == other.Version && receipt.LaunchToken == other.LaunchToken &&
		receipt.CgroupIdentity.Equal(other.CgroupIdentity) && receipt.OwnerUID == other.OwnerUID &&
		receipt.LauncherUID == other.LauncherUID && receipt.Anchor.Equal(other.Anchor) &&
		receipt.Helper.Equal(other.Helper) && receipt.Launcher.Equal(other.Launcher) &&
		receipt.Mirror.Equal(other.Mirror) && receipt.Proof.Equal(other.Proof)
}

func (proof LinuxCgroupPreGateReleaseProof) Validate() error {
	if proof.Version != LinuxCgroupIdentityVersion || proof.Receipt.Validate() != nil ||
		(proof.LeafDisposition != LinuxCgroupLeafRemoved && proof.LeafDisposition != LinuxCgroupLeafAbsentAfterReceipt) {
		return errors.New("Linux pre-gate cgroup release proof is invalid")
	}
	return nil
}

func (proof LinuxCgroupPreGateReleaseProof) Clone() LinuxCgroupPreGateReleaseProof {
	cloned := proof
	cloned.Receipt = proof.Receipt.Clone()
	return cloned
}

func (proof LinuxCgroupPreGateReleaseProof) ValidFor(receipt LinuxCgroupPreGateStopReceipt) bool {
	return proof.Validate() == nil && proof.Receipt.Equal(receipt)
}

func (proof LinuxCgroupPreGateReleaseProof) Equal(other LinuxCgroupPreGateReleaseProof) bool {
	return proof.Version == other.Version && proof.Receipt.Equal(other.Receipt) && proof.LeafDisposition == other.LeafDisposition
}

func (value LinuxCgroupGateIdentity) Validate() error {
	if value.Process.Validate() != nil || value.HostUID == 0 || value.ObservedAt.IsZero() || value.BirthKind != LinuxCgroupTargetBirthTrustedAGate ||
		value.Cgroup.Validate() != nil || !value.BornInLeaf || !value.TrustedImage {
		return errors.New("Linux trusted gate identity is invalid")
	}
	return nil
}

func (value LinuxCgroupGateIdentity) Equal(other LinuxCgroupGateIdentity) bool {
	return value.Process.Equal(other.Process) && value.HostUID == other.HostUID && value.ObservedAt.Equal(other.ObservedAt) && value.BirthKind == other.BirthKind &&
		value.Cgroup.Equal(other.Cgroup) && value.BornInLeaf == other.BornInLeaf &&
		value.TrustedImage == other.TrustedImage
}

// Caller contract, not an independent kernel or cgroup-filesystem observation by State.
func (proof LinuxCgroupReservationAbortProof) Validate() error {
	if proof.Version != LinuxCgroupAbortProofVersion || !validLowerHex(proof.LaunchToken, TokenBytes*2) ||
		proof.Parent.Validate() != nil || !validRandomLeafName(proof.LeafName) || proof.OwnerUID == 0 ||
		proof.Anchor.Validate() != nil || proof.Mirror.Validate() != nil || proof.Witness.Validate() != nil || !proof.CreatorFenced ||
		!proof.NoProducerCanAct || !proof.ParentUnchanged || proof.LauncherGrantPossible {
		return errors.New("Linux cgroup reservation abort proof is invalid")
	}
	switch proof.LeafDisposition {
	case LinuxCgroupAbortLeafNeverCreated:
		if proof.LeafIdentity != nil || !proof.LeafAbsent || proof.LeafEmpty || proof.LeafNeverAppeared != proof.CreateStarted ||
			proof.RmdirSucceeded || proof.RmdirOutcomeUnknown || proof.HelperExit != nil || proof.Helper != nil ||
			proof.Launcher != nil || proof.LauncherExit != nil {
			return errors.New("Linux never-created leaf abort proof is invalid")
		}
	case LinuxCgroupAbortLeafRemovedEmpty, LinuxCgroupAbortLeafAbsentAfterRmdir:
		if !proof.CreateStarted || proof.LeafIdentity == nil || proof.LeafIdentity.Validate() != nil ||
			proof.LeafIdentity.LeafName != proof.LeafName || !proof.LeafIdentity.Parent.Equal(proof.Parent) ||
			!proof.LeafEmpty || !proof.LeafAbsent || proof.LeafNeverAppeared ||
			proof.Helper != nil && proof.Helper.Validate() != nil ||
			proof.Launcher != nil && proof.Launcher.Validate() != nil ||
			proof.HelperExit != nil && (proof.Helper == nil || !proof.HelperExit.ValidFor(*proof.Helper, proof.Anchor)) ||
			proof.LauncherExit != nil && (proof.Launcher == nil || proof.Helper == nil ||
				!proof.LauncherExit.ValidForOwnedDescendant(*proof.Launcher, *proof.Helper, proof.Anchor, proof.HelperExit)) {
			return errors.New("Linux removed-leaf abort proof is invalid")
		}
		if proof.LeafDisposition == LinuxCgroupAbortLeafRemovedEmpty && (!proof.RmdirSucceeded || proof.RmdirOutcomeUnknown) {
			return errors.New("Linux removed-leaf abort proof has no successful removal")
		}
		if proof.LeafDisposition == LinuxCgroupAbortLeafAbsentAfterRmdir && (proof.RmdirSucceeded || !proof.RmdirOutcomeUnknown) {
			return errors.New("Linux absent-after-rmdir abort proof has no unknown rmdir outcome")
		}
	default:
		return errors.New("Linux cgroup reservation abort disposition is invalid")
	}
	return nil
}

func (proof LinuxCgroupReservationAbortProof) ValidFor(launchToken string, parent LinuxCgroupParentIdentity, leafName string, createStarted bool, ownerUID uint32, anchor, mirror LinuxCgroupProcessIdentity, helper, launcher *LinuxCgroupProcessIdentity) bool {
	return proof.Validate() == nil && proof.LaunchToken == launchToken && proof.Parent.Equal(parent) &&
		proof.LeafName == leafName && proof.CreateStarted == createStarted && proof.OwnerUID == ownerUID && proof.Anchor.Equal(anchor) &&
		proof.Mirror.Equal(mirror) && equalLinuxCgroupProcessIdentity(proof.Helper, helper) &&
		equalLinuxCgroupProcessIdentity(proof.Launcher, launcher) &&
		proof.Witness.Equal(anchor)
}

func (proof LinuxCgroupReservationAbortProof) Clone() LinuxCgroupReservationAbortProof {
	cloned := proof
	cloned.LeafIdentity = cloneLinuxCgroupIdentity(proof.LeafIdentity)
	cloned.Helper = cloneLinuxCgroupProcessIdentity(proof.Helper)
	cloned.Launcher = cloneLinuxCgroupProcessIdentity(proof.Launcher)
	cloned.HelperExit = cloneLinuxCgroupExitProof(proof.HelperExit)
	cloned.LauncherExit = cloneLinuxCgroupExitProof(proof.LauncherExit)
	return cloned
}

func (proof LinuxCgroupReservationAbortProof) Equal(other LinuxCgroupReservationAbortProof) bool {
	return proof.Version == other.Version && proof.LaunchToken == other.LaunchToken && proof.Parent.Equal(other.Parent) &&
		proof.LeafName == other.LeafName && proof.CreateStarted == other.CreateStarted &&
		proof.LauncherGrantPossible == other.LauncherGrantPossible &&
		equalLinuxCgroupIdentity(proof.LeafIdentity, other.LeafIdentity) && proof.LeafDisposition == other.LeafDisposition &&
		proof.OwnerUID == other.OwnerUID &&
		proof.Anchor.Equal(other.Anchor) && proof.Mirror.Equal(other.Mirror) &&
		equalLinuxCgroupProcessIdentity(proof.Helper, other.Helper) && equalLinuxCgroupProcessIdentity(proof.Launcher, other.Launcher) &&
		proof.Witness.Equal(other.Witness) &&
		equalLinuxCgroupExitProof(proof.HelperExit, other.HelperExit) &&
		equalLinuxCgroupExitProof(proof.LauncherExit, other.LauncherExit) &&
		proof.CreatorFenced == other.CreatorFenced && proof.NoProducerCanAct == other.NoProducerCanAct &&
		proof.ParentUnchanged == other.ParentUnchanged && proof.LeafEmpty == other.LeafEmpty &&
		proof.LeafAbsent == other.LeafAbsent && proof.LeafNeverAppeared == other.LeafNeverAppeared &&
		proof.RmdirSucceeded == other.RmdirSucceeded && proof.RmdirOutcomeUnknown == other.RmdirOutcomeUnknown
}

func cloneLinuxCgroupIdentity(value *LinuxCgroupIdentity) *LinuxCgroupIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalLinuxCgroupIdentity(left, right *LinuxCgroupIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func cloneLinuxCgroupExitProof(value *LinuxCgroupExitProof) *LinuxCgroupExitProof {
	if value == nil {
		return nil
	}
	cloned := value.Clone()
	return &cloned
}

func equalLinuxCgroupExitProof(left, right *LinuxCgroupExitProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func cloneLinuxCgroupProcessIdentity(value *LinuxCgroupProcessIdentity) *LinuxCgroupProcessIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalLinuxCgroupProcessIdentity(left, right *LinuxCgroupProcessIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func (proof LinuxCgroupStopProof) ValidateFor(value Supervisor) error {
	if value.Version != SupervisorVersionV2 || proof.Version != LinuxCgroupIdentityVersion ||
		proof.Issuer.Validate() != nil || proof.Reaper.Validate() != nil ||
		!proof.PinnedLeafFD || !proof.CgroupKillAcknowledged || !proof.CgroupPopulatedZero ||
		!proof.NoMigrationObserved || !proof.ProducerFenced ||
		!proof.DescendantsReaped || value.LauncherExit == nil ||
		!proof.LauncherExit.Equal(*value.LauncherExit) {
		return errors.New("Linux cgroup stop proof is invalid")
	}
	helper := LinuxCgroupProcessIdentity{PID: value.SupervisorPID, Identity: value.SupervisorIdentity}
	anchor := LinuxCgroupProcessIdentity{PID: value.AnchorPID, Identity: value.AnchorIdentity}
	launcher := LinuxCgroupProcessIdentity{PID: value.LauncherPID, Identity: value.LauncherIdentity}
	target := LinuxCgroupProcessIdentity{PID: value.TargetPID, Identity: value.TargetIdentity}
	switch proof.Kind {
	case LinuxCgroupStopProofHelper:
		if !proof.Issuer.Equal(helper) || !proof.Reaper.Equal(helper) || proof.HelperExit != nil ||
			!proof.GateExit.ValidForOwnedDescendant(target, helper, anchor, nil) ||
			!proof.LauncherExit.ValidForOwnedDescendant(launcher, helper, anchor, nil) {
			return errors.New("Linux helper stop proof identity is invalid")
		}
	case LinuxCgroupStopProofMirror:
		if value.Mirror == nil || !proof.Issuer.Equal(*value.Mirror) || !proof.Reaper.Equal(anchor) ||
			proof.HelperExit == nil || !proof.HelperExit.ValidFor(helper, anchor) ||
			!proof.GateExit.ValidForOwnedDescendant(target, helper, anchor, proof.HelperExit) ||
			!proof.LauncherExit.ValidForOwnedDescendant(launcher, helper, anchor, proof.HelperExit) {
			return errors.New("Linux mirror stop proof identity is invalid")
		}
	default:
		return errors.New("Linux cgroup stop proof kind is invalid")
	}
	if proof.LauncherExit.Validate() != nil || !proof.LauncherExit.Process.Equal(launcher) {
		return errors.New("Linux launcher stop proof identity is invalid")
	}
	return nil
}

func (proof LinuxCgroupStopProof) Equal(other LinuxCgroupStopProof) bool {
	if proof.Version != other.Version || proof.Kind != other.Kind || !proof.Issuer.Equal(other.Issuer) ||
		!proof.Reaper.Equal(other.Reaper) || proof.PinnedLeafFD != other.PinnedLeafFD ||
		proof.CgroupKillAcknowledged != other.CgroupKillAcknowledged ||
		proof.CgroupPopulatedZero != other.CgroupPopulatedZero || proof.NoMigrationObserved != other.NoMigrationObserved ||
		proof.ProducerFenced != other.ProducerFenced ||
		proof.DescendantsReaped != other.DescendantsReaped || !proof.LauncherExit.Equal(other.LauncherExit) ||
		!proof.GateExit.Equal(other.GateExit) {
		return false
	}
	return equalLinuxCgroupExitProof(proof.HelperExit, other.HelperExit)
}

func cloneLinuxCgroupStopProof(value *LinuxCgroupStopProof) *LinuxCgroupStopProof {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.LauncherExit.HelperExit = cloneLinuxCgroupExitProof(value.LauncherExit.HelperExit)
	cloned.GateExit.HelperExit = cloneLinuxCgroupExitProof(value.GateExit.HelperExit)
	cloned.HelperExit = cloneLinuxCgroupExitProof(value.HelperExit)
	return &cloned
}

func equalLinuxCgroupStopProof(left, right *LinuxCgroupStopProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func validBootID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value != strings.ToLower(value) {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	return validHex(compact, 32)
}

func validCanonicalCgroupPath(value string) bool {
	return validBounded(value, 4096) && value != "/" && strings.IndexByte(value, 0) < 0 &&
		!strings.Contains(value, `\`) && path.IsAbs(value) && path.Clean(value) == value
}

func validRandomLeafName(value string) bool {
	return len(value) == TokenBytes*2 && value == strings.ToLower(value) && validHex(value, TokenBytes*2) &&
		!strings.ContainsAny(value, `/\.`)
}
