package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/contracts"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	providerActionResourceID = "11111111-1111-4111-8111-111111111111"
	providerActionWorkItemID = "22222222-2222-4222-8222-222222222222"
	providerActionID         = "33333333-3333-4333-8333-333333333333"
)

func TestClientExecuteProviderActionUsesProviderTokenAndMappedPath(t *testing.T) {
	const machineToken = "machine-secret"
	const providerToken = "provider-secret"
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if request.URL.Path != "/control-root/v1/provider-actions" {
			t.Errorf("path = %q, want /control-root/v1/provider-actions", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+providerToken {
			t.Errorf("Authorization = %q, want provider bearer", got)
		}
		if got := request.Header.Get("Idempotency-Key"); got != "" {
			t.Errorf("Idempotency-Key = %q, want absent", got)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		if got := string(decoded["action_id"]); got != `"`+providerActionID+`"` {
			t.Errorf("action_id = %s", got)
		}
		if got := string(decoded["resource_id"]); got != `"`+providerActionResourceID+`"` {
			t.Errorf("resource_id = %s", got)
		}
		if got := string(decoded["operation"]); got != `"change.upsert"` {
			t.Errorf("operation = %s", got)
		}
		if got := string(decoded["input"]); got != `{"title":"ship"}` {
			t.Errorf("input = %s", got)
		}
		if strings.Contains(string(body), machineToken) {
			t.Errorf("machine token leaked into provider request body")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, providerActionSuccessJSON())
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/control-root", machineToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ExecuteProviderAction(
		context.Background(),
		validProviderActionAccess(providerToken, "change.upsert"),
		providerActionID,
		providerActionResourceID,
		"change.upsert",
		json.RawMessage(`{"title":"ship"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != ProviderActionSucceeded || response.WorkItemID != providerActionWorkItemID {
		t.Fatalf("response = %#v", response)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want one", calls.Load())
	}
}

func TestClientExecuteProviderActionPreservesSuccessfulAndUnknownResults(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		body      string
		want      ProviderActionOutcome
		check     func(*testing.T, ProviderActionResponse)
	}{
		{
			name:      "resource sync success",
			operation: "resource.sync",
			body:      fmt.Sprintf(`{"operation":"resource.sync","resource_id":%q,"projected":true,"resource":{"provider":"github","status":"healthy"},"readback":"applied"}`, providerActionResourceID),
			want:      ProviderActionSucceeded,
			check: func(t *testing.T, response ProviderActionResponse) {
				if len(response.Resource) == 0 || len(response.Delivery) != 0 || string(response.Readback) != `"applied"` {
					t.Fatalf("resource sync response = %#v", response)
				}
			},
		},
		{
			name:      "unknown replay result",
			operation: "change.upsert",
			body:      fmt.Sprintf(`{"operation":"change.upsert","resource_id":%q,"outcome":"unknown","projected":false,"readback_status":"unconfirmed","readback":{"operation":"resource.sync","projected":false}}`, providerActionResourceID),
			want:      ProviderActionUnknown,
			check: func(t *testing.T, response ProviderActionResponse) {
				if response.ReadbackStatus != "unconfirmed" || len(response.Readback) == 0 {
					t.Fatalf("unknown response = %#v", response)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, err := NewClient(server.URL+"/api", "machine-secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			input := json.RawMessage(`{"title":"ship"}`)
			if test.operation == "resource.sync" {
				input = json.RawMessage(`{}`)
			}
			response, err := client.ExecuteProviderAction(context.Background(), validProviderActionAccess("provider-secret", test.operation), providerActionID, providerActionResourceID, test.operation, input)
			if err != nil {
				t.Fatal(err)
			}
			if response.Outcome != test.want || string(response.Result) != test.body {
				t.Fatalf("outcome/result = %q/%s, want %q/%s", response.Outcome, response.Result, test.want, test.body)
			}
			test.check(t, response)
		})
	}
}

func TestClientExecuteProviderActionPreservesExactReplayResponse(t *testing.T) {
	const body = `{"operation":"change.upsert","resource_id":"11111111-1111-4111-8111-111111111111","work_item_id":"22222222-2222-4222-8222-222222222222","projected":false,"delivery":{"pull_request_url":"https://github.com/acme/symmetry/pull/42","provider_data":{"opaque":true}}}`
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(writer, body)
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/api", "machine-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	access := validProviderActionAccess("provider-secret", "change.upsert")
	first, err := client.ExecuteProviderAction(context.Background(), access, providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"replay"}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.ExecuteProviderAction(context.Background(), access, providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"replay"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Result) != body || string(second.Result) != body || string(first.Result) != string(second.Result) {
		t.Fatalf("replay result changed: first=%s second=%s", first.Result, second.Result)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want one request per caller and no hidden retry", calls.Load())
	}
}

func TestClientExecuteProviderActionRejectsInvalidPathInputAndGrant(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := NewClient(server.URL+"/api", "machine-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		access protocol.ProviderAccess
		action string
		res    string
		op     string
		input  json.RawMessage
	}{
		{name: "absolute path", access: protocol.ProviderAccess{Path: "https://other.example/api/v1/provider-actions", Token: "provider-secret", Grants: validProviderActionAccess("provider-secret", "change.upsert").Grants}, action: providerActionID, res: providerActionResourceID, op: "change.upsert", input: json.RawMessage(`{"title":"x"}`)},
		{name: "invalid action id", access: validProviderActionAccess("provider-secret", "change.upsert"), action: "not-a-uuid", res: providerActionResourceID, op: "change.upsert", input: json.RawMessage(`{"title":"x"}`)},
		{name: "invalid operation", access: validProviderActionAccess("provider-secret", "change.upsert"), action: providerActionID, res: providerActionResourceID, op: "change.delete", input: json.RawMessage(`{"title":"x"}`)},
		{name: "array input", access: validProviderActionAccess("provider-secret", "change.upsert"), action: providerActionID, res: providerActionResourceID, op: "change.upsert", input: json.RawMessage(`[]`)},
		{name: "resource sync nonempty input", access: validProviderActionAccess("provider-secret", "resource.sync"), action: providerActionID, res: providerActionResourceID, op: "resource.sync", input: json.RawMessage(`{"unexpected":true}`)},
		{name: "ungranted operation", access: validProviderActionAccess("provider-secret", "resource.sync"), action: providerActionID, res: providerActionResourceID, op: "change.upsert", input: json.RawMessage(`{"title":"x"}`)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.ExecuteProviderAction(context.Background(), test.access, test.action, test.res, test.op, test.input); err == nil {
				t.Fatal("ExecuteProviderAction() error = nil")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("validation performed %d HTTP calls", calls.Load())
	}
}

func TestClientExecuteProviderActionAllowsAdditiveResponseFields(t *testing.T) {
	body := strings.Replace(providerActionSuccessJSON(), `"projected":true`, `"projected":true,"future_field":{"v":1}`, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, body)
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/api", "machine-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ExecuteProviderAction(context.Background(), validProviderActionAccess("provider-secret", "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Result) != body {
		t.Fatalf("result = %s, want exact additive response %s", response.Result, body)
	}
}

func TestClientExecuteProviderActionRejectsMalformedOversizedAndContradictoryResponses(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "explicit succeeded outcome", body: strings.Replace(providerActionSuccessJSON(), `"operation":`, `"outcome":"succeeded","operation":`, 1), want: "outcome must be unknown"},
		{name: "unknown projected", body: fmt.Sprintf(`{"operation":"change.upsert","resource_id":%q,"outcome":"unknown","projected":true,"readback_status":"unconfirmed","readback":{}}`, providerActionResourceID), want: "must not be projected"},
		{name: "malformed JSON", body: `{`, want: "decode strict provider action response"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { _, _ = io.WriteString(writer, test.body) }))
			defer server.Close()
			client, err := NewClient(server.URL+"/api", "machine-secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ExecuteProviderAction(context.Background(), validProviderActionAccess("provider-secret", "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, providerActionSuccessJSON()+strings.Repeat("x", 100))
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/api", "machine-secret", server.Client(), WithMaxResponseBytes(32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExecuteProviderAction(context.Background(), validProviderActionAccess("provider-secret", "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`)); err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestClientExecuteProviderActionPreservesTypedDefiniteErrorsAndAmbiguousFailures(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantStatus int
	}{
		{name: "definite conflict", status: http.StatusConflict, body: `{"error":{"code":"idempotency_conflict","message":"same action key has different input"}}`, wantStatus: http.StatusConflict},
		{name: "ambiguous provider failure", status: http.StatusBadGateway, body: `{"error":{"code":"provider_failure","message":"provider unavailable"}}`, wantStatus: http.StatusBadGateway},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, err := NewClient(server.URL+"/api", "machine-secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ExecuteProviderAction(context.Background(), validProviderActionAccess("provider-secret", "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`))
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != test.wantStatus {
				t.Fatalf("error = %T %v, want APIError status %d", err, err, test.wantStatus)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls = %d, want one", calls.Load())
			}
		})
	}

	t.Run("transport failure remains non-API ambiguity", func(t *testing.T) {
		transportErr := errors.New("upstream connection failed")
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})}
		client, err := NewClient("https://control.example.test/api", "machine-secret", httpClient)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ExecuteProviderAction(context.Background(), validProviderActionAccess("provider-secret", "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`))
		if err == nil || errors.As(err, new(*APIError)) || !errors.Is(err, transportErr) {
			t.Fatalf("transport error = %T %v, want wrapped non-API transport error", err, err)
		}
	})

	t.Run("deadline remains non-API ambiguity", func(t *testing.T) {
		httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return nil, &url.Error{Op: "Post", URL: request.URL.String(), Err: context.DeadlineExceeded}
		})}
		client, err := NewClient("https://control.example.test/api", "machine-secret", httpClient)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ExecuteProviderAction(context.Background(), validProviderActionAccess("provider-secret", "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`))
		if err == nil || errors.As(err, new(*APIError)) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline error = %T %v, want wrapped non-API deadline error", err, err)
		}
	})
}

func TestClientExecuteProviderActionRedactsProviderTokenFromErrors(t *testing.T) {
	const providerToken = "provider-secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, fmt.Sprintf(`{"error":{"code":"%s","message":"bad %s","details":{"nested":{"%s":"\u0070rovider-secret"}}}}`, providerToken, providerToken, providerToken))
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/api", "machine-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExecuteProviderAction(context.Background(), validProviderActionAccess(providerToken, "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`))
	if err == nil || strings.Contains(fmt.Sprintf("%v", err), providerToken) {
		t.Fatalf("error = %v, provider token leaked", err)
	}
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("redacted API error = %#v", apiError)
	}
	if strings.Contains(string(apiError.Code), providerToken) || strings.Contains(string(apiError.Details), providerToken) || apiError.EvidenceBatchConflictDetails != nil {
		t.Fatalf("redacted API error retained token/cache: %#v", apiError)
	}
	if !strings.Contains(string(apiError.Details), "[REDACTED]") {
		t.Fatalf("redacted API details = %s, want redaction marker", apiError.Details)
	}

	cached := &APIError{
		StatusCode:                   http.StatusConflict,
		Code:                         IdempotencyConflict,
		Details:                      json.RawMessage(`{"items":[{"index":0,"evidence_key":"provider-secret","disposition":"conflict"}]}`),
		EvidenceBatchConflictDetails: &contracts.SymmetryEvidenceBatchConflictDetailsV1{Items: []contracts.ConflictItem{{EvidenceKey: providerToken}}},
	}
	redactedCached := redactProviderActionAPIError(cached, providerToken)
	if redactedCached.EvidenceBatchConflictDetails != nil || strings.Contains(string(redactedCached.Details), providerToken) {
		t.Fatalf("cached API details were not cleared/redacted: %#v", redactedCached)
	}
}

func TestClientExecuteProviderActionRedirectAndCancellationAreSingleAttempt(t *testing.T) {
	t.Run("redirect denied", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.WriteString(writer, providerActionSuccessJSON())
		}))
		defer target.Close()
		redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
		}))
		defer redirect.Close()
		client, err := NewClient(redirect.URL+"/api", "machine-secret", redirect.Client())
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ExecuteProviderAction(context.Background(), validProviderActionAccess("provider-secret", "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`))
		if err == nil || !strings.Contains(err.Error(), "does not allow redirects") || strings.Contains(err.Error(), "provider-secret") {
			t.Fatalf("redirect error = %v", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			close(started)
			select {
			case <-request.Context().Done():
			case <-release:
			}
		}))
		defer server.Close()
		client, err := NewClient(server.URL+"/api", "machine-secret", server.Client())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, callErr := client.ExecuteProviderAction(ctx, validProviderActionAccess("provider-secret", "change.upsert"), providerActionID, providerActionResourceID, "change.upsert", json.RawMessage(`{"title":"x"}`))
			result <- callErr
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("request did not reach server")
		}
		cancel()
		select {
		case err := <-result:
			if err == nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled request did not return")
		}
		close(release)
	})
}

func validProviderActionAccess(token, operation string) protocol.ProviderAccess {
	return protocol.ProviderAccess{
		Path:  providerActionPath,
		Token: token,
		Grants: []protocol.ProviderGrant{{
			ResourceID: providerActionResourceID,
			Provider:   "github",
			Kind:       "repository",
			Operations: []string{operation},
		}},
	}
}

func providerActionSuccessJSON() string {
	return fmt.Sprintf(`{"operation":"change.upsert","resource_id":%q,"work_item_id":%q,"projected":true,"delivery":{"pull_request_url":"https://github.com/acme/symmetry/pull/42"}}`, providerActionResourceID, providerActionWorkItemID)
}
