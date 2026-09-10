package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"

	contractdto "github.com/wxxb789/symmetry/daemon/internal/contracts"
)

// TaskResultKind is the semantic result vocabulary emitted by a native turn.
type TaskResultKind string

const (
	TaskResultProgress            TaskResultKind = "progress"
	TaskResultCandidateCompletion TaskResultKind = "candidate_completion"
	TaskResultBlocked             TaskResultKind = "blocked"
	TaskResultRepairRequired      TaskResultKind = "repair_required"
	TaskResultReplanRequired      TaskResultKind = "replan_required"
	TaskResultFailed              TaskResultKind = "failed"
	TaskResultPlanProposed        TaskResultKind = "plan_proposed"
)

// BlockerKind identifies why a task cannot continue without another action.
type BlockerKind string

const (
	BlockerDecision    BlockerKind = "decision"
	BlockerExternal    BlockerKind = "external"
	BlockerDependency  BlockerKind = "dependency"
	BlockerEnvironment BlockerKind = "environment"
)

// Blocker is a typed, non-executable explanation for a blocked result.
type Blocker struct {
	Kind        BlockerKind `json:"kind"`
	DecisionID  string      `json:"decision_id,omitempty"`
	ResourceID  string      `json:"resource_id,omitempty"`
	ExternalRef string      `json:"external_ref,omitempty"`
	NextCheckAt string      `json:"next_check_at,omitempty"`
	WorkItemIDs []string    `json:"work_item_ids,omitempty"`
	Code        string      `json:"code,omitempty"`
	Detail      string      `json:"detail,omitempty"`
}

func (blocker Blocker) Validate() error {
	switch blocker.Kind {
	case BlockerDecision:
		if err := validateUUID(blocker.DecisionID, "blocker.decision_id"); err != nil {
			return err
		}
		if blocker.ResourceID != "" || blocker.ExternalRef != "" || blocker.NextCheckAt != "" ||
			len(blocker.WorkItemIDs) != 0 || blocker.Code != "" || blocker.Detail != "" {
			return fmt.Errorf("decision blocker contains fields for another kind")
		}
	case BlockerExternal:
		if err := validateUUID(blocker.ResourceID, "blocker.resource_id"); err != nil {
			return err
		}
		if err := validateLongText(blocker.ExternalRef, "blocker.external_ref"); err != nil {
			return err
		}
		if err := validateUTCTimestamp(blocker.NextCheckAt, "blocker.next_check_at"); err != nil {
			return err
		}
		if blocker.DecisionID != "" || len(blocker.WorkItemIDs) != 0 || blocker.Code != "" || blocker.Detail != "" {
			return fmt.Errorf("external blocker contains fields for another kind")
		}
	case BlockerDependency:
		if len(blocker.WorkItemIDs) == 0 {
			return fmt.Errorf("blocker.work_item_ids must not be empty")
		}
		if err := validateUniqueUUIDs(blocker.WorkItemIDs, "blocker.work_item_ids"); err != nil {
			return err
		}
		if blocker.DecisionID != "" || blocker.ResourceID != "" || blocker.ExternalRef != "" ||
			blocker.NextCheckAt != "" || blocker.Code != "" || blocker.Detail != "" {
			return fmt.Errorf("dependency blocker contains fields for another kind")
		}
	case BlockerEnvironment:
		if err := validateShortIdentifier(blocker.Code, "blocker.code"); err != nil {
			return err
		}
		if err := validateLongText(blocker.Detail, "blocker.detail"); err != nil {
			return err
		}
		if blocker.DecisionID != "" || blocker.ResourceID != "" || blocker.ExternalRef != "" ||
			blocker.NextCheckAt != "" || len(blocker.WorkItemIDs) != 0 {
			return fmt.Errorf("environment blocker contains fields for another kind")
		}
	default:
		return fmt.Errorf("blocker.kind %q is invalid", blocker.Kind)
	}
	return nil
}

func (blocker *Blocker) UnmarshalJSON(data []byte) error {
	var decoded blockerWire
	if err := decodeStrictObject(data, &decoded, "kind"); err != nil {
		return err
	}
	value := Blocker(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*blocker = value
	return nil
}

type blockerWire Blocker

// NextActionKind identifies a proposed follow-up. It is data only and never a
// shell command or an authorization to perform the action.
type NextActionKind string

const (
	NextActionValidate NextActionKind = "validate"
	NextActionRepair   NextActionKind = "repair"
	NextActionObserve  NextActionKind = "observe"
	NextActionReplan   NextActionKind = "replan"
	NextActionWait     NextActionKind = "wait"
)

// NextAction is a typed proposal attached to a TaskResult.
type NextAction struct {
	Kind            NextActionKind `json:"kind"`
	ProducingTaskID string         `json:"producing_task_id,omitempty"`
	WorkItemID      string         `json:"work_item_id,omitempty"`
	Reason          string         `json:"reason,omitempty"`
	ResourceID      string         `json:"resource_id,omitempty"`
	ExternalRef     string         `json:"external_ref,omitempty"`
	Blocker         *Blocker       `json:"blocker,omitempty"`
}

func (action NextAction) Validate() error {
	switch action.Kind {
	case NextActionValidate:
		if err := validateUUID(action.ProducingTaskID, "proposed_next_action.producing_task_id"); err != nil {
			return err
		}
		if action.WorkItemID != "" || action.Reason != "" || action.ResourceID != "" || action.ExternalRef != "" || action.Blocker != nil {
			return fmt.Errorf("validate next action contains fields for another kind")
		}
	case NextActionRepair:
		if err := validateUUID(action.WorkItemID, "proposed_next_action.work_item_id"); err != nil {
			return err
		}
		if err := validateLongText(action.Reason, "proposed_next_action.reason"); err != nil {
			return err
		}
		if action.ProducingTaskID != "" || action.ResourceID != "" || action.ExternalRef != "" || action.Blocker != nil {
			return fmt.Errorf("repair next action contains fields for another kind")
		}
	case NextActionObserve:
		if err := validateUUID(action.ResourceID, "proposed_next_action.resource_id"); err != nil {
			return err
		}
		if err := validateLongText(action.ExternalRef, "proposed_next_action.external_ref"); err != nil {
			return err
		}
		if action.ProducingTaskID != "" || action.WorkItemID != "" || action.Reason != "" || action.Blocker != nil {
			return fmt.Errorf("observe next action contains fields for another kind")
		}
	case NextActionReplan:
		if err := validateLongText(action.Reason, "proposed_next_action.reason"); err != nil {
			return err
		}
		if action.ProducingTaskID != "" || action.WorkItemID != "" || action.ResourceID != "" || action.ExternalRef != "" || action.Blocker != nil {
			return fmt.Errorf("replan next action contains fields for another kind")
		}
	case NextActionWait:
		if action.Blocker == nil {
			return fmt.Errorf("proposed_next_action.blocker is required")
		}
		if err := action.Blocker.Validate(); err != nil {
			return fmt.Errorf("proposed_next_action.blocker: %w", err)
		}
		if action.ProducingTaskID != "" || action.WorkItemID != "" || action.Reason != "" || action.ResourceID != "" || action.ExternalRef != "" {
			return fmt.Errorf("wait next action contains fields for another kind")
		}
	default:
		return fmt.Errorf("proposed_next_action.kind %q is invalid", action.Kind)
	}
	return nil
}

func (action *NextAction) UnmarshalJSON(data []byte) error {
	var decoded nextActionWire
	if err := decodeStrictObject(data, &decoded, "kind"); err != nil {
		return err
	}
	value := NextAction(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*action = value
	return nil
}

type nextActionWire NextAction

// TaskResultReason identifies a normalized failure or stopping reason.
type TaskResultReason string

const (
	TaskResultReasonAuth               TaskResultReason = "auth"
	TaskResultReasonQuota              TaskResultReason = "quota"
	TaskResultReasonRateLimit          TaskResultReason = "rate_limit"
	TaskResultReasonNetwork            TaskResultReason = "network"
	TaskResultReasonUnsupportedVersion TaskResultReason = "unsupported_version"
	TaskResultReasonResumeRejected     TaskResultReason = "resume_rejected"
	TaskResultReasonHandoffUnsupported TaskResultReason = "handoff_unsupported"
	TaskResultReasonContextOverflow    TaskResultReason = "context_overflow"
	TaskResultReasonMissingResult      TaskResultReason = "missing_result"
	TaskResultReasonProcessFailure     TaskResultReason = "process_failure"
	TaskResultReasonCancelled          TaskResultReason = "cancelled"
	TaskResultReasonUnknownOutcome     TaskResultReason = "unknown_outcome"
	TaskResultReasonNoVerifiedProgress TaskResultReason = "no_verified_progress"
	TaskResultReasonValidationFailed   TaskResultReason = "validation_failed"
)

// DiagnosticSeverity controls the presentation of an optional normalized
// diagnostic without changing task semantics.
type DiagnosticSeverity string

const (
	DiagnosticInfo    DiagnosticSeverity = "info"
	DiagnosticWarning DiagnosticSeverity = "warning"
	DiagnosticError   DiagnosticSeverity = "error"
)

// Diagnostic is bounded non-authoritative context attached to a result.
type Diagnostic struct {
	Code     string              `json:"code"`
	Message  string              `json:"message"`
	Severity *DiagnosticSeverity `json:"severity,omitempty"`
	present  map[string]json.RawMessage
}

func (diagnostic Diagnostic) Validate() error {
	if err := validateShortIdentifier(diagnostic.Code, "diagnostic.code"); err != nil {
		return err
	}
	if err := validateLongText(diagnostic.Message, "diagnostic.message"); err != nil {
		return err
	}
	if diagnostic.Severity != nil {
		switch *diagnostic.Severity {
		case DiagnosticInfo, DiagnosticWarning, DiagnosticError:
		default:
			return fmt.Errorf("diagnostic.severity %q is invalid", *diagnostic.Severity)
		}
	}
	if value, present := diagnostic.present["severity"]; present && string(bytes.TrimSpace(value)) == "null" {
		return fmt.Errorf("diagnostic.severity must be an enum when present")
	}
	return nil
}

func (diagnostic *Diagnostic) UnmarshalJSON(data []byte) error {
	var decoded diagnosticWire
	if err := decodeStrictObject(data, &decoded, "code", "message"); err != nil {
		return err
	}
	value := Diagnostic(decoded)
	fields, err := objectFields(data)
	if err != nil {
		return err
	}
	value.present = fields
	if err := value.Validate(); err != nil {
		return err
	}
	*diagnostic = value
	return nil
}

type diagnosticWire Diagnostic

// TaskResult is the normalized semantic outcome of one bounded native turn.
// Every result names its complete subject. A candidate_completion therefore
// carries the candidate artifact, while acceptance remains a control-plane
// decision rather than a producer action.
type TaskResult struct {
	SchemaVersion      string         `json:"schema_version"`
	ResultID           string         `json:"result_id"`
	Kind               TaskResultKind `json:"kind"`
	Summary            string         `json:"summary"`
	Subject            Subject        `json:"subject"`
	SubjectHash        string         `json:"subject_hash"`
	EvidenceRefs       []string       `json:"evidence_refs"`
	Blocker            *Blocker       `json:"blocker"`
	ProposedNextAction *NextAction    `json:"proposed_next_action"`
	// Proposal is present only for plan_proposed. Keeping the raw, schema-valid
	// JSON here avoids creating a second Go model for the shared PlanProposal
	// contract while preserving the exact wire payload for control-plane use.
	Proposal    *json.RawMessage  `json:"proposal"`
	Reason      *TaskResultReason `json:"reason"`
	Diagnostics []Diagnostic      `json:"diagnostics"`
}

func (result TaskResult) Validate() error {
	if result.SchemaVersion != TaskResultSchemaVersion {
		return fmt.Errorf("schema_version must be %q", TaskResultSchemaVersion)
	}
	if err := validateUUID(result.ResultID, "result_id"); err != nil {
		return err
	}
	switch result.Kind {
	case TaskResultProgress, TaskResultCandidateCompletion, TaskResultBlocked,
		TaskResultRepairRequired, TaskResultReplanRequired, TaskResultFailed,
		TaskResultPlanProposed:
	default:
		return fmt.Errorf("kind %q is invalid", result.Kind)
	}
	if err := validateLongText(result.Summary, "summary"); err != nil {
		return err
	}
	if err := result.Subject.Validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if err := validateSHA256(result.SubjectHash, "subject_hash"); err != nil {
		return err
	}
	computedSubjectHash, err := result.Subject.Hash()
	if err != nil {
		return fmt.Errorf("hash task result subject: %w", err)
	}
	if result.SubjectHash != computedSubjectHash {
		return fmt.Errorf("subject_hash does not match canonical subject")
	}
	if result.EvidenceRefs == nil {
		return fmt.Errorf("evidence_refs is required")
	}
	if result.Diagnostics == nil {
		return fmt.Errorf("diagnostics is required")
	}
	switch result.Kind {
	case TaskResultPlanProposed:
		if result.Proposal == nil {
			return fmt.Errorf("proposal is required when kind is %q", result.Kind)
		}
		if err := contractdto.Validate(contractdto.EnvelopePlanProposal, *result.Proposal); err != nil {
			return fmt.Errorf("proposal: %w", err)
		}
	default:
		if result.Proposal != nil {
			return fmt.Errorf("proposal is only valid when kind is %q", TaskResultPlanProposed)
		}
	}
	if err := validateUniqueUUIDs(result.EvidenceRefs, "evidence_refs"); err != nil {
		return err
	}
	if len(result.EvidenceRefs) > 1024 {
		return fmt.Errorf("evidence_refs must contain at most 1024 items")
	}
	if result.Reason != nil {
		switch *result.Reason {
		case TaskResultReasonAuth, TaskResultReasonQuota, TaskResultReasonRateLimit,
			TaskResultReasonNetwork, TaskResultReasonUnsupportedVersion,
			TaskResultReasonResumeRejected, TaskResultReasonHandoffUnsupported,
			TaskResultReasonContextOverflow,
			TaskResultReasonMissingResult, TaskResultReasonProcessFailure,
			TaskResultReasonCancelled, TaskResultReasonUnknownOutcome,
			TaskResultReasonNoVerifiedProgress, TaskResultReasonValidationFailed:
		default:
			return fmt.Errorf("reason %q is invalid", *result.Reason)
		}
	}
	if result.Kind == TaskResultBlocked && result.Blocker == nil {
		return fmt.Errorf("blocker is required when kind is %q", TaskResultBlocked)
	}
	if result.Kind != TaskResultBlocked && result.Blocker != nil {
		return fmt.Errorf("blocker is only valid when kind is %q", TaskResultBlocked)
	}
	if result.Kind == TaskResultFailed {
		if result.Reason == nil {
			return fmt.Errorf("reason is required when kind is %q", TaskResultFailed)
		}
		if !isTerminalTaskResultFailureReason(*result.Reason) {
			return fmt.Errorf("reason %q is not valid when kind is %q", *result.Reason, TaskResultFailed)
		}
	} else if result.Reason != nil {
		return fmt.Errorf("reason is only valid when kind is %q", TaskResultFailed)
	}
	if len(result.Diagnostics) > 128 {
		return fmt.Errorf("diagnostics must contain at most 128 items")
	}
	for index := range result.Diagnostics {
		if err := result.Diagnostics[index].Validate(); err != nil {
			return fmt.Errorf("diagnostics[%d]: %w", index, err)
		}
	}
	if result.Blocker != nil {
		if err := result.Blocker.Validate(); err != nil {
			return fmt.Errorf("blocker: %w", err)
		}
	}
	if result.ProposedNextAction != nil {
		if err := result.ProposedNextAction.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateTaskResultForAdmission enforces the cross-envelope result
// vocabulary. A planning admission has one legal semantic result, while a
// non-planning admission must never smuggle a plan proposal through the
// ordinary task-result path.
func ValidateTaskResultForAdmission(admission Admission, result TaskResult) error {
	if admission.Purpose == AdmissionPurposePlan {
		if result.Kind != TaskResultPlanProposed {
			return fmt.Errorf("purpose %q requires result kind %q, got %q", admission.Purpose, TaskResultPlanProposed, result.Kind)
		}
		return nil
	}
	if result.Kind == TaskResultPlanProposed {
		return fmt.Errorf("result kind %q is only valid for purpose %q admissions", result.Kind, AdmissionPurposePlan)
	}
	return nil
}

func isTerminalTaskResultFailureReason(reason TaskResultReason) bool {
	switch reason {
	case TaskResultReasonAuth, TaskResultReasonQuota, TaskResultReasonRateLimit,
		TaskResultReasonNetwork, TaskResultReasonUnsupportedVersion,
		TaskResultReasonResumeRejected, TaskResultReasonHandoffUnsupported,
		TaskResultReasonContextOverflow,
		TaskResultReasonMissingResult, TaskResultReasonProcessFailure,
		TaskResultReasonCancelled, TaskResultReasonUnknownOutcome:
		return true
	default:
		return false
	}
}

func (result *TaskResult) UnmarshalJSON(data []byte) error {
	var decoded taskResultWire
	if err := decodeStrictObject(data, &decoded,
		"schema_version", "result_id", "kind", "summary", "subject", "subject_hash",
		"evidence_refs", "blocker", "proposed_next_action", "proposal", "reason", "diagnostics"); err != nil {
		return err
	}
	value := TaskResult(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*result = value
	return nil
}

type taskResultWire TaskResult

// ParseTaskResult decodes and validates a normalized semantic result.
func ParseTaskResult(data []byte) (TaskResult, error) {
	var wire contractdto.SymmetryTaskResultV1
	var result TaskResult
	if err := decodeGoalEnvelope(data, contractdto.EnvelopeTaskResult, &wire, &result); err != nil {
		return TaskResult{}, err
	}
	return result, nil
}

// EvidenceKind identifies the normalized source of one receipt.
type EvidenceKind string

const (
	EvidenceCheck       EvidenceKind = "check"
	EvidenceArtifact    EvidenceKind = "artifact"
	EvidenceReview      EvidenceKind = "review"
	EvidenceObservation EvidenceKind = "observation"
)

// EvidenceVerdict is intentionally separate from TaskResultKind. Unknown and
// not_applicable are explicit outcomes, not omitted values.
type EvidenceVerdict string

const (
	EvidencePassed        EvidenceVerdict = "passed"
	EvidenceFailed        EvidenceVerdict = "failed"
	EvidenceUnknown       EvidenceVerdict = "unknown"
	EvidenceNotApplicable EvidenceVerdict = "not_applicable"
)

// SourceRef identifies the authenticated source of an evidence receipt.
type SourceRef struct {
	Kind             EvidenceKind `json:"kind"`
	Ref              string       `json:"ref"`
	ValidatorProfile *string      `json:"validator_profile,omitempty"`
	SubjectHash      *string      `json:"subject_hash,omitempty"`
	ResourceID       *string      `json:"resource_id,omitempty"`
	Commit           *string      `json:"commit,omitempty"`
	Path             *string      `json:"path,omitempty"`
	ReviewTaskID     *string      `json:"review_task_id,omitempty"`
	ExternalRef      *string      `json:"external_ref,omitempty"`
	present          map[string]json.RawMessage
}

func (source SourceRef) Validate() error {
	switch source.Kind {
	case EvidenceCheck, EvidenceArtifact, EvidenceReview, EvidenceObservation:
	default:
		return fmt.Errorf("source_ref.kind %q is invalid", source.Kind)
	}
	if err := validateShortIdentifier(source.Ref, "source_ref.ref"); err != nil {
		return err
	}
	if source.ResourceID != nil {
		if err := validateUUID(*source.ResourceID, "source_ref.resource_id"); err != nil {
			return err
		}
	}
	if source.hasNullField("resource_id") || source.hasNullField("commit") {
		return fmt.Errorf("source_ref optional identity fields must be strings when present")
	}
	if source.Commit != nil {
		if err := validateGitObjectID(*source.Commit); err != nil {
			return fmt.Errorf("source_ref.commit: %w", err)
		}
	}
	if source.ValidatorProfile != nil {
		if err := validateShortIdentifier(*source.ValidatorProfile, "source_ref.validator_profile"); err != nil {
			return err
		}
	}
	if source.SubjectHash != nil {
		if err := validateSHA256(*source.SubjectHash, "source_ref.subject_hash"); err != nil {
			return err
		}
	}
	if source.Path != nil {
		if err := validateCommitPath(*source.Path); err != nil {
			return err
		}
	}
	if source.ReviewTaskID != nil {
		if err := validateUUID(*source.ReviewTaskID, "source_ref.review_task_id"); err != nil {
			return err
		}
	}
	if source.ExternalRef != nil {
		if err := validateLongText(*source.ExternalRef, "source_ref.external_ref"); err != nil {
			return err
		}
	}
	return nil
}

func (source *SourceRef) UnmarshalJSON(data []byte) error {
	var decoded sourceRefWire
	if err := decodeStrictObject(data, &decoded, "kind", "ref"); err != nil {
		return err
	}
	value := SourceRef(decoded)
	fields, err := objectFields(data)
	if err != nil {
		return err
	}
	value.present = fields
	if err := value.Validate(); err != nil {
		return err
	}
	*source = value
	return nil
}

type sourceRefWire SourceRef

func (source SourceRef) hasNullField(name string) bool {
	if _, present := source.present[name]; !present {
		return false
	}
	return string(bytes.TrimSpace(source.present[name])) == "null"
}

func (source SourceRef) hasField(name string) bool {
	_, present := source.present[name]
	return present
}

// OutputRef points at bounded inline output or a durable artifact reference.
type OutputRef struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func (output OutputRef) Validate() error {
	if output.Kind != "inline" && output.Kind != "artifact" {
		return fmt.Errorf("output_ref.kind %q is invalid", output.Kind)
	}
	return validateLongText(output.Value, "output_ref.value")
}

func (output *OutputRef) UnmarshalJSON(data []byte) error {
	var decoded outputRefWire
	if err := decodeStrictObject(data, &decoded, "kind", "value"); err != nil {
		return err
	}
	value := OutputRef(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*output = value
	return nil
}

type outputRefWire OutputRef

// ReviewFinding is one bounded independent-review finding.
type ReviewFinding struct {
	Severity string  `json:"severity"`
	Code     *string `json:"code,omitempty"`
	Summary  string  `json:"summary"`
	Detail   *string `json:"detail,omitempty"`
	present  map[string]json.RawMessage
}

func (finding ReviewFinding) Validate() error {
	switch finding.Severity {
	case "info", "low", "medium", "high", "critical":
	default:
		return fmt.Errorf("review finding severity %q is invalid", finding.Severity)
	}
	if err := validateLongText(finding.Summary, "review finding summary"); err != nil {
		return err
	}
	if finding.Code != nil {
		if err := validateShortIdentifier(*finding.Code, "review finding code"); err != nil {
			return err
		}
	}
	if finding.hasNullField("code") || finding.hasNullField("detail") {
		return fmt.Errorf("review finding optional fields must be strings when present")
	}
	if finding.Detail != nil {
		if err := validateLongText(*finding.Detail, "review finding detail"); err != nil {
			return err
		}
	}
	return nil
}

func (finding *ReviewFinding) UnmarshalJSON(data []byte) error {
	var decoded reviewFindingWire
	if err := decodeStrictObject(data, &decoded, "severity", "summary"); err != nil {
		return err
	}
	value := ReviewFinding(decoded)
	fields, err := objectFields(data)
	if err != nil {
		return err
	}
	value.present = fields
	if err := value.Validate(); err != nil {
		return err
	}
	*finding = value
	return nil
}

type reviewFindingWire ReviewFinding

func (finding ReviewFinding) hasNullField(name string) bool {
	value, present := finding.present[name]
	return present && string(bytes.TrimSpace(value)) == "null"
}

// Receipt contains the union of the four schema-defined evidence receipt
// shapes. Evidence.Validate checks only the fields allowed for its kind.
type Receipt struct {
	PredicateID       *string          `json:"predicate_id,omitempty"`
	Subject           *Subject         `json:"subject,omitempty"`
	ProfileDigest     *string          `json:"profile_digest,omitempty"`
	CommandArgvDigest *string          `json:"command_argv_digest,omitempty"`
	ExitCode          *int64           `json:"exit_code,omitempty"`
	SubjectHash       *string          `json:"subject_hash,omitempty"`
	StartedAt         *string          `json:"started_at,omitempty"`
	FinishedAt        *string          `json:"finished_at,omitempty"`
	OutputRef         *OutputRef       `json:"output_ref,omitempty"`
	ResourceID        *string          `json:"resource_id,omitempty"`
	Commit            *string          `json:"commit,omitempty"`
	Path              *string          `json:"path,omitempty"`
	ContentDigest     *string          `json:"content_digest,omitempty"`
	ReviewTaskID      *string          `json:"review_task_id,omitempty"`
	Findings          []ReviewFinding  `json:"findings,omitempty"`
	Verdict           *EvidenceVerdict `json:"verdict,omitempty"`
	ExternalRef       *string          `json:"external_ref,omitempty"`
	ObservedAt        *string          `json:"observed_at,omitempty"`
	Note              *string          `json:"note,omitempty"`
	present           map[string]json.RawMessage
}

func (receipt *Receipt) UnmarshalJSON(data []byte) error {
	var decoded receiptWire
	if err := decodeStrictObject(data, &decoded); err != nil {
		return err
	}
	value := Receipt(decoded)
	fields, err := objectFields(data)
	if err != nil {
		return err
	}
	value.present = fields
	*receipt = value
	return nil
}

type receiptWire Receipt

func (receipt Receipt) validateForKind(kind EvidenceKind) error {
	allowed := map[EvidenceKind]map[string]struct{}{
		EvidenceCheck: {
			"predicate_id": {}, "subject": {}, "subject_hash": {},
			"profile_digest": {}, "command_argv_digest": {}, "exit_code": {},
			"started_at": {}, "finished_at": {}, "output_ref": {},
		},
		EvidenceArtifact: {
			"predicate_id": {}, "subject": {}, "subject_hash": {},
			"resource_id": {}, "commit": {}, "path": {}, "content_digest": {},
		},
		EvidenceReview: {
			"predicate_id": {}, "subject": {}, "subject_hash": {},
			"review_task_id": {}, "findings": {}, "verdict": {},
		},
		EvidenceObservation: {
			"predicate_id": {}, "subject": {}, "subject_hash": {},
			"external_ref": {}, "observed_at": {}, "note": {},
		},
	}
	for field := range receipt.present {
		if _, known := allowed[kind][field]; !known {
			return fmt.Errorf("payload field %q is not allowed for kind %q", field, kind)
		}
	}
	if receipt.PredicateID == nil {
		return fmt.Errorf("payload.predicate_id is required")
	}
	if err := validateShortIdentifier(*receipt.PredicateID, "payload.predicate_id"); err != nil {
		return err
	}
	if receipt.Subject == nil || receipt.SubjectHash == nil {
		return fmt.Errorf("payload subject and subject_hash are required")
	}
	if err := receipt.Subject.Validate(); err != nil {
		return fmt.Errorf("payload.subject: %w", err)
	}
	if err := validateSHA256(*receipt.SubjectHash, "payload.subject_hash"); err != nil {
		return err
	}
	switch kind {
	case EvidenceCheck:
		if receipt.ProfileDigest == nil || receipt.CommandArgvDigest == nil || receipt.ExitCode == nil || receipt.StartedAt == nil || receipt.FinishedAt == nil || receipt.OutputRef == nil {
			return fmt.Errorf("check payload is missing a required field")
		}
		if err := validateSHA256(*receipt.ProfileDigest, "payload.profile_digest"); err != nil {
			return err
		}
		if err := validateSHA256(*receipt.CommandArgvDigest, "payload.command_argv_digest"); err != nil {
			return err
		}
		if *receipt.ExitCode < 0 || *receipt.ExitCode > 255 {
			return fmt.Errorf("payload.exit_code must be between 0 and 255")
		}
		if err := validateUTCTimestamp(*receipt.StartedAt, "payload.started_at"); err != nil {
			return err
		}
		if err := validateUTCTimestamp(*receipt.FinishedAt, "payload.finished_at"); err != nil {
			return err
		}
		return receipt.OutputRef.Validate()
	case EvidenceArtifact:
		if receipt.ResourceID == nil || receipt.Commit == nil || receipt.Path == nil || receipt.ContentDigest == nil {
			return fmt.Errorf("artifact payload is missing a required field")
		}
		if err := validateUUID(*receipt.ResourceID, "payload.resource_id"); err != nil {
			return err
		}
		if err := validateGitObjectID(*receipt.Commit); err != nil {
			return err
		}
		if err := validateCommitPath(*receipt.Path); err != nil {
			return err
		}
		return validateSHA256(*receipt.ContentDigest, "payload.content_digest")
	case EvidenceReview:
		if receipt.ReviewTaskID == nil || receipt.Verdict == nil || receipt.Findings == nil || !receipt.hasField("findings") {
			return fmt.Errorf("review payload is missing a required field")
		}
		if err := validateUUID(*receipt.ReviewTaskID, "payload.review_task_id"); err != nil {
			return err
		}
		if err := validateEvidenceVerdict(*receipt.Verdict, "payload.verdict"); err != nil {
			return err
		}
		if len(receipt.Findings) > 512 {
			return fmt.Errorf("payload.findings must contain at most 512 items")
		}
		for index := range receipt.Findings {
			if err := receipt.Findings[index].Validate(); err != nil {
				return fmt.Errorf("payload.findings[%d]: %w", index, err)
			}
		}
		return nil
	case EvidenceObservation:
		if receipt.ExternalRef == nil || receipt.ObservedAt == nil || receipt.Note == nil {
			return fmt.Errorf("observation payload is missing a required field")
		}
		if err := validateLongText(*receipt.ExternalRef, "payload.external_ref"); err != nil {
			return err
		}
		if err := validateUTCTimestamp(*receipt.ObservedAt, "payload.observed_at"); err != nil {
			return err
		}
		return validateLongText(*receipt.Note, "payload.note")
	default:
		return fmt.Errorf("kind %q is invalid", kind)
	}
}

func (receipt Receipt) hasField(name string) bool {
	_, present := receipt.present[name]
	return present
}

// Evidence is a normalized validator receipt bound to one run and subject.
type Evidence struct {
	SchemaVersion    string          `json:"schema_version"`
	EvidenceID       string          `json:"evidence_id"`
	RunID            string          `json:"run_id"`
	EvidenceKey      string          `json:"evidence_key"`
	Kind             EvidenceKind    `json:"kind"`
	Subject          Subject         `json:"subject"`
	SubjectHash      string          `json:"subject_hash"`
	SourceRef        SourceRef       `json:"source_ref"`
	SourceRevision   string          `json:"source_revision"`
	ValidatorProfile *string         `json:"validator_profile"`
	Verdict          EvidenceVerdict `json:"verdict"`
	Payload          Receipt         `json:"payload"`
	ObservedAt       string          `json:"observed_at"`
}

func (evidence Evidence) Validate() error {
	if evidence.SchemaVersion != EvidenceSchemaVersion {
		return fmt.Errorf("schema_version must be %q", EvidenceSchemaVersion)
	}
	if err := validateUUID(evidence.EvidenceID, "evidence_id"); err != nil {
		return err
	}
	if err := validateUUID(evidence.RunID, "run_id"); err != nil {
		return err
	}
	if err := validateShortIdentifier(evidence.EvidenceKey, "evidence_key"); err != nil {
		return err
	}
	switch evidence.Kind {
	case EvidenceCheck, EvidenceArtifact, EvidenceReview, EvidenceObservation:
	default:
		return fmt.Errorf("kind %q is invalid", evidence.Kind)
	}
	if err := validateSHA256(evidence.SubjectHash, "subject_hash"); err != nil {
		return err
	}
	if err := evidence.Subject.Validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	computedSubjectHash, err := evidence.Subject.Hash()
	if err != nil {
		return err
	}
	if computedSubjectHash != evidence.SubjectHash {
		return fmt.Errorf("subject_hash does not match canonical subject")
	}
	if err := evidence.SourceRef.Validate(); err != nil {
		return err
	}
	if evidence.SourceRef.Kind != evidence.Kind {
		return fmt.Errorf("source_ref.kind %q does not match evidence kind %q", evidence.SourceRef.Kind, evidence.Kind)
	}
	if evidence.SourceRef.SubjectHash == nil || *evidence.SourceRef.SubjectHash != evidence.SubjectHash {
		return fmt.Errorf("source_ref.subject_hash must match evidence.subject_hash")
	}
	if evidence.Kind == EvidenceCheck || evidence.Kind == EvidenceArtifact || evidence.Kind == EvidenceReview {
		if evidence.ValidatorProfile == nil {
			return fmt.Errorf("validator_profile is required for %s evidence", evidence.Kind)
		}
	}
	if evidence.ValidatorProfile != nil {
		if err := validateShortIdentifier(*evidence.ValidatorProfile, "validator_profile"); err != nil {
			return err
		}
	}
	if evidence.SourceRef.ValidatorProfile != nil && evidence.ValidatorProfile != nil && *evidence.SourceRef.ValidatorProfile != *evidence.ValidatorProfile {
		return fmt.Errorf("source_ref.validator_profile must match validator_profile")
	}
	if evidence.Kind == EvidenceCheck && evidence.SourceRef.ValidatorProfile == nil {
		return fmt.Errorf("check source_ref.validator_profile is required")
	}
	switch evidence.Kind {
	case EvidenceCheck:
		if evidence.SourceRef.ValidatorProfile == nil {
			return fmt.Errorf("check source_ref.validator_profile is required")
		}
		if evidence.SourceRef.hasArtifactIdentity() || evidence.SourceRef.hasReviewIdentity() || evidence.SourceRef.hasObservationIdentity() {
			return fmt.Errorf("source_ref identity fields are not valid for check evidence")
		}
	case EvidenceArtifact:
		if evidence.SourceRef.ResourceID == nil || evidence.SourceRef.Commit == nil || evidence.SourceRef.Path == nil {
			return fmt.Errorf("artifact source_ref requires resource_id, commit, and path")
		}
		if evidence.SourceRef.hasField("validator_profile") || evidence.SourceRef.hasReviewIdentity() || evidence.SourceRef.hasObservationIdentity() {
			return fmt.Errorf("source_ref identity fields are not valid for artifact evidence")
		}
	case EvidenceReview:
		if evidence.SourceRef.ReviewTaskID == nil {
			return fmt.Errorf("review source_ref.review_task_id is required")
		}
		if evidence.SourceRef.hasField("validator_profile") || evidence.SourceRef.hasArtifactIdentity() || evidence.SourceRef.hasObservationIdentity() {
			return fmt.Errorf("source_ref identity fields are not valid for review evidence")
		}
	case EvidenceObservation:
		if evidence.SourceRef.ExternalRef == nil {
			return fmt.Errorf("observation source_ref.external_ref is required")
		}
		if evidence.SourceRef.hasField("validator_profile") || evidence.SourceRef.hasArtifactIdentity() || evidence.SourceRef.hasReviewIdentity() {
			return fmt.Errorf("source_ref identity fields are not valid for observation evidence")
		}
	}
	if err := validateShortIdentifier(evidence.SourceRevision, "source_revision"); err != nil {
		return err
	}
	if evidence.ValidatorProfile != nil {
		if err := validateShortIdentifier(*evidence.ValidatorProfile, "validator_profile"); err != nil {
			return err
		}
	}
	if err := validateEvidenceVerdict(evidence.Verdict, "verdict"); err != nil {
		return err
	}
	if err := evidence.Payload.validateForKind(evidence.Kind); err != nil {
		return err
	}
	if evidence.Kind == EvidenceReview && evidence.Payload.Verdict != nil && *evidence.Payload.Verdict != evidence.Verdict {
		return fmt.Errorf("payload.verdict must match evidence.verdict for review evidence")
	}
	if evidence.Payload.Subject == nil || *evidence.Payload.Subject != evidence.Subject || evidence.Payload.SubjectHash == nil || *evidence.Payload.SubjectHash != evidence.SubjectHash {
		return fmt.Errorf("payload subject and hashes must match evidence subject")
	}
	switch evidence.Kind {
	case EvidenceArtifact:
		if evidence.SourceRef.ResourceID == nil || evidence.Payload.ResourceID == nil || *evidence.SourceRef.ResourceID != *evidence.Payload.ResourceID ||
			evidence.SourceRef.Commit == nil || evidence.Payload.Commit == nil || *evidence.SourceRef.Commit != *evidence.Payload.Commit ||
			evidence.SourceRef.Path == nil || evidence.Payload.Path == nil || *evidence.SourceRef.Path != *evidence.Payload.Path {
			return fmt.Errorf("artifact source_ref and payload identity must match")
		}
	case EvidenceReview:
		if evidence.SourceRef.ReviewTaskID == nil || evidence.Payload.ReviewTaskID == nil || *evidence.SourceRef.ReviewTaskID != *evidence.Payload.ReviewTaskID {
			return fmt.Errorf("review source_ref and payload identity must match")
		}
	case EvidenceObservation:
		if evidence.SourceRef.ExternalRef == nil || evidence.Payload.ExternalRef == nil || *evidence.SourceRef.ExternalRef != *evidence.Payload.ExternalRef {
			return fmt.Errorf("observation source_ref and payload identity must match")
		}
	}
	if evidence.Kind == EvidenceCheck || evidence.Kind == EvidenceReview {
		if evidence.Payload.SubjectHash == nil || *evidence.Payload.SubjectHash != evidence.SubjectHash {
			return fmt.Errorf("payload.subject_hash must match evidence.subject_hash")
		}
	}
	return validateUTCTimestamp(evidence.ObservedAt, "observed_at")
}

func (source SourceRef) hasArtifactIdentity() bool {
	return source.ResourceID != nil || source.Commit != nil || source.Path != nil ||
		source.hasField("resource_id") || source.hasField("commit") || source.hasField("path")
}

func (source SourceRef) hasReviewIdentity() bool {
	return source.ReviewTaskID != nil || source.hasField("review_task_id")
}

func (source SourceRef) hasObservationIdentity() bool {
	return source.ExternalRef != nil || source.hasField("external_ref")
}

func (evidence *Evidence) UnmarshalJSON(data []byte) error {
	var decoded evidenceWire
	if err := decodeStrictObject(data, &decoded,
		"schema_version", "evidence_id", "run_id", "evidence_key", "kind", "subject", "subject_hash",
		"source_ref", "source_revision", "validator_profile", "verdict", "payload", "observed_at"); err != nil {
		return err
	}
	value := Evidence(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*evidence = value
	return nil
}

type evidenceWire Evidence

// ParseEvidence decodes and validates a normalized evidence receipt.
func ParseEvidence(data []byte) (Evidence, error) {
	var wire contractdto.SymmetryEvidenceV1
	var evidence Evidence
	if err := decodeGoalEnvelope(data, contractdto.EnvelopeEvidence, &wire, &evidence); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

// CostBasis identifies the provenance of a monetary usage amount.
type CostBasis string

const (
	CostReported  CostBasis = "reported"
	CostEstimated CostBasis = "estimated"
	CostUnknown   CostBasis = "unknown"
)

// Usage is normalized provider accounting. Monetary values remain decimal
// strings on the wire so they never pass through JSON floating point numbers.
type Usage struct {
	SchemaVersion     string    `json:"schema_version"`
	UsageID           string    `json:"usage_id"`
	RunID             string    `json:"run_id"`
	UsageKey          string    `json:"usage_key"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	InputTokens       *int64    `json:"input_tokens"`
	OutputTokens      *int64    `json:"output_tokens"`
	CachedInputTokens *int64    `json:"cached_input_tokens"`
	CostMicrousd      *string   `json:"cost_microusd"`
	CostBasis         CostBasis `json:"cost_basis"`
	PriceVersion      *string   `json:"price_version"`
	SupersedesID      *string   `json:"supersedes_id"`
	ObservedAt        string    `json:"observed_at"`
}

func (usage Usage) Validate() error {
	if usage.SchemaVersion != UsageSchemaVersion {
		return fmt.Errorf("schema_version must be %q", UsageSchemaVersion)
	}
	if err := validateUUID(usage.UsageID, "usage_id"); err != nil {
		return err
	}
	if err := validateUUID(usage.RunID, "run_id"); err != nil {
		return err
	}
	if err := validateShortIdentifier(usage.UsageKey, "usage_key"); err != nil {
		return err
	}
	if err := validateShortIdentifier(usage.Provider, "provider"); err != nil {
		return err
	}
	if err := validateShortIdentifier(usage.Model, "model"); err != nil {
		return err
	}
	for field, value := range map[string]*int64{
		"input_tokens":        usage.InputTokens,
		"output_tokens":       usage.OutputTokens,
		"cached_input_tokens": usage.CachedInputTokens,
	} {
		if value != nil {
			if err := validateSafeNonNegativeInt(*value, field); err != nil {
				return err
			}
		}
	}
	switch usage.CostBasis {
	case CostReported, CostEstimated:
		if usage.CostMicrousd == nil {
			return fmt.Errorf("cost_microusd is required when cost_basis is %q", usage.CostBasis)
		}
		if err := ValidateMicroUSD(*usage.CostMicrousd); err != nil {
			return err
		}
	case CostUnknown:
		if usage.CostMicrousd != nil {
			return fmt.Errorf("cost_microusd must be null when cost_basis is %q", usage.CostBasis)
		}
	default:
		return fmt.Errorf("cost_basis %q is invalid", usage.CostBasis)
	}
	if usage.PriceVersion != nil {
		if err := validateShortIdentifier(*usage.PriceVersion, "price_version"); err != nil {
			return err
		}
	}
	if usage.SupersedesID != nil {
		if err := validateUUID(*usage.SupersedesID, "supersedes_id"); err != nil {
			return err
		}
	}
	return validateUTCTimestamp(usage.ObservedAt, "observed_at")
}

func (usage *Usage) UnmarshalJSON(data []byte) error {
	var decoded usageWire
	if err := decodeStrictObject(data, &decoded,
		"schema_version", "usage_id", "run_id", "usage_key", "provider", "model",
		"input_tokens", "output_tokens", "cached_input_tokens", "cost_microusd",
		"cost_basis", "price_version", "supersedes_id", "observed_at"); err != nil {
		return err
	}
	value := Usage(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*usage = value
	return nil
}

type usageWire Usage

// ParseUsage decodes and validates normalized provider usage.
func ParseUsage(data []byte) (Usage, error) {
	var wire contractdto.SymmetryUsageV1
	var usage Usage
	if err := decodeGoalEnvelope(data, contractdto.EnvelopeUsage, &wire, &usage); err != nil {
		return Usage{}, err
	}
	return usage, nil
}
