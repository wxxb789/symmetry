// Package authority contains the small durable binding used to recover a
// machine-local process containment boundary. It is deliberately independent
// of platform and state packages so the journal can store the exact authority
// without making either package own the other's policy.
package authority

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	// SupervisorVersion is the first durable supervisor-authority schema.
	SupervisorVersion = 1
	// SupervisorHandoffVersion is the first durable pre-authority handoff schema.
	SupervisorHandoffVersion = 1
	// SupervisorHandoffAbortProofVersion is the first pre-authority abort-proof
	// schema. It is independent from both the handoff and stop-receipt schemas.
	SupervisorHandoffAbortProofVersion = 1
	// SupervisorVersionV2 is the exact-cgroup Linux supervisor schema. Version 1
	// remains the authority for existing Linux process-group and Windows owners.
	SupervisorVersionV2 = 2
	// SupervisorHandoffVersionV2 is the pre-authority schema for a new Linux
	// cgroup-v2 owner.
	SupervisorHandoffVersionV2 = 2
	// LinuxCgroupIdentityVersion identifies the immutable cgroup identity shape.
	LinuxCgroupIdentityVersion = 2
	// SupervisorHandoffAbortDisposition identifies a proof that the launch was
	// definitively aborted before supervisor authority was committed.
	SupervisorHandoffAbortDisposition = "preauth_aborted"
	// LinuxCgroupTargetBirthTrustedAGate identifies the same-PID trusted A gate
	// that exists before the one-shot release to native UID B.
	LinuxCgroupTargetBirthTrustedAGate = "trusted_a_gate"
	// LinuxCgroupStopProofHelper and LinuxCgroupStopProofMirror bound the two
	// supported v2 stop-proof issuers.
	LinuxCgroupStopProofHelper = "helper"
	LinuxCgroupStopProofMirror = "mirror"
	// LinuxCgroupGateOutcomeNeverCreated is emitted by the authenticated root
	// launcher only when clone3 did not create an A gate.
	LinuxCgroupGateOutcomeNeverCreated = "never_created"
	// LinuxCgroupLeafRemoved and LinuxCgroupLeafAbsentAfterReceipt distinguish a
	// verified removal from completing a prior, receipt-authorized removal.
	LinuxCgroupLeafRemoved            = "removed"
	LinuxCgroupLeafAbsentAfterReceipt = "absent_after_receipt"
	// LinuxCgroupAbortProofVersion identifies structural no-target abort evidence.
	LinuxCgroupAbortProofVersion         = 2
	LinuxCgroupAbortLeafNeverCreated     = "never_created"
	LinuxCgroupAbortLeafRemovedEmpty     = "removed_empty"
	LinuxCgroupAbortLeafAbsentAfterRmdir = "absent_after_rmdir"
	// SecretBytes is the entropy carried by a supervisor authority secret.
	SecretBytes = 32
	// TokenBytes is the entropy used for a pipe address and Job label.
	TokenBytes = 16
	// OwnerKindLinuxHelper identifies the platform-neutral Linux helper owner.
	OwnerKindLinuxHelper = "linux_helper"
	// OwnerKindLegacyWindows identifies the existing Windows owner/session
	// binding. Empty owner fields remain valid for legacy v1 records.
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
	// LinuxCloneVersion is the additive authority version for a B-owned Linux
	// standalone clone and its A-owned seed/promotion evidence.
	LinuxCloneVersion = 2
	// LinuxCloneNonceBytes is the entropy reserved for one run's clone intent.
	LinuxCloneNonceBytes = 32
	// LinuxCloneMaxSeedBytes bounds the exact self-contained seed bundle size.
	LinuxCloneMaxSeedBytes int64 = 512 << 20
)

// LinuxCloneIntent is the immutable, RunKey-bound authority for one B-owned
// standalone clone. SeedPath is an A-owned, B-readable bundle; Target is the
// disposable B-writable checkout path. A source ref, not B-owned Git metadata,
// pins BaseCommit before the bundle is published. ResourceID, BaseCommit and
// BaseTreeDigest together preserve the complete admitted Subject identity.
type LinuxCloneIntent struct {
	Version        int    `json:"version"`
	RunID          string `json:"run_id"`
	Generation     int64  `json:"generation"`
	BindingKey     string `json:"binding_key"`
	ResourceID     string `json:"resource_id"`
	BaseCommit     string `json:"base_commit"`
	BaseTreeDigest string `json:"base_tree_digest"`
	Nonce          string `json:"nonce"`
	SeedPath       string `json:"seed_path"`
	Target         string `json:"target"`
}

func (value LinuxCloneIntent) Validate() error {
	if value.Version != LinuxCloneVersion || !validLinuxCloneRunID(value.RunID) || value.Generation <= 0 ||
		!validLinuxCloneRunID(value.BindingKey) || !validLinuxCloneResourceID(value.ResourceID) ||
		!validLinuxCloneGitObjectID(value.BaseCommit) ||
		!validLinuxCloneDigest(value.BaseTreeDigest) || !validLowerHex(value.Nonce, LinuxCloneNonceBytes*2) ||
		!validLinuxClonePath(value.SeedPath) || !validLinuxClonePath(value.Target) ||
		linuxClonePathsOverlap(value.SeedPath, value.Target) {
		return errors.New("Linux clone intent is invalid")
	}
	return nil
}

func (value LinuxCloneIntent) Equal(other LinuxCloneIntent) bool {
	return value.Version == other.Version && value.RunID == other.RunID && value.Generation == other.Generation &&
		value.BindingKey == other.BindingKey && value.ResourceID == other.ResourceID && value.BaseCommit == other.BaseCommit &&
		value.BaseTreeDigest == other.BaseTreeDigest && value.Nonce == other.Nonce &&
		value.SeedPath == other.SeedPath && value.Target == other.Target
}

func (value LinuxCloneIntent) Clone() LinuxCloneIntent { return value }

// LinuxCloneSeedReceipt is A's measured receipt for the immutable bundle
// published at LinuxCloneIntent.SeedPath.
type LinuxCloneSeedReceipt struct {
	Version    int    `json:"version"`
	BaseCommit string `json:"base_commit"`
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
}

func (value LinuxCloneSeedReceipt) Validate() error {
	if value.Version != LinuxCloneVersion || !validLinuxCloneGitObjectID(value.BaseCommit) ||
		!validLinuxCloneDigest(value.Digest) || value.Size <= 0 || value.Size > LinuxCloneMaxSeedBytes || value.Inode == 0 {
		return errors.New("Linux clone seed receipt is invalid")
	}
	return nil
}

func (value LinuxCloneSeedReceipt) Equal(other LinuxCloneSeedReceipt) bool {
	return value.Version == other.Version && value.BaseCommit == other.BaseCommit && value.Digest == other.Digest &&
		value.Size == other.Size && value.Device == other.Device && value.Inode == other.Inode
}

func (value LinuxCloneSeedReceipt) Clone() LinuxCloneSeedReceipt { return value }

// LinuxCloneGrant is the exact reserved intent plus its durable A-measured
// seed receipt. It is the only clone authority delivered to the gate.
type LinuxCloneGrant struct {
	Intent LinuxCloneIntent      `json:"intent"`
	Seed   LinuxCloneSeedReceipt `json:"seed"`
}

func (value LinuxCloneGrant) Validate() error {
	if value.Intent.Validate() != nil || value.Seed.Validate() != nil || value.Seed.BaseCommit != value.Intent.BaseCommit {
		return errors.New("Linux clone grant is invalid")
	}
	return nil
}

func (value LinuxCloneGrant) Equal(other LinuxCloneGrant) bool {
	return value.Intent.Equal(other.Intent) && value.Seed.Equal(other.Seed)
}

func (value LinuxCloneGrant) Clone() LinuxCloneGrant {
	return LinuxCloneGrant{Intent: value.Intent.Clone(), Seed: value.Seed.Clone()}
}

// LinuxCloneCheckoutReceipt binds the B-created checkout to the reserved run,
// binding, resource, target, seed, base commit and observed target inode. It is
// not source-Git ownership evidence and cannot clear process containment.
type LinuxCloneCheckoutReceipt struct {
	Version      int    `json:"version"`
	RunID        string `json:"run_id"`
	Generation   int64  `json:"generation"`
	BindingKey   string `json:"binding_key"`
	ResourceID   string `json:"resource_id"`
	Target       string `json:"target"`
	Nonce        string `json:"nonce"`
	SeedDigest   string `json:"seed_digest"`
	BaseCommit   string `json:"base_commit"`
	TargetDevice uint64 `json:"target_device"`
	TargetInode  uint64 `json:"target_inode"`
}

func (value LinuxCloneCheckoutReceipt) Validate() error {
	if value.Version != LinuxCloneVersion || !validLinuxCloneRunID(value.RunID) || value.Generation <= 0 ||
		!validLinuxCloneRunID(value.BindingKey) || !validLinuxCloneResourceID(value.ResourceID) || !validLinuxClonePath(value.Target) ||
		!validLowerHex(value.Nonce, LinuxCloneNonceBytes*2) || !validLinuxCloneDigest(value.SeedDigest) ||
		!validLinuxCloneGitObjectID(value.BaseCommit) || value.TargetInode == 0 {
		return errors.New("Linux clone checkout receipt is invalid")
	}
	return nil
}

func (value LinuxCloneCheckoutReceipt) Equal(other LinuxCloneCheckoutReceipt) bool {
	return value.Version == other.Version && value.RunID == other.RunID && value.Generation == other.Generation &&
		value.BindingKey == other.BindingKey && value.ResourceID == other.ResourceID && value.Target == other.Target && value.Nonce == other.Nonce &&
		value.SeedDigest == other.SeedDigest && value.BaseCommit == other.BaseCommit &&
		value.TargetDevice == other.TargetDevice && value.TargetInode == other.TargetInode
}

func (value LinuxCloneCheckoutReceipt) Clone() LinuxCloneCheckoutReceipt { return value }

// LinuxClonePromotionReceipt records the A-validated commit after its object
// graph and full tree have been checked in A-owned storage and promoted under
// the deterministic source ref for this RunKey.
type LinuxClonePromotionReceipt struct {
	Version    int    `json:"version"`
	Commit     string `json:"commit"`
	TreeDigest string `json:"tree_digest"`
	Ref        string `json:"ref"`
}

func (value LinuxClonePromotionReceipt) Validate() error {
	if value.Version != LinuxCloneVersion || !validLinuxCloneGitObjectID(value.Commit) ||
		!validLinuxCloneDigest(value.TreeDigest) || !validLinuxClonePromotionRef(value.Ref) {
		return errors.New("Linux clone promotion receipt is invalid")
	}
	return nil
}

func (value LinuxClonePromotionReceipt) Equal(other LinuxClonePromotionReceipt) bool {
	return value.Version == other.Version && value.Commit == other.Commit &&
		value.TreeDigest == other.TreeDigest && value.Ref == other.Ref
}

func (value LinuxClonePromotionReceipt) Clone() LinuxClonePromotionReceipt { return value }

// LinuxCloneAbortProof certifies A-side cleanup of an intent that never
// reached B checkout: the exact target, seed path and run-keyed refs are
// independently verified absent. It is a cleanup tombstone, not physical-stop
// or checkout/promotion evidence.
type LinuxCloneAbortProof struct {
	Version             int                               `json:"version"`
	Intent              LinuxCloneIntent                  `json:"intent"`
	SeedReceipt         *LinuxCloneSeedReceipt            `json:"seed_receipt,omitempty"`
	PreGateReleaseProof *LinuxCgroupPreGateReleaseProof   `json:"pre_gate_release_proof,omitempty"`
	CgroupAbortProof    *LinuxCgroupReservationAbortProof `json:"cgroup_abort_proof,omitempty"`
	BaseRef             string                            `json:"base_ref"`
	PromotionRef        string                            `json:"promotion_ref"`
	TargetAbsent        bool                              `json:"target_absent"`
	SeedAbsent          bool                              `json:"seed_absent"`
	BaseRefAbsent       bool                              `json:"base_ref_absent"`
	PromotionRefAbsent  bool                              `json:"promotion_ref_absent"`
}

func (proof LinuxCloneAbortProof) Validate() error {
	baseRef, baseErr := LinuxCloneBaseRef(proof.Intent.RunID, proof.Intent.Generation)
	promotionRef, promotionErr := LinuxClonePromotionRef(proof.Intent.RunID, proof.Intent.Generation)
	if proof.Version != LinuxCloneVersion || proof.Intent.Validate() != nil || baseErr != nil || promotionErr != nil ||
		proof.BaseRef != baseRef || proof.PromotionRef != promotionRef || !proof.TargetAbsent || !proof.SeedAbsent ||
		!proof.BaseRefAbsent || !proof.PromotionRefAbsent ||
		proof.PreGateReleaseProof != nil && proof.CgroupAbortProof != nil ||
		proof.SeedReceipt != nil && (proof.SeedReceipt.Validate() != nil || proof.SeedReceipt.BaseCommit != proof.Intent.BaseCommit) ||
		proof.PreGateReleaseProof != nil && proof.PreGateReleaseProof.Validate() != nil ||
		proof.CgroupAbortProof != nil && proof.CgroupAbortProof.Validate() != nil {
		return errors.New("Linux clone abort proof is invalid")
	}
	return nil
}

func (proof LinuxCloneAbortProof) Clone() LinuxCloneAbortProof {
	cloned := proof
	if proof.SeedReceipt != nil {
		seed := proof.SeedReceipt.Clone()
		cloned.SeedReceipt = &seed
	}
	if proof.PreGateReleaseProof != nil {
		preGateProof := proof.PreGateReleaseProof.Clone()
		cloned.PreGateReleaseProof = &preGateProof
	}
	if proof.CgroupAbortProof != nil {
		cgroupAbortProof := proof.CgroupAbortProof.Clone()
		cloned.CgroupAbortProof = &cgroupAbortProof
	}
	return cloned
}

func (proof LinuxCloneAbortProof) Equal(other LinuxCloneAbortProof) bool {
	return proof.Version == other.Version && proof.Intent.Equal(other.Intent) &&
		equalLinuxCloneSeedReceipt(proof.SeedReceipt, other.SeedReceipt) &&
		equalLinuxCgroupPreGateReleaseProof(proof.PreGateReleaseProof, other.PreGateReleaseProof) &&
		equalLinuxCgroupReservationAbortProof(proof.CgroupAbortProof, other.CgroupAbortProof) &&
		proof.BaseRef == other.BaseRef && proof.PromotionRef == other.PromotionRef &&
		proof.TargetAbsent == other.TargetAbsent && proof.SeedAbsent == other.SeedAbsent &&
		proof.BaseRefAbsent == other.BaseRefAbsent && proof.PromotionRefAbsent == other.PromotionRefAbsent
}

func equalLinuxCloneSeedReceipt(left, right *LinuxCloneSeedReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

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

// LinuxCloneBaseRef returns the A-owned branch ref that pins the exact base
// commit for one run and can be named by git clone --branch. The hash avoids
// putting arbitrary run IDs into Git ref components; it is lowercase hex
// SHA-256 of runID + NUL + the base-10 generation.
func LinuxCloneBaseRef(runID string, generation int64) (string, error) {
	keyHash, err := linuxCloneRunKeyHash(runID, generation)
	if err != nil {
		return "", err
	}
	return "refs/heads/symmetry/clone-v2/base/" + keyHash, nil
}

// LinuxClonePromotionRef returns the A-owned branch ref for the verified
// candidate promoted from one run's B-owned clone.
func LinuxClonePromotionRef(runID string, generation int64) (string, error) {
	keyHash, err := linuxCloneRunKeyHash(runID, generation)
	if err != nil {
		return "", err
	}
	return "refs/heads/symmetry/clone-v2/promoted/" + keyHash, nil
}

func linuxCloneRunKeyHash(runID string, generation int64) (string, error) {
	if !validLinuxCloneRunID(runID) || generation <= 0 {
		return "", errors.New("Linux clone RunKey is invalid")
	}
	digest := sha256.Sum256([]byte(runID + "\x00" + strconv.FormatInt(generation, 10)))
	return hex.EncodeToString(digest[:]), nil
}

func validLinuxCloneRunID(value string) bool {
	if len(value) == 0 || len(value) > 128 || !isASCIIAlphaNumeric(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if !isASCIIAlphaNumeric(character) && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func validLinuxCloneGitObjectID(value string) bool {
	return (len(value) == 40 || len(value) == 64) && validLowerHex(value, len(value))
}

func validLinuxCloneResourceID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func validLinuxCloneDigest(value string) bool {
	return len(value) == len("sha256:")+64 && strings.HasPrefix(value, "sha256:") && validLowerHex(value[len("sha256:"):], 64)
}

func validLinuxClonePath(value string) bool {
	return len(value) > 1 && len(value) <= 32768 && path.IsAbs(value) && path.Clean(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func linuxClonePathsOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func validLinuxClonePromotionRef(value string) bool {
	const prefix = "refs/heads/symmetry/clone-v2/promoted/"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	return validLowerHex(strings.TrimPrefix(value, prefix), 64)
}

func isASCIIAlphaNumeric(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

// SupervisorScanUpdate is an in-process mutation envelope carried only by the
// existing handoff callback. It is never serialized into the handoff or sent
// as authority by itself; the Store binds it to the run key and exact helper.
type SupervisorScanUpdate struct {
	SupervisorPID      int
	SupervisorIdentity string
	MirrorIdentity     string
	Owner              string
	Sequence           uint64
	Phase              string
	FailureKind        string
}

// LinuxCgroupParentIdentity binds the delegated manager parent on one boot and
// cgroup2 mount. Path is a canonical absolute cgroup path, not a host
// filesystem path supplied for later reopening.
type LinuxCgroupParentIdentity struct {
	BootID  string `json:"boot_id"`
	MountID uint64 `json:"mount_id"`
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	Path    string `json:"path"`
}

// LinuxCgroupIdentity names one measured run leaf under the exact exclusive
// delegated parent. Matching these fields after all live pins are lost does
// not re-establish authority to reopen or act on the leaf.
type LinuxCgroupIdentity struct {
	Version    int                       `json:"version"`
	Parent     LinuxCgroupParentIdentity `json:"parent"`
	LeafName   string                    `json:"leaf_name"`
	LeafDevice uint64                    `json:"leaf_device"`
	LeafInode  uint64                    `json:"leaf_inode"`
}

// LinuxCgroupProcessIdentity is an exact PID plus a platform-supplied stable
// creation identity. Identity is opaque to this package and must never be
// replaced by PID-only evidence.
type LinuxCgroupProcessIdentity struct {
	PID      int    `json:"pid"`
	Identity string `json:"identity"`
}

// LinuxCgroupExitProof is a structural caller attestation of an exact wait and
// reap. State validates identity and shape; it cannot observe kernel wait state.
type LinuxCgroupExitProof struct {
	Version int                        `json:"version"`
	Process LinuxCgroupProcessIdentity `json:"process"`
	Witness LinuxCgroupProcessIdentity `json:"witness"`
	Waited  bool                       `json:"waited"`
	Reaped  bool                       `json:"reaped"`
	// HelperExit is present only when the anchor adopted this child after the
	// exact helper was independently waited/reaped. A normal helper-owned wait
	// leaves it nil.
	HelperExit *LinuxCgroupExitProof `json:"helper_exit,omitempty"`
}

// LinuxCgroupGateProductionResult is the authenticated root-launcher outcome
// used only for the narrow pre-gate stop path. The platform verifies the MAC
// against the exact per-run launch secret; State checks the version, result,
// launcher identity, and structurally valid authenticator but does not claim
// to observe clone3 or kernel state itself.
type LinuxCgroupGateProductionResult struct {
	Version       int                        `json:"version"`
	LaunchToken   string                     `json:"launch_token"`
	Launcher      LinuxCgroupProcessIdentity `json:"launcher"`
	Outcome       string                     `json:"outcome"`
	Authenticator string                     `json:"authenticator"`
}

// LinuxCgroupPreGateStopProof is a physical stop attestation for a reserved
// leaf where launcher grant may have been armed but the authenticated root
// launcher proves that no gate was produced. It is separate from a gate-bound
// StopReceipt because no target PID or native gate existed.
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

// LinuxCgroupPreGateStopReceipt binds the no-gate physical stop to the exact
// reserved run leaf, producer set and authenticated root-launcher outcome.
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

// LinuxCgroupPreGateReleaseProof is the receipt-bound post-removal proof for
// a PID-zero reservation. The Linux platform seals this only after a pinned
// exact rmdir or receipt-backed absence check under the same parent pin.
type LinuxCgroupPreGateReleaseProof struct {
	Version         int                           `json:"version"`
	Receipt         LinuxCgroupPreGateStopReceipt `json:"receipt"`
	LeafDisposition string                        `json:"leaf_disposition"`
}

// LinuxCgroupGateIdentity binds the trusted A gate at clone birth to the
// measured run leaf. The booleans are platform attestations, not facts proved
// by this package.
type LinuxCgroupGateIdentity struct {
	Process      LinuxCgroupProcessIdentity `json:"process"`
	HostUID      uint32                     `json:"host_uid"`
	ObservedAt   time.Time                  `json:"observed_at"`
	BirthKind    string                     `json:"birth_kind"`
	Cgroup       LinuxCgroupIdentity        `json:"cgroup"`
	BornInLeaf   bool                       `json:"born_in_leaf"`
	TrustedImage bool                       `json:"trusted_image"`
}

// LinuxCgroupStopProof is the bounded physical-stop attestation carried by a
// v2 receipt. Its exact subject/producer identities are checked structurally;
// the helper or mirror caller remains responsible for verifying cgroup.kill,
// cgroup.events, wait/reap and producer-fence results against live pinned FDs.
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

// LinuxCgroupReservationAbortProof describes the exact no-target evidence
// needed to abandon a reserved leaf. Removed-leaf fields are platform
// attestations. For LeafNeverCreated with CreateStarted=false, State may issue
// the tombstone from its exclusive durable creator fence; it is not a physical
// stop receipt and does not claim to inspect the cgroup filesystem.
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
	OwnerUID           uint32 `json:"owner_uid,omitempty"`
	LauncherUID        uint32 `json:"launcher_uid,omitempty"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	// The following exact-subject fields are present only for v2 Linux cgroup
	// authority. Version 1 records retain their original encoded shape.
	TargetBirthKind  string                      `json:"target_birth_kind,omitempty"`
	CgroupIdentity   *LinuxCgroupIdentity        `json:"cgroup_identity,omitempty"`
	AnchorPID        int                         `json:"anchor_pid,omitempty"`
	AnchorIdentity   string                      `json:"anchor_identity,omitempty"`
	LauncherPID      int                         `json:"launcher_pid,omitempty"`
	LauncherIdentity string                      `json:"launcher_identity,omitempty"`
	LauncherExit     *LinuxCgroupExitProof       `json:"launcher_exit,omitempty"`
	Mirror           *LinuxCgroupProcessIdentity `json:"mirror,omitempty"`
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
	OwnerUID           uint32 `json:"owner_uid,omitempty"`
	LauncherUID        uint32 `json:"launcher_uid,omitempty"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	SupervisorPID      int    `json:"supervisor_pid,omitempty"`
	SupervisorIdentity string `json:"supervisor_identity,omitempty"`
	// Version 2 binds an A-born gate, its cgroup leaf and every producer. These
	// fields are omitted from all existing v1 records.
	TargetBirthKind  string                      `json:"target_birth_kind,omitempty"`
	CgroupIdentity   *LinuxCgroupIdentity        `json:"cgroup_identity,omitempty"`
	AnchorPID        int                         `json:"anchor_pid,omitempty"`
	AnchorIdentity   string                      `json:"anchor_identity,omitempty"`
	LauncherPID      int                         `json:"launcher_pid,omitempty"`
	LauncherIdentity string                      `json:"launcher_identity,omitempty"`
	LauncherExit     *LinuxCgroupExitProof       `json:"launcher_exit,omitempty"`
	Mirror           *LinuxCgroupProcessIdentity `json:"mirror,omitempty"`
	// CreatorSessionID is optional only for legacy handoffs. A present pointer
	// distinguishes Windows session 0 from a missing legacy field.
	CreatorSessionID *uint32      `json:"creator_session_id,omitempty"`
	StopReceipt      *StopReceipt `json:"stop_receipt,omitempty"`
	// ScanUpdate is a request-only envelope for the existing Store callback.
	// It is intentionally excluded from journal encoding and handoff equality.
	ScanUpdate *SupervisorScanUpdate `json:"-"`
}

// SupervisorHandoffReleaseProof is a narrow, non-durable proof supplied by
// the platform after it has released the helper. It deliberately carries the
// exact handoff identity instead of allowing a caller to clear by PID alone.
// The platform may adopt this shape later; this package does not implement
// platform release behavior.
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

func (proof SupervisorHandoffReleaseProof) Clone() SupervisorHandoffReleaseProof {
	cloned := proof
	cloned.CgroupIdentity = cloneLinuxCgroupIdentity(proof.CgroupIdentity)
	cloned.LauncherExit = cloneLinuxCgroupExitProof(proof.LauncherExit)
	cloned.Mirror = cloneLinuxCgroupProcessIdentity(proof.Mirror)
	cloned.StopReceipt = cloneStopReceipt(proof.StopReceipt)
	cloned.CreatorSessionID = cloneUint32(proof.CreatorSessionID)
	return cloned
}

func (proof SupervisorHandoffReleaseProof) Equal(other SupervisorHandoffReleaseProof) bool {
	return proof.Version == other.Version && proof.Status == other.Status && proof.LaunchToken == other.LaunchToken &&
		proof.OwnerKind == other.OwnerKind && proof.OwnerContext == other.OwnerContext && proof.TargetPID == other.TargetPID &&
		proof.TargetIdentity == other.TargetIdentity && proof.OwnerUID == other.OwnerUID && proof.LauncherUID == other.LauncherUID &&
		proof.PipeToken == other.PipeToken && proof.JobID == other.JobID && proof.SupervisorPID == other.SupervisorPID &&
		proof.SupervisorIdentity == other.SupervisorIdentity && proof.TargetBirthKind == other.TargetBirthKind &&
		equalLinuxCgroupIdentity(proof.CgroupIdentity, other.CgroupIdentity) && proof.AnchorPID == other.AnchorPID &&
		proof.AnchorIdentity == other.AnchorIdentity && proof.LauncherPID == other.LauncherPID &&
		proof.LauncherIdentity == other.LauncherIdentity && equalLinuxCgroupExitProof(proof.LauncherExit, other.LauncherExit) &&
		equalLinuxCgroupProcessIdentity(proof.Mirror, other.Mirror) && proof.LeafDisposition == other.LeafDisposition &&
		equalStopReceiptPointer(proof.StopReceipt, other.StopReceipt) &&
		equalUint32(proof.CreatorSessionID, other.CreatorSessionID)
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

// ValidForOwnedDescendant accepts the normal helper wait or an anchor wait
// only when the exact helper's preceding death/reap proof is also present.
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

// Validate checks a pre-effect abort attestation. It is a caller contract,
// not an independent kernel or cgroup-filesystem observation by State.
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
	cloned := *value
	cloned.HelperExit = cloneLinuxCgroupExitProof(value.HelperExit)
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

// Validate checks the durable authority shape without interpreting a target's
// platform-specific process identity.
func (value Supervisor) Validate() error {
	if value.Version == SupervisorVersionV2 {
		if err := validateV2SupervisorFields(value.LaunchToken, value.OwnerKind, value.OwnerContext, value.TargetPID,
			value.TargetIdentity, value.SupervisorPID, value.SupervisorIdentity, value.TargetBirthKind, value.OwnerUID, value.LauncherUID,
			value.CgroupIdentity, value.AnchorPID, value.AnchorIdentity, value.LauncherPID,
			value.LauncherIdentity, value.LauncherExit, value.Mirror, value.CreatorSessionID, true); err != nil {
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
	if value.Version != SupervisorVersion || value.TargetPID <= 0 || value.SupervisorPID <= 0 || value.OwnerUID != 0 || value.LauncherUID != 0 || hasV2SupervisorFields(
		value.TargetBirthKind, value.CgroupIdentity, value.AnchorPID, value.AnchorIdentity,
		value.LauncherPID, value.LauncherIdentity, value.LauncherExit, value.Mirror) {
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
	if value.Version == SupervisorVersionV2 {
		return receipt.Version == SupervisorVersionV2 && receipt.Status == "stopped" && receipt.ActiveProcesses == 0 &&
			receipt.OwnerKind == value.OwnerKind && receipt.OwnerContext == value.OwnerContext &&
			receipt.TargetPID == value.TargetPID && receipt.TargetIdentity == value.TargetIdentity &&
			receipt.OwnerUID == value.OwnerUID && receipt.LauncherUID == value.LauncherUID &&
			receipt.PipeToken == value.PipeToken && receipt.JobID == value.JobID &&
			receipt.SupervisorPID == value.SupervisorPID && receipt.SupervisorIdentity == value.SupervisorIdentity &&
			receipt.TargetBirthKind == value.TargetBirthKind && equalLinuxCgroupIdentity(receipt.CgroupIdentity, value.CgroupIdentity) &&
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

// Equal compares a receipt's complete nested cgroup evidence by value.
func (receipt StopReceipt) Equal(other StopReceipt) bool {
	return equalStopReceipt(receipt, other)
}

// Clone returns an independent copy suitable for handing across a callback
// boundary. Secret is copied as an ordinary string and is never formatted.
func (value Supervisor) Clone() Supervisor {
	cloned := value
	cloned.CreatorSessionID = cloneUint32(value.CreatorSessionID)
	cloned.CgroupIdentity = cloneLinuxCgroupIdentity(value.CgroupIdentity)
	cloned.LauncherExit = cloneLinuxCgroupExitProof(value.LauncherExit)
	cloned.Mirror = cloneLinuxCgroupProcessIdentity(value.Mirror)
	if value.StopReceipt != nil {
		receipt := *value.StopReceipt
		receipt.CgroupIdentity = cloneLinuxCgroupIdentity(value.StopReceipt.CgroupIdentity)
		receipt.LinuxProof = cloneLinuxCgroupStopProof(value.StopReceipt.LinuxProof)
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
		value.TargetBirthKind != other.TargetBirthKind || value.OwnerUID != other.OwnerUID || value.LauncherUID != other.LauncherUID ||
		value.AnchorPID != other.AnchorPID || value.AnchorIdentity != other.AnchorIdentity ||
		value.LauncherPID != other.LauncherPID || value.LauncherIdentity != other.LauncherIdentity ||
		!equalLinuxCgroupIdentity(value.CgroupIdentity, other.CgroupIdentity) ||
		!equalLinuxCgroupExitProof(value.LauncherExit, other.LauncherExit) || !equalLinuxCgroupProcessIdentity(value.Mirror, other.Mirror) ||
		!equalUint32(value.CreatorSessionID, other.CreatorSessionID) {
		return false
	}
	if value.StopReceipt == nil || other.StopReceipt == nil {
		return value.StopReceipt == nil && other.StopReceipt == nil
	}
	return equalStopReceipt(*value.StopReceipt, *other.StopReceipt)
}

// Validate checks the durable pre-authority handoff shape. A helper identity
// is either completely absent or completely present; partial state is unsafe
// because it cannot identify the retained helper after a daemon restart.
func (value SupervisorHandoff) Validate() error {
	if value.Version == SupervisorHandoffVersionV2 {
		if value.ScanUpdate != nil || validateV2SupervisorFields(value.LaunchToken, value.OwnerKind, value.OwnerContext,
			value.TargetPID, value.TargetIdentity, value.SupervisorPID, value.SupervisorIdentity,
			value.TargetBirthKind, value.OwnerUID, value.LauncherUID, value.CgroupIdentity, value.AnchorPID, value.AnchorIdentity,
			value.LauncherPID, value.LauncherIdentity, value.LauncherExit, value.Mirror,
			value.CreatorSessionID, false) != nil {
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
	if value.OwnerUID != 0 || value.LauncherUID != 0 || hasV2SupervisorFields(value.TargetBirthKind, value.CgroupIdentity, value.AnchorPID, value.AnchorIdentity,
		value.LauncherPID, value.LauncherIdentity, value.LauncherExit, value.Mirror) {
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

// ToSupervisor converts a fully bound handoff into the existing durable
// authority type. The helper identity is required at the commit boundary.
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
	result := Supervisor{
		Version:            value.Version,
		Secret:             value.Secret,
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
		TargetBirthKind:    value.TargetBirthKind,
		CgroupIdentity:     cloneLinuxCgroupIdentity(value.CgroupIdentity),
		AnchorPID:          value.AnchorPID,
		AnchorIdentity:     value.AnchorIdentity,
		LauncherPID:        value.LauncherPID,
		LauncherIdentity:   value.LauncherIdentity,
		LauncherExit:       cloneLinuxCgroupExitProof(value.LauncherExit),
		Mirror:             cloneLinuxCgroupProcessIdentity(value.Mirror),
		CreatorSessionID:   cloneUint32(value.CreatorSessionID),
	}
	if value.StopReceipt != nil {
		receipt := *value.StopReceipt
		receipt.CgroupIdentity = cloneLinuxCgroupIdentity(value.StopReceipt.CgroupIdentity)
		receipt.LinuxProof = cloneLinuxCgroupStopProof(value.StopReceipt.LinuxProof)
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
	cloned.CgroupIdentity = cloneLinuxCgroupIdentity(value.CgroupIdentity)
	cloned.LauncherExit = cloneLinuxCgroupExitProof(value.LauncherExit)
	cloned.Mirror = cloneLinuxCgroupProcessIdentity(value.Mirror)
	if value.StopReceipt != nil {
		receipt := *value.StopReceipt
		receipt.CgroupIdentity = cloneLinuxCgroupIdentity(value.StopReceipt.CgroupIdentity)
		receipt.LinuxProof = cloneLinuxCgroupStopProof(value.StopReceipt.LinuxProof)
		cloned.StopReceipt = &receipt
	}
	if value.ScanUpdate != nil {
		update := *value.ScanUpdate
		cloned.ScanUpdate = &update
	}
	return cloned
}

// Equal compares every durable handoff field, including helper identity and
// stop receipt. It is used by exact compare-and-set mutations.
func (value SupervisorHandoff) Equal(other SupervisorHandoff) bool {
	if value.Version != other.Version || value.LaunchToken != other.LaunchToken || value.Secret != other.Secret || value.OwnerKind != other.OwnerKind || value.OwnerContext != other.OwnerContext || value.TargetPID != other.TargetPID ||
		value.TargetIdentity != other.TargetIdentity || value.PipeToken != other.PipeToken || value.JobID != other.JobID ||
		value.SupervisorPID != other.SupervisorPID || value.SupervisorIdentity != other.SupervisorIdentity ||
		value.TargetBirthKind != other.TargetBirthKind || value.OwnerUID != other.OwnerUID || value.LauncherUID != other.LauncherUID ||
		value.AnchorPID != other.AnchorPID || value.AnchorIdentity != other.AnchorIdentity ||
		value.LauncherPID != other.LauncherPID || value.LauncherIdentity != other.LauncherIdentity ||
		!equalLinuxCgroupIdentity(value.CgroupIdentity, other.CgroupIdentity) ||
		!equalLinuxCgroupExitProof(value.LauncherExit, other.LauncherExit) || !equalLinuxCgroupProcessIdentity(value.Mirror, other.Mirror) ||
		!equalUint32(value.CreatorSessionID, other.CreatorSessionID) {
		return false
	}
	if value.StopReceipt == nil || other.StopReceipt == nil {
		return value.StopReceipt == nil && other.StopReceipt == nil
	}
	return equalStopReceipt(*value.StopReceipt, *other.StopReceipt)
}

// SameLaunch compares the immutable launch binding while ignoring the helper
// binding and terminal receipt, which may be added by later dedicated phases.
func (value SupervisorHandoff) SameLaunch(other SupervisorHandoff) bool {
	return value.Version == other.Version && value.LaunchToken == other.LaunchToken && value.Secret == other.Secret &&
		value.OwnerKind == other.OwnerKind && value.OwnerContext == other.OwnerContext && value.TargetPID == other.TargetPID && value.TargetIdentity == other.TargetIdentity && value.PipeToken == other.PipeToken && value.JobID == other.JobID &&
		value.TargetBirthKind == other.TargetBirthKind && value.OwnerUID == other.OwnerUID && value.LauncherUID == other.LauncherUID &&
		equalLinuxCgroupIdentity(value.CgroupIdentity, other.CgroupIdentity) &&
		value.AnchorPID == other.AnchorPID && value.AnchorIdentity == other.AnchorIdentity &&
		value.LauncherPID == other.LauncherPID && value.LauncherIdentity == other.LauncherIdentity &&
		equalLinuxCgroupExitProof(value.LauncherExit, other.LauncherExit) &&
		equalUint32(value.CreatorSessionID, other.CreatorSessionID)
}

// ValidForHandoff reports whether a stop receipt exactly identifies a bound
// handoff. The receipt schema remains shared with the existing Supervisor
// authority for backwards compatibility.
func (receipt StopReceipt) ValidForHandoff(value SupervisorHandoff) bool {
	if value.Version == SupervisorHandoffVersionV2 {
		return value.SupervisorPID > 0 && value.Mirror != nil && receipt.Version == SupervisorVersionV2 &&
			receipt.Status == "stopped" && receipt.ActiveProcesses == 0 &&
			receipt.OwnerKind == value.OwnerKind && receipt.OwnerContext == value.OwnerContext &&
			receipt.TargetPID == value.TargetPID && receipt.TargetIdentity == value.TargetIdentity &&
			receipt.OwnerUID == value.OwnerUID && receipt.LauncherUID == value.LauncherUID &&
			receipt.PipeToken == value.PipeToken && receipt.JobID == value.JobID &&
			receipt.SupervisorPID == value.SupervisorPID && receipt.SupervisorIdentity == value.SupervisorIdentity &&
			receipt.TargetBirthKind == value.TargetBirthKind && equalLinuxCgroupIdentity(receipt.CgroupIdentity, value.CgroupIdentity) &&
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

// ReleaseProof preserves the legacy v1 proof constructor. V2 Linux release
// evidence is minted only by the platform after pinned cgroup removal; this
// method returns a zero value for v2. CreatorSessionID is copied so a v1 proof
// cannot mutate the handoff through its optional session pointer.
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

// Validate checks a release proof before it can clear a pending handoff.
func (proof SupervisorHandoffReleaseProof) Validate() error {
	if proof.Version == SupervisorHandoffVersionV2 {
		if proof.Status != "released" || !validLowerHex(proof.LaunchToken, TokenBytes*2) ||
			validateV2SupervisorFields(proof.LaunchToken, proof.OwnerKind, proof.OwnerContext,
				proof.TargetPID, proof.TargetIdentity, proof.SupervisorPID, proof.SupervisorIdentity,
				proof.TargetBirthKind, proof.OwnerUID, proof.LauncherUID, proof.CgroupIdentity, proof.AnchorPID, proof.AnchorIdentity,
				proof.LauncherPID, proof.LauncherIdentity, proof.LauncherExit, proof.Mirror,
				proof.CreatorSessionID, true) != nil ||
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
	if proof.OwnerUID != 0 || proof.LauncherUID != 0 || proof.StopReceipt != nil || hasV2SupervisorFields(proof.TargetBirthKind, proof.CgroupIdentity, proof.AnchorPID, proof.AnchorIdentity,
		proof.LauncherPID, proof.LauncherIdentity, proof.LauncherExit, proof.Mirror) || proof.LeafDisposition != "" {
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
	if value.Version == SupervisorHandoffVersionV2 {
		return proof.Version == SupervisorHandoffVersionV2 && proof.Status == "released" &&
			proof.LaunchToken == value.LaunchToken && proof.OwnerKind == value.OwnerKind && proof.OwnerContext == value.OwnerContext &&
			proof.TargetPID == value.TargetPID && proof.TargetIdentity == value.TargetIdentity &&
			proof.OwnerUID == value.OwnerUID && proof.LauncherUID == value.LauncherUID &&
			proof.PipeToken == value.PipeToken && proof.JobID == value.JobID &&
			proof.SupervisorPID == value.SupervisorPID && proof.SupervisorIdentity == value.SupervisorIdentity &&
			proof.TargetBirthKind == value.TargetBirthKind && equalLinuxCgroupIdentity(proof.CgroupIdentity, value.CgroupIdentity) &&
			proof.AnchorPID == value.AnchorPID && proof.AnchorIdentity == value.AnchorIdentity &&
			proof.LauncherPID == value.LauncherPID && proof.LauncherIdentity == value.LauncherIdentity &&
			equalLinuxCgroupExitProof(proof.LauncherExit, value.LauncherExit) &&
			equalLinuxCgroupProcessIdentity(proof.Mirror, value.Mirror) &&
			(proof.LeafDisposition == LinuxCgroupLeafRemoved || proof.LeafDisposition == LinuxCgroupLeafAbsentAfterReceipt) &&
			proof.StopReceipt != nil && value.StopReceipt != nil && proof.StopReceipt.Equal(*value.StopReceipt)
	}
	return proof.Version == SupervisorHandoffVersion && proof.Status == "released" &&
		proof.LaunchToken == value.LaunchToken && proof.OwnerKind == value.OwnerKind && proof.OwnerContext == value.OwnerContext && proof.TargetPID == value.TargetPID && proof.TargetIdentity == value.TargetIdentity &&
		proof.PipeToken == value.PipeToken && proof.JobID == value.JobID &&
		proof.SupervisorPID == value.SupervisorPID && proof.SupervisorIdentity == value.SupervisorIdentity &&
		equalUint32(proof.CreatorSessionID, value.CreatorSessionID) && proof.OwnerUID == 0 && proof.LauncherUID == 0 && proof.LeafDisposition == "" && proof.StopReceipt == nil &&
		!hasV2SupervisorFields(proof.TargetBirthKind, proof.CgroupIdentity, proof.AnchorPID, proof.AnchorIdentity,
			proof.LauncherPID, proof.LauncherIdentity, proof.LauncherExit, proof.Mirror)
}

// ValidForSupervisor reports whether a v2 release proof identifies the exact
// committed authority whose process marker and cgroup reservation it clears.
func (proof SupervisorHandoffReleaseProof) ValidForSupervisor(value Supervisor) bool {
	return value.Version == SupervisorVersionV2 && proof.Version == SupervisorHandoffVersionV2 &&
		proof.Status == "released" && proof.LaunchToken == value.LaunchToken &&
		proof.OwnerKind == value.OwnerKind && proof.OwnerContext == value.OwnerContext &&
		proof.TargetPID == value.TargetPID && proof.TargetIdentity == value.TargetIdentity &&
		proof.OwnerUID == value.OwnerUID && proof.LauncherUID == value.LauncherUID &&
		proof.PipeToken == value.PipeToken && proof.JobID == value.JobID &&
		proof.SupervisorPID == value.SupervisorPID && proof.SupervisorIdentity == value.SupervisorIdentity &&
		proof.TargetBirthKind == value.TargetBirthKind && equalLinuxCgroupIdentity(proof.CgroupIdentity, value.CgroupIdentity) &&
		proof.AnchorPID == value.AnchorPID && proof.AnchorIdentity == value.AnchorIdentity &&
		proof.LauncherPID == value.LauncherPID && proof.LauncherIdentity == value.LauncherIdentity &&
		equalLinuxCgroupExitProof(proof.LauncherExit, value.LauncherExit) &&
		equalLinuxCgroupProcessIdentity(proof.Mirror, value.Mirror) &&
		(proof.LeafDisposition == LinuxCgroupLeafRemoved || proof.LeafDisposition == LinuxCgroupLeafAbsentAfterReceipt) &&
		proof.StopReceipt != nil && value.StopReceipt != nil && proof.StopReceipt.Equal(*value.StopReceipt)
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
	return value.Version == SupervisorHandoffVersion && proof.Version == SupervisorHandoffAbortProofVersion && proof.Disposition == SupervisorHandoffAbortDisposition &&
		proof.LaunchToken == value.LaunchToken && proof.JobID == value.JobID && proof.OwnerKind == value.OwnerKind && proof.OwnerContext == value.OwnerContext &&
		proof.TargetPID == value.TargetPID && proof.TargetIdentity == value.TargetIdentity &&
		proof.SupervisorPID == value.SupervisorPID && proof.SupervisorIdentity == value.SupervisorIdentity &&
		(proof.OwnerKind == OwnerKindLinuxHelper || (proof.CreatorSessionID != nil && value.CreatorSessionID != nil)) && equalUint32(proof.CreatorSessionID, value.CreatorSessionID)
}

func validateV2SupervisorFields(launchToken, ownerKind, ownerContext string, targetPID int, targetIdentity string,
	supervisorPID int, supervisorIdentity, targetBirthKind string, ownerUID, launcherUID uint32,
	cgroup *LinuxCgroupIdentity,
	anchorPID int, anchorIdentity string, launcherPID int, launcherIdentity string,
	launcherExit *LinuxCgroupExitProof, mirror *LinuxCgroupProcessIdentity,
	creatorSessionID *uint32, requireHelper bool) error {
	if !validLowerHex(launchToken, TokenBytes*2) || ownerKind != OwnerKindLinuxHelper || !validBounded(ownerContext, 4096) ||
		targetPID <= 0 || !validBounded(targetIdentity, 4096) || targetBirthKind != LinuxCgroupTargetBirthTrustedAGate ||
		ownerUID == 0 || launcherUID != 0 || cgroup == nil || cgroup.Validate() != nil ||
		anchorPID <= 0 || !validBounded(anchorIdentity, 4096) ||
		launcherPID <= 0 || !validBounded(launcherIdentity, 4096) || launcherExit == nil || creatorSessionID != nil {
		return errors.New("Linux cgroup supervisor identity is invalid")
	}
	launcher := LinuxCgroupProcessIdentity{PID: launcherPID, Identity: launcherIdentity}
	anchor := LinuxCgroupProcessIdentity{PID: anchorPID, Identity: anchorIdentity}
	target := LinuxCgroupProcessIdentity{PID: targetPID, Identity: targetIdentity}
	if launcherExit.Validate() != nil || !launcherExit.Process.Equal(launcher) {
		return errors.New("Linux cgroup launcher exit identity is invalid")
	}
	if launcherExit.Witness.Equal(anchor) {
		if launcherExit.HelperExit == nil || !launcherExit.HelperExit.ValidFor(launcherExit.HelperExit.Process, anchor) {
			return errors.New("Linux launcher anchor wait lacks exact helper-death adoption proof")
		}
	} else if launcherExit.HelperExit != nil {
		return errors.New("Linux launcher helper wait unexpectedly carries helper-death proof")
	}
	if target.Equal(anchor) || target.Equal(launcher) || anchor.Equal(launcher) {
		return errors.New("Linux cgroup producer roles alias one process identity")
	}
	if supervisorPID < 0 || (supervisorPID == 0) != (strings.TrimSpace(supervisorIdentity) == "") ||
		!validOptionalBounded(supervisorIdentity, 4096) || (requireHelper && supervisorPID == 0) {
		return errors.New("Linux cgroup helper identity is invalid")
	}
	if supervisorPID == 0 {
		if mirror != nil {
			return errors.New("Linux cgroup mirror cannot precede helper binding")
		}
		return nil
	}
	helper := LinuxCgroupProcessIdentity{PID: supervisorPID, Identity: supervisorIdentity}
	if helper.Validate() != nil || mirror == nil || mirror.Validate() != nil {
		return errors.New("Linux cgroup mirror identity is invalid")
	}
	if launcherExit.Witness.Equal(anchor) && (launcherExit.HelperExit == nil || !launcherExit.HelperExit.ValidFor(helper, anchor)) {
		return errors.New("Linux launcher anchor witness does not identify the exact helper exit")
	}
	if !launcherExit.ValidForOwnedDescendant(launcher, helper, anchor, launcherExit.HelperExit) {
		return errors.New("Linux launcher exit witness does not match the exact helper or adopted child")
	}
	if target.Equal(helper) || target.Equal(*mirror) || anchor.Equal(helper) || anchor.Equal(*mirror) ||
		helper.Equal(launcher) || helper.Equal(*mirror) || launcher.Equal(*mirror) {
		return errors.New("Linux cgroup producer roles alias one process identity")
	}
	return nil
}

func hasV2SupervisorFields(targetBirthKind string, cgroup *LinuxCgroupIdentity, anchorPID int,
	anchorIdentity string, launcherPID int, launcherIdentity string,
	launcherExit *LinuxCgroupExitProof, mirror *LinuxCgroupProcessIdentity) bool {
	return targetBirthKind != "" || cgroup != nil || anchorPID != 0 || anchorIdentity != "" ||
		launcherPID != 0 || launcherIdentity != "" || launcherExit != nil || mirror != nil
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

func supervisorFromHandoff(value SupervisorHandoff) Supervisor {
	return Supervisor{
		Version:            value.Version,
		Secret:             value.Secret,
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
		TargetBirthKind:    value.TargetBirthKind,
		CgroupIdentity:     cloneLinuxCgroupIdentity(value.CgroupIdentity),
		AnchorPID:          value.AnchorPID,
		AnchorIdentity:     value.AnchorIdentity,
		LauncherPID:        value.LauncherPID,
		LauncherIdentity:   value.LauncherIdentity,
		LauncherExit:       cloneLinuxCgroupExitProof(value.LauncherExit),
		Mirror:             cloneLinuxCgroupProcessIdentity(value.Mirror),
		CreatorSessionID:   cloneUint32(value.CreatorSessionID),
	}
}

func supervisorFromReleaseProof(proof SupervisorHandoffReleaseProof) Supervisor {
	return Supervisor{
		Version:            SupervisorVersionV2,
		LaunchToken:        proof.LaunchToken,
		OwnerKind:          proof.OwnerKind,
		OwnerContext:       proof.OwnerContext,
		TargetPID:          proof.TargetPID,
		TargetIdentity:     proof.TargetIdentity,
		OwnerUID:           proof.OwnerUID,
		LauncherUID:        proof.LauncherUID,
		PipeToken:          proof.PipeToken,
		JobID:              proof.JobID,
		SupervisorPID:      proof.SupervisorPID,
		SupervisorIdentity: proof.SupervisorIdentity,
		TargetBirthKind:    proof.TargetBirthKind,
		CgroupIdentity:     cloneLinuxCgroupIdentity(proof.CgroupIdentity),
		AnchorPID:          proof.AnchorPID,
		AnchorIdentity:     proof.AnchorIdentity,
		LauncherPID:        proof.LauncherPID,
		LauncherIdentity:   proof.LauncherIdentity,
		LauncherExit:       cloneLinuxCgroupExitProof(proof.LauncherExit),
		Mirror:             cloneLinuxCgroupProcessIdentity(proof.Mirror),
		StopReceipt:        cloneStopReceipt(proof.StopReceipt),
	}
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
