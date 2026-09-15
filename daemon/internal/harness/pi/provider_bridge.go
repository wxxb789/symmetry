package pi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	providerBridgePath                    = "/v1/actions"
	providerBridgeNonceHeader             = "X-Symmetry-Bridge-Nonce"
	providerBridgeMaxBodyBytes            = 64 << 10
	providerBridgeMaxResponseBytes        = 64 << 10
	providerBridgeMaxReplayEntries        = 256
	providerBridgeMaxActiveExecutions     = 32
	providerBridgeMaxReplayWaiters        = 64
	providerBridgeMaxRunIDBytes           = 256
	providerBridgeMaxResourceIDBytes      = 256
	providerBridgeMaxActionKeyBytes       = 256
	providerBridgeMaxFailureCodeBytes     = 256
	providerBridgeDefaultExecutionTimeout = 30 * time.Second
	providerBridgeMaxExecutionTimeout     = 5 * time.Minute
	providerBridgeCloseTimeout            = 5 * time.Second
	providerBridgeReadTimeout             = 15 * time.Second
	providerBridgeWriteTimeout            = 15 * time.Second
	providerBridgeIdleTimeout             = 30 * time.Second
	providerBridgeMaxHeaderBytes          = 32 << 10
	providerBridgePeerVerificationTimeout = 5 * time.Second
)

var (
	errProviderBridgeClosed               = errors.New("provider bridge is closed")
	errProviderBridgeNotStarted           = errors.New("provider bridge is not started")
	errProviderBridgeAlreadyStarted       = errors.New("provider bridge is already started")
	errProviderBridgeStartInProgress      = errors.New("provider bridge start is already in progress")
	errProviderBridgeServeFailed          = errors.New("provider bridge Serve failed")
	errProviderBridgeInvalidOptions       = errors.New("provider bridge options are invalid")
	errProviderBridgeReplayCapacity       = errors.New("provider bridge replay capacity is exhausted")
	errProviderBridgeExecutionCapacity    = errors.New("provider bridge execution capacity is exhausted")
	errProviderBridgeReplayWaiterCapacity = errors.New("provider bridge replay waiter capacity is exhausted")
	errProviderBridgeExecutionUnproven    = errors.New("provider bridge execution shutdown is unproven")
	errProviderBridgePeerNotBound         = errors.New("provider bridge process identity is not bound")
	errProviderBridgePeerAlreadyBound     = errors.New("provider bridge process identity is already bound")
)

// ProviderBridgeOutcome is the executor's conclusive classification. Unknown
// is intentionally terminal for this in-memory foundation: it is replayed and
// never dispatched a second time.
type ProviderBridgeOutcome string

const (
	ProviderBridgeOutcomeSucceeded ProviderBridgeOutcome = "succeeded"
	ProviderBridgeOutcomeFailed    ProviderBridgeOutcome = "failed"
	ProviderBridgeOutcomeUnknown   ProviderBridgeOutcome = "unknown"
)

// ProviderBridgeOptions configures the daemon-owned, session-local bridge.
// This first slice deliberately has no token, Control URL, or provider
// credential. It is not wired into Pi capability advertisement yet.
type ProviderBridgeOptions struct {
	RunID               string
	Grants              []protocol.ProviderGrant
	Execute             ExecuteProviderAction
	ExecutionTimeout    time.Duration
	RequirePeerIdentity bool
}

// ExecuteProviderAction is the narrow local seam used by the bridge. The
// action_id is derived by the bridge; callers cannot supply or replace it.
type ExecuteProviderAction func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error)

// ProviderBridgeRequest is the validated action delivered to the executor.
type ProviderBridgeRequest struct {
	ResourceID string          `json:"resource_id"`
	Operation  string          `json:"operation"`
	ActionKey  string          `json:"action_key"`
	Input      json.RawMessage `json:"input"`
}

// ProviderBridgeResponse is the executor result retained for exact replay.
// Result is opaque validated JSON; the bridge does not invent provider error
// semantics or retry an unknown external effect.
type ProviderBridgeResponse struct {
	Outcome     ProviderBridgeOutcome `json:"outcome"`
	Result      json.RawMessage       `json:"result,omitempty"`
	FailureCode string                `json:"failure_code,omitempty"`
}

// ProviderBridgeEndpoint is the local URL and per-session nonce required by a
// future daemon-owned native extension. The URL is always numeric IPv4
// loopback and already contains the bridge route.
type ProviderBridgeEndpoint struct {
	URL   string `json:"url"`
	Nonce string `json:"nonce"`
}

// ProviderBridge is an in-memory, bounded local protocol foundation. Its
// replay table is deliberately not durable and must not be advertised as
// ProviderAccess until later Pi wiring adds a durable run journal mapping and
// verifies the native process peer identity, lease/fence ownership, and
// Control-owned input normalization. A callback that ignores the bridge-owned
// cancellation cannot be claimed clean: Close returns an explicit unproven
// shutdown error until that callback exits.
type ProviderBridge struct {
	mu sync.Mutex

	runID               string
	grants              map[string]map[string]struct{}
	execute             ExecuteProviderAction
	replay              map[string]*providerBridgeReplayEntry
	executionTimeout    time.Duration
	lifetimeCtx         context.Context
	cancelLifetime      context.CancelFunc
	activeExecutions    int
	executionDone       chan struct{}
	replayWaiters       int
	requirePeerIdentity bool
	peerPID             int
	peerIdentity        string
	// onReplayWait is a package-local synchronization seam used only by the
	// deterministic single-flight tests; production construction leaves it nil.
	onReplayWait func()

	starting  bool
	started   bool
	closed    bool
	nonce     string
	endpoint  ProviderBridgeEndpoint
	server    *http.Server
	listener  net.Listener
	serveDone chan struct{}
	serveErr  error

	closeDone chan struct{}
	closeErr  error
}

type providerBridgeReplayEntry struct {
	actionKey string
	canonical []byte
	done      chan struct{}
	response  ProviderBridgeResponse
	err       error
}

// NewProviderBridge validates and copies its grants. It creates no listener
// and cannot expose ProviderAccess by itself.
func NewProviderBridge(options ProviderBridgeOptions) (*ProviderBridge, error) {
	if err := validateProviderBridgeOptions(options); err != nil {
		return nil, err
	}
	lifetimeContext, cancelLifetime := context.WithCancel(context.Background())

	grants := make(map[string]map[string]struct{}, len(options.Grants))
	for _, grant := range options.Grants {
		operations := make(map[string]struct{}, len(grant.Operations))
		for _, operation := range grant.Operations {
			operations[operation] = struct{}{}
		}
		grants[grant.ResourceID] = operations
	}

	return &ProviderBridge{
		runID:               options.RunID,
		grants:              grants,
		execute:             options.Execute,
		executionTimeout:    normalizedProviderBridgeExecutionTimeout(options.ExecutionTimeout),
		lifetimeCtx:         lifetimeContext,
		cancelLifetime:      cancelLifetime,
		replay:              make(map[string]*providerBridgeReplayEntry),
		executionDone:       closedProviderBridgeChannel(),
		closeDone:           make(chan struct{}),
		requirePeerIdentity: options.RequirePeerIdentity,
	}, nil
}

// BindProcess authorizes the one native Pi process allowed to call a bridge
// configured with RequirePeerIdentity. Exact replay is idempotent; replacing
// the process identity is forbidden for the bridge lifetime.
func (bridge *ProviderBridge) BindProcess(pid int, identity string) error {
	if bridge == nil || pid <= 0 || validateBridgeString(identity, 4096, false) != nil {
		return errors.New("provider bridge process identity is invalid")
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.closed {
		return errProviderBridgeClosed
	}
	if !bridge.requirePeerIdentity {
		return errors.New("provider bridge peer identity is not required")
	}
	if bridge.peerPID != 0 || bridge.peerIdentity != "" {
		if bridge.peerPID == pid && bridge.peerIdentity == identity {
			return nil
		}
		return errProviderBridgePeerAlreadyBound
	}
	bridge.peerPID = pid
	bridge.peerIdentity = identity
	return nil
}

func normalizedProviderBridgeExecutionTimeout(timeout time.Duration) time.Duration {
	if timeout == 0 {
		return providerBridgeDefaultExecutionTimeout
	}
	return timeout
}

func closedProviderBridgeChannel() chan struct{} {
	channel := make(chan struct{})
	close(channel)
	return channel
}

// Start binds only numeric IPv4 loopback. The supplied context bounds the
// listener setup; it does not turn into a lifetime context for the server.
func (bridge *ProviderBridge) Start(ctx context.Context) (ProviderBridgeEndpoint, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	bridge.mu.Lock()
	if bridge.closed {
		bridge.mu.Unlock()
		return ProviderBridgeEndpoint{}, errProviderBridgeClosed
	}
	if bridge.serveErr != nil {
		bridge.mu.Unlock()
		return ProviderBridgeEndpoint{}, errProviderBridgeServeFailed
	}
	if bridge.started {
		bridge.mu.Unlock()
		return ProviderBridgeEndpoint{}, errProviderBridgeAlreadyStarted
	}
	if bridge.starting {
		bridge.mu.Unlock()
		return ProviderBridgeEndpoint{}, errProviderBridgeStartInProgress
	}
	bridge.starting = true
	bridge.mu.Unlock()

	nonceBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		bridge.finishFailedStart()
		return ProviderBridgeEndpoint{}, errors.New("generate provider bridge nonce")
	}
	if err := ctx.Err(); err != nil {
		bridge.finishFailedStart()
		return ProviderBridgeEndpoint{}, err
	}
	nonce := hex.EncodeToString(nonceBytes)

	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		bridge.finishFailedStart()
		if ctx.Err() != nil {
			return ProviderBridgeEndpoint{}, ctx.Err()
		}
		return ProviderBridgeEndpoint{}, errors.New("listen on provider bridge loopback")
	}
	if err := ctx.Err(); err != nil {
		_ = listener.Close()
		bridge.finishFailedStart()
		return ProviderBridgeEndpoint{}, err
	}

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.IP == nil || !address.IP.Equal(net.IPv4(127, 0, 0, 1)) || address.Port <= 0 {
		_ = listener.Close()
		bridge.finishFailedStart()
		return ProviderBridgeEndpoint{}, errors.New("provider bridge listener is not numeric IPv4 loopback")
	}

	server := &http.Server{
		Handler: http.HandlerFunc(bridge.serveHTTP),
		ConnContext: func(ctx context.Context, connection net.Conn) context.Context {
			return context.WithValue(ctx, providerBridgeConnectionContextKey{}, connection)
		},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       providerBridgeReadTimeout,
		WriteTimeout:      providerBridgeWriteTimeout,
		IdleTimeout:       providerBridgeIdleTimeout,
		MaxHeaderBytes:    providerBridgeMaxHeaderBytes,
	}
	serveDone := make(chan struct{})
	endpoint := ProviderBridgeEndpoint{
		URL:   fmt.Sprintf("http://127.0.0.1:%d%s", address.Port, providerBridgePath),
		Nonce: nonce,
	}

	bridge.mu.Lock()
	if bridge.closed {
		bridge.starting = false
		bridge.mu.Unlock()
		_ = listener.Close()
		return ProviderBridgeEndpoint{}, errProviderBridgeClosed
	}
	bridge.listener = listener
	bridge.server = server
	bridge.nonce = nonce
	bridge.endpoint = endpoint
	bridge.serveDone = serveDone
	bridge.started = true
	bridge.starting = false
	bridge.mu.Unlock()

	go func() {
		serveErr := server.Serve(listener)
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		bridge.mu.Lock()
		bridge.serveErr = serveErr
		if serveErr != nil {
			bridge.started = false
			bridge.cancelLifetime()
		}
		close(serveDone)
		bridge.mu.Unlock()
	}()
	return endpoint, nil
}

// Close stops accepting requests and bounds server shutdown. Repeated calls
// replay the first shutdown result; a caller with a canceled context may stop
// waiting without creating a second shutdown owner.
func (bridge *ProviderBridge) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	bridge.mu.Lock()
	if !bridge.closed {
		bridge.closed = true
		server := bridge.server
		serveDone := bridge.serveDone
		cancelLifetime := bridge.cancelLifetime
		if server == nil {
			cancelLifetime()
			bridge.closeErr = nil
			close(bridge.closeDone)
			bridge.mu.Unlock()
			return nil
		}
		bridge.mu.Unlock()

		cancelLifetime()
		shutdownErr := bridge.shutdownServer(ctx, server, serveDone)
		bridge.mu.Lock()
		bridge.closeErr = shutdownErr
		close(bridge.closeDone)
		bridge.mu.Unlock()
		return shutdownErr
	}
	done := bridge.closeDone
	bridge.mu.Unlock()

	select {
	case <-done:
		bridge.mu.Lock()
		err := bridge.closeErr
		bridge.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (bridge *ProviderBridge) shutdownServer(ctx context.Context, server *http.Server, serveDone <-chan struct{}) error {
	shutdownContext, cancel := providerBridgeCloseContext(ctx)
	defer cancel()

	var shutdownErrors []error
	if err := server.Shutdown(shutdownContext); err != nil {
		shutdownErrors = append(shutdownErrors, err)
		_ = server.Close()
	}
	if serveDone != nil {
		select {
		case <-serveDone:
		case <-shutdownContext.Done():
			_ = server.Close()
			shutdownErrors = append(shutdownErrors, shutdownContext.Err())
		}
	}
	if err := bridge.waitForExecutions(shutdownContext); err != nil {
		shutdownErrors = append(shutdownErrors, err)
	}

	bridge.mu.Lock()
	serveErr := bridge.serveErr
	bridge.mu.Unlock()
	if serveErr != nil {
		shutdownErrors = append(shutdownErrors, serveErr)
	}
	return errors.Join(shutdownErrors...)
}

func providerBridgeCloseContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(providerBridgeCloseTimeout)
	if existingDeadline, ok := ctx.Deadline(); ok && existingDeadline.Before(deadline) {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, deadline)
}

func (bridge *ProviderBridge) waitForExecutions(ctx context.Context) error {
	bridge.mu.Lock()
	done := bridge.executionDone
	active := bridge.activeExecutions
	bridge.mu.Unlock()
	if active == 0 {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errProviderBridgeExecutionUnproven
	}
}

func (bridge *ProviderBridge) finishFailedStart() {
	bridge.mu.Lock()
	bridge.starting = false
	bridge.mu.Unlock()
}

func (bridge *ProviderBridge) serveHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodPost {
		writeProviderBridgeError(response, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if request.URL == nil || request.URL.Path != providerBridgePath || request.URL.RawQuery != "" {
		writeProviderBridgeError(response, http.StatusNotFound, "not_found")
		return
	}
	if _, present := request.Header["Authorization"]; present {
		writeProviderBridgeError(response, http.StatusBadRequest, "authorization_not_supported")
		return
	}
	contentTypes := request.Header.Values("Content-Type")
	contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if len(contentTypes) != 1 || err != nil || contentType != "application/json" {
		writeProviderBridgeError(response, http.StatusUnsupportedMediaType, "content_type_required")
		return
	}

	bridge.mu.Lock()
	started := bridge.started && !bridge.closed
	nonce := bridge.nonce
	bridge.mu.Unlock()
	if !started {
		writeProviderBridgeError(response, http.StatusServiceUnavailable, "bridge_unavailable")
		return
	}
	values := request.Header.Values(providerBridgeNonceHeader)
	if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte(nonce)) != 1 {
		writeProviderBridgeError(response, http.StatusUnauthorized, "invalid_bridge_nonce")
		return
	}
	if err := bridge.verifyRequestPeer(request.Context()); err != nil {
		if errors.Is(err, errProviderBridgePeerNotBound) {
			writeProviderBridgeError(response, http.StatusServiceUnavailable, "bridge_peer_not_bound")
			return
		}
		writeProviderBridgeError(response, http.StatusForbidden, "bridge_peer_not_authorized")
		return
	}
	if request.ContentLength > providerBridgeMaxBodyBytes {
		writeProviderBridgeError(response, http.StatusRequestEntityTooLarge, "body_too_large")
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, providerBridgeMaxBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil || len(body) > providerBridgeMaxBodyBytes {
		writeProviderBridgeError(response, http.StatusRequestEntityTooLarge, "body_too_large")
		return
	}
	parsed, canonical, actionID, err := bridge.parseRequest(body)
	if err != nil {
		writeProviderBridgeError(response, http.StatusBadRequest, "invalid_request")
		return
	}

	result, err := bridge.executeRequest(request.Context(), parsed, canonical, actionID)
	if err != nil {
		if errors.Is(err, errProviderBridgeClosed) || errors.Is(err, errProviderBridgeNotStarted) {
			writeProviderBridgeError(response, http.StatusServiceUnavailable, "bridge_unavailable")
			return
		}
		if errors.Is(err, errProviderBridgeConflict) {
			writeProviderBridgeError(response, http.StatusConflict, "idempotency_conflict")
			return
		}
		if errors.Is(err, errProviderBridgeReplayCapacity) {
			writeProviderBridgeError(response, http.StatusServiceUnavailable, "replay_capacity_exhausted")
			return
		}
		if errors.Is(err, errProviderBridgeExecutionCapacity) {
			writeProviderBridgeError(response, http.StatusServiceUnavailable, "execution_capacity_exhausted")
			return
		}
		if errors.Is(err, errProviderBridgeReplayWaiterCapacity) {
			writeProviderBridgeError(response, http.StatusServiceUnavailable, "replay_waiter_capacity_exhausted")
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			writeProviderBridgeError(response, http.StatusRequestTimeout, "request_canceled")
			return
		}
		writeProviderBridgeError(response, http.StatusInternalServerError, "bridge_execution_failed")
		return
	}
	writeProviderBridgeResponse(response, actionID, result)
}

type providerBridgeConnectionContextKey struct{}

func (bridge *ProviderBridge) verifyRequestPeer(ctx context.Context) error {
	bridge.mu.Lock()
	required := bridge.requirePeerIdentity
	pid := bridge.peerPID
	identity := bridge.peerIdentity
	bridge.mu.Unlock()
	if !required {
		return nil
	}
	if pid <= 0 || identity == "" {
		return errProviderBridgePeerNotBound
	}
	connection, ok := ctx.Value(providerBridgeConnectionContextKey{}).(net.Conn)
	if !ok || connection == nil {
		return errors.New("provider bridge connection identity is unavailable")
	}
	verificationContext, cancel := context.WithTimeout(ctx, providerBridgePeerVerificationTimeout)
	defer cancel()
	return platform.VerifyLoopbackTCPPeer(verificationContext, connection, pid, identity)
}

var errProviderBridgeConflict = errors.New("provider bridge idempotency conflict")

func (bridge *ProviderBridge) parseRequest(body []byte) (ProviderBridgeRequest, []byte, string, error) {
	if len(body) == 0 || !utf8.Valid(body) {
		return ProviderBridgeRequest{}, nil, "", errors.New("invalid request body")
	}
	if err := validateUniqueJSON(body); err != nil {
		return ProviderBridgeRequest{}, nil, "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return ProviderBridgeRequest{}, nil, "", err
	}
	if fields == nil {
		return ProviderBridgeRequest{}, nil, "", errors.New("request must be an object")
	}
	allowed := map[string]struct{}{
		"resource_id": {},
		"operation":   {},
		"action_key":  {},
		"input":       {},
	}
	for field := range fields {
		if _, ok := allowed[field]; !ok {
			return ProviderBridgeRequest{}, nil, "", errors.New("unknown request field")
		}
	}
	for field := range allowed {
		if _, ok := fields[field]; !ok {
			return ProviderBridgeRequest{}, nil, "", errors.New("missing request field")
		}
	}

	var wire struct {
		ResourceID string          `json:"resource_id"`
		Operation  string          `json:"operation"`
		ActionKey  string          `json:"action_key"`
		Input      json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ProviderBridgeRequest{}, nil, "", err
	}
	if err := validateBridgeString(wire.ResourceID, providerBridgeMaxResourceIDBytes, false); err != nil {
		return ProviderBridgeRequest{}, nil, "", err
	}
	if err := validateBridgeString(wire.ActionKey, providerBridgeMaxActionKeyBytes, false); err != nil {
		return ProviderBridgeRequest{}, nil, "", err
	}
	if _, ok := allowedProviderBridgeOperations[wire.Operation]; !ok {
		return ProviderBridgeRequest{}, nil, "", errors.New("operation is not supported")
	}
	operations, ok := bridge.grants[wire.ResourceID]
	if !ok {
		return ProviderBridgeRequest{}, nil, "", errors.New("resource is not granted")
	}
	if _, ok := operations[wire.Operation]; !ok {
		return ProviderBridgeRequest{}, nil, "", errors.New("operation is not granted")
	}
	if err := validateJSONObject(wire.Input); err != nil {
		return ProviderBridgeRequest{}, nil, "", err
	}
	if wire.Operation == "resource.sync" {
		var inputFields map[string]json.RawMessage
		if err := json.Unmarshal(wire.Input, &inputFields); err != nil || len(inputFields) != 0 {
			return ProviderBridgeRequest{}, nil, "", errors.New("resource.sync input must be empty")
		}
	}

	canonical, err := protocol.CanonicalizeJSON(body)
	if err != nil {
		return ProviderBridgeRequest{}, nil, "", err
	}
	request := ProviderBridgeRequest{
		ResourceID: wire.ResourceID,
		Operation:  wire.Operation,
		ActionKey:  wire.ActionKey,
		Input:      append(json.RawMessage(nil), wire.Input...),
	}
	return request, canonical, deriveProviderBridgeActionID(bridge.runID, request), nil
}

func (bridge *ProviderBridge) executeRequest(ctx context.Context, request ProviderBridgeRequest, canonical []byte, actionID string) (ProviderBridgeResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProviderBridgeResponse{}, err
	}
	bridge.mu.Lock()
	if bridge.closed {
		bridge.mu.Unlock()
		return ProviderBridgeResponse{}, errProviderBridgeClosed
	}
	if !bridge.started {
		bridge.mu.Unlock()
		return ProviderBridgeResponse{}, errProviderBridgeNotStarted
	}
	if bridge.lifetimeCtx.Err() != nil {
		bridge.mu.Unlock()
		return ProviderBridgeResponse{}, errProviderBridgeClosed
	}
	if entry, ok := bridge.replay[request.ActionKey]; ok {
		if !bytes.Equal(entry.canonical, canonical) {
			bridge.mu.Unlock()
			return ProviderBridgeResponse{}, errProviderBridgeConflict
		}
		select {
		case <-entry.done:
			result := cloneProviderBridgeResponse(entry.response)
			entryErr := entry.err
			bridge.mu.Unlock()
			if entryErr != nil {
				return ProviderBridgeResponse{}, entryErr
			}
			return result, nil
		default:
		}
		if bridge.replayWaiters >= providerBridgeMaxReplayWaiters {
			bridge.mu.Unlock()
			return ProviderBridgeResponse{}, errProviderBridgeReplayWaiterCapacity
		}
		bridge.replayWaiters++
		done := entry.done
		waitHook := bridge.onReplayWait
		lifetimeContext := bridge.lifetimeCtx
		bridge.mu.Unlock()
		if waitHook != nil {
			waitHook()
		}
		select {
		case <-done:
			bridge.mu.Lock()
			result := cloneProviderBridgeResponse(entry.response)
			entryErr := entry.err
			bridge.replayWaiters--
			bridge.mu.Unlock()
			if entryErr != nil {
				return ProviderBridgeResponse{}, entryErr
			}
			return result, nil
		case <-ctx.Done():
			bridge.mu.Lock()
			bridge.replayWaiters--
			bridge.mu.Unlock()
			return ProviderBridgeResponse{}, ctx.Err()
		case <-lifetimeContext.Done():
			bridge.mu.Lock()
			bridge.replayWaiters--
			bridge.mu.Unlock()
			return ProviderBridgeResponse{}, errProviderBridgeClosed
		}
	}
	if len(bridge.replay) >= providerBridgeMaxReplayEntries {
		bridge.mu.Unlock()
		return ProviderBridgeResponse{}, errProviderBridgeReplayCapacity
	}
	if bridge.activeExecutions >= providerBridgeMaxActiveExecutions {
		bridge.mu.Unlock()
		return ProviderBridgeResponse{}, errProviderBridgeExecutionCapacity
	}
	entry := &providerBridgeReplayEntry{
		actionKey: request.ActionKey,
		canonical: append([]byte(nil), canonical...),
		done:      make(chan struct{}),
	}
	bridge.replay[request.ActionKey] = entry
	execute := bridge.execute
	lifetimeContext := bridge.lifetimeCtx
	executionTimeout := bridge.executionTimeout
	if bridge.activeExecutions == 0 {
		bridge.executionDone = make(chan struct{})
	}
	bridge.activeExecutions++
	bridge.mu.Unlock()

	if err := providerBridgePreDispatchError(ctx, lifetimeContext); err != nil {
		bridge.finishProviderBridgeExecution(entry, ProviderBridgeResponse{}, err)
		return ProviderBridgeResponse{}, err
	}
	result, executeErr := executeProviderBridgeAction(ctx, lifetimeContext, executionTimeout, execute, actionID, request)
	bridge.finishProviderBridgeExecution(entry, result, executeErr)
	return result, executeErr
}

func providerBridgePreDispatchError(requestContext, lifetimeContext context.Context) error {
	if err := requestContext.Err(); err != nil {
		return err
	}
	if err := lifetimeContext.Err(); err != nil {
		return errProviderBridgeClosed
	}
	return nil
}

func (bridge *ProviderBridge) finishProviderBridgeExecution(entry *providerBridgeReplayEntry, response ProviderBridgeResponse, executionErr error) {
	bridge.mu.Lock()
	if executionErr != nil {
		if current, ok := bridge.replay[entry.actionKey]; ok && current == entry {
			delete(bridge.replay, entry.actionKey)
		}
	}
	entry.response = cloneProviderBridgeResponse(response)
	entry.err = executionErr
	close(entry.done)
	bridge.activeExecutions--
	if bridge.activeExecutions == 0 {
		close(bridge.executionDone)
	}
	bridge.mu.Unlock()
}

func executeProviderBridgeAction(
	requestContext context.Context,
	lifetimeContext context.Context,
	executionTimeout time.Duration,
	execute ExecuteProviderAction,
	actionID string,
	request ProviderBridgeRequest,
) (result ProviderBridgeResponse, executionErr error) {
	result = providerBridgeUnknownResponse("executor_error")
	if execute == nil {
		return result, nil
	}
	if err := providerBridgePreDispatchError(requestContext, lifetimeContext); err != nil {
		return ProviderBridgeResponse{}, err
	}

	executionContext, cancel := context.WithTimeout(requestContext, executionTimeout)
	stopLifetimeCancellation := context.AfterFunc(lifetimeContext, cancel)
	defer func() {
		stopLifetimeCancellation()
		cancel()
		if recover() != nil {
			result = providerBridgeUnknownResponse("executor_panic")
		}
	}()
	if err := providerBridgePreDispatchError(requestContext, lifetimeContext); err != nil {
		return ProviderBridgeResponse{}, err
	}

	candidate, err := execute(executionContext, actionID, request)
	if executionContext.Err() != nil {
		if errors.Is(executionContext.Err(), context.DeadlineExceeded) {
			return providerBridgeUnknownResponse("execution_deadline_exceeded"), nil
		}
		return providerBridgeUnknownResponse("execution_canceled"), nil
	}
	if err != nil {
		return providerBridgeUnknownResponse("executor_error"), nil
	}
	return normalizeProviderBridgeResponse(candidate), nil
}

func validateProviderBridgeOptions(options ProviderBridgeOptions) error {
	if validateBridgeString(options.RunID, providerBridgeMaxRunIDBytes, false) != nil ||
		options.Execute == nil || len(options.Grants) == 0 ||
		options.ExecutionTimeout < 0 || options.ExecutionTimeout > providerBridgeMaxExecutionTimeout {
		return errProviderBridgeInvalidOptions
	}
	seenResources := make(map[string]struct{}, len(options.Grants))
	for _, grant := range options.Grants {
		if validateProviderBridgeGrant(grant) != nil {
			return errProviderBridgeInvalidOptions
		}
		if _, duplicate := seenResources[grant.ResourceID]; duplicate || len(grant.Operations) == 0 {
			return errProviderBridgeInvalidOptions
		}
		seenResources[grant.ResourceID] = struct{}{}
	}
	return nil
}

func validateProviderBridgeGrant(grant protocol.ProviderGrant) error {
	if validateProviderBridgeUUID(grant.ResourceID) != nil {
		return errors.New("provider grant resource is not a UUID")
	}
	switch grant.Provider {
	case "github", "azure_devops":
	default:
		return errors.New("provider grant provider is unsupported")
	}
	switch grant.Kind {
	case "repository", "work_tracking", "ci":
	default:
		return errors.New("provider grant kind is unsupported")
	}
	if len(grant.Operations) == 0 {
		return errors.New("provider grant has no operations")
	}
	seenOperations := make(map[string]struct{}, len(grant.Operations))
	for _, operation := range grant.Operations {
		if _, supported := allowedProviderBridgeOperations[operation]; !supported {
			return errors.New("provider grant operation is unsupported")
		}
		if grant.Kind != "repository" && operation != "resource.sync" {
			return errors.New("provider grant kind does not allow operation")
		}
		if _, duplicate := seenOperations[operation]; duplicate {
			return errors.New("provider grant operation is duplicated")
		}
		seenOperations[operation] = struct{}{}
	}
	return nil
}

func validateProviderBridgeUUID(value string) error {
	if len(value) != 36 || !utf8.ValidString(value) {
		return errors.New("UUID is invalid")
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return errors.New("UUID is invalid")
			}
			continue
		}
		if character >= 'A' && character <= 'F' {
			return errors.New("UUID must use lowercase hexadecimal digits")
		}
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return errors.New("UUID is invalid")
		}
	}
	return nil
}

var allowedProviderBridgeOperations = map[string]struct{}{
	"resource.sync": {},
	"change.upsert": {},
	"change.update": {},
}

func validateBridgeString(value string, maxBytes int, allowEmpty bool) error {
	if !allowEmpty && value == "" {
		return errors.New("string is empty")
	}
	if len(value) > maxBytes || !utf8.ValidString(value) {
		return errors.New("string is invalid")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return errors.New("string contains a control character")
		}
	}
	return nil
}

func validateJSONObject(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("JSON value must be an object")
	}
	if err := validateUniqueJSON(data); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return errors.New("JSON value must be an object")
	}
	return nil
}

// validateUniqueJSON walks the entire JSON value so duplicate object keys are
// rejected before encoding/json or canonicalization can silently overwrite.
func validateUniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeProviderBridgeJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err == nil && token != nil {
			return errors.New("JSON contains more than one value")
		}
		if err != nil {
			return errors.New("invalid trailing JSON")
		}
		return errors.New("invalid trailing JSON")
	}
	return nil
}

func consumeProviderBridgeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate JSON object field")
			}
			seen[key] = struct{}{}
			if err := consumeProviderBridgeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeProviderBridgeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("unterminated JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func deriveProviderBridgeActionID(runID string, request ProviderBridgeRequest) string {
	hash := sha256.New()
	for index, value := range []string{runID, request.ResourceID, request.Operation, request.ActionKey} {
		if index > 0 {
			_, _ = hash.Write([]byte{0})
		}
		_, _ = hash.Write([]byte(value))
	}
	digest := hash.Sum(nil)
	digest[6] = digest[6]&0x0f | 0x50
	digest[8] = digest[8]&0x3f | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(digest[0:4]),
		hex.EncodeToString(digest[4:6]),
		hex.EncodeToString(digest[6:8]),
		hex.EncodeToString(digest[8:10]),
		hex.EncodeToString(digest[10:16]),
	)
}

func normalizeProviderBridgeResponse(response ProviderBridgeResponse) ProviderBridgeResponse {
	if response.Outcome != ProviderBridgeOutcomeSucceeded && response.Outcome != ProviderBridgeOutcomeFailed && response.Outcome != ProviderBridgeOutcomeUnknown {
		return providerBridgeUnknownResponse("invalid_executor_response")
	}
	if response.Outcome == ProviderBridgeOutcomeSucceeded && response.FailureCode != "" {
		return providerBridgeUnknownResponse("invalid_executor_response")
	}
	if (response.Outcome == ProviderBridgeOutcomeFailed || response.Outcome == ProviderBridgeOutcomeUnknown) && response.FailureCode == "" {
		return providerBridgeUnknownResponse("invalid_executor_response")
	}
	if response.FailureCode != "" && validateBridgeString(response.FailureCode, providerBridgeMaxFailureCodeBytes, true) != nil {
		return providerBridgeUnknownResponse("invalid_executor_failure")
	}
	if len(response.Result) > providerBridgeMaxResponseBytes {
		return providerBridgeUnknownResponse("result_too_large")
	}
	if len(response.Result) > 0 && !json.Valid(response.Result) {
		return providerBridgeUnknownResponse("invalid_executor_result")
	}
	return cloneProviderBridgeResponse(response)
}

func providerBridgeUnknownResponse(code string) ProviderBridgeResponse {
	return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeUnknown, FailureCode: code}
}

func cloneProviderBridgeResponse(response ProviderBridgeResponse) ProviderBridgeResponse {
	response.Result = append(json.RawMessage(nil), response.Result...)
	return response
}

type providerBridgeHTTPResponse struct {
	ActionID    string                `json:"action_id"`
	Outcome     ProviderBridgeOutcome `json:"outcome"`
	Result      json.RawMessage       `json:"result,omitempty"`
	FailureCode string                `json:"failure_code,omitempty"`
}

func writeProviderBridgeResponse(writer http.ResponseWriter, actionID string, response ProviderBridgeResponse) {
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(providerBridgeHTTPResponse{
		ActionID: actionID, Outcome: response.Outcome, Result: response.Result, FailureCode: response.FailureCode,
	})
}

func writeProviderBridgeError(writer http.ResponseWriter, status int, code string) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}{Error: struct {
		Code string `json:"code"`
	}{Code: code}})
}
