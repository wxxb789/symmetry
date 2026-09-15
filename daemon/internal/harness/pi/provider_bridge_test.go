package pi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const testProviderResourceID = "11111111-1111-4111-8111-111111111111"

func TestProviderBridgeBindsNumericLoopbackAndReturnsFreshNonce(t *testing.T) {
	bridgeOne := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	bridgeTwo := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	endpointOne := startTestProviderBridge(t, bridgeOne)
	endpointTwo := startTestProviderBridge(t, bridgeTwo)

	for index, endpoint := range []ProviderBridgeEndpoint{endpointOne, endpointTwo} {
		parsed, err := url.Parse(endpoint.URL)
		if err != nil {
			t.Fatalf("endpoint %d URL parse: %v", index, err)
		}
		if parsed.Scheme != "http" || parsed.Path != providerBridgePath || parsed.RawQuery != "" {
			t.Fatalf("endpoint %d URL = %q, want loopback bridge route", index, endpoint.URL)
		}
		host, port, err := net.SplitHostPort(parsed.Host)
		if err != nil || host != "127.0.0.1" || port == "" {
			t.Fatalf("endpoint %d host = %q, want numeric IPv4 loopback with port", index, parsed.Host)
		}
		if endpoint.Nonce == "" || len(endpoint.Nonce) != 64 {
			t.Fatalf("endpoint %d nonce length = %d, want 64 hex characters", index, len(endpoint.Nonce))
		}
		if _, err := hex.DecodeString(endpoint.Nonce); err != nil {
			t.Fatalf("endpoint %d nonce is not hex: %v", index, err)
		}
	}
	if endpointOne.Nonce == endpointTwo.Nonce {
		t.Fatal("two bridge sessions returned the same nonce")
	}
}

func TestProviderBridgeConfiguresBoundedHTTPServer(t *testing.T) {
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	startTestProviderBridge(t, bridge)
	bridge.mu.Lock()
	server := bridge.server
	bridge.mu.Unlock()
	if server == nil {
		t.Fatal("provider bridge server is nil after Start")
	}
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, 5*time.Second)
	}
	if server.ReadTimeout != providerBridgeReadTimeout || server.WriteTimeout != providerBridgeWriteTimeout || server.IdleTimeout != providerBridgeIdleTimeout || server.MaxHeaderBytes != providerBridgeMaxHeaderBytes {
		t.Fatalf("server bounds = read:%s write:%s idle:%s headers:%d, want read:%s write:%s idle:%s headers:%d", server.ReadTimeout, server.WriteTimeout, server.IdleTimeout, server.MaxHeaderBytes, providerBridgeReadTimeout, providerBridgeWriteTimeout, providerBridgeIdleTimeout, providerBridgeMaxHeaderBytes)
	}
}

func TestProviderBridgeRejectsMissingOrWrongNonceBeforeExecutor(t *testing.T) {
	var executions atomic.Int32
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	body := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"resource.sync","action_key":"sync-1","input":{}}`)

	for _, nonce := range []string{"", "wrong-nonce"} {
		request, err := http.NewRequest(http.MethodPost, endpoint.URL, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if nonce != "" {
			request.Header.Set(providerBridgeNonceHeader, nonce)
		}
		response := doProviderBridgeRequest(t, request)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("nonce %q status = %d, want %d", nonce, response.StatusCode, http.StatusUnauthorized)
		}
		closeResponseBody(t, response)
	}
	if got := executions.Load(); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}
}

func TestProviderBridgeRejectsMalformedMethodPathContentTypeAndOversizedBody(t *testing.T) {
	var executions atomic.Int32
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	validBody := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"resource.sync","action_key":"malformed-1","input":{}}`)

	tests := []struct {
		name        string
		method      string
		endpoint    string
		contentType string
		body        []byte
		status      int
	}{
		{name: "method", method: http.MethodGet, endpoint: endpoint.URL, contentType: "application/json", body: validBody, status: http.StatusMethodNotAllowed},
		{name: "path", method: http.MethodPost, endpoint: endpoint.URL + "/wrong", contentType: "application/json", body: validBody, status: http.StatusNotFound},
		{name: "content type", method: http.MethodPost, endpoint: endpoint.URL, contentType: "text/plain", body: validBody, status: http.StatusUnsupportedMediaType},
		{name: "oversized", method: http.MethodPost, endpoint: endpoint.URL, contentType: "application/json", body: bytes.Repeat([]byte("x"), providerBridgeMaxBodyBytes+1), status: http.StatusRequestEntityTooLarge},
		{name: "duplicate", method: http.MethodPost, endpoint: endpoint.URL, contentType: "application/json", body: []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","resource_id":"11111111-1111-4111-8111-111111111111","operation":"resource.sync","action_key":"duplicate-1","input":{}}`), status: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, test.endpoint, bytes.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set(providerBridgeNonceHeader, endpoint.Nonce)
			response := doProviderBridgeRequest(t, request)
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.status)
			}
			closeResponseBody(t, response)
		})
	}
	if got := executions.Load(); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}
}

func TestProviderBridgeRejectsUngrantableResourceAndOperation(t *testing.T) {
	var executions atomic.Int32
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID: "run-1",
		Grants: []protocol.ProviderGrant{{
			ResourceID: "11111111-1111-4111-8111-111111111111",
			Provider:   "github",
			Kind:       "repository",
			Operations: []string{"resource.sync"},
		}},
		Execute: func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			executions.Add(1)
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewProviderBridge() error = %v", err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	endpoint := startTestProviderBridge(t, bridge)

	tests := []string{
		`{"resource_id":"22222222-2222-4222-8222-222222222222","operation":"resource.sync","action_key":"grant-1","input":{}}`,
		`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.update","action_key":"grant-2","input":{"title":"x"}}`,
		`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"not-supported","action_key":"grant-3","input":{}}`,
	}
	for _, body := range tests {
		request := newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, []byte(body))
		response := doProviderBridgeRequest(t, request)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want %d", body, response.StatusCode, http.StatusBadRequest)
		}
		closeResponseBody(t, response)
	}
	if got := executions.Load(); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}
}

func TestProviderBridgeDerivesStableActionIDAndReplaysExactBody(t *testing.T) {
	var executions atomic.Int32
	var actionID string
	var received ProviderBridgeRequest
	bridge := newTestProviderBridge(t, func(_ context.Context, gotActionID string, gotRequest ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		actionID = gotActionID
		received = gotRequest
		return ProviderBridgeResponse{
			Outcome: ProviderBridgeOutcomeSucceeded,
			Result:  json.RawMessage(`{"accepted":true,"value":1}`),
		}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	firstBody := []byte(`{"input":{"z":[2,1],"a":"x"},"action_key":"stable-1","operation":"change.upsert","resource_id":"11111111-1111-4111-8111-111111111111"}`)
	secondBody := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.upsert","action_key":"stable-1","input":{"a":"x","z":[2,1]}}`)

	first := doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, firstBody))
	firstPayload := decodeProviderBridgePayload(t, first)
	second := doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, secondBody))
	secondPayload := decodeProviderBridgePayload(t, second)

	if got := executions.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
	if actionID == "" || firstPayload.ActionID != actionID || secondPayload.ActionID != actionID {
		t.Fatalf("action IDs = executor %q, first %q, second %q", actionID, firstPayload.ActionID, secondPayload.ActionID)
	}
	wantID := expectedProviderBridgeActionID("run-1", received)
	if actionID != wantID {
		t.Fatalf("action ID = %q, want %q", actionID, wantID)
	}
	if firstPayload.Outcome != ProviderBridgeOutcomeSucceeded || secondPayload.Outcome != ProviderBridgeOutcomeSucceeded {
		t.Fatalf("replayed outcomes = %q and %q", firstPayload.Outcome, secondPayload.Outcome)
	}
	if !bytes.Equal(firstPayload.Result, secondPayload.Result) || !bytes.Equal(firstPayload.Result, json.RawMessage(`{"accepted":true,"value":1}`)) {
		t.Fatalf("replayed result changed: first=%s second=%s", firstPayload.Result, secondPayload.Result)
	}
}

func TestProviderBridgeRejectsSameActionKeyWithChangedBodyWithoutExecutor(t *testing.T) {
	var executions atomic.Int32
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	first := doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.upsert","action_key":"conflict-1","input":{"title":"one"}}`)))
	if first.StatusCode != http.StatusOK {
		closeResponseBody(t, first)
		t.Fatalf("first status = %d, want %d", first.StatusCode, http.StatusOK)
	}
	closeResponseBody(t, first)
	conflict := doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.upsert","action_key":"conflict-1","input":{"title":"two"}}`)))
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status = %d, want %d", conflict.StatusCode, http.StatusConflict)
	}
	closeResponseBody(t, conflict)
	if got := executions.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
}

func TestProviderBridgeConcurrentSameActionExecutesOnce(t *testing.T) {
	var executions atomic.Int32
	started := make(chan struct{})
	waiterReached := make(chan struct{})
	release := make(chan struct{})
	bridge := newTestProviderBridge(t, func(ctx context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		if executions.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded, Result: json.RawMessage(`{"once":true}`)}, nil
		case <-ctx.Done():
			return ProviderBridgeResponse{}, ctx.Err()
		}
	})
	bridge.onReplayWait = func() {
		select {
		case <-waiterReached:
		default:
			close(waiterReached)
		}
	}
	endpoint := startTestProviderBridge(t, bridge)
	body := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.upsert","action_key":"single-flight-1","input":{"title":"same"}}`)

	responses := make(chan *http.Response, 2)
	client := &http.Client{}
	request := func() {
		req, err := http.NewRequest(http.MethodPost, endpoint.URL, bytes.NewReader(body))
		if err != nil {
			responses <- &http.Response{StatusCode: 599, Body: io.NopCloser(strings.NewReader(err.Error()))}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(providerBridgeNonceHeader, endpoint.Nonce)
		response, err := client.Do(req)
		if err != nil {
			responses <- &http.Response{StatusCode: 598, Body: io.NopCloser(strings.NewReader(err.Error()))}
			return
		}
		responses <- response
	}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		request()
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not start")
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		request()
	}()
	select {
	case <-waiterReached:
	case <-time.After(5 * time.Second):
		t.Fatal("second request did not reach the existing replay entry")
	}
	close(release)
	workers.Wait()
	close(responses)
	for response := range responses {
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			t.Fatalf("concurrent response status = %d body=%s", response.StatusCode, body)
		}
		closeResponseBody(t, response)
	}
	if got := executions.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
}

func TestProviderBridgeCanceledWaiterDoesNotOwnExecutor(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	waiterReached := make(chan struct{})
	bridge := newTestProviderBridge(t, func(ctx context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		close(started)
		select {
		case <-release:
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
		case <-ctx.Done():
			return ProviderBridgeResponse{}, ctx.Err()
		}
	})
	bridge.onReplayWait = func() { close(waiterReached) }
	startTestProviderBridge(t, bridge)
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "canceled-waiter", Input: json.RawMessage(`{"title":"same"}`)}
	canonical := []byte(`{"action_key":"canceled-waiter","input":{"title":"same"},"operation":"change.upsert","resource_id":"11111111-1111-4111-8111-111111111111"}`)
	ownerDone := make(chan error, 1)
	go func() {
		_, err := bridge.executeRequest(context.Background(), request, canonical, "owner-action")
		ownerDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("owner executor did not start")
	}
	waiterContext, cancelWaiter := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := bridge.executeRequest(waiterContext, request, canonical, "waiter-action")
		waiterDone <- err
	}()
	select {
	case <-waiterReached:
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not reach replay entry")
	}
	cancelWaiter()
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled waiter did not return")
	}
	close(release)
	select {
	case err := <-ownerDone:
		if err != nil {
			t.Fatalf("owner error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not finish")
	}
}

func TestProviderBridgeBoundsReplayWaitersAndReclaimsCanceledWaiter(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	allWaitersReached := make(chan struct{})
	var waiterCalls atomic.Int32
	bridge := newTestProviderBridge(t, func(ctx context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		close(started)
		select {
		case <-release:
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
		case <-ctx.Done():
			return ProviderBridgeResponse{}, ctx.Err()
		}
	})
	bridge.onReplayWait = func() {
		if waiterCalls.Add(1) == providerBridgeMaxReplayWaiters {
			close(allWaitersReached)
		}
	}
	startTestProviderBridge(t, bridge)
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "waiter-cap", Input: json.RawMessage(`{"title":"same"}`)}
	canonical := []byte(`{"action_key":"waiter-cap","input":{"title":"same"}}`)
	ownerDone := make(chan error, 1)
	go func() {
		_, err := bridge.executeRequest(context.Background(), request, canonical, "waiter-owner")
		ownerDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("waiter-cap owner did not start")
	}
	waiterContexts := make([]context.Context, providerBridgeMaxReplayWaiters)
	waiterCancels := make([]context.CancelFunc, providerBridgeMaxReplayWaiters)
	waiterDone := make([]chan error, providerBridgeMaxReplayWaiters)
	for index := range waiterContexts {
		waiterContexts[index], waiterCancels[index] = context.WithCancel(context.Background())
		waiterDone[index] = make(chan error, 1)
		go func(index int) {
			_, err := bridge.executeRequest(waiterContexts[index], request, canonical, fmt.Sprintf("waiter-%d", index))
			waiterDone[index] <- err
		}(index)
	}
	select {
	case <-allWaitersReached:
	case <-time.After(5 * time.Second):
		t.Fatal("replay waiter capacity was not reached")
	}
	if _, err := bridge.executeRequest(context.Background(), request, canonical, "waiter-overflow"); !errors.Is(err, errProviderBridgeReplayWaiterCapacity) {
		t.Fatalf("waiter overflow error = %v, want replay waiter capacity error", err)
	}
	waiterCancels[0]()
	select {
	case err := <-waiterDone[0]:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled waiter did not return")
	}
	newWaiterReached := make(chan struct{})
	bridge.onReplayWait = func() { close(newWaiterReached) }
	newWaiterDone := make(chan error, 1)
	go func() {
		_, err := bridge.executeRequest(context.Background(), request, canonical, "waiter-reclaimed")
		newWaiterDone <- err
	}()
	select {
	case <-newWaiterReached:
	case <-time.After(5 * time.Second):
		t.Fatal("reclaimed waiter did not enter replay wait")
	}
	close(release)
	for index := 1; index < len(waiterDone); index++ {
		select {
		case err := <-waiterDone[index]:
			if err != nil {
				t.Fatalf("waiter %d error = %v", index, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("waiter %d did not finish", index)
		}
	}
	select {
	case err := <-newWaiterDone:
		if err != nil {
			t.Fatalf("reclaimed waiter error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reclaimed waiter did not finish")
	}
	select {
	case err := <-ownerDone:
		if err != nil {
			t.Fatalf("waiter-cap owner error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter-cap owner did not finish")
	}
	bridge.mu.Lock()
	remaining := bridge.replayWaiters
	bridge.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("replay waiters after completion = %d, want 0", remaining)
	}
}

func TestProviderBridgeBoundsReplayEntriesWithoutEviction(t *testing.T) {
	var executions atomic.Int32
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeUnknown, FailureCode: "confirmed_unknown"}, nil
	})
	startTestProviderBridge(t, bridge)
	for index := 0; index < providerBridgeMaxReplayEntries; index++ {
		request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: fmt.Sprintf("replay-cap-%d", index), Input: json.RawMessage(`{"title":"same"}`)}
		if _, err := bridge.executeRequest(context.Background(), request, []byte(fmt.Sprintf(`{"key":%d}`, index)), fmt.Sprintf("action-%d", index)); err != nil {
			t.Fatalf("fill replay entry %d: %v", index, err)
		}
	}
	before := executions.Load()
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "replay-cap-overflow", Input: json.RawMessage(`{"title":"same"}`)}
	if _, err := bridge.executeRequest(context.Background(), request, []byte(`{"key":"overflow"}`), "overflow-action"); !errors.Is(err, errProviderBridgeReplayCapacity) {
		t.Fatalf("overflow error = %v, want replay capacity error", err)
	}
	if got := executions.Load(); got != before {
		t.Fatalf("executor calls after replay capacity rejection = %d, want %d", got, before)
	}
	existing := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "replay-cap-0", Input: json.RawMessage(`{"title":"same"}`)}
	if response, err := bridge.executeRequest(context.Background(), existing, []byte(`{"key":0}`), "different-action-id"); err != nil || response.Outcome != ProviderBridgeOutcomeUnknown {
		t.Fatalf("confirmed replay = (%+v, %v), want stored unknown", response, err)
	}
}

func TestProviderBridgeBoundsActiveExecutionsBeforeExecute(t *testing.T) {
	var executions atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	bridge := newTestProviderBridge(t, func(ctx context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		if executions.Add(1) == providerBridgeMaxActiveExecutions {
			close(started)
		}
		select {
		case <-release:
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
		case <-ctx.Done():
			return ProviderBridgeResponse{}, ctx.Err()
		}
	})
	startTestProviderBridge(t, bridge)
	var workers sync.WaitGroup
	workers.Add(providerBridgeMaxActiveExecutions)
	for index := 0; index < providerBridgeMaxActiveExecutions; index++ {
		index := index
		go func() {
			defer workers.Done()
			request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: fmt.Sprintf("active-cap-%d", index), Input: json.RawMessage(`{"title":"same"}`)}
			if _, err := bridge.executeRequest(context.Background(), request, []byte(fmt.Sprintf(`{"key":%d}`, index)), fmt.Sprintf("active-action-%d", index)); err != nil {
				t.Errorf("active action %d: %v", index, err)
			}
		}()
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("active execution cap was not reached")
	}
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "active-cap-overflow", Input: json.RawMessage(`{"title":"same"}`)}
	if _, err := bridge.executeRequest(context.Background(), request, []byte(`{"key":"overflow"}`), "active-overflow"); !errors.Is(err, errProviderBridgeExecutionCapacity) {
		t.Fatalf("active overflow error = %v, want execution capacity error", err)
	}
	if got := executions.Load(); got != providerBridgeMaxActiveExecutions {
		t.Fatalf("executor calls after active capacity rejection = %d, want %d", got, providerBridgeMaxActiveExecutions)
	}
	close(release)
	workers.Wait()
}

func TestProviderBridgeOversizedResultBecomesReplayedUnknown(t *testing.T) {
	var executions atomic.Int32
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded, Result: bytes.Repeat([]byte("x"), providerBridgeMaxResponseBytes+1)}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	body := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.upsert","action_key":"oversized-result","input":{"title":"same"}}`)
	first := decodeProviderBridgePayload(t, doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body)))
	second := decodeProviderBridgePayload(t, doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body)))
	if first.Outcome != ProviderBridgeOutcomeUnknown || second.Outcome != ProviderBridgeOutcomeUnknown || first.FailureCode != "result_too_large" || second.FailureCode != first.FailureCode {
		t.Fatalf("oversized results = (%+v), (%+v), want replayed result_too_large unknown", first, second)
	}
	if got := executions.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
}

func TestProviderBridgeExecutionDeadlineMapsLateSuccessToUnknown(t *testing.T) {
	started := make(chan struct{})
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:            "run-1",
		Grants:           testProviderGrants(),
		ExecutionTimeout: 25 * time.Millisecond,
		Execute: func(ctx context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			close(started)
			<-ctx.Done()
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewProviderBridge() error = %v", err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	startTestProviderBridge(t, bridge)
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "deadline", Input: json.RawMessage(`{"title":"same"}`)}
	resultDone := make(chan ProviderBridgeResponse, 1)
	go func() {
		result, _ := bridge.executeRequest(context.Background(), request, []byte(`{"action_key":"deadline"}`), "deadline-action")
		resultDone <- result
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("deadline executor did not start")
	}
	select {
	case result := <-resultDone:
		if result.Outcome != ProviderBridgeOutcomeUnknown || result.FailureCode != "execution_deadline_exceeded" {
			t.Fatalf("deadline result = %+v, want unknown execution_deadline_exceeded", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deadline executor did not finish")
	}
}

func TestProviderBridgeRejectsCanceledExecutionBeforeExecute(t *testing.T) {
	var executions atomic.Int32
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	startTestProviderBridge(t, bridge)
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "pre-canceled", Input: json.RawMessage(`{"title":"same"}`)}
	canonical := []byte(`{"action_key":"pre-canceled","input":{"title":"same"}}`)
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bridge.executeRequest(canceledContext, request, canonical, "pre-canceled-action"); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled execute error = %v, want context.Canceled", err)
	}
	if got := executions.Load(); got != 0 {
		t.Fatalf("executor calls for pre-canceled request = %d, want 0", got)
	}
	if _, err := bridge.executeRequest(context.Background(), request, canonical, "retry-after-cancel"); err != nil {
		t.Fatalf("retry after pre-canceled request error = %v", err)
	}
	if got := executions.Load(); got != 1 {
		t.Fatalf("executor calls after retry = %d, want 1", got)
	}
}

func TestProviderBridgeInvalidResultBecomesReplayedUnknown(t *testing.T) {
	var executions atomic.Int32
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded, Result: json.RawMessage(`{"not":`)}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	body := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.upsert","action_key":"invalid-result","input":{"title":"same"}}`)
	first := decodeProviderBridgePayload(t, doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body)))
	second := decodeProviderBridgePayload(t, doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body)))
	if first.Outcome != ProviderBridgeOutcomeUnknown || second.Outcome != ProviderBridgeOutcomeUnknown || first.FailureCode != "invalid_executor_result" || second.FailureCode != first.FailureCode {
		t.Fatalf("invalid results = (%+v), (%+v), want replayed invalid_executor_result unknown", first, second)
	}
	if got := executions.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
}

func TestProviderBridgeResponseInvariantsBecomeReplayedUnknown(t *testing.T) {
	cases := []struct {
		name        string
		response    ProviderBridgeResponse
		failureCode string
	}{
		{name: "succeeded with failure", response: ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded, FailureCode: "contradictory"}, failureCode: "invalid_executor_response"},
		{name: "failed without failure", response: ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeFailed}, failureCode: "invalid_executor_response"},
		{name: "unknown without failure", response: ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeUnknown}, failureCode: "invalid_executor_response"},
		{name: "invalid failure code", response: ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeFailed, FailureCode: "bad\x00code"}, failureCode: "invalid_executor_failure"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var executions atomic.Int32
			bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
				executions.Add(1)
				return test.response, nil
			})
			startTestProviderBridge(t, bridge)
			request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "response-invariant-" + test.name, Input: json.RawMessage(`{"title":"same"}`)}
			canonical := []byte(`{"action_key":"response-invariant","input":{"title":"same"}}`)
			first, err := bridge.executeRequest(context.Background(), request, canonical, "response-invariant-action")
			if err != nil {
				t.Fatalf("first executeRequest error = %v", err)
			}
			second, err := bridge.executeRequest(context.Background(), request, canonical, "response-invariant-replay")
			if err != nil {
				t.Fatalf("replay executeRequest error = %v", err)
			}
			if first.Outcome != ProviderBridgeOutcomeUnknown || second.Outcome != ProviderBridgeOutcomeUnknown || first.FailureCode != test.failureCode || second.FailureCode != test.failureCode {
				t.Fatalf("results = (%+v), (%+v), want replayed unknown %q", first, second, test.failureCode)
			}
			if got := executions.Load(); got != 1 {
				t.Fatalf("executor calls = %d, want 1", got)
			}
		})
	}
}

func TestProviderBridgePreservesUnknownWithoutAutomaticRetry(t *testing.T) {
	var executions atomic.Int32
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		executions.Add(1)
		return ProviderBridgeResponse{
			Outcome:     ProviderBridgeOutcomeUnknown,
			FailureCode: "external_effect_unresolved",
			Result:      json.RawMessage(`{"observed":"unknown"}`),
		}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	body := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.update","action_key":"unknown-1","input":{"title":"uncertain"}}`)
	first := decodeProviderBridgePayload(t, doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body)))
	second := decodeProviderBridgePayload(t, doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body)))
	if first.Outcome != ProviderBridgeOutcomeUnknown || second.Outcome != ProviderBridgeOutcomeUnknown {
		t.Fatalf("outcomes = %q, %q, want unknown", first.Outcome, second.Outcome)
	}
	if first.FailureCode != "external_effect_unresolved" || second.FailureCode != first.FailureCode {
		t.Fatalf("failure codes = %q, %q", first.FailureCode, second.FailureCode)
	}
	if got := executions.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
}

func TestProviderBridgeCloseIsBoundedAndIdempotent(t *testing.T) {
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bridge.Close(closeContext); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := bridge.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	bridge.mu.Lock()
	serveDone := bridge.serveDone
	bridge.mu.Unlock()
	select {
	case <-serveDone:
	default:
		t.Fatal("Close returned before Serve lifecycle completed")
	}
	request := newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"resource.sync","action_key":"after-close","input":{}}`))
	response, err := (&http.Client{}).Do(request)
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("request after Close unexpectedly connected")
	}
	if !strings.Contains(err.Error(), "connect") && !strings.Contains(err.Error(), "refused") && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("request after Close error = %v, want connection failure", err)
	}
	if _, err := bridge.Start(context.Background()); err == nil {
		t.Fatal("Start after Close unexpectedly succeeded")
	}
}

func TestProviderBridgeReportsUnexpectedServeFailure(t *testing.T) {
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	startTestProviderBridge(t, bridge)
	bridge.mu.Lock()
	listener := bridge.listener
	serveDone := bridge.serveDone
	bridge.mu.Unlock()
	if listener == nil || serveDone == nil {
		t.Fatal("bridge did not retain Serve lifecycle handles")
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close test listener: %v", err)
	}
	select {
	case <-serveDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not report the unexpected listener failure")
	}
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "serve-failed", Input: json.RawMessage(`{"title":"same"}`)}
	if _, err := bridge.executeRequest(context.Background(), request, []byte(`{"action_key":"serve-failed"}`), "serve-failed-action"); !errors.Is(err, errProviderBridgeNotStarted) {
		t.Fatalf("executeRequest after Serve failure error = %v, want not started", err)
	}
	bridge.mu.Lock()
	serveErr := bridge.serveErr
	bridge.mu.Unlock()
	if serveErr == nil {
		t.Fatal("Serve failure was not retained")
	}
	if _, err := bridge.Start(context.Background()); !errors.Is(err, errProviderBridgeServeFailed) {
		t.Fatalf("Start after Serve failure error = %v, want stable serve-failed error", err)
	}
	bridge.mu.Lock()
	listenerAfterRestart := bridge.listener
	bridge.mu.Unlock()
	if listenerAfterRestart != listener {
		t.Fatal("Start after Serve failure replaced the one-shot listener")
	}
	if err := bridge.Close(context.Background()); !errors.Is(err, serveErr) || errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Close error = %v, want original unexpected Serve error", err)
	}
}

func TestProviderBridgeCloseCancelsCooperativeExecution(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	bridge := newTestProviderBridge(t, func(ctx context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ProviderBridgeResponse{}, ctx.Err()
	})
	startTestProviderBridge(t, bridge)
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "close-cancel", Input: json.RawMessage(`{"title":"same"}`)}
	ownerDone := make(chan error, 1)
	go func() {
		_, err := bridge.executeRequest(context.Background(), request, []byte(`{"action_key":"close-cancel"}`), "close-cancel-action")
		ownerDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("cooperative executor did not start")
	}
	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bridge.Close(closeContext); err != nil {
		t.Fatalf("Close cooperative execution error = %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not observe bridge cancellation")
	}
	select {
	case err := <-ownerDone:
		if err != nil {
			t.Fatalf("owner executeRequest error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cooperative owner did not finish")
	}
}

func TestProviderBridgeCloseReportsIgnoringExecutorAsUnproven(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		close(started)
		<-release
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	startTestProviderBridge(t, bridge)
	request := ProviderBridgeRequest{ResourceID: testProviderResourceID, Operation: "change.upsert", ActionKey: "close-unproven", Input: json.RawMessage(`{"title":"same"}`)}
	ownerDone := make(chan error, 1)
	go func() {
		_, err := bridge.executeRequest(context.Background(), request, []byte(`{"action_key":"close-unproven"}`), "close-unproven-action")
		ownerDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("uncooperative executor did not start")
	}
	closeContext, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	err := bridge.Close(closeContext)
	cancel()
	if !errors.Is(err, errProviderBridgeExecutionUnproven) {
		t.Fatalf("Close error = %v, want execution unproven", err)
	}
	close(release)
	select {
	case ownerErr := <-ownerDone:
		if ownerErr != nil {
			t.Fatalf("uncooperative owner executeRequest error = %v", ownerErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("uncooperative owner did not finish after release")
	}
}

func TestProviderBridgeDoesNotExposeCredentialMaterial(t *testing.T) {
	bridge := newTestProviderBridge(t, func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
	})
	endpoint := startTestProviderBridge(t, bridge)
	values := []any{
		endpoint,
		ProviderBridgeRequest{ResourceID: "11111111-1111-4111-8111-111111111111", Operation: "resource.sync", ActionKey: "key-1", Input: json.RawMessage(`{}`)},
		ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeUnknown, FailureCode: "unknown"},
	}
	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %T: %v", value, err)
		}
		lower := strings.ToLower(string(encoded))
		for _, forbidden := range []string{"token", "authorization", "control_url", "controlurl", "credential"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%T JSON exposes forbidden material marker %q: %s", value, forbidden, encoded)
			}
		}
	}
	if strings.Contains(strings.ToLower(endpoint.URL), "token") || strings.Contains(strings.ToLower(endpoint.URL), "credential") {
		t.Fatalf("endpoint URL contains credential marker: %q", endpoint.URL)
	}
}

func TestProviderBridgeRejectsInvalidOptions(t *testing.T) {
	validExecute := func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error) {
		return ProviderBridgeResponse{}, nil
	}
	invalid := []ProviderBridgeOptions{
		{RunID: "", Grants: testProviderGrants(), Execute: validExecute},
		{RunID: "run-1", Grants: nil, Execute: validExecute},
		{RunID: "run-1", Grants: testProviderGrants(), Execute: nil},
		{RunID: "run-1", Grants: []protocol.ProviderGrant{{ResourceID: "11111111-1111-4111-8111-111111111111", Provider: "github", Kind: "repository", Operations: []string{"not-supported"}}}, Execute: validExecute},
		{RunID: "run-1", Grants: []protocol.ProviderGrant{{ResourceID: "repo-1", Provider: "github", Kind: "repository", Operations: []string{"resource.sync"}}}, Execute: validExecute},
		{RunID: "run-1", Grants: []protocol.ProviderGrant{{ResourceID: "11111111-1111-4111-8111-111111111111", Provider: "gitlab", Kind: "repository", Operations: []string{"resource.sync"}}}, Execute: validExecute},
		{RunID: "run-1", Grants: []protocol.ProviderGrant{{ResourceID: "11111111-1111-4111-8111-111111111111", Provider: "github", Kind: "work_tracking", Operations: []string{"change.upsert"}}}, Execute: validExecute},
		{RunID: "run-1", Grants: testProviderGrants(), Execute: validExecute, ExecutionTimeout: -time.Second},
	}
	for index, options := range invalid {
		if bridge, err := NewProviderBridge(options); bridge != nil || err == nil {
			t.Fatalf("invalid options %d returned bridge=%v err=%v", index, bridge, err)
		}
	}
}

func TestProviderBridgeAcceptsCanonicalVersionZeroUUIDGrant(t *testing.T) {
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID: "run-1",
		Grants: []protocol.ProviderGrant{{
			ResourceID: "00000000-0000-0000-0000-000000000000",
			Provider:   "github",
			Kind:       "repository",
			Operations: []string{"resource.sync"},
		}},
		Execute: func(_ context.Context, _ string, _ ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded}, nil
		},
	})
	if err != nil {
		t.Fatalf("version-zero UUID grant rejected: %v", err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	startTestProviderBridge(t, bridge)
	request := ProviderBridgeRequest{ResourceID: "00000000-0000-0000-0000-000000000000", Operation: "resource.sync", ActionKey: "zero-uuid", Input: json.RawMessage(`{}`)}
	if response, err := bridge.executeRequest(context.Background(), request, []byte(`{"action_key":"zero-uuid"}`), "zero-uuid-action"); err != nil || response.Outcome != ProviderBridgeOutcomeSucceeded {
		t.Fatalf("version-zero UUID execution = (%+v, %v), want succeeded", response, err)
	}
}

func newTestProviderBridge(t *testing.T, execute ExecuteProviderAction) *ProviderBridge {
	t.Helper()
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:   "run-1",
		Grants:  testProviderGrants(),
		Execute: execute,
	})
	if err != nil {
		t.Fatalf("NewProviderBridge() error = %v", err)
	}
	t.Cleanup(func() {
		_ = bridge.Close(context.Background())
	})
	return bridge
}

func testProviderGrants() []protocol.ProviderGrant {
	return []protocol.ProviderGrant{{
		ResourceID: "11111111-1111-4111-8111-111111111111",
		Provider:   "github",
		Kind:       "repository",
		Operations: []string{"resource.sync", "change.upsert", "change.update"},
	}}
}

func startTestProviderBridge(t *testing.T, bridge *ProviderBridge) ProviderBridgeEndpoint {
	t.Helper()
	endpoint, err := bridge.Start(context.Background())
	if err != nil {
		t.Fatalf("ProviderBridge.Start() error = %v", err)
	}
	return endpoint
}

func newProviderBridgeRequest(t *testing.T, endpoint ProviderBridgeEndpoint, method, endpointURL string, body []byte) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, endpointURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(providerBridgeNonceHeader, endpoint.Nonce)
	return request
}

func doProviderBridgeRequest(t *testing.T, request *http.Request) *http.Response {
	t.Helper()
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatalf("HTTP request error = %v", err)
	}
	return response
}

func closeResponseBody(t *testing.T, response *http.Response) {
	t.Helper()
	if response == nil || response.Body == nil {
		return
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("drain response body: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

type providerBridgePayload struct {
	ActionID    string                `json:"action_id"`
	Outcome     ProviderBridgeOutcome `json:"outcome"`
	Result      json.RawMessage       `json:"result"`
	FailureCode string                `json:"failure_code"`
}

func decodeProviderBridgePayload(t *testing.T, response *http.Response) providerBridgePayload {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want %d; body=%s", response.StatusCode, http.StatusOK, body)
	}
	var payload providerBridgePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode provider bridge response: %v", err)
	}
	return payload
}

func expectedProviderBridgeActionID(runID string, request ProviderBridgeRequest) string {
	digest := sha256.Sum256([]byte(runID + "\x00" + request.ResourceID + "\x00" + request.Operation + "\x00" + request.ActionKey))
	digest[6] = digest[6]&0x0f | 0x50
	digest[8] = digest[8]&0x3f | 0x80
	encoded := hex.EncodeToString(digest[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])
}
