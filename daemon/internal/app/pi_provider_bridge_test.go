package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const (
	testPiProviderToken    = "provider-token-do-not-leak"
	testPiProviderResource = "00000000-0000-4000-8000-000000000001"
	testPiProviderActionID = "00000000-0000-4000-8000-000000000002"
)

type piProviderActionControlStub struct {
	fakeControl

	mutex sync.Mutex
	calls int
	last  piProviderActionCall
	fn    func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error)
}

type piProviderActionCall struct {
	access    protocol.ProviderAccess
	actionID  string
	resource  string
	operation string
	input     json.RawMessage
}

func (stub *piProviderActionControlStub) ExecuteProviderAction(
	ctx context.Context,
	access protocol.ProviderAccess,
	actionID string,
	resourceID string,
	operation string,
	input json.RawMessage,
) (control.ProviderActionResponse, error) {
	stub.mutex.Lock()
	stub.calls++
	stub.last = piProviderActionCall{
		access:    access,
		actionID:  actionID,
		resource:  resourceID,
		operation: operation,
		input:     append(json.RawMessage(nil), input...),
	}
	fn := stub.fn
	stub.mutex.Unlock()
	if fn == nil {
		return control.ProviderActionResponse{}, nil
	}
	return fn(ctx, access, actionID, resourceID, operation, input)
}

func (stub *piProviderActionControlStub) callCount() int {
	stub.mutex.Lock()
	defer stub.mutex.Unlock()
	return stub.calls
}

func (stub *piProviderActionControlStub) lastCall() piProviderActionCall {
	stub.mutex.Lock()
	defer stub.mutex.Unlock()
	call := stub.last
	call.input = append(json.RawMessage(nil), call.input...)
	call.access = clonePiProviderActionAccess(call.access)
	return call
}

func testPiProviderAccess() protocol.ProviderAccess {
	return protocol.ProviderAccess{
		Path:  piProviderActionPath,
		Token: testPiProviderToken,
		Grants: []protocol.ProviderGrant{{
			ResourceID: testPiProviderResource,
			Provider:   "github",
			Kind:       "repository",
			Operations: []string{"resource.sync", "change.upsert", "change.update"},
		}},
	}
}

func newPiProviderActionTestExecutor(t *testing.T, stub *piProviderActionControlStub, access protocol.ProviderAccess) pi.ExecuteProviderAction {
	t.Helper()
	executor, err := newPiProviderBridgeExecutor(stub, access)
	if err != nil {
		t.Fatalf("newPiProviderBridgeExecutor() error = %v", err)
	}
	return executor
}

func callPiProviderActionExecutor(t *testing.T, executor pi.ExecuteProviderAction, ctx context.Context, actionID string, request pi.ProviderBridgeRequest) pi.ProviderBridgeResponse {
	t.Helper()
	response, err := executor(ctx, actionID, request)
	if err != nil {
		t.Fatalf("provider bridge executor error = %v, want mapped response", err)
	}
	return response
}

func testPiProviderRequest() pi.ProviderBridgeRequest {
	return pi.ProviderBridgeRequest{
		ResourceID: testPiProviderResource,
		Operation:  "change.upsert",
		ActionKey:  "action-key-1",
		Input:      json.RawMessage(`{"title":"change"}`),
	}
}

func TestPiProviderBridgeExecutorForwardsActionAndReturnsExactSuccess(t *testing.T) {
	access := testPiProviderAccess()
	stub := &piProviderActionControlStub{}
	stub.fn = func(_ context.Context, received protocol.ProviderAccess, actionID, resourceID, operation string, input json.RawMessage) (control.ProviderActionResponse, error) {
		if received.Token != testPiProviderToken {
			t.Fatalf("control access token = %q, want original token", received.Token)
		}
		return control.ProviderActionResponse{
			Outcome:    control.ProviderActionSucceeded,
			Result:     json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","projected":true,"work_item_id":"00000000-0000-4000-8000-000000000003","delivery":{"url":"https://example.test/change"}}`),
			Operation:  "change.upsert",
			ResourceID: testPiProviderResource,
			WorkItemID: "00000000-0000-4000-8000-000000000003",
			Projected:  true,
			Delivery:   json.RawMessage(`{"url":"https://example.test/change"}`),
		}, nil
	}

	executor := newPiProviderActionTestExecutor(t, stub, access)
	request := testPiProviderRequest()
	got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, request)
	if got.Outcome != pi.ProviderBridgeOutcomeSucceeded {
		t.Fatalf("outcome = %q, want succeeded", got.Outcome)
	}
	wantResult := `{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","projected":true,"work_item_id":"00000000-0000-4000-8000-000000000003","delivery":{"url":"https://example.test/change"}}`
	if string(got.Result) != wantResult {
		t.Fatalf("result = %s, want exact raw result %s", got.Result, wantResult)
	}
	if got.FailureCode != "" {
		t.Fatalf("failure code = %q, want empty", got.FailureCode)
	}
	call := stub.lastCall()
	if call.actionID != testPiProviderActionID || call.resource != request.ResourceID || call.operation != request.Operation || string(call.input) != string(request.Input) {
		t.Fatalf("forwarded call = %#v, want action/resource/operation/input from request", call)
	}

	access.Grants[0].Operations[0] = "mutated-after-construction"
	second := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, request)
	if second.Outcome != pi.ProviderBridgeOutcomeSucceeded {
		t.Fatalf("second outcome = %q, want succeeded after caller mutation", second.Outcome)
	}
	if stub.lastCall().access.Grants[0].Operations[0] != "resource.sync" {
		t.Fatalf("executor did not retain immutable grant copy: %#v", stub.lastCall().access.Grants)
	}
}

func TestPiProviderBridgeExecutorMapsExplicitUnknownAndPreservesSafeResult(t *testing.T) {
	stub := &piProviderActionControlStub{
		fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
			return control.ProviderActionResponse{
				Outcome:        control.ProviderActionUnknown,
				Result:         json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","outcome":"unknown","projected":false,"readback_status":"unconfirmed","readback":{"status":"pending"}}`),
				Operation:      "change.upsert",
				ResourceID:     testPiProviderResource,
				Projected:      false,
				ReadbackStatus: "unconfirmed",
				Readback:       json.RawMessage(`{"status":"pending"}`),
			}, nil
		},
	}
	executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
	got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if got.Outcome != pi.ProviderBridgeOutcomeUnknown || got.FailureCode != piProviderActionFailureControlUnknown {
		t.Fatalf("unknown mapping = %#v", got)
	}
	if len(got.Result) == 0 || !json.Valid(got.Result) || !bytes.Contains(got.Result, []byte(`"outcome":"unknown"`)) {
		t.Fatalf("unknown result = %s, want exact safe Control result", got.Result)
	}
}

func TestCloneSafePiProviderActionResultUsesSemanticSizeForLegacyEscapes(t *testing.T) {
	semanticPayloadBytes := piProviderActionMaxResultBytes - len(`{"payload":""}`)
	legacy := json.RawMessage(`{"payload":"` + strings.Repeat(`\u003c`, semanticPayloadBytes) + `"}`)
	if len(legacy) <= piProviderActionMaxResultBytes {
		t.Fatalf("legacy result bytes = %d, want escaped representation over raw limit", len(legacy))
	}
	cloned, safe := cloneSafePiProviderActionResult(legacy, "")
	if !safe || string(cloned) != string(legacy) {
		t.Fatalf("legacy semantic result safe=%t cloned bytes=%d, want original escaped result", safe, len(cloned))
	}
}

func TestPiProviderBridgeExecutorValidatesTypedControlResponses(t *testing.T) {
	validSuccess := testPiProviderSuccess()
	validUnknown := control.ProviderActionResponse{
		Outcome:        control.ProviderActionUnknown,
		Result:         json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","outcome":"unknown","projected":false,"readback_status":"unconfirmed","readback":{"status":"pending"}}`),
		Operation:      "change.upsert",
		ResourceID:     testPiProviderResource,
		Projected:      false,
		ReadbackStatus: "unconfirmed",
		Readback:       json.RawMessage(`{"status":"pending"}`),
	}
	tests := []struct {
		name     string
		response control.ProviderActionResponse
		want     pi.ProviderBridgeOutcome
		failure  string
	}{
		{name: "valid success", response: validSuccess, want: pi.ProviderBridgeOutcomeSucceeded},
		{name: "valid unknown", response: validUnknown, want: pi.ProviderBridgeOutcomeUnknown, failure: piProviderActionFailureControlUnknown},
		{name: "succeeded empty object", response: control.ProviderActionResponse{Outcome: control.ProviderActionSucceeded, Result: json.RawMessage(`{}`)}, want: pi.ProviderBridgeOutcomeUnknown, failure: piProviderActionFailureControlResult},
		{name: "mismatched operation", response: func() control.ProviderActionResponse {
			value := validSuccess
			value.Operation = "change.update"
			return value
		}(), want: pi.ProviderBridgeOutcomeUnknown, failure: piProviderActionFailureControlResult},
		{name: "mismatched resource", response: func() control.ProviderActionResponse {
			value := validSuccess
			value.ResourceID = "00000000-0000-4000-8000-000000000099"
			return value
		}(), want: pi.ProviderBridgeOutcomeUnknown, failure: piProviderActionFailureControlResult},
		{name: "contradictory typed outcome", response: func() control.ProviderActionResponse {
			value := validSuccess
			value.Outcome = control.ProviderActionUnknown
			return value
		}(), want: pi.ProviderBridgeOutcomeUnknown, failure: piProviderActionFailureControlResult},
		{name: "invalid unknown", response: control.ProviderActionResponse{
			Outcome:        control.ProviderActionUnknown,
			Result:         json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","outcome":"unknown","projected":true,"readback_status":"unconfirmed","readback":{}}`),
			Operation:      "change.upsert",
			ResourceID:     testPiProviderResource,
			Projected:      true,
			ReadbackStatus: "unconfirmed",
			Readback:       json.RawMessage(`{}`),
		}, want: pi.ProviderBridgeOutcomeUnknown, failure: piProviderActionFailureControlResult},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
				return test.response, nil
			}}
			executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
			got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
			if got.Outcome != test.want || got.FailureCode != test.failure {
				t.Fatalf("mapping = %#v, want %s/%q", got, test.want, test.failure)
			}
			if test.failure == piProviderActionFailureControlResult && len(got.Result) != 0 {
				t.Fatalf("unsafe mapping retained result: %s", got.Result)
			}
		})
	}
}

func TestPiProviderBridgeExecutorMapsAllDefiniteHTTP4xxToFailed(t *testing.T) {
	for status := http.StatusBadRequest; status < http.StatusInternalServerError; status++ {
		if status == http.StatusConflict {
			continue
		}
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			stub := &piProviderActionControlStub{
				fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
					return control.ProviderActionResponse{}, &control.APIError{
						StatusCode: status,
						Code:       control.InvalidRequest,
						Message:    "definite rejection " + testPiProviderToken,
					}
				},
			}
			executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
			got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
			wantCode := "control_rejected_" + fmt.Sprint(status)
			if got.Outcome != pi.ProviderBridgeOutcomeFailed || got.FailureCode != wantCode || len(got.Result) != 0 {
				t.Fatalf("status %d mapping = %#v, want failed/%q without result", status, got, wantCode)
			}
			if strings.Contains(fmt.Sprintf("%#v", got), testPiProviderToken) {
				t.Fatalf("status %d mapping leaked provider token: %#v", status, got)
			}
			if stub.callCount() != 1 {
				t.Fatalf("status %d call count = %d, want one call and no retry", status, stub.callCount())
			}
		})
	}
}

func TestPiProviderBridgeExecutorMapsActiveControlIntentToUnknown(t *testing.T) {
	stub := &piProviderActionControlStub{
		fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
			return control.ProviderActionResponse{}, &control.APIError{
				StatusCode: http.StatusConflict,
				Code:       control.StateConflict,
				Message:    "active provider dispatch " + testPiProviderToken,
			}
		},
	}
	executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
	got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if got.Outcome != pi.ProviderBridgeOutcomeUnknown || got.FailureCode != piProviderActionFailureControlInFlight || len(got.Result) != 0 {
		t.Fatalf("state_conflict mapping = %#v", got)
	}
	if stub.callCount() != 1 || strings.Contains(fmt.Sprintf("%#v", got), testPiProviderToken) {
		t.Fatalf("state_conflict call/result = calls:%d result:%#v", stub.callCount(), got)
	}
}

func TestPiProviderBridgeExecutorMapsDefinitiveConflictToFailed(t *testing.T) {
	for _, code := range []control.ErrorCode{control.IdempotencyConflict, control.OwnershipLost} {
		t.Run(string(code), func(t *testing.T) {
			stub := &piProviderActionControlStub{
				fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
					return control.ProviderActionResponse{}, &control.APIError{StatusCode: http.StatusConflict, Code: code}
				},
			}
			executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
			got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
			if got.Outcome != pi.ProviderBridgeOutcomeFailed || got.FailureCode != "control_rejected_409" {
				t.Fatalf("%s mapping = %#v", code, got)
			}
		})
	}
}

func TestPiProviderBridgeExecutorMapsAmbiguousFailuresToUnknownWithoutRetry(t *testing.T) {
	tests := []struct {
		name string
		fn   func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error)
		want string
	}{
		{
			name: "server_error",
			fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
				return control.ProviderActionResponse{}, &control.APIError{StatusCode: http.StatusBadGateway, Code: control.ServiceUnavailable, Message: testPiProviderToken}
			},
			want: piProviderActionFailureControlServer,
		},
		{
			name: "transport_error",
			fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
				return control.ProviderActionResponse{}, errors.New("transport failed with " + testPiProviderToken)
			},
			want: piProviderActionFailureControlTransport,
		},
		{
			name: "panic",
			fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
				panic("panic contains " + testPiProviderToken)
			},
			want: piProviderActionFailureControlPanic,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &piProviderActionControlStub{fn: test.fn}
			executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
			got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
			if got.Outcome != pi.ProviderBridgeOutcomeUnknown || got.FailureCode != test.want {
				t.Fatalf("mapping = %#v, want unknown/%q", got, test.want)
			}
			if stub.callCount() != 1 {
				t.Fatalf("call count = %d, want one call and no retry", stub.callCount())
			}
			if strings.Contains(fmt.Sprintf("%#v", got), testPiProviderToken) {
				t.Fatalf("mapping leaked provider token: %#v", got)
			}
		})
	}
}

func TestPiProviderBridgeExecutorMapsResponseErrorsToUnknown(t *testing.T) {
	tests := []struct {
		name        string
		body        []byte
		maxResponse int64
	}{
		{name: "malformed", body: []byte(`{"not":`), maxResponse: 0},
		{name: "oversized", body: bytes.Repeat([]byte("x"), 65), maxResponse: 32},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write(test.body)
			}))
			t.Cleanup(server.Close)
			options := []control.Option{}
			if test.maxResponse > 0 {
				options = append(options, control.WithMaxResponseBytes(test.maxResponse))
			}
			client, err := control.NewClient(server.URL+"/api", "machine-token", server.Client(), options...)
			if err != nil {
				t.Fatalf("control.NewClient() error = %v", err)
			}
			// Use the real optional client for this boundary: ResponseError is
			// deliberately not constructible outside the control package.
			realExecutor, err := newPiProviderBridgeExecutor(client, testPiProviderAccess())
			if err != nil {
				t.Fatalf("newPiProviderBridgeExecutor(real client) error = %v", err)
			}
			got := callPiProviderActionExecutor(t, realExecutor, context.Background(), testPiProviderActionID, testPiProviderRequest())
			if got.Outcome != pi.ProviderBridgeOutcomeUnknown || got.FailureCode != piProviderActionFailureControlResponse {
				t.Fatalf("mapping = %#v, want unknown/%q", got, piProviderActionFailureControlResponse)
			}
		})
	}
}

func TestPiProviderBridgeExecutorHandlesCallerCancellation(t *testing.T) {
	started := make(chan struct{})
	stub := &piProviderActionControlStub{
		fn: func(ctx context.Context, _ protocol.ProviderAccess, _ string, _ string, _ string, _ json.RawMessage) (control.ProviderActionResponse, error) {
			close(started)
			<-ctx.Done()
			return control.ProviderActionResponse{}, ctx.Err()
		},
	}
	executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
	ctx, cancel := context.WithCancel(context.Background())
	type executionResult struct {
		response pi.ProviderBridgeResponse
		err      error
	}
	result := make(chan executionResult, 1)
	go func() {
		response, err := executor(ctx, testPiProviderActionID, testPiProviderRequest())
		result <- executionResult{response: response, err: err}
	}()
	<-started
	cancel()
	completed := <-result
	if completed.err != nil {
		t.Fatalf("canceled executor error = %v, want mapped response", completed.err)
	}
	got := completed.response
	if got.Outcome != pi.ProviderBridgeOutcomeUnknown || got.FailureCode != piProviderActionFailureControlCanceled {
		t.Fatalf("canceled mapping = %#v, want unknown/%q", got, piProviderActionFailureControlCanceled)
	}
	if stub.callCount() != 1 {
		t.Fatalf("call count = %d, want one dispatched call", stub.callCount())
	}

	preCanceled, cancelBefore := context.WithCancel(context.Background())
	cancelBefore()
	preCanceledResult := callPiProviderActionExecutor(t, executor, preCanceled, testPiProviderActionID, testPiProviderRequest())
	if preCanceledResult.Outcome != pi.ProviderBridgeOutcomeUnknown || preCanceledResult.FailureCode != piProviderActionFailureControlCanceled {
		t.Fatalf("pre-canceled mapping = %#v, want unknown/%q", preCanceledResult, piProviderActionFailureControlCanceled)
	}
	if stub.callCount() != 1 {
		t.Fatalf("pre-canceled call count = %d, want no dispatch", stub.callCount())
	}
}

func TestPiProviderBridgeExecutorRejectsMissingClientOrInvalidAccess(t *testing.T) {
	validAccess := testPiProviderAccess()
	invalid := []struct {
		name   string
		client ControlAPI
		access protocol.ProviderAccess
	}{
		{name: "nil client", client: nil, access: validAccess},
		{name: "missing optional method", client: &fakeControl{}, access: validAccess},
		{name: "empty token", client: &piProviderActionControlStub{}, access: func() protocol.ProviderAccess { value := validAccess; value.Token = ""; return value }()},
		{name: "wrong path", client: &piProviderActionControlStub{}, access: func() protocol.ProviderAccess {
			value := validAccess
			value.Path = "https://control.example.test/api/v1/provider-actions"
			return value
		}()},
		{name: "missing grants", client: &piProviderActionControlStub{}, access: func() protocol.ProviderAccess { value := validAccess; value.Grants = nil; return value }()},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			executor, err := newPiProviderBridgeExecutor(test.client, test.access)
			if executor != nil || err == nil {
				t.Fatalf("constructor = (%v, %v), want nil executor and error", executor, err)
			}
			if strings.Contains(err.Error(), testPiProviderToken) || strings.Contains(err.Error(), "control.example.test") {
				t.Fatalf("constructor error leaked credential or URL: %v", err)
			}
		})
	}
}

func TestPiProviderBridgeExecutorDoesNotExposeCredentialInSafeResult(t *testing.T) {
	stub := &piProviderActionControlStub{
		fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
			return control.ProviderActionResponse{
				Outcome: control.ProviderActionSucceeded,
				Result:  json.RawMessage(`{"provider_token":"` + testPiProviderToken + `"}`),
			}, nil
		},
	}
	executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
	got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if got.Outcome != pi.ProviderBridgeOutcomeUnknown || got.FailureCode != piProviderActionFailureControlResult || len(got.Result) != 0 {
		t.Fatalf("unsafe result mapping = %#v, want redacted unknown", got)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), testPiProviderToken) {
		t.Fatalf("unsafe result mapping leaked token: %#v", got)
	}
}

func TestJSONResultContainsTokenFailsClosedForLargeNumbersAndTrailingData(t *testing.T) {
	token := testPiProviderToken
	for _, test := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "large number with escaped token", value: `{"big":1e10000,"credential":"\u0070rovider-token-do-not-leak"}`, want: true},
		{name: "safe large number", value: `{"big":1e10000,"safe":true}`, want: false},
		{name: "malformed value", value: `{"credential":"unterminated`, want: true},
		{name: "malformed trailing data", value: `{"safe":true} trailing`, want: true},
		{name: "second JSON value", value: `{"safe":true}{"other":true}`, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := jsonResultContainsToken([]byte(test.value), token); got != test.want {
				t.Fatalf("jsonResultContainsToken() = %t, want %t for %s", got, test.want, test.value)
			}
		})
	}
}

func TestPiProviderBridgeExecutorRedactsEscapedTokenWithLargeNumber(t *testing.T) {
	unsafeResult := json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","projected":true,"work_item_id":"00000000-0000-4000-8000-000000000003","delivery":{"url":"https://example.test/change"},"big":1e10000,"credential":"\u0070rovider-token-do-not-leak"}`)
	if bytes.Contains(unsafeResult, []byte(testPiProviderToken)) {
		t.Fatal("unsafe result contains the raw token and does not require semantic scanning")
	}
	var legacy any
	if err := json.Unmarshal(unsafeResult, &legacy); err == nil {
		t.Fatal("unsafe result does not distinguish number-preserving scanning from default JSON decoding")
	}
	stub := &piProviderActionControlStub{
		fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
			response := testPiProviderSuccess()
			response.Result = unsafeResult
			return response, nil
		},
	}
	executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
	got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if got.Outcome != pi.ProviderBridgeOutcomeUnknown || got.FailureCode != piProviderActionFailureControlResult || len(got.Result) != 0 {
		t.Fatalf("large-number escaped-token mapping = %#v, want redacted unknown", got)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), testPiProviderToken) {
		t.Fatalf("large-number escaped-token mapping leaked token: %#v", got)
	}
}

func TestPiProviderBridgeExecutorRejectsEscapedDuplicateResultMembers(t *testing.T) {
	stub := &piProviderActionControlStub{
		fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
			return control.ProviderActionResponse{
				Outcome: control.ProviderActionSucceeded,
				Result:  json.RawMessage(`{"payload":"\u0070rovider-token-do-not-leak","\u0070ayload":"safe"}`),
			}, nil
		},
	}
	executor := newPiProviderActionTestExecutor(t, stub, testPiProviderAccess())
	got := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if got.Outcome != pi.ProviderBridgeOutcomeUnknown || got.FailureCode != piProviderActionFailureControlResult || len(got.Result) != 0 {
		t.Fatalf("escaped duplicate result mapping = %#v, want redacted unknown", got)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), testPiProviderToken) {
		t.Fatalf("escaped duplicate result leaked provider token: %#v", got)
	}
}

func TestPreparePiProviderBridgeRunsBoundPeerAndPersistsAction(t *testing.T) {
	directory := t.TempDir()
	key := state.RunKey{RunID: "run-provider-app-bridge", Generation: 1}
	store := newPiProviderActionStore(t, directory, key)
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		return testPiProviderSuccess(), nil
	}}
	daemon := &daemon{config: config.Config{StateDir: directory}, store: store, control: stub}
	launch, err := daemon.preparePiProviderBridge(context.Background(), key, harness.KindPi, func() *protocol.ProviderAccess {
		value := testPiProviderAccess()
		return &value
	}())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = launch.Lifecycle.Close(context.Background()) })
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := launch.Lifecycle.BindProcess(os.Getpid(), identity); err != nil {
		t.Fatal(err)
	}
	extension, err := os.ReadFile(launch.ExtensionPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(extension), testPiProviderToken) || strings.Contains(string(extension), launch.Nonce) || strings.Contains(string(extension), launch.URL) {
		t.Fatal("generated extension persisted a provider token or ephemeral bridge secret")
	}

	body := []byte(`{"resource_id":"` + testPiProviderResource + `","operation":"change.upsert","action_key":"tool-call-app","input":{"title":"change"}}`)
	request, err := http.NewRequest(http.MethodPost, launch.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Symmetry-Bridge-Nonce", launch.Nonce)
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("provider bridge status = %d", response.StatusCode)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.ProviderActionIntents) != 1 || journal.ProviderActionIntents[0].Outcome != state.ProviderActionOutcomeSucceeded {
		t.Fatalf("durable provider action = %#v", journal.ProviderActionIntents)
	}
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), testPiProviderToken) || strings.Contains(string(encoded), `"title":"change"`) {
		t.Fatalf("provider token or action input leaked into journal: %s", encoded)
	}
}
