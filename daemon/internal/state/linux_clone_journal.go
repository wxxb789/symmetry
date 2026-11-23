package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// RunCloneState is the workspace-ownership and Linux clone authority block of
// RunJournal; embedding keeps the persisted journal's flat JSON key order.
type RunCloneState struct {
	WorkspacePath             string `json:"workspace_path"`
	WorkspaceRecoveryRequired bool   `json:"workspace_recovery_required,omitempty"`
	RetainWorkspace           bool   `json:"retain_workspace,omitempty"`
	WorkspaceBindingKey       string `json:"workspace_binding_key"`
	// LinuxCloneIntent and the stage receipts are additive v2 clone authority
	// in this existing run journal. Each advances only through its dedicated
	// RunKey/fence CAS; checkout presence never clears process containment.
	LinuxCloneIntent          *authority.LinuxCloneIntent          `json:"linux_clone_intent,omitempty"`
	LinuxCloneSeedReceipt     *authority.LinuxCloneSeedReceipt     `json:"linux_clone_seed_receipt,omitempty"`
	LinuxCloneCheckoutReceipt *authority.LinuxCloneCheckoutReceipt `json:"linux_clone_checkout_receipt,omitempty"`
	LinuxCloneReuseGrant      *authority.LinuxCloneReuseGrant      `json:"linux_clone_reuse_grant,omitempty"`
	// LinuxCloneReuseRetired stops the consuming journal from acting as a live
	// reuse alias. The grant and retained workspace remain for provenance and
	// because the source journal still owns the target.
	LinuxCloneReuseRetired bool `json:"linux_clone_reuse_retired,omitempty"`
	// LinuxCloneReuseAbortedBeforeNativeStart is durable evidence that the
	// consuming RunKey retired its alias before any native launch or containment
	// effect, so no containment release receipt can exist.
	LinuxCloneReuseAbortedBeforeNativeStart bool                                  `json:"linux_clone_reuse_aborted_before_native_start,omitempty"`
	LinuxClonePromotionIntent               *authority.LinuxClonePromotionIntent  `json:"linux_clone_promotion_intent,omitempty"`
	LinuxClonePromotionReceipt              *authority.LinuxClonePromotionReceipt `json:"linux_clone_promotion_receipt,omitempty"`
	// LinuxCloneAbortPending is written before A-side no-checkout cleanup and
	// fences every later gate/checkout stage. LinuxCloneAbortProof is the exact
	// absence tombstone written only after A verifies target, seed and refs gone.
	LinuxCloneAbortPending bool                            `json:"linux_clone_abort_pending,omitempty"`
	LinuxCloneAbortProof   *authority.LinuxCloneAbortProof `json:"linux_clone_abort_proof,omitempty"`
	// LinuxCloneCleanupPending fences exact normal cleanup effects after all
	// candidate/stop/owner gates. The target-removal receipt is persisted before
	// A-side cleanup; the cleanup proof is the final durable readback.
	LinuxCloneCleanupPending     bool                                    `json:"linux_clone_cleanup_pending,omitempty"`
	LinuxCloneCleanupIntent      *authority.LinuxCloneCleanupIntent      `json:"linux_clone_cleanup_intent,omitempty"`
	LinuxCloneTargetRemovalProof *authority.LinuxCloneTargetRemovalProof `json:"linux_clone_target_removal_proof,omitempty"`
	LinuxCloneCleanupProof       *authority.LinuxCloneCleanupProof       `json:"linux_clone_cleanup_proof,omitempty"`
}

func linuxClonePathAliases(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func linuxCloneArtifactReceiptRequired(journal RunJournal) bool {
	if !journal.GoalDeliveryEnabled || journal.TerminalState != "completed" {
		return false
	}
	admission, present, err := protocol.ParseAdmissionInput(journal.Work.Input)
	if err != nil || !present {
		return true
	}
	if admission.Purpose != protocol.AdmissionPurposeImplement {
		return false
	}
	switch journal.TerminalTaskResultKind {
	case protocol.TaskResultCandidateCompletion:
		for _, delivery := range journal.DeliveredGoalDeliveries {
			if delivery.Kind == GoalDeliveryEvidence && delivery.Evidence != nil && delivery.Evidence.Kind == protocol.EvidenceArtifact {
				return false
			}
		}
		return true
	case protocol.TaskResultProgress, protocol.TaskResultBlocked, protocol.TaskResultRepairRequired,
		protocol.TaskResultReplanRequired, protocol.TaskResultFailed, protocol.TaskResultPlanProposed:
		return false
	default:
		return true
	}
}

// LinuxCloneCleanupIntentForJournal returns the deterministic cleanup identity
// derived from an exact RunJournal. It never accepts a caller-selected path,
// ref, seed, candidate, or release receipt.
func LinuxCloneCleanupIntentForJournal(journal RunJournal) (authority.LinuxCloneCleanupIntent, error) {
	derived, err := linuxCloneCleanupIntentFromJournal(journal)
	if err != nil {
		return authority.LinuxCloneCleanupIntent{}, err
	}
	if journal.LinuxCloneCleanupIntent != nil {
		if !journal.LinuxCloneCleanupIntent.Equal(derived) {
			return authority.LinuxCloneCleanupIntent{}, errors.New("stored Linux clone cleanup intent differs from RunJournal authority")
		}
		return journal.LinuxCloneCleanupIntent.Clone(), nil
	}
	if !journal.LinuxCloneCleanupBeginEligible(journal.Key(), journal.Fence()) {
		return authority.LinuxCloneCleanupIntent{}, errors.New("RunJournal is not eligible to begin Linux clone cleanup")
	}
	return derived, nil
}

func linuxCloneCleanupIntentFromJournal(journal RunJournal) (authority.LinuxCloneCleanupIntent, error) {
	if journal.LinuxCloneIntent == nil || journal.LinuxCloneSeedReceipt == nil || journal.LinuxCloneCheckoutReceipt == nil ||
		journal.LinuxClonePromotionIntent == nil || journal.LinuxClonePromotionReceipt == nil ||
		journal.ContainmentCgroupReleaseProof == nil {
		return authority.LinuxCloneCleanupIntent{}, errors.New("Linux clone cleanup intent lacks complete promotion and release evidence")
	}
	root, err := authority.LinuxCloneWorkspaceRoot(*journal.LinuxCloneIntent)
	if err != nil {
		return authority.LinuxCloneCleanupIntent{}, err
	}
	baseRef, err := authority.LinuxCloneBaseRef(journal.RunID, journal.Generation)
	if err != nil {
		return authority.LinuxCloneCleanupIntent{}, err
	}
	fenceBytes, err := json.Marshal(journal.Fence())
	if err != nil {
		return authority.LinuxCloneCleanupIntent{}, fmt.Errorf("encode Linux clone cleanup fence: %w", err)
	}
	fenceHash := sha256.Sum256(fenceBytes)
	cleanup := authority.LinuxCloneCleanupIntent{
		Version:            authority.LinuxCloneVersion,
		Intent:             journal.LinuxCloneIntent.Clone(),
		FenceDigest:        "sha256:" + hex.EncodeToString(fenceHash[:]),
		RootPath:           root,
		BaseRef:            baseRef,
		SeedReceipt:        journal.LinuxCloneSeedReceipt.Clone(),
		CheckoutReceipt:    journal.LinuxCloneCheckoutReceipt.Clone(),
		PromotionIntent:    journal.LinuxClonePromotionIntent.Clone(),
		PromotionReceipt:   journal.LinuxClonePromotionReceipt.Clone(),
		NormalReleaseProof: journal.ContainmentCgroupReleaseProof.Clone(),
	}
	artifact, ok := linuxCloneDeliveredArtifactIdentity(journal)
	if !ok {
		return authority.LinuxCloneCleanupIntent{}, errors.New("exact promoted candidate artifact delivery is missing")
	}
	cleanup.ArtifactEvidenceID = artifact.EvidenceID
	cleanup.ArtifactEvidenceKey = artifact.EvidenceKey
	cleanup.ArtifactSubjectHash = artifact.SubjectHash
	cleanup.ArtifactDeliveryDigest = artifact.DeliveryDigest
	if err := cleanup.Validate(); err != nil {
		return authority.LinuxCloneCleanupIntent{}, err
	}
	return cleanup, nil
}

func linuxCloneCleanupIntentMatchesJournal(journal RunJournal, expected authority.LinuxCloneCleanupIntent) bool {
	actual, err := linuxCloneCleanupIntentFromJournal(journal)
	return err == nil && actual.Equal(expected)
}

func linuxCloneExactArtifactDelivered(journal RunJournal) bool {
	_, ok := linuxCloneDeliveredArtifactIdentity(journal)
	return ok
}

type linuxCloneArtifactReceipt struct {
	EvidenceID     string
	EvidenceKey    string
	SubjectHash    string
	DeliveryDigest string
}

func linuxCloneDeliveredArtifactIdentity(journal RunJournal) (linuxCloneArtifactReceipt, bool) {
	intent := journal.LinuxClonePromotionIntent
	if intent == nil || !journal.GoalDeliveryEnabled || journal.TerminalState != "completed" ||
		journal.TerminalTaskResultKind != protocol.TaskResultCandidateCompletion {
		return linuxCloneArtifactReceipt{}, false
	}
	admission, present, err := protocol.ParseAdmissionInput(journal.Work.Input)
	if err != nil || !present || admission.Purpose != protocol.AdmissionPurposeImplement ||
		admission.Subject.ResourceID != intent.ResourceID {
		return linuxCloneArtifactReceipt{}, false
	}
	subject := protocol.Subject{ResourceID: intent.ResourceID, Commit: intent.Commit, TreeDigest: intent.TreeDigest}
	subjectHash, err := subject.Hash()
	if err != nil {
		return linuxCloneArtifactReceipt{}, false
	}
	for _, delivery := range journal.DeliveredGoalDeliveries {
		evidence := delivery.Evidence
		if delivery.Kind == GoalDeliveryEvidence && delivery.Ready && delivery.Fence == journal.Fence() &&
			evidence != nil && delivery.DeliveryID == evidence.EvidenceKey && validGoalDeliveryDigest(delivery.PayloadDigest) &&
			evidence.Validate() == nil && evidence.RunID == journal.RunID &&
			evidence.Kind == protocol.EvidenceArtifact && evidence.Subject == subject && evidence.SubjectHash == subjectHash {
			candidate := delivery
			candidate.PayloadDigest = ""
			if err := prepareGoalDelivery(journal.RunID, &candidate); err != nil || candidate.PayloadDigest != delivery.PayloadDigest {
				continue
			}
			return linuxCloneArtifactReceipt{
				EvidenceID: evidence.EvidenceID, EvidenceKey: evidence.EvidenceKey,
				SubjectHash: evidence.SubjectHash, DeliveryDigest: delivery.PayloadDigest,
			}, true
		}
	}
	return linuxCloneArtifactReceipt{}, false
}

// ReserveLinuxCloneIntent durably reserves the exact RunKey, current admitted
// Subject, seed path and B-owned target before the workspace service performs
// any Git/ref/bundle or checkout side effect. Even exact replay requires the
// target Control lease and lifecycle to remain effect-ready.
func (store *Store) ReserveLinuxCloneIntent(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, liveOwners map[RunKey]protocol.Fence) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := intent.Validate(); err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation {
		return RunJournal{}, errors.New("Linux clone intent does not match RunKey")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if err := store.checkLinuxV2NativeAdmissionLocked(*journal, key, fence, time.Now(), liveOwners, nil, &intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent != nil {
			if !journal.LinuxCloneIntent.Equal(intent) {
				return errors.New("Linux clone intent cannot be replaced")
			}
			if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
				return errors.New("Linux clone intent is fenced for pre-checkout abort")
			}
			return nil
		}
		if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" || journal.LocalState == "stale" ||
			journal.HasProcessDetails() || journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil ||
			journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("Linux clone intent must be reserved before process containment or terminal cleanup")
		}
		if journal.WorkspacePath != "" {
			return errors.New("Linux clone intent cannot adopt a pre-existing workspace path")
		}
		copy := intent.Clone()
		journal.LinuxCloneIntent = &copy
		journal.WorkspacePath = intent.Target
		journal.WorkspaceBindingKey = intent.BindingKey
		journal.WorkspaceRecoveryRequired = true
		return nil
	})
}

// BindLinuxCloneSeed records the A-measured immutable seed bundle receipt only
// after its exact reserved intent has been persisted.
func (store *Store) BindLinuxCloneSeed(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, seed authority.LinuxCloneSeedReceipt) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := seed.Validate(); err != nil || seed.BaseCommit != intent.BaseCommit {
		return RunJournal{}, errors.New("Linux clone seed receipt does not match its intent")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) {
			return errors.New("Linux clone seed intent is not durably reserved")
		}
		if journal.LinuxCloneSeedReceipt != nil {
			if !journal.LinuxCloneSeedReceipt.Equal(seed) {
				return errors.New("Linux clone seed receipt cannot be changed")
			}
			return nil
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil || !journal.linuxCloneStageCanAdvance(journal.LocalState) {
			return errors.New("Linux clone seed cannot advance after abort or terminal fencing")
		}
		copy := seed.Clone()
		journal.LinuxCloneSeedReceipt = &copy
		return nil
	})
}

// RecordLinuxCloneCheckoutReady binds the B-created target inode to the
// reserved intent and seed. The initial write is legal only after the v2
// handoff is durably committed; the trusted same-PID gate calls it after its
// existing initial-lease-ACK/RELEASE barrier and before native exec.
func (store *Store) RecordLinuxCloneCheckoutReady(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, receipt authority.LinuxCloneCheckoutReceipt) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := receipt.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) || journal.LinuxCloneSeedReceipt == nil {
			return errors.New("Linux clone checkout is missing its reserved intent or seed")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("Linux clone checkout is fenced for pre-checkout abort")
		}
		if !linuxCloneCheckoutMatches(intent, *journal.LinuxCloneSeedReceipt, receipt) {
			return errors.New("Linux clone checkout receipt does not match its intent and seed")
		}
		if journal.LinuxCloneCheckoutReceipt != nil {
			if !journal.LinuxCloneCheckoutReceipt.Equal(receipt) {
				return errors.New("Linux clone checkout receipt cannot be changed")
			}
			return nil
		}
		if !journal.linuxCloneStageCanAdvance(journal.LocalState) {
			return errors.New("Linux clone checkout cannot advance after terminal fencing")
		}
		if !linuxCloneCommittedGateReady(*journal) {
			return errors.New("Linux clone checkout requires committed v2 gate authority")
		}
		copy := receipt.Clone()
		journal.LinuxCloneCheckoutReceipt = &copy
		return nil
	})
}

// BindLinuxCloneReuse authorizes a new RunKey to keep using the exact B target
// owned by one stopped source RunKey. It is intentionally separate from
// ReserveLinuxCloneIntent: the new journal owns no clone, seed, checkout,
// promotion or cleanup effect. The source journal remains the deletion owner.
func (store *Store) BindLinuxCloneReuse(key RunKey, fence protocol.Fence, grant authority.LinuxCloneReuseGrant, liveOwners map[RunKey]protocol.Fence) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := grant.Validate(); err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation {
		return RunJournal{}, errors.New("Linux clone reuse grant does not match the new RunKey")
	}
	sourceKey := RunKey{RunID: grant.Source.Intent.RunID, Generation: grant.Source.Intent.Generation}
	if sourceKey == key {
		return RunJournal{}, errors.New("Linux clone reuse cannot bind the source RunKey")
	}
	return store.mutateJournalCAS(key, fence, errors.New("Linux clone reuse target lease or lifecycle is not effect-ready"), func(journal *RunJournal) error {
		if !journal.hasClaimGrant() || !linuxV2TargetCanStartNativeEffects(*journal, time.Now()) {
			return errors.New("Linux clone reuse target lease or lifecycle is not effect-ready")
		}
		if journal.LinuxCloneReuseGrant != nil {
			if journal.LinuxCloneReuseRetired || journal.LinuxCloneReuseAbortedBeforeNativeStart ||
				!journal.LinuxCloneReuseGrant.Equal(grant) {
				return errors.New("Linux clone reuse grant cannot be replaced")
			}
			return nil
		}
		if journal.LinuxCloneIntent != nil || journal.LinuxCloneSeedReceipt != nil ||
			journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionIntent != nil ||
			journal.LinuxClonePromotionReceipt != nil || journal.LinuxCloneAbortPending ||
			journal.LinuxCloneAbortProof != nil || journal.LinuxCloneCleanupPending ||
			journal.LinuxCloneCleanupIntent != nil || journal.LinuxCloneTargetRemovalProof != nil ||
			journal.LinuxCloneCleanupProof != nil || journal.WorkspacePath != "" ||
			journal.WorkspaceRecoveryRequired || journal.RetainWorkspace ||
			journal.LinuxCloneReuseRetired || journal.LinuxCloneReuseAbortedBeforeNativeStart {
			return errors.New("Linux clone reuse requires an empty clone and workspace state")
		}
		if err := requireLinuxCloneJournalFenceSubject(*journal, grant.Source.Intent); err != nil {
			return err
		}
		source, err := store.loadJournalLocked(sourceKey)
		if err != nil {
			return fmt.Errorf("load Linux clone reuse source journal: %w", err)
		}
		if !linuxCloneReuseSourceMatches(source, grant) {
			return errors.New("Linux clone reuse source journal differs from its grant")
		}
		if _, err := store.linuxCloneReuseGoalSessionLocked(grant); err != nil {
			return err
		}
		if err := store.scanLinuxV2NativeAdmissionLocked(&key, fence, time.Now(), liveOwners, nil, nil); err != nil {
			return err
		}
		if err := store.retireStoppedLinuxCloneReuseAliasesLocked(key, sourceKey, grant.SourceCheckout.Target); err != nil {
			return err
		}
		if err := store.checkLinuxCloneReuseAliasesLocked(key, sourceKey, grant.SourceCheckout.Target); err != nil {
			return err
		}
		copyGrant := grant.Clone()
		journal.LinuxCloneReuseGrant = &copyGrant
		journal.WorkspacePath = grant.SourceCheckout.Target
		journal.WorkspaceBindingKey = grant.Source.Intent.BindingKey
		journal.WorkspaceRecoveryRequired = true
		journal.RetainWorkspace = true
		return nil
	})
}

func linuxCloneReuseSourceMatches(source RunJournal, grant authority.LinuxCloneReuseGrant) bool {
	return source.LinuxCloneIntent != nil && source.LinuxCloneSeedReceipt != nil &&
		source.LinuxCloneCheckoutReceipt != nil &&
		source.LinuxCloneIntent.Equal(grant.Source.Intent) &&
		source.LinuxCloneSeedReceipt.Equal(grant.Source.Seed) &&
		source.LinuxCloneCheckoutReceipt.Equal(grant.SourceCheckout) &&
		source.WorkspacePath == grant.SourceCheckout.Target &&
		source.WorkspaceBindingKey == grant.Source.Intent.BindingKey &&
		source.WorkspaceRecoveryRequired && source.RetainWorkspace &&
		source.TerminalState == "completed" && !source.HasProcessDetails() &&
		!source.HasPendingContainment() && !source.LinuxCloneCleanupPending &&
		source.LinuxCloneCleanupIntent == nil && source.LinuxCloneTargetRemovalProof == nil &&
		source.LinuxCloneCleanupProof == nil && linuxCloneStopProven(source)
}

func (store *Store) linuxCloneReuseGoalSessionLocked(grant authority.LinuxCloneReuseGrant) (GoalSessionJournal, error) {
	if !validGoalSessionUUID(grant.PredecessorStop.SessionID) {
		return GoalSessionJournal{}, errors.New("Linux clone reuse stop session ID is invalid")
	}
	sessions, err := store.listGoalSessionsLocked()
	if err != nil {
		return GoalSessionJournal{}, err
	}
	var match *GoalSessionJournal
	for index := range sessions {
		if sessions[index].ControlSessionID != grant.PredecessorStop.SessionID {
			continue
		}
		if match != nil {
			return GoalSessionJournal{}, ErrGoalSessionConflict
		}
		copy := sessions[index]
		match = &copy
	}
	if match == nil {
		return GoalSessionJournal{}, errors.New("Linux clone reuse goal session is unavailable")
	}
	session := *match
	if session.SessionState != GoalSessionStateAvailable || session.StopCertificate == nil ||
		!session.HasVerifiedControlAttachment() || session.NeedsReconciliation() {
		return GoalSessionJournal{}, errors.New("Linux clone reuse goal session is not stably stopped")
	}
	stop := grant.PredecessorStop
	certificate := *session.StopCertificate
	if certificate.RunID != stop.RunID || certificate.Generation != stop.Generation ||
		certificate.SessionID != stop.SessionID || certificate.LocalHandleID != stop.LocalHandleID ||
		certificate.BindingID != stop.BindingID || certificate.DeliveryDigest != stop.DeliveryDigest ||
		certificate.ReceiptID != stop.ReceiptID {
		return GoalSessionJournal{}, errors.New("Linux clone reuse stop certificate differs from its grant")
	}
	if session.WorkspaceOwnerRunKey.RunID != grant.Source.Intent.RunID ||
		session.WorkspaceOwnerRunKey.Generation != grant.Source.Intent.Generation ||
		session.WorkspacePath != grant.SourceCheckout.Target ||
		session.WorkspaceFingerprint != grant.WorkspaceFingerprint ||
		session.RepositoryResourceID != grant.Source.Intent.ResourceID {
		return GoalSessionJournal{}, errors.New("Linux clone reuse goal session differs from its clone provenance")
	}
	return session, nil
}

func (store *Store) checkLinuxCloneReuseAliasesLocked(key, sourceKey RunKey, target string) error {
	return store.forEachJournalLocked(func(foreign RunJournal) error {
		if foreign.Key() == key || foreign.Key() == sourceKey {
			return nil
		}
		if foreign.LinuxCloneIntent != nil && linuxClonePathAliases(foreign.LinuxCloneIntent.Target, target) {
			return errors.New("Linux clone reuse target is already reserved")
		}
		if foreign.LinuxCloneReuseGrant != nil && !foreign.LinuxCloneReuseRetired &&
			linuxClonePathAliases(foreign.LinuxCloneReuseGrant.SourceCheckout.Target, target) {
			return errors.New("Linux clone reuse target already has another reuse owner")
		}
		return nil
	})
}

func (store *Store) retireStoppedLinuxCloneReuseAliasesLocked(currentKey, sourceKey RunKey, target string) error {
	return store.forEachJournalLocked(func(foreign RunJournal) error {
		if foreign.Key() == currentKey || foreign.Key() == sourceKey ||
			foreign.LinuxCloneReuseGrant == nil || foreign.LinuxCloneReuseRetired ||
			!linuxClonePathAliases(foreign.LinuxCloneReuseGrant.SourceCheckout.Target, target) {
			return nil
		}
		if !linuxCloneReuseAliasRetirable(foreign) {
			return errors.New("Linux clone reuse target has a live predecessor alias")
		}
		retired := foreign
		retired.LinuxCloneReuseRetired = true
		return store.saveJournalWithCapacityGuardLocked(retired)
	})
}

func linuxCloneReuseAliasRetirable(journal RunJournal) bool {
	return journal.LinuxCloneReuseGrant != nil && !journal.LinuxCloneReuseRetired &&
		isTerminalTransitionState(journal.TerminalState) && !journal.HasProcessDetails() &&
		!journal.HasPendingContainment() && !journal.NativeUsageRecoveryRequired &&
		journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof != nil &&
		journal.ContainmentCgroupPreGateReleaseProof == nil && linuxCloneStopProven(journal)
}

// AbortLinuxCloneReuseBeforeNativeStart retires a consuming alias after a
// pre-native admission failure. The source target is untouched; this proof is
// accepted as safely retired only while no process, containment reservation or
// release receipt exists.
func (store *Store) AbortLinuxCloneReuseBeforeNativeStart(key RunKey, fence protocol.Fence, grant authority.LinuxCloneReuseGrant) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := grant.Validate(); err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation {
		return RunJournal{}, errors.New("Linux clone reuse abort grant does not match the RunKey")
	}
	stale := errors.New("Linux clone reuse abort has no exact consuming grant")
	return store.mutateJournalCAS(key, fence, stale, func(journal *RunJournal) error {
		if journal.LinuxCloneReuseGrant == nil || !journal.LinuxCloneReuseGrant.Equal(grant) {
			return stale
		}
		if journal.LinuxCloneReuseAbortedBeforeNativeStart {
			return nil
		}
		if journal.LinuxCloneReuseRetired || journal.HasProcessDetails() || journal.HasPendingContainment() ||
			journal.ContainmentUnproven || journal.NativeUsageRecoveryRequired ||
			journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil ||
			journal.ContainmentCgroupPreGateReleaseProof != nil || journal.LinuxCloneAbortPending ||
			journal.LinuxCloneAbortProof != nil || journal.LinuxCloneCleanupPending ||
			journal.LinuxCloneCleanupIntent != nil || journal.LinuxCloneTargetRemovalProof != nil ||
			journal.LinuxCloneCleanupProof != nil {
			return errors.New("Linux clone reuse abort follows a native or containment effect")
		}
		journal.LinuxCloneReuseRetired = true
		journal.LinuxCloneReuseAbortedBeforeNativeStart = true
		return nil
	})
}

// ReserveLinuxClonePromotion persists the exact candidate before the workspace
// service imports objects or creates the A-owned promotion ref. It is legal
// only under the committed checkout's durable v2 stop receipt. A replay never
// adopts a ref; it must match this already stored candidate intent, and the
// target Control lease/lifecycle must still authorize a first promotion effect.
func (store *Store) ReserveLinuxClonePromotion(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, candidate authority.LinuxClonePromotionIntent) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := candidate.Validate(); err != nil {
		return RunJournal{}, err
	}
	if candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) {
		return RunJournal{}, errors.New("Linux clone promotion candidate does not match its resource or object format")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if !linuxV2TargetCanStartNativeEffects(*journal, time.Now()) {
			return errors.New("Linux clone promotion target lease or lifecycle is not effect-ready")
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) ||
			journal.LinuxCloneSeedReceipt == nil || journal.LinuxCloneCheckoutReceipt == nil {
			return errors.New("Linux clone promotion intent is missing its reserved checkout evidence")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("Linux clone promotion is fenced for pre-checkout abort")
		}
		if journal.LinuxClonePromotionIntent == nil && journal.LinuxClonePromotionReceipt != nil {
			return errors.New("legacy Linux clone promotion receipt has no pre-effect candidate intent")
		}
		if journal.LinuxClonePromotionIntent != nil {
			if !journal.LinuxClonePromotionIntent.Equal(candidate) {
				return errors.New("Linux clone promotion intent cannot be changed")
			}
			return nil
		}
		if !journal.linuxCloneStageCanAdvance(journal.LocalState) {
			return errors.New("Linux clone promotion intent cannot advance after terminal fencing")
		}
		owner := journal.ContainmentAuthority
		if owner == nil || owner.Version != authority.SupervisorVersionV2 || owner.StopReceipt == nil ||
			!owner.StopReceipt.ValidFor(*owner) || journal.ContainmentCgroup == nil || journal.ContainmentHandoff != nil ||
			journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentUnproven || !journal.HasProcessDetails() {
			return errors.New("Linux clone promotion intent requires the durable v2 stop receipt before release")
		}
		copy := candidate.Clone()
		journal.LinuxClonePromotionIntent = &copy
		return nil
	})
}

// RecordLinuxClonePromotion records only the A-validated candidate receipt for
// its immutable promotion intent and deterministic RunKey ref. An exact intent
// authorizes crash-recovery readback after normal release; a stranded ref with
// no prior intent is never adopted. This mutation never clears containment.
func (store *Store) RecordLinuxClonePromotion(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, candidate authority.LinuxClonePromotionIntent, receipt authority.LinuxClonePromotionReceipt) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := candidate.Validate(); err != nil || receipt.Validate() != nil ||
		candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) ||
		receipt.Version != candidate.Version || receipt.Commit != candidate.Commit || receipt.TreeDigest != candidate.TreeDigest {
		return RunJournal{}, errors.New("Linux clone promotion receipt does not match candidate intent")
	}
	ref, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil || receipt.Ref != ref {
		return RunJournal{}, errors.New("Linux clone promotion ref does not match RunKey")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) ||
			journal.LinuxCloneSeedReceipt == nil || journal.LinuxCloneCheckoutReceipt == nil ||
			journal.LinuxClonePromotionIntent == nil || !journal.LinuxClonePromotionIntent.Equal(candidate) {
			return errors.New("Linux clone promotion receipt has no exact durable intent")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil || !linuxCloneStopProven(*journal) {
			return errors.New("Linux clone promotion receipt lacks durable physical-stop evidence")
		}
		if journal.LinuxClonePromotionReceipt != nil {
			if !journal.LinuxClonePromotionReceipt.Equal(receipt) {
				return errors.New("Linux clone promotion receipt cannot be changed")
			}
			return nil
		}
		copy := receipt.Clone()
		journal.LinuxClonePromotionReceipt = &copy
		return nil
	})
}

// FenceLinuxCloneBeforeCheckout closes every B-side launch path before A
// removes a reserved seed or source ref. Replaying the same fence only adds a
// fresh persistence barrier; an existing checkout cannot enter this stage.
func (store *Store) FenceLinuxCloneBeforeCheckout(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := intent.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) ||
			journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil {
			return errors.New("Linux clone is not eligible for pre-checkout abort fencing")
		}
		if journal.LinuxCloneAbortProof != nil {
			if journal.LinuxCloneAbortProof.Validate() == nil &&
				linuxCloneAbortHistoryMatches(*journal, journal.LinuxCloneAbortProof) &&
				journal.LinuxCloneAbortProof.Intent.Equal(intent) && !journal.LinuxCloneAbortPending {
				return nil
			}
			return errors.New("Linux clone abort fence conflicts with completed proof")
		}
		if journal.LinuxCloneAbortPending {
			if !linuxCloneAbortFenceAvailable(*journal) {
				return errors.New("Linux clone abort fence is unresolved after possible B release")
			}
			return nil
		}
		if !linuxCloneAbortFenceAvailable(*journal) {
			return errors.New("Linux clone abort fence requires no producer or a durable no-gate abort proof")
		}
		journal.LinuxCloneAbortPending = true
		journal.WorkspacePath = intent.Target
		journal.WorkspaceBindingKey = intent.BindingKey
		journal.WorkspaceRecoveryRequired = true
		return nil
	})
}

// CompleteLinuxCloneAbort records exact A-side proof that the target, seed and
// run-keyed refs are absent after FenceLinuxCloneBeforeCheckout. It does not
// create checkout, promotion, or physical-stop evidence.
func (store *Store) CompleteLinuxCloneAbort(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, proof authority.LinuxCloneAbortProof) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) || !proof.Intent.Equal(intent) ||
			journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil {
			return errors.New("Linux clone abort proof does not match a pre-checkout intent")
		}
		if journal.LinuxCloneAbortProof != nil {
			if !journal.LinuxCloneAbortProof.Equal(proof) || journal.LinuxCloneAbortPending {
				return errors.New("Linux clone abort proof replay conflicts with journal")
			}
			return nil
		}
		if !journal.LinuxCloneAbortPending {
			return errors.New("Linux clone abort completion has no durable creator fence")
		}
		if journal.LinuxCloneSeedReceipt != nil &&
			(proof.SeedReceipt == nil || !journal.LinuxCloneSeedReceipt.Equal(*proof.SeedReceipt)) {
			return errors.New("Linux clone abort proof does not preserve the bound seed receipt")
		}
		if !linuxCloneAbortHistoryMatches(*journal, &proof) {
			return errors.New("Linux clone abort proof does not match durable no-gate history")
		}
		copy := proof.Clone()
		journal.LinuxCloneAbortProof = &copy
		journal.LinuxCloneAbortPending = false
		journal.WorkspacePath = ""
		journal.WorkspaceRecoveryRequired = false
		return nil
	})
}

// ClearLinuxCloneNoTargetRetention releases a retained-workspace barrier only
// after App has independently checked that no retained GoalSession or other
// retention owner remains. The exact Store-bound absence proof and RunKey fence
// make the mutation replayable; ordinary checkout/promotion retention cannot
// be cleared through this path.
func (store *Store) ClearLinuxCloneNoTargetRetention(key RunKey, fence protocol.Fence, proof authority.LinuxCloneAbortProof) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, proof.Intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(proof.Intent) ||
			journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof == nil ||
			!journal.LinuxCloneAbortProof.Equal(proof) || journal.LinuxCloneCheckoutReceipt != nil ||
			journal.LinuxClonePromotionReceipt != nil || journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired ||
			!linuxCloneAbortHistoryMatches(*journal, &proof) {
			return errors.New("Linux clone retention clear lacks exact no-target abort proof")
		}
		journal.RetainWorkspace = false
		return nil
	})
}

// BeginLinuxCloneCleanup persists the derived normal cleanup identity before
// Workspace or the trusted root deleter performs any destructive effect.
// App must separately exclude retained GoalSessions and volatile owners.
func (store *Store) BeginLinuxCloneCleanup(key RunKey, fence protocol.Fence) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournalCAS(key, fence, errors.New("Linux clone cleanup RunKey fence is stale"), func(journal *RunJournal) error {
		if journal.LinuxCloneIntent == nil {
			return errors.New("Linux clone cleanup requires a clone owner")
		}
		if err := store.requireNoLinuxCloneReuseAliasLocked(key, journal.LinuxCloneIntent.Target); err != nil {
			return err
		}
		if journal.LinuxCloneCleanupIntent != nil {
			intent, err := LinuxCloneCleanupIntentForJournal(*journal)
			if err != nil || !journal.LinuxCloneCleanupIntent.Equal(intent) {
				return errors.New("Linux clone cleanup intent readback is invalid")
			}
			if journal.LinuxCloneCleanupProof != nil {
				if journal.LinuxCloneCleanupPending || !journal.LinuxCloneCleanupProof.Intent.Equal(intent) {
					return errors.New("Linux clone cleanup proof conflicts with its intent")
				}
				return nil
			}
			if !journal.LinuxCloneCleanupPending || !journal.LinuxCloneCleanupBeginEligible(key, fence) {
				return errors.New("Linux clone cleanup intent is not durably pending")
			}
			return nil
		}
		if journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupProof != nil ||
			!journal.LinuxCloneCleanupBeginEligible(key, fence) {
			return errors.New("Linux clone cleanup is not eligible to begin")
		}
		intent, err := linuxCloneCleanupIntentFromJournal(*journal)
		if err != nil {
			return err
		}
		copy := intent.Clone()
		journal.LinuxCloneCleanupIntent = &copy
		journal.LinuxCloneCleanupPending = true
		journal.WorkspacePath = intent.Intent.Target
		journal.WorkspaceBindingKey = intent.Intent.BindingKey
		journal.WorkspaceRecoveryRequired = true
		return nil
	})
}

func (store *Store) requireNoLinuxCloneReuseAliasLocked(sourceKey RunKey, target string) error {
	return store.forEachJournalLocked(func(foreign RunJournal) error {
		if foreign.Key() == sourceKey || foreign.LinuxCloneReuseGrant == nil {
			return nil
		}
		if linuxClonePathAliases(foreign.LinuxCloneReuseGrant.SourceCheckout.Target, target) {
			return errors.New("Linux clone target is retained by a reuse owner")
		}
		return nil
	})
}

// RecordLinuxCloneTargetRemoval persists the exact trusted-root receipt before
// any A-side cleanup. The initial receipt must prove unlink of the pinned inode;
// an absent path is never promoted into a fresh removal fact. Exact replay
// supplies a fresh durability barrier for a response whose outcome was lost.
func (store *Store) RecordLinuxCloneTargetRemoval(key RunKey, fence protocol.Fence, expected authority.LinuxCloneCleanupIntent, proof authority.LinuxCloneTargetRemovalProof) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := expected.Validate(); err != nil || proof.Validate() != nil ||
		proof.Disposition != authority.LinuxCloneTargetDispositionRemoved || !proof.TargetNlinkZero ||
		!proof.Intent.Equal(expected) {
		return RunJournal{}, errors.New("Linux clone target-removal proof does not match cleanup intent")
	}
	return store.mutateJournalCAS(key, fence, errors.New("Linux clone target-removal fence is stale"), func(journal *RunJournal) error {
		if !journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent == nil ||
			!journal.LinuxCloneCleanupIntent.Equal(expected) || !linuxCloneCleanupIntentMatchesJournal(*journal, expected) ||
			!journal.LinuxCloneCleanupBeginEligible(key, fence) {
			return errors.New("Linux clone target-removal proof lacks its exact pending cleanup intent and gates")
		}
		if journal.LinuxCloneTargetRemovalProof != nil {
			if !journal.LinuxCloneTargetRemovalProof.Equal(proof) {
				return errors.New("Linux clone target-removal proof replay conflicts with journal")
			}
			return nil
		}
		copy := proof.Clone()
		journal.LinuxCloneTargetRemovalProof = &copy
		return nil
	})
}

// CompleteLinuxCloneCleanup records the trusted root worker's exact target
// removal and A-side seed/base-ref/scratch readbacks. It never infers absence
// from PID or cgroup state and preserves the promoted artifact ref.
func (store *Store) CompleteLinuxCloneCleanup(key RunKey, fence protocol.Fence, expected authority.LinuxCloneCleanupIntent, proof authority.LinuxCloneCleanupProof) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := expected.Validate(); err != nil || proof.Validate() != nil || !proof.Intent.Equal(expected) {
		return RunJournal{}, errors.New("Linux clone cleanup proof does not match cleanup intent")
	}
	return store.mutateJournalCAS(key, fence, errors.New("Linux clone cleanup completion fence is stale"), func(journal *RunJournal) error {
		if journal.LinuxCloneCleanupProof != nil {
			if journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent == nil ||
				journal.LinuxCloneTargetRemovalProof == nil || !journal.LinuxCloneTargetRemovalProof.Equal(proof.TargetRemovalProof) ||
				!journal.LinuxCloneCleanupIntent.Equal(expected) || !journal.LinuxCloneCleanupProof.Equal(proof) {
				return errors.New("Linux clone cleanup proof replay conflicts with journal")
			}
			return nil
		}
		if !journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent == nil ||
			!journal.LinuxCloneCleanupIntent.Equal(expected) || !linuxCloneCleanupIntentMatchesJournal(*journal, expected) ||
			journal.LinuxCloneTargetRemovalProof == nil ||
			!journal.LinuxCloneTargetRemovalProof.Equal(proof.TargetRemovalProof) ||
			!journal.LinuxCloneCleanupBeginEligible(key, fence) || !proof.Intent.Equal(expected) {
			return errors.New("Linux clone cleanup completion lacks its exact pending intent and gates")
		}
		copy := proof.Clone()
		journal.LinuxCloneCleanupProof = &copy
		journal.LinuxCloneCleanupPending = false
		journal.WorkspacePath = ""
		journal.WorkspaceRecoveryRequired = false
		return nil
	})
}

func requireLinuxClonePlatform() error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("Linux clone-v2 mutations are supported only on Linux/amd64")
	}
	return nil
}

func requireLinuxCloneJournalFence(journal RunJournal, fence protocol.Fence, intent authority.LinuxCloneIntent) error {
	if intent.Validate() != nil || intent.RunID != journal.RunID || intent.Generation != journal.Generation ||
		journal.Fence() != fence || !journal.hasClaimGrant() {
		return errors.New("Linux clone RunKey or fence is stale")
	}
	if intent.BindingKey != journal.WorkspaceBindingKey {
		return errors.New("Linux clone binding key does not match the journal")
	}
	admission, present, err := protocol.ParseAdmissionInput(journal.Work.Input)
	if err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID ||
		admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest {
		return errors.New("Linux clone intent does not match the current admitted Subject")
	}
	return nil
}

func requireLinuxCloneJournalFenceSubject(journal RunJournal, intent authority.LinuxCloneIntent) error {
	admission, present, err := protocol.ParseAdmissionInput(journal.Work.Input)
	if err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID ||
		admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest {
		return errors.New("admitted Subject does not match Linux clone intent")
	}
	return nil
}

func sameLinuxCloneIntent(left, right *authority.LinuxCloneIntent) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneReuseGrant(left, right *authority.LinuxCloneReuseGrant) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneSeedReceipt(left, right *authority.LinuxCloneSeedReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneCheckoutReceipt(left, right *authority.LinuxCloneCheckoutReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxClonePromotionIntent(left, right *authority.LinuxClonePromotionIntent) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxClonePromotionReceipt(left, right *authority.LinuxClonePromotionReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneAbortProof(left, right *authority.LinuxCloneAbortProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneCleanupIntent(left, right *authority.LinuxCloneCleanupIntent) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneTargetRemovalProof(left, right *authority.LinuxCloneTargetRemovalProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneCleanupProof(left, right *authority.LinuxCloneCleanupProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
