package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// GoalDeliveryKind identifies a machine-to-control-plane receipt that must
// survive daemon restart. It is intentionally separate from protocol v1's
// event/transition outbox because each Goal endpoint has its own receipt.
type GoalDeliveryKind string

const (
	GoalDeliverySessionAttach  GoalDeliveryKind = "session_attach"
	GoalDeliverySessionStopped GoalDeliveryKind = "session_stopped"
	GoalDeliveryEvidence       GoalDeliveryKind = "evidence"
	GoalDeliveryUsage          GoalDeliveryKind = "usage"
)

// GoalSessionAttachDelivery contains only the safe control-plane projection
// of a native session. Raw native handles and native filenames never belong
// in this record or in a control request.
type GoalSessionAttachDelivery struct {
	GoalID               string  `json:"goal_id"`
	LocalHandleID        string  `json:"local_handle_id"`
	BindingID            string  `json:"binding_id,omitempty"`
	ServerIssuedBinding  bool    `json:"server_issued_binding,omitempty"`
	HarnessKind          string  `json:"harness_kind"`
	HarnessVersion       string  `json:"harness_version"`
	AdapterVersion       string  `json:"adapter_version"`
	WorkspaceFingerprint string  `json:"workspace_fingerprint"`
	Workspace            string  `json:"workspace"`
	RepositoryResourceID *string `json:"repository_resource_id,omitempty"`
}

// GoalSessionStoppedDelivery is the safe proof needed to release one
// retained native attachment after its terminal transition is acknowledged.
type GoalSessionStoppedDelivery struct {
	SessionID     string `json:"session_id"`
	LocalHandleID string `json:"local_handle_id"`
	BindingID     string `json:"binding_id"`
}

// GoalDelivery is one immutable outbox record. DeliveryID is the receiver's
// stable idempotency identity: local_handle_id, evidence_key, or usage_key.
// PayloadDigest covers the kind, ID, full fence, and typed body.
type GoalDelivery struct {
	Kind           GoalDeliveryKind            `json:"kind"`
	DeliveryID     string                      `json:"delivery_id"`
	PayloadDigest  string                      `json:"payload_digest"`
	Fence          protocol.Fence              `json:"fence"`
	Ready          bool                        `json:"ready,omitempty"`
	SessionAttach  *GoalSessionAttachDelivery  `json:"session_attach,omitempty"`
	SessionStopped *GoalSessionStoppedDelivery `json:"session_stopped,omitempty"`
	Evidence       *protocol.Evidence          `json:"evidence,omitempty"`
	Usage          *protocol.Usage             `json:"usage,omitempty"`
}

// GoalDeliveryRetirement preserves a definitive receiver rejection without
// treating it as successful delivery or silently discarding the request.
type GoalDeliveryRetirement struct {
	Delivery   GoalDelivery `json:"delivery"`
	StatusCode int          `json:"status_code"`
	Code       string       `json:"code"`
	Message    string       `json:"message"`
	RetiredAt  time.Time    `json:"retired_at"`
}

// LateGoalUsageLedger persists the original fence and usage accounting after
// execution cleanup. It cannot recreate a RunJournal or native execution.
type LateGoalUsageLedger struct {
	SchemaVersion          int                      `json:"schema_version"`
	RunID                  string                   `json:"run_id"`
	Generation             int64                    `json:"generation"`
	Fence                  protocol.Fence           `json:"fence"`
	PendingUsageDeliveries []GoalDelivery           `json:"pending_usage_deliveries,omitempty"`
	DeliveredDeliveries    []GoalDelivery           `json:"delivered_deliveries,omitempty"`
	RetiredDeliveries      []GoalDeliveryRetirement `json:"retired_deliveries,omitempty"`
	CreatedAt              time.Time                `json:"created_at"`
	UpdatedAt              time.Time                `json:"updated_at"`
}

const lateGoalUsageLedgerSchemaVersion = 1

var ErrGoalDeliveryConflict = errors.New("goal delivery conflicts with pending receipt")

// QueueGoalSessionAttach records the control-plane attach intent before the
// native launch begins. Call MarkGoalSessionAttachDeliveryReady only after the
// local native handle has been durably persisted.
func (store *Store) QueueGoalSessionAttach(key RunKey, payload GoalSessionAttachDelivery) (RunJournal, error) {
	if (payload.BindingID == "" && !payload.ServerIssuedBinding) || (payload.BindingID != "" && payload.ServerIssuedBinding) {
		return RunJournal{}, errors.New("Goal session attach binding authority is invalid")
	}
	if payload.BindingID != "" && !validGoalSessionUUID(payload.BindingID) {
		return RunJournal{}, errors.New("Goal session attach binding ID is invalid")
	}
	if _, err := store.LoadLateGoalUsage(key); err == nil {
		return RunJournal{}, ErrGoalDeliveryConflict
	} else if !IsNotFound(err) {
		return RunJournal{}, err
	}
	journal, err := store.queueGoalDelivery(key, GoalDelivery{Kind: GoalDeliverySessionAttach, DeliveryID: payload.LocalHandleID, SessionAttach: &payload})
	if !IsNotFound(err) {
		return journal, err
	}
	if _, lateErr := store.LoadLateGoalUsage(key); lateErr == nil {
		return RunJournal{}, ErrGoalDeliveryConflict
	} else if !IsNotFound(lateErr) {
		return RunJournal{}, lateErr
	}
	return RunJournal{}, err
}

// QueueGoalSessionStopped persists an exact stop receipt before it can be
// delivered. The app holds it until the terminal transition is acknowledged.
func (store *Store) QueueGoalSessionStopped(key RunKey, payload GoalSessionStoppedDelivery) (RunJournal, error) {
	return store.queueGoalDelivery(key, GoalDelivery{Kind: GoalDeliverySessionStopped, DeliveryID: payload.BindingID, SessionStopped: &payload, Ready: true})
}

// MarkGoalSessionAttachDeliveryReady records the post-handle-persistence
// barrier. It is idempotent and never changes the immutable request body.
func (store *Store) MarkGoalSessionAttachDeliveryReady(key RunKey, localHandleID string) (RunJournal, error) {
	if !validRequiredString(localHandleID, 4096) {
		return RunJournal{}, errors.New("Goal session attach delivery ID is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		for index := range journal.PendingGoalDeliveries {
			delivery := &journal.PendingGoalDeliveries[index]
			if delivery.Kind == GoalDeliverySessionAttach && delivery.DeliveryID == localHandleID {
				if delivery.Ready {
					return nil
				}
				if err := store.validateGoalSessionAttachDeliveryReadyLocked(*journal, *delivery, localHandleID); err != nil {
					return err
				}
				delivery.Ready = true
				return nil
			}
		}
		return errors.New("Goal session attach delivery is not pending")
	})
}

func (store *Store) validateGoalSessionAttachDeliveryReadyLocked(journal RunJournal, delivery GoalDelivery, localHandleID string) error {
	payload := delivery.SessionAttach
	if payload == nil || delivery.DeliveryID != localHandleID || payload.LocalHandleID != localHandleID || !validRequiredString(payload.GoalID, 4096) {
		return ErrGoalDeliveryConflict
	}
	session, err := store.loadGoalSessionPathLocked(store.goalSessionPath(GoalSessionKey{GoalID: payload.GoalID, LocalHandleID: localHandleID}))
	if err != nil {
		return fmt.Errorf("load Goal session for attach delivery: %w", err)
	}
	if session.GoalID != payload.GoalID || session.LocalHandleID != localHandleID ||
		session.RunID != journal.RunID || session.Generation != journal.Generation ||
		session.RuntimeID != journal.RuntimeID || session.RuntimeEpoch != journal.ClaimedRuntimeEpoch ||
		session.HarnessKind != payload.HarnessKind || session.HarnessVersion != payload.HarnessVersion ||
		session.AdapterVersion != payload.AdapterVersion || session.WorkspaceFingerprint != payload.WorkspaceFingerprint ||
		!sameGoalSessionAttachRepositoryResource(session.RepositoryResourceID, payload.RepositoryResourceID) {
		return ErrGoalDeliveryConflict
	}
	if session.NeedsReconciliation() {
		return ErrGoalSessionUncertain
	}
	if session.LaunchState != GoalSessionLaunchStateAttached || validateGoalSessionHandle(GoalSessionHandle{
		NativeSessionID:       session.NativeSessionID,
		NativeSessionFilename: session.NativeSessionFilename,
	}) != nil {
		return ErrGoalSessionNotAttached
	}
	if session.SessionMode == GoalSessionModeResume || !payload.ServerIssuedBinding {
		if session.BindingID != payload.BindingID {
			return ErrGoalDeliveryConflict
		}
		return nil
	}
	if session.BindingID != "" {
		return ErrGoalDeliveryConflict
	}
	return nil
}

func sameGoalSessionAttachRepositoryResource(sessionResourceID string, deliveryResourceID *string) bool {
	if deliveryResourceID == nil {
		return sessionResourceID == ""
	}
	return sessionResourceID == *deliveryResourceID
}

// DiscardUnreadyGoalSessionAttachDelivery removes an attach intent only before
// native Start has been invoked. Once Ready is true, its receiver may have
// observed the request and it must remain available for exact replay.
func (store *Store) DiscardUnreadyGoalSessionAttachDelivery(key RunKey, localHandleID string) (RunJournal, error) {
	if !validRequiredString(localHandleID, 4096) {
		return RunJournal{}, errors.New("Goal session attach delivery ID is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		for index, delivery := range journal.PendingGoalDeliveries {
			if delivery.Kind != GoalDeliverySessionAttach || delivery.DeliveryID != localHandleID {
				continue
			}
			if delivery.Ready {
				return ErrGoalDeliveryConflict
			}
			journal.PendingGoalDeliveries = append(journal.PendingGoalDeliveries[:index], journal.PendingGoalDeliveries[index+1:]...)
			return nil
		}
		return errors.New("Goal session attach delivery is not pending")
	})
}

// QueueGoalEvidence records a caller-supplied, already validated receipt. It
// deliberately does not derive evidence from native output or TaskResult refs.
func (store *Store) QueueGoalEvidence(key RunKey, evidence protocol.Evidence) (RunJournal, error) {
	if _, err := store.LoadLateGoalUsage(key); err == nil {
		return RunJournal{}, ErrGoalDeliveryConflict
	} else if !IsNotFound(err) {
		return RunJournal{}, err
	}
	journal, err := store.queueGoalDelivery(key, GoalDelivery{Kind: GoalDeliveryEvidence, DeliveryID: evidence.EvidenceKey, Evidence: &evidence, Ready: true})
	if !IsNotFound(err) {
		return journal, err
	}
	if _, lateErr := store.LoadLateGoalUsage(key); lateErr == nil {
		return RunJournal{}, ErrGoalDeliveryConflict
	} else if !IsNotFound(lateErr) {
		return RunJournal{}, lateErr
	}
	return RunJournal{}, err
}

// QueueGoalEvidenceAndTerminalTransition atomically records a non-empty,
// fixed evidence set together with the terminal transition whose semantic
// payload references exactly those evidence IDs. A terminal already present
// in the journal may only be replayed with the exact same transition and
// complete evidence bodies; it can never acquire new evidence.
func (store *Store) QueueGoalEvidenceAndTerminalTransition(key RunKey, evidence []protocol.Evidence, transition protocol.StateTransitionRequest, pendingAt time.Time) (RunJournal, error) {
	if len(evidence) == 0 {
		return RunJournal{}, errors.New("Goal evidence set must not be empty")
	}
	if pendingAt.IsZero() {
		return RunJournal{}, errors.New("terminal pending time is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		prepared, err := prepareTransition(journal, transition)
		if err != nil {
			return err
		}
		if !isTerminalTransitionState(prepared.State) {
			return errors.New("terminal transition state is invalid")
		}

		// Resolve an existing terminal before validating or appending any new
		// delivery. This preserves the first authoritative terminal and makes
		// cancellation races fail closed without adding required evidence.
		if existing, present := authoritativeTerminalTransition(journal); present {
			if existing == nil || !sameStateTransition(*existing, prepared) {
				return ErrGoalDeliveryConflict
			}
			refs, err := terminalEvidenceRefs(prepared.Payload)
			if err != nil {
				return err
			}
			preparedDeliveries, err := prepareGoalEvidenceDeliveries(journal, evidence)
			if err != nil {
				return err
			}
			if !sameEvidenceIDSet(refs, preparedDeliveries) {
				return errors.New("terminal transition evidence references do not match deliveries")
			}
			for _, delivery := range preparedDeliveries {
				if err := requireExactGoalEvidenceDelivery(*journal, delivery); err != nil {
					return err
				}
			}
			return nil
		}

		refs, err := terminalEvidenceRefs(prepared.Payload)
		if err != nil {
			return err
		}
		preparedDeliveries, err := prepareGoalEvidenceDeliveries(journal, evidence)
		if err != nil {
			return err
		}
		if !sameEvidenceIDSet(refs, preparedDeliveries) {
			return errors.New("terminal transition evidence references do not match deliveries")
		}
		for _, delivery := range preparedDeliveries {
			if err := queueGoalDeliveryOnJournal(journal, delivery); err != nil {
				return err
			}
		}
		return queueTerminalTransition(journal, prepared, pendingAt)
	})
}

// QueueGoalUsage records caller-supplied normalized accounting. It remains
// pending after terminal transitions so late accounting cannot be lost.
func (store *Store) QueueGoalUsage(key RunKey, usage protocol.Usage) (RunJournal, error) {
	if _, err := store.LoadLateGoalUsage(key); err == nil {
		if _, err := store.QueueLateGoalUsage(key, usage); err != nil {
			return RunJournal{}, err
		}
		return RunJournal{RunID: key.RunID, Generation: key.Generation}, nil
	} else if !IsNotFound(err) {
		return RunJournal{}, err
	}
	journal, err := store.queueGoalDelivery(key, GoalDelivery{Kind: GoalDeliveryUsage, DeliveryID: usage.UsageKey, Usage: &usage, Ready: true})
	if !IsNotFound(err) {
		return journal, err
	}
	if _, lateErr := store.QueueLateGoalUsage(key, usage); lateErr != nil {
		return RunJournal{}, lateErr
	}
	return RunJournal{RunID: key.RunID, Generation: key.Generation}, nil
}

// QueueNativeUsageRecovery atomically retains the accounting-recovery barrier
// and the exact native usage delivery. The barrier may only be cleared after
// this immutable body is present in the journal delivery history.
func (store *Store) QueueNativeUsageRecovery(key RunKey, usage protocol.Usage) (RunJournal, error) {
	if err := usage.Validate(); err != nil {
		return RunJournal{}, err
	}
	if usage.RunID != key.RunID {
		return RunJournal{}, errors.New("native usage recovery run ID does not match journal")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("run journal has no claim grant")
		}
		journal.NativeUsageRecoveryRequired = true
		delivery := GoalDelivery{Kind: GoalDeliveryUsage, DeliveryID: usage.UsageKey, Fence: journal.Fence(), Usage: &usage, Ready: true}
		if err := prepareGoalDelivery(journal.RunID, &delivery); err != nil {
			return err
		}
		for _, pending := range journal.PendingGoalDeliveries {
			if pending.Kind != delivery.Kind || pending.DeliveryID != delivery.DeliveryID {
				continue
			}
			if pending.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(pending, delivery) {
				return nil
			}
			return ErrGoalDeliveryConflict
		}
		for _, retired := range journal.RetiredGoalDeliveries {
			if retired.Delivery.Kind == delivery.Kind && retired.Delivery.DeliveryID == delivery.DeliveryID {
				return ErrGoalDeliveryConflict
			}
		}
		for _, delivered := range journal.DeliveredGoalDeliveries {
			if delivered.Kind != delivery.Kind || delivered.DeliveryID != delivery.DeliveryID {
				continue
			}
			if delivered.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(delivered, delivery) {
				return nil
			}
			return ErrGoalDeliveryConflict
		}
		journal.PendingGoalDeliveries = append(journal.PendingGoalDeliveries, delivery)
		journal.GoalDeliveryEnabled = true
		return nil
	})
}

func (store *Store) queueGoalDelivery(key RunKey, delivery GoalDelivery) (RunJournal, error) {
	if err := validateKey(key); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return queueGoalDeliveryOnJournal(journal, delivery)
	})
}

func queueGoalDeliveryOnJournal(journal *RunJournal, delivery GoalDelivery) error {
	if !journal.hasClaimGrant() {
		return errors.New("run journal has no claim grant")
	}
	delivery.Fence = journal.Fence()
	if err := prepareGoalDelivery(journal.RunID, &delivery); err != nil {
		return err
	}
	if err := rejectGoalEvidenceIdentityConflict(*journal, delivery); err != nil {
		return err
	}
	for _, pending := range journal.PendingGoalDeliveries {
		if pending.Kind != delivery.Kind || pending.DeliveryID != delivery.DeliveryID {
			continue
		}
		if pending.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(pending, delivery) {
			return nil
		}
		return ErrGoalDeliveryConflict
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if retired.Delivery.Kind == delivery.Kind && retired.Delivery.DeliveryID == delivery.DeliveryID {
			return ErrGoalDeliveryConflict
		}
	}
	for _, delivered := range journal.DeliveredGoalDeliveries {
		if delivered.Kind == delivery.Kind && delivered.DeliveryID == delivery.DeliveryID {
			if delivered.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(delivered, delivery) {
				return nil
			}
			return ErrGoalDeliveryConflict
		}
	}
	journal.PendingGoalDeliveries = append(journal.PendingGoalDeliveries, delivery)
	journal.GoalDeliveryEnabled = true
	return nil
}

func authoritativeTerminalTransition(journal *RunJournal) (*protocol.StateTransitionRequest, bool) {
	for index := range journal.PendingTransitions {
		if isTerminalTransitionState(journal.PendingTransitions[index].State) {
			return &journal.PendingTransitions[index], true
		}
	}
	if isTerminalTransitionState(journal.TerminalState) {
		// Once the transition has been acknowledged, only TerminalState remains
		// in the journal. Its original body is unavailable, so no new evidence
		// can be attached to that authoritative terminal.
		return nil, true
	}
	return nil, false
}

func sameStateTransition(left, right protocol.StateTransitionRequest) bool {
	return left.Fence == right.Fence && left.TransitionID == right.TransitionID && left.State == right.State && bytes.Equal(left.Payload, right.Payload)
}

func prepareGoalEvidenceDeliveries(journal *RunJournal, evidence []protocol.Evidence) ([]GoalDelivery, error) {
	if len(evidence) == 0 {
		return nil, errors.New("Goal evidence set must not be empty")
	}
	seenKeys := make(map[string]struct{}, len(evidence))
	seenIDs := make(map[string]struct{}, len(evidence))
	deliveries := make([]GoalDelivery, 0, len(evidence))
	for index := range evidence {
		item := evidence[index]
		if _, exists := seenKeys[item.EvidenceKey]; exists {
			return nil, errors.New("Goal evidence set contains a duplicate evidence key")
		}
		if _, exists := seenIDs[item.EvidenceID]; exists {
			return nil, errors.New("Goal evidence set contains a duplicate evidence ID")
		}
		seenKeys[item.EvidenceKey] = struct{}{}
		seenIDs[item.EvidenceID] = struct{}{}
		delivery := GoalDelivery{Kind: GoalDeliveryEvidence, DeliveryID: item.EvidenceKey, Fence: journal.Fence(), Evidence: &item, Ready: true}
		if err := prepareGoalDelivery(journal.RunID, &delivery); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func sameEvidenceIDSet(refs []string, deliveries []GoalDelivery) bool {
	if len(refs) != len(deliveries) {
		return false
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if _, exists := seen[ref]; exists {
			return false
		}
		seen[ref] = struct{}{}
	}
	for _, delivery := range deliveries {
		if delivery.Evidence == nil {
			return false
		}
		if _, exists := seen[delivery.Evidence.EvidenceID]; !exists {
			return false
		}
		delete(seen, delivery.Evidence.EvidenceID)
	}
	return len(seen) == 0
}

func requireExactGoalEvidenceDelivery(journal RunJournal, candidate GoalDelivery) error {
	for _, existing := range journal.PendingGoalDeliveries {
		matched, err := compareGoalEvidenceDelivery(existing, candidate)
		if err != nil {
			return err
		}
		if matched {
			return nil
		}
	}
	for _, existing := range journal.DeliveredGoalDeliveries {
		matched, err := compareGoalEvidenceDelivery(existing, candidate)
		if err != nil {
			return err
		}
		if matched {
			return nil
		}
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		matched, err := compareGoalEvidenceDelivery(retired.Delivery, candidate)
		if err != nil {
			return err
		}
		if matched {
			return ErrGoalDeliveryConflict
		}
	}
	return errors.New("required Goal evidence delivery is not durable")
}

func compareGoalEvidenceDelivery(existing, candidate GoalDelivery) (bool, error) {
	if existing.Kind != GoalDeliveryEvidence || existing.Evidence == nil || candidate.Kind != GoalDeliveryEvidence || candidate.Evidence == nil {
		return false, nil
	}
	if existing.DeliveryID != candidate.DeliveryID && existing.Evidence.EvidenceID != candidate.Evidence.EvidenceID {
		return false, nil
	}
	if existing.PayloadDigest == candidate.PayloadDigest && equalGoalDelivery(existing, candidate) {
		return true, nil
	}
	return false, ErrGoalDeliveryConflict
}

func rejectGoalEvidenceIdentityConflict(journal RunJournal, candidate GoalDelivery) error {
	if candidate.Kind != GoalDeliveryEvidence || candidate.Evidence == nil {
		return nil
	}
	for _, existing := range journal.PendingGoalDeliveries {
		if _, err := compareGoalEvidenceDelivery(existing, candidate); err != nil {
			return err
		}
	}
	for _, existing := range journal.DeliveredGoalDeliveries {
		if _, err := compareGoalEvidenceDelivery(existing, candidate); err != nil {
			return err
		}
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if _, err := compareGoalEvidenceDelivery(retired.Delivery, candidate); err != nil {
			return err
		}
	}
	return nil
}

func terminalEvidenceRefs(payload json.RawMessage) ([]string, error) {
	if len(payload) == 0 {
		return nil, errors.New("terminal transition evidence references are missing")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope == nil {
		return nil, errors.New("terminal transition payload is invalid")
	}
	refs, direct := envelope["evidence_refs"]
	taskResult, nested := envelope["task_result"]
	if direct && nested {
		return nil, errors.New("terminal transition evidence references are ambiguous")
	}
	if nested {
		var task map[string]json.RawMessage
		if err := json.Unmarshal(taskResult, &task); err != nil || task == nil {
			return nil, errors.New("terminal task result is invalid")
		}
		var ok bool
		refs, ok = task["evidence_refs"]
		if !ok {
			return nil, errors.New("terminal transition evidence references are missing")
		}
	}
	if !direct && !nested {
		return nil, errors.New("terminal transition evidence references are missing")
	}
	var values []string
	if err := json.Unmarshal(refs, &values); err != nil || len(values) == 0 {
		return nil, errors.New("terminal transition evidence references are invalid")
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validRequiredString(value, 4096) {
			return nil, errors.New("terminal transition evidence reference is invalid")
		}
		if _, exists := seen[value]; exists {
			return nil, errors.New("terminal transition evidence references are duplicated")
		}
		seen[value] = struct{}{}
	}
	return values, nil
}

// RetireGoalDelivery atomically records a conclusive HTTP rejection and lets
// unrelated terminal delivery proceed without retrying a known failure.
func (store *Store) RetireGoalDelivery(key RunKey, kind GoalDeliveryKind, deliveryID, payloadDigest string, statusCode int, code, message string, retiredAt time.Time) (RunJournal, error) {
	if !validGoalDeliveryKind(kind) || !validRequiredString(deliveryID, 4096) || !validGoalDeliveryDigest(payloadDigest) || statusCode < 400 || statusCode > 599 || !validRequiredString(code, 256) || !validRequiredString(message, 4096) || retiredAt.IsZero() {
		return RunJournal{}, errors.New("Goal delivery retirement is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		for index, pending := range journal.PendingGoalDeliveries {
			if pending.Kind != kind || pending.DeliveryID != deliveryID {
				continue
			}
			if pending.PayloadDigest != payloadDigest {
				return ErrGoalDeliveryConflict
			}
			journal.PendingGoalDeliveries = append(journal.PendingGoalDeliveries[:index], journal.PendingGoalDeliveries[index+1:]...)
			journal.RetiredGoalDeliveries = append(journal.RetiredGoalDeliveries, GoalDeliveryRetirement{Delivery: pending, StatusCode: statusCode, Code: code, Message: message, RetiredAt: retiredAt.UTC()})
			return nil
		}
		return errors.New("Goal delivery is not pending")
	})
}

// MarkGoalDeliveryDelivered moves exactly the immutable receipt acknowledged by
// the control plane into delivered history. A changed/replaced record is never
// removed after an ambiguous request result.
func (store *Store) MarkGoalDeliveryDelivered(key RunKey, kind GoalDeliveryKind, deliveryID, payloadDigest string) (RunJournal, error) {
	if !validGoalDeliveryKind(kind) || !validRequiredString(deliveryID, 4096) || !validGoalDeliveryDigest(payloadDigest) {
		return RunJournal{}, errors.New("Goal delivery receipt is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		for _, delivered := range journal.DeliveredGoalDeliveries {
			if delivered.Kind != kind || delivered.DeliveryID != deliveryID {
				continue
			}
			if delivered.PayloadDigest != payloadDigest {
				return ErrGoalDeliveryConflict
			}
			return nil
		}
		for index, pending := range journal.PendingGoalDeliveries {
			if pending.Kind != kind || pending.DeliveryID != deliveryID {
				continue
			}
			if pending.PayloadDigest != payloadDigest {
				return ErrGoalDeliveryConflict
			}
			if !pending.Ready {
				return errors.New("Goal delivery was not ready for delivery")
			}
			journal.PendingGoalDeliveries = append(journal.PendingGoalDeliveries[:index], journal.PendingGoalDeliveries[index+1:]...)
			journal.DeliveredGoalDeliveries = append(journal.DeliveredGoalDeliveries, pending)
			return nil
		}
		return errors.New("Goal delivery is not pending")
	})
}

// HasPendingGoalDeliveries reports whether the run cannot be safely removed.
func (journal RunJournal) HasPendingGoalDeliveries() bool {
	return len(journal.PendingGoalDeliveries) != 0
}

func prepareGoalDelivery(runID string, delivery *GoalDelivery) error {
	if delivery.Fence == (protocol.Fence{}) {
		return errors.New("Goal delivery fence is invalid")
	}
	if !validGoalDeliveryKind(delivery.Kind) || !validRequiredString(delivery.DeliveryID, 4096) {
		return errors.New("Goal delivery is invalid")
	}
	switch delivery.Kind {
	case GoalDeliverySessionAttach:
		if delivery.SessionAttach == nil || delivery.SessionStopped != nil || delivery.Evidence != nil || delivery.Usage != nil || delivery.DeliveryID != delivery.SessionAttach.LocalHandleID {
			return errors.New("Goal session attach delivery is invalid")
		}
		if err := validateGoalSessionAttachDelivery(*delivery.SessionAttach); err != nil {
			return err
		}
	case GoalDeliverySessionStopped:
		if delivery.SessionAttach != nil || delivery.SessionStopped == nil || delivery.Evidence != nil || delivery.Usage != nil || !delivery.Ready || delivery.DeliveryID != delivery.SessionStopped.BindingID {
			return errors.New("Goal session stopped delivery is invalid")
		}
		if err := validateGoalSessionStoppedDelivery(*delivery.SessionStopped); err != nil {
			return err
		}
	case GoalDeliveryEvidence:
		if delivery.SessionAttach != nil || delivery.SessionStopped != nil || delivery.Evidence == nil || delivery.Usage != nil || !delivery.Ready || delivery.DeliveryID != delivery.Evidence.EvidenceKey || delivery.Evidence.RunID != runID {
			return errors.New("Goal evidence delivery is invalid")
		}
		if err := delivery.Evidence.Validate(); err != nil {
			return fmt.Errorf("Goal evidence delivery: %w", err)
		}
	case GoalDeliveryUsage:
		if delivery.SessionAttach != nil || delivery.SessionStopped != nil || delivery.Evidence != nil || delivery.Usage == nil || !delivery.Ready || delivery.DeliveryID != delivery.Usage.UsageKey || delivery.Usage.RunID != runID {
			return errors.New("Goal usage delivery is invalid")
		}
		if err := delivery.Usage.Validate(); err != nil {
			return fmt.Errorf("Goal usage delivery: %w", err)
		}
	}
	digest, err := goalDeliveryDigest(*delivery)
	if err != nil {
		return err
	}
	if delivery.PayloadDigest != "" && delivery.PayloadDigest != digest {
		return ErrGoalDeliveryConflict
	}
	delivery.PayloadDigest = digest
	return nil
}

func validateGoalDeliveries(journal RunJournal) error {
	seen := make(map[string]struct{}, len(journal.PendingGoalDeliveries))
	for index := range journal.PendingGoalDeliveries {
		delivery := &journal.PendingGoalDeliveries[index]
		if !sameFence(delivery.Fence, journal.Fence()) {
			return errors.New("Goal delivery fence does not match journal")
		}
		if err := validatePersistedGoalDelivery(journal.RunID, delivery); err != nil {
			return err
		}
		identity := string(delivery.Kind) + "\x00" + delivery.DeliveryID
		if _, duplicate := seen[identity]; duplicate {
			return errors.New("Goal delivery is duplicated")
		}
		seen[identity] = struct{}{}
	}
	for index := range journal.DeliveredGoalDeliveries {
		delivery := &journal.DeliveredGoalDeliveries[index]
		if !delivery.Ready || !sameFence(delivery.Fence, journal.Fence()) {
			return errors.New("Goal delivered record is invalid")
		}
		if err := validatePersistedGoalDelivery(journal.RunID, delivery); err != nil {
			return err
		}
		identity := string(delivery.Kind) + "\x00" + delivery.DeliveryID
		if _, duplicate := seen[identity]; duplicate {
			return errors.New("Goal delivery is duplicated")
		}
		seen[identity] = struct{}{}
	}
	for index := range journal.RetiredGoalDeliveries {
		retired := &journal.RetiredGoalDeliveries[index]
		if err := validateGoalDeliveryRetirement(journal.RunID, journal.Fence(), retired); err != nil {
			return err
		}
		identity := string(retired.Delivery.Kind) + "\x00" + retired.Delivery.DeliveryID
		if _, duplicate := seen[identity]; duplicate {
			return errors.New("Goal delivery is duplicated")
		}
		seen[identity] = struct{}{}
	}
	if !journal.GoalDeliveryEnabled && (len(journal.PendingGoalDeliveries) != 0 || len(journal.DeliveredGoalDeliveries) != 0 || len(journal.RetiredGoalDeliveries) != 0) {
		return errors.New("Goal delivery marker is invalid")
	}
	return nil
}

func validateGoalDeliveryRetirement(runID string, fence protocol.Fence, retired *GoalDeliveryRetirement) error {
	if retired == nil || retired.StatusCode < 400 || retired.StatusCode > 599 || !validRequiredString(retired.Code, 256) || !validRequiredString(retired.Message, 4096) || retired.RetiredAt.IsZero() || !sameFence(retired.Delivery.Fence, fence) {
		return errors.New("Goal delivery retirement is invalid")
	}
	return validatePersistedGoalDelivery(runID, &retired.Delivery)
}

func validatePersistedGoalDelivery(runID string, delivery *GoalDelivery) error {
	if delivery == nil || !validGoalDeliveryDigest(delivery.PayloadDigest) {
		return errors.New("Goal delivery payload digest is invalid")
	}
	persistedDigest := delivery.PayloadDigest
	prepared := *delivery
	prepared.PayloadDigest = ""
	if err := prepareGoalDelivery(runID, &prepared); err != nil {
		return err
	}
	if prepared.PayloadDigest != persistedDigest {
		return ErrGoalDeliveryConflict
	}
	return nil
}

// QueueLateGoalUsage records final accounting under a fence retained during
// execution cleanup. It never creates a RunJournal.
func (store *Store) QueueLateGoalUsage(key RunKey, usage protocol.Usage) (LateGoalUsageLedger, error) {
	if err := validateKey(key); err != nil {
		return LateGoalUsageLedger{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return LateGoalUsageLedger{}, err
	}
	ledger, err := store.loadLateGoalUsageLocked(key)
	if err != nil {
		return LateGoalUsageLedger{}, err
	}
	delivery := GoalDelivery{Kind: GoalDeliveryUsage, DeliveryID: usage.UsageKey, Fence: ledger.Fence, Ready: true, Usage: &usage}
	if err := prepareGoalDelivery(ledger.RunID, &delivery); err != nil {
		return LateGoalUsageLedger{}, err
	}
	for _, pending := range ledger.PendingUsageDeliveries {
		if pending.DeliveryID != delivery.DeliveryID {
			continue
		}
		if pending.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(pending, delivery) {
			return ledger, nil
		}
		return LateGoalUsageLedger{}, ErrGoalDeliveryConflict
	}
	for _, retired := range ledger.RetiredDeliveries {
		if retired.Delivery.DeliveryID == delivery.DeliveryID {
			return LateGoalUsageLedger{}, ErrGoalDeliveryConflict
		}
	}
	for _, delivered := range ledger.DeliveredDeliveries {
		if delivered.DeliveryID == delivery.DeliveryID {
			if delivered.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(delivered, delivery) {
				return ledger, nil
			}
			return LateGoalUsageLedger{}, ErrGoalDeliveryConflict
		}
	}
	ledger.PendingUsageDeliveries = append(ledger.PendingUsageDeliveries, delivery)
	ledger.UpdatedAt = time.Now().UTC()
	if err := store.saveLateGoalUsageLocked(ledger); err != nil {
		return LateGoalUsageLedger{}, err
	}
	return ledger, nil
}

func (store *Store) LoadLateGoalUsage(key RunKey) (LateGoalUsageLedger, error) {
	if err := validateKey(key); err != nil {
		return LateGoalUsageLedger{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return LateGoalUsageLedger{}, err
	}
	return store.loadLateGoalUsageLocked(key)
}

func (store *Store) ListLateGoalUsage() ([]LateGoalUsageLedger, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return nil, err
	}
	if err := store.ensureLateGoalUsageDirLocked(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(store.lateGoalUsageDir())
	if err != nil {
		return nil, errors.New("list late Goal usage ledgers")
	}
	result := make([]LateGoalUsageLedger, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isLateGoalUsageFile(entry.Name()) {
			continue
		}
		ledger, err := store.loadLateGoalUsagePathLocked(filepath.Join(store.lateGoalUsageDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		result = append(result, ledger)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].RunID < result[right].RunID || (result[left].RunID == result[right].RunID && result[left].Generation < result[right].Generation)
	})
	return result, nil
}

func (store *Store) MarkLateGoalUsageDelivered(key RunKey, deliveryID, payloadDigest string) (LateGoalUsageLedger, error) {
	return store.mutateLateGoalUsage(key, func(ledger *LateGoalUsageLedger) error {
		for _, delivered := range ledger.DeliveredDeliveries {
			if delivered.DeliveryID != deliveryID {
				continue
			}
			if delivered.PayloadDigest != payloadDigest {
				return ErrGoalDeliveryConflict
			}
			return nil
		}
		for index, delivery := range ledger.PendingUsageDeliveries {
			if delivery.DeliveryID != deliveryID {
				continue
			}
			if delivery.PayloadDigest != payloadDigest {
				return ErrGoalDeliveryConflict
			}
			ledger.PendingUsageDeliveries = append(ledger.PendingUsageDeliveries[:index], ledger.PendingUsageDeliveries[index+1:]...)
			ledger.DeliveredDeliveries = append(ledger.DeliveredDeliveries, delivery)
			return nil
		}
		return errors.New("late Goal usage delivery is not pending")
	})
}

func (store *Store) RetireLateGoalUsage(key RunKey, deliveryID, payloadDigest string, statusCode int, code, message string, retiredAt time.Time) (LateGoalUsageLedger, error) {
	return store.mutateLateGoalUsage(key, func(ledger *LateGoalUsageLedger) error {
		if statusCode < 400 || statusCode > 599 || !validRequiredString(code, 256) || !validRequiredString(message, 4096) || retiredAt.IsZero() {
			return errors.New("late Goal usage retirement is invalid")
		}
		for index, delivery := range ledger.PendingUsageDeliveries {
			if delivery.DeliveryID != deliveryID {
				continue
			}
			if delivery.PayloadDigest != payloadDigest {
				return ErrGoalDeliveryConflict
			}
			ledger.PendingUsageDeliveries = append(ledger.PendingUsageDeliveries[:index], ledger.PendingUsageDeliveries[index+1:]...)
			ledger.RetiredDeliveries = append(ledger.RetiredDeliveries, GoalDeliveryRetirement{Delivery: delivery, StatusCode: statusCode, Code: code, Message: message, RetiredAt: retiredAt.UTC()})
			return nil
		}
		return errors.New("late Goal usage delivery is not pending")
	})
}

func (store *Store) archiveLateGoalUsageLocked(journal RunJournal) error {
	retiredUsage := make([]GoalDeliveryRetirement, 0, len(journal.RetiredGoalDeliveries))
	for _, retired := range journal.RetiredGoalDeliveries {
		if retired.Delivery.Kind == GoalDeliveryUsage {
			retiredUsage = append(retiredUsage, retired)
		}
	}
	deliveredUsage := make([]GoalDelivery, 0, len(journal.DeliveredGoalDeliveries))
	for _, delivered := range journal.DeliveredGoalDeliveries {
		if delivered.Kind == GoalDeliveryUsage {
			deliveredUsage = append(deliveredUsage, delivered)
		}
	}
	ledger := LateGoalUsageLedger{SchemaVersion: lateGoalUsageLedgerSchemaVersion, RunID: journal.RunID, Generation: journal.Generation, Fence: journal.Fence(), DeliveredDeliveries: deliveredUsage, RetiredDeliveries: retiredUsage, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if existing, err := store.loadLateGoalUsageLocked(journal.Key()); err == nil {
		if existing.Fence != ledger.Fence {
			return ErrGoalDeliveryConflict
		}
		if len(existing.RetiredDeliveries) == 0 && len(ledger.RetiredDeliveries) != 0 {
			existing.RetiredDeliveries = ledger.RetiredDeliveries
			existing.UpdatedAt = time.Now().UTC()
		}
		if len(existing.DeliveredDeliveries) == 0 && len(ledger.DeliveredDeliveries) != 0 {
			existing.DeliveredDeliveries = ledger.DeliveredDeliveries
			existing.UpdatedAt = time.Now().UTC()
		}
		if len(existing.RetiredDeliveries) != 0 || len(existing.DeliveredDeliveries) != 0 {
			return store.saveLateGoalUsageLocked(existing)
		}
		return nil
	} else if !IsNotFound(err) {
		return err
	}
	return store.saveLateGoalUsageLocked(ledger)
}

func (store *Store) mutateLateGoalUsage(key RunKey, mutate func(*LateGoalUsageLedger) error) (LateGoalUsageLedger, error) {
	if err := validateKey(key); err != nil {
		return LateGoalUsageLedger{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return LateGoalUsageLedger{}, err
	}
	ledger, err := store.loadLateGoalUsageLocked(key)
	if err != nil {
		return LateGoalUsageLedger{}, err
	}
	if err := mutate(&ledger); err != nil {
		return LateGoalUsageLedger{}, err
	}
	ledger.UpdatedAt = time.Now().UTC()
	if err := store.saveLateGoalUsageLocked(ledger); err != nil {
		return LateGoalUsageLedger{}, err
	}
	return ledger, nil
}

func (store *Store) ensureLateGoalUsageDirLocked() error {
	return ensurePrivateDirectory(store.lateGoalUsageDir())
}
func (store *Store) lateGoalUsageDir() string {
	return filepath.Join(store.dir, goalUsageDirectoryName)
}
func (store *Store) lateGoalUsagePath(key RunKey) string {
	digest := sha256.Sum256([]byte(key.RunID + "\x00" + fmt.Sprintf("%d", key.Generation)))
	return filepath.Join(store.lateGoalUsageDir(), "usage-"+hex.EncodeToString(digest[:])+".json")
}
func isLateGoalUsageFile(name string) bool {
	digest := strings.TrimSuffix(strings.TrimPrefix(name, "usage-"), ".json")
	_, err := hex.DecodeString(digest)
	return strings.HasPrefix(name, "usage-") && strings.HasSuffix(name, ".json") && len(digest) == sha256.Size*2 && err == nil
}
func (store *Store) loadLateGoalUsageLocked(key RunKey) (LateGoalUsageLedger, error) {
	return store.loadLateGoalUsagePathLocked(store.lateGoalUsagePath(key))
}
func (store *Store) loadLateGoalUsagePathLocked(path string) (LateGoalUsageLedger, error) {
	var ledger LateGoalUsageLedger
	if err := store.readJSONWithLimit(path, "late Goal usage ledger", &ledger, maxJournalFileBytes); err != nil {
		return LateGoalUsageLedger{}, err
	}
	if err := validateLateGoalUsageLedger(ledger); err != nil || filepath.Base(path) != filepath.Base(store.lateGoalUsagePath(RunKey{RunID: ledger.RunID, Generation: ledger.Generation})) {
		return LateGoalUsageLedger{}, errors.New("invalid late Goal usage ledger")
	}
	return ledger, nil
}
func (store *Store) saveLateGoalUsageLocked(ledger LateGoalUsageLedger) error {
	if err := validateLateGoalUsageLedger(ledger); err != nil {
		return err
	}
	if err := store.ensureLateGoalUsageDirLocked(); err != nil {
		return err
	}
	return store.writeJSONWithLimit(store.lateGoalUsagePath(RunKey{RunID: ledger.RunID, Generation: ledger.Generation}), ledger, "late Goal usage ledger", maxJournalFileBytes)
}
func validateLateGoalUsageLedger(ledger LateGoalUsageLedger) error {
	if ledger.SchemaVersion != lateGoalUsageLedgerSchemaVersion || validateKey(RunKey{RunID: ledger.RunID, Generation: ledger.Generation}) != nil || ledger.Fence.Generation != ledger.Generation || ledger.Fence == (protocol.Fence{}) || ledger.CreatedAt.IsZero() || ledger.UpdatedAt.IsZero() {
		return errors.New("late Goal usage ledger is invalid")
	}
	seen := map[string]struct{}{}
	for index := range ledger.PendingUsageDeliveries {
		delivery := &ledger.PendingUsageDeliveries[index]
		if delivery.Kind != GoalDeliveryUsage || !sameFence(delivery.Fence, ledger.Fence) || validatePersistedGoalDelivery(ledger.RunID, delivery) != nil {
			return errors.New("late Goal usage delivery is invalid")
		}
		if _, duplicate := seen[delivery.DeliveryID]; duplicate {
			return errors.New("late Goal usage delivery is duplicated")
		}
		seen[delivery.DeliveryID] = struct{}{}
	}
	for index := range ledger.DeliveredDeliveries {
		delivery := &ledger.DeliveredDeliveries[index]
		if delivery.Kind != GoalDeliveryUsage || !delivery.Ready || !sameFence(delivery.Fence, ledger.Fence) || validatePersistedGoalDelivery(ledger.RunID, delivery) != nil {
			return errors.New("late Goal delivered usage is invalid")
		}
		if _, duplicate := seen[delivery.DeliveryID]; duplicate {
			return errors.New("late Goal usage delivery is duplicated")
		}
		seen[delivery.DeliveryID] = struct{}{}
	}
	for index := range ledger.RetiredDeliveries {
		retired := &ledger.RetiredDeliveries[index]
		if err := validateGoalDeliveryRetirement(ledger.RunID, ledger.Fence, retired); err != nil || retired.Delivery.Kind != GoalDeliveryUsage {
			return errors.New("late Goal usage retirement is invalid")
		}
		if _, duplicate := seen[retired.Delivery.DeliveryID]; duplicate {
			return errors.New("late Goal usage delivery is duplicated")
		}
		seen[retired.Delivery.DeliveryID] = struct{}{}
	}
	return nil
}

func validGoalDeliveryKind(kind GoalDeliveryKind) bool {
	return kind == GoalDeliverySessionAttach || kind == GoalDeliverySessionStopped || kind == GoalDeliveryEvidence || kind == GoalDeliveryUsage
}

func validGoalDeliveryDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateGoalSessionAttachDelivery(payload GoalSessionAttachDelivery) error {
	if !validRequiredString(payload.GoalID, 4096) || !validGoalSessionUUID(payload.LocalHandleID) ||
		!validRequiredString(payload.HarnessKind, 256) || !validRequiredString(payload.HarnessVersion, 4096) ||
		!validRequiredString(payload.AdapterVersion, 4096) || !validRequiredString(payload.WorkspaceFingerprint, 4096) ||
		!validRequiredString(payload.Workspace, 32768) {
		return errors.New("Goal session attach delivery payload is invalid")
	}
	if payload.BindingID != "" && !validGoalSessionUUID(payload.BindingID) {
		return errors.New("Goal session attach delivery binding ID is invalid")
	}
	if payload.BindingID == "" && !payload.ServerIssuedBinding {
		// Legacy journals lack this marker. Keep them readable so recovery can
		// fail closed at delivery rather than treating state integrity as lost.
		return nil
	}
	if payload.BindingID != "" && payload.ServerIssuedBinding {
		return errors.New("Goal session attach delivery binding authority is invalid")
	}
	if payload.RepositoryResourceID != nil && !validRequiredString(*payload.RepositoryResourceID, 4096) {
		return errors.New("Goal session attach delivery repository resource ID is invalid")
	}
	return nil
}

func validateGoalSessionStoppedDelivery(payload GoalSessionStoppedDelivery) error {
	if !validGoalSessionUUID(payload.SessionID) || !validGoalSessionUUID(payload.LocalHandleID) || !validGoalSessionUUID(payload.BindingID) {
		return errors.New("Goal session stopped delivery is invalid")
	}
	return nil
}

func goalDeliveryDigest(delivery GoalDelivery) (string, error) {
	// encoding/json deterministically orders map keys and this envelope uses
	// typed structs, so it is stable across process restart for the same body.
	var encoded []byte
	var err error
	if delivery.Kind == GoalDeliverySessionStopped {
		encoded, err = json.Marshal(struct {
			Kind           GoalDeliveryKind
			DeliveryID     string
			Fence          protocol.Fence
			SessionAttach  *GoalSessionAttachDelivery
			SessionStopped *GoalSessionStoppedDelivery
			Evidence       *protocol.Evidence
			Usage          *protocol.Usage
		}{delivery.Kind, delivery.DeliveryID, delivery.Fence, delivery.SessionAttach, delivery.SessionStopped, delivery.Evidence, delivery.Usage})
	} else {
		// Preserve the original receipt hash envelope exactly for journals
		// written before session-stopped deliveries existed.
		encoded, err = json.Marshal(struct {
			Kind          GoalDeliveryKind
			DeliveryID    string
			Fence         protocol.Fence
			SessionAttach *GoalSessionAttachDelivery
			Evidence      *protocol.Evidence
			Usage         *protocol.Usage
		}{delivery.Kind, delivery.DeliveryID, delivery.Fence, delivery.SessionAttach, delivery.Evidence, delivery.Usage})
	}
	if err != nil {
		return "", fmt.Errorf("encode Goal delivery: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func equalGoalDelivery(left, right GoalDelivery) bool {
	left.Ready = false
	right.Ready = false
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}
