//go:build windows

package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	nativePromotionProofEnabledEnv     = "SYMMETRY_OPENCODE_PROMOTION_PROOF"
	nativePromotionProofExecutableEnv  = "SYMMETRY_OPENCODE_PROMOTION_PROOF_EXECUTABLE"
	nativePromotionProofReceiptEnv     = "SYMMETRY_OPENCODE_PROMOTION_PROOF_RECEIPT"
	nativePromotionProofModelsEnv      = "SYMMETRY_OPENCODE_PROMOTION_PROOF_MODELS"
	nativePromotionProofSourceEnv      = "SYMMETRY_OPENCODE_PROMOTION_PROOF_SOURCE_CONFIG"
	nativePromotionProofCredentialEnv  = "TENCENT_GLM_API_KEY"
	nativePromotionProofProvider       = "tencent-litellm"
	nativePromotionProofModel          = "deepseek-v4.1-flash-fast"
	nativePromotionProofTimeout        = 2 * time.Minute
	nativePromotionProofFirstEventWait = 60 * time.Second
	nativePromotionProofPollInterval   = 500 * time.Millisecond
	nativePromotionProofGoal           = "Output the integers from 1 through 10000, one per line, with no other text. Do not use tools."
	nativePromotionProofSuccessGoal    = "Reply exactly OPENCODE_NATIVE_SUCCESS. Do not use tools."
	// The admitted alias a Goal admission always carries (admission.model_profile ==
	// runtime.agent_profile); the proof must exercise a non-empty profile.
	nativePromotionProofProfile = "implementation-default"
)

// nativePromotionProofOperation records one admitted turn's observable evidence.
type nativePromotionProofOperation struct {
	SessionStarted  bool
	NativeFrames    int
	WaitTurn        string
	ReadbackOutcome string
	ResultKind      string
	ControlApplied  bool
	Terminated      bool
}

func (operation nativePromotionProofOperation) receiptFields() map[string]any {
	return map[string]any{
		"session_started":  operation.SessionStarted,
		"native_frames":    operation.NativeFrames,
		"readiness":        map[string]any{"source_pid_accepted": true, "foreign_pid_refused": true},
		"wait_turn":        operation.WaitTurn,
		"readback_outcome": operation.ReadbackOutcome,
		"result_kind":      operation.ResultKind,
		"control_applied":  operation.ControlApplied,
		"close_terminated": operation.Terminated,
	}
}

type nativePromotionProofSink struct {
	mu             sync.Mutex
	sessionStarted bool
	nativeFrames   int
	firstFrame     chan struct{}
	firstOnce      sync.Once
}

func (sink *nativePromotionProofSink) Handle(_ context.Context, event harness.Event) error {
	sink.mu.Lock()
	if event.Kind == harness.EventSessionStarted {
		sink.sessionStarted = true
	}
	if event.Kind == harness.EventNativeFrame {
		sink.nativeFrames++
	}
	sink.mu.Unlock()
	if event.Kind == harness.EventNativeFrame {
		sink.firstOnce.Do(func() { close(sink.firstFrame) })
	}
	return nil
}

func (sink *nativePromotionProofSink) snapshot() (bool, int) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return sink.sessionStarted, sink.nativeFrames
}

// TestNativePromotionProofOnThePinnedWindowsBlob is the opt-in Phase-1 Windows promotion proof:
// two admitted turns behind a non-empty profile, with verified outcome readbacks, on the pinned PE.
func TestNativePromotionProofOnThePinnedWindowsBlob(t *testing.T) {
	if os.Getenv(nativePromotionProofEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_PROMOTION_PROOF=1 to run the Phase-1 Windows promotion proof")
	}
	executable := nativePromotionProofEnvPath(t, nativePromotionProofExecutableEnv, "the pinned OpenCode "+TestedVersion+" executable")
	receiptPath := nativePromotionProofEnvPath(t, nativePromotionProofReceiptEnv, "the receipt path")
	modelsPath := nativePromotionProofEnvPath(t, nativePromotionProofModelsEnv, "the operator OMP models config")
	sourcePath := nativePromotionProofEnvPath(t, nativePromotionProofSourceEnv, "the operator OpenCode provider config")
	digest, size := nativePromotionProofExecutableDigest(t, executable)
	if digest != TestedExecutableHash {
		t.Fatalf("promotion proof executable digest = %q, want %q", digest, TestedExecutableHash)
	}
	credential := nativePromotionProofCredential(t, modelsPath)

	root := t.TempDir()
	environment := nativeOpenCodeSmokeEnvironment(t, root)
	configPath := filepath.Join(root, "config", "opencode.json")
	if err := os.MkdirAll(filepath.Join(root, "managed"), 0o700); err != nil {
		t.Fatalf("create promotion proof managed directory: %v", err)
	}
	nativePromotionProofWriteConfig(t, configPath, sourcePath)
	environment = nativeOpenCodeConfigEnvironment(environment, configPath, filepath.Dir(configPath), filepath.Join(root, "managed"), nativePromotionProofCredentialEnv+"="+credential)
	versionLine := nativeOpenCodeRepositoryTaskCheckVersionLine(t, executable, root, environment, "opencode v"+TestedVersion, nativePromotionProofTimeout)

	succeeded := nativePromotionProofRunTurn(t, executable, filepath.Join(root, "succeeded-workspace"), environment, nativePromotionProofSuccessGoal, false)
	interrupted := nativePromotionProofRunTurn(t, executable, filepath.Join(root, "interrupted-workspace"), environment, nativePromotionProofGoal, true)

	receipt := map[string]any{
		"goos":                     "windows",
		"tested_version":           TestedVersion,
		"executable_sha256":        digest,
		"executable_size":          size,
		"version_line":             versionLine,
		"relay_host_recorded":      false,
		"model_profile_alias_used": true,
		"operations": map[string]any{
			"succeeded":   succeeded.receiptFields(),
			"interrupted": interrupted.receiptFields(),
		},
		"delivered_sha256": nativePromotionProofDeliveredDigests(t),
	}
	encoded, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatalf("encode promotion proof receipt: %v", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(receiptPath, encoded, 0o600); err != nil {
		t.Fatalf("write promotion proof receipt: %v", err)
	}
	sum := sha256.Sum256(encoded)
	t.Logf("promotion proof receipt written with sha256 %s", "sha256:"+hex.EncodeToString(sum[:]))
}

// nativePromotionProofEnvPath keeps the operator-proof harness free of host filesystem
// layout: every operator input arrives as one explicit absolute environment path.
func nativePromotionProofEnvPath(t *testing.T, name, what string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" || !filepath.IsAbs(value) {
		t.Fatalf("%s must name the absolute %s", name, what)
	}
	return value
}

// nativePromotionProofRunTurn drives one admitted turn behind a non-empty ModelProfile
// through start, observed events, an optional applied cancel and a verified WaitTurn terminal.
func nativePromotionProofRunTurn(t *testing.T, executable, workspace string, environment []string, goal string, cancelTurn bool) nativePromotionProofOperation {
	t.Helper()
	operation := nativePromotionProofOperation{}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create promotion proof workspace: %v", err)
	}
	sink := &nativePromotionProofSink{firstFrame: make(chan struct{})}
	startContext, startCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	defer startCancel()
	session, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace:      workspace,
		ModelProfile:   nativePromotionProofProfile,
		Invocation:     execution.Invocation{Env: environment},
		PersistProcess: acceptNativeOpenCodeProcess,
	}, sink)
	if err != nil {
		t.Fatalf("start pinned OpenCode promotion proof: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Errorf("cleanup pinned OpenCode promotion proof: %v", err)
		}
		if _, err := session.Wait(cleanupContext); err != nil {
			t.Errorf("wait for pinned OpenCode promotion proof cleanup: %v", err)
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("pinned OpenCode adapter did not return a staged session")
	}
	openContext, openCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	defer openCancel()
	handle, err := staged.Open(openContext)
	if err != nil {
		t.Fatalf("open pinned OpenCode session: %v", err)
	}
	if !isID(handle.ID, "ses_") {
		t.Fatalf("pinned OpenCode session identity = %q, want a ses_ identity", handle.ID)
	}
	native, ok := session.(*nativeSession)
	if !ok {
		t.Fatalf("pinned OpenCode session = %T, want *nativeSession", session)
	}
	native.mu.Lock()
	client, ok := native.client.(*Client)
	native.mu.Unlock()
	if !ok || client == nil {
		t.Fatal("pinned OpenCode session did not retain its verified production client after Open")
	}
	pid, identity := staged.ProcessDetails()
	if pid <= 0 || strings.TrimSpace(identity) == "" {
		t.Fatalf("pinned OpenCode process identity = (%d, %q), want the launched server", pid, identity)
	}
	readinessContext, readinessCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	if err := client.Readiness(readinessContext, pid); err != nil {
		readinessCancel()
		t.Fatalf("live readiness identity refused the launched server: %v", err)
	}
	if err := client.Readiness(readinessContext, pid+1); !errors.Is(err, ErrInvalidIdentity) {
		readinessCancel()
		t.Fatalf("live readiness identity accepted a foreign pid: %v", err)
	}
	readinessCancel()

	turnContext, turnCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	defer turnCancel()
	if err := staged.StartTurn(turnContext, harness.TurnRequest{Goal: goal, Context: json.RawMessage(`{"promotion_proof":"phase1"}`)}); err != nil {
		t.Fatalf("start pinned OpenCode turn: %v", err)
	}
	select {
	case <-sink.firstFrame:
	case <-time.After(nativePromotionProofFirstEventWait):
		started, frames := sink.snapshot()
		t.Fatalf("pinned OpenCode turn produced no native session event; session_started=%t native_frames=%d", started, frames)
	}
	if cancelTurn {
		controlContext, controlCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
		controlReceipt, controlErr := nativePromotionProofCancel(controlContext, staged)
		controlCancel()
		if controlErr != nil {
			t.Fatalf("cancel pinned OpenCode turn: %v", controlErr)
		}
		if controlReceipt.Outcome != harness.ControlApplied || controlReceipt.Capability != harness.CapabilityCancel {
			t.Fatalf("cancel receipt = %+v, want an applied cancel capability", controlReceipt)
		}
		operation.ControlApplied = true
	}
	waitContext, waitCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	waitErr := staged.WaitTurn(waitContext)
	waitCancel()
	if waitErr != nil {
		t.Fatalf("pinned OpenCode WaitTurn() = %v, want the verified recorded terminal", waitErr)
	}
	operation.WaitTurn = "verified"
	readbackContext, readbackCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	readback, readbackErr := client.SessionOutcome(readbackContext, handle.ID)
	readbackCancel()
	expectedOutcome := "succeeded"
	if cancelTurn {
		expectedOutcome = "interrupted"
	}
	if readbackErr != nil || readback.Outcome != expectedOutcome {
		t.Fatalf("pinned OpenCode outcome readback = %+v, %v, want %q", readback, readbackErr, expectedOutcome)
	}
	operation.ReadbackOutcome = readback.Outcome

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatalf("close pinned OpenCode promotion proof: %v", err)
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatalf("wait pinned OpenCode promotion proof: %v", err)
	}
	closed = true
	wantKind := harness.ResultSucceeded
	if cancelTurn {
		wantKind = harness.ResultCancelled
		if result.Reason == nil || *result.Reason != protocol.TaskResultReasonCancelled {
			t.Fatalf("cancelled result reason = %v, want %q", result.Reason, protocol.TaskResultReasonCancelled)
		}
	}
	if result.Kind != wantKind || result.Semantic != nil || result.Usage.State != harness.UsageUnknown || !result.Process.Terminated ||
		result.Process.SinkError != nil || result.Process.OutputError != nil ||
		result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatalf("pinned OpenCode promotion proof result = %+v, want %q with bounded clean process termination", result, wantKind)
	}
	operation.SessionStarted, operation.NativeFrames = sink.snapshot()
	operation.ResultKind = string(result.Kind)
	operation.Terminated = result.Process.Terminated
	return operation
}

// nativePromotionProofCancel retries the cancel until the server applies it. A 2.4.1
// interrupt arriving before the first generated token is refused, not queued.
func nativePromotionProofCancel(ctx context.Context, staged harness.StagedSession) (harness.ControlReceipt, error) {
	for {
		receipt, err := staged.Control(ctx, harness.ControlRequest{CommandID: "opencode-phase1-promotion-proof", Kind: harness.ControlCancel})
		if err == nil {
			return receipt, nil
		}
		if !errors.Is(err, ErrInterruptUnapplied) {
			return receipt, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return receipt, fmt.Errorf("interrupt did not apply to a live turn: %w", ctxErr)
		}
		time.Sleep(nativePromotionProofPollInterval)
	}
}

func nativePromotionProofExecutableDigest(t *testing.T, executable string) (string, int64) {
	t.Helper()
	file, err := os.Open(executable)
	if err != nil {
		t.Fatalf("open pinned OpenCode executable: %v", err)
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := file.WriteTo(hasher)
	if err != nil {
		t.Fatalf("hash pinned OpenCode executable: %v", err)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), size
}

func nativePromotionProofCredential(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the operator OMP models config: %v", err)
	}
	inProviders := false
	inProvider := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inProviders {
			inProviders = trimmed == "providers:"
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			break
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			inProvider = strings.TrimSuffix(trimmed, ":") == nativePromotionProofProvider
			continue
		}
		if !inProvider || !strings.HasPrefix(trimmed, "apiKey:") {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "apiKey:")), `"'`)
		if value == "" {
			t.Fatal("the operator OMP models config carries an empty provider apiKey")
		}
		return value
	}
	t.Fatal("the operator OMP models config carries no provider apiKey for the proof provider")
	return ""
}

// nativePromotionProofWriteConfig narrows the operator provider block to the pinned model,
// replacing the key with the credential environment reference; provider-level fields ride along.
func nativePromotionProofWriteConfig(t *testing.T, path, sourcePath string) {
	t.Helper()
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read the operator OpenCode provider config: %v", err)
	}
	var source map[string]any
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatalf("decode the operator OpenCode provider config: %v", err)
	}
	providers, ok := source["provider"].(map[string]any)
	if !ok {
		t.Fatalf("the operator OpenCode config carries no provider object")
	}
	provider, ok := providers[nativePromotionProofProvider].(map[string]any)
	if !ok {
		t.Fatalf("the operator OpenCode config carries no %s provider", nativePromotionProofProvider)
	}
	options, ok := provider["options"].(map[string]any)
	if !ok {
		t.Fatalf("the operator OpenCode %s provider carries no options object", nativePromotionProofProvider)
	}
	relay, ok := options["baseURL"].(string)
	if !ok || !strings.HasPrefix(relay, "https://") {
		t.Fatalf("the operator OpenCode %s provider carries no https baseURL", nativePromotionProofProvider)
	}
	models, ok := provider["models"].(map[string]any)
	if !ok {
		t.Fatalf("the operator OpenCode %s provider carries no models object", nativePromotionProofProvider)
	}
	entry, ok := models[nativePromotionProofModel]
	if !ok {
		t.Fatalf("the operator OpenCode %s provider carries no %s model", nativePromotionProofProvider, nativePromotionProofModel)
	}
	provider["options"] = map[string]any{"baseURL": relay, "apiKey": "{env:" + nativePromotionProofCredentialEnv + "}"}
	provider["models"] = map[string]any{nativePromotionProofModel: entry}
	config := map[string]any{
		"model":             nativePromotionProofProvider + "/" + nativePromotionProofModel,
		"small_model":       nativePromotionProofProvider + "/" + nativePromotionProofModel,
		"enabled_providers": []string{nativePromotionProofProvider},
		"provider":          map[string]any{nativePromotionProofProvider: provider},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encode promotion proof provider config: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write promotion proof provider config: %v", err)
	}
}

func nativePromotionProofDeliveredDigests(t *testing.T) map[string]string {
	t.Helper()
	digests := map[string]string{}
	for _, name := range []string{"adapter.go", "client.go", "sse.go", "types.go", "probe_promotion_test.go", "adapter_native_promotion_test.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read delivered %s: %v", name, err)
		}
		sum := sha256.Sum256(raw)
		digests[name] = "sha256:" + hex.EncodeToString(sum[:])
	}
	return digests
}
