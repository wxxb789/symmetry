package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	goalReceiptE2EEnabledEnv = "SYMMETRY_GOAL_RECEIPT_E2E"
	goalReceiptE2EInputEnv   = "SYMMETRY_GOAL_RECEIPT_E2E_INPUT"

	goalReceiptWrongLeaseToken    = "00000000-0000-4000-8000-000000000001"
	goalReceiptWrongLocalHandleID = "00000000-0000-4000-8000-000000000002"
	goalReceiptWrongBindingID     = "00000000-0000-4000-8000-000000000003"
)

var errGoalReceiptResponseLost = errors.New("simulated response loss after committed HTTP response")

type goalReceiptE2EConfig struct {
	URL                string                     `json:"url"`
	MachineToken       string                     `json:"machine_token"`
	RunID              string                     `json:"run_id"`
	Attach             GoalSessionAttachRequest   `json:"attach"`
	Phase              string                     `json:"phase"`
	ExpectedAttachment *GoalSessionReceipt        `json:"expected_attachment"`
	ExpectedStop       *GoalSessionStoppedReceipt `json:"expected_stop"`
	OutputPath         string                     `json:"output_path"`
}

type goalReceiptE2EObservation struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
}

// goalReceiptE2EOutput deliberately excludes fixture input and HTTP bodies.
// The durable receipts are private local coordination artifacts for the parent
// integration fixture, never test diagnostics.
type goalReceiptE2EOutput struct {
	Phase            string                      `json:"phase"`
	DroppedResponses int                         `json:"dropped_responses"`
	Requests         []goalReceiptE2EObservation `json:"requests"`
	Attachment       *GoalSessionReceipt         `json:"attachment"`
	Stopped          *GoalSessionStoppedReceipt  `json:"stopped"`
}

// goalReceiptE2ETransport observes genuine loopback HTTP responses. When
// configured, it drains and closes one matching response before returning a
// synthetic transport failure, modeling an acknowledgement lost after commit.
type goalReceiptE2ETransport struct {
	base       http.RoundTripper
	dropStatus int

	mu       sync.Mutex
	dropped  int
	requests []goalReceiptE2EObservation
}

func (transport *goalReceiptE2ETransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}

	transport.mu.Lock()
	transport.requests = append(transport.requests, goalReceiptE2EObservation{
		Method: request.Method,
		Path:   request.URL.Path,
		Status: response.StatusCode,
	})
	drop := transport.dropStatus != 0 && transport.dropStatus == response.StatusCode && transport.dropped == 0
	if drop {
		transport.dropped++
	}
	transport.mu.Unlock()

	if !drop {
		return response, nil
	}

	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return nil, errGoalReceiptResponseLost
}

func (transport *goalReceiptE2ETransport) CloseIdleConnections() {
	if closeable, ok := transport.base.(interface{ CloseIdleConnections() }); ok {
		closeable.CloseIdleConnections()
	}
}

func (transport *goalReceiptE2ETransport) snapshot() (int, []goalReceiptE2EObservation) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.dropped, append([]goalReceiptE2EObservation(nil), transport.requests...)
}

func TestGoalSessionReceiptHTTP(t *testing.T) {
	if os.Getenv(goalReceiptE2EEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_GOAL_RECEIPT_E2E=1 to run the private receipt protocol fixture")
	}

	inputPath := os.Getenv(goalReceiptE2EInputEnv)
	if inputPath == "" || !filepath.IsAbs(inputPath) {
		t.Fatal("SYMMETRY_GOAL_RECEIPT_E2E_INPUT must be an absolute private JSON configuration path")
	}
	configuration, err := loadGoalReceiptE2EConfig(inputPath)
	if err != nil {
		t.Fatal("load private receipt protocol configuration")
	}
	if err := validateGoalReceiptE2EConfig(configuration); err != nil {
		t.Fatal("invalid private receipt protocol configuration")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var output goalReceiptE2EOutput
	switch configuration.Phase {
	case "attach_lost_ack":
		output = runGoalReceiptAttachLostAck(t, ctx, configuration)
	case "attach_recovery":
		output = runGoalReceiptAttachRecovery(t, ctx, configuration)
	case "stop_lost_ack":
		output = runGoalReceiptStopLostAck(t, ctx, configuration)
	case "stop_recovery":
		output = runGoalReceiptStopRecovery(t, ctx, configuration)
	default:
		t.Fatal("unsupported receipt protocol phase")
	}

	writeGoalReceiptE2EOutput(t, configuration.OutputPath, output)
}

func loadGoalReceiptE2EConfig(path string) (goalReceiptE2EConfig, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return goalReceiptE2EConfig{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var configuration goalReceiptE2EConfig
	if err := decoder.Decode(&configuration); err != nil {
		return goalReceiptE2EConfig{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return goalReceiptE2EConfig{}, errors.New("configuration contains multiple JSON values")
		}
		return goalReceiptE2EConfig{}, err
	}
	return configuration, nil
}

func validateGoalReceiptE2EConfig(configuration goalReceiptE2EConfig) error {
	if strings.TrimSpace(configuration.MachineToken) == "" || strings.TrimSpace(configuration.OutputPath) == "" {
		return errors.New("missing private fixture configuration")
	}
	parsed, err := url.ParseRequestURI(configuration.URL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || !goalReceiptLoopbackHost(parsed.Hostname()) {
		return errors.New("fixture URL must be loopback HTTP")
	}
	if err := validateGoalSessionAttach(configuration.RunID, configuration.Attach); err != nil {
		return err
	}

	switch configuration.Phase {
	case "attach_lost_ack":
		return nil
	case "attach_recovery", "stop_lost_ack":
		if configuration.ExpectedAttachment == nil {
			return errors.New("fixture requires expected attachment receipt")
		}
	case "stop_recovery":
		if configuration.ExpectedAttachment == nil || configuration.ExpectedStop == nil {
			return errors.New("fixture requires expected receipts")
		}
	default:
		return errors.New("unsupported receipt protocol phase")
	}
	return nil
}

func goalReceiptLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func runGoalReceiptAttachLostAck(t *testing.T, ctx context.Context, configuration goalReceiptE2EConfig) goalReceiptE2EOutput {
	t.Helper()
	transport := newGoalReceiptE2ETransport(http.StatusCreated)
	client, closeClient := newGoalReceiptE2EClient(t, configuration, transport)
	defer closeClient()

	if _, err := client.AttachHarnessSession(ctx, configuration.RunID, configuration.Attach); !errors.Is(err, errGoalReceiptResponseLost) {
		t.Fatal("attach lost-ack call did not return the simulated transport error")
	}
	return goalReceiptE2EOutputFor(t, configuration.Phase, transport, []goalReceiptE2EObservation{{
		Method: http.MethodPut,
		Path:   goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session"),
		Status: http.StatusCreated,
	}})
}

func runGoalReceiptAttachRecovery(t *testing.T, ctx context.Context, configuration goalReceiptE2EConfig) goalReceiptE2EOutput {
	t.Helper()
	transport := newGoalReceiptE2ETransport(0)
	client, closeClient := newGoalReceiptE2EClient(t, configuration, transport)
	defer closeClient()

	attachment, err := client.FetchHarnessSessionAttachment(ctx, configuration.RunID, configuration.Attach.Fence)
	if err != nil {
		t.Fatal("fetch immutable attachment receipt")
	}
	requireGoalReceiptEqual(t, attachment, *configuration.ExpectedAttachment)

	replayed, err := client.AttachHarnessSession(ctx, configuration.RunID, configuration.Attach)
	if err != nil {
		t.Fatal("replay exact attachment request")
	}
	requireGoalReceiptEqual(t, replayed, *configuration.ExpectedAttachment)

	if _, err := client.FetchRunContext(ctx, configuration.RunID, configuration.Attach.Fence); err != nil {
		t.Fatal("fetch and validate production run context")
	}

	wrongLease := configuration.Attach
	wrongLease.LeaseToken = goalReceiptMutationUUID(t, configuration.Attach.LeaseToken, goalReceiptWrongLeaseToken)
	_, err = client.AttachHarnessSession(ctx, configuration.RunID, wrongLease)
	requireGoalReceiptHTTPStatus(t, err, http.StatusConflict, OwnershipLost)

	wrongHandle := configuration.Attach
	wrongHandle.LocalHandleID = goalReceiptMutationUUID(t, configuration.Attach.LocalHandleID, goalReceiptWrongLocalHandleID)
	_, err = client.AttachHarnessSession(ctx, configuration.RunID, wrongHandle)
	requireGoalReceiptHTTPStatus(t, err, http.StatusConflict, IdempotencyConflict)

	output := goalReceiptE2EOutputFor(t, configuration.Phase, transport, []goalReceiptE2EObservation{
		{Method: http.MethodGet, Path: goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session"), Status: http.StatusOK},
		{Method: http.MethodPut, Path: goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session"), Status: http.StatusOK},
		{Method: http.MethodGet, Path: goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/context"), Status: http.StatusOK},
		{Method: http.MethodPut, Path: goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session"), Status: http.StatusConflict},
		{Method: http.MethodPut, Path: goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session"), Status: http.StatusConflict},
	})
	output.Attachment = &replayed
	return output
}

func runGoalReceiptStopLostAck(t *testing.T, ctx context.Context, configuration goalReceiptE2EConfig) goalReceiptE2EOutput {
	t.Helper()
	transport := newGoalReceiptE2ETransport(http.StatusCreated)
	client, closeClient := newGoalReceiptE2EClient(t, configuration, transport)
	defer closeClient()

	request := goalReceiptStopRequest(t, configuration)
	if _, err := client.MarkHarnessSessionStopped(ctx, configuration.RunID, request); !errors.Is(err, errGoalReceiptResponseLost) {
		t.Fatal("stop lost-ack call did not return the simulated transport error")
	}
	return goalReceiptE2EOutputFor(t, configuration.Phase, transport, []goalReceiptE2EObservation{{
		Method: http.MethodPut,
		Path:   goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session/stopped"),
		Status: http.StatusCreated,
	}})
}

func runGoalReceiptStopRecovery(t *testing.T, ctx context.Context, configuration goalReceiptE2EConfig) goalReceiptE2EOutput {
	t.Helper()
	transport := newGoalReceiptE2ETransport(0)
	client, closeClient := newGoalReceiptE2EClient(t, configuration, transport)
	defer closeClient()

	request := goalReceiptStopRequest(t, configuration)
	stopped, err := client.MarkHarnessSessionStopped(ctx, configuration.RunID, request)
	if err != nil {
		t.Fatal("replay exact stop request")
	}
	if !reflect.DeepEqual(stopped, *configuration.ExpectedStop) {
		t.Fatal("stop replay receipt differs from authoritative expected receipt")
	}

	attachment, err := client.FetchHarnessSessionAttachment(ctx, configuration.RunID, configuration.Attach.Fence)
	if err != nil {
		t.Fatal("fetch immutable attachment receipt after stop")
	}
	requireGoalReceiptEqual(t, attachment, *configuration.ExpectedAttachment)

	wrongBinding := request
	wrongBinding.BindingID = goalReceiptMutationUUID(t, request.BindingID, goalReceiptWrongBindingID)
	_, err = client.MarkHarnessSessionStopped(ctx, configuration.RunID, wrongBinding)
	requireGoalReceiptHTTPStatus(t, err, http.StatusConflict, OwnershipLost)

	output := goalReceiptE2EOutputFor(t, configuration.Phase, transport, []goalReceiptE2EObservation{
		{Method: http.MethodPut, Path: goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session/stopped"), Status: http.StatusOK},
		{Method: http.MethodGet, Path: goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session"), Status: http.StatusOK},
		{Method: http.MethodPut, Path: goalReceiptE2EPath(t, configuration.URL, "v1/runs/"+configuration.RunID+"/session/stopped"), Status: http.StatusConflict},
	})
	output.Attachment = &attachment
	output.Stopped = &stopped
	return output
}

func newGoalReceiptE2ETransport(dropStatus int) *goalReceiptE2ETransport {
	return &goalReceiptE2ETransport{
		base:       http.DefaultTransport.(*http.Transport).Clone(),
		dropStatus: dropStatus,
	}
}

func newGoalReceiptE2EClient(t *testing.T, configuration goalReceiptE2EConfig, transport *goalReceiptE2ETransport) (*Client, func()) {
	t.Helper()
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	client, err := NewClient(configuration.URL, configuration.MachineToken, httpClient)
	if err != nil {
		t.Fatal("create receipt protocol control client")
	}
	return client, httpClient.CloseIdleConnections
}

func goalReceiptStopRequest(t *testing.T, configuration goalReceiptE2EConfig) GoalSessionStoppedRequest {
	t.Helper()
	attachment := configuration.ExpectedAttachment
	if attachment == nil {
		t.Fatal("missing attachment receipt required for stop request")
	}
	request := GoalSessionStoppedRequest{
		Fence:         configuration.Attach.Fence,
		SessionID:     attachment.SessionID,
		LocalHandleID: attachment.LocalHandleID,
		BindingID:     attachment.BindingID,
	}
	if err := validateGoalSessionStopped(configuration.RunID, request); err != nil {
		t.Fatal("expected attachment cannot produce a valid stop request")
	}
	return request
}

func goalReceiptMutationUUID(t *testing.T, original, replacement string) string {
	t.Helper()
	if original == replacement {
		t.Fatal("fixture identity conflicts with deterministic mutation UUID")
	}
	return replacement
}

func requireGoalReceiptEqual(t *testing.T, actual, expected GoalSessionReceipt) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("attachment receipt differs from authoritative expected receipt")
	}
}

func requireGoalReceiptHTTPStatus(t *testing.T, err error, want int, code ErrorCode) {
	t.Helper()
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != want || apiError.Code != code {
		t.Fatalf("expected HTTP %d rejection with code %s", want, code)
	}
}

func goalReceiptE2EOutputFor(t *testing.T, phase string, transport *goalReceiptE2ETransport, expected []goalReceiptE2EObservation) goalReceiptE2EOutput {
	t.Helper()
	dropped, requests := transport.snapshot()
	if !reflect.DeepEqual(requests, expected) {
		t.Fatal("receipt protocol HTTP observations did not match the required sequence")
	}
	if (phase == "attach_lost_ack" || phase == "stop_lost_ack") && dropped != 1 {
		t.Fatal("lost-ack phase did not drop exactly one response")
	}
	if phase != "attach_lost_ack" && phase != "stop_lost_ack" && dropped != 0 {
		t.Fatal("recovery phase unexpectedly dropped a response")
	}
	return goalReceiptE2EOutput{Phase: phase, DroppedResponses: dropped, Requests: requests}
}

func goalReceiptE2EPath(t *testing.T, baseURL, endpoint string) string {
	t.Helper()
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil {
		t.Fatal("parse validated loopback fixture URL")
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if basePath == "" {
		basePath = "/api"
	}
	return basePath + "/" + endpoint
}

func writeGoalReceiptE2EOutput(t *testing.T, path string, output goalReceiptE2EOutput) {
	t.Helper()
	contents, err := json.Marshal(output)
	if err != nil {
		t.Fatal("encode sanitized receipt protocol output")
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal("write sanitized receipt protocol output")
	}
}
