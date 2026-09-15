package pi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestProviderBridgeExtensionSourceIsDeterministicAndCredentialFree(t *testing.T) {
	first := []protocol.ProviderGrant{
		{ResourceID: "22222222-2222-4222-8222-222222222222", Provider: "github", Kind: "repository", Operations: []string{"change.update", "resource.sync"}},
		{ResourceID: "11111111-1111-4111-8111-111111111111", Provider: "github", Kind: "repository", Operations: []string{"change.upsert", "resource.sync"}},
	}
	second := []protocol.ProviderGrant{first[1], first[0]}
	left, err := providerBridgeExtensionSource(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := providerBridgeExtensionSource(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatal("extension source depends on grant order")
	}
	source := string(left)
	for _, required := range []string{
		"symmetry_resource_sync",
		"symmetry_change_upsert",
		"symmetry_change_update",
		"action_key: toolCallId",
		ProviderBridgeURLEnvironment,
		ProviderBridgeNonceEnvironment,
		"additionalProperties: false",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("extension source omits %q", required)
		}
	}
	for _, forbidden := range []string{"Authorization", "provider_token", "source_branch", "target_branch", "pull_request_url"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("extension source contains forbidden authority field %q", forbidden)
		}
	}
}

func TestMaterializeProviderBridgeExtensionIsImmutable(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "extensions")
	first, err := MaterializeProviderBridgeExtension(directory, testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	second, err := MaterializeProviderBridgeExtension(directory, testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !filepath.IsAbs(first.Path) || len(first.SHA256) != 64 {
		t.Fatalf("materialized extension = %#v then %#v", first, second)
	}
	if err := os.WriteFile(first.Path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeProviderBridgeExtension(directory, testProviderGrants()); err == nil {
		t.Fatal("materialization replaced a conflicting immutable extension")
	}
}

func TestMaterializeProviderBridgeExtensionRejectsExistingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink may require an elevated Windows token")
	}
	directory := filepath.Join(t.TempDir(), "extensions")
	source, err := providerBridgeExtensionSource(testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	path := filepath.Join(directory, providerBridgeExtensionPrefix+hex.EncodeToString(digest[:])+providerBridgeExtensionSuffix)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.ts")
	if err := os.WriteFile(target, source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeProviderBridgeExtension(directory, testProviderGrants()); err == nil {
		t.Fatal("materialization accepted an existing symlink")
	}
}

func TestPiRPCArgsWithProviderExtensionUsesOnlyDaemonOwnedLoader(t *testing.T) {
	extension := filepath.Join(t.TempDir(), "provider.ts")
	if err := os.WriteFile(extension, []byte("export default function () {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := piRPCArgsWithProviderExtension([]string{"--provider", "openai", "--no-extensions", "--offline"}, nil, extension)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--mode", "rpc", "--provider", "openai", "--offline", "--no-extensions", "--extension", extension}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider extension args = %#v, want %#v", got, want)
	}
	for _, profile := range [][]string{{"--no-tools"}, {"-nt"}, {"--tools", "write"}, {"-t", "write"}} {
		if _, err := piRPCArgsWithProviderExtension(profile, nil, extension); err == nil {
			t.Fatalf("profile tool override %#v was accepted", profile)
		}
	}
}

func TestAdapterBindsAndRevokesDaemonOwnedProviderBridge(t *testing.T) {
	extension, err := MaterializeProviderBridgeExtension(filepath.Join(t.TempDir(), "extensions"), testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	process := newFakeNativeProcess()
	lifecycle := &fakeProviderBridgeLifecycle{}
	var invocation execution.Invocation
	adapter := &Adapter{
		executable: "pi-test",
		startProcess: func(_ context.Context, got execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			invocation = got
			process.sink = sink
			return process, nil
		},
	}
	access := &protocol.ProviderAccess{Path: "/api/v1/provider-actions", Token: "provider-token-do-not-forward", Grants: testProviderGrants()}
	session := startPiSession(t, adapter, harness.StartRequest{
		Workspace:      t.TempDir(),
		ProviderAccess: access,
		ProviderBridge: &harness.ProviderBridgeLaunch{
			URL:             "http://127.0.0.1:43123/v1/actions",
			Nonce:           strings.Repeat("a", 64),
			ExtensionPath:   extension.Path,
			ExtensionSHA256: extension.SHA256,
			Lifecycle:       lifecycle,
		},
		Invocation: execution.Invocation{Args: []string{"--offline", "--no-extensions"}, Env: []string{"PATH=test"}},
	}, &recordingHarnessSink{})

	if got, want := invocation.Args, []string{"--mode", "rpc", "--offline", "--no-extensions", "--extension", extension.Path}; !reflect.DeepEqual(got, want) {
		t.Fatalf("provider bridge argv = %#v, want %#v", got, want)
	}
	joinedEnvironment := strings.Join(invocation.Env, "\n")
	if !strings.Contains(joinedEnvironment, ProviderBridgeURLEnvironment+"=http://127.0.0.1:43123/v1/actions") ||
		!strings.Contains(joinedEnvironment, ProviderBridgeNonceEnvironment+"="+strings.Repeat("a", 64)) ||
		strings.Contains(joinedEnvironment, access.Token) {
		t.Fatalf("provider bridge environment is invalid: %q", joinedEnvironment)
	}
	pid, identity, bindCalls, closeCalls := lifecycle.snapshot()
	if pid != 42 || identity != "test:42" || bindCalls != 1 || closeCalls != 0 {
		t.Fatalf("provider bridge lifecycle after Start = pid:%d identity:%q bind:%d close:%d", pid, identity, bindCalls, closeCalls)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, bindCalls, closeCalls = lifecycle.snapshot()
	if bindCalls != 1 || closeCalls != 1 || process.terminateCount() != 1 {
		t.Fatalf("provider bridge cleanup = bind:%d close:%d terminate:%d", bindCalls, closeCalls, process.terminateCount())
	}
}

func TestAdapterProviderBridgeBindFailureReturnsOwnedSessionForCleanup(t *testing.T) {
	extension, err := MaterializeProviderBridgeExtension(filepath.Join(t.TempDir(), "extensions"), testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("peer identity unavailable")
	process := newFakeNativeProcess()
	lifecycle := &fakeProviderBridgeLifecycle{bindErr: want}
	adapter := &Adapter{
		executable: "pi-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	started, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace:      t.TempDir(),
		ProviderAccess: &protocol.ProviderAccess{Path: "/api/v1/provider-actions", Token: "provider-token", Grants: testProviderGrants()},
		ProviderBridge: &harness.ProviderBridgeLaunch{
			URL:             "http://127.0.0.1:43123/v1/actions",
			Nonce:           strings.Repeat("b", 64),
			ExtensionPath:   extension.Path,
			ExtensionSHA256: extension.SHA256,
			Lifecycle:       lifecycle,
		},
	}, &recordingHarnessSink{})
	if !errors.Is(err, want) || started == nil {
		t.Fatalf("Start() = (%T, %v), want owned session and bind failure", started, err)
	}
	if closeErr := started.Close(context.Background()); closeErr != nil {
		t.Fatal(closeErr)
	}
	if _, waitErr := started.Wait(context.Background()); waitErr != nil {
		t.Fatal(waitErr)
	}
	_, _, bindCalls, closeCalls := lifecycle.snapshot()
	if bindCalls != 1 || closeCalls != 1 || process.terminateCount() != 1 {
		t.Fatalf("failed bind cleanup = bind:%d close:%d terminate:%d", bindCalls, closeCalls, process.terminateCount())
	}
}

func TestAdapterRejectsInvalidProviderBridgeBeforeProcessStart(t *testing.T) {
	extension, err := MaterializeProviderBridgeExtension(filepath.Join(t.TempDir(), "extensions"), testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	validAccess := &protocol.ProviderAccess{Path: "/api/v1/provider-actions", Token: "provider-token", Grants: testProviderGrants()}
	validLaunch := harness.ProviderBridgeLaunch{
		URL:             "http://127.0.0.1:43123/v1/actions",
		Nonce:           strings.Repeat("c", 64),
		ExtensionPath:   extension.Path,
		ExtensionSHA256: extension.SHA256,
		Lifecycle:       &fakeProviderBridgeLifecycle{},
	}
	tests := []struct {
		name   string
		access *protocol.ProviderAccess
		launch *harness.ProviderBridgeLaunch
		env    []string
	}{
		{name: "launch without access", launch: &validLaunch},
		{name: "access without launch", access: validAccess},
		{name: "changed digest", access: validAccess, launch: func() *harness.ProviderBridgeLaunch {
			value := validLaunch
			value.ExtensionSHA256 = strings.Repeat("d", 64)
			return &value
		}()},
		{name: "changed grants", access: &protocol.ProviderAccess{Path: validAccess.Path, Token: validAccess.Token, Grants: []protocol.ProviderGrant{{ResourceID: testProviderResourceID, Provider: "github", Kind: "repository", Operations: []string{"resource.sync"}}}}, launch: &validLaunch},
		{name: "reserved environment", access: validAccess, launch: &validLaunch, env: []string{ProviderBridgeNonceEnvironment + "=caller-value"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			adapter := &Adapter{
				executable: "pi-test",
				startProcess: func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
					called = true
					return newFakeNativeProcess(), nil
				},
			}
			started, err := adapter.Start(context.Background(), harness.StartRequest{
				Workspace:      t.TempDir(),
				ProviderAccess: test.access,
				ProviderBridge: test.launch,
				Invocation:     execution.Invocation{Env: test.env},
			}, &recordingHarnessSink{})
			if err == nil || started != nil || called {
				t.Fatalf("Start() = (%T, %v), process started=%t", started, err, called)
			}
			if strings.Contains(err.Error(), validAccess.Token) || strings.Contains(err.Error(), "caller-value") {
				t.Fatalf("validation error leaked sensitive configuration: %v", err)
			}
		})
	}
}

func TestProviderBridgeExtensionRejectsInvalidInputs(t *testing.T) {
	if _, err := MaterializeProviderBridgeExtension("relative", testProviderGrants()); err == nil {
		t.Fatal("relative extension directory was accepted")
	}
	if _, err := providerBridgeExtensionSource(nil); err == nil {
		t.Fatal("empty grants were accepted")
	}
	duplicate := append(testProviderGrants(), testProviderGrants()[0])
	if _, err := providerBridgeExtensionSource(duplicate); err == nil {
		t.Fatal("duplicate resource grant was accepted")
	}
}

func TestGeneratedProviderBridgeExtensionLoadsInNativePi(t *testing.T) {
	if os.Getenv("SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE") != "1" {
		t.Skip("set SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE=1 to load the generated extension in native Pi")
	}
	extension, err := MaterializeProviderBridgeExtension(filepath.Join(t.TempDir(), "extensions"), testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	args, err := piRPCArgsWithProviderExtension([]string{"--offline"}, nil, extension.Path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := execution.RunBoundedCommand(ctx, execution.Invocation{
		Program: DefaultExecutable,
		Args:    args,
		Dir:     t.TempDir(),
		Env: append(os.Environ(),
			ProviderBridgeURLEnvironment+"=http://127.0.0.1:1/v1/actions",
			ProviderBridgeNonceEnvironment+"="+strings.Repeat("a", 64),
		),
		InitialInput:           []byte("{\"id\":\"state-1\",\"type\":\"get_state\"}\n"),
		CloseInputAfterInitial: true,
	}, 1<<20)
	if err != nil {
		t.Fatalf("native Pi extension smoke error = %v; output=%s", err, output)
	}
	if !bytes.Contains(output, []byte(`"id":"state-1"`)) || !bytes.Contains(output, []byte(`"type":"response"`)) {
		t.Fatalf("native Pi extension smoke omitted correlated get_state response: %s", output)
	}
}

func TestNativePiProviderBridgeLifecycle(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("native Pi provider bridge smoke supports Linux and Windows only")
	}
	if os.Getenv("SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE") != "1" {
		t.Skip("set SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE=1 to run the native provider bridge smoke")
	}
	executable := strings.TrimSpace(os.Getenv(nativeSmokeExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute Pi 0.85.1 executable", nativeSmokeExecutableEnv)
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	sessionDirectory := filepath.Join(root, "sessions")
	for _, directory := range []string{workspace, sessionDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	extension, err := MaterializeProviderBridgeExtension(filepath.Join(root, "extensions"), testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:               "native-provider-bridge-smoke",
		Grants:              testProviderGrants(),
		RequirePeerIdentity: true,
		Execute: func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			t.Fatal("transport-only smoke unexpectedly executed a provider action")
			return ProviderBridgeResponse{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	endpoint, err := bridge.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	startContext, startCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer startCancel()
	started, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace: workspace,
		ProviderAccess: &protocol.ProviderAccess{
			Path:   "/api/v1/provider-actions",
			Token:  "transport-smoke-provider-token",
			Grants: testProviderGrants(),
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
				"--offline",
				"--no-skills",
				"--no-prompt-templates",
				"--no-themes",
				"--no-context-files",
				"--no-approve",
				"--no-builtin-tools",
				"--session-dir", sessionDirectory,
				"--provider", "openai",
				"--model", "gpt-4o",
			},
			Env: nativePiSmokeEnvironment(t, root),
		},
	}, &recordingHarnessSink{})
	if err != nil {
		t.Fatalf("start native Pi provider bridge transport: %v", err)
	}
	staged, ok := started.(harness.StagedSession)
	if !ok {
		t.Fatalf("session = %T, want staged session", started)
	}
	if _, err := staged.Open(startContext); err != nil {
		t.Fatalf("open native Pi provider bridge transport: %v", err)
	}
	if err := staged.Close(startContext); err != nil {
		t.Fatalf("close native Pi provider bridge transport: %v", err)
	}
	result, err := staged.Wait(startContext)
	if err != nil {
		t.Fatalf("wait native Pi provider bridge transport: %v", err)
	}
	if !result.Process.Terminated {
		t.Fatalf("native Pi provider bridge process was not terminated: %+v", result.Process)
	}
}

type fakeProviderBridgeLifecycle struct {
	mutex      sync.Mutex
	pid        int
	identity   string
	bindCalls  int
	closeCalls int
	bindErr    error
	closeErr   error
}

func (lifecycle *fakeProviderBridgeLifecycle) BindProcess(pid int, identity string) error {
	lifecycle.mutex.Lock()
	defer lifecycle.mutex.Unlock()
	lifecycle.bindCalls++
	lifecycle.pid = pid
	lifecycle.identity = identity
	return lifecycle.bindErr
}

func (lifecycle *fakeProviderBridgeLifecycle) Close(context.Context) error {
	lifecycle.mutex.Lock()
	defer lifecycle.mutex.Unlock()
	lifecycle.closeCalls++
	return lifecycle.closeErr
}

func (lifecycle *fakeProviderBridgeLifecycle) snapshot() (int, string, int, int) {
	lifecycle.mutex.Lock()
	defer lifecycle.mutex.Unlock()
	return lifecycle.pid, lifecycle.identity, lifecycle.bindCalls, lifecycle.closeCalls
}
