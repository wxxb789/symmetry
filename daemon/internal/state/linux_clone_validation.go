package state

import (
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func linuxV2ReleasedCloneStateSafe(journal RunJournal) bool {
	if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil || journal.LinuxCloneCleanupPending {
		return false
	}
	if reuse := journal.LinuxCloneReuseGrant; reuse != nil {
		return reuse.Validate() == nil && reuse.RunID == journal.RunID && reuse.Generation == journal.Generation &&
			journal.LinuxCloneIntent == nil && journal.LinuxCloneSeedReceipt == nil &&
			journal.LinuxCloneCheckoutReceipt == nil && journal.LinuxClonePromotionIntent == nil &&
			journal.LinuxClonePromotionReceipt == nil && journal.LinuxCloneCleanupIntent == nil &&
			journal.LinuxCloneTargetRemovalProof == nil && journal.LinuxCloneCleanupProof == nil &&
			journal.WorkspacePath == reuse.SourceCheckout.Target &&
			journal.WorkspaceBindingKey == reuse.Source.Intent.BindingKey &&
			journal.WorkspaceRecoveryRequired && journal.RetainWorkspace
	}
	if journal.LinuxCloneIntent == nil {
		return journal.LinuxCloneSeedReceipt == nil && journal.LinuxCloneCheckoutReceipt == nil &&
			journal.LinuxClonePromotionIntent == nil && journal.LinuxClonePromotionReceipt == nil &&
			journal.LinuxCloneCleanupIntent == nil && journal.LinuxCloneTargetRemovalProof == nil &&
			journal.LinuxCloneCleanupProof == nil && journal.WorkspacePath == "" &&
			!journal.WorkspaceRecoveryRequired && !journal.RetainWorkspace
	}
	if journal.LinuxCloneIntent.ValidateForJournal() != nil {
		return false
	}
	if journal.LinuxCloneCleanupProof != nil {
		return journal.LinuxCloneCleanupIntent != nil && journal.LinuxCloneTargetRemovalProof != nil &&
			journal.LinuxCloneCleanupIntent.Validate() == nil && journal.LinuxCloneTargetRemovalProof.Validate() == nil &&
			journal.LinuxCloneCleanupProof.Validate() == nil &&
			journal.LinuxCloneCleanupIntent.Intent.Equal(*journal.LinuxCloneIntent) &&
			journal.LinuxCloneTargetRemovalProof.Intent.Equal(*journal.LinuxCloneCleanupIntent) &&
			journal.LinuxCloneCleanupProof.Intent.Equal(*journal.LinuxCloneCleanupIntent) &&
			journal.LinuxCloneCleanupProof.TargetRemovalProof.Equal(*journal.LinuxCloneTargetRemovalProof) &&
			linuxCloneCleanupIntentMatchesJournal(journal, *journal.LinuxCloneCleanupIntent) &&
			journal.WorkspacePath == "" && !journal.WorkspaceRecoveryRequired && !journal.RetainWorkspace
	}
	if linuxV2ReleasedCloneRetainedAfterRelease(journal) {
		return true
	}
	return journal.LinuxCloneCleanupIntent == nil && journal.LinuxCloneTargetRemovalProof == nil &&
		journal.LinuxCloneCheckoutReceipt != nil && journal.LinuxCloneCheckoutReceipt.Validate() == nil &&
		journal.LinuxCloneCheckoutReceipt.Target == journal.LinuxCloneIntent.Target &&
		journal.WorkspacePath == journal.LinuxCloneIntent.Target && journal.WorkspaceRecoveryRequired && journal.RetainWorkspace
}

// linuxV2ReleasedCloneRetainedAfterRelease reports the exact shape a normal release
// deliberately retains: a pre-checkout clone whose seed and target reservation stay
// armed because no product disposition deletes them. Every term is required; every
// other shape keeps blocking foreign native admission.
func linuxV2ReleasedCloneRetainedAfterRelease(journal RunJournal) bool {
	intent := journal.LinuxCloneIntent
	return intent != nil && intent.ValidateForJournal() == nil && journal.LinuxCloneSeedReceipt != nil &&
		journal.LinuxCloneCheckoutReceipt == nil && journal.LinuxClonePromotionIntent == nil &&
		journal.LinuxClonePromotionReceipt == nil && !journal.LinuxCloneAbortPending &&
		journal.LinuxCloneAbortProof == nil && !journal.LinuxCloneCleanupPending &&
		journal.LinuxCloneCleanupIntent == nil && journal.LinuxCloneTargetRemovalProof == nil &&
		journal.LinuxCloneCleanupProof == nil && journal.LinuxCloneReuseGrant == nil &&
		!journal.NativeUsageRecoveryRequired && journal.WorkspacePath == intent.Target &&
		journal.WorkspaceBindingKey == intent.BindingKey && journal.WorkspaceRecoveryRequired
}

func (journal RunCloneState) linuxCloneStageCanAdvance(localState string) bool {
	return localState != "terminal_pending" && localState != "cleanup_pending" && localState != "stale" &&
		!journal.LinuxCloneAbortPending && journal.LinuxCloneAbortProof == nil
}

func linuxCloneCheckoutMatches(intent authority.LinuxCloneIntent, seed authority.LinuxCloneSeedReceipt, receipt authority.LinuxCloneCheckoutReceipt) bool {
	return receipt.Validate() == nil && receipt.RunID == intent.RunID && receipt.Generation == intent.Generation &&
		receipt.BindingKey == intent.BindingKey && receipt.ResourceID == intent.ResourceID &&
		receipt.Target == intent.Target && receipt.Nonce == intent.Nonce &&
		receipt.SeedDigest == seed.Digest && receipt.BaseCommit == intent.BaseCommit && seed.BaseCommit == intent.BaseCommit
}

func linuxCloneCommittedGateReady(journal RunJournal) bool {
	if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" || journal.LocalState == "stale" ||
		!journal.HasProcessDetails() || journal.ContainmentUnproven || journal.ContainmentHandoff != nil ||
		journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Gate == nil || journal.ContainmentAuthority == nil {
		return false
	}
	owner := journal.ContainmentAuthority
	gate := journal.ContainmentCgroup.Gate
	return owner.Version == authority.SupervisorVersionV2 && owner.Validate() == nil && owner.StopReceipt == nil &&
		linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *owner) &&
		owner.TargetPID == journal.PID && owner.TargetIdentity == journal.ProcessIdentity &&
		gate.Process.PID == journal.PID && gate.Process.Identity == journal.ProcessIdentity
}

func linuxCloneStopProven(journal RunJournal) bool {
	if owner := journal.ContainmentAuthority; owner != nil && owner.Version == authority.SupervisorVersionV2 &&
		owner.StopReceipt != nil && owner.StopReceipt.ValidFor(*owner) {
		return true
	}
	release := journal.ContainmentCgroupReleaseProof
	return release != nil && release.Version == authority.SupervisorHandoffVersionV2 && release.Validate() == nil
}

func validateLinuxCloneJournal(journal RunJournal) error {
	if reuse := journal.LinuxCloneReuseGrant; reuse != nil {
		if err := validateLinuxCloneReuseBranch(journal, reuse); err != nil {
			return err
		}
		return nil
	}
	intent := journal.LinuxCloneIntent
	if intent == nil {
		if err := journal.validateLinuxCloneIntentAbsent(); err != nil {
			return err
		}
		return nil
	}
	if err := validateLinuxCloneIntent(journal, intent); err != nil {
		return err
	}
	seed := journal.LinuxCloneSeedReceipt
	if err := validateLinuxCloneSeedReceipt(intent, seed); err != nil {
		return err
	}
	if journal.LinuxCloneAbortPending && journal.LinuxCloneAbortProof != nil {
		return errors.New("run journal Linux clone abort is both pending and complete")
	}
	if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
		if err := validateLinuxCloneAbort(journal, intent, seed); err != nil {
			return err
		}
		return nil
	}
	if err := validateLinuxCloneCheckout(journal, intent, seed); err != nil {
		return err
	}
	return nil
}

func validateLinuxCloneReuseBranch(journal RunJournal, reuse *authority.LinuxCloneReuseGrant) error {
	if err := validateLinuxCloneReuseGrant(journal, reuse); err != nil {
		return err
	}
	if err := validateLinuxCloneReuseAbortState(journal); err != nil {
		return err
	}
	if err := validateLinuxCloneReuseRetainedTarget(journal, reuse); err != nil {
		return err
	}
	return nil
}

func validateLinuxCloneReuseGrant(journal RunJournal, reuse *authority.LinuxCloneReuseGrant) error {
	if reuse.Validate() != nil || reuse.RunID != journal.RunID || reuse.Generation != journal.Generation ||
		journal.LinuxCloneIntent != nil || journal.LinuxCloneSeedReceipt != nil ||
		journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionIntent != nil ||
		journal.LinuxClonePromotionReceipt != nil || journal.LinuxCloneAbortPending ||
		journal.LinuxCloneAbortProof != nil || journal.LinuxCloneCleanupPending ||
		journal.LinuxCloneCleanupIntent != nil || journal.LinuxCloneTargetRemovalProof != nil ||
		journal.LinuxCloneCleanupProof != nil {
		return errors.New("run journal Linux clone reuse grant is invalid")
	}
	return nil
}

func validateLinuxCloneReuseAbortState(journal RunJournal) error {
	if journal.LinuxCloneReuseAbortedBeforeNativeStart && !journal.LinuxCloneReuseRetired {
		return errors.New("run journal Linux clone reuse abort lacks retirement")
	}
	if journal.LinuxCloneReuseAbortedBeforeNativeStart &&
		(journal.HasProcessDetails() || journal.HasPendingContainment() ||
			journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil ||
			journal.ContainmentCgroupPreGateReleaseProof != nil) {
		return errors.New("run journal Linux clone reuse abort follows a native or containment effect")
	}
	return nil
}

func validateLinuxCloneReuseRetainedTarget(journal RunJournal, reuse *authority.LinuxCloneReuseGrant) error {
	if journal.WorkspacePath != reuse.SourceCheckout.Target ||
		journal.WorkspaceBindingKey != reuse.Source.Intent.BindingKey ||
		!journal.WorkspaceRecoveryRequired || !journal.RetainWorkspace || !journal.hasClaimGrant() {
		return errors.New("run journal Linux clone reuse lost its retained target")
	}
	if err := requireLinuxCloneJournalFenceSubject(journal, reuse.Source.Intent); err != nil {
		return errors.New("run journal Linux clone reuse does not match admitted Subject")
	}
	return nil
}

func (journal RunCloneState) validateLinuxCloneIntentAbsent() error {
	if journal.LinuxCloneSeedReceipt != nil || journal.LinuxCloneCheckoutReceipt != nil ||
		journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil ||
		journal.LinuxCloneReuseRetired || journal.LinuxCloneReuseAbortedBeforeNativeStart ||
		journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil || journal.LinuxCloneCleanupPending ||
		journal.LinuxCloneCleanupIntent != nil || journal.LinuxCloneTargetRemovalProof != nil || journal.LinuxCloneCleanupProof != nil {
		return errors.New("run journal has Linux clone evidence without an intent")
	}
	return nil
}

func validateLinuxCloneIntent(journal RunJournal, intent *authority.LinuxCloneIntent) error {
	if intent.ValidateForJournal() != nil || intent.RunID != journal.RunID || intent.Generation != journal.Generation ||
		intent.BindingKey != journal.WorkspaceBindingKey || !journal.hasClaimGrant() {
		return errors.New("run journal Linux clone intent is invalid")
	}
	if err := requireLinuxCloneJournalFenceSubject(journal, *intent); err != nil {
		return errors.New("run journal Linux clone intent does not match admitted Subject")
	}
	return nil
}

func validateLinuxCloneSeedReceipt(intent *authority.LinuxCloneIntent, seed *authority.LinuxCloneSeedReceipt) error {
	if seed != nil && (seed.Validate() != nil || seed.BaseCommit != intent.BaseCommit) {
		return errors.New("run journal Linux clone seed receipt is invalid")
	}
	return nil
}

func validateLinuxCloneAbort(journal RunJournal, intent *authority.LinuxCloneIntent, seed *authority.LinuxCloneSeedReceipt) error {
	if journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil {
		return errors.New("run journal Linux clone abort state is invalid")
	}
	if journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent != nil ||
		journal.LinuxCloneTargetRemovalProof != nil || journal.LinuxCloneCleanupProof != nil {
		return errors.New("run journal clone abort conflicts with normal cleanup")
	}
	if journal.LinuxCloneAbortPending {
		if err := validateLinuxCloneAbortPending(journal, intent); err != nil {
			return err
		}
		return nil
	}
	if err := validateLinuxCloneAbortProof(journal, intent, seed); err != nil {
		return err
	}
	return nil
}

func validateLinuxCloneAbortPending(journal RunJournal, intent *authority.LinuxCloneIntent) error {
	if !linuxCloneAbortFenceAvailable(journal) && !linuxCloneAbortPendingReleaseUnresolved(journal) {
		return errors.New("run journal Linux clone abort lost its producer fence")
	}
	if journal.WorkspacePath != intent.Target || !journal.WorkspaceRecoveryRequired {
		return errors.New("run journal pending clone abort lost its reserved target")
	}
	return nil
}

func validateLinuxCloneAbortProof(journal RunJournal, intent *authority.LinuxCloneIntent, seed *authority.LinuxCloneSeedReceipt) error {
	proof := journal.LinuxCloneAbortProof
	if proof.ValidateForJournal() != nil || !proof.Intent.Equal(*intent) || journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired {
		return errors.New("run journal Linux clone abort proof is invalid")
	}
	if proof.NormalReleaseProof != nil {
		if !linuxCloneAbortPendingReleaseUnresolved(journal) ||
			!journal.ContainmentCgroupReleaseProof.Equal(*proof.NormalReleaseProof) {
			return errors.New("legacy normal-release Linux clone abort remains unresolved")
		}
	} else if !linuxCloneAbortHistoryMatches(journal, proof) {
		return errors.New("run journal Linux clone abort proof lacks exact no-gate history")
	}
	if seed != nil && (proof.SeedReceipt == nil || !seed.Equal(*proof.SeedReceipt)) {
		return errors.New("run journal Linux clone abort proof lost its bound seed receipt")
	}
	return nil
}

func validateLinuxCloneCheckout(journal RunJournal, intent *authority.LinuxCloneIntent, seed *authority.LinuxCloneSeedReceipt) error {
	checkout := journal.LinuxCloneCheckoutReceipt
	if checkout == nil {
		if journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil ||
			journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent != nil ||
			journal.LinuxCloneTargetRemovalProof != nil || journal.LinuxCloneCleanupProof != nil ||
			journal.WorkspacePath != intent.Target {
			return errors.New("run journal Linux clone promotion has no checkout receipt")
		}
		return nil
	}
	if !linuxCloneCheckoutMatches(*intent, *seed, *checkout) {
		return errors.New("run journal Linux clone checkout receipt is invalid")
	}
	if err := validateLinuxClonePromotion(journal, intent); err != nil {
		return err
	}
	return nil
}

func validateLinuxClonePromotion(journal RunJournal, intent *authority.LinuxCloneIntent) error {
	promotionIntent := journal.LinuxClonePromotionIntent
	if promotionIntent == nil {
		if err := validateLinuxClonePromotionWithoutIntent(journal, intent); err != nil {
			return err
		}
		return nil
	}
	ref, err := authority.LinuxClonePromotionRef(journal.RunID, journal.Generation)
	if promotionIntent.Validate() != nil || promotionIntent.ResourceID != intent.ResourceID ||
		len(promotionIntent.Commit) != len(intent.BaseCommit) || !linuxCloneStopProven(journal) {
		return errors.New("run journal Linux clone promotion intent is invalid")
	}
	promotion := journal.LinuxClonePromotionReceipt
	if promotion == nil {
		if journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent != nil ||
			journal.LinuxCloneTargetRemovalProof != nil || journal.LinuxCloneCleanupProof != nil ||
			journal.WorkspacePath != intent.Target {
			return errors.New("run journal Linux clone cleanup has no promotion receipt")
		}
		return nil
	}
	if promotion.Validate() != nil || err != nil || promotion.Ref != ref || promotion.Commit != promotionIntent.Commit ||
		promotion.TreeDigest != promotionIntent.TreeDigest || !linuxCloneStopProven(journal) {
		return errors.New("run journal Linux clone promotion receipt is invalid")
	}
	if err := validateLinuxCloneCleanup(journal, intent); err != nil {
		return err
	}
	return nil
}

func validateLinuxClonePromotionWithoutIntent(journal RunJournal, intent *authority.LinuxCloneIntent) error {
	if journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent != nil ||
		journal.LinuxCloneTargetRemovalProof != nil || journal.LinuxCloneCleanupProof != nil {
		return errors.New("run journal Linux clone promotion receipt has no intent")
	}
	promotion := journal.LinuxClonePromotionReceipt
	if promotion == nil {
		if journal.WorkspacePath != intent.Target {
			return errors.New("run journal Linux clone target path changed")
		}
		return nil
	}
	legacyRef, refErr := authority.LinuxClonePromotionRef(journal.RunID, journal.Generation)
	if promotion.Validate() != nil || refErr != nil || promotion.Ref != legacyRef ||
		len(promotion.Commit) != len(intent.BaseCommit) || !linuxCloneStopProven(journal) ||
		journal.WorkspacePath != intent.Target {
		return errors.New("run journal legacy Linux clone promotion receipt is invalid")
	}
	return nil
}

func validateLinuxCloneCleanup(journal RunJournal, intent *authority.LinuxCloneIntent) error {
	if journal.LinuxCloneCleanupIntent == nil {
		if journal.LinuxCloneCleanupPending || journal.LinuxCloneTargetRemovalProof != nil ||
			journal.LinuxCloneCleanupProof != nil || journal.WorkspacePath != intent.Target {
			return errors.New("run journal Linux clone cleanup stage is invalid")
		}
		return nil
	}
	cleanupIntent, err := linuxCloneCleanupIntentFromJournal(journal)
	if err != nil || !cleanupIntent.Equal(*journal.LinuxCloneCleanupIntent) {
		return errors.New("run journal Linux clone cleanup intent is invalid")
	}
	if journal.LinuxCloneCleanupPending {
		if err := journal.validateLinuxCloneCleanupPending(intent, cleanupIntent); err != nil {
			return err
		}
		return nil
	}
	if err := journal.validateLinuxCloneCleanupProof(cleanupIntent); err != nil {
		return err
	}
	return nil
}

func (journal RunCloneState) validateLinuxCloneCleanupPending(intent *authority.LinuxCloneIntent, cleanupIntent authority.LinuxCloneCleanupIntent) error {
	if journal.LinuxCloneCleanupProof != nil || journal.WorkspacePath != intent.Target || !journal.WorkspaceRecoveryRequired {
		return errors.New("run journal pending Linux clone cleanup stage is invalid")
	}
	if journal.LinuxCloneTargetRemovalProof != nil &&
		(journal.LinuxCloneTargetRemovalProof.Validate() != nil ||
			!journal.LinuxCloneTargetRemovalProof.Intent.Equal(cleanupIntent)) {
		return errors.New("run journal pending Linux clone target-removal proof is invalid")
	}
	return nil
}

func (journal RunCloneState) validateLinuxCloneCleanupProof(cleanupIntent authority.LinuxCloneCleanupIntent) error {
	cleanupProof := journal.LinuxCloneCleanupProof
	if journal.LinuxCloneTargetRemovalProof == nil || journal.LinuxCloneTargetRemovalProof.Validate() != nil ||
		!journal.LinuxCloneTargetRemovalProof.Intent.Equal(cleanupIntent) || cleanupProof == nil ||
		cleanupProof.Validate() != nil || !cleanupProof.Intent.Equal(cleanupIntent) ||
		!cleanupProof.TargetRemovalProof.Equal(*journal.LinuxCloneTargetRemovalProof) ||
		journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired {
		return errors.New("run journal Linux clone cleanup proof is invalid")
	}
	return nil
}

func linuxCloneAbortHistoryMatches(journal RunJournal, proof *authority.LinuxCloneAbortProof) bool {
	if !linuxCloneAbortNoActiveOwner(journal) || proof != nil && proof.NormalReleaseProof != nil {
		return false
	}
	if proof == nil {
		return journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
			journal.ContainmentCgroupPreGateReleaseProof == nil
	}
	if proof.CgroupAbortProof != nil {
		return proof.PreGateReleaseProof == nil && proof.NormalReleaseProof == nil && journal.ContainmentCgroup != nil &&
			journal.ContainmentCgroup.AbortProof != nil && journal.ContainmentCgroup.AbortProof.Equal(*proof.CgroupAbortProof) &&
			linuxCgroupReservationAbortMatches(*journal.ContainmentCgroup, *proof.CgroupAbortProof) &&
			journal.ContainmentCgroupReleaseProof == nil && journal.ContainmentCgroupPreGateReleaseProof == nil
	}
	if proof.PreGateReleaseProof != nil {
		return journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
			journal.ContainmentCgroupPreGateReleaseProof != nil &&
			journal.ContainmentCgroupPreGateReleaseProof.Equal(*proof.PreGateReleaseProof)
	}
	return journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
		journal.ContainmentCgroupPreGateReleaseProof == nil
}

// linuxCloneAbortPendingReleaseUnresolved recognizes only the old structural
// state shape where a no-checkout abort was fenced after normal v2 release.
// It keeps that journal readable but never supplies no-gate authority.
func linuxCloneAbortPendingReleaseUnresolved(journal RunJournal) bool {
	release := journal.ContainmentCgroupReleaseProof
	return linuxCloneAbortNoActiveOwner(journal) && journal.ContainmentCgroup == nil &&
		journal.ContainmentCgroupPreGateReleaseProof == nil && release != nil &&
		release.Validate() == nil && release.Version == authority.SupervisorHandoffVersionV2 && release.StopReceipt != nil
}

func linuxCloneAbortFenceAvailable(journal RunJournal) bool {
	if !linuxCloneAbortNoActiveOwner(journal) {
		return false
	}
	if journal.ContainmentCgroupReleaseProof != nil {
		return false
	}
	if reservation := journal.ContainmentCgroup; reservation != nil {
		proof := reservation.AbortProof
		return proof != nil && reservation.PreGateStopReceipt == nil && reservation.Gate == nil &&
			linuxCgroupReservationAbortMatches(*reservation, *proof) && journal.ContainmentCgroupPreGateReleaseProof == nil
	}
	if proof := journal.ContainmentCgroupPreGateReleaseProof; proof != nil {
		return proof.Version == authority.SupervisorHandoffVersionV2 && proof.Validate() == nil
	}
	return true
}

func linuxCloneAbortNoActiveOwner(journal RunJournal) bool {
	return !journal.HasProcessDetails() && journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil &&
		journal.ContainmentScans == nil && !journal.ContainmentUnproven && !journal.HasPendingContainment()
}
