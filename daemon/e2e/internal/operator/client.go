package operator

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

	"github.com/wxxb789/symmetry/daemon/internal/contracts"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const defaultMaxResponseBytes int64 = 1 << 20

var errRedirectNotAllowed = errors.New("control client does not allow redirects")

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

type APIError struct {
	StatusCode int
	Code       ErrorCode
	Message    string

	Details                      json.RawMessage
	EvidenceBatchConflictDetails *contracts.SymmetryEvidenceBatchConflictDetailsV1
	RetryAfter                   time.Duration
	retryAfterSet                bool
	detailsDecodeErr             error
}

// It preserves the underlying cause for diagnostics while remaining permanent
// for retry classification.
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

func IsOwnershipLost(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.Code == OwnershipLost
}

func IsTerminalGraceExpired(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.Code == TerminalGraceExpired
}

type Option func(*transport)

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

type OperatorClient struct {
	*transport
	operatorToken string
}

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

func (client *OperatorClient) SubmitTask(ctx context.Context, idempotencyKey string, request protocol.TaskSubmitRequest) (Task, error) {
	var response Task
	if err := client.operatorRequest(ctx, http.MethodPost, "v1/tasks", nil, idempotencyKey, request, &response); err != nil {
		return Task{}, err
	}
	if err := validateTaskResponse("submit task", "", response); err != nil {
		return Task{}, err
	}
	return response, nil
}

func (client *OperatorClient) GetTask(ctx context.Context, taskID string) (Task, error) {
	if err := validatePathID("task ID", taskID); err != nil {
		return Task{}, err
	}
	var response Task
	if err := client.operatorRequest(ctx, http.MethodGet, "v1/tasks/"+taskID, nil, "", nil, &response); err != nil {
		return Task{}, err
	}
	if err := validateTaskResponse("get task", taskID, response); err != nil {
		return Task{}, err
	}
	return response, nil
}

// The returned resource may be runless for historical actions.
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

// It does not perform retries; callers retain their own retry and liveness policies.
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

func rejectDuplicateJSONMembersOnly(value []byte) error {
	err := protocol.RejectDuplicateJSONMembers(value)
	if err == nil || !strings.Contains(err.Error(), "duplicate JSON object member") {
		return nil
	}
	return err
}

func decodeJSON(value []byte, target any) error {
	if err := rejectDuplicateJSONMembersOnly(value); err != nil {
		return err
	}
	return decodeJSONUnchecked(value, target)
}

func decodeJSONUnchecked(value []byte, target any) error {
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
	var envelope struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
			Detail  json.RawMessage `json:"detail"`
		} `json:"error"`
	}
	if err := decodeJSON(body, &envelope); err == nil {
		if envelope.Error.Code != "" {
			apiError.Code = ErrorCode(envelope.Error.Code)
		}
		apiError.Message = envelope.Error.Message
		apiError.Details = append(apiError.Details, envelope.Error.Details...)
		if len(apiError.Details) == 0 {
			apiError.Details = append(apiError.Details, envelope.Error.Detail...)
		}
		if apiError.Code == IdempotencyConflict {
			if details, err := decodeEvidenceBatchConflictDetails(apiError.Details); err != nil {
				apiError.detailsDecodeErr = err
			} else {
				apiError.EvidenceBatchConflictDetails = details
			}
		}
	}
	return apiError
}

func decodeEvidenceBatchConflictDetails(data json.RawMessage) (*contracts.SymmetryEvidenceBatchConflictDetailsV1, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, nil
	}
	if _, present := fields["items"]; !present {
		return nil, nil
	}
	var details contracts.SymmetryEvidenceBatchConflictDetailsV1
	if err := contracts.DecodeSchema(contracts.EnvelopeEvidenceBatchConflictDetails, trimmed, &details); err != nil {
		return nil, err
	}
	return &details, nil
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
