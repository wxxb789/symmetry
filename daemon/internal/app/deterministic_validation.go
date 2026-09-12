package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const (
	deterministicArtifactValidatorProfile = "artifact"
	deterministicArtifactSummary          = "Deterministic artifact validation passed."
)

// deterministicArtifactValidationResult is the durable result of a validation
// that did not need a native session. The caller owns terminal-slot and
// outbox wakeup bookkeeping after Handled becomes true.
type deterministicArtifactValidationResult struct {
	Handled    bool
	Journal    state.RunJournal
	Evidence   []protocol.Evidence
	TaskResult protocol.TaskResult
	Transition protocol.StateTransitionRequest
}

// deterministicArtifactValidationError keeps pre-native validation failures
// recognizable while preserving the original cause for diagnostics and tests.
type deterministicArtifactValidationError struct {
	Stage string
	Cause error
}

func (err *deterministicArtifactValidationError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Cause == nil {
		return "deterministic artifact validation " + err.Stage
	}
	return fmt.Sprintf("deterministic artifact validation %s: %v", err.Stage, err.Cause)
}

func (err *deterministicArtifactValidationError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

type goalArtifactReader interface {
	ReadSubjectArtifact(context.Context, string, protocol.Subject, string) (workspace.SubjectArtifact, error)
}

// tryDeterministicArtifactValidation handles validate admissions whose frozen
// acceptance contract consists solely of artifact predicates. It must run
// before any workspace preparation, session attachment, or native launch.
//
// A non-validate admission returns Handled=false with no error. A validation
// admission whose contract contains a non-artifact predicate also returns
// Handled=false, preserving its existing native validation path. Once an
// artifact-only contract is selected, every identity, path, and read failure
// remains fail-closed.
func (daemon *daemon) tryDeterministicArtifactValidation(
	ctx context.Context,
	key state.RunKey,
	claim protocol.ClaimResponse,
	admission protocol.Admission,
) (deterministicArtifactValidationResult, error) {
	if daemon == nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("preflight", errors.New("daemon is nil"))
	}
	if admission.Purpose != protocol.AdmissionPurposeValidate {
		return deterministicArtifactValidationResult{}, nil
	}
	if claim.ProviderAccess != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("claim", errors.New("deterministic artifact validation does not allow provider access"))
	}
	if ctx == nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("context", errors.New("context is nil"))
	}
	if daemon.store == nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("state", errors.New("state store is unavailable"))
	}
	if admission.Subject.ResourceID != daemon.config.Runtime.RepositoryResourceID {
		return deterministicArtifactValidationResult{}, deterministicValidationError("subject", errors.New("admission Subject resource_id does not match the configured runtime repository_resource_id"))
	}

	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("state", err)
	}
	if err := validateDeterministicValidationAdmission(key, claim, admission, journal); err != nil {
		return deterministicArtifactValidationResult{}, err
	}
	if err := daemon.validateDeterministicActiveRun(key, 0); err != nil {
		return deterministicArtifactValidationResult{}, err
	}
	deadline := parseAdmissionDeadline(admission.Limits.DeadlineAt)
	if deadline.IsZero() || !deadline.After(daemon.nowUTC()) {
		return deterministicArtifactValidationResult{}, deterministicValidationError("execution", errors.New("Goal admission deadline has elapsed before deterministic validation"))
	}
	validationContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := daemon.ensureDeterministicValidationMayComplete(validationContext, key, journal, deadline); err != nil {
		return deterministicArtifactValidationResult{}, err
	}
	if claim.Work.Workspace != "" && claim.Work.Workspace != daemon.config.Runtime.Workspace {
		return deterministicArtifactValidationResult{}, deterministicValidationError(
			"claim",
			fmt.Errorf("claim workspace %q does not match configured workspace %q", claim.Work.Workspace, daemon.config.Runtime.Workspace),
		)
	}

	goalAPI, ok := daemon.control.(goalControlAPI)
	if !ok {
		return deterministicArtifactValidationResult{}, deterministicValidationError("control", errors.New("control API does not implement Goal run context retrieval"))
	}
	canonical, err := goalAPI.FetchRunContext(validationContext, key.RunID, journal.Fence())
	if err != nil {
		if fenceErr := daemon.ensureDeterministicValidationMayComplete(validationContext, key, journal, deadline); fenceErr != nil {
			return deterministicArtifactValidationResult{}, fenceErr
		}
		return deterministicArtifactValidationResult{}, deterministicValidationError("context", fmt.Errorf("fetch canonical Goal context: %w", err))
	}
	if err := daemon.ensureDeterministicValidationMayComplete(validationContext, key, journal, deadline); err != nil {
		return deterministicArtifactValidationResult{}, err
	}

	predicates := canonical.Context.WorkContract.Acceptance.Predicates
	if len(predicates) == 0 {
		return deterministicArtifactValidationResult{}, deterministicValidationError("contract", errors.New("acceptance predicates are empty"))
	}
	for index := range predicates {
		if predicates[index].Kind != "artifact" {
			return deterministicArtifactValidationResult{}, nil
		}
	}
	if err := validateDeterministicArtifactAdmission(claim, admission, canonical); err != nil {
		return deterministicArtifactValidationResult{}, err
	}
	if err := validateDeterministicValidationContext(key, claim, admission, canonical); err != nil {
		return deterministicArtifactValidationResult{}, err
	}

	reader, ok := daemon.workspace.(goalArtifactReader)
	if !ok {
		return deterministicArtifactValidationResult{}, deterministicValidationError("workspace", errors.New("workspace service does not support immutable artifact reads"))
	}

	now := daemon.nowUTC()
	subjectHash, err := admission.Subject.Hash()
	if err != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("subject", fmt.Errorf("hash admitted Subject: %w", err))
	}

	evidence := make([]protocol.Evidence, 0, len(predicates))
	evidenceIDs := make([]string, 0, len(predicates))
	seenPredicateIDs := make(map[string]struct{}, len(predicates))
	seenEvidenceIDs := make(map[string]struct{}, len(predicates)+1)
	for index := range predicates {
		predicate := predicates[index]
		if _, exists := seenPredicateIDs[predicate.ID]; exists {
			return deterministicArtifactValidationResult{}, deterministicValidationError("contract", fmt.Errorf("acceptance predicate ID %q is duplicated", predicate.ID))
		}
		seenPredicateIDs[predicate.ID] = struct{}{}
		if predicate.ResourceID != admission.Subject.ResourceID {
			return deterministicArtifactValidationResult{}, deterministicValidationError(
				"subject",
				fmt.Errorf("artifact predicate %q resource_id %q does not match admitted Subject resource_id %q", predicate.ID, predicate.ResourceID, admission.Subject.ResourceID),
			)
		}
		if err := validateDeterministicArtifactPath(predicate.Path); err != nil {
			return deterministicArtifactValidationResult{}, deterministicValidationError(
				"path",
				fmt.Errorf("artifact predicate %q path %q: %w", predicate.ID, predicate.Path, err),
			)
		}

		if err := daemon.ensureDeterministicValidationMayComplete(validationContext, key, journal, deadline); err != nil {
			return deterministicArtifactValidationResult{}, err
		}
		artifact, readErr := reader.ReadSubjectArtifact(validationContext, daemon.config.Runtime.Workspace, admission.Subject, predicate.Path)
		if readErr != nil {
			if fenceErr := daemon.ensureDeterministicValidationMayComplete(validationContext, key, journal, deadline); fenceErr != nil {
				return deterministicArtifactValidationResult{}, fenceErr
			}
			return deterministicArtifactValidationResult{}, deterministicValidationError(
				"artifact",
				fmt.Errorf("read artifact predicate %q path %q: %w", predicate.ID, predicate.Path, readErr),
			)
		}
		if err := daemon.ensureDeterministicValidationMayComplete(validationContext, key, journal, deadline); err != nil {
			return deterministicArtifactValidationResult{}, err
		}
		if artifact.Path != predicate.Path {
			return deterministicArtifactValidationResult{}, deterministicValidationError(
				"artifact",
				fmt.Errorf("artifact predicate %q reader returned path %q, want %q", predicate.ID, artifact.Path, predicate.Path),
			)
		}
		contentDigest := digestDeterministicArtifact(artifact.Content)
		if artifact.ContentDigest != contentDigest {
			return deterministicArtifactValidationResult{}, deterministicValidationError(
				"artifact",
				fmt.Errorf("artifact predicate %q reader returned content digest %q, want %q", predicate.ID, artifact.ContentDigest, contentDigest),
			)
		}

		evidenceID, idErr := daemon.nextDeterministicValidationID()
		if idErr != nil {
			return deterministicArtifactValidationResult{}, deterministicValidationError("identity", fmt.Errorf("generate evidence ID: %w", idErr))
		}
		if _, exists := seenEvidenceIDs[evidenceID]; exists {
			return deterministicArtifactValidationResult{}, deterministicValidationError("identity", fmt.Errorf("generated duplicate evidence ID %q", evidenceID))
		}
		seenEvidenceIDs[evidenceID] = struct{}{}
		evidenceKey := deterministicArtifactEvidenceKey(predicate.ID)
		resourceID := admission.Subject.ResourceID
		commit := admission.Subject.Commit
		path := predicate.Path
		validatorProfile := deterministicArtifactValidatorProfile
		evidenceItem := protocol.Evidence{
			SchemaVersion: protocol.EvidenceSchemaVersion,
			EvidenceID:    evidenceID,
			RunID:         key.RunID,
			EvidenceKey:   evidenceKey,
			Kind:          protocol.EvidenceArtifact,
			Subject:       admission.Subject,
			SubjectHash:   subjectHash,
			SourceRef: protocol.SourceRef{
				Kind:        protocol.EvidenceArtifact,
				Ref:         evidenceKey,
				SubjectHash: &subjectHash,
				ResourceID:  &resourceID,
				Commit:      &commit,
				Path:        &path,
			},
			SourceRevision:   "commit:" + admission.Subject.Commit,
			ValidatorProfile: &validatorProfile,
			Verdict:          protocol.EvidencePassed,
			Payload: protocol.Receipt{
				PredicateID:   &predicate.ID,
				Subject:       subjectPointer(admission.Subject),
				SubjectHash:   &subjectHash,
				ResourceID:    &resourceID,
				Commit:        &commit,
				Path:          &path,
				ContentDigest: &contentDigest,
			},
			ObservedAt: now.Format(time.RFC3339Nano),
		}
		if err := evidenceItem.Validate(); err != nil {
			return deterministicArtifactValidationResult{}, deterministicValidationError(
				"evidence",
				fmt.Errorf("validate artifact predicate %q evidence: %w", predicate.ID, err),
			)
		}
		evidence = append(evidence, evidenceItem)
		evidenceIDs = append(evidenceIDs, evidenceID)
	}

	resultID, err := daemon.nextDeterministicValidationID()
	if err != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("identity", fmt.Errorf("generate task result ID: %w", err))
	}
	if _, exists := seenEvidenceIDs[resultID]; exists {
		return deterministicArtifactValidationResult{}, deterministicValidationError("identity", fmt.Errorf("generated task result ID duplicates an evidence ID %q", resultID))
	}
	result := protocol.TaskResult{
		SchemaVersion: protocol.TaskResultSchemaVersion,
		ResultID:      resultID,
		Kind:          protocol.TaskResultCandidateCompletion,
		Summary:       deterministicArtifactSummary,
		Subject:       admission.Subject,
		SubjectHash:   subjectHash,
		EvidenceRefs:  evidenceIDs,
		Blocker:       nil,
		Diagnostics:   []protocol.Diagnostic{},
	}
	if err := result.Validate(); err != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("result", err)
	}
	if err := protocol.ValidateTaskResultForAdmission(admission, result); err != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("result", err)
	}
	if err := daemon.ensureDeterministicValidationMayComplete(validationContext, key, journal, deadline); err != nil {
		return deterministicArtifactValidationResult{}, err
	}

	transitionID, err := daemon.nextDeterministicValidationID()
	if err != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("identity", fmt.Errorf("generate terminal transition ID: %w", err))
	}
	transitionPayload, err := json.Marshal(map[string]any{
		"summary":     deterministicArtifactSummary,
		"task_result": result,
	})
	if err != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("result", fmt.Errorf("encode terminal task result: %w", err))
	}
	transition := protocol.StateTransitionRequest{
		Fence:        journal.Fence(),
		TransitionID: transitionID,
		State:        "completed",
		Payload:      transitionPayload,
	}
	queued, err := daemon.queueDeterministicValidationTerminal(validationContext, key, journal, deadline, evidence, transition)
	if err != nil {
		return deterministicArtifactValidationResult{}, deterministicValidationError("state", fmt.Errorf("queue artifact evidence and terminal transition: %w", err))
	}
	return deterministicArtifactValidationResult{
		Handled:    true,
		Journal:    queued,
		Evidence:   evidence,
		TaskResult: result,
		Transition: transition,
	}, nil
}

func deterministicValidationError(stage string, cause error) error {
	return &deterministicArtifactValidationError{Stage: stage, Cause: cause}
}

func deterministicArtifactEvidenceKey(predicateID string) string {
	digest := sha256.Sum256([]byte(predicateID))
	return "artifact:" + hex.EncodeToString(digest[:])
}

func (daemon *daemon) ensureDeterministicValidationMayComplete(ctx context.Context, key state.RunKey, journal state.RunJournal, deadline time.Time) error {
	if err := ctx.Err(); err != nil {
		return deterministicValidationError("execution", err)
	}
	if deadline.IsZero() || !deadline.After(daemon.nowUTC()) {
		return deterministicValidationError("execution", errors.New("Goal admission deadline has elapsed before deterministic validation"))
	}

	daemon.mu.Lock()
	active := daemon.running[key]
	if active == nil {
		daemon.mu.Unlock()
		return deterministicValidationError("execution", errors.New("Goal validation run is no longer owned by this daemon"))
	}
	if active.cancelled {
		daemon.mu.Unlock()
		return deterministicValidationError("execution", errors.New("Goal validation was cancelled before terminal evidence could be queued"))
	}
	if active.stale {
		daemon.mu.Unlock()
		return deterministicValidationError("execution", errors.New("Goal validation became stale before terminal evidence could be queued"))
	}
	if active.terminal || active.terminalizing > 0 {
		daemon.mu.Unlock()
		return deterministicValidationError("execution", errors.New("Goal validation has a terminal owner before terminal evidence could be queued"))
	}
	daemon.mu.Unlock()

	current, err := daemon.store.LoadJournal(key)
	if err != nil {
		return deterministicValidationError("state", fmt.Errorf("reload Goal validation fence: %w", err))
	}
	if current.Key() != key || current.Fence() != journal.Fence() {
		return deterministicValidationError("execution", errors.New("Goal validation fence changed before deterministic validation completed"))
	}
	if current.LocalState == "stale" || current.LocalState == "terminal_pending" || current.LocalState == "cleanup_pending" {
		return deterministicValidationError("execution", errors.New("Goal validation is no longer in a live claimed state"))
	}
	if !current.LeaseExpiresAt.After(daemon.nowUTC()) {
		return deterministicValidationError("execution", errors.New("Goal validation lease expired before terminal evidence could be queued"))
	}
	return nil
}

// queueDeterministicValidationTerminal establishes the terminal reservation and
// queues the immutable evidence/terminal pair while the cancellation and stale
// flags are serialized by daemon.mu. A cancellation that wins this lock is
// rejected before the terminal write; a cancellation arriving afterward is
// ordered after the already durable terminal transition.
func (daemon *daemon) queueDeterministicValidationTerminal(
	ctx context.Context,
	key state.RunKey,
	expected state.RunJournal,
	deadline time.Time,
	evidence []protocol.Evidence,
	transition protocol.StateTransitionRequest,
) (state.RunJournal, error) {
	finishTerminal := daemon.beginTerminal(key)
	persisted := false
	defer func() { finishTerminal(persisted) }()

	if err := ctx.Err(); err != nil {
		return state.RunJournal{}, deterministicValidationError("execution", err)
	}
	if deadline.IsZero() || !deadline.After(daemon.nowUTC()) {
		return state.RunJournal{}, deterministicValidationError("execution", errors.New("Goal admission deadline has elapsed before deterministic validation"))
	}
	if err := daemon.validateDeterministicActiveRun(key, 1); err != nil {
		return state.RunJournal{}, err
	}

	current, err := daemon.store.LoadJournal(key)
	if err != nil {
		return state.RunJournal{}, deterministicValidationError("state", fmt.Errorf("reload Goal validation fence before terminal queue: %w", err))
	}
	if current.Key() != key || current.Fence() != expected.Fence() {
		return state.RunJournal{}, deterministicValidationError("execution", errors.New("Goal validation fence changed before terminal evidence could be queued"))
	}
	if current.LocalState == "stale" || current.LocalState == "terminal_pending" || current.LocalState == "cleanup_pending" {
		return state.RunJournal{}, deterministicValidationError("execution", errors.New("Goal validation is no longer in a live claimed state"))
	}
	if !current.LeaseExpiresAt.After(daemon.nowUTC()) {
		return state.RunJournal{}, deterministicValidationError("execution", errors.New("Goal validation lease expired before terminal evidence could be queued"))
	}

	pendingAt := daemon.nowUTC()
	queued, err := daemon.store.QueueGoalEvidenceAndTerminalTransition(key, evidence, transition, pendingAt)
	if err != nil {
		return state.RunJournal{}, err
	}
	if queued.Key() != key || queued.Fence() != expected.Fence() || queued.LocalState != "terminal_pending" || queued.TerminalState != transition.State {
		return state.RunJournal{}, errors.New("deterministic terminal queue returned an invalid fenced journal")
	}
	persisted = true
	return queued, nil
}

func (daemon *daemon) validateDeterministicActiveRun(key state.RunKey, ownTerminalReservations int) error {
	daemon.mu.Lock()
	active := daemon.running[key]
	if active == nil {
		daemon.mu.Unlock()
		return deterministicValidationError("execution", errors.New("Goal validation run is no longer owned by this daemon"))
	}
	processPresent := !isNilProcess(active.process)
	nativeSessionPresent := !isNilHarnessSession(active.nativeSession)
	starting := active.starting
	terminal := active.terminal
	terminalizing := active.terminalizing
	cancelled := active.cancelled
	stale := active.stale
	daemon.mu.Unlock()
	if processPresent || nativeSessionPresent {
		return deterministicValidationError("execution", errors.New("deterministic artifact validation cannot run with an active native process or session"))
	}
	if terminal || terminalizing > ownTerminalReservations {
		return deterministicValidationError("execution", errors.New("deterministic artifact validation has a competing terminal owner"))
	}
	if starting && processPresent {
		return deterministicValidationError("execution", errors.New("deterministic artifact validation observed an invalid native start state"))
	}
	if cancelled {
		return deterministicValidationError("execution", errors.New("Goal validation was cancelled before terminal evidence could be queued"))
	}
	if stale {
		return deterministicValidationError("execution", errors.New("Goal validation became stale before terminal evidence could be queued"))
	}
	return nil
}

func validateDeterministicValidationAdmission(key state.RunKey, claim protocol.ClaimResponse, admission protocol.Admission, journal state.RunJournal) error {
	if err := admission.Validate(); err != nil {
		return deterministicValidationError("admission", err)
	}
	if admission.Purpose != protocol.AdmissionPurposeValidate || admission.ValidationOfTaskID == nil {
		return deterministicValidationError("admission", errors.New("admission is not a validation task"))
	}
	if journal.Key() != key || journal.RunID != claim.RunID || journal.Generation != claim.Generation || journal.ClaimID != claim.ClaimID || journal.LeaseToken != claim.LeaseToken {
		return deterministicValidationError("claim", errors.New("claim does not match the durable run fence"))
	}
	if claim.RunID != key.RunID || claim.Generation != key.Generation || strings.TrimSpace(claim.TaskID) == "" || strings.TrimSpace(claim.ClaimID) == "" || strings.TrimSpace(claim.LeaseToken) == "" {
		return deterministicValidationError("claim", errors.New("claim identity does not match the requested run"))
	}
	if claim.Work.AgentProfile != "" && claim.Work.AgentProfile != admission.ModelProfile {
		return deterministicValidationError("claim", fmt.Errorf("claim agent_profile %q does not match admission model_profile %q", claim.Work.AgentProfile, admission.ModelProfile))
	}
	return nil
}

func validateDeterministicArtifactAdmission(claim protocol.ClaimResponse, admission protocol.Admission, runContext control.GoalRunContext) error {
	if claim.ProviderAccess != nil {
		return deterministicValidationError("claim", errors.New("deterministic artifact validation does not allow provider access"))
	}
	if admission.SessionMode != protocol.SessionModeFresh || admission.RequestedSessionID != nil || admission.HandoffSourceRunID != nil {
		return deterministicValidationError("admission", errors.New("deterministic artifact validation requires a fresh admission without a native session"))
	}
	if (claim.HarnessSessionID == nil) != (claim.HarnessBindingID == nil) {
		return deterministicValidationError("claim", errors.New("claim harness session and binding must be present together or both absent"))
	}
	if claim.HarnessSessionID != nil || claim.HarnessBindingID != nil {
		return deterministicValidationError("claim", errors.New("deterministic artifact validation must not use a native harness session"))
	}
	if runContext.SessionID != nil {
		return deterministicValidationError("context", errors.New("deterministic artifact validation context must not contain a session_id"))
	}
	return nil
}

func validateDeterministicValidationContext(key state.RunKey, claim protocol.ClaimResponse, admission protocol.Admission, runContext control.GoalRunContext) error {
	if runContext.GoalID != admission.GoalID || runContext.TaskID != claim.TaskID || runContext.RunID != key.RunID || runContext.Generation != key.Generation {
		return deterministicValidationError("context", errors.New("canonical Goal run context does not match admission, claim, or run"))
	}
	if runContext.SessionID != nil {
		return deterministicValidationError("context", errors.New("deterministic artifact validation context must not contain a session_id"))
	}
	contextSnapshot := runContext.Context
	if contextSnapshot.GoalID != admission.GoalID || contextSnapshot.GoalRevision != admission.GoalRevision ||
		!equalOptionalGoalString(contextSnapshot.WorkItemID, admission.WorkItemID) ||
		contextSnapshot.WorkContract.Purpose != string(admission.Purpose) ||
		contextSnapshot.SnapshotID != admission.ContextSnapshotID ||
		contextSnapshot.ContentHash != admission.ContextHash ||
		contextSnapshot.Subject != admission.Subject {
		return deterministicValidationError("context", errors.New("canonical Goal context does not match admission"))
	}
	if contextSnapshot.Subject.Validate() != nil {
		return deterministicValidationError("context", fmt.Errorf("canonical Goal context Subject is invalid: %w", contextSnapshot.Subject.Validate()))
	}
	return nil
}

func validateDeterministicArtifactPath(path string) error {
	// SourceRef.Validate is the public protocol boundary that applies the
	// canonical CommitPath contract. Keep the original path untouched: Git tree
	// identity is byte-for-byte path identity, not a filepath-normalized path.
	pathRef := protocol.SourceRef{Kind: protocol.EvidenceArtifact, Ref: "artifact", Path: &path}
	if err := pathRef.Validate(); err != nil {
		return fmt.Errorf("artifact path: %w", err)
	}
	return nil
}

func digestDeterministicArtifact(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func subjectPointer(subject protocol.Subject) *protocol.Subject {
	copy := subject
	return &copy
}

func (daemon *daemon) nextDeterministicValidationID() (string, error) {
	if daemon.options.newID != nil {
		return daemon.options.newID()
	}
	return state.NewDaemonInstanceID()
}

func (daemon *daemon) nowUTC() time.Time {
	if daemon.options.clock != nil {
		return daemon.options.clock().UTC()
	}
	return time.Now().UTC()
}
