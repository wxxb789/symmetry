// Package control provides a strict, single-request HTTP client for protocol v1.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const defaultMaxResponseBytes int64 = 1 << 20

var errRedirectNotAllowed = errors.New("control client does not allow redirects")

// ErrorCode is a control-plane error that callers can branch on safely.
type ErrorCode string

const (
	InvalidRequest       ErrorCode = "invalid_request"
	Unauthenticated      ErrorCode = "unauthenticated"
	Forbidden            ErrorCode = "forbidden"
	NotFound             ErrorCode = "not_found"
	CapacityExhausted    ErrorCode = "capacity_exhausted"
	IdempotencyConflict  ErrorCode = "idempotency_conflict"
	OwnershipLost        ErrorCode = "ownership_lost"
	TerminalGraceExpired ErrorCode = "terminal_grace_expired"
	StateConflict        ErrorCode = "state_conflict"
	AssignmentExpired    ErrorCode = "assignment_expired"
	InvalidTransition    ErrorCode = "invalid_transition"
	RateLimited          ErrorCode = "rate_limited"
	ServiceUnavailable   ErrorCode = "service_unavailable"
	UnexpectedHTTPStatus ErrorCode = "unexpected_http_status"
)

// APIError describes a non-success response returned by the control plane.
type APIError struct {
	StatusCode    int
	Code          ErrorCode
	Message       string
	RetryAfter    time.Duration
	retryAfterSet bool
}

// ResponseError marks a malformed or unexpected successful control-plane response.
// It preserves the underlying cause for diagnostics while remaining permanent for
// retry classification.
type ResponseError struct {
	cause error
}

func (err *ResponseError) Error() string { return err.cause.Error() }

func (err *ResponseError) Unwrap() error { return err.cause }

func responseErrorf(format string, arguments ...any) error {
	return &ResponseError{cause: fmt.Errorf(format, arguments...)}
}

func (err *APIError) Error() string {
	if err.Message == "" {
		return fmt.Sprintf("control API error: %s (HTTP %d)", err.Code, err.StatusCode)
	}
	return fmt.Sprintf("control API error: %s (HTTP %d): %s", err.Code, err.StatusCode, err.Message)
}

// IsOwnershipLost reports whether a request was rejected because its fence is stale.
func IsOwnershipLost(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.Code == OwnershipLost
}

// IsTerminalGraceExpired reports that terminal-only delivery exceeded its
// generation-scoped grace window.
func IsTerminalGraceExpired(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.Code == TerminalGraceExpired
}

// Option changes construction-time HTTP transport behavior.
type Option func(*transport)

// WithMaxResponseBytes bounds every successful and error response body.
func WithMaxResponseBytes(limit int64) Option {
	return func(transport *transport) {
		if limit > 0 {
			transport.maxResponseBytes = limit
		}
	}
}

type transport struct {
	baseURL          *url.URL
	httpClient       *http.Client
	maxResponseBytes int64
}

// Client invokes machine-authenticated protocol v1 daemon endpoints. It never retries requests.
type Client struct {
	*transport
	machineToken string
}

// EnrollmentClient invokes enrollment with an explicitly supplied one-time token.
type EnrollmentClient struct {
	*transport
}

// OperatorClient invokes task-control endpoints with an explicitly supplied operator token.
type OperatorClient struct {
	*transport
	operatorToken string
}

// GoalSessionAttachRequest is the flat request body accepted by the fenced
// machine-only session endpoint. The run ID remains authoritative in the URL;
// it is intentionally not duplicated in this body.
type GoalSessionAttachRequest struct {
	protocol.Fence
	LocalHandleID        string  `json:"local_handle_id"`
	BindingID            *string `json:"binding_id,omitempty"`
	HarnessKind          string  `json:"harness_kind"`
	HarnessVersion       string  `json:"harness_version"`
	AdapterVersion       string  `json:"adapter_version"`
	WorkspaceFingerprint string  `json:"workspace_fingerprint"`
	Workspace            string  `json:"workspace"`
	RepositoryResourceID *string `json:"repository_resource_id,omitempty"`
}

// GoalSessionReceipt is the durable session identity returned by attach. The
// endpoint may omit optional projection fields, but never private native
// session payloads or credentials.
type GoalSessionReceipt struct {
	ID                   string `json:"id"`
	SessionID            string `json:"session_id,omitempty"`
	AttachmentReceiptID  string `json:"attachment_receipt_id,omitempty"`
	GoalID               string `json:"goal_id,omitempty"`
	TaskID               string `json:"task_id,omitempty"`
	RunID                string `json:"run_id"`
	MachineID            string `json:"machine_id,omitempty"`
	RuntimeID            string `json:"runtime_id,omitempty"`
	RepositoryResourceID string `json:"repository_resource_id,omitempty"`
	ActiveRunID          string `json:"active_run_id,omitempty"`
	HarnessKind          string `json:"harness_kind,omitempty"`
	HarnessVersion       string `json:"harness_version,omitempty"`
	AdapterVersion       string `json:"adapter_version,omitempty"`
	LocalHandleID        string `json:"local_handle_id,omitempty"`
	BindingID            string `json:"binding_id,omitempty"`
	WorkspaceFingerprint string `json:"workspace_fingerprint,omitempty"`
	Workspace            string `json:"workspace,omitempty"`
	State                string `json:"state,omitempty"`
	LockVersion          int64  `json:"lock_version,omitempty"`
	InsertedAt           string `json:"inserted_at,omitempty"`
	UpdatedAt            string `json:"updated_at,omitempty"`
}

// GoalSessionStoppedRequest is the exact durable proof that one native
// attachment stopped. The run ID is authoritative in the endpoint path.
type GoalSessionStoppedRequest struct {
	protocol.Fence
	SessionID     string `json:"session_id"`
	LocalHandleID string `json:"local_handle_id"`
	BindingID     string `json:"binding_id"`
}

// GoalSessionStoppedReceipt is the immutable control-plane receipt for a
// released retained session. It never includes native private identity.
type GoalSessionStoppedReceipt struct {
	ReceiptID     string  `json:"receipt_id"`
	RunID         string  `json:"run_id"`
	SessionID     string  `json:"session_id"`
	LocalHandleID string  `json:"local_handle_id"`
	BindingID     string  `json:"binding_id"`
	State         string  `json:"state"`
	ActiveRunID   *string `json:"active_run_id"`
	LockVersion   int64   `json:"lock_version"`
}

// GoalEvidenceReceipt is the compact receipt returned after an evidence batch
// is durably inserted or replayed.
type GoalEvidenceReceipt struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	EvidenceKey string `json:"evidence_key"`
	Kind        string `json:"kind"`
	SubjectHash string `json:"subject_hash"`
	Verdict     string `json:"verdict"`
}

// GoalUsageReceipt is the compact receipt returned after usage accounting is
// durably inserted or replayed. cost_microusd follows the wire decimal-string
// convention and may be null when cost_basis is unknown.
type GoalUsageReceipt struct {
	ID           string  `json:"id"`
	RunID        string  `json:"run_id"`
	UsageKey     string  `json:"usage_key"`
	CostMicrousd *string `json:"cost_microusd"`
	CostBasis    string  `json:"cost_basis"`
}

// UnmarshalJSON accepts only the documented decimal-string form or null.
// Unknown fields remain rejected.
func (receipt *GoalUsageReceipt) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID           string          `json:"id"`
		RunID        string          `json:"run_id"`
		UsageKey     string          `json:"usage_key"`
		CostMicrousd json.RawMessage `json:"cost_microusd"`
		CostBasis    string          `json:"cost_basis"`
	}
	if err := decodeStrictObjectJSON(data, &wire, "id", "run_id", "usage_key", "cost_microusd", "cost_basis"); err != nil {
		return err
	}
	var cost *string
	trimmed := bytes.TrimSpace(wire.CostMicrousd)
	if !bytes.Equal(trimmed, []byte("null")) {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return errors.New("cost_microusd must be a decimal string or null")
		}
		if err := protocol.ValidateMicroUSD(text); err != nil {
			return err
		}
		cost = &text
	}
	*receipt = GoalUsageReceipt{ID: wire.ID, RunID: wire.RunID, UsageKey: wire.UsageKey, CostMicrousd: cost, CostBasis: wire.CostBasis}
	return nil
}

// ApprovedGoal is the authority-bearing portion of a canonical context
// snapshot. It contains policy data, but no executable command or credential.
type ApprovedGoal struct {
	GoalID          string          `json:"goal_id"`
	Revision        int64           `json:"revision"`
	Objective       string          `json:"objective"`
	AuthorityPolicy AuthorityPolicy `json:"authority_policy"`
}

// AuthorityPolicy describes the operator boundaries carried by an approved
// goal. It is descriptive context; it never grants authority to the daemon.
type AuthorityPolicy struct {
	OperatorRequiredForScopeChange bool     `json:"operator_required_for_scope_change"`
	OperatorRequiredForCompletion  bool     `json:"operator_required_for_completion"`
	PublicationAllowed             bool     `json:"publication_allowed"`
	AllowedActions                 []string `json:"allowed_actions"`
}

// AcceptancePredicate is one typed acceptance condition in a work contract.
// The populated identity fields depend on Kind.
type AcceptancePredicate struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	ValidatorProfile string `json:"validator_profile,omitempty"`
	ResourceID       string `json:"resource_id,omitempty"`
	Path             string `json:"path,omitempty"`
	ReviewerProfile  string `json:"reviewer_profile,omitempty"`
	present          map[string]struct{}
}

// AcceptanceContract is the versioned predicate contract embedded in a work
// contract.
type AcceptanceContract struct {
	SchemaVersion string                `json:"schema_version"`
	Description   string                `json:"description"`
	Predicates    []AcceptancePredicate `json:"predicates"`
}

// WorkContract identifies the bounded work and its acceptance rules.
type WorkContract struct {
	Title              string                         `json:"title"`
	Description        string                         `json:"description"`
	Purpose            string                         `json:"purpose"`
	ChangeTarget       *protocol.ProviderChangeTarget `json:"change_target"`
	Acceptance         AcceptanceContract             `json:"acceptance"`
	ValidationBindings []ValidationBinding            `json:"validation_bindings"`
}

// ValidationBinding freezes one server-resolved validation profile and the
// runtimes authorized to produce its evidence for this context snapshot.
type ValidationBinding struct {
	ProfileName       string   `json:"profile_name"`
	Kind              string   `json:"kind"`
	ProfileDigest     string   `json:"profile_digest"`
	AllowedRuntimeIDs []string `json:"allowed_runtime_ids"`
}

// ContextContent is the safe, typed content carried by a context source.
type ContextContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// ContextSource identifies one bounded source included in a snapshot.
type ContextSource struct {
	ResourceID     string         `json:"resource_id"`
	SourceKind     string         `json:"source_kind"`
	SourceRevision string         `json:"source_revision"`
	ContentHash    string         `json:"content_hash"`
	ObservedAt     string         `json:"observed_at"`
	Trust          string         `json:"trust"`
	Required       bool           `json:"required"`
	Content        ContextContent `json:"content"`
	Stale          *bool          `json:"stale,omitempty"`
	present        map[string]struct{}
}

// DecisionReference is a compact reference to a durable decision.
type DecisionReference struct {
	DecisionID string `json:"decision_id"`
	ActionHash string `json:"action_hash"`
	State      string `json:"state"`
}

// EvidenceReference is a compact reference to validated evidence.
type EvidenceReference struct {
	EvidenceID  string `json:"evidence_id"`
	PredicateID string `json:"predicate_id"`
	SubjectHash string `json:"subject_hash"`
	Verdict     string `json:"verdict"`
}

// FailedAttempt records a prior execution attempt without exposing native
// transcripts or local session identifiers.
type FailedAttempt struct {
	TaskID     string  `json:"task_id"`
	RunID      *string `json:"run_id"`
	Reason     string  `json:"reason"`
	ObservedAt string  `json:"observed_at"`
}

// Blocker is the typed blocker used by a wait next action.
type Blocker struct {
	Kind        string   `json:"kind"`
	DecisionID  string   `json:"decision_id,omitempty"`
	ResourceID  string   `json:"resource_id,omitempty"`
	ExternalRef string   `json:"external_ref,omitempty"`
	NextCheckAt string   `json:"next_check_at,omitempty"`
	WorkItemIDs []string `json:"work_item_ids,omitempty"`
	Code        string   `json:"code,omitempty"`
	Detail      string   `json:"detail,omitempty"`
	present     map[string]struct{}
}

// NextAction is a typed, non-authoritative follow-up proposal.
type NextAction struct {
	Kind            string   `json:"kind"`
	ProducingTaskID string   `json:"producing_task_id,omitempty"`
	WorkItemID      string   `json:"work_item_id,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	ResourceID      string   `json:"resource_id,omitempty"`
	ExternalRef     string   `json:"external_ref,omitempty"`
	Blocker         *Blocker `json:"blocker,omitempty"`
	present         map[string]struct{}
}

// ContextSize reports the bounded size accounting for a canonical snapshot.
type ContextSize struct {
	MandatoryBytes int64  `json:"mandatory_bytes"`
	OptionalBytes  int64  `json:"optional_bytes"`
	TotalBytes     int64  `json:"total_bytes"`
	ByteBudget     int64  `json:"byte_budget"`
	TokenEstimate  *int64 `json:"token_estimate"`
}

// GoalContextSnapshot is the canonical symmetry.context_snapshot.v1 payload
// returned to the owning machine. It is deliberately modeled as typed data so
// additive or legacy DB projections cannot be mistaken for canonical context.
type GoalContextSnapshot struct {
	SchemaVersion string `json:"schema_version"`
	SnapshotID    string `json:"snapshot_id"`
	GoalID        string `json:"goal_id"`
	GoalRevision  int64  `json:"goal_revision"`
	// WorkItemID is nil only for a planning context. Keep the nullable wire
	// identity explicit instead of treating an empty string as absence.
	WorkItemID        *string             `json:"work_item_id"`
	ContentHash       string              `json:"content_hash"`
	CreatedAt         string              `json:"created_at"`
	ApprovedGoal      ApprovedGoal        `json:"approved_goal"`
	WorkContract      WorkContract        `json:"work_contract"`
	Subject           protocol.Subject    `json:"subject"`
	Sources           []ContextSource     `json:"sources"`
	CurrentDecisions  []DecisionReference `json:"current_decisions"`
	ValidatedEvidence []EvidenceReference `json:"validated_evidence"`
	FailedAttempts    []FailedAttempt     `json:"failed_attempts"`
	AdvisoryRecall    []ContextSource     `json:"advisory_recall"`
	NextAction        *NextAction         `json:"next_action"`
	Size              ContextSize         `json:"size"`
}

// GoalRunContext binds the sanitized context to the current run/session.
type GoalRunContext struct {
	GoalID     string              `json:"goal_id"`
	TaskID     string              `json:"task_id"`
	RunID      string              `json:"run_id"`
	Generation int64               `json:"generation"`
	SessionID  *string             `json:"session_id"`
	Context    GoalContextSnapshot `json:"context"`
}

// NewClient creates a machine-authenticated client. baseURL is the API prefix,
// for example https://control.example.test/api.
func NewClient(baseURL, machineToken string, httpClient *http.Client, options ...Option) (*Client, error) {
	if strings.TrimSpace(machineToken) == "" {
		return nil, errors.New("machine token must not be empty")
	}
	transport, err := newTransport(baseURL, httpClient, options...)
	if err != nil {
		return nil, err
	}
	return &Client{transport: transport, machineToken: machineToken}, nil
}

// NewEnrollmentClient creates a client that can only authenticate enrollment
// requests with an explicitly supplied one-time enrollment token.
func NewEnrollmentClient(baseURL string, httpClient *http.Client, options ...Option) (*EnrollmentClient, error) {
	transport, err := newTransport(baseURL, httpClient, options...)
	if err != nil {
		return nil, err
	}
	return &EnrollmentClient{transport: transport}, nil
}

// NewOperatorClient creates a client for task-control requests under a distinct operator credential.
func NewOperatorClient(baseURL, operatorToken string, httpClient *http.Client, options ...Option) (*OperatorClient, error) {
	if strings.TrimSpace(operatorToken) == "" {
		return nil, errors.New("operator token must not be empty")
	}
	transport, err := newTransport(baseURL, httpClient, options...)
	if err != nil {
		return nil, err
	}
	return &OperatorClient{transport: transport, operatorToken: operatorToken}, nil
}

func newTransport(baseURL string, httpClient *http.Client, options ...Option) (*transport, error) {
	parsed, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	clonedHTTPClient := *httpClient
	clonedHTTPClient.CheckRedirect = func(request *http.Request, _ []*http.Request) error {
		return fmt.Errorf("%w: %s", errRedirectNotAllowed, request.URL.Redacted())
	}
	transport := &transport{
		baseURL:          parsed,
		httpClient:       &clonedHTTPClient,
		maxResponseBytes: defaultMaxResponseBytes,
	}
	for _, option := range options {
		if option != nil {
			option(transport)
		}
	}
	return transport, nil
}

func parseBaseURL(value string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(value, "#") {
		return nil, errors.New("base URL must be an absolute http or https URL without credentials, query, or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "" {
		parsed.Path = "/api"
	}
	return parsed, nil
}

// Enroll registers a machine using a one-time enrollment bearer token.
func (client *EnrollmentClient) Enroll(ctx context.Context, enrollmentToken, idempotencyKey string, request protocol.EnrollRequest) (protocol.EnrollResponse, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return protocol.EnrollResponse{}, errors.New("idempotency key must not be empty")
	}
	if strings.TrimSpace(request.MachineToken) == "" {
		return protocol.EnrollResponse{}, errors.New("machine token must not be empty")
	}
	var response protocol.EnrollResponse
	statusCode, err := client.requestWithStatus(ctx, http.MethodPost, "v1/machines", nil, enrollmentToken, idempotencyKey, request, &response)
	if err != nil {
		return protocol.EnrollResponse{}, err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return protocol.EnrollResponse{}, responseErrorf("invalid enroll response: expected HTTP 200 or 201, got HTTP %d", statusCode)
	}
	if err := validateEnrollResponse(request, response); err != nil {
		return protocol.EnrollResponse{}, err
	}
	return response, nil
}

// RegisterSession registers a daemon instance and its configured runtimes.
func (client *Client) RegisterSession(ctx context.Context, machineID, daemonInstanceID string, request protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	if err := validatePathID("machine ID", machineID); err != nil {
		return protocol.SessionRegistrationResponse{}, err
	}
	if err := validatePathID("daemon instance ID", daemonInstanceID); err != nil {
		return protocol.SessionRegistrationResponse{}, err
	}
	if err := request.Validate(); err != nil {
		return protocol.SessionRegistrationResponse{}, fmt.Errorf("validate session registration: %w", err)
	}
	var response protocol.SessionRegistrationResponse
	endpoint := "v1/machines/" + machineID + "/sessions/" + daemonInstanceID
	if err := client.machineRequest(ctx, http.MethodPut, endpoint, nil, "", request, &response); err != nil {
		return protocol.SessionRegistrationResponse{}, err
	}
	if err := validateSessionResponse(request, response); err != nil {
		return protocol.SessionRegistrationResponse{}, err
	}
	return response, nil
}

// Heartbeat reports all active runs and returns the latest runtime snapshot.
func (client *Client) Heartbeat(ctx context.Context, runtimeID string, request protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error) {
	if err := validatePathID("runtime ID", runtimeID); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	var response protocol.RuntimeSnapshot
	if err := client.machineRequest(ctx, http.MethodPatch, "v1/runtimes/"+runtimeID, nil, "", request, &response); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	if err := validateRuntimeSnapshot(response); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	return response, nil
}

// Dispatch fetches the non-destructive current snapshot for a runtime.
func (client *Client) Dispatch(ctx context.Context, runtimeID string, runtimeEpoch int64) (protocol.RuntimeSnapshot, error) {
	if err := validatePathID("runtime ID", runtimeID); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	var response protocol.RuntimeSnapshot
	query := url.Values{"runtime_epoch": []string{strconv.FormatInt(runtimeEpoch, 10)}}
	if err := client.machineRequest(ctx, http.MethodGet, "v1/runtimes/"+runtimeID+"/dispatch", query, "", nil, &response); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	if err := validateRuntimeSnapshot(response); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	return response, nil
}

// Claim claims an assignment. The caller owns claim-ID persistence and retries.
func (client *Client) Claim(ctx context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	if err := validatePathID("run ID", runID); err != nil {
		return protocol.ClaimResponse{}, err
	}
	if err := validatePathID("claim ID", request.ClaimID); err != nil {
		return protocol.ClaimResponse{}, err
	}
	var response protocol.ClaimResponse
	body := struct {
		RuntimeID    string `json:"runtime_id"`
		RuntimeEpoch int64  `json:"runtime_epoch"`
		Generation   int64  `json:"generation"`
	}{RuntimeID: request.RuntimeID, RuntimeEpoch: request.RuntimeEpoch, Generation: request.Generation}
	endpoint := "v1/runs/" + runID + "/claims/" + request.ClaimID
	if err := client.machineRequest(ctx, http.MethodPut, endpoint, nil, "", body, &response); err != nil {
		return protocol.ClaimResponse{}, err
	}
	if err := validateClaimResponse(runID, request, response); err != nil {
		return protocol.ClaimResponse{}, err
	}
	return response, nil
}

// RenewLease extends an unexpired lease with the caller-supplied fence.
func (client *Client) RenewLease(ctx context.Context, runID string, request protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	if err := validatePathID("run ID", runID); err != nil {
		return protocol.LeaseHeartbeatResponse{}, err
	}
	var response protocol.LeaseHeartbeatResponse
	if err := client.machineRequest(ctx, http.MethodPatch, "v1/runs/"+runID+"/lease", nil, "", request, &response); err != nil {
		return protocol.LeaseHeartbeatResponse{}, err
	}
	if err := validateLeaseHeartbeatResponse(response); err != nil {
		return protocol.LeaseHeartbeatResponse{}, err
	}
	return response, nil
}

// AppendEvents appends caller-identified events without a retry policy.
func (client *Client) AppendEvents(ctx context.Context, runID string, request protocol.AppendEventsRequest) error {
	if err := validatePathID("run ID", runID); err != nil {
		return err
	}
	return client.requestNoContent(ctx, http.MethodPost, "v1/runs/"+runID+"/events", nil, client.machineToken, "", request)
}

// AttachHarnessSession attaches a daemon-local native session to a fenced run.
// The response is decoded with the Goal strict decoder; legacy endpoint
// decoders remain tolerant of additive fields.
func (client *Client) AttachHarnessSession(ctx context.Context, runID string, request GoalSessionAttachRequest) (GoalSessionReceipt, error) {
	if err := validateGoalSessionAttach(runID, request); err != nil {
		return GoalSessionReceipt{}, err
	}
	body := request
	var wire struct {
		Session GoalSessionReceipt `json:"session"`
	}
	statusCode, err := client.requestGoalWithStatus(ctx, http.MethodPut, "v1/runs/"+runID+"/session", nil, "", body, &wire)
	if err != nil {
		return GoalSessionReceipt{}, err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return GoalSessionReceipt{}, responseErrorf("invalid attach session response: expected HTTP 200 or 201, got HTTP %d", statusCode)
	}
	if err := validateGoalSessionReceipt(runID, request, wire.Session); err != nil {
		return GoalSessionReceipt{}, err
	}
	return wire.Session, nil
}

// FetchHarnessSessionAttachment returns the immutable attach receipt for the
// current fenced run. It is used to recover an attach request whose response
// was lost after Control committed it.
func (client *Client) FetchHarnessSessionAttachment(ctx context.Context, runID string, fence protocol.Fence) (GoalSessionReceipt, error) {
	if err := validateGoalFence(runID, fence); err != nil {
		return GoalSessionReceipt{}, err
	}
	var wire struct {
		Session GoalSessionReceipt `json:"session"`
	}
	statusCode, err := client.requestGoalWithStatus(ctx, http.MethodGet, "v1/runs/"+runID+"/session", goalFenceQuery(fence), "", nil, &wire)
	if err != nil {
		return GoalSessionReceipt{}, err
	}
	if statusCode != http.StatusOK {
		return GoalSessionReceipt{}, responseErrorf("invalid fetch session attachment response: expected HTTP 200, got HTTP %d", statusCode)
	}
	if err := validateGoalSessionAttachmentReadback(runID, fence, wire.Session); err != nil {
		return GoalSessionReceipt{}, err
	}
	return wire.Session, nil
}

// MarkHarnessSessionStopped releases a retained native attachment only after
// the control plane has accepted the run's terminal transition.
func (client *Client) MarkHarnessSessionStopped(ctx context.Context, runID string, request GoalSessionStoppedRequest) (GoalSessionStoppedReceipt, error) {
	if err := validateGoalSessionStopped(runID, request); err != nil {
		return GoalSessionStoppedReceipt{}, err
	}
	var wire struct {
		SessionStopped GoalSessionStoppedReceipt `json:"session_stopped"`
	}
	statusCode, err := client.requestGoalWithStatus(ctx, http.MethodPut, "v1/runs/"+runID+"/session/stopped", nil, "", request, &wire)
	if err != nil {
		return GoalSessionStoppedReceipt{}, err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return GoalSessionStoppedReceipt{}, responseErrorf("invalid session stopped response: expected HTTP 200 or 201, got HTTP %d", statusCode)
	}
	if err := validateGoalSessionStoppedReceipt(runID, request, wire.SessionStopped); err != nil {
		return GoalSessionStoppedReceipt{}, err
	}
	return wire.SessionStopped, nil
}

// AppendEvidence submits one normalized evidence receipt under the current
// run fence. The request's run_id must match the path run ID.
func (client *Client) AppendEvidence(ctx context.Context, runID string, fence protocol.Fence, evidence protocol.Evidence) (GoalEvidenceReceipt, error) {
	if err := validateGoalEvidence(runID, fence, evidence); err != nil {
		return GoalEvidenceReceipt{}, err
	}
	body := struct {
		protocol.Fence
		protocol.Evidence
	}{Fence: fence, Evidence: evidence}
	var wire struct {
		Evidence GoalEvidenceReceipt `json:"evidence"`
	}
	statusCode, err := client.requestGoalWithStatus(ctx, http.MethodPost, "v1/runs/"+runID+"/evidence", nil, "", body, &wire)
	if err != nil {
		return GoalEvidenceReceipt{}, err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return GoalEvidenceReceipt{}, responseErrorf("invalid evidence response: expected HTTP 200 or 201, got HTTP %d", statusCode)
	}
	if err := validateGoalEvidenceReceipt(runID, evidence, wire.Evidence); err != nil {
		return GoalEvidenceReceipt{}, err
	}
	return wire.Evidence, nil
}

// RecordUsage submits normalized usage under the current run fence. Late usage
// remains accepted by the server only under its documented accounting rule.
func (client *Client) RecordUsage(ctx context.Context, runID string, fence protocol.Fence, usage protocol.Usage) (GoalUsageReceipt, error) {
	if err := validateGoalUsage(runID, fence, usage); err != nil {
		return GoalUsageReceipt{}, err
	}
	body := struct {
		protocol.Fence
		protocol.Usage
	}{Fence: fence, Usage: usage}
	var wire struct {
		Usage GoalUsageReceipt `json:"usage"`
	}
	statusCode, err := client.requestGoalWithStatus(ctx, http.MethodPost, "v1/runs/"+runID+"/usage", nil, "", body, &wire)
	if err != nil {
		return GoalUsageReceipt{}, err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return GoalUsageReceipt{}, responseErrorf("invalid usage response: expected HTTP 200 or 201, got HTTP %d", statusCode)
	}
	if err := validateGoalUsageReceipt(runID, usage, wire.Usage); err != nil {
		return GoalUsageReceipt{}, err
	}
	return wire.Usage, nil
}

// FetchRunContext returns the current sanitized context for a claimed run.
// Fence values are query parameters because the endpoint has no request body.
func (client *Client) FetchRunContext(ctx context.Context, runID string, fence protocol.Fence) (GoalRunContext, error) {
	if err := validateGoalFence(runID, fence); err != nil {
		return GoalRunContext{}, err
	}
	query := goalFenceQuery(fence)
	var response GoalRunContext
	if err := client.requestGoal(ctx, http.MethodGet, "v1/runs/"+runID+"/context", query, "", nil, &response); err != nil {
		return GoalRunContext{}, err
	}
	if err := validateGoalRunContext(runID, fence, response); err != nil {
		return GoalRunContext{}, err
	}
	return response, nil
}

func goalFenceQuery(fence protocol.Fence) url.Values {
	return url.Values{
		"runtime_id":    []string{fence.RuntimeID},
		"runtime_epoch": []string{strconv.FormatInt(fence.RuntimeEpoch, 10)},
		"generation":    []string{strconv.FormatInt(fence.Generation, 10)},
		"claim_id":      []string{fence.ClaimID},
		"lease_token":   []string{fence.LeaseToken},
	}
}

// Transition applies a caller-identified lifecycle transition without a retry policy.
func (client *Client) Transition(ctx context.Context, runID string, request protocol.StateTransitionRequest) error {
	if err := validatePathID("run ID", runID); err != nil {
		return err
	}
	if err := validatePathID("transition ID", request.TransitionID); err != nil {
		return err
	}
	body := struct {
		protocol.Fence
		State   string          `json:"state"`
		Payload json.RawMessage `json:"payload"`
	}{Fence: request.Fence, State: request.State, Payload: request.Payload}
	endpoint := "v1/runs/" + runID + "/transitions/" + request.TransitionID
	var response protocol.Run
	statusCode, err := client.requestWithStatus(ctx, http.MethodPut, endpoint, nil, client.machineToken, "", body, &response)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK {
		return responseErrorf("invalid transition response: expected HTTP 200 OK, got HTTP %d", statusCode)
	}
	if err := validateTransitionResponse(runID, request, response); err != nil {
		return responseErrorf("%v", err)
	}
	return nil
}

// Reconcile compares the caller's local run journal with durable control-plane state.
func (client *Client) Reconcile(ctx context.Context, runtimeID string, request protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	if err := validatePathID("runtime ID", runtimeID); err != nil {
		return protocol.ReconcileResponse{}, err
	}
	var response protocol.ReconcileResponse
	if err := client.machineRequest(ctx, http.MethodPut, "v1/runtimes/"+runtimeID+"/reconciliation", nil, "", request, &response); err != nil {
		return protocol.ReconcileResponse{}, err
	}
	if err := validateReconcileResponse(response); err != nil {
		return protocol.ReconcileResponse{}, err
	}
	return response, nil
}

// AcknowledgeCommand records command delivery with the caller-supplied ack ID.
func (client *Client) AcknowledgeCommand(ctx context.Context, commandID string, request protocol.CommandAcknowledgement) error {
	if err := validatePathID("command ID", commandID); err != nil {
		return err
	}
	if commandID != request.CommandID {
		return errors.New("command ID does not match acknowledgement body")
	}
	if err := validatePathID("acknowledgement ID", request.AckID); err != nil {
		return err
	}
	body := struct {
		protocol.Fence
		RunID   string `json:"run_id"`
		Outcome string `json:"outcome"`
	}{Fence: request.Fence, RunID: request.RunID, Outcome: request.Outcome}
	endpoint := "v1/commands/" + commandID + "/acknowledgements/" + request.AckID
	var response protocol.TaskCommand
	statusCode, err := client.requestWithStatus(ctx, http.MethodPut, endpoint, nil, client.machineToken, "", body, &response)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK {
		return responseErrorf("invalid acknowledge command response: expected HTTP 200 OK, got HTTP %d", statusCode)
	}
	if err := validateAcknowledgementResponse(commandID, request, response); err != nil {
		return responseErrorf("%v", err)
	}
	return nil
}

// SubmitTask creates or retrieves a task under the supplied idempotency key.
func (client *OperatorClient) SubmitTask(ctx context.Context, idempotencyKey string, request protocol.TaskSubmitRequest) (protocol.Task, error) {
	var response protocol.Task
	if err := client.operatorRequest(ctx, http.MethodPost, "v1/tasks", nil, idempotencyKey, request, &response); err != nil {
		return protocol.Task{}, err
	}
	if err := validateTaskResponse("submit task", "", response); err != nil {
		return protocol.Task{}, err
	}
	return response, nil
}

// GetTask returns the current durable task state.
func (client *OperatorClient) GetTask(ctx context.Context, taskID string) (protocol.Task, error) {
	if err := validatePathID("task ID", taskID); err != nil {
		return protocol.Task{}, err
	}
	var response protocol.Task
	if err := client.operatorRequest(ctx, http.MethodGet, "v1/tasks/"+taskID, nil, "", nil, &response); err != nil {
		return protocol.Task{}, err
	}
	if err := validateTaskResponse("get task", taskID, response); err != nil {
		return protocol.Task{}, err
	}
	return response, nil
}

// CreateTaskCommand creates or retrieves an operator command under the supplied
// idempotency key. The returned resource may be runless for historical actions.
func (client *OperatorClient) CreateTaskCommand(ctx context.Context, taskID, idempotencyKey string, request protocol.TaskCommandRequest) (protocol.TaskCommand, error) {
	if err := validatePathID("task ID", taskID); err != nil {
		return protocol.TaskCommand{}, err
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return protocol.TaskCommand{}, errors.New("idempotency key must not be empty")
	}
	if err := validateTaskCommandRequest(request); err != nil {
		return protocol.TaskCommand{}, err
	}
	var response protocol.TaskCommand
	endpoint := "v1/tasks/" + taskID + "/commands"
	statusCode, err := client.operatorRequestWithStatus(ctx, http.MethodPost, endpoint, nil, idempotencyKey, request, &response)
	if err != nil {
		return protocol.TaskCommand{}, err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return protocol.TaskCommand{}, responseErrorf("invalid create task command response: expected HTTP 200 or 201, got HTTP %d", statusCode)
	}
	if err := validateTaskCommandResponse("create task command", taskID, request, response); err != nil {
		return protocol.TaskCommand{}, err
	}
	return response, nil
}

// RetryDelay classifies a failed request and selects a retry delay. It does not
// perform retries; callers retain their own retry and liveness policies.
func RetryDelay(ctx context.Context, err error, fallback time.Duration) (time.Duration, bool) {
	if err == nil || (ctx != nil && ctx.Err() != nil) || errors.Is(err, context.Canceled) {
		return 0, false
	}
	var apiError *APIError
	if errors.As(err, &apiError) {
		if apiError.StatusCode != http.StatusTooManyRequests && apiError.StatusCode < http.StatusInternalServerError {
			return 0, false
		}
		if apiError.retryAfterSet || apiError.RetryAfter > 0 {
			return apiError.RetryAfter, true
		}
		return fallback, true
	}
	var responseError *ResponseError
	if errors.As(err, &responseError) {
		return 0, false
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		if retryableTransportCause(urlError.Err) {
			return fallback, true
		}
		return 0, false
	}
	if retryableTransportCause(err) {
		return fallback, true
	}
	return 0, false
}

func retryableTransportCause(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func (client *Client) machineRequest(ctx context.Context, method, endpoint string, query url.Values, idempotencyKey string, request, response any) error {
	return client.request(ctx, method, endpoint, query, client.machineToken, idempotencyKey, request, response)
}

func (client *Client) requestGoal(ctx context.Context, method, endpoint string, query url.Values, idempotencyKey string, request, response any) error {
	_, err := client.requestGoalWithStatus(ctx, method, endpoint, query, idempotencyKey, request, response)
	return err
}

func (client *Client) requestGoalWithStatus(ctx context.Context, method, endpoint string, query url.Values, idempotencyKey string, request, response any) (int, error) {
	statusCode, responseBody, oversized, err := client.perform(ctx, method, endpoint, query, client.machineToken, idempotencyKey, request)
	if err != nil {
		return 0, err
	}
	if oversized {
		return 0, responseErrorf("response body exceeds %d bytes", client.maxResponseBytes)
	}
	if response == nil {
		return statusCode, nil
	}
	if err := decodeStrictJSON(responseBody, response); err != nil {
		return 0, responseErrorf("decode strict goal response: %w", err)
	}
	return statusCode, nil
}

func (client *OperatorClient) operatorRequest(ctx context.Context, method, endpoint string, query url.Values, idempotencyKey string, request, response any) error {
	return client.request(ctx, method, endpoint, query, client.operatorToken, idempotencyKey, request, response)
}

func (client *OperatorClient) operatorRequestWithStatus(ctx context.Context, method, endpoint string, query url.Values, idempotencyKey string, request, response any) (int, error) {
	return client.requestWithStatus(ctx, method, endpoint, query, client.operatorToken, idempotencyKey, request, response)
}

func (client *transport) request(ctx context.Context, method, endpoint string, query url.Values, token, idempotencyKey string, request, response any) error {
	_, err := client.requestWithStatus(ctx, method, endpoint, query, token, idempotencyKey, request, response)
	return err
}

func (client *transport) requestWithStatus(ctx context.Context, method, endpoint string, query url.Values, token, idempotencyKey string, request, response any) (int, error) {
	statusCode, responseBody, oversized, err := client.perform(ctx, method, endpoint, query, token, idempotencyKey, request)
	if err != nil {
		return 0, err
	}
	if oversized {
		return 0, responseErrorf("response body exceeds %d bytes", client.maxResponseBytes)
	}
	if response == nil && len(responseBody) == 0 {
		return statusCode, nil
	}
	if response == nil {
		var discarded any
		if err := decodeJSON(responseBody, &discarded); err != nil {
			return 0, responseErrorf("decode response: %w", err)
		}
		return statusCode, nil
	}
	if err := decodeJSON(responseBody, response); err != nil {
		return 0, responseErrorf("decode response: %w", err)
	}
	return statusCode, nil
}

func (client *transport) requestNoContent(ctx context.Context, method, endpoint string, query url.Values, token, idempotencyKey string, request any) error {
	statusCode, responseBody, oversized, err := client.perform(ctx, method, endpoint, query, token, idempotencyKey, request)
	if err != nil {
		return err
	}
	if statusCode != http.StatusNoContent {
		return responseErrorf("invalid append events response: expected HTTP 204 No Content, got HTTP %d", statusCode)
	}
	if oversized || len(responseBody) != 0 {
		return responseErrorf("invalid append events response: HTTP 204 must not include a response body")
	}
	return nil
}

func (client *transport) perform(ctx context.Context, method, endpoint string, query url.Values, token, idempotencyKey string, request any) (int, []byte, bool, error) {
	if strings.TrimSpace(token) == "" {
		return 0, nil, false, errors.New("bearer token must not be empty")
	}

	var body io.Reader
	if request != nil {
		encoded, err := json.Marshal(request)
		if err != nil {
			return 0, nil, false, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	endpointURL := *client.baseURL
	endpointURL.Path += "/" + endpoint
	endpointURL.RawPath = ""
	endpointURL.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpointURL.String(), body)
	if err != nil {
		return 0, nil, false, fmt.Errorf("create request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	if request != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		httpRequest.Header.Set("Idempotency-Key", idempotencyKey)
	}

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return 0, nil, false, fmt.Errorf("perform request: %w", err)
	}
	defer httpResponse.Body.Close()

	responseBody, oversized, err := readBounded(httpResponse.Body, client.maxResponseBytes)
	if err != nil {
		return 0, nil, false, err
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return 0, nil, false, decodeAPIError(httpResponse.StatusCode, httpResponse.Header, responseBody)
	}
	return httpResponse.StatusCode, responseBody, oversized, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, bool, error) {
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	value, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, false, fmt.Errorf("read response: %w", err)
	}
	if int64(len(value)) > limit {
		return nil, true, nil
	}
	return value, false, nil
}

func decodeJSON(value []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("response must contain one JSON value")
		}
		return err
	}
	return nil
}

func decodeAPIError(statusCode int, header http.Header, body []byte) error {
	retryDelay, retryAfterSet := retryAfter(header.Get("Retry-After"))
	apiError := &APIError{
		StatusCode:    statusCode,
		Code:          errorCodeForStatus(statusCode),
		RetryAfter:    retryDelay,
		retryAfterSet: retryAfterSet,
	}
	var envelope protocol.ErrorEnvelope
	if err := decodeJSON(body, &envelope); err == nil {
		if envelope.Error.Code != "" {
			apiError.Code = ErrorCode(envelope.Error.Code)
		}
		apiError.Message = envelope.Error.Message
	}
	return apiError
}

func errorCodeForStatus(statusCode int) ErrorCode {
	switch statusCode {
	case http.StatusBadRequest:
		return InvalidRequest
	case http.StatusUnauthorized:
		return Unauthenticated
	case http.StatusForbidden:
		return Forbidden
	case http.StatusNotFound:
		return NotFound
	case http.StatusConflict:
		return StateConflict
	case http.StatusGone:
		return AssignmentExpired
	case http.StatusUnprocessableEntity:
		return InvalidTransition
	case http.StatusTooManyRequests:
		return RateLimited
	case http.StatusServiceUnavailable:
		return ServiceUnavailable
	default:
		return UnexpectedHTTPStatus
	}
}

func retryAfter(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		if seconds > int64(1<<63-1)/int64(time.Second) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if date, err := http.ParseTime(value); err == nil {
		remaining := time.Until(date)
		if remaining < 0 {
			remaining = 0
		}
		return remaining, true
	}
	return 0, false
}

func validatePathID(field, value string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("%s must be a non-empty safe path segment", field)
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return fmt.Errorf("%s must be a non-empty safe path segment", field)
	}
	return nil
}
