package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const jsonRPCVersion = "2.0"

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type rpcResponse struct {
	result  json.RawMessage
	err     json.RawMessage
	failure error
}

type rpcErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   rpcError        `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type nativeRPCError struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (err nativeRPCError) Error() string {
	if err.Message == "" {
		return "Codex app-server returned an RPC error"
	}
	return "Codex app-server RPC error: " + err.Message
}

func decodeRPCError(method string, raw json.RawMessage) error {
	response, err := parseNativeRPCError(raw)
	if err != nil {
		return fmt.Errorf("%s returned invalid RPC error: %w", method, err)
	}
	return response
}

func parseNativeRPCError(raw json.RawMessage) (nativeRPCError, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		if err == nil {
			err = errors.New("RPC error must be a JSON object")
		}
		return nativeRPCError{}, fmt.Errorf("malformed RPC error: %w", err)
	}
	codeRaw, ok := object["code"]
	if !ok {
		return nativeRPCError{}, errors.New("RPC error is missing code")
	}
	var code int64
	if err := json.Unmarshal(codeRaw, &code); err != nil {
		return nativeRPCError{}, fmt.Errorf("RPC error code must be an integer: %w", err)
	}
	messageRaw, ok := object["message"]
	if !ok {
		return nativeRPCError{}, errors.New("RPC error is missing message")
	}
	var message string
	if err := json.Unmarshal(messageRaw, &message); err != nil || message == "" {
		if err == nil {
			err = errors.New("message must be non-empty")
		}
		return nativeRPCError{}, fmt.Errorf("RPC error message is invalid: %w", err)
	}
	return nativeRPCError{
		Code:    append(json.RawMessage(nil), codeRaw...),
		Message: message,
		Data:    append(json.RawMessage(nil), object["data"]...),
	}, nil
}

func validateRPCResult(method string, raw json.RawMessage) error {
	if !hasRPCValue(raw) {
		return fmt.Errorf("%s response result is missing or null", method)
	}
	if method != appServerMethodTurnInterrupt {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		if err == nil {
			err = errors.New("result must be a JSON object")
		}
		return fmt.Errorf("%s response result is invalid: %w", method, err)
	}
	return nil
}

// parseRPCID accepts only the numeric IDs this adapter emits. The native
// protocol permits string IDs too, but treating "1" as identical to 1 would
// let an unrelated response satisfy an outstanding request.
func parseRPCID(raw json.RawMessage) (uint64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var numeric uint64
	if err := json.Unmarshal(raw, &numeric); err == nil && numeric > 0 {
		return numeric, true
	}
	return 0, false
}

func hasRPCValue(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null"
}

func validServerRequestID(raw json.RawMessage) bool {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text != ""
	}
	var numeric int64
	return json.Unmarshal(raw, &numeric) == nil
}

type clientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type initializeCapabilities struct {
	OptOutNotificationMethods []string `json:"optOutNotificationMethods,omitempty"`
}

type initializeParams struct {
	ClientInfo   clientInfo              `json:"clientInfo"`
	Capabilities *initializeCapabilities `json:"capabilities,omitempty"`
}

type initializeResponse struct {
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
	UserAgent      string `json:"userAgent"`
}

type threadStartParams struct {
	ApprovalPolicy    string `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer string `json:"approvalsReviewer,omitempty"`
	CWD               string `json:"cwd"`
	Ephemeral         *bool  `json:"ephemeral,omitempty"`
	Model             string `json:"model,omitempty"`
	Sandbox           string `json:"sandbox,omitempty"`
}

type nativeThread struct {
	CWD       string `json:"cwd,omitempty"`
	Ephemeral *bool  `json:"ephemeral,omitempty"`
	ID        string `json:"id"`
}

type threadStartResponse struct {
	ApprovalPolicy        json.RawMessage     `json:"approvalPolicy"`
	ApprovalsReviewer     json.RawMessage     `json:"approvalsReviewer"`
	CWD                   string              `json:"cwd"`
	Model                 string              `json:"model"`
	ModelProvider         string              `json:"modelProvider"`
	Sandbox               nativeSandboxPolicy `json:"sandbox"`
	Thread                nativeThread        `json:"thread"`
	RuntimeWorkspaceRoots []string            `json:"runtimeWorkspaceRoots"`
}

// nativeSandboxPolicy is the response-side policy selected by app-server.
// The request accepts legacy string values, but the response is a tagged
// object. Keeping both sides explicit prevents a requested workspace-write
// policy from being mistaken for an actually granted write-capable session.
type nativeSandboxPolicy struct {
	Type          string          `json:"type"`
	NetworkAccess json.RawMessage `json:"networkAccess,omitempty"`
	WritableRoots []string        `json:"writableRoots,omitempty"`
}

func (response threadStartResponse) validate(expectedCWD string) error {
	if response.Thread.ID == "" {
		return errors.New("thread/start response is missing thread.id")
	}
	if response.Thread.Ephemeral == nil || *response.Thread.Ephemeral {
		return errors.New("thread/start response did not retain the native thread")
	}
	if response.Thread.CWD == "" || !sameWorkspaceDirectory(response.Thread.CWD, expectedCWD) {
		return fmt.Errorf("thread/start response thread.cwd %q does not match requested workspace %q", response.Thread.CWD, expectedCWD)
	}
	if response.CWD == "" || !sameWorkspaceDirectory(response.CWD, expectedCWD) {
		return fmt.Errorf("thread/start response cwd %q does not match requested workspace %q", response.CWD, expectedCWD)
	}
	if strings.TrimSpace(response.Model) == "" || strings.TrimSpace(response.ModelProvider) == "" {
		return errors.New("thread/start response is missing model identity")
	}
	if len(response.ApprovalPolicy) == 0 || string(response.ApprovalPolicy) == "null" {
		return errors.New("thread/start response is missing approvalPolicy")
	}
	if len(response.ApprovalsReviewer) == 0 || string(response.ApprovalsReviewer) == "null" {
		return errors.New("thread/start response is missing approvalsReviewer")
	}
	var approvalPolicy string
	if err := json.Unmarshal(response.ApprovalPolicy, &approvalPolicy); err != nil || approvalPolicy != nativeApprovalOnRequest {
		return fmt.Errorf("thread/start response approvalPolicy %q is not %q", approvalPolicy, nativeApprovalOnRequest)
	}
	var approvalsReviewer string
	if err := json.Unmarshal(response.ApprovalsReviewer, &approvalsReviewer); err != nil || approvalsReviewer != "user" {
		return fmt.Errorf("thread/start response approvalsReviewer %q is not user", approvalsReviewer)
	}
	if response.Sandbox.Type == "" {
		return errors.New("thread/start response is missing sandbox.type")
	}
	if response.Sandbox.Type == "workspaceWrite" {
		if len(response.Sandbox.WritableRoots) == 0 {
			return errors.New("thread/start workspaceWrite response is missing writableRoots")
		}
		for _, root := range response.Sandbox.WritableRoots {
			if strings.TrimSpace(root) == "" || !workspaceContains(expectedCWD, root) {
				return fmt.Errorf("thread/start writable root %q escapes requested workspace %q", root, expectedCWD)
			}
		}
		if len(response.Sandbox.NetworkAccess) == 0 || strings.TrimSpace(string(response.Sandbox.NetworkAccess)) == "null" {
			return errors.New("thread/start workspaceWrite response is missing networkAccess")
		}
		networkAccess, err := optionalBool(response.Sandbox.NetworkAccess)
		if err != nil {
			return fmt.Errorf("thread/start networkAccess is invalid: %w", err)
		}
		if networkAccess {
			return errors.New("thread/start workspaceWrite response enables network access beyond the admitted policy")
		}
		for _, root := range response.RuntimeWorkspaceRoots {
			if strings.TrimSpace(root) == "" || !workspaceContains(expectedCWD, root) {
				return fmt.Errorf("thread/start runtime workspace root %q escapes requested workspace %q", root, expectedCWD)
			}
		}
	}
	return nil
}

func optionalBool(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return false, errors.New("must be a boolean when present")
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, err
	}
	return value, nil
}

func workspaceContains(workspace, candidate string) bool {
	if !filepath.IsAbs(strings.TrimSpace(candidate)) {
		return false
	}
	workspace, err := canonicalPath(workspace)
	if err != nil {
		return false
	}
	candidate, err = canonicalPath(candidate)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(workspace, candidate)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	if relative == "." {
		return true
	}
	parent := ".." + string(os.PathSeparator)
	return relative != ".." && !strings.HasPrefix(relative, parent)
}

func canonicalPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	return filepath.Clean(absolute), nil
}

func sameWorkspaceDirectory(left, right string) bool {
	left, leftErr := filepath.Abs(left)
	right, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(left); err == nil {
		left = resolved
	}
	if resolved, err := filepath.EvalSymlinks(right); err == nil {
		right = resolved
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if os.PathSeparator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}

type userInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type turnStartParams struct {
	CWD          string         `json:"cwd,omitempty"`
	Model        string         `json:"model,omitempty"`
	Input        []userInput    `json:"input"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	ThreadID     string         `json:"threadId"`
}

type nativeTurn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Items  []json.RawMessage `json:"items"`
	Error  *nativeTurnError  `json:"error,omitempty"`
}

type nativeTurnError struct {
	CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
	Message        string          `json:"message"`
}

type turnStartResponse struct {
	Turn nativeTurn `json:"turn"`
}

type turnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type agentMessageDeltaParams struct {
	Delta    string `json:"delta"`
	ItemID   string `json:"itemId"`
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type threadTokenUsageParams struct {
	ThreadID   string          `json:"threadId"`
	TokenUsage json.RawMessage `json:"tokenUsage"`
	TurnID     string          `json:"turnId"`
}

type tokenCounterWire struct {
	CacheWriteInputTokens *int64 `json:"cacheWriteInputTokens"`
	CachedInputTokens     *int64 `json:"cachedInputTokens"`
	InputTokens           *int64 `json:"inputTokens"`
	OutputTokens          *int64 `json:"outputTokens"`
	ReasoningOutputTokens *int64 `json:"reasoningOutputTokens"`
	TotalTokens           *int64 `json:"totalTokens"`
}

type tokenUsageWire struct {
	Last               *tokenCounterWire `json:"last"`
	ModelContextWindow *int64            `json:"modelContextWindow"`
	Total              *tokenCounterWire `json:"total"`
}

type normalizedTokenCounter struct {
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	CacheWriteInputTokens *int64 `json:"cache_write_input_tokens,omitempty"`
	InputTokens           int64  `json:"input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
}

type normalizedTokenUsage struct {
	Last               normalizedTokenCounter `json:"last"`
	ModelContextWindow *int64                 `json:"model_context_window,omitempty"`
	Total              normalizedTokenCounter `json:"total"`
}

func normalizeTokenUsage(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var value tokenUsageWire
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value.Last == nil || value.Total == nil {
		return nil, errors.New("tokenUsage requires last and total counters")
	}
	if value.ModelContextWindow != nil && *value.ModelContextWindow < 0 {
		return nil, errors.New("modelContextWindow must not be negative")
	}
	last, err := normalizeTokenCounter(value.Last)
	if err != nil {
		return nil, fmt.Errorf("last counters: %w", err)
	}
	total, err := normalizeTokenCounter(value.Total)
	if err != nil {
		return nil, fmt.Errorf("total counters: %w", err)
	}
	return json.Marshal(normalizedTokenUsage{
		Last:               last,
		ModelContextWindow: cloneInt64(value.ModelContextWindow),
		Total:              total,
	})
}

func normalizeTokenCounter(value *tokenCounterWire) (normalizedTokenCounter, error) {
	if value == nil || value.CachedInputTokens == nil || value.InputTokens == nil || value.OutputTokens == nil || value.ReasoningOutputTokens == nil || value.TotalTokens == nil {
		return normalizedTokenCounter{}, errors.New("all token counters are required")
	}
	values := []int64{*value.CachedInputTokens, *value.InputTokens, *value.OutputTokens, *value.ReasoningOutputTokens, *value.TotalTokens}
	if value.CacheWriteInputTokens != nil {
		values = append(values, *value.CacheWriteInputTokens)
	}
	for _, counter := range values {
		if counter < 0 {
			return normalizedTokenCounter{}, errors.New("token counters must not be negative")
		}
	}
	return normalizedTokenCounter{
		CachedInputTokens:     *value.CachedInputTokens,
		CacheWriteInputTokens: cloneInt64(value.CacheWriteInputTokens),
		InputTokens:           *value.InputTokens,
		OutputTokens:          *value.OutputTokens,
		ReasoningOutputTokens: *value.ReasoningOutputTokens,
		TotalTokens:           *value.TotalTokens,
	}, nil
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

type turnCompletedParams struct {
	ThreadID string     `json:"threadId"`
	Turn     nativeTurn `json:"turn"`
}

type threadStartedParams struct {
	Thread nativeThread `json:"thread"`
}

type turnStartedParams struct {
	ThreadID string     `json:"threadId"`
	Turn     nativeTurn `json:"turn"`
}

type nativeErrorNotificationParams struct {
	Error     nativeErrorDetail `json:"error"`
	ThreadID  string            `json:"threadId"`
	TurnID    string            `json:"turnId"`
	WillRetry bool              `json:"willRetry"`
}

type nativeErrorDetail struct {
	CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
	Message        string          `json:"message"`
}

func classifyNativeError(raw json.RawMessage) *protocol.TaskResultReason {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var reason protocol.TaskResultReason
	switch value {
	case "unauthorized":
		reason = protocol.TaskResultReasonAuth
	case "usageLimitExceeded", "sessionBudgetExceeded":
		reason = protocol.TaskResultReasonQuota
	case "rateLimitExceeded", "serverOverloaded":
		reason = protocol.TaskResultReasonRateLimit
	case "contextWindowExceeded":
		reason = protocol.TaskResultReasonContextOverflow
	default:
		return nil
	}
	return &reason
}

func (params turnCompletedParams) taskResult() (protocol.TaskResult, json.RawMessage, error) {
	if params.Turn.Status != "completed" && params.Turn.Status != "interrupted" && params.Turn.Status != "failed" {
		return protocol.TaskResult{}, nil, fmt.Errorf("turn status %q is not terminal", params.Turn.Status)
	}
	for index := len(params.Turn.Items) - 1; index >= 0; index-- {
		var item struct {
			Type  string `json:"type"`
			Phase string `json:"phase"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal(params.Turn.Items[index], &item); err != nil {
			continue
		}
		if item.Type != "agentMessage" || (item.Phase != "" && item.Phase != "final_answer") || item.Text == "" {
			continue
		}
		raw := json.RawMessage(item.Text)
		result, err := protocol.ParseTaskResult(raw)
		if err != nil {
			return protocol.TaskResult{}, nil, fmt.Errorf("final agentMessage is not a valid Symmetry TaskResult: %w", err)
		}
		return result, append(json.RawMessage(nil), raw...), nil
	}
	return protocol.TaskResult{}, nil, errors.New("terminal turn contains no final agentMessage TaskResult")
}

func notificationThreadID(raw json.RawMessage) (string, bool) {
	var identity struct {
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.ThreadID == "" {
		return "", false
	}
	return identity.ThreadID, true
}

func notificationIdentity(raw json.RawMessage) (string, string, bool) {
	var identity struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil || identity.ThreadID == "" || identity.TurnID == "" {
		return "", "", false
	}
	return identity.ThreadID, identity.TurnID, true
}

func itemNotificationIdentity(raw json.RawMessage) (string, string, json.RawMessage, bool) {
	var identity struct {
		ThreadID string          `json:"threadId"`
		TurnID   string          `json:"turnId"`
		Item     json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil || identity.ThreadID == "" || identity.TurnID == "" || len(identity.Item) == 0 {
		return "", "", nil, false
	}
	return identity.ThreadID, identity.TurnID, identity.Item, true
}
