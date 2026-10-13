//go:build linux

package opencode

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

// This launches the pinned native server without an installed-v2 grant. It
// proves zero-effect legacy refusal/cleanup, not native owner acceptance.
func TestLinuxLegacyOpenCodeRefusesBeforeHealthRequest(t *testing.T) {
	if os.Getenv(nativeSyntheticToolEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL=1 to run the native legacy refusal test")
	}
	executable := strings.TrimSpace(os.Getenv(nativeSyntheticToolExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute OpenCode %s executable", nativeSyntheticToolExecutableEnv, TestedVersion)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("native OpenCode executable must be an existing regular file")
	}

	var providerRequests atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		providerRequests.Add(1)
		http.Error(response, "legacy launch must not reach the provider", http.StatusServiceUnavailable)
	}))
	t.Cleanup(gateway.Close)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	environment := nativeOpenCodeRepositoryTaskEnvironment(t, root, gateway.URL+"/v1")
	nativeOpenCodeRepositoryTaskCheckEnvironment(t, environment, gateway.URL+"/v1")
	nativeOpenCodeRepositoryTaskCheckVersion(t, executable, workspace, environment)

	adapter := NewAdapter(executable)
	productionAPI := adapter.newAPI
	var verifierCalls, unsupportedPeers atomic.Int32
	observer := &nativeLegacyOpenCodeHTTPObserver{}
	adapter.newAPI = func(config Config) (api, error) {
		productionVerifier := config.VerifyConnection
		if productionVerifier == nil {
			return nil, errors.New("native OpenCode client omitted its production peer verifier")
		}
		config.VerifyConnection = func(ctx context.Context, connection net.Conn) error {
			verifierCalls.Add(1)
			err := productionVerifier(ctx, connection)
			if errors.Is(err, errors.ErrUnsupported) {
				unsupportedPeers.Add(1)
			}
			return err
		}
		configured, err := productionAPI(config)
		if err != nil {
			return nil, err
		}
		client, ok := configured.(*Client)
		if !ok || client == nil {
			return nil, errors.New("native OpenCode API factory did not return its production HTTP client")
		}
		// Observe the real native endpoint without replacing its verifier,
		// connection, credentials, or production transport.
		observer.transport = client.httpClient.Transport
		client.httpClient.Transport = observer
		t.Cleanup(client.httpClient.CloseIdleConnections)
		return client, nil
	}

	sink := &nativeOpenCodeRepositoryTaskSink{}
	ctx, cancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer cancel()
	persistedPID, persistedIdentity := 0, ""
	started, startErr := adapter.Start(ctx, harness.StartRequest{
		Workspace:  workspace,
		Invocation: execution.Invocation{Env: environment},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			persistedPID, persistedIdentity = pid, identity
			return nil
		},
	}, sink)
	closed := false
	if started != nil {
		t.Cleanup(func() {
			if closed {
				return
			}
			cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
			defer cleanupCancel()
			if err := started.Close(cleanupContext); err != nil {
				t.Errorf("cleanup refused legacy OpenCode session: %v", err)
			}
			if _, err := started.Wait(cleanupContext); err != nil {
				t.Errorf("wait for refused legacy OpenCode cleanup: %v", err)
			}
		})
	}
	if startErr != nil {
		t.Fatalf("start legacy native OpenCode server: %v", startErr)
	}
	staged, ok := started.(harness.StagedSession)
	if !ok {
		t.Fatalf("legacy OpenCode session = %T, want an owned staged session", started)
	}
	pid, identity := staged.ProcessDetails()
	if pid <= 0 || identity == "" || pid != persistedPID || identity != persistedIdentity {
		t.Fatal("legacy OpenCode process identity was not persisted before session exposure")
	}
	handle, openErr := staged.Open(ctx)
	if !errors.Is(openErr, ErrPeerOwnership) || !errors.Is(openErr, errors.ErrUnsupported) || handle.ID != "" {
		t.Fatalf("legacy OpenCode Open() = %+v, %v, want unsupported peer before native session creation", handle, openErr)
	}
	cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	defer cleanupCancel()
	if err := staged.Close(cleanupContext); err != nil {
		t.Fatalf("close refused legacy OpenCode session: %v", err)
	}
	result, err := staged.Wait(cleanupContext)
	if err != nil {
		t.Fatalf("wait for refused legacy OpenCode session: %v", err)
	}
	closed = true
	if result.Kind != harness.ResultUnknown || result.Semantic != nil || !result.Process.Terminated ||
		result.Process.SinkError != nil || result.Process.OutputError != nil ||
		result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatalf("refused legacy OpenCode result = %+v, want unknown with clean bounded process stop", result)
	}
	if current, err := platform.ProcessIdentity(pid); err == nil && current == identity {
		t.Fatal("refused legacy OpenCode process survived Close/Wait")
	}
	if calls, unsupported := verifierCalls.Load(), unsupportedPeers.Load(); calls != 1 || unsupported != 1 {
		t.Fatalf("native OpenCode peer verification = calls:%d unsupported:%d, want one real unsupported peer", calls, unsupported)
	}
	if attempts, other, connections, headers := observer.attempts.Load(), observer.nonHealth.Load(), observer.connections.Load(), observer.headers.Load(); attempts != 1 || other != 0 || connections != 0 || headers != 0 {
		t.Fatalf("native OpenCode HTTP = health attempts:%d other attempts:%d exposed connections:%d written headers:%d, want one rejected health attempt and zero HTTP effects", attempts, other, connections, headers)
	}
	if requests := providerRequests.Load(); requests != 0 {
		t.Fatalf("legacy OpenCode provider HTTP requests = %d, want zero", requests)
	}
	if sink.has(harness.EventSessionStarted) || sink.has(harness.EventTaskResult) {
		t.Fatalf("refused legacy OpenCode emitted native session/task evidence: %#v", sink.snapshot())
	}
}

// GotConn fires only when the production transport exposes a verified
// connection to HTTP; no GotConn and no WroteHeaders prove refusal before HTTP.
type nativeLegacyOpenCodeHTTPObserver struct {
	transport   http.RoundTripper
	attempts    atomic.Int32
	nonHealth   atomic.Int32
	connections atomic.Int32
	headers     atomic.Int32
}

func (observer *nativeLegacyOpenCodeHTTPObserver) RoundTrip(request *http.Request) (*http.Response, error) {
	observer.attempts.Add(1)
	if request.Method != http.MethodGet || request.URL.Path != "/api/health" {
		observer.nonHealth.Add(1)
	}
	trace := &httptrace.ClientTrace{
		GotConn:      func(httptrace.GotConnInfo) { observer.connections.Add(1) },
		WroteHeaders: func() { observer.headers.Add(1) },
	}
	return observer.transport.RoundTrip(request.WithContext(httptrace.WithClientTrace(request.Context(), trace)))
}

func (observer *nativeLegacyOpenCodeHTTPObserver) CloseIdleConnections() {
	if transport, ok := observer.transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}
