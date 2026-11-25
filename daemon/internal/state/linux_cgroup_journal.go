package state

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// LinuxCgroupReservation is the single RunJournal's v2 run-leaf creation and
// producer-fencing record. Its key/fence are inherited from the enclosing
// journal. Parent and leaf name are immutable reservation intent; matching
// path/inode fields never recreate authority after all live pins are lost.
// AbortProof is an exact pre-effect no-target tombstone, not a stop receipt;
// retaining it makes lost abort-write responses replayable without making the
// aborted leaf a pending containment owner.
type LinuxCgroupReservation struct {
	Version          int                                 `json:"version"`
	LaunchToken      string                              `json:"launch_token"`
	Secret           string                              `json:"secret"`
	PipeToken        string                              `json:"pipe_token"`
	JobID            string                              `json:"job_id"`
	OwnerUID         uint32                              `json:"owner_uid"`
	LauncherUID      uint32                              `json:"launcher_uid"`
	RecoveryEndpoint string                              `json:"recovery_endpoint"`
	Parent           authority.LinuxCgroupParentIdentity `json:"parent"`
	LeafName         string                              `json:"leaf_name"`
	CreateStarted    bool                                `json:"create_started,omitempty"`
	// LauncherGrantPossible is set and fsynced before the grant can reach the
	// root launcher. Once true, the no-target abort path is permanently closed.
	LauncherGrantPossible bool                                        `json:"launcher_grant_possible,omitempty"`
	LeafIdentity          *authority.LinuxCgroupIdentity              `json:"leaf_identity,omitempty"`
	Anchor                authority.LinuxCgroupProcessIdentity        `json:"anchor"`
	Helper                *authority.LinuxCgroupProcessIdentity       `json:"helper,omitempty"`
	Launcher              *authority.LinuxCgroupProcessIdentity       `json:"launcher,omitempty"`
	LauncherExit          *authority.LinuxCgroupExitProof             `json:"launcher_exit,omitempty"`
	Gate                  *authority.LinuxCgroupGateIdentity          `json:"gate,omitempty"`
	HelperExit            *authority.LinuxCgroupExitProof             `json:"helper_exit,omitempty"`
	Mirror                *authority.LinuxCgroupProcessIdentity       `json:"mirror,omitempty"`
	AbortProof            *authority.LinuxCgroupReservationAbortProof `json:"abort_proof,omitempty"`
	PreGateStopReceipt    *authority.LinuxCgroupPreGateStopReceipt    `json:"pre_gate_stop_receipt,omitempty"`
}

const LinuxCgroupReservationVersion = 2

func (value LinuxCgroupReservation) Validate() error {
	if err := value.validateBaseIdentity(); err != nil {
		return err
	}
	if err := value.validateLeafIdentity(); err != nil {
		return err
	}
	if err := value.validateHelperIdentity(); err != nil {
		return err
	}
	if err := value.validateLauncher(); err != nil {
		return err
	}
	if err := value.validateGate(); err != nil {
		return err
	}
	if err := value.validateHelperExitProof(); err != nil {
		return err
	}
	if err := value.validateMirrorAliases(); err != nil {
		return err
	}
	if err := value.validateProofs(); err != nil {
		return err
	}
	if err := value.validateCreationOrder(); err != nil {
		return err
	}
	return nil
}

func (value LinuxCgroupReservation) validateBaseIdentity() error {
	if value.Version != LinuxCgroupReservationVersion || !validStateLowerHex(value.LaunchToken, authority.TokenBytes*2) ||
		!validStateLowerHex(value.Secret, authority.SecretBytes*2) || !validStateLowerHex(value.PipeToken, authority.TokenBytes*2) ||
		!validStateLowerHex(value.JobID, authority.TokenBytes*2) || !validRequiredString(value.RecoveryEndpoint, 4096) ||
		value.OwnerUID == 0 || value.LauncherUID != 0 || strings.IndexByte(value.RecoveryEndpoint, 0) >= 0 || value.Parent.Validate() != nil ||
		!validStateLowerHex(value.LeafName, authority.TokenBytes*2) || value.Anchor.Validate() != nil ||
		value.Mirror == nil || value.Mirror.Validate() != nil {
		return errors.New("Linux cgroup reservation is invalid")
	}
	return nil
}

func (value LinuxCgroupReservation) validateLeafIdentity() error {
	if value.LeafIdentity != nil {
		if !value.CreateStarted || value.LeafIdentity.Validate() != nil ||
			!value.LeafIdentity.Parent.Equal(value.Parent) || value.LeafIdentity.LeafName != value.LeafName {
			return errors.New("Linux cgroup reservation leaf identity is invalid")
		}
	}
	return nil
}

func (value LinuxCgroupReservation) validateHelperIdentity() error {
	if value.Helper != nil && (value.LeafIdentity == nil || value.Helper.Validate() != nil) {
		return errors.New("Linux cgroup helper identity is invalid")
	}
	if value.Helper != nil && value.Helper.Equal(value.Anchor) {
		return errors.New("Linux cgroup helper aliases the anchor process")
	}
	return nil
}

func (value LinuxCgroupReservation) validateLauncher() error {
	if value.Launcher != nil && (value.Helper == nil || value.Launcher.Validate() != nil) {
		return errors.New("Linux cgroup launcher identity is invalid")
	}
	if value.LauncherGrantPossible && value.Launcher == nil {
		return errors.New("Linux cgroup launcher grant has no bound launcher")
	}
	if value.Launcher != nil && (value.Launcher.Equal(value.Anchor) || value.Launcher.Equal(*value.Helper)) {
		return errors.New("Linux cgroup launcher aliases another producer")
	}
	if value.LauncherExit != nil && (value.Launcher == nil ||
		value.Helper == nil || !value.LauncherExit.ValidForOwnedDescendant(*value.Launcher, *value.Helper, value.Anchor, value.HelperExit)) {
		return errors.New("Linux cgroup launcher exit proof is invalid")
	}
	return nil
}

func (value LinuxCgroupReservation) validateGate() error {
	if value.Gate != nil && (!value.LauncherGrantPossible || value.LauncherExit == nil || value.LeafIdentity == nil ||
		value.Gate.Validate() != nil || value.Gate.HostUID != value.OwnerUID || !value.Gate.Cgroup.Equal(*value.LeafIdentity)) {
		return errors.New("Linux cgroup gate identity is invalid")
	}
	if value.Gate != nil && (value.Gate.Process.Equal(value.Anchor) || value.Gate.Process.Equal(*value.Helper) ||
		value.Gate.Process.Equal(*value.Launcher)) {
		return errors.New("Linux cgroup gate aliases another producer")
	}
	return nil
}

func (value LinuxCgroupReservation) validateHelperExitProof() error {
	if value.HelperExit != nil && (value.Helper == nil || !value.HelperExit.ValidFor(*value.Helper, value.Anchor)) {
		return errors.New("Linux cgroup helper exit proof is invalid")
	}
	return nil
}

func (value LinuxCgroupReservation) validateMirrorAliases() error {
	if value.Mirror.Equal(value.Anchor) || (value.Helper != nil && value.Mirror.Equal(*value.Helper)) ||
		(value.Launcher != nil && value.Mirror.Equal(*value.Launcher)) || (value.Gate != nil && value.Mirror.Equal(value.Gate.Process)) {
		return errors.New("Linux cgroup mirror aliases another producer")
	}
	return nil
}

func (value LinuxCgroupReservation) validateProofs() error {
	if value.AbortProof != nil && !linuxCgroupReservationAbortMatches(value, *value.AbortProof) {
		return errors.New("Linux cgroup reservation abort proof is invalid")
	}
	if value.PreGateStopReceipt != nil && !linuxCgroupPreGateReceiptMatchesReservation(value, *value.PreGateStopReceipt) {
		return errors.New("Linux pre-gate cgroup stop receipt is invalid")
	}
	if value.AbortProof != nil && value.PreGateStopReceipt != nil {
		return errors.New("Linux cgroup reservation cannot have both abort and physical-stop proof")
	}
	if value.AbortProof != nil && (value.HelperExit != nil && value.AbortProof.HelperExit == nil ||
		value.LauncherExit != nil && value.AbortProof.LauncherExit == nil) {
		return errors.New("Linux cgroup aborted reservation lost producer-exit evidence")
	}
	return nil
}

func (value LinuxCgroupReservation) validateCreationOrder() error {
	if !value.CreateStarted && (value.LeafIdentity != nil || value.Helper != nil || value.Launcher != nil ||
		value.LauncherExit != nil || value.Gate != nil || value.HelperExit != nil ||
		value.AbortProof != nil && value.AbortProof.LeafDisposition != authority.LinuxCgroupAbortLeafNeverCreated) {
		return errors.New("Linux cgroup reservation has effects before creation intent")
	}
	return nil
}

func (value LinuxCgroupReservation) ValidateWithoutPreGateReceipt() error {
	cloned := value.Clone()
	cloned.PreGateStopReceipt = nil
	return cloned.Validate()
}

func (value LinuxCgroupReservation) Clone() LinuxCgroupReservation {
	cloned := value
	if value.LeafIdentity != nil {
		leaf := *value.LeafIdentity
		cloned.LeafIdentity = &leaf
	}
	if value.Helper != nil {
		helper := *value.Helper
		cloned.Helper = &helper
	}
	if value.Launcher != nil {
		launcher := *value.Launcher
		cloned.Launcher = &launcher
	}
	if value.LauncherExit != nil {
		proof := *value.LauncherExit
		cloned.LauncherExit = &proof
	}
	if value.Gate != nil {
		gate := *value.Gate
		cloned.Gate = &gate
	}
	if value.HelperExit != nil {
		proof := *value.HelperExit
		cloned.HelperExit = &proof
	}
	if value.Mirror != nil {
		mirror := *value.Mirror
		cloned.Mirror = &mirror
	}
	if value.AbortProof != nil {
		proof := value.AbortProof.Clone()
		cloned.AbortProof = &proof
	}
	if value.PreGateStopReceipt != nil {
		receipt := value.PreGateStopReceipt.Clone()
		cloned.PreGateStopReceipt = &receipt
	}
	return cloned
}

func (value LinuxCgroupReservation) Equal(other LinuxCgroupReservation) bool {
	return value.Version == other.Version && value.LaunchToken == other.LaunchToken && value.Secret == other.Secret &&
		value.PipeToken == other.PipeToken && value.JobID == other.JobID && value.RecoveryEndpoint == other.RecoveryEndpoint &&
		value.OwnerUID == other.OwnerUID && value.LauncherUID == other.LauncherUID &&
		value.Parent.Equal(other.Parent) && value.LeafName == other.LeafName && value.CreateStarted == other.CreateStarted &&
		value.LauncherGrantPossible == other.LauncherGrantPossible &&
		equalAuthorityCgroupIdentity(value.LeafIdentity, other.LeafIdentity) &&
		value.Anchor.Equal(other.Anchor) && equalAuthorityProcess(value.Helper, other.Helper) &&
		equalAuthorityProcess(value.Launcher, other.Launcher) && equalAuthorityExit(value.LauncherExit, other.LauncherExit) &&
		equalAuthorityGate(value.Gate, other.Gate) && equalAuthorityExit(value.HelperExit, other.HelperExit) &&
		equalAuthorityProcess(value.Mirror, other.Mirror) && equalAuthorityAbortProof(value.AbortProof, other.AbortProof) &&
		equalAuthorityPreGateReceipt(value.PreGateStopReceipt, other.PreGateStopReceipt)
}

func validStateLowerHex(value string, length int) bool {
	return len(value) == length && value == strings.ToLower(value) && validHex(value)
}

func equalAuthorityCgroupIdentity(left, right *authority.LinuxCgroupIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityProcess(left, right *authority.LinuxCgroupProcessIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityExit(left, right *authority.LinuxCgroupExitProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityGate(left, right *authority.LinuxCgroupGateIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityAbortProof(left, right *authority.LinuxCgroupReservationAbortProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityPreGateReceipt(left, right *authority.LinuxCgroupPreGateStopReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func exactCgroupMutationStage(journal *RunJournal, expected, after LinuxCgroupReservation) (bool, error) {
	if !journal.hasClaimGrant() || journal.LocalState == "cleanup_pending" || journal.ContainmentCgroup == nil ||
		expected.AbortProof != nil || expected.PreGateStopReceipt != nil {
		return false, errors.New("Linux cgroup reservation is not mutable")
	}
	if err := after.Validate(); err != nil {
		return false, err
	}
	current := journal.ContainmentCgroup
	if current.Equal(after) {
		return true, nil
	}
	if current.Equal(expected) {
		return false, nil
	}
	return false, errors.New("Linux cgroup reservation compare-and-set mismatch")
}

func (store *Store) checkLinuxV2NativeAdmissionLocked(
	target RunJournal,
	key RunKey,
	fence protocol.Fence,
	now time.Time,
	liveOwners map[RunKey]protocol.Fence,
	reservation *LinuxCgroupReservation,
	cloneIntent *authority.LinuxCloneIntent,
) error {
	if target.Key() != key || target.Fence() != fence || !target.hasClaimGrant() ||
		!validLinuxV2AdmissionFence(key, fence) || now.IsZero() {
		return errors.New("Linux v2 native admission target RunKey/fence is stale")
	}
	if !linuxV2TargetCanStartNativeEffects(target, now) {
		return errors.New("Linux v2 native admission target lease or lifecycle is not effect-ready")
	}
	return store.scanLinuxV2NativeAdmissionLocked(&key, fence, now, liveOwners, reservation, cloneIntent)
}

func linuxV2TargetCanStartNativeEffects(journal RunJournal, now time.Time) bool {
	if !journal.hasClaimGrant() || !journal.LeaseExpiresAt.After(now) || journal.NativeUsageRecoveryRequired ||
		journal.ContainmentUnproven || journal.TerminalState != "" || journal.TerminalVerdict != "" ||
		hasPendingTerminalTransition(journal.PendingTransitions) || journal.LinuxCloneAbortPending ||
		journal.LinuxCloneAbortProof != nil || journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent != nil ||
		journal.LinuxCloneTargetRemovalProof != nil || journal.LinuxCloneCleanupProof != nil ||
		journal.LinuxCloneReuseRetired || journal.LinuxCloneReuseAbortedBeforeNativeStart {
		return false
	}
	switch journal.LocalState {
	case "claimed", "running":
		return true
	default:
		return false
	}
}

func (store *Store) scanLinuxV2NativeAdmissionLocked(
	targetKey *RunKey,
	targetFence protocol.Fence,
	now time.Time,
	liveOwners map[RunKey]protocol.Fence,
	reservation *LinuxCgroupReservation,
	cloneIntent *authority.LinuxCloneIntent,
) error {
	if now.IsZero() {
		return errors.New("Linux v2 admission clock is missing")
	}
	for candidateKey, candidateFence := range liveOwners {
		if !validLinuxV2AdmissionFence(candidateKey, candidateFence) ||
			targetKey != nil && candidateKey == *targetKey && candidateFence != targetFence {
			return errors.New("Linux v2 live-owner snapshot contains an invalid RunKey/fence")
		}
	}
	matchedLiveOwners := 0
	targetFound := targetKey == nil
	err := store.forEachJournalLocked(func(foreign RunJournal) error {
		foreignKey := foreign.Key()
		if targetKey != nil && foreignKey == *targetKey {
			targetFound = foreign.Fence() == targetFence
			if !targetFound {
				return errors.New("Linux v2 admission target changed fence during Store scan")
			}
			if _, hasLiveOwner := liveOwners[foreignKey]; hasLiveOwner {
				matchedLiveOwners++
			}
			return nil
		}
		liveFence, hasLiveOwner := liveOwners[foreignKey]
		if hasLiveOwner {
			if liveFence != foreign.Fence() || !linuxV2ForeignOwnerIsHealthy(foreign, now) {
				return fmt.Errorf("foreign Linux v2 live-owner snapshot is stale or unhealthy for %s/%d", foreignKey.RunID, foreignKey.Generation)
			}
			matchedLiveOwners++
		} else if !linuxV2ForeignJournalIsSafelyRetired(foreign) {
			return fmt.Errorf("foreign RunJournal %s/%d has unresolved native ownership", foreignKey.RunID, foreignKey.Generation)
		}
		if cloneIntent != nil && foreign.LinuxCloneIntent != nil &&
			!(foreign.LinuxCloneAbortProof != nil && !foreign.LinuxCloneAbortPending) {
			other := foreign.LinuxCloneIntent
			if linuxClonePathAliases(cloneIntent.SeedPath, other.SeedPath) ||
				linuxClonePathAliases(cloneIntent.Target, other.Target) ||
				linuxClonePathAliases(cloneIntent.SeedPath, other.Target) ||
				linuxClonePathAliases(cloneIntent.Target, other.SeedPath) {
				return errors.New("Linux clone seed or target path is already reserved")
			}
		}
		if reservation != nil {
			if other := foreign.ContainmentCgroup; other != nil {
				if other.Parent.Path == reservation.Parent.Path && other.LeafName == reservation.LeafName {
					return errors.New("Linux cgroup leaf path is already reserved")
				}
			} else if linuxV2RetainedReleaseNamesLeaf(foreign, *reservation) {
				return errors.New("Linux cgroup leaf path is already reserved")
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan State journals before Linux v2 native admission: %w", err)
	}
	if !targetFound {
		return errors.New("Linux v2 admission target journal is missing from the locked Store scan")
	}
	if matchedLiveOwners != len(liveOwners) {
		return errors.New("Linux v2 live-owner snapshot has no current RunJournal for every fenced owner")
	}
	return nil
}

func validLinuxV2AdmissionFence(key RunKey, fence protocol.Fence) bool {
	return validateKey(key) == nil && fence.Generation == key.Generation &&
		validRequiredString(fence.RuntimeID, 4096) && fence.RuntimeEpoch > 0 &&
		validRequiredString(fence.ClaimID, 4096) && validRequiredString(fence.LeaseToken, 65536)
}

// linuxV2RetainedReleaseNamesLeaf keeps a retained pre-checkout clone's leaf name reserved:
// its release proof still names the parent path and leaf a new reservation must not reuse.
func linuxV2RetainedReleaseNamesLeaf(journal RunJournal, reservation LinuxCgroupReservation) bool {
	proof := journal.ContainmentCgroupReleaseProof
	return proof != nil && proof.CgroupIdentity != nil && LinuxV2ReleasedCloneRetainedAfterRelease(journal) &&
		proof.CgroupIdentity.Parent.Path == reservation.Parent.Path &&
		proof.CgroupIdentity.LeafName == reservation.LeafName
}

func linuxV2ForeignJournalIsSafelyRetired(journal RunJournal) bool {
	noActiveMarkers := !journal.HasProcessDetails() && journal.ContainmentAuthority == nil &&
		journal.ContainmentHandoff == nil && journal.ContainmentScans == nil && !journal.ContainmentUnproven
	if journal.LinuxCloneReuseAbortedBeforeNativeStart {
		return noActiveMarkers && journal.LinuxCloneReuseGrant != nil && journal.LinuxCloneReuseRetired &&
			journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
			journal.ContainmentCgroupPreGateReleaseProof == nil && !journal.LinuxCloneAbortPending &&
			journal.LinuxCloneAbortProof == nil && !journal.NativeUsageRecoveryRequired &&
			linuxV2ReleasedCloneStateSafe(journal)
	}
	if release := journal.ContainmentCgroupReleaseProof; release != nil {
		return noActiveMarkers && journal.ContainmentCgroup == nil && journal.ContainmentCgroupPreGateReleaseProof == nil &&
			release.Validate() == nil && release.Version == authority.SupervisorHandoffVersionV2 && release.StopReceipt != nil &&
			(release.LeafDisposition == authority.LinuxCgroupLeafRemoved || release.LeafDisposition == authority.LinuxCgroupLeafAbsentAfterReceipt) &&
			linuxV2ReleasedCloneStateSafe(journal)
	}
	if release := journal.ContainmentCgroupPreGateReleaseProof; release != nil {
		return noActiveMarkers && journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
			release.Validate() == nil && release.Version == authority.SupervisorHandoffVersionV2 &&
			release.Receipt.Proof.GateOutcome.Outcome == authority.LinuxCgroupGateOutcomeNeverCreated
	}
	if reservation := journal.ContainmentCgroup; reservation != nil && reservation.AbortProof != nil {
		return noActiveMarkers && journal.ContainmentCgroupReleaseProof == nil && journal.ContainmentCgroupPreGateReleaseProof == nil &&
			reservation.Validate() == nil && linuxCgroupReservationAbortMatches(*reservation, *reservation.AbortProof)
	}
	if proof := journal.LinuxCloneAbortProof; proof != nil {
		return noActiveMarkers && proof.Validate() == nil && linuxCloneAbortHistoryMatches(journal, proof)
	}
	if owner := journal.ContainmentAuthority; owner != nil && owner.Version == authority.SupervisorVersion && owner.StopReceipt != nil {
		return linuxV1StopReceiptSafelyRetires(journal, *owner)
	}
	return false
}

func linuxV1StopReceiptSafelyRetires(journal RunJournal, owner authority.Supervisor) bool {
	switch journal.LocalState {
	case "terminal_pending", "cleanup_pending", "stale":
	default:
		return false
	}
	return owner.Validate() == nil && owner.Version == authority.SupervisorVersion && owner.StopReceipt != nil &&
		owner.StopReceipt.ValidFor(owner) && journal.HasProcessDetails() && journal.PID == owner.TargetPID &&
		journal.ProcessIdentity == owner.TargetIdentity && journal.ContainmentHandoff == nil &&
		journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
		journal.ContainmentCgroupPreGateReleaseProof == nil && !journal.ContainmentUnproven &&
		!journal.NativeUsageRecoveryRequired && journal.requireLinuxScansSealedForAuthority(owner) == nil &&
		!linuxV2JournalHasPendingEffects(journal)
}

func linuxV2JournalHasPendingEffects(journal RunJournal) bool {
	return journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil ||
		journal.ContainmentCgroupPreGateReleaseProof != nil || journal.LinuxCloneIntent != nil || journal.LinuxCloneSeedReceipt != nil ||
		journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxCloneReuseGrant != nil ||
		journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil ||
		journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil || journal.LinuxCloneCleanupPending ||
		journal.LinuxCloneCleanupIntent != nil || journal.LinuxCloneTargetRemovalProof != nil || journal.LinuxCloneCleanupProof != nil
}

func linuxV2ForeignOwnerIsHealthy(journal RunJournal, now time.Time) bool {
	reservation := journal.ContainmentCgroup
	owner := journal.ContainmentAuthority
	switch journal.LocalState {
	case "claimed", "running", "paused", "waiting_for_input":
	default:
		return false
	}
	return reservation != nil && owner != nil && owner.Version == authority.SupervisorVersionV2 && owner.StopReceipt == nil &&
		reservation.Validate() == nil && reservation.AbortProof == nil && reservation.PreGateStopReceipt == nil &&
		linuxCgroupReservationMatchesAuthority(*reservation, *owner) && journal.ContainmentHandoff == nil &&
		journal.ContainmentCgroupReleaseProof == nil && journal.ContainmentCgroupPreGateReleaseProof == nil &&
		journal.ContainmentScans == nil && !journal.ContainmentUnproven && !journal.LinuxCloneAbortPending &&
		journal.LinuxCloneAbortProof == nil && !journal.LinuxCloneCleanupPending && journal.LinuxCloneCleanupIntent == nil &&
		journal.LinuxCloneTargetRemovalProof == nil && journal.LinuxCloneCleanupProof == nil && journal.HasProcessDetails() &&
		journal.PID == owner.TargetPID && journal.ProcessIdentity == owner.TargetIdentity &&
		journal.TerminalState == "" && journal.TerminalVerdict == "" && !journal.NativeUsageRecoveryRequired &&
		journal.LeaseExpiresAt.After(now) && journal.hasClaimGrant()
}

// CheckLinuxV2NativeAdmissionBeforeClaim is a read-only dispatch gate for a
// fresh native assignment, before Control has issued the target RunKey/fence.
// It never authorizes launch; the claimed-key checks and reservation CAS are
// still mandatory before seed, cgroup, or producer effects.
func (store *Store) CheckLinuxV2NativeAdmissionBeforeClaim(now time.Time, liveOwners map[RunKey]protocol.Fence) error {
	if err := requireLinuxV2NativeAdmissionPlatform(); err != nil {
		return err
	}
	if now.IsZero() {
		return errors.New("Linux v2 native admission time is missing")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	return store.scanLinuxV2NativeAdmissionLocked(nil, protocol.Fence{}, now, liveOwners, nil, nil)
}

// CheckLinuxV2NativeAdmission performs the post-claim read-only admission scan.
// The live-owner map is an App snapshot; State verifies every full fence against
// the current journals while holding the Store lock.
func (store *Store) CheckLinuxV2NativeAdmission(key RunKey, fence protocol.Fence, now time.Time, liveOwners map[RunKey]protocol.Fence) error {
	if err := requireLinuxV2NativeAdmissionPlatform(); err != nil {
		return err
	}
	if err := validateKey(key); err != nil || !validLinuxV2AdmissionFence(key, fence) || now.IsZero() {
		return errors.New("Linux v2 native admission RunKey, fence, or time is invalid")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	journal, err := store.loadJournalLocked(key)
	if err != nil {
		return err
	}
	return store.checkLinuxV2NativeAdmissionLocked(journal, key, fence, now, liveOwners, nil, nil)
}

// ReserveLinuxCgroup durably reserves the immutable run leaf before any mkdir
// or producer launch. Exact retries write the same journal again, including a
// fresh filesystem persistence barrier after an unknown earlier result, only
// while the target Control lease and lifecycle remain effect-ready. The
// caller's current fence and App-attested foreign owners are checked atomically
// with the cross-RunKey journal scan and this reservation CAS.
func (store *Store) ReserveLinuxCgroup(key RunKey, fence protocol.Fence, reservation LinuxCgroupReservation, liveOwners map[RunKey]protocol.Fence) (RunJournal, error) {
	if err := requireLinuxV2NativeAdmissionPlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := reservation.Validate(); err != nil {
		return RunJournal{}, err
	}
	if reservation.CreateStarted || reservation.LeafIdentity != nil || reservation.Helper != nil ||
		reservation.Launcher != nil || reservation.LauncherExit != nil || reservation.Gate != nil ||
		reservation.HelperExit != nil || reservation.AbortProof != nil || reservation.PreGateStopReceipt != nil ||
		reservation.Mirror == nil {
		return RunJournal{}, errors.New("Linux cgroup reservation must precede creation")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.Fence() != fence || !validLinuxV2AdmissionFence(key, fence) {
			return errors.New("Linux cgroup reservation RunKey/fence is stale")
		}
		if err := store.checkLinuxV2NativeAdmissionLocked(*journal, key, fence, time.Now(), liveOwners, &reservation, nil); err != nil {
			return err
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("Linux cgroup reservation is fenced by clone abort")
		}
		if journal.LocalState == "cleanup_pending" || journal.HasProcessDetails() ||
			journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
			journal.ContainmentScans != nil || journal.ContainmentUnproven || journal.ContainmentCgroupReleaseProof != nil ||
			journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("Linux cgroup reservation conflicts with existing containment")
		}
		if journal.ContainmentCgroup != nil {
			if journal.ContainmentCgroup.Equal(reservation) {
				return nil
			}
			return errors.New("Linux cgroup reservation conflicts with journal")
		}
		cloned := reservation.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// MarkLinuxCgroupCreateStarted commits the before-mkdir creation intent.
// Replaying its exact pre-state after an unknown write performs another fsync
// barrier only while the target Control lease and lifecycle remain effect-ready;
// no later stage can be inferred from the flag alone. The reserved daemon mirror
// identity remains present here; it is not a cgroup producer grant and does not
// make the leaf exist.
func (store *Store) MarkLinuxCgroupCreateStarted(key RunKey, fence protocol.Fence, expected LinuxCgroupReservation, liveOwners map[RunKey]protocol.Fence) (RunJournal, error) {
	if err := requireLinuxV2NativeAdmissionPlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.CreateStarted || expected.LeafIdentity != nil || expected.Helper != nil ||
		expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil ||
		expected.HelperExit != nil {
		return RunJournal{}, errors.New("Linux cgroup creation intent expected value is invalid")
	}
	staleFence := errors.New("Linux cgroup creation RunKey/fence is stale")
	return store.mutateJournalCAS(key, fence, staleFence, func(journal *RunJournal) error {
		if !validLinuxV2AdmissionFence(key, fence) {
			return staleFence
		}
		if err := store.checkLinuxV2NativeAdmissionLocked(*journal, key, fence, time.Now(), liveOwners, nil, nil); err != nil {
			return err
		}
		after := expected.Clone()
		after.CreateStarted = true
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// BindLinuxCgroupLeaf records the first measured leaf object identity after a
// successful durable creation intent. It never adopts an already-bound or
// path-reopened leaf with a different object identity. The caller must invoke
// it only while the measured directory is still pinned through uninterrupted
// A-owned FD provenance; matching a path/stat after all pins are lost is not
// evidence that this reservation created that object.
func (store *Store) BindLinuxCgroupLeaf(key RunKey, expected LinuxCgroupReservation, measured authority.LinuxCgroupIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := measured.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !expected.CreateStarted || measured.LeafName != expected.LeafName || !measured.Parent.Equal(expected.Parent) {
		return RunJournal{}, errors.New("measured Linux cgroup leaf does not match reservation")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.LeafIdentity != nil || expected.Helper != nil || expected.Launcher != nil || expected.Gate != nil {
			return errors.New("Linux cgroup leaf must bind before its producers")
		}
		leaf := measured
		after := expected.Clone()
		after.LeafIdentity = &leaf
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// BindLinuxCgroupHelper records the exact UID-A helper identity after the
// daemon and helper have independently pinned the measured leaf.
func (store *Store) BindLinuxCgroupHelper(key RunKey, expected LinuxCgroupReservation, helper authority.LinuxCgroupProcessIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := helper.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.LeafIdentity == nil || expected.Helper != nil || expected.Launcher != nil || expected.Gate != nil ||
			expected.AbortProof != nil || expected.PreGateStopReceipt != nil || journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil {
			return errors.New("Linux cgroup helper bind is out of order")
		}
		bound := helper
		after := expected.Clone()
		after.Helper = &bound
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// BindLinuxCgroupLauncher records the exact root-launcher process before that
// producer can receive or act on a run grant.
func (store *Store) BindLinuxCgroupLauncher(key RunKey, expected LinuxCgroupReservation, launcher authority.LinuxCgroupProcessIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := launcher.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.LeafIdentity == nil || expected.Helper == nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil ||
			journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil {
			return errors.New("Linux cgroup launcher bind is out of order")
		}
		bound := launcher
		after := expected.Clone()
		after.Launcher = &bound
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// ArmLinuxCgroupLauncherGrant durably records that an external grant may now
// reach the exact bound launcher. Callers must replay this exact CAS and obtain
// a successful persistence barrier before sending the grant; expired or
// otherwise effect-ineligible targets cannot use replay to send it. After this
// point the no-target abort path is closed even if the launcher later exits.
func (store *Store) ArmLinuxCgroupLauncherGrant(key RunKey, fence protocol.Fence, expected LinuxCgroupReservation, liveOwners map[RunKey]protocol.Fence) (RunJournal, error) {
	if err := requireLinuxV2NativeAdmissionPlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Launcher == nil || expected.LauncherGrantPossible || expected.LauncherExit != nil || expected.Gate != nil ||
		expected.AbortProof != nil {
		return RunJournal{}, errors.New("Linux cgroup launcher grant expected value is invalid")
	}
	staleFence := errors.New("Linux cgroup launcher grant RunKey/fence is stale")
	return store.mutateJournalCAS(key, fence, staleFence, func(journal *RunJournal) error {
		if !validLinuxV2AdmissionFence(key, fence) {
			return staleFence
		}
		if err := store.checkLinuxV2NativeAdmissionLocked(*journal, key, fence, time.Now(), liveOwners, nil, nil); err != nil {
			return err
		}
		if journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil || journal.ContainmentCgroup == nil {
			return errors.New("Linux cgroup launcher grant has no pending reservation")
		}
		after := expected.Clone()
		after.LauncherGrantPossible = true
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// RecordLinuxCgroupLauncherExit persists the anchor's exact wait/reap witness
// before gate binding or handoff preparation may advance.
func (store *Store) RecordLinuxCgroupLauncherExit(key RunKey, expected LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.Launcher == nil || expected.Helper == nil || expected.LauncherExit != nil ||
			!proof.ValidForOwnedDescendant(*expected.Launcher, *expected.Helper, expected.Anchor, expected.HelperExit) {
			return errors.New("Linux cgroup launcher exit proof does not match producer")
		}
		copy := proof
		after := expected.Clone()
		after.LauncherExit = &copy
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// BindLinuxCgroupGate records the trusted A gate born in the bound leaf after
// the exact root-launcher wait has been durably acknowledged.
func (store *Store) BindLinuxCgroupGate(key RunKey, expected LinuxCgroupReservation, gate authority.LinuxCgroupGateIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := gate.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.LeafIdentity == nil || expected.Helper == nil ||
			expected.LauncherExit == nil || expected.Gate != nil ||
			!gate.Cgroup.Equal(*expected.LeafIdentity) ||
			journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil {
			return errors.New("Linux cgroup gate bind is out of order")
		}
		copy := gate
		after := expected.Clone()
		after.Gate = &copy
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// RecordLinuxCgroupHelperExit stores the anchor's exact helper wait/reap proof
// for a permitted mirror-takeover receipt. It does not itself prove cgroup stop.
func (store *Store) RecordLinuxCgroupHelperExit(key RunKey, expected LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.StopReceipt != nil ||
			journal.ContainmentAuthority != nil && journal.ContainmentAuthority.StopReceipt != nil {
			return errors.New("Linux cgroup helper exit cannot follow a durable stop receipt")
		}
		if expected.Helper == nil || expected.HelperExit != nil || !proof.ValidFor(*expected.Helper, expected.Anchor) {
			return errors.New("Linux cgroup helper exit proof does not match producer")
		}
		copy := proof
		after := expected.Clone()
		after.HelperExit = &copy
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// AbortLinuxCgroupReservation records a replayable no-target abort only after
// an independent caller proves the exact parent unchanged, every producer
// fenced, no launcher grant possible and either never-created or exact empty
// leaf removal. It never creates a stop receipt. The tombstone is not pending
// containment but remains until journal deletion so an unknown write can be
// replayed with the same proof and a fresh persistence barrier.
func (store *Store) AbortLinuxCgroupReservation(key RunKey, expected LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !proof.ValidFor(expected.LaunchToken, expected.Parent, expected.LeafName, expected.CreateStarted,
		expected.OwnerUID, expected.Anchor, *expected.Mirror, expected.Helper, expected.Launcher) ||
		expected.AbortProof != nil || expected.PreGateStopReceipt != nil || expected.Gate != nil {
		return RunJournal{}, errors.New("Linux cgroup reservation abort proof does not match a pre-effect owner")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return applyLinuxCgroupReservationAbort(journal, expected, proof)
	})
}

// AbortNeverCreatedLinuxCgroupReservation records a no-target tombstone when
// the durable before-mkdir CAS was never advanced. The exclusive open Store
// lock and exact RunKey/fence prove that no compliant creator could have
// proceeded past CreateStarted=false; this is not a filesystem observation,
// physical-stop receipt, or authority to remove a cgroup leaf.
func (store *Store) AbortNeverCreatedLinuxCgroupReservation(key RunKey, fence protocol.Fence, expected LinuxCgroupReservation) (RunJournal, error) {
	if err := expected.Validate(); err != nil || expected.CreateStarted || expected.LeafIdentity != nil ||
		expected.Helper != nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil ||
		expected.HelperExit != nil || expected.AbortProof != nil || expected.PreGateStopReceipt != nil ||
		expected.LauncherGrantPossible || expected.Mirror == nil {
		return RunJournal{}, errors.New("Linux cgroup reservation is not a never-created state")
	}
	proof := authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: expected.LaunchToken,
		Parent: expected.Parent, LeafName: expected.LeafName, CreateStarted: false,
		LeafDisposition: authority.LinuxCgroupAbortLeafNeverCreated, OwnerUID: expected.OwnerUID,
		Anchor: expected.Anchor, Mirror: *expected.Mirror, Witness: expected.Anchor,
		CreatorFenced: true, NoProducerCanAct: true, ParentUnchanged: true, LeafAbsent: true,
		LeafNeverAppeared: false,
	}
	if err := proof.Validate(); err != nil || !linuxCgroupReservationAbortMatches(expected, proof) {
		return RunJournal{}, errors.New("Linux cgroup never-created tombstone is invalid")
	}
	staleFence := errors.New("Linux cgroup never-created abort has a stale RunKey fence")
	return store.mutateJournalCAS(key, fence, staleFence, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return staleFence
		}
		return applyLinuxCgroupReservationAbort(journal, expected, proof)
	})
}

func applyLinuxCgroupReservationAbort(journal *RunJournal, expected LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) error {
	if journal.ContainmentCgroup == nil {
		return errors.New("Linux cgroup reservation abort compare-and-set mismatch")
	}
	current := journal.ContainmentCgroup
	if current.AbortProof != nil {
		previous := current.Clone()
		previous.AbortProof = nil
		if previous.Equal(expected) && current.AbortProof.Equal(proof) &&
			journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil &&
			journal.ContainmentScans == nil && !journal.HasProcessDetails() && !journal.ContainmentUnproven {
			return nil
		}
		return errors.New("Linux cgroup reservation abort replay conflicts with journal")
	}
	if !current.Equal(expected) || journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil ||
		journal.ContainmentScans != nil || journal.HasProcessDetails() || journal.ContainmentUnproven {
		return errors.New("Linux cgroup reservation abort compare-and-set mismatch")
	}
	if !linuxCgroupReservationAbortMatches(*current, proof) {
		return errors.New("Linux cgroup reservation abort has a live or unidentified producer")
	}
	copy := proof.Clone()
	current.AbortProof = &copy
	return nil
}

func requireLinuxV2NativeAdmissionPlatform() error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("Linux v2 native admission is supported only on Linux/amd64")
	}
	return nil
}

func sameContainmentCgroup(left, right *LinuxCgroupReservation) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameContainmentCgroupReleaseProof(left, right *authority.SupervisorHandoffReleaseProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameContainmentPreGateReleaseProof(left, right *authority.LinuxCgroupPreGateReleaseProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func cloneStateCgroupIdentity(value *authority.LinuxCgroupIdentity) *authority.LinuxCgroupIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStateStopProof(value *authority.LinuxCgroupStopProof) *authority.LinuxCgroupStopProof {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.LauncherExit.HelperExit = cloneJournalExitProof(value.LauncherExit.HelperExit)
	cloned.GateExit.HelperExit = cloneJournalExitProof(value.GateExit.HelperExit)
	cloned.HelperExit = cloneJournalExitProof(value.HelperExit)
	return &cloned
}

func cloneJournalExitProof(value *authority.LinuxCgroupExitProof) *authority.LinuxCgroupExitProof {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.HelperExit = cloneJournalExitProof(value.HelperExit)
	return &cloned
}

func linuxCgroupReservationMatchesHandoff(reservation LinuxCgroupReservation, handoff authority.SupervisorHandoff, requireBound bool) bool {
	if reservation.Validate() != nil || reservation.PreGateStopReceipt != nil || handoff.Version != authority.SupervisorHandoffVersionV2 ||
		reservation.LeafIdentity == nil || reservation.Helper == nil || reservation.LauncherExit == nil || reservation.Gate == nil ||
		!reservation.LauncherGrantPossible ||
		handoff.LaunchToken != reservation.LaunchToken || handoff.Secret != reservation.Secret ||
		handoff.PipeToken != reservation.PipeToken || handoff.JobID != reservation.JobID ||
		handoff.OwnerKind != authority.OwnerKindLinuxHelper || handoff.OwnerContext != reservation.RecoveryEndpoint ||
		handoff.OwnerUID != reservation.OwnerUID || handoff.LauncherUID != reservation.LauncherUID ||
		handoff.TargetBirthKind != reservation.Gate.BirthKind || handoff.TargetPID != reservation.Gate.Process.PID ||
		handoff.TargetIdentity != reservation.Gate.Process.Identity || handoff.CgroupIdentity == nil ||
		!handoff.CgroupIdentity.Equal(*reservation.LeafIdentity) || handoff.AnchorPID != reservation.Anchor.PID ||
		handoff.AnchorIdentity != reservation.Anchor.Identity || handoff.LauncherPID != reservation.Launcher.PID ||
		handoff.LauncherIdentity != reservation.Launcher.Identity || handoff.LauncherExit == nil ||
		!handoff.LauncherExit.Equal(*reservation.LauncherExit) {
		return false
	}
	if handoff.SupervisorPID == 0 {
		return !requireBound && handoff.SupervisorIdentity == "" && handoff.Mirror == nil && reservation.Mirror != nil
	}
	return requireBound && handoff.SupervisorPID == reservation.Helper.PID &&
		handoff.SupervisorIdentity == reservation.Helper.Identity && reservation.Mirror != nil &&
		equalAuthorityProcess(handoff.Mirror, reservation.Mirror)
}

func linuxCgroupReservationMatchesAuthority(reservation LinuxCgroupReservation, value authority.Supervisor) bool {
	return reservation.Validate() == nil && reservation.AbortProof == nil && reservation.PreGateStopReceipt == nil &&
		reservation.LauncherGrantPossible && value.Version == authority.SupervisorVersionV2 &&
		reservation.LeafIdentity != nil && reservation.Helper != nil && reservation.Launcher != nil &&
		reservation.LauncherExit != nil && reservation.Gate != nil && reservation.Mirror != nil &&
		value.LaunchToken == reservation.LaunchToken && value.Secret == reservation.Secret &&
		value.PipeToken == reservation.PipeToken && value.JobID == reservation.JobID &&
		value.OwnerKind == authority.OwnerKindLinuxHelper && value.OwnerContext == reservation.RecoveryEndpoint &&
		value.OwnerUID == reservation.OwnerUID && value.LauncherUID == reservation.LauncherUID &&
		value.TargetBirthKind == reservation.Gate.BirthKind && value.TargetPID == reservation.Gate.Process.PID &&
		value.TargetIdentity == reservation.Gate.Process.Identity && value.CgroupIdentity != nil &&
		value.CgroupIdentity.Equal(*reservation.LeafIdentity) && value.AnchorPID == reservation.Anchor.PID &&
		value.AnchorIdentity == reservation.Anchor.Identity && value.LauncherPID == reservation.Launcher.PID &&
		value.LauncherIdentity == reservation.Launcher.Identity && value.LauncherExit != nil &&
		value.LauncherExit.Equal(*reservation.LauncherExit) && value.SupervisorPID == reservation.Helper.PID &&
		value.SupervisorIdentity == reservation.Helper.Identity && equalAuthorityProcess(value.Mirror, reservation.Mirror)
}

func linuxCgroupReceiptMatchesReservation(reservation LinuxCgroupReservation, receipt authority.StopReceipt) bool {
	if reservation.Validate() != nil || reservation.AbortProof != nil || reservation.PreGateStopReceipt != nil ||
		!reservation.LauncherGrantPossible || reservation.LeafIdentity == nil || reservation.Helper == nil ||
		reservation.Launcher == nil || reservation.LauncherExit == nil || reservation.Gate == nil || reservation.Mirror == nil {
		return false
	}
	value := authority.Supervisor{
		Version:            authority.SupervisorVersionV2,
		Secret:             reservation.Secret,
		LaunchToken:        reservation.LaunchToken,
		OwnerKind:          authority.OwnerKindLinuxHelper,
		OwnerContext:       reservation.RecoveryEndpoint,
		TargetPID:          reservation.Gate.Process.PID,
		TargetIdentity:     reservation.Gate.Process.Identity,
		OwnerUID:           reservation.OwnerUID,
		LauncherUID:        reservation.LauncherUID,
		PipeToken:          reservation.PipeToken,
		JobID:              reservation.JobID,
		SupervisorPID:      reservation.Helper.PID,
		SupervisorIdentity: reservation.Helper.Identity,
		TargetBirthKind:    reservation.Gate.BirthKind,
		CgroupIdentity:     reservation.LeafIdentity,
		AnchorPID:          reservation.Anchor.PID,
		AnchorIdentity:     reservation.Anchor.Identity,
		LauncherPID:        reservation.Launcher.PID,
		LauncherIdentity:   reservation.Launcher.Identity,
		LauncherExit:       reservation.LauncherExit,
		Mirror:             reservation.Mirror,
	}
	if value.Validate() != nil || !receipt.ValidFor(value) || receipt.LinuxProof == nil {
		return false
	}
	if receipt.LinuxProof.Kind == authority.LinuxCgroupStopProofMirror {
		return reservation.HelperExit != nil && receipt.LinuxProof.HelperExit != nil &&
			receipt.LinuxProof.HelperExit.Equal(*reservation.HelperExit)
	}
	return receipt.LinuxProof.Kind == authority.LinuxCgroupStopProofHelper && reservation.HelperExit == nil
}

func linuxCgroupReservationAbortMatches(reservation LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) bool {
	if reservation.Version != LinuxCgroupReservationVersion || proof.Validate() != nil || reservation.LauncherGrantPossible ||
		!proof.ValidFor(reservation.LaunchToken, reservation.Parent, reservation.LeafName, reservation.CreateStarted,
			reservation.OwnerUID, reservation.Anchor, *reservation.Mirror, reservation.Helper, reservation.Launcher) ||
		reservation.AbortProof != nil && !reservation.AbortProof.Equal(proof) ||
		reservation.Gate != nil || reservation.PreGateStopReceipt != nil {
		return false
	}
	switch proof.LeafDisposition {
	case authority.LinuxCgroupAbortLeafNeverCreated:
		return reservation.LeafIdentity == nil && reservation.Helper == nil && reservation.HelperExit == nil &&
			reservation.Launcher == nil && reservation.LauncherExit == nil && proof.LeafIdentity == nil &&
			proof.LauncherExit == nil && !proof.LauncherGrantPossible
	case authority.LinuxCgroupAbortLeafRemovedEmpty, authority.LinuxCgroupAbortLeafAbsentAfterRmdir:
		if reservation.LeafIdentity == nil || proof.LeafIdentity == nil || !reservation.LeafIdentity.Equal(*proof.LeafIdentity) ||
			!proof.LeafEmpty || !proof.LeafAbsent || proof.LauncherGrantPossible {
			return false
		}
		if reservation.Launcher == nil {
			if reservation.LauncherExit != nil || proof.LauncherExit != nil {
				return false
			}
		} else if reservation.LauncherExit == nil || proof.LauncherExit == nil ||
			!reservation.LauncherExit.Equal(*proof.LauncherExit) ||
			!proof.LauncherExit.ValidForOwnedDescendant(*reservation.Launcher, *reservation.Helper, reservation.Anchor, reservation.HelperExit) {
			return false
		}
		if reservation.Helper == nil {
			return reservation.HelperExit == nil && proof.HelperExit == nil
		}
		return reservation.HelperExit != nil && proof.HelperExit != nil &&
			reservation.HelperExit.Equal(*proof.HelperExit) &&
			proof.HelperExit.ValidFor(*reservation.Helper, reservation.Anchor)
	default:
		return false
	}
}

func linuxCgroupPreGateReceiptMatchesReservation(reservation LinuxCgroupReservation, receipt authority.LinuxCgroupPreGateStopReceipt) bool {
	return reservation.Version == LinuxCgroupReservationVersion && reservation.ValidateWithoutPreGateReceipt() == nil &&
		reservation.CreateStarted && reservation.LauncherGrantPossible && reservation.LeafIdentity != nil &&
		reservation.Helper != nil && reservation.Launcher != nil && reservation.LauncherExit != nil &&
		reservation.Mirror != nil && reservation.Gate == nil && reservation.AbortProof == nil &&
		(reservation.PreGateStopReceipt == nil || reservation.PreGateStopReceipt.Equal(receipt)) &&
		receipt.ValidFor(reservation.LaunchToken, *reservation.LeafIdentity, reservation.OwnerUID, reservation.LauncherUID,
			reservation.Anchor, *reservation.Helper, *reservation.Launcher, *reservation.Mirror, *reservation.LauncherExit, reservation.HelperExit)
}
