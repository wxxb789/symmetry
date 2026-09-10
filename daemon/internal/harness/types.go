// Package harness contains the small consumer-facing seam between the daemon
// and a coding-agent harness. Native protocol DTOs belong in a concrete
// adapter package; this package only carries normalized lifecycle values.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// Kind identifies a configured harness family.
type Kind string

const (
	KindGeneric  Kind = "generic"
	KindCodex    Kind = "codex"
	KindClaude   Kind = "claude"
	KindPi       Kind = "pi"
	KindOpenCode Kind = "opencode"
)

// GuidanceCapability describes how an adapter can deliver guidance.
type GuidanceCapability string

const (
	GuidanceNativeSteer GuidanceCapability = "native_steer"
	GuidanceNextTurn    GuidanceCapability = "next_turn"
	GuidanceUnsupported GuidanceCapability = "unsupported"
)

// PauseCapability describes whether an adapter can retain and safely resume a
// native turn. Process interruption is cancellation, not safe pause.
type PauseCapability string

const (
	PauseSafeBoundary PauseCapability = "safe_boundary"
	PauseUnsupported  PauseCapability = "unsupported"
)

// UsageCapability describes the quality of usage information exposed by an
// adapter.
type UsageCapability string

const (
	UsageReported  UsageCapability = "reported"
	UsageEstimated UsageCapability = "estimated"
	UsageUnknown   UsageCapability = "unknown"
)

// Capability names are used by admission checks and registry callers.
type Capability string

const (
	CapabilityStart            Capability = "start"
	CapabilityEvents           Capability = "events"
	CapabilityCancel           Capability = "cancel"
	CapabilityResume           Capability = "resume"
	CapabilityGuidance         Capability = "guidance"
	CapabilityPause            Capability = "pause"
	CapabilityApprovalResponse Capability = "approval_response"
	CapabilityUsage            Capability = "usage"
	CapabilityHardCostLimit    Capability = "hard_cost_limit"
	CapabilityProviderAccess   Capability = "provider_access"
)

// Capabilities are the versioned, fail-closed adapter projection. A false
// boolean is intentionally different from an omitted claim: Unsupported holds
// the reason that the operation is unavailable or unverified.
type Capabilities struct {
	Kind                  Kind               `json:"kind"`
	NativeVersion         string             `json:"native_version,omitempty"`
	ImplementationVersion string             `json:"implementation_version,omitempty"`
	ProtocolVersion       int                `json:"protocol_version"`
	VersionKnown          bool               `json:"version_known"`
	TransportVerified     bool               `json:"transport_verified"`
	Verified              bool               `json:"verified"`
	Start                 bool               `json:"start"`
	Events                bool               `json:"events"`
	Cancel                bool               `json:"cancel"`
	Resume                bool               `json:"resume"`
	Guidance              GuidanceCapability `json:"guidance"`
	Pause                 PauseCapability    `json:"pause"`
	ApprovalResponse      bool               `json:"approval_response"`
	Usage                 UsageCapability    `json:"usage"`
	HardCostLimit         bool               `json:"hard_cost_limit"`
	Unsupported           map[string]string  `json:"unsupported,omitempty"`
}

// UnsupportedCapabilities creates the conservative projection used before a
// native protocol has been behaviorally verified.
func UnsupportedCapabilities(kind Kind, reason string) Capabilities {
	if strings.TrimSpace(reason) == "" {
		reason = "capability has not been verified"
	}
	return Capabilities{
		Kind:            kind,
		ProtocolVersion: 1,
		Guidance:        GuidanceUnsupported,
		Pause:           PauseUnsupported,
		Usage:           UsageUnknown,
		Unsupported: map[string]string{
			string(CapabilityStart):            reason,
			string(CapabilityEvents):           reason,
			string(CapabilityCancel):           reason,
			string(CapabilityResume):           reason,
			string(CapabilityGuidance):         reason,
			string(CapabilityPause):            reason,
			string(CapabilityApprovalResponse): reason,
			string(CapabilityUsage):            reason,
			string(CapabilityHardCostLimit):    reason,
		},
	}
}

// Validate checks the invariants that make capability claims safe to consume.
// In particular, an unverified adapter cannot advertise any operation.
func (capabilities Capabilities) Validate() error {
	if strings.TrimSpace(string(capabilities.Kind)) == "" {
		return errors.New("harness capability kind must not be empty")
	}
	if capabilities.ProtocolVersion <= 0 {
		return errors.New("harness capability protocol version must be positive")
	}
	if capabilities.VersionKnown != (strings.TrimSpace(capabilities.NativeVersion) != "") {
		return errors.New("harness capability version_known must match native_version presence")
	}
	if capabilities.TransportVerified && !capabilities.VersionKnown {
		return errors.New("verified harness transport requires a known native version")
	}
	if capabilities.Verified && (!capabilities.VersionKnown || !capabilities.TransportVerified || strings.TrimSpace(capabilities.ImplementationVersion) == "") {
		return errors.New("verified harness adapter requires a known native version, verified transport, and implementation version")
	}
	if capabilities.Guidance != GuidanceNativeSteer &&
		capabilities.Guidance != GuidanceNextTurn &&
		capabilities.Guidance != GuidanceUnsupported {
		return fmt.Errorf("unknown guidance capability %q", capabilities.Guidance)
	}
	if capabilities.Pause != PauseSafeBoundary && capabilities.Pause != PauseUnsupported {
		return fmt.Errorf("unknown pause capability %q", capabilities.Pause)
	}
	if capabilities.Usage != UsageReported &&
		capabilities.Usage != UsageEstimated &&
		capabilities.Usage != UsageUnknown {
		return fmt.Errorf("unknown usage capability %q", capabilities.Usage)
	}
	if capabilities.Resume && !capabilities.Start {
		return errors.New("resume capability requires start capability")
	}
	if capabilities.Events && !capabilities.Start {
		return errors.New("events capability requires start capability")
	}
	if capabilities.Cancel && (!capabilities.Start || !capabilities.Events) {
		return errors.New("cancel capability requires start and events capabilities")
	}
	if capabilities.ApprovalResponse && (!capabilities.Start || !capabilities.Events) {
		return errors.New("approval response capability requires start and events capabilities")
	}
	if capabilities.Guidance == GuidanceNativeSteer && !capabilities.Start {
		return errors.New("native guidance capability requires start capability")
	}
	if capabilities.Pause == PauseSafeBoundary && !capabilities.Resume {
		return errors.New("safe pause capability requires resume capability")
	}
	if capabilities.Usage == UsageReported && !capabilities.Events {
		return errors.New("reported usage capability requires events capability")
	}
	if !capabilities.Verified {
		if capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume ||
			capabilities.ApprovalResponse || capabilities.HardCostLimit ||
			capabilities.Guidance != GuidanceUnsupported ||
			capabilities.Pause != PauseUnsupported || capabilities.Usage != UsageUnknown {
			return errors.New("unverified adapter cannot advertise executable capabilities")
		}
	}
	return nil
}

// Supports reports whether an operation is explicitly verified and available.
func (capabilities Capabilities) Supports(capability Capability) bool {
	if capabilities.Validate() != nil || !capabilities.Verified {
		return false
	}
	switch capability {
	case CapabilityStart:
		return capabilities.Start
	case CapabilityEvents:
		return capabilities.Events
	case CapabilityCancel:
		return capabilities.Cancel
	case CapabilityResume:
		return capabilities.Resume
	case CapabilityGuidance:
		return capabilities.Guidance != GuidanceUnsupported
	case CapabilityPause:
		return capabilities.Pause != PauseUnsupported
	case CapabilityApprovalResponse:
		return capabilities.ApprovalResponse
	case CapabilityUsage:
		return capabilities.Usage != UsageUnknown
	case CapabilityHardCostLimit:
		return capabilities.HardCostLimit
	default:
		return false
	}
}

// Require returns a typed failure for the first missing operation.
func (capabilities Capabilities) Require(required ...Capability) error {
	for _, capability := range required {
		if capabilities.Supports(capability) {
			continue
		}
		reason := "capability is not verified"
		if capabilities.Unsupported != nil {
			if configured, ok := capabilities.Unsupported[string(capability)]; ok && strings.TrimSpace(configured) != "" {
				reason = configured
			}
		}
		return &CapabilityError{Kind: capabilities.Kind, Capability: capability, Reason: reason}
	}
	return nil
}

// Adapter is the stable consumer-side lifecycle seam. Native wire DTOs must
// not leak through this interface.
type Adapter interface {
	Probe(context.Context) (Capabilities, error)
	Start(context.Context, StartRequest, EventSink) (Session, error)
}

// Session owns at most one native turn at a time.
type Session interface {
	Control(context.Context, ControlRequest) (ControlReceipt, error)
	Wait(context.Context) (TaskResult, error)
	Close(context.Context) error
}

// StagedSession is implemented by native adapters that must expose durable
// launch barriers. Start creates only the supervised transport process; the
// daemon records its process identity before Open creates a native session,
// and records the opaque native handle before StartTurn admits work.
//
// Generic adapters intentionally do not implement this interface because
// their legacy process lifecycle has no retained native session boundary.
type StagedSession interface {
	Session
	ProcessDetails() (int, string)
	Open(context.Context) (NativeSessionHandle, error)
	StartTurn(context.Context, TurnRequest) error
	// WaitTurn waits only for the matching native turn terminal observation.
	// It deliberately does not return a TaskResult: Wait remains the physical
	// final-result barrier after the retained native process is closed.
	WaitTurn(context.Context) error
}

// NativeSessionHandle is a daemon-local opaque identity. It is persisted only
// in the local Goal session journal and must never be placed on the control
// wire.
type NativeSessionHandle struct {
	ID       string
	Filename string
}

// TurnRequest is the canonical context and task material for one already
// opened native session. The adapter must treat Context as data rather than
// authority; the admission identity remains the authoritative boundary.
type TurnRequest struct {
	Goal    string
	Context json.RawMessage
}

// StartRequest contains daemon admission data and a local process invocation.
// ProviderAccess is an ephemeral broker capability for a single claimed run;
// it must never be persisted or included in native prompts. Server credentials
// and lease tokens are intentionally absent.
type StartRequest struct {
	AdmissionID    string
	LocalHandleID  string
	Workspace      string
	Context        json.RawMessage
	Goal           string
	ModelProfile   string
	Limits         Limits
	Resume         *ResumeHandle
	ProviderAccess *protocol.ProviderAccess
	Invocation     execution.Invocation
	// PersistProcess is called after the native process has started but before
	// the adapter exposes the session or drains pre-ready output. A failure
	// leaves the native launch outcome conservative; the adapter must attempt
	// bounded process cleanup before returning the error.
	PersistProcess func(pid int, identity string) error
}

// Limits bounds one admitted native turn.
type Limits struct {
	MaxTurns        int
	DeadlineAt      time.Time
	MaxCostMicrousd *string
}

// ResumeHandle is a daemon-local, validated resume reference. Native handles
// remain opaque and are never accepted without a compatible adapter probe.
type ResumeHandle struct {
	LocalHandleID        string
	NativeSessionID      string
	WorkspaceFingerprint string
	NativeVersion        string
}

// EventKind is the normalized event vocabulary. Native adapters may add
// diagnostics, but unknown native events never become task success.
type EventKind string

const (
	EventOutput            EventKind = "output"
	EventNativeFrame       EventKind = "native_frame"
	EventSessionStarted    EventKind = "session_started"
	EventMessageDelta      EventKind = "message_delta"
	EventToolStarted       EventKind = "tool_started"
	EventToolFinished      EventKind = "tool_finished"
	EventApprovalRequested EventKind = "approval_requested"
	EventUsageObserved     EventKind = "usage_observed"
	EventTaskResult        EventKind = "task_result"
	EventDiagnostic        EventKind = "diagnostic"
)

// Event is bounded normalized data delivered to the daemon journal sink.
type Event struct {
	Kind       EventKind
	Stream     string
	Sequence   uint64
	At         time.Time
	Data       []byte
	Payload    json.RawMessage
	Diagnostic bool
	Code       string
	Message    string
}

// EventSink accepts events and returns persistence errors. A returned error
// stops acknowledgement by the process runner.
type EventSink interface {
	Handle(context.Context, Event) error
}

// EventSinkFunc adapts a function to EventSink.
type EventSinkFunc func(context.Context, Event) error

// Handle calls the wrapped function.
func (function EventSinkFunc) Handle(ctx context.Context, event Event) error {
	return function(ctx, event)
}

// ControlKind names a command applied to a live session.
type ControlKind string

const (
	ControlCancel           ControlKind = "cancel"
	ControlPause            ControlKind = "pause"
	ControlResume           ControlKind = "resume"
	ControlGuidance         ControlKind = "guidance"
	ControlApprovalResponse ControlKind = "approval_response"
)

// ControlRequest is deliberately transport-neutral.
type ControlRequest struct {
	CommandID string
	Kind      ControlKind
	Message   string
	Payload   json.RawMessage
}

// ControlOutcome records the local delivery decision.
type ControlOutcome string

const (
	ControlApplied     ControlOutcome = "applied"
	ControlUnsupported ControlOutcome = "unsupported"
	ControlRejected    ControlOutcome = "rejected"
	ControlFailed      ControlOutcome = "failed"
)

// ControlReceipt is returned only after the adapter has applied or rejected a
// control. Writing bytes to a process is not an applied receipt.
type ControlReceipt struct {
	CommandID  string
	Kind       ControlKind
	Outcome    ControlOutcome
	Capability Capability
	Message    string
	AppliedAt  time.Time
}

// ResultKind is the semantic outcome of a bounded turn.
type ResultKind string

const (
	ResultSucceeded ResultKind = "succeeded"
	ResultFailed    ResultKind = "failed"
	ResultCancelled ResultKind = "cancelled"
	ResultUnknown   ResultKind = "unknown"
)

// Usage is intentionally nullable/unknown when the native harness does not
// report authoritative counters.
type Usage struct {
	State        UsageCapability
	InputTokens  int64
	OutputTokens int64
	CostMicrousd string
}

// TaskResult separates process completion from semantic result evidence.
type TaskResult struct {
	Kind         ResultKind
	Summary      string
	Semantic     *protocol.TaskResult
	Reason       *protocol.TaskResultReason
	Process      execution.Result
	Usage        Usage
	EventCount   uint64
	LastSequence uint64
}

var (
	ErrUnknownAdapter        = errors.New("unknown harness adapter")
	ErrUnsupportedCapability = errors.New("unsupported harness capability")
	ErrUnsupportedVersion    = errors.New("unsupported harness version")
	ErrHarnessUnavailable    = errors.New("harness executable unavailable")
	ErrNativeUnverified      = errors.New("native harness behavior is unverified")
)

// CapabilityError makes unsupported operation and version failures inspectable
// without requiring callers to parse strings.
type CapabilityError struct {
	Kind       Kind
	Capability Capability
	Reason     string
}

func (err *CapabilityError) Error() string {
	if err == nil {
		return ""
	}
	if err.Kind == "" {
		return fmt.Sprintf("%s %s: %s", ErrUnsupportedCapability, err.Capability, err.Reason)
	}
	return fmt.Sprintf("%s %s %s: %s", err.Kind, ErrUnsupportedCapability, err.Capability, err.Reason)
}

func (err *CapabilityError) Unwrap() error { return ErrUnsupportedCapability }
