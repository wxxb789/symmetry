//go:build linux

package pi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// A real legacy launch cannot supply the retained-v2 peer required by Linux.
// This proves refusal and cleanup, not installed-v2 owner/action acceptance.
func TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction(t *testing.T) {
	if os.Getenv(nativeProviderBridgeActionSmokeEnv) != "1" {
		t.Skip("set SYMMETRY_PI_PROVIDER_ACTION_SMOKE=1 to run the native legacy refusal test")
	}
	executable := strings.TrimSpace(os.Getenv(nativeSmokeExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute Pi executable", nativeSmokeExecutableEnv)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("native Pi executable must be an existing regular file")
	}

	var providerRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		providerRequests.Add(1)
		http.Error(response, "legacy launch must not reach the provider", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	sessionDirectory := filepath.Join(root, "sessions")
	for _, directory := range []string{workspace, sessionDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	environment := nativePiRepositoryTaskLoopbackEnvironment(t, root, server.URL+"/v1")
	nativePiRetainedCheckVersion(t, executable, workspace, environment)
	grants := testProviderGrants()
	extension, err := MaterializeProviderBridgeExtension(filepath.Join(root, "extensions"), grants)
	if err != nil {
		t.Fatal(err)
	}
	var executeCalls atomic.Int32
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:               "native-legacy-provider-refusal",
		Grants:              grants,
		RequirePeerIdentity: true,
		Execute: func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			executeCalls.Add(1)
			return ProviderBridgeResponse{}, errors.New("legacy launch reached provider execution")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := bridge.Close(context.Background()); err != nil {
			t.Errorf("close legacy refusal bridge: %v", err)
		}
	})
	endpoint, err := bridge.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	started, startErr := NewAdapter(executable).Start(ctx, harness.StartRequest{
		Workspace: workspace,
		ProviderAccess: &protocol.ProviderAccess{
			Path:   "/api/v1/provider-actions",
			Token:  "native-provider-action-token-not-forwarded",
			Grants: grants,
		},
		ProviderBridge: &harness.ProviderBridgeLaunch{
			URL:             endpoint.URL,
			Nonce:           endpoint.Nonce,
			ExtensionPath:   extension.Path,
			ExtensionSHA256: extension.SHA256,
			Lifecycle:       bridge,
		},
		Invocation: execution.Invocation{
			Args: []string{
				"--no-skills", "--no-prompt-templates", "--no-themes",
				"--no-context-files", "--no-approve", "--no-builtin-tools",
				"--session-dir", sessionDirectory,
				"--provider", nativePiLoopbackProvider,
				"--model", nativePiLoopbackModel,
				"--thinking", nativePiLoopbackThinking,
			},
			Env: environment,
		},
	}, &recordingHarnessSink{})
	closed := false
	if started != nil {
		t.Cleanup(func() {
			if closed {
				return
			}
			cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
			defer cleanupCancel()
			if err := started.Close(cleanupContext); err != nil {
				t.Errorf("cleanup refused legacy Pi session: %v", err)
			}
			if _, err := started.Wait(cleanupContext); err != nil {
				t.Errorf("wait for refused legacy Pi cleanup: %v", err)
			}
		})
	}
	if !errors.Is(startErr, errors.ErrUnsupported) {
		t.Fatalf("legacy Pi Start() error = %v, want unsupported retained-v2 peer", startErr)
	}
	staged, ok := started.(harness.StagedSession)
	if !ok {
		t.Fatalf("refused legacy Pi session = %T, want an owned staged session for cleanup", started)
	}
	pid, identity := staged.ProcessDetails()
	if pid <= 0 || identity == "" {
		t.Fatal("refused legacy Pi session lost its owned process identity")
	}
	cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	defer cleanupCancel()
	if err := staged.Close(cleanupContext); err != nil {
		t.Fatalf("close refused legacy Pi session: %v", err)
	}
	result, err := staged.Wait(cleanupContext)
	if err != nil {
		t.Fatalf("wait for refused legacy Pi session: %v", err)
	}
	closed = true
	if result.Kind != harness.ResultFailed || result.Semantic != nil || !result.Process.Terminated ||
		result.Process.SinkError != nil || result.Process.OutputError != nil ||
		result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatalf("refused legacy Pi result = %+v, want failed with clean bounded process stop", result)
	}
	if current, err := platform.ProcessIdentity(pid); err == nil && current == identity {
		t.Fatal("refused legacy Pi process survived Close/Wait")
	}
	if err := bridge.Close(cleanupContext); err != nil {
		t.Fatalf("join refused legacy Pi bridge cleanup: %v", err)
	}
	if calls, requests := executeCalls.Load(), providerRequests.Load(); calls != 0 || requests != 0 {
		t.Fatalf("legacy Pi effects = provider actions:%d provider HTTP requests:%d, want zero", calls, requests)
	}
}
