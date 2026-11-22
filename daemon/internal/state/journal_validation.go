package state

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func validateJournal(journal RunJournal) error {
	if err := validateJournalKeyScalarsAndProcessDetails(journal); err != nil {
		return err
	}
	if err := journal.validateContainmentAuthority(); err != nil {
		return err
	}
	if err := journal.validateContainmentHandoff(journal.hasClaimGrant()); err != nil {
		return err
	}
	if err := journal.validateContainmentCgroup(journal.hasClaimGrant()); err != nil {
		return err
	}
	if err := journal.validateContainmentScans(); err != nil {
		return err
	}
	if err := journal.validateContainmentCgroupReleaseProof(); err != nil {
		return err
	}
	if err := journal.validateContainmentCgroupPreGateReleaseProof(); err != nil {
		return err
	}
	if err := validateJournalLeaseTerminalAndGrantInvariants(journal); err != nil {
		return err
	}
	if err := validateLinuxCloneJournal(journal); err != nil {
		return err
	}
	if err := validatePendingJournalEntriesAndDeliveries(journal); err != nil {
		return err
	}
	if err := validateJournalNativeUsageObservation(journal); err != nil {
		return err
	}
	if err := validateJournalInputAndCommandIntents(journal); err != nil {
		return err
	}
	return nil
}

func validateJournalKeyScalarsAndProcessDetails(journal RunJournal) error {
	if err := validateKey(journal.Key()); err != nil {
		return err
	}
	if !validRequiredString(journal.RuntimeKey, 4096) || !validRequiredString(journal.RuntimeID, 4096) || journal.ClaimedRuntimeEpoch <= 0 || !validRequiredString(journal.ClaimID, 4096) || !validRequiredString(journal.LocalState, 256) || len(journal.WorkspacePath) > 32768 || !validRequiredString(journal.WorkspaceBindingKey, 4096) || journal.PID < 0 || len(journal.ProcessIdentity) > 4096 || journal.LastEventSequence < 0 {
		return errors.New("run journal is invalid")
	}
	if journal.PID == 0 && (!journal.StartedAt.IsZero() || journal.ProcessIdentity != "" || journal.ContainmentAuthority != nil) {
		return errors.New("run journal process details are invalid")
	}
	if journal.PID > 0 && (journal.StartedAt.IsZero() || !validRequiredString(journal.ProcessIdentity, 4096)) {
		return errors.New("run journal process details are invalid")
	}
	if journal.ContainmentUnproven && !journal.HasProcessDetails() && journal.ContainmentHandoff == nil &&
		journal.ContainmentScans == nil && journal.ContainmentCgroup == nil {
		return errors.New("run journal containment uncertainty is invalid")
	}
	return nil
}

func validateJournalLeaseTerminalAndGrantInvariants(journal RunJournal) error {
	if (strings.TrimSpace(journal.LeaseToken) == "") != journal.LeaseExpiresAt.IsZero() || len(journal.LeaseToken) > 65536 || !validWork(journal.Work) {
		return errors.New("run journal is invalid")
	}
	if !validTerminalState(journal) {
		return errors.New("run journal is invalid")
	}
	if journal.NativeUsageTerminalRecovery != nil && !journal.NativeUsageRecoveryRequired {
		return errors.New("run journal is invalid")
	}
	if journal.DroppedOutputChunks < 0 || journal.DroppedOutputBytes < 0 {
		return errors.New("run journal is invalid")
	}
	if !journal.hasClaimGrant() && (journal.PID != 0 || !journal.StartedAt.IsZero() || journal.ContainmentCgroup != nil ||
		journal.LinuxCloneIntent != nil || journal.LinuxCloneSeedReceipt != nil || journal.LinuxCloneCheckoutReceipt != nil ||
		journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil || journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil ||
		len(journal.PendingEvents) != 0 || journal.DroppedOutputChunks != 0 ||
		journal.DroppedOutputBytes != 0 || len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0) {
		return errors.New("run journal is invalid")
	}
	return nil
}

func validatePendingJournalEntriesAndDeliveries(journal RunJournal) error {
	if err := journal.validatePendingEntries(journal.RunID, journal.Fence()); err != nil {
		return err
	}
	if err := validateGoalDeliveries(journal); err != nil {
		return err
	}
	return nil
}

func validateJournalNativeUsageObservation(journal RunJournal) error {
	if journal.NativeUsageObservation != nil {
		if err := journal.NativeUsageObservation.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateJournalInputAndCommandIntents(journal RunJournal) error {
	if intent := journal.InputCommandIntent; intent != nil {
		if !validInputCommandIntent(*intent) || intent.EventSequenceBarrier > journal.LastEventSequence {
			return errors.New("run journal input command intent is invalid")
		}
		pending := false
		for _, acknowledgement := range journal.PendingCommandAcknowledgements {
			if acknowledgement.AckID == intent.AckID {
				pending = acknowledgement.CommandID == intent.CommandID && acknowledgement.Outcome == intent.Outcome
				break
			}
		}
		if intent.Outcome == "" && pending {
			return errors.New("unresolved input command has an acknowledgement")
		}
		if intent.Outcome != "" && intent.AcknowledgementDelivered == pending {
			return errors.New("input command acknowledgement delivery is invalid")
		}
	}
	if err := validateControlCommandIntents(journal); err != nil {
		return err
	}
	if err := validateProviderActionIntents(journal); err != nil {
		return err
	}
	return nil
}

func validTerminalState(journal RunJournal) bool {
	if !validNativeUsageTerminalRecovery(journal.NativeUsageTerminalRecovery) {
		return false
	}
	if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" {
		return !hasPendingTerminalTransition(journal.PendingTransitions) && journal.TerminalPendingAt.IsZero() && journal.TerminalState == "" && journal.TerminalTaskResultKind == "" && journal.TerminalIntentDigest == "" && journal.TerminalCommandDigest == "" && journal.TerminalSettlementDigest == "" && journal.TerminalVerdict == "" && journal.TerminalResolvedAt.IsZero()
	}
	if journal.TerminalPendingAt.IsZero() || !isTerminalTransitionState(journal.TerminalState) {
		return false
	}
	if journal.TerminalTaskResultKind != "" && !validTaskResultKind(journal.TerminalTaskResultKind) {
		return false
	}
	if journal.TerminalIntentDigest != "" && (len(journal.TerminalIntentDigest) != sha256.Size*2 || !validHex(journal.TerminalIntentDigest)) {
		return false
	}
	if journal.TerminalCommandDigest != "" && (len(journal.TerminalCommandDigest) != sha256.Size*2 || !validHex(journal.TerminalCommandDigest)) {
		return false
	}
	if journal.TerminalSettlementDigest != "" && (len(journal.TerminalSettlementDigest) != sha256.Size*2 || !validHex(journal.TerminalSettlementDigest)) {
		return false
	}
	if journal.LocalState == "cleanup_pending" {
		if !validTerminalVerdict(journal.TerminalVerdict) || journal.TerminalResolvedAt.IsZero() {
			return false
		}
		// Conclusive v1 terminal delivery may retire ordinary items while late
		// Goal usage/evidence remains retriable under its original fence.
		return len(journal.PendingEvents) == 0 && len(journal.PendingTransitions) == 0 && len(journal.PendingCommandAcknowledgements) == 0 && len(journal.AttemptedTransitionIDs) == 0
	}
	if journal.TerminalVerdict == "" {
		return journal.TerminalResolvedAt.IsZero()
	}
	return validTerminalVerdict(journal.TerminalVerdict) && !journal.TerminalResolvedAt.IsZero()
}

func terminalTaskResultKind(payload json.RawMessage) (protocol.TaskResultKind, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return "", fmt.Errorf("completed terminal payload is invalid: %w", err)
	}
	rawResult, present := envelope["task_result"]
	if !present {
		return "", nil
	}
	var result struct {
		Kind protocol.TaskResultKind `json:"kind"`
	}
	if err := json.Unmarshal(rawResult, &result); err != nil {
		return "", fmt.Errorf("completed terminal task result kind is invalid: %w", err)
	}
	if !validTaskResultKind(result.Kind) {
		return "", errors.New("completed terminal task result kind is invalid")
	}
	return result.Kind, nil
}

func validTaskResultKind(kind protocol.TaskResultKind) bool {
	switch kind {
	case protocol.TaskResultProgress,
		protocol.TaskResultCandidateCompletion,
		protocol.TaskResultBlocked,
		protocol.TaskResultRepairRequired,
		protocol.TaskResultReplanRequired,
		protocol.TaskResultFailed,
		protocol.TaskResultPlanProposed:
		return true
	default:
		return false
	}
}

func validTerminalVerdict(verdict string) bool {
	switch verdict {
	case TerminalVerdictAccepted, TerminalVerdictOwnershipLost, TerminalVerdictGraceExpired:
		return true
	default:
		return false
	}
}

func validConclusiveTerminalVerdict(verdict string) bool {
	return IsConclusiveTerminalVerdict(verdict)
}
