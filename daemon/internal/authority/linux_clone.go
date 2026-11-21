package authority

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strconv"
	"strings"
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
	return value.validate(true)
}

// ValidateForJournal permits structurally valid target paths recorded by the
// original clone-v2 journal schema, which did not enforce the deterministic
// RunRef suffix. It is only for reading existing durable records; new grants
// and filesystem effects must continue to use Validate.
func (value LinuxCloneIntent) ValidateForJournal() error {
	return value.validate(false)
}

func (value LinuxCloneIntent) validate(requireRunRefLayout bool) error {
	if value.Version != LinuxCloneVersion || !validLinuxCloneRunID(value.RunID) || value.Generation <= 0 ||
		!validLinuxCloneRunID(value.BindingKey) || !validLinuxCloneResourceID(value.ResourceID) ||
		!validLinuxCloneGitObjectID(value.BaseCommit) ||
		!validLinuxCloneDigest(value.BaseTreeDigest) || !validLowerHex(value.Nonce, LinuxCloneNonceBytes*2) ||
		!validLinuxClonePath(value.SeedPath) || !validLinuxClonePath(value.Target) ||
		linuxClonePathsOverlap(value.SeedPath, value.Target) {
		return errors.New("Linux clone intent is invalid")
	}
	if requireRunRefLayout {
		if _, err := linuxCloneWorkspaceRootFromIntent(value); err != nil {
			return errors.New("Linux clone target does not match its binding RunRef layout")
		}
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

// LinuxCloneReuseStop mirrors the exact stopped native execution whose
// retained session is being resumed. It is deliberately independent of the
// state package so the RunJournal can persist the authority without an import
// cycle.
type LinuxCloneReuseStop struct {
	RunID          string `json:"run_id"`
	Generation     int64  `json:"generation"`
	SessionID      string `json:"session_id"`
	LocalHandleID  string `json:"local_handle_id"`
	BindingID      string `json:"binding_id"`
	DeliveryDigest string `json:"delivery_digest"`
	ReceiptID      string `json:"receipt_id"`
}

func (value LinuxCloneReuseStop) Validate() error {
	if !validLinuxCloneRunID(value.RunID) || value.Generation <= 0 ||
		!validRequiredAuthorityString(value.SessionID, 4096) ||
		!validRequiredAuthorityString(value.LocalHandleID, 4096) ||
		!validRequiredAuthorityString(value.BindingID, 4096) ||
		!validLowerHex(value.DeliveryDigest, LinuxCloneNonceBytes*2) ||
		!validRequiredAuthorityString(value.ReceiptID, 4096) {
		return errors.New("Linux clone reuse stop certificate is invalid")
	}
	return nil
}

func (value LinuxCloneReuseStop) Equal(other LinuxCloneReuseStop) bool {
	return value == other
}

func (value LinuxCloneReuseStop) Clone() LinuxCloneReuseStop { return value }

// LinuxCloneReuseGrant authorizes a new RunKey to consume the exact retained
// target of a stopped predecessor. The source clone records remain owned by
// the source RunJournal; this grant never transfers deletion or promotion
// ownership.
type LinuxCloneReuseGrant struct {
	Version              int                       `json:"version"`
	Source               LinuxCloneGrant           `json:"source"`
	SourceCheckout       LinuxCloneCheckoutReceipt `json:"source_checkout"`
	PredecessorStop      LinuxCloneReuseStop       `json:"predecessor_stop"`
	RunID                string                    `json:"run_id"`
	Generation           int64                     `json:"generation"`
	WorkspaceFingerprint string                    `json:"workspace_fingerprint"`
}

func (value LinuxCloneReuseGrant) Validate() error {
	if value.Version != LinuxCloneVersion || value.Source.Validate() != nil ||
		value.SourceCheckout.Validate() != nil || value.PredecessorStop.Validate() != nil ||
		!validLinuxCloneRunID(value.RunID) || value.Generation <= 0 ||
		!validLinuxCloneDigest(value.WorkspaceFingerprint) {
		return errors.New("Linux clone reuse grant is invalid")
	}
	if value.RunID == value.Source.Intent.RunID && value.Generation == value.Source.Intent.Generation {
		return errors.New("Linux clone reuse grant must name a new RunKey")
	}
	intent := value.Source.Intent
	receipt := value.SourceCheckout
	if receipt.RunID != intent.RunID || receipt.Generation != intent.Generation ||
		receipt.BindingKey != intent.BindingKey || receipt.ResourceID != intent.ResourceID ||
		receipt.Target != intent.Target || receipt.Nonce != intent.Nonce ||
		receipt.SeedDigest != value.Source.Seed.Digest || receipt.BaseCommit != intent.BaseCommit {
		return errors.New("Linux clone reuse source checkout differs from its source grant")
	}
	return nil
}

func (value LinuxCloneReuseGrant) Equal(other LinuxCloneReuseGrant) bool {
	return value.Version == other.Version && value.Source.Equal(other.Source) &&
		value.SourceCheckout.Equal(other.SourceCheckout) &&
		value.PredecessorStop.Equal(other.PredecessorStop) &&
		value.RunID == other.RunID && value.Generation == other.Generation &&
		value.WorkspaceFingerprint == other.WorkspaceFingerprint
}

func (value LinuxCloneReuseGrant) Clone() LinuxCloneReuseGrant {
	return LinuxCloneReuseGrant{
		Version:              value.Version,
		Source:               value.Source.Clone(),
		SourceCheckout:       value.SourceCheckout.Clone(),
		PredecessorStop:      value.PredecessorStop.Clone(),
		RunID:                value.RunID,
		Generation:           value.Generation,
		WorkspaceFingerprint: value.WorkspaceFingerprint,
	}
}

func validRequiredAuthorityString(value string, limit int) bool {
	return value != "" && len(value) <= limit && !strings.ContainsRune(value, '\x00')
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

// LinuxClonePromotionIntent is the immutable candidate subject reserved in
// State before A imports or compare-creates the run-keyed promotion ref.
// Ref is deliberately derived from the enclosing RunKey, not supplied here.
type LinuxClonePromotionIntent struct {
	Version    int    `json:"version"`
	ResourceID string `json:"resource_id"`
	Commit     string `json:"commit"`
	TreeDigest string `json:"tree_digest"`
}

func (intent LinuxClonePromotionIntent) Validate() error {
	if intent.Version != LinuxCloneVersion || !validLinuxCloneResourceID(intent.ResourceID) ||
		!validLinuxCloneGitObjectID(intent.Commit) || !validLinuxCloneDigest(intent.TreeDigest) {
		return errors.New("Linux clone promotion intent is invalid")
	}
	return nil
}

func (intent LinuxClonePromotionIntent) Equal(other LinuxClonePromotionIntent) bool {
	return intent.Version == other.Version && intent.ResourceID == other.ResourceID &&
		intent.Commit == other.Commit && intent.TreeDigest == other.TreeDigest
}

func (intent LinuxClonePromotionIntent) Clone() LinuxClonePromotionIntent { return intent }

// LinuxCloneAbortProof certifies A-side cleanup of an intent without a durable
// checkout receipt: the exact target, seed path and run-keyed refs are
// independently verified absent. A new proof may bind only no-gate evidence;
// a normal release can have exposed the clone to B before checkout-ready was
// persisted, so it cannot establish that the target tree is absent.
type LinuxCloneAbortProof struct {
	Version             int                               `json:"version"`
	Intent              LinuxCloneIntent                  `json:"intent"`
	SeedReceipt         *LinuxCloneSeedReceipt            `json:"seed_receipt,omitempty"`
	PreGateReleaseProof *LinuxCgroupPreGateReleaseProof   `json:"pre_gate_release_proof,omitempty"`
	CgroupAbortProof    *LinuxCgroupReservationAbortProof `json:"cgroup_abort_proof,omitempty"`
	// NormalReleaseProof is decode-only compatibility for old unresolved
	// journals. Validate rejects it; a normal release cannot authorize no-target
	// cleanup because B may already have cloned or renamed the target.
	NormalReleaseProof *SupervisorHandoffReleaseProof `json:"normal_release_proof,omitempty"`
	BaseRef            string                         `json:"base_ref"`
	PromotionRef       string                         `json:"promotion_ref"`
	TargetAbsent       bool                           `json:"target_absent"`
	SeedAbsent         bool                           `json:"seed_absent"`
	BaseRefAbsent      bool                           `json:"base_ref_absent"`
	PromotionRefAbsent bool                           `json:"promotion_ref_absent"`
}

func (proof LinuxCloneAbortProof) Validate() error {
	return proof.validate(false)
}

// ValidateForJournal accepts pre-layout target paths and legacy normal-release
// proof fields only while loading existing durable tombstones. It is strictly
// read-only compatibility: new proof writes and filesystem effects must call
// Validate, which rejects normal-release no-target authority.
func (proof LinuxCloneAbortProof) ValidateForJournal() error {
	return proof.validate(true)
}

func (proof LinuxCloneAbortProof) validate(allowLegacyJournalEvidence bool) error {
	baseRef, baseErr := LinuxCloneBaseRef(proof.Intent.RunID, proof.Intent.Generation)
	promotionRef, promotionErr := LinuxClonePromotionRef(proof.Intent.RunID, proof.Intent.Generation)
	intentErr := proof.Intent.Validate()
	if allowLegacyJournalEvidence {
		intentErr = proof.Intent.ValidateForJournal()
	}
	if proof.Version != LinuxCloneVersion || intentErr != nil || baseErr != nil || promotionErr != nil ||
		proof.BaseRef != baseRef || proof.PromotionRef != promotionRef || !proof.TargetAbsent || !proof.SeedAbsent ||
		!proof.BaseRefAbsent || !proof.PromotionRefAbsent ||
		countLinuxCloneAbortHistories(proof) > 1 ||
		proof.SeedReceipt != nil && (proof.SeedReceipt.Validate() != nil || proof.SeedReceipt.BaseCommit != proof.Intent.BaseCommit) ||
		proof.PreGateReleaseProof != nil && proof.PreGateReleaseProof.Validate() != nil ||
		proof.CgroupAbortProof != nil && proof.CgroupAbortProof.Validate() != nil ||
		proof.NormalReleaseProof != nil && (proof.NormalReleaseProof.Validate() != nil ||
			proof.NormalReleaseProof.Version != SupervisorHandoffVersionV2 || proof.NormalReleaseProof.StopReceipt == nil || !allowLegacyJournalEvidence) {
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
	if proof.NormalReleaseProof != nil {
		normalReleaseProof := proof.NormalReleaseProof.Clone()
		cloned.NormalReleaseProof = &normalReleaseProof
	}
	return cloned
}

func (proof LinuxCloneAbortProof) Equal(other LinuxCloneAbortProof) bool {
	return proof.Version == other.Version && proof.Intent.Equal(other.Intent) &&
		equalLinuxCloneSeedReceipt(proof.SeedReceipt, other.SeedReceipt) &&
		equalLinuxCgroupPreGateReleaseProof(proof.PreGateReleaseProof, other.PreGateReleaseProof) &&
		equalLinuxCgroupReservationAbortProof(proof.CgroupAbortProof, other.CgroupAbortProof) &&
		equalSupervisorHandoffReleaseProof(proof.NormalReleaseProof, other.NormalReleaseProof) &&
		proof.BaseRef == other.BaseRef && proof.PromotionRef == other.PromotionRef &&
		proof.TargetAbsent == other.TargetAbsent && proof.SeedAbsent == other.SeedAbsent &&
		proof.BaseRefAbsent == other.BaseRefAbsent && proof.PromotionRefAbsent == other.PromotionRefAbsent
}

func countLinuxCloneAbortHistories(proof LinuxCloneAbortProof) int {
	count := 0
	if proof.PreGateReleaseProof != nil {
		count++
	}
	if proof.CgroupAbortProof != nil {
		count++
	}
	if proof.NormalReleaseProof != nil {
		count++
	}
	return count
}

func equalLinuxCloneSeedReceipt(left, right *LinuxCloneSeedReceipt) bool {
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
	keyHash, err := LinuxCloneRunKeyHash(runID, generation)
	if err != nil {
		return "", err
	}
	return "refs/heads/symmetry/clone-v2/base/" + keyHash, nil
}

// LinuxClonePromotionRef returns the A-owned branch ref for the verified
// candidate promoted from one run's B-owned clone.
func LinuxClonePromotionRef(runID string, generation int64) (string, error) {
	keyHash, err := LinuxCloneRunKeyHash(runID, generation)
	if err != nil {
		return "", err
	}
	return "refs/heads/symmetry/clone-v2/promoted/" + keyHash, nil
}

// LinuxCloneWorkspaceRoot derives the configured B-writable root from the
// deterministic binding/RunKey Target layout. It does not resolve or open the
// path; Workspace preflight remains responsible for filesystem identity and ACLs.
func LinuxCloneWorkspaceRoot(intent LinuxCloneIntent) (string, error) {
	if err := intent.Validate(); err != nil {
		return "", err
	}
	return linuxCloneWorkspaceRootFromIntent(intent)
}

func linuxCloneWorkspaceRootFromIntent(intent LinuxCloneIntent) (string, error) {
	suffix := "/binding-" + intent.BindingKey + "/run-" + intent.RunID + "/generation-" + strconv.FormatInt(intent.Generation, 10)
	if !strings.HasSuffix(intent.Target, suffix) {
		return "", errors.New("Linux clone Target does not follow the deterministic RunRef layout")
	}
	root := strings.TrimSuffix(intent.Target, suffix)
	if root == "" {
		root = "/"
	}
	if !validLinuxClonePath(root) || root == "/" {
		return "", errors.New("Linux clone workspace root is invalid")
	}
	return root, nil
}

// LinuxCloneCleanupIntent freezes every immutable authority and measured
// identity needed by the normal, post-promotion cleanup effect. FenceDigest is
// a non-secret SHA-256 binding of the complete current protocol.Fence; the raw
// lease token is never sent to the root cleanup worker.
type LinuxCloneCleanupIntent struct {
	Version                int                           `json:"version"`
	Intent                 LinuxCloneIntent              `json:"intent"`
	FenceDigest            string                        `json:"fence_digest"`
	RootPath               string                        `json:"root_path"`
	BaseRef                string                        `json:"base_ref"`
	SeedReceipt            LinuxCloneSeedReceipt         `json:"seed_receipt"`
	CheckoutReceipt        LinuxCloneCheckoutReceipt     `json:"checkout_receipt"`
	PromotionIntent        LinuxClonePromotionIntent     `json:"promotion_intent"`
	PromotionReceipt       LinuxClonePromotionReceipt    `json:"promotion_receipt"`
	NormalReleaseProof     SupervisorHandoffReleaseProof `json:"normal_release_proof"`
	ArtifactEvidenceID     string                        `json:"artifact_evidence_id"`
	ArtifactEvidenceKey    string                        `json:"artifact_evidence_key"`
	ArtifactSubjectHash    string                        `json:"artifact_subject_hash"`
	ArtifactDeliveryDigest string                        `json:"artifact_delivery_digest"`
}

func (intent LinuxCloneCleanupIntent) Validate() error {
	root, rootErr := LinuxCloneWorkspaceRoot(intent.Intent)
	baseRef, baseRefErr := LinuxCloneBaseRef(intent.Intent.RunID, intent.Intent.Generation)
	promotionRef, promotionRefErr := LinuxClonePromotionRef(intent.Intent.RunID, intent.Intent.Generation)
	if intent.Version != LinuxCloneVersion || intent.Intent.Validate() != nil || rootErr != nil ||
		intent.RootPath != root || baseRefErr != nil || promotionRefErr != nil || intent.BaseRef != baseRef || !validLinuxCloneDigest(intent.FenceDigest) ||
		intent.SeedReceipt.Validate() != nil || intent.SeedReceipt.BaseCommit != intent.Intent.BaseCommit ||
		intent.CheckoutReceipt.Validate() != nil ||
		!intent.CheckoutReceipt.Equal(LinuxCloneCheckoutReceipt{
			Version: LinuxCloneVersion, RunID: intent.Intent.RunID, Generation: intent.Intent.Generation,
			BindingKey: intent.Intent.BindingKey, ResourceID: intent.Intent.ResourceID, Target: intent.Intent.Target,
			Nonce: intent.Intent.Nonce, SeedDigest: intent.SeedReceipt.Digest, BaseCommit: intent.Intent.BaseCommit,
			TargetDevice: intent.CheckoutReceipt.TargetDevice, TargetInode: intent.CheckoutReceipt.TargetInode,
		}) || intent.PromotionIntent.Validate() != nil ||
		intent.PromotionIntent.ResourceID != intent.Intent.ResourceID ||
		len(intent.PromotionIntent.Commit) != len(intent.Intent.BaseCommit) ||
		intent.PromotionReceipt.Validate() != nil ||
		intent.PromotionReceipt.Commit != intent.PromotionIntent.Commit ||
		intent.PromotionReceipt.TreeDigest != intent.PromotionIntent.TreeDigest ||
		intent.PromotionReceipt.Ref != promotionRef ||
		intent.NormalReleaseProof.Validate() != nil || intent.NormalReleaseProof.Version != SupervisorHandoffVersionV2 ||
		intent.NormalReleaseProof.StopReceipt == nil || !validLinuxCloneResourceID(intent.ArtifactEvidenceID) ||
		!validBounded(intent.ArtifactEvidenceKey, 4096) || !validLinuxCloneDigest(intent.ArtifactSubjectHash) ||
		!validLowerHex(intent.ArtifactDeliveryDigest, 64) {
		return errors.New("Linux clone cleanup intent is invalid")
	}
	return nil
}

func (intent LinuxCloneCleanupIntent) Clone() LinuxCloneCleanupIntent {
	cloned := intent
	cloned.Intent = intent.Intent.Clone()
	cloned.SeedReceipt = intent.SeedReceipt.Clone()
	cloned.CheckoutReceipt = intent.CheckoutReceipt.Clone()
	cloned.PromotionIntent = intent.PromotionIntent.Clone()
	cloned.PromotionReceipt = intent.PromotionReceipt.Clone()
	cloned.NormalReleaseProof = intent.NormalReleaseProof.Clone()
	return cloned
}

func (intent LinuxCloneCleanupIntent) Equal(other LinuxCloneCleanupIntent) bool {
	return intent.Version == other.Version && intent.Intent.Equal(other.Intent) && intent.FenceDigest == other.FenceDigest &&
		intent.RootPath == other.RootPath && intent.BaseRef == other.BaseRef &&
		intent.SeedReceipt.Equal(other.SeedReceipt) && intent.CheckoutReceipt.Equal(other.CheckoutReceipt) &&
		intent.PromotionIntent.Equal(other.PromotionIntent) && intent.PromotionReceipt.Equal(other.PromotionReceipt) &&
		intent.NormalReleaseProof.Equal(other.NormalReleaseProof) && intent.ArtifactEvidenceID == other.ArtifactEvidenceID &&
		intent.ArtifactEvidenceKey == other.ArtifactEvidenceKey && intent.ArtifactSubjectHash == other.ArtifactSubjectHash &&
		intent.ArtifactDeliveryDigest == other.ArtifactDeliveryDigest
}

// LinuxCloneTargetRemovalProof is the trusted root worker's receipt for one
// exact B target inode. It includes a full direct-child wait/reap proof and
// independently rewalked root/target identities; it does not assert that a
// same-UID B process cannot race a rename elsewhere in the owned root.
type LinuxCloneTargetRemovalProof struct {
	Version             int                        `json:"version"`
	Intent              LinuxCloneCleanupIntent    `json:"intent"`
	RootDevice          uint64                     `json:"root_device"`
	RootInode           uint64                     `json:"root_inode"`
	TargetDevice        uint64                     `json:"target_device"`
	TargetInode         uint64                     `json:"target_inode"`
	Disposition         string                     `json:"disposition"`
	PathRewalkVerified  bool                       `json:"path_rewalk_verified"`
	BOwnedMountConfined bool                       `json:"b_owned_mount_confined"`
	TargetNlinkZero     bool                       `json:"target_nlink_zero"`
	RootDeleterUID      uint32                     `json:"root_deleter_uid"`
	RootDeleter         LinuxCgroupProcessIdentity `json:"root_deleter"`
	RootDeleterExit     LinuxCgroupExitProof       `json:"root_deleter_exit"`
}

func (proof LinuxCloneTargetRemovalProof) Validate() error {
	witness := LinuxCgroupProcessIdentity{
		PID: proof.Intent.NormalReleaseProof.AnchorPID, Identity: proof.Intent.NormalReleaseProof.AnchorIdentity,
	}
	if proof.Version != LinuxCloneVersion || proof.Intent.Validate() != nil || proof.RootInode == 0 ||
		proof.TargetDevice != proof.Intent.CheckoutReceipt.TargetDevice ||
		proof.TargetInode != proof.Intent.CheckoutReceipt.TargetInode || proof.TargetInode == 0 ||
		proof.Disposition != LinuxCloneTargetDispositionRemoved || !proof.TargetNlinkZero ||
		!proof.PathRewalkVerified || !proof.BOwnedMountConfined || proof.RootDeleterUID != 0 ||
		proof.RootDeleter.Validate() != nil || proof.RootDeleterExit.Validate() != nil ||
		proof.RootDeleter.Equal(witness) || !proof.RootDeleterExit.ValidFor(proof.RootDeleter, witness) {
		return errors.New("Linux clone target removal proof is invalid")
	}
	return nil
}

func (proof LinuxCloneTargetRemovalProof) Clone() LinuxCloneTargetRemovalProof {
	cloned := proof
	cloned.Intent = proof.Intent.Clone()
	cloned.RootDeleterExit = proof.RootDeleterExit.Clone()
	return cloned
}

func (proof LinuxCloneTargetRemovalProof) Equal(other LinuxCloneTargetRemovalProof) bool {
	return proof.Version == other.Version && proof.Intent.Equal(other.Intent) &&
		proof.RootDevice == other.RootDevice && proof.RootInode == other.RootInode &&
		proof.TargetDevice == other.TargetDevice && proof.TargetInode == other.TargetInode &&
		proof.Disposition == other.Disposition && proof.PathRewalkVerified == other.PathRewalkVerified &&
		proof.BOwnedMountConfined == other.BOwnedMountConfined && proof.TargetNlinkZero == other.TargetNlinkZero &&
		proof.RootDeleterUID == other.RootDeleterUID &&
		proof.RootDeleter.Equal(other.RootDeleter) && proof.RootDeleterExit.Equal(other.RootDeleterExit)
}

// LinuxCloneCleanupProof is the final A-side readback for all normal clone
// cleanup effects. It preserves the promoted ref while proving the disposable
// B target, seed, base ref and owned scratch are absent.
type LinuxCloneCleanupProof struct {
	Version             int                          `json:"version"`
	Intent              LinuxCloneCleanupIntent      `json:"intent"`
	TargetRemovalProof  LinuxCloneTargetRemovalProof `json:"target_removal_proof"`
	SeedAbsent          bool                         `json:"seed_absent"`
	BaseRefAbsent       bool                         `json:"base_ref_absent"`
	ScratchAbsent       bool                         `json:"scratch_absent"`
	PromotionRefPresent bool                         `json:"promotion_ref_present"`
}

func (proof LinuxCloneCleanupProof) Validate() error {
	if proof.Version != LinuxCloneVersion || proof.Intent.Validate() != nil ||
		proof.TargetRemovalProof.Validate() != nil || !proof.TargetRemovalProof.Intent.Equal(proof.Intent) ||
		!proof.SeedAbsent || !proof.BaseRefAbsent || !proof.ScratchAbsent || !proof.PromotionRefPresent {
		return errors.New("Linux clone cleanup proof is invalid")
	}
	return nil
}

func (proof LinuxCloneCleanupProof) Clone() LinuxCloneCleanupProof {
	cloned := proof
	cloned.Intent = proof.Intent.Clone()
	cloned.TargetRemovalProof = proof.TargetRemovalProof.Clone()
	return cloned
}

func (proof LinuxCloneCleanupProof) Equal(other LinuxCloneCleanupProof) bool {
	return proof.Version == other.Version && proof.Intent.Equal(other.Intent) &&
		proof.TargetRemovalProof.Equal(other.TargetRemovalProof) && proof.SeedAbsent == other.SeedAbsent &&
		proof.BaseRefAbsent == other.BaseRefAbsent && proof.ScratchAbsent == other.ScratchAbsent &&
		proof.PromotionRefPresent == other.PromotionRefPresent
}

func LinuxCloneRunKeyHash(runID string, generation int64) (string, error) {
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
