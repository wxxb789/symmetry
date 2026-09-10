package protocol

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	contractdto "github.com/wxxb789/symmetry/daemon/internal/contracts"
)

// AdmissionPurpose identifies the bounded native turn being admitted.
type AdmissionPurpose string

const (
	AdmissionPurposeImplement AdmissionPurpose = "implement"
	AdmissionPurposeValidate  AdmissionPurpose = "validate"
	AdmissionPurposePlan      AdmissionPurpose = "plan"
	AdmissionPurposeObserve   AdmissionPurpose = "observe"
	AdmissionPurposeChat      AdmissionPurpose = "chat"
)

// SessionMode identifies how the native session is selected for an admission.
type SessionMode string

const (
	SessionModeFresh   SessionMode = "fresh"
	SessionModeResume  SessionMode = "resume"
	SessionModeHandoff SessionMode = "handoff"
)

// AdmissionLimits bounds one host invocation under an admission.
type AdmissionLimits struct {
	MaxTurns        int64   `json:"max_turns"`
	DeadlineAt      string  `json:"deadline_at"`
	MaxCostMicrousd *string `json:"max_cost_microusd"`
}

func (limits AdmissionLimits) Validate() error {
	if err := validateSafePositiveInt(limits.MaxTurns, "limits.max_turns"); err != nil {
		return err
	}
	if err := validateUTCTimestamp(limits.DeadlineAt, "limits.deadline_at"); err != nil {
		return err
	}
	if limits.MaxCostMicrousd != nil {
		if err := ValidateMicroUSD(*limits.MaxCostMicrousd); err != nil {
			return fmt.Errorf("limits.max_cost_microusd: %w", err)
		}
	}
	return nil
}

func (limits *AdmissionLimits) UnmarshalJSON(data []byte) error {
	var decoded admissionLimitsWire
	if err := decodeStrictObject(data, &decoded, "max_turns", "deadline_at", "max_cost_microusd"); err != nil {
		return err
	}
	value := AdmissionLimits(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*limits = value
	return nil
}

type admissionLimitsWire AdmissionLimits

// ProviderOperation is one provider-broker action explicitly granted for a
// resource. It is intentionally narrower than a provider's native API.
type ProviderOperation string

const (
	ProviderOperationResourceSync ProviderOperation = "resource.sync"
	ProviderOperationChangeUpsert ProviderOperation = "change.upsert"
	ProviderOperationChangeUpdate ProviderOperation = "change.update"
)

// ProviderScope binds an admission to an exact set of broker resource grants.
// The daemon must not infer operations or resources from local configuration.
type ProviderScope struct {
	ResourceIDs          []string                       `json:"resource_ids"`
	OperationsByResource map[string][]ProviderOperation `json:"operations_by_resource"`
	ChangeTarget         *ProviderChangeTarget          `json:"change_target"`
}

func (scope ProviderScope) Validate() error {
	if len(scope.ResourceIDs) == 0 || len(scope.ResourceIDs) > 256 {
		return fmt.Errorf("provider_scope.resource_ids must contain 1..256 resources")
	}
	if len(scope.OperationsByResource) == 0 || len(scope.OperationsByResource) > 256 {
		return fmt.Errorf("provider_scope.operations_by_resource must contain 1..256 resource grants")
	}

	resourceIDs := make(map[string]struct{}, len(scope.ResourceIDs))
	for index, resourceID := range scope.ResourceIDs {
		if err := validateUUID(resourceID, fmt.Sprintf("provider_scope.resource_ids[%d]", index)); err != nil {
			return err
		}
		if _, duplicate := resourceIDs[resourceID]; duplicate {
			return fmt.Errorf("provider_scope.resource_ids contains duplicate UUID %q", resourceID)
		}
		resourceIDs[resourceID] = struct{}{}
	}
	if len(resourceIDs) != len(scope.OperationsByResource) {
		return fmt.Errorf("provider_scope.operations_by_resource keys must equal provider_scope.resource_ids")
	}
	for resourceID, operations := range scope.OperationsByResource {
		if _, granted := resourceIDs[resourceID]; !granted {
			return fmt.Errorf("provider_scope.operations_by_resource key %q is not a scoped resource", resourceID)
		}
		if err := validateProviderOperations(operations, resourceID); err != nil {
			return err
		}
	}
	for resourceID := range resourceIDs {
		if _, present := scope.OperationsByResource[resourceID]; !present {
			return fmt.Errorf("provider_scope scoped resource %q has no operation set", resourceID)
		}
	}
	if scope.ChangeTarget != nil {
		return scope.ChangeTarget.Validate()
	}
	return nil
}

func (scope *ProviderScope) UnmarshalJSON(data []byte) error {
	var decoded providerScopeWire
	if err := decodeStrictObject(data, &decoded, "resource_ids", "operations_by_resource", "change_target"); err != nil {
		return err
	}
	value := ProviderScope(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*scope = value
	return nil
}

type providerScopeWire ProviderScope

func validateProviderOperations(operations []ProviderOperation, resourceID string) error {
	if len(operations) == 0 || len(operations) > 3 {
		return fmt.Errorf("provider_scope.operations_by_resource[%q] must contain 1..3 operations", resourceID)
	}
	seen := make(map[ProviderOperation]struct{}, len(operations))
	for index, operation := range operations {
		switch operation {
		case ProviderOperationResourceSync, ProviderOperationChangeUpsert, ProviderOperationChangeUpdate:
		default:
			return fmt.Errorf("provider_scope.operations_by_resource[%q][%d] %q is invalid", resourceID, index, operation)
		}
		if _, duplicate := seen[operation]; duplicate {
			return fmt.Errorf("provider_scope.operations_by_resource[%q] contains duplicate operation %q", resourceID, operation)
		}
		seen[operation] = struct{}{}
	}
	return nil
}

// ProviderChangeTarget identifies the one allowed branch or pull request
// target for provider-side change operations. It is nullable at the scope
// level because read-only scopes have no change target.
type ProviderChangeTarget struct {
	Kind           ProviderChangeTargetKind `json:"kind"`
	SourceBranch   *string                  `json:"source_branch,omitempty"`
	TargetBranch   *string                  `json:"target_branch,omitempty"`
	PullRequestURL *string                  `json:"pull_request_url,omitempty"`
}

type ProviderChangeTargetKind string

const (
	ProviderChangeTargetBranches    ProviderChangeTargetKind = "branches"
	ProviderChangeTargetPullRequest ProviderChangeTargetKind = "pull_request"
)

func (target ProviderChangeTarget) Validate() error {
	switch target.Kind {
	case ProviderChangeTargetBranches:
		if target.SourceBranch == nil || target.TargetBranch == nil || target.PullRequestURL != nil {
			return errorsProviderChangeTargetShape(target.Kind)
		}
		if *target.SourceBranch == *target.TargetBranch {
			return fmt.Errorf("provider_scope.change_target source_branch and target_branch must differ")
		}
		if err := validateProviderTargetString(*target.SourceBranch, "provider_scope.change_target.source_branch", 255); err != nil {
			return err
		}
		return validateProviderTargetString(*target.TargetBranch, "provider_scope.change_target.target_branch", 255)
	case ProviderChangeTargetPullRequest:
		if target.PullRequestURL == nil || target.SourceBranch != nil || target.TargetBranch != nil {
			return errorsProviderChangeTargetShape(target.Kind)
		}
		return validateLongText(*target.PullRequestURL, "provider_scope.change_target.pull_request_url")
	default:
		return fmt.Errorf("provider_scope.change_target.kind %q is invalid", target.Kind)
	}
}

func (target *ProviderChangeTarget) UnmarshalJSON(data []byte) error {
	var discriminator struct {
		Kind ProviderChangeTargetKind `json:"kind"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return fmt.Errorf("decode provider_scope.change_target kind: %w", err)
	}
	if discriminator.Kind == "" {
		return fmt.Errorf("provider_scope.change_target.kind is required")
	}
	if !isJSONObject(data) {
		return fmt.Errorf("provider_scope.change_target must be an object")
	}

	var decoded providerChangeTargetWire
	switch discriminator.Kind {
	case ProviderChangeTargetBranches:
		if err := decodeStrictObject(data, &decoded, "kind", "source_branch", "target_branch"); err != nil {
			return err
		}
	case ProviderChangeTargetPullRequest:
		if err := decodeStrictObject(data, &decoded, "kind", "pull_request_url"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("provider_scope.change_target.kind %q is invalid", discriminator.Kind)
	}

	value := ProviderChangeTarget(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*target = value
	return nil
}

type providerChangeTargetWire ProviderChangeTarget

func errorsProviderChangeTargetShape(kind ProviderChangeTargetKind) error {
	return fmt.Errorf("provider_scope.change_target %q has an invalid field set", kind)
}

func validateProviderTargetString(value, field string, maximum int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must contain valid UTF-8", field)
	}
	length := utf8.RuneCountInString(value)
	if length < 1 || length > maximum {
		return fmt.Errorf("%s must contain 1..%d characters", field, maximum)
	}
	return nil
}

// Admission is the server-to-daemon Goal 0006 transport envelope. It is
// additive to Work.Input and does not alter the legacy v1 Work decoder.
type Admission struct {
	SchemaVersion string `json:"schema_version"`
	AdmissionID   string `json:"admission_id"`
	GoalID        string `json:"goal_id"`
	GoalRevision  int64  `json:"goal_revision"`
	// WorkItemID is nil only for an operator-requested planning admission. Do
	// not encode the absence of a work item as an empty UUID-like string.
	WorkItemID         *string          `json:"work_item_id"`
	Purpose            AdmissionPurpose `json:"purpose"`
	ContextSnapshotID  string           `json:"context_snapshot_id"`
	ContextHash        string           `json:"context_hash"`
	ModelProfile       string           `json:"model_profile"`
	SessionMode        SessionMode      `json:"session_mode"`
	RequestedSessionID *string          `json:"requested_session_id"`
	HandoffSourceRunID *string          `json:"handoff_source_run_id,omitempty"`
	Subject            Subject          `json:"subject"`
	Limits             AdmissionLimits  `json:"limits"`
	ValidationOfTaskID *string          `json:"validation_of_task_id"`
	ProviderScope      *ProviderScope   `json:"provider_scope"`
}

func (admission Admission) Validate() error {
	if admission.SchemaVersion != AdmissionSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AdmissionSchemaVersion)
	}
	if err := validateUUID(admission.AdmissionID, "admission_id"); err != nil {
		return err
	}
	if err := validateUUID(admission.GoalID, "goal_id"); err != nil {
		return err
	}
	if err := validateSafePositiveInt(admission.GoalRevision, "goal_revision"); err != nil {
		return err
	}
	if !validAdmissionPurpose(admission.Purpose) {
		return fmt.Errorf("purpose %q is invalid", admission.Purpose)
	}
	if admission.Purpose == AdmissionPurposePlan {
		if admission.WorkItemID != nil {
			return fmt.Errorf("work_item_id must be null for purpose %q", admission.Purpose)
		}
	} else {
		if admission.WorkItemID == nil {
			return fmt.Errorf("work_item_id is required for purpose %q", admission.Purpose)
		}
		if err := validateUUID(*admission.WorkItemID, "work_item_id"); err != nil {
			return err
		}
	}
	if err := validateUUID(admission.ContextSnapshotID, "context_snapshot_id"); err != nil {
		return err
	}
	if err := validateSHA256(admission.ContextHash, "context_hash"); err != nil {
		return err
	}
	if err := validateShortIdentifier(admission.ModelProfile, "model_profile"); err != nil {
		return err
	}
	if !validSessionMode(admission.SessionMode) {
		return fmt.Errorf("session_mode %q is invalid", admission.SessionMode)
	}
	switch admission.SessionMode {
	case SessionModeFresh:
		if admission.RequestedSessionID != nil {
			return fmt.Errorf("requested_session_id must be null for session_mode %q", admission.SessionMode)
		}
		if admission.HandoffSourceRunID != nil {
			return fmt.Errorf("handoff_source_run_id must be null for session_mode %q", admission.SessionMode)
		}
	case SessionModeResume:
		if admission.RequestedSessionID == nil {
			return fmt.Errorf("requested_session_id is required for session_mode %q", admission.SessionMode)
		}
		if err := validateUUID(*admission.RequestedSessionID, "requested_session_id"); err != nil {
			return err
		}
		if admission.HandoffSourceRunID != nil {
			return fmt.Errorf("handoff_source_run_id must be null for session_mode %q", admission.SessionMode)
		}
	case SessionModeHandoff:
		if admission.RequestedSessionID != nil {
			return fmt.Errorf("requested_session_id must be null for session_mode %q", admission.SessionMode)
		}
		if admission.HandoffSourceRunID == nil {
			return fmt.Errorf("handoff_source_run_id is required for session_mode %q", admission.SessionMode)
		}
		if err := validateUUID(*admission.HandoffSourceRunID, "handoff_source_run_id"); err != nil {
			return err
		}
	}
	if admission.Purpose == AdmissionPurposeValidate {
		if admission.ValidationOfTaskID == nil {
			return fmt.Errorf("validation_of_task_id is required for purpose %q", admission.Purpose)
		}
	} else if admission.ValidationOfTaskID != nil {
		return fmt.Errorf("validation_of_task_id must be null for purpose %q", admission.Purpose)
	}
	if admission.ValidationOfTaskID != nil {
		if err := validateUUID(*admission.ValidationOfTaskID, "validation_of_task_id"); err != nil {
			return err
		}
	}
	if err := admission.Subject.Validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if admission.Purpose != AdmissionPurposeImplement && admission.ProviderScope != nil {
		return fmt.Errorf("provider_scope must be null for purpose %q", admission.Purpose)
	}
	if admission.ProviderScope != nil {
		if err := admission.ProviderScope.Validate(); err != nil {
			return fmt.Errorf("provider_scope: %w", err)
		}
	}
	return admission.Limits.Validate()
}

func (admission *Admission) UnmarshalJSON(data []byte) error {
	var decoded admissionWire
	if err := decodeStrictObject(data, &decoded,
		"schema_version", "admission_id", "goal_id", "goal_revision", "work_item_id",
		"purpose", "context_snapshot_id", "context_hash", "model_profile", "session_mode",
		"requested_session_id", "subject", "limits", "validation_of_task_id", "provider_scope"); err != nil {
		return err
	}
	value := Admission(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*admission = value
	return nil
}

type admissionWire Admission

// ParseAdmission decodes and validates a Goal 0006 admission envelope.
func ParseAdmission(data []byte) (Admission, error) {
	var wire contractdto.SymmetryAdmissionV1
	var admission Admission
	if err := decodeGoalEnvelope(data, contractdto.EnvelopeAdmission, &wire, &admission); err != nil {
		return Admission{}, err
	}
	return admission, nil
}

func validAdmissionPurpose(value AdmissionPurpose) bool {
	switch value {
	case AdmissionPurposeImplement, AdmissionPurposeValidate, AdmissionPurposePlan,
		AdmissionPurposeObserve, AdmissionPurposeChat:
		return true
	default:
		return false
	}
}

func validSessionMode(value SessionMode) bool {
	switch value {
	case SessionModeFresh, SessionModeResume, SessionModeHandoff:
		return true
	default:
		return false
	}
}

// GuidanceCapability describes how an adapter accepts operator guidance.
type GuidanceCapability string

const (
	GuidanceNativeSteer GuidanceCapability = "native_steer"
	GuidanceNextTurn    GuidanceCapability = "next_turn"
	GuidanceUnsupported GuidanceCapability = "unsupported"
)

// PauseCapability describes whether a native safe retained boundary is
// available. Process interruption is not represented by this enum.
type PauseCapability string

const (
	PauseSafeBoundary PauseCapability = "safe_boundary"
	PauseUnsupported  PauseCapability = "unsupported"
)

// UsageCapability records whether an adapter reports or estimates usage.
type UsageCapability string

const (
	UsageReported  UsageCapability = "reported"
	UsageEstimated UsageCapability = "estimated"
	UsageUnknown   UsageCapability = "unknown"
)

// AdapterOperations is the versioned native operation advertisement. All
// fields are required in the transport object, including false capabilities.
type AdapterOperations struct {
	Start            bool               `json:"start"`
	Events           bool               `json:"events"`
	Cancel           bool               `json:"cancel"`
	Resume           bool               `json:"resume"`
	Handoff          bool               `json:"handoff"`
	Guidance         GuidanceCapability `json:"guidance"`
	Pause            PauseCapability    `json:"pause"`
	ApprovalResponse bool               `json:"approval_response"`
	Usage            UsageCapability    `json:"usage"`
	HardCostLimit    bool               `json:"hard_cost_limit"`
}

func (operations AdapterOperations) Validate() error {
	switch operations.Guidance {
	case GuidanceNativeSteer, GuidanceNextTurn, GuidanceUnsupported:
	default:
		return fmt.Errorf("operations.guidance %q is invalid", operations.Guidance)
	}
	switch operations.Pause {
	case PauseSafeBoundary, PauseUnsupported:
	default:
		return fmt.Errorf("operations.pause %q is invalid", operations.Pause)
	}
	switch operations.Usage {
	case UsageReported, UsageEstimated, UsageUnknown:
	default:
		return fmt.Errorf("operations.usage %q is invalid", operations.Usage)
	}
	if operations.Resume && !operations.Start {
		return fmt.Errorf("operations.resume requires operations.start")
	}
	if operations.Handoff && (!operations.Start || !operations.Events || !operations.Cancel) {
		return fmt.Errorf("operations.handoff requires operations.start, operations.events, and operations.cancel")
	}
	if operations.Events && !operations.Start {
		return fmt.Errorf("operations.events requires operations.start")
	}
	if operations.Guidance == GuidanceNativeSteer && !operations.Start {
		return fmt.Errorf("operations.guidance native_steer requires operations.start")
	}
	if operations.Pause == PauseSafeBoundary && !operations.Resume {
		return fmt.Errorf("operations.pause safe_boundary requires operations.resume")
	}
	if operations.Usage == UsageReported && !operations.Events {
		return fmt.Errorf("operations.usage reported requires operations.events")
	}
	return nil
}

func (operations AdapterOperations) validateForHarness(harnessKind string) error {
	if harnessKind != "generic" {
		return nil
	}
	if operations.Resume || operations.Handoff || operations.Guidance != GuidanceUnsupported ||
		operations.Pause != PauseUnsupported || operations.ApprovalResponse ||
		operations.Usage != UsageUnknown || operations.HardCostLimit {
		return fmt.Errorf("generic adapter operations advertise unsupported native behavior")
	}
	return nil
}

func (operations *AdapterOperations) UnmarshalJSON(data []byte) error {
	var decoded adapterOperationsWire
	if err := decodeStrictObject(data, &decoded,
		"start", "events", "cancel", "resume", "handoff", "guidance", "pause",
		"approval_response", "usage", "hard_cost_limit"); err != nil {
		return err
	}
	value := AdapterOperations(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*operations = value
	return nil
}

type adapterOperationsWire AdapterOperations

// Adapter describes one exact native integration/version.
type Adapter struct {
	Kind                  string            `json:"kind"`
	NativeVersion         string            `json:"native_version"`
	ImplementationVersion string            `json:"implementation_version"`
	ProtocolVersion       int64             `json:"protocol_version"`
	Operations            AdapterOperations `json:"operations"`
}

func (adapter Adapter) Validate() error {
	if err := validateShortIdentifier(adapter.Kind, "adapter.kind"); err != nil {
		return err
	}
	if err := validateShortIdentifier(adapter.NativeVersion, "adapter.native_version"); err != nil {
		return err
	}
	if err := validateShortIdentifier(adapter.ImplementationVersion, "adapter.implementation_version"); err != nil {
		return err
	}
	if err := validateSafePositiveInt(adapter.ProtocolVersion, "adapter.protocol_version"); err != nil {
		return err
	}
	return adapter.Operations.Validate()
}

func (adapter *Adapter) UnmarshalJSON(data []byte) error {
	var decoded adapterWire
	if err := decodeStrictObject(data, &decoded,
		"kind", "native_version", "implementation_version", "protocol_version", "operations"); err != nil {
		return err
	}
	value := Adapter(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*adapter = value
	return nil
}

type adapterWire Adapter

// ParseAdapterCapabilities decodes the exact capabilities object embedded in a
// runtime registration. It preserves the legacy boolean fields and adds the
// versioned adapter object without inventing a parallel envelope.
func ParseAdapterCapabilities(data []byte) (RuntimeCapabilities, error) {
	var wire contractdto.SymmetryAdapterCapabilitiesV1
	var capabilities RuntimeCapabilities
	if err := decodeGoalEnvelope(data, contractdto.EnvelopeAdapterCapabilities, &wire, &capabilities); err != nil {
		return RuntimeCapabilities{}, err
	}
	return capabilities, nil
}
