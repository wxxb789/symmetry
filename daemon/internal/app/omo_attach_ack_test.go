package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const (
	omoAttachAckRunID      = "00000000-0000-4000-8000-000000000101"
	omoAttachAckTaskID     = "00000000-0000-4000-8000-000000000102"
	omoAttachAckMachineID  = "00000000-0000-4000-8000-000000000103"
	omoAttachAckRuntimeID  = "00000000-0000-4000-8000-000000000104"
	omoAttachAckLeaseToken = "00000000-0000-4000-8000-000000000105"
	omoAttachAckSessionID  = "00000000-0000-4000-8000-000000000106"
	omoAttachAckBindingID  = "00000000-0000-4000-8000-000000000107"
	omoAttachAckReceiptID  = "00000000-0000-4000-8000-000000000108"
	omoAttachAckOtherID    = "00000000-0000-4000-8000-000000000109"
)

// Only attachment crosses this real HTTP boundary. Existing admission/native
// fixtures own the surrounding daemon lifecycle; no native binary is launched.
type omoAttachAckControl struct {
	*nativeAdmissionControl
	attachment *control.Client
	app        *daemon
	t          *testing.T
	fixture    *omoAttachAckFixture
}

func (client *omoAttachAckControl) Claim(ctx context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	claim, err := client.nativeAdmissionControl.Claim(ctx, runID, request)
	claim.LeaseToken = omoAttachAckLeaseToken
	return claim, err
}

func (client *omoAttachAckControl) AttachHarnessSession(ctx context.Context, runID string, request control.GoalSessionAttachRequest) (control.GoalSessionReceipt, error) {
	receipt, err := client.attachment.AttachHarnessSession(ctx, runID, request)
	client.fixture.putErr = err
	return receipt, err
}

func (client *omoAttachAckControl) FetchHarnessSessionAttachment(ctx context.Context, runID string, fence protocol.Fence) (control.GoalSessionReceipt, error) {
	receipt, err := client.attachment.FetchHarnessSessionAttachment(ctx, runID, fence)
	if err == nil && client.fixture.afterRead != nil {
		client.fixture.afterRead()
	}
	return receipt, err
}

func (client *omoAttachAckControl) FetchRunContext(ctx context.Context, runID string, fence protocol.Fence) (control.GoalRunContext, error) {
	client.fixture.contextCalls++
	client.fixture.mu.Lock()
	request, original := client.fixture.request, client.fixture.original
	client.fixture.mu.Unlock()
	session, err := client.app.store.LoadGoalSessionByHandle(request.LocalHandleID)
	if err != nil {
		return control.GoalRunContext{}, err
	}
	journal, err := client.app.store.LoadJournal(state.RunKey{RunID: runID, Generation: fence.Generation})
	if err != nil {
		return control.GoalRunContext{}, err
	}
	if session.ControlSessionID != omoAttachAckSessionID || session.ControlAttachmentReceiptID != omoAttachAckReceiptID ||
		session.BindingID != omoAttachAckBindingID || session.NativeSessionID != "native-thread-1" || fence != original.Fence ||
		len(journal.DeliveredGoalDeliveries) != 1 || !reflect.DeepEqual(journal.DeliveredGoalDeliveries[0], original) {
		client.t.Errorf("canonical context crossed an unpersisted attachment: session=%#v journal=%#v fence=%#v", session, journal, fence)
	}
	return client.nativeAdmissionControl.FetchRunContext(ctx, runID, fence)
}

type omoAttachAckRoundTripper func(*http.Request) (*http.Response, error)

func (transport omoAttachAckRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type omoAttachAckFixture struct {
	t            *testing.T
	app          *daemon
	session      *fakeNativeGoalSession
	control      *omoAttachAckControl
	server       *httptest.Server
	mu           sync.Mutex
	methods      []string
	contexts     []context.Context
	queries      []url.Values
	request      control.GoalSessionAttachRequest
	original     state.GoalDelivery
	receipt      control.GoalSessionReceipt
	putErr       error
	contextCalls int
	putMode      string
	readStatus   int
	readCode     control.ErrorCode
	readBody     *string
	mutate       func(*control.GoalSessionReceipt)
	afterPut     func(context.Context)
	afterRead    func()
}

func newOmOAttachAckFixture(t *testing.T, kind harness.Kind) *omoAttachAckFixture {
	t.Helper()
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission: present=%t error=%v", present, err)
	}
	configKind := config.RuntimeHarnessOmO
	if kind == harness.KindCodex {
		configKind = config.RuntimeHarnessCodex
	}
	app, store, session, base := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, kind, configKind, testVerifiedNativeCapabilities(kind))
	t.Cleanup(func() { _ = store.Close() })
	app.log = slog.New(slog.NewJSONHandler(io.Discard, nil))
	app.machineID, app.runtimeID = omoAttachAckMachineID, omoAttachAckRuntimeID
	app.options.newID = state.NewGoalSessionLocalHandleID
	base.taskIDForRun = func(string) string { return omoAttachAckTaskID }
	base.attachSessionID = omoAttachAckSessionID
	fixture := &omoAttachAckFixture{t: t, app: app, session: session, putMode: "eof", readStatus: http.StatusOK}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(fixture.server.Close)
	transport := fixture.server.Client().Transport
	httpClient := &http.Client{Transport: omoAttachAckRoundTripper(func(request *http.Request) (*http.Response, error) {
		fixture.mu.Lock()
		fixture.contexts = append(fixture.contexts, request.Context())
		fixture.mu.Unlock()
		response, err := transport.RoundTrip(request)
		if request.Method == http.MethodPut && fixture.afterPut != nil {
			fixture.afterPut(request.Context())
		}
		if request.Method == http.MethodPut && fixture.putMode == "local_deadline" {
			return nil, context.DeadlineExceeded
		}
		if request.Method == http.MethodPut && (fixture.putMode == "wrapped_api_429" || fixture.putMode == "wrapped_api_503") {
			status := http.StatusTooManyRequests
			if fixture.putMode == "wrapped_api_503" {
				status = http.StatusServiceUnavailable
			}
			return nil, errors.Join(&control.APIError{StatusCode: status, Code: control.ServiceUnavailable}, io.EOF)
		}
		if request.Method == http.MethodPut && fixture.putMode == "wrapper_only" {
			return nil, &url.Error{Op: "Put", URL: request.URL.String(), Err: errors.New("permanent local transport configuration failure")}
		}
		return response, err
	})}
	attachment, err := control.NewClient(fixture.server.URL, "test-machine-token", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	fixture.control = &omoAttachAckControl{nativeAdmissionControl: base, attachment: attachment, app: app, t: t, fixture: fixture}
	app.control = fixture.control
	return fixture
}

func (fixture *omoAttachAckFixture) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.methods = append(fixture.methods, request.Method)
	fixture.queries = append(fixture.queries, request.URL.Query())
	if request.URL.Path != "/api/v1/runs/"+omoAttachAckRunID+"/session" {
		fixture.t.Errorf("unexpected attachment path: %s", request.URL.Path)
		http.Error(writer, "unexpected path", http.StatusBadRequest)
		return
	}
	if request.Method == http.MethodGet && len(fixture.methods) == 1 {
		writeOmOAttachAckAPIError(writer, http.StatusNotFound, control.NotFound)
		return
	}
	if request.Method == http.MethodPut {
		if err := json.NewDecoder(request.Body).Decode(&fixture.request); err != nil {
			fixture.t.Errorf("decode attachment request: %v", err)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		journal, err := fixture.app.store.LoadJournal(state.RunKey{RunID: omoAttachAckRunID, Generation: 1})
		if err != nil || len(journal.PendingGoalDeliveries) != 1 {
			fixture.t.Errorf("load durable attachment before commit: journal=%#v error=%v", journal, err)
			http.Error(writer, "missing intent", http.StatusBadRequest)
			return
		}
		fixture.original = journal.PendingGoalDeliveries[0]
		payload := fixture.original.SessionAttach
		var bindingID *string
		if payload != nil && payload.BindingID != "" {
			bindingID = &payload.BindingID
		}
		if !fixture.original.Ready || payload == nil || fixture.request.Fence != fixture.original.Fence ||
			fixture.request.LocalHandleID != payload.LocalHandleID || fixture.request.HarnessKind != payload.HarnessKind ||
			fixture.request.HarnessVersion != payload.HarnessVersion || fixture.request.AdapterVersion != payload.AdapterVersion ||
			fixture.request.WorkspaceFingerprint != payload.WorkspaceFingerprint || fixture.request.Workspace != payload.Workspace ||
			!reflect.DeepEqual(fixture.request.RepositoryResourceID, payload.RepositoryResourceID) || !reflect.DeepEqual(fixture.request.BindingID, bindingID) ||
			(bindingID == nil && !payload.ServerIssuedBinding) {
			fixture.t.Errorf("PUT changed the exact ready attachment: request=%#v delivery=%#v", fixture.request, fixture.original)
		}
		fixture.receipt = control.GoalSessionReceipt{
			ID: omoAttachAckSessionID, SessionID: omoAttachAckSessionID, AttachmentReceiptID: omoAttachAckReceiptID,
			GoalID: payload.GoalID, TaskID: omoAttachAckTaskID, RunID: omoAttachAckRunID, ActiveRunID: omoAttachAckRunID,
			MachineID: omoAttachAckMachineID, RuntimeID: fixture.request.RuntimeID, RepositoryResourceID: *fixture.request.RepositoryResourceID,
			LocalHandleID: fixture.request.LocalHandleID, BindingID: omoAttachAckBindingID, HarnessKind: fixture.request.HarnessKind,
			HarnessVersion: fixture.request.HarnessVersion, AdapterVersion: fixture.request.AdapterVersion,
			WorkspaceFingerprint: fixture.request.WorkspaceFingerprint, Workspace: fixture.request.Workspace, State: state.GoalSessionStateBusy,
		}
		switch fixture.putMode {
		case "429":
			writeOmOAttachAckAPIError(writer, http.StatusTooManyRequests, control.RateLimited)
		case "503":
			writeOmOAttachAckAPIError(writer, http.StatusServiceUnavailable, control.ServiceUnavailable)
		case "empty_response":
			writer.WriteHeader(http.StatusCreated)
		case "redirect":
			writer.Header().Set("Location", fixture.server.URL+"/forbidden-redirect")
			writer.WriteHeader(http.StatusTemporaryRedirect)
		case "unexpected_eof":
			writer.Header().Set("Content-Length", "1000")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"session":`)
			writer.(http.Flusher).Flush()
			closeOmOAttachAckResponse(fixture.t, writer)
		default:
			// Control has committed its immutable receipt above. Close the
			// connection before any response bytes cross the acknowledgement.
			closeOmOAttachAckResponse(fixture.t, writer)
		}
		return
	}
	if request.Method != http.MethodGet {
		http.Error(writer, "unexpected method", http.StatusMethodNotAllowed)
		return
	}
	if fixture.readStatus == 0 {
		closeOmOAttachAckResponse(fixture.t, writer)
		return
	}
	if fixture.readStatus != http.StatusOK {
		writeOmOAttachAckAPIError(writer, fixture.readStatus, fixture.readCode)
		return
	}
	if fixture.readBody != nil {
		_, _ = io.WriteString(writer, *fixture.readBody)
		return
	}
	receipt := fixture.receipt
	if fixture.mutate != nil {
		fixture.mutate(&receipt)
	}
	if err := json.NewEncoder(writer).Encode(map[string]any{"session": receipt}); err != nil {
		fixture.t.Errorf("write immutable receipt: %v", err)
	}
}

func writeOmOAttachAckAPIError(writer http.ResponseWriter, status int, code control.ErrorCode) {
	writer.WriteHeader(status)
	_, _ = fmt.Fprintf(writer, `{"error":{"code":%q,"message":"attachment readback rejected"}}`, code)
}

func closeOmOAttachAckResponse(t *testing.T, writer http.ResponseWriter) {
	connection, _, err := writer.(http.Hijacker).Hijack()
	if err != nil {
		t.Errorf("hijack committed attachment response: %v", err)
		return
	}
	_ = connection.Close()
}

func (fixture *omoAttachAckFixture) run(ctx context.Context) state.RunJournal {
	fixture.t.Helper()
	fixture.app.startAssignment(ctx, protocol.Assignment{RunID: omoAttachAckRunID, Generation: 1, Work: fixture.control.work})
	fixture.app.workers.Wait()
	journal, err := fixture.app.store.LoadJournal(state.RunKey{RunID: omoAttachAckRunID, Generation: 1})
	if err != nil {
		fixture.t.Fatal(err)
	}
	return journal
}

func (fixture *omoAttachAckFixture) assertRequests(want []string) {
	fixture.t.Helper()
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if !reflect.DeepEqual(fixture.methods, want) {
		journal, loadErr := fixture.app.store.LoadJournal(state.RunKey{RunID: omoAttachAckRunID, Generation: 1})
		failure := ""
		for _, transition := range journal.PendingTransitions {
			if isTerminalTransition(transition.State) {
				failure = string(transition.Payload)
				break
			}
		}
		fixture.t.Fatalf("attachment requests=%v, want %v (PUT error=%v local_state=%s terminal_state=%s failure=%s native_calls=%v journal_error=%v)", fixture.methods, want, fixture.putErr, journal.LocalState, journal.TerminalState, failure, fixture.session.callsSnapshot(), loadErr)
	}
	if len(fixture.contexts) != len(want) {
		fixture.t.Fatalf("request contexts=%d, want %d", len(fixture.contexts), len(want))
	}
	for index := range want {
		if fixture.contexts[index] != fixture.contexts[0] {
			fixture.t.Errorf("attachment request %d gained a fresh context/budget", index)
		}
		if want[index] != http.MethodGet || fixture.original.SessionAttach == nil {
			continue
		}
		fence := fixture.original.Fence
		query := url.Values{
			"runtime_id": {fence.RuntimeID}, "runtime_epoch": {strconv.FormatInt(fence.RuntimeEpoch, 10)},
			"generation": {strconv.FormatInt(fence.Generation, 10)}, "claim_id": {fence.ClaimID}, "lease_token": {fence.LeaseToken},
		}
		if !reflect.DeepEqual(fixture.queries[index], query) {
			fixture.t.Errorf("GET %d changed original fence: got=%v want=%v", index, fixture.queries[index], query)
		}
	}
}

func (fixture *omoAttachAckFixture) assertStoppedWithoutTurn() {
	fixture.t.Helper()
	calls := fixture.session.callsSnapshot()
	if countNativeStartCall(calls, "start_turn") != 0 || countNativeStartCall(calls, "close") != 1 || fixture.contextCalls != 0 {
		fixture.t.Fatalf("unresolved attachment started work or did not physically stop: calls=%v contexts=%d", calls, fixture.contextCalls)
	}
}

func (fixture *omoAttachAckFixture) assertExactPending(journal state.RunJournal) {
	fixture.t.Helper()
	fixture.mu.Lock()
	original := fixture.original
	fixture.mu.Unlock()
	if len(journal.PendingGoalDeliveries) != 1 || !reflect.DeepEqual(journal.PendingGoalDeliveries[0], original) ||
		len(journal.RetiredGoalDeliveries) != 0 || len(journal.DeliveredGoalDeliveries) != 0 {
		fixture.t.Fatalf("unknown PUT outcome changed the ready recovery barrier: journal=%#v original=%#v", journal, original)
	}
	session, err := fixture.app.store.LoadGoalSessionByHandle(original.SessionAttach.LocalHandleID)
	if err != nil || session.ControlSessionID != "" || session.ControlAttachmentReceiptID != "" || session.NativeSessionID != "native-thread-1" {
		fixture.t.Fatalf("unresolved readback changed native/control identities: session=%#v error=%v", session, err)
	}
}

func TestOmOAttachAckRegressionCommittedResponseLossStartsOriginalTurn(t *testing.T) {
	for _, kind := range []harness.Kind{harness.KindOmO, harness.KindCodex} {
		for _, mode := range []string{"eof", "unexpected_eof", "local_deadline"} {
			t.Run(string(kind)+"/"+mode, func(t *testing.T) {
				fixture := newOmOAttachAckFixture(t, kind)
				fixture.putMode = mode
				journal := fixture.run(context.Background())
				fixture.assertRequests([]string{http.MethodGet, http.MethodPut, http.MethodGet})
				if fixture.putErr == nil || fixture.contextCalls != 1 || journal.TerminalState != "completed" {
					t.Fatalf("committed attachment did not recover original work: error=%v contexts=%d journal=%#v", fixture.putErr, fixture.contextCalls, journal)
				}
				calls := fixture.session.callsSnapshot()
				if countNativeStartCall(calls, "open") != 1 || countNativeStartCall(calls, "start_turn") != 1 || countNativeStartCall(calls, "close") != 1 || fixture.control.claimCalls != 1 {
					t.Fatalf("response loss duplicated native work/admission: calls=%v claims=%d", calls, fixture.control.claimCalls)
				}
			})
		}
	}
}

func TestOmOAttachAckRegressionUnresolvedReadbackRetainsOriginalBarrier(t *testing.T) {
	malformed := `{"session":`
	for _, test := range []struct {
		name   string
		status int
		body   *string
		mutate func(*control.GoalSessionReceipt)
	}{
		{name: "absent", status: http.StatusNotFound},
		{name: "second_transport_loss", status: 0},
		{name: "malformed", status: http.StatusOK, body: &malformed},
		{name: "run", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.RunID = omoAttachAckOtherID }},
		{name: "goal", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.GoalID = omoAttachAckOtherID }},
		{name: "task", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.TaskID = omoAttachAckOtherID }},
		{name: "machine", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.MachineID = omoAttachAckOtherID }},
		{name: "runtime", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.RuntimeID = omoAttachAckOtherID }},
		{name: "active_run", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.ActiveRunID = omoAttachAckOtherID }},
		{name: "handle", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.LocalHandleID = omoAttachAckOtherID }},
		{name: "kind", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.HarnessKind = "codex" }},
		{name: "native_version", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.HarnessVersion = "other" }},
		{name: "adapter_version", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.AdapterVersion = "other" }},
		{name: "workspace_fingerprint", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.WorkspaceFingerprint = "sha256:" + strings.Repeat("b", 64) }},
		{name: "workspace", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.Workspace = "other" }},
		{name: "repository", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.RepositoryResourceID = omoAttachAckOtherID }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOmOAttachAckFixture(t, harness.KindOmO)
			fixture.readStatus, fixture.readBody, fixture.mutate = test.status, test.body, test.mutate
			journal := fixture.run(context.Background())
			fixture.assertRequests([]string{http.MethodGet, http.MethodPut, http.MethodGet})
			fixture.assertStoppedWithoutTurn()
			fixture.assertExactPending(journal)
			if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 || !strings.Contains(string(journal.PendingTransitions[0].Payload), string(protocol.TaskResultReasonUnknownOutcome)) {
				t.Fatalf("unresolved committed PUT lost unknown-outcome classification: %#v", journal)
			}
		})
	}
}

func TestOmOAttachAckRegressionPermanentPUTDoesNotReadAgain(t *testing.T) {
	for _, mode := range []string{"429", "503", "wrapped_api_429", "wrapped_api_503", "empty_response", "redirect", "wrapper_only"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newOmOAttachAckFixture(t, harness.KindOmO)
			fixture.putMode = mode
			journal := fixture.run(context.Background())
			fixture.assertRequests([]string{http.MethodGet, http.MethodPut})
			fixture.assertStoppedWithoutTurn()
			fixture.assertExactPending(journal)
			if mode == "empty_response" {
				var responseError *control.ResponseError
				if !errors.As(fixture.putErr, &responseError) || !errors.Is(fixture.putErr, io.EOF) {
					t.Fatalf("fixture did not produce a permanent ResponseError wrapping EOF: %v", fixture.putErr)
				}
			}
		})
	}
}

// This direct-delivery fixture keeps the HTTP client and durable outbox real,
// while exposing the returned error and the existing resume receipt validator.
func (fixture *omoAttachAckFixture) prepareDelivery(bound bool) state.RunJournal {
	fixture.t.Helper()
	key := state.RunKey{RunID: omoAttachAckRunID, Generation: 1}
	claimID, err := state.NewGoalSessionLocalHandleID()
	if err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := fixture.app.store.SaveClaimIntent(state.ClaimIntent{
		Key: key, RuntimeKey: fixture.app.config.Runtime.RuntimeKey, RuntimeID: omoAttachAckRuntimeID, RuntimeEpoch: 1,
		ClaimID: claimID, LocalState: "claiming", Work: fixture.control.work, WorkspaceBindingKey: "local",
	}); err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := fixture.app.store.SaveClaimGrant(key, protocol.ClaimResponse{
		RunID: key.RunID, TaskID: omoAttachAckTaskID, Generation: 1, ClaimID: claimID, LeaseToken: omoAttachAckLeaseToken,
		LeaseExpiresAt: time.Now().Add(time.Minute), Work: fixture.control.work,
	}); err != nil {
		fixture.t.Fatal(err)
	}
	admission := fixture.control.admission
	capabilities := fixture.app.harnessCapabilities
	payload := state.GoalSessionAttachDelivery{
		GoalID: admission.GoalID, LocalHandleID: omoAttachAckOtherID, ServerIssuedBinding: !bound,
		HarnessKind: string(capabilities.Kind), HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion,
		WorkspaceFingerprint: "sha256:" + strings.Repeat("a", 64), Workspace: "local", RepositoryResourceID: &admission.Subject.ResourceID,
	}
	if bound {
		payload.BindingID = omoAttachAckBindingID
	}
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: payload.LocalHandleID}
	if _, err := fixture.app.store.SaveGoalSessionLaunchIntent(state.GoalSessionLaunchIntent{
		LaunchIntentID: omoAttachAckOtherID, GoalID: admission.GoalID, GoalRevision: admission.GoalRevision,
		WorkItemID: admissionWorkItemIDValue(admission.WorkItemID), TaskID: omoAttachAckTaskID, RunID: key.RunID, Generation: 1,
		AdmissionID: admission.AdmissionID, LocalHandleID: payload.LocalHandleID, BindingID: payload.BindingID,
		MachineID: omoAttachAckMachineID, RuntimeID: omoAttachAckRuntimeID, RuntimeEpoch: 1,
		HarnessKind: payload.HarnessKind, HarnessVersion: payload.HarnessVersion, AdapterVersion: payload.AdapterVersion,
		AdapterProtocolVersion: 1, WorkspaceFingerprint: payload.WorkspaceFingerprint, WorkspacePath: fixture.app.config.StateDir,
		RepositoryResourceID: admission.Subject.ResourceID, SessionMode: state.GoalSessionModeFresh,
	}); err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := fixture.app.store.MarkGoalSessionLaunchStarted(sessionKey); err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := fixture.app.store.PersistGoalSessionHandle(sessionKey, state.GoalSessionHandle{NativeSessionID: "native-thread-1"}); err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := fixture.app.store.QueueGoalSessionAttach(key, payload); err != nil {
		fixture.t.Fatal(err)
	}
	journal, err := fixture.app.store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
	if err != nil {
		fixture.t.Fatal(err)
	}
	return journal
}

func TestOmOAttachAckRegressionUnresolvedGETPreservesPUTErrorClass(t *testing.T) {
	malformed := `{"session":`
	for _, test := range []struct {
		name     string
		status   int
		code     control.ErrorCode
		body     *string
		mutate   func(*control.GoalSessionReceipt)
		definite bool
	}{
		{name: "404", status: http.StatusNotFound, code: control.NotFound},
		{name: "transport", status: 0},
		{name: "malformed", status: http.StatusOK, body: &malformed},
		{name: "daemon_identity", status: http.StatusOK, mutate: func(r *control.GoalSessionReceipt) { r.TaskID = omoAttachAckOtherID }},
		{name: "401", status: http.StatusUnauthorized, code: control.Unauthenticated, definite: true},
		{name: "403", status: http.StatusForbidden, code: control.Forbidden, definite: true},
		{name: "ownership", status: http.StatusConflict, code: control.OwnershipLost, definite: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOmOAttachAckFixture(t, harness.KindOmO)
			journal := fixture.prepareDelivery(false)
			fixture.readStatus, fixture.readCode, fixture.readBody, fixture.mutate = test.status, test.code, test.body, test.mutate
			_, receipt, err := fixture.app.deliverGoalDelivery(context.Background(), journal, state.GoalDeliverySessionAttach, omoAttachAckOtherID, nil)
			fixture.assertRequests([]string{http.MethodGet, http.MethodPut, http.MethodGet})
			if err == nil || receipt != nil || !errors.Is(fixture.putErr, io.EOF) {
				t.Fatalf("fixture did not expose unresolved committed PUT: receipt=%#v error=%v PUT error=%v", receipt, err, fixture.putErr)
			}
			var apiError *control.APIError
			var responseError *control.ResponseError
			if test.definite {
				if !errors.As(err, &apiError) || apiError.StatusCode != test.status || apiError.Code != test.code || errors.Is(err, io.EOF) {
					t.Fatalf("definite GET rejection was replaced by uncertain PUT: %v", err)
				}
			} else {
				if !errors.Is(err, io.EOF) || errors.As(err, &apiError) || errors.As(err, &responseError) {
					t.Fatalf("inconclusive GET replaced original transport-unknown classification: %v", err)
				}
				loaded, loadErr := fixture.app.store.LoadJournal(journal.Key())
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				fixture.assertExactPending(loaded)
			}
		})
	}
}

func TestOmOAttachAckRegressionReadbackUsesReservedResumePair(t *testing.T) {
	for _, mismatch := range []string{"none", "session", "binding"} {
		t.Run(mismatch, func(t *testing.T) {
			fixture := newOmOAttachAckFixture(t, harness.KindOmO)
			journal := fixture.prepareDelivery(true)
			admission := fixture.control.admission
			admission.SessionMode, admission.RequestedSessionID = protocol.SessionModeResume, new(omoAttachAckSessionID)
			claim := protocol.ClaimResponse{TaskID: omoAttachAckTaskID, HarnessSessionID: admission.RequestedSessionID, HarnessBindingID: new(omoAttachAckBindingID)}
			fixture.mutate = func(receipt *control.GoalSessionReceipt) {
				if mismatch == "session" {
					receipt.ID, receipt.SessionID = omoAttachAckReceiptID, omoAttachAckReceiptID
				} else if mismatch == "binding" {
					receipt.BindingID = omoAttachAckReceiptID
				}
			}
			updated, receipt, err := fixture.app.deliverGoalDelivery(context.Background(), journal, state.GoalDeliverySessionAttach, omoAttachAckOtherID, func(receipt control.GoalSessionReceipt) error {
				return validateGoalSessionReceipt(admission, claim, journal.Key(), omoAttachAckOtherID, receipt)
			})
			fixture.assertRequests([]string{http.MethodGet, http.MethodPut, http.MethodGet})
			if mismatch != "none" {
				if receipt != nil || !errors.Is(err, io.EOF) {
					t.Fatalf("mismatched reservation resolved uncertain PUT: receipt=%#v error=%v", receipt, err)
				}
				fixture.assertExactPending(updated)
				return
			}
			session, loadErr := fixture.app.store.LoadGoalSessionByHandle(omoAttachAckOtherID)
			if err != nil || receipt == nil || loadErr != nil || session.ControlSessionID != omoAttachAckSessionID || session.BindingID != omoAttachAckBindingID ||
				session.ControlAttachmentReceiptID != omoAttachAckReceiptID || len(updated.PendingGoalDeliveries) != 0 || len(updated.DeliveredGoalDeliveries) != 1 {
				t.Fatalf("exact reserved pair was not durably mapped before outbox ack: receipt=%#v session=%#v journal=%#v error=%v load=%v", receipt, session, updated, err, loadErr)
			}
		})
	}
}

func TestOmOAttachAckRegressionReadbackRejectionKeepsDefiniteSemantics(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		code    control.ErrorCode
		retired bool
	}{
		{name: "unauthenticated", status: http.StatusUnauthorized, code: control.Unauthenticated},
		{name: "forbidden", status: http.StatusForbidden, code: control.Forbidden, retired: true},
		{name: "ownership_lost", status: http.StatusConflict, code: control.OwnershipLost, retired: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOmOAttachAckFixture(t, harness.KindOmO)
			fixture.readStatus, fixture.readCode = test.status, test.code
			journal := fixture.run(context.Background())
			fixture.assertRequests([]string{http.MethodGet, http.MethodPut, http.MethodGet})
			fixture.assertStoppedWithoutTurn()
			if !test.retired {
				fixture.assertExactPending(journal)
				return
			}
			if len(journal.PendingGoalDeliveries) != 0 || len(journal.RetiredGoalDeliveries) != 1 ||
				!reflect.DeepEqual(journal.RetiredGoalDeliveries[0].Delivery, fixture.original) ||
				journal.RetiredGoalDeliveries[0].StatusCode != test.status || journal.RetiredGoalDeliveries[0].Code != string(test.code) {
				t.Fatalf("definite readback rejection lost existing retirement semantics: %#v", journal)
			}
		})
	}
}

func TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget(t *testing.T) {
	for _, mode := range []string{"caller_cancel", "caller_deadline", "admission_deadline", "request_deadline"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newOmOAttachAckFixture(t, harness.KindOmO)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "caller_cancel" {
				fixture.afterPut = func(context.Context) { cancel() }
			} else {
				deadline := time.Now().Add(2 * time.Second)
				if mode == "caller_deadline" {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithDeadline(ctx, deadline)
					defer deadlineCancel()
				} else if mode == "admission_deadline" {
					admission := fixture.control.admission
					admission.Limits.DeadlineAt = deadline.UTC().Format(time.RFC3339Nano)
					fixture.control.admission = admission
					input, err := json.Marshal(admission)
					if err != nil {
						t.Fatal(err)
					}
					fixture.control.work.Input = input
				}
				fixture.afterPut = func(requestContext context.Context) { <-requestContext.Done() }
			}
			journal := fixture.run(ctx)
			fixture.assertRequests([]string{http.MethodGet, http.MethodPut})
			fixture.assertStoppedWithoutTurn()
			fixture.assertExactPending(journal)
			if mode == "request_deadline" && ctx.Err() != nil {
				t.Fatalf("request-limit fixture exhausted caller rather than original request context: %v", ctx.Err())
			}
		})
	}
}

func TestOmOAttachAckRegressionHistoricalReceiptDoesNotAuthorizeCancelledTurn(t *testing.T) {
	for _, mode := range []string{"operator_cancel", "lease_expired"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newOmOAttachAckFixture(t, harness.KindOmO)
			fixture.afterRead = func() {
				key := state.RunKey{RunID: omoAttachAckRunID, Generation: 1}
				if mode == "operator_cancel" {
					fixture.app.handleCommand(context.Background(), protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: omoAttachAckOtherID, Kind: "cancel"})
				} else {
					journal, err := fixture.app.store.LoadJournal(key)
					if err != nil {
						t.Error(err)
						return
					}
					fixture.app.terminateForLease(journal, "lease expired after attachment commit")
				}
			}
			journal := fixture.run(context.Background())
			fixture.assertRequests([]string{http.MethodGet, http.MethodPut, http.MethodGet})
			calls := fixture.session.callsSnapshot()
			if countNativeStartCall(calls, "start_turn") != 0 || countNativeStartCall(calls, "close") != 1 {
				t.Fatalf("historical busy receipt authorized cancelled/expired work: calls=%v journal=%#v", calls, journal)
			}
			if len(journal.DeliveredGoalDeliveries) != 1 || journal.DeliveredGoalDeliveries[0].Kind != state.GoalDeliverySessionAttach || fixture.contextCalls != 1 {
				t.Fatalf("fixture did not pass a successful historical receipt through durable mapping: contexts=%d journal=%#v", fixture.contextCalls, journal)
			}
		})
	}
}
