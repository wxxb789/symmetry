package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// Client only speaks to an explicit numeric loopback OpenCode serve endpoint.
// It intentionally disables proxy selection, redirects, compression, and
// automatic retries so credentials and ambiguous admissions cannot escape.
type Client struct {
	baseURL      *url.URL
	username     string
	password     string
	httpClient   *http.Client
	maxBodyBytes int64
}

// Config configures one private OpenCode serve connection.
type Config struct {
	BaseURL          string
	Username         string
	Password         string
	Timeout          time.Duration
	MaxBodyBytes     int64
	VerifyConnection ConnectionVerifier
}

// ConnectionVerifier confirms that one already-connected TCP peer belongs to
// the daemon-owned server before http.Transport can write any request bytes.
// It must validate the connection itself, not a second listener lookup.
type ConnectionVerifier func(context.Context, net.Conn) error

// NewClient validates credential locality before any request is issued.
func NewClient(config Config) (*Client, error) {
	baseURL, err := parseLoopbackURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.Username) == "" || strings.TrimSpace(config.Password) == "" {
		return nil, fmt.Errorf("%w: basic-auth username and password must be non-empty", ErrInvalidConfig)
	}
	maxBodyBytes := config.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	if config.VerifyConnection != nil {
		dialer := &net.Dialer{}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			if err := config.VerifyConnection(ctx, connection); err != nil {
				_ = connection.Close()
				return nil, fmt.Errorf("%w: %w", ErrPeerOwnership, err)
			}
			return connection, nil
		}
	}
	return &Client{
		baseURL:  baseURL,
		username: config.Username,
		password: config.Password,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxBodyBytes: maxBodyBytes,
	}, nil
}

func parseLoopbackURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" || parsed.Port() == "" {
		return nil, fmt.Errorf("%w: base URL must be an absolute http loopback URL with an explicit port", ErrInvalidConfig)
	}
	host := net.ParseIP(parsed.Hostname())
	if host == nil || !host.IsLoopback() {
		return nil, fmt.Errorf("%w: base URL host must be a numeric loopback address", ErrInvalidConfig)
	}
	port, err := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil || port == 0 || port > 65535 {
		return nil, fmt.Errorf("%w: base URL port must be in 1..65535", ErrInvalidConfig)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("%w: base URL cannot contain a path", ErrInvalidConfig)
	}
	parsed.Path = ""
	return parsed, nil
}

// Health performs the authenticated readiness boundary. HTTP reachability is
// insufficient: OpenCode must return exactly a healthy boolean response.
func (client *Client) Health(ctx context.Context) error {
	body, err := client.do(ctx, http.MethodGet, "/api/health", nil, http.StatusOK)
	if err != nil {
		return err
	}
	object, err := strictObject(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidHealth, err)
	}
	raw, ok := object["healthy"]
	if !ok || !bytes.Equal(bytes.TrimSpace(raw), []byte("true")) {
		return ErrInvalidHealth
	}
	return nil
}

// CreateSession sends one non-retryable session creation request and validates
// the server identity before returning it to a caller.
func (client *Client) CreateSession(ctx context.Context, request CreateSessionRequest) (SessionInfo, error) {
	if err := request.validate(); err != nil {
		return SessionInfo{}, err
	}
	body, err := client.do(ctx, http.MethodPost, "/api/session", request, http.StatusOK)
	if err != nil {
		return SessionInfo{}, err
	}
	object, err := responseDataObject(body)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	info, err := decodeSessionInfo(object)
	if err != nil {
		return SessionInfo{}, err
	}
	if err := validateSession(info, request); err != nil {
		return SessionInfo{}, err
	}
	return info, nil
}

// Prompt sends one input admission request. A returned admission is explicitly
// not evidence of task execution, output, usage, or completion.
func (client *Client) Prompt(ctx context.Context, sessionID string, request PromptRequest) (PromptAdmission, error) {
	if !isID(sessionID, "ses_") {
		return PromptAdmission{}, fmt.Errorf("%w: session id must start with ses_", ErrInvalidPrompt)
	}
	if err := request.validate(); err != nil {
		return PromptAdmission{}, err
	}
	payload := struct {
		ID       string `json:"id"`
		Prompt   any    `json:"prompt"`
		Delivery string `json:"delivery"`
		Resume   *bool  `json:"resume,omitempty"`
	}{
		ID: request.ID,
		Prompt: struct {
			Text string `json:"text"`
		}{Text: request.Text},
		Delivery: request.Delivery,
		Resume: func() *bool {
			if !request.Resume {
				return nil
			}
			return &request.Resume
		}(),
	}
	body, err := client.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/prompt", payload, http.StatusOK)
	if err != nil {
		return PromptAdmission{}, err
	}
	object, err := responseDataObject(body)
	if err != nil {
		return PromptAdmission{}, fmt.Errorf("%w: %v", ErrInvalidPrompt, err)
	}
	admission, err := decodeAdmission(object)
	if err != nil {
		return PromptAdmission{}, err
	}
	if err := validateAdmission(admission, sessionID, request); err != nil {
		return PromptAdmission{}, err
	}
	return admission, nil
}

// Interrupt issues the documented cancellation control. It does not establish
// pause, resume, process shutdown, or a terminal native result.
func (client *Client) Interrupt(ctx context.Context, sessionID string) error {
	if !isID(sessionID, "ses_") {
		return fmt.Errorf("%w: session id must start with ses_", ErrInvalidSession)
	}
	body, err := client.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/interrupt", nil, http.StatusNoContent)
	if err != nil {
		return err
	}
	if len(body) != 0 {
		return fmt.Errorf("%w: interrupt response must be empty", ErrMalformedResponse)
	}
	return nil
}

// OpenGlobalEvents opens the private global SSE endpoint. Its caller owns the
// returned body and must feed it through Decoder; a connection marker is not a
// task lifecycle or completion signal.
func (client *Client) OpenGlobalEvents(ctx context.Context) (io.ReadCloser, error) {
	return client.openSSE(ctx, "/api/event", "")
}

// OpenSessionEvents opens OpenCode's durable replay stream using its exclusive
// sequence cursor. The stream itself remains untrusted until every frame is
// accepted by SessionEventValidator.
func (client *Client) OpenSessionEvents(ctx context.Context, sessionID string, after uint64) (io.ReadCloser, error) {
	if !isID(sessionID, "ses_") {
		return nil, fmt.Errorf("%w: session id must start with ses_", ErrInvalidDurableCursor)
	}
	return client.openSSE(ctx, "/api/session/"+url.PathEscape(sessionID)+"/event", "after="+fmt.Sprintf("%d", after))
}

func (client *Client) openSSE(ctx context.Context, endpoint, rawQuery string) (io.ReadCloser, error) {
	if client == nil || client.baseURL == nil || client.httpClient == nil {
		return nil, fmt.Errorf("%w: client is nil", ErrInvalidConfig)
	}
	endpointURL := *client.baseURL
	endpointURL.Path = path.Clean(endpoint)
	endpointURL.RawQuery = rawQuery
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create SSE request: %v", ErrInvalidConfig, err)
	}
	request.SetBasicAuth(client.username, client.password)
	request.Header.Set("Accept", "text/event-stream")
	// An SSE session can legitimately outlive ordinary RPC timeouts; cancellation
	// belongs exclusively to the caller's context and closing the returned body.
	streamClient := *client.httpClient
	streamClient.Timeout = 0
	response, err := streamClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%w: got %d, want %d", ErrUnexpectedStatus, response.StatusCode, http.StatusOK)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/event-stream" {
		response.Body.Close()
		return nil, fmt.Errorf("%w: SSE response must have text/event-stream content type", ErrMalformedResponse)
	}
	return response.Body, nil
}

func (client *Client) do(ctx context.Context, method, endpoint string, payload any, expectedStatus int) ([]byte, error) {
	if client == nil || client.baseURL == nil || client.httpClient == nil {
		return nil, fmt.Errorf("%w: client is nil", ErrInvalidConfig)
	}
	endpointURL := *client.baseURL
	endpointURL.Path = path.Clean(endpoint)
	if !strings.HasPrefix(endpointURL.Path, "/api/") {
		return nil, fmt.Errorf("%w: invalid endpoint", ErrInvalidConfig)
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("%w: encode request: %v", ErrInvalidConfig, err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpointURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", ErrInvalidConfig, err)
	}
	request.SetBasicAuth(client.username, client.password)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, client.maxBodyBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(responseBody)) > client.maxBodyBytes {
		return nil, ErrResponseTooLarge
	}
	if response.StatusCode != expectedStatus {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrUnexpectedStatus, response.StatusCode, expectedStatus)
	}
	return responseBody, nil
}

func responseDataObject(body []byte) (map[string]json.RawMessage, error) {
	outer, err := strictObject(body)
	if err != nil {
		return nil, err
	}
	raw, ok := outer["data"]
	if !ok {
		return nil, ErrMalformedResponse
	}
	return strictObject(raw)
}

func decodeSessionInfo(object map[string]json.RawMessage) (SessionInfo, error) {
	info := SessionInfo{}
	var ok bool
	if info.ID, ok = requiredString(object, "id"); !ok {
		return SessionInfo{}, ErrInvalidSession
	}
	if info.ProjectID, ok = requiredString(object, "projectID"); !ok {
		return SessionInfo{}, ErrInvalidSession
	}
	rawLocation, exists := object["location"]
	if !exists {
		return SessionInfo{}, ErrInvalidSession
	}
	location, err := strictObject(rawLocation)
	if err != nil {
		return SessionInfo{}, ErrInvalidSession
	}
	if info.Location.Directory, ok = requiredString(location, "directory"); !ok {
		return SessionInfo{}, ErrInvalidSession
	}
	if raw, exists := location["workspaceID"]; exists {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &info.Location.WorkspaceID) != nil || strings.TrimSpace(info.Location.WorkspaceID) == "" {
			return SessionInfo{}, ErrInvalidSession
		}
	}
	return info, nil
}

func decodeAdmission(object map[string]json.RawMessage) (PromptAdmission, error) {
	admission := PromptAdmission{}
	var ok bool
	if admission.AdmittedSeq, ok = requiredPositiveInt(object, "admittedSeq"); !ok {
		return PromptAdmission{}, ErrInvalidPrompt
	}
	if admission.ID, ok = requiredString(object, "id"); !ok {
		return PromptAdmission{}, ErrInvalidPrompt
	}
	if admission.SessionID, ok = requiredString(object, "sessionID"); !ok {
		return PromptAdmission{}, ErrInvalidPrompt
	}
	if admission.Delivery, ok = requiredString(object, "delivery"); !ok {
		return PromptAdmission{}, ErrInvalidPrompt
	}
	if admission.TimeCreated, ok = requiredPositiveInt64(object, "timeCreated"); !ok {
		return PromptAdmission{}, ErrInvalidPrompt
	}
	rawPrompt, exists := object["prompt"]
	if !exists {
		return PromptAdmission{}, ErrInvalidPrompt
	}
	prompt, err := strictObject(rawPrompt)
	if err != nil {
		return PromptAdmission{}, ErrInvalidPrompt
	}
	if admission.Text, ok = requiredString(prompt, "text"); !ok {
		return PromptAdmission{}, ErrInvalidPrompt
	}
	if raw, exists := object["promotedSeq"]; exists {
		var promoted uint64
		if json.Unmarshal(raw, &promoted) != nil || promoted == 0 {
			return PromptAdmission{}, ErrInvalidPrompt
		}
		admission.PromotedSeq = &promoted
	}
	return admission, nil
}
