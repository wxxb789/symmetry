// Package control provides a strict, single-request HTTP client for protocol v1.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const defaultMaxResponseBytes int64 = 1 << 20

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
	StateConflict        ErrorCode = "state_conflict"
	AssignmentExpired    ErrorCode = "assignment_expired"
	InvalidTransition    ErrorCode = "invalid_transition"
	RateLimited          ErrorCode = "rate_limited"
	ServiceUnavailable   ErrorCode = "service_unavailable"
	UnexpectedHTTPStatus ErrorCode = "unexpected_http_status"
)

// APIError describes a non-success response returned by the control plane.
type APIError struct {
	StatusCode int
	Code       ErrorCode
	Message    string
	RetryAfter time.Duration
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
	transport := &transport{
		baseURL:          parsed,
		httpClient:       httpClient,
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
	return parsed, nil
}

// Enroll registers a machine using a one-time enrollment bearer token.
func (client *EnrollmentClient) Enroll(ctx context.Context, enrollmentToken string, request protocol.EnrollRequest) (protocol.EnrollResponse, error) {
	var response protocol.EnrollResponse
	if err := client.request(ctx, http.MethodPost, "v1/daemon/enroll", nil, enrollmentToken, "", request, &response); err != nil {
		return protocol.EnrollResponse{}, err
	}
	if err := validateEnrollResponse(response); err != nil {
		return protocol.EnrollResponse{}, err
	}
	return response, nil
}

// RegisterSession registers a daemon instance and its configured runtimes.
func (client *Client) RegisterSession(ctx context.Context, request protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	var response protocol.SessionRegistrationResponse
	if err := client.machineRequest(ctx, http.MethodPost, "v1/daemon/sessions", nil, "", request, &response); err != nil {
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
	if err := client.machineRequest(ctx, http.MethodPost, "v1/runtimes/"+runtimeID+"/heartbeat", nil, "", request, &response); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	if err := validateRuntimeSnapshot(response); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	return response, nil
}

// Work fetches the non-destructive current snapshot for a runtime.
func (client *Client) Work(ctx context.Context, runtimeID string, runtimeEpoch int64) (protocol.RuntimeSnapshot, error) {
	if err := validatePathID("runtime ID", runtimeID); err != nil {
		return protocol.RuntimeSnapshot{}, err
	}
	var response protocol.RuntimeSnapshot
	query := url.Values{"runtime_epoch": []string{strconv.FormatInt(runtimeEpoch, 10)}}
	if err := client.machineRequest(ctx, http.MethodGet, "v1/runtimes/"+runtimeID+"/work", query, "", nil, &response); err != nil {
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
	var response protocol.ClaimResponse
	if err := client.machineRequest(ctx, http.MethodPost, "v1/runs/"+runID+"/claim", nil, "", request, &response); err != nil {
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
	if err := client.machineRequest(ctx, http.MethodPost, "v1/runs/"+runID+"/heartbeat", nil, "", request, &response); err != nil {
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
	return client.machineRequest(ctx, http.MethodPost, "v1/runs/"+runID+"/events", nil, "", request, nil)
}

// Transition applies a caller-identified lifecycle transition without a retry policy.
func (client *Client) Transition(ctx context.Context, runID string, request protocol.StateTransitionRequest) error {
	if err := validatePathID("run ID", runID); err != nil {
		return err
	}
	return client.machineRequest(ctx, http.MethodPost, "v1/runs/"+runID+"/state", nil, "", request, nil)
}

// Reconcile compares the caller's local run journal with durable control-plane state.
func (client *Client) Reconcile(ctx context.Context, runtimeID string, request protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	if err := validatePathID("runtime ID", runtimeID); err != nil {
		return protocol.ReconcileResponse{}, err
	}
	var response protocol.ReconcileResponse
	if err := client.machineRequest(ctx, http.MethodPost, "v1/runtimes/"+runtimeID+"/reconcile", nil, "", request, &response); err != nil {
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
	return client.machineRequest(ctx, http.MethodPost, "v1/commands/"+commandID+"/ack", nil, "", request, nil)
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

// CancelTask requests cancellation for a task.
func (client *OperatorClient) CancelTask(ctx context.Context, taskID string) (protocol.Task, error) {
	if err := validatePathID("task ID", taskID); err != nil {
		return protocol.Task{}, err
	}
	var response protocol.Task
	if err := client.operatorRequest(ctx, http.MethodPost, "v1/tasks/"+taskID+"/cancel", nil, "", struct{}{}, &response); err != nil {
		return protocol.Task{}, err
	}
	if err := validateTaskResponse("cancel task", taskID, response); err != nil {
		return protocol.Task{}, err
	}
	return response, nil
}

// SubmitInput sends human input under the supplied idempotency key.
func (client *OperatorClient) SubmitInput(ctx context.Context, taskID, idempotencyKey string, request protocol.TaskInputRequest) (protocol.Task, error) {
	if err := validatePathID("task ID", taskID); err != nil {
		return protocol.Task{}, err
	}
	var response protocol.Task
	if err := client.operatorRequest(ctx, http.MethodPost, "v1/tasks/"+taskID+"/input", nil, idempotencyKey, request, &response); err != nil {
		return protocol.Task{}, err
	}
	if err := validateTaskResponse("submit task input", taskID, response); err != nil {
		return protocol.Task{}, err
	}
	return response, nil
}

func (client *Client) machineRequest(ctx context.Context, method, endpoint string, query url.Values, idempotencyKey string, request, response any) error {
	return client.request(ctx, method, endpoint, query, client.machineToken, idempotencyKey, request, response)
}

func (client *OperatorClient) operatorRequest(ctx context.Context, method, endpoint string, query url.Values, idempotencyKey string, request, response any) error {
	return client.request(ctx, method, endpoint, query, client.operatorToken, idempotencyKey, request, response)
}

func (client *transport) request(ctx context.Context, method, endpoint string, query url.Values, token, idempotencyKey string, request, response any) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("bearer token must not be empty")
	}

	var body io.Reader
	if request != nil {
		encoded, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	endpointURL := *client.baseURL
	basePath := strings.TrimRight(endpointURL.Path, "/")
	if basePath == "" {
		basePath = "/api"
	}
	endpointURL.Path = basePath + "/" + endpoint
	endpointURL.RawPath = ""
	endpointURL.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpointURL.String(), body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
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
		return fmt.Errorf("perform request: %w", err)
	}
	defer httpResponse.Body.Close()

	responseBody, oversized, err := readBounded(httpResponse.Body, client.maxResponseBytes)
	if err != nil {
		return err
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return decodeAPIError(httpResponse.StatusCode, httpResponse.Header, responseBody)
	}
	if oversized {
		return fmt.Errorf("response body exceeds %d bytes", client.maxResponseBytes)
	}
	if response == nil && len(responseBody) == 0 {
		return nil
	}
	if response == nil {
		var discarded any
		if err := decodeJSON(responseBody, &discarded); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}
	if err := decodeJSON(responseBody, response); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
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
	apiError := &APIError{StatusCode: statusCode, Code: errorCodeForStatus(statusCode), RetryAfter: retryAfter(header.Get("Retry-After"))}
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

func retryAfter(value string) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		remaining := time.Until(date)
		if remaining > 0 {
			return remaining
		}
	}
	return 0
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
