//go:build windows

package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
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
)

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
// Open, StartTurn, observed events and an applied cancel against the exact pinned 2.4.1 PE.
func TestNativePromotionProofOnThePinnedWindowsBlob(t *testing.T) {
	if os.Getenv(nativePromotionProofEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_PROMOTION_PROOF=1 to run the Phase-1 Windows promotion proof")
	}
	executable := strings.TrimSpace(os.Getenv(nativePromotionProofExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute pinned OpenCode %s executable", nativePromotionProofExecutableEnv, TestedVersion)
	}
	receiptPath := strings.TrimSpace(os.Getenv(nativePromotionProofReceiptEnv))
	if receiptPath == "" || !filepath.IsAbs(receiptPath) {
		t.Fatalf("%s must name the absolute receipt path", nativePromotionProofReceiptEnv)
	}
	digest, size := nativePromotionProofExecutableDigest(t, executable)
	if digest != TestedExecutableHash {
		t.Fatalf("promotion proof executable digest = %q, want %q", digest, TestedExecutableHash)
	}
	credential := nativePromotionProofCredential(t)

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create promotion proof workspace: %v", err)
	}
	environment := nativeOpenCodeSmokeEnvironment(t, root)
	configPath := filepath.Join(root, "config", "opencode.json")
	if err := os.MkdirAll(filepath.Join(root, "managed"), 0o700); err != nil {
		t.Fatalf("create promotion proof managed directory: %v", err)
	}
	nativePromotionProofWriteConfig(t, configPath)
	environment = nativeOpenCodeConfigEnvironment(environment, configPath, filepath.Dir(configPath), filepath.Join(root, "managed"), nativePromotionProofCredentialEnv+"="+credential)
	versionContext, versionCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	defer versionCancel()
	versionCommand := exec.CommandContext(versionContext, executable, "--version")
	versionCommand.Dir = workspace
	versionCommand.Env = environment
	versionOutput, err := versionCommand.Output()
	if err != nil {
		t.Fatalf("run pinned OpenCode version check: %v", err)
	}
	versionLine := strings.TrimSpace(string(versionOutput))
	if versionLine != "opencode v"+TestedVersion {
		t.Fatalf("pinned OpenCode version = %q, want opencode v%s", versionLine, TestedVersion)
	}

	sink := &nativePromotionProofSink{firstFrame: make(chan struct{})}
	startContext, startCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	defer startCancel()
	session, err := startNativeOpenCodeTestSession(startContext, executable, workspace, environment, acceptNativeOpenCodeProcess, sink)
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
	if err := staged.StartTurn(turnContext, harness.TurnRequest{Goal: nativePromotionProofGoal, Context: json.RawMessage(`{"promotion_proof":"phase1"}`)}); err != nil {
		t.Fatalf("start pinned OpenCode turn: %v", err)
	}
	select {
	case <-sink.firstFrame:
	case <-time.After(nativePromotionProofFirstEventWait):
		started, frames := sink.snapshot()
		t.Fatalf("pinned OpenCode turn produced no native session event; session_started=%t native_frames=%d", started, frames)
	}
	controlContext, controlCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	defer controlCancel()
	controlReceipt, controlErr := nativePromotionProofCancel(controlContext, staged)
	if controlErr != nil {
		t.Fatalf("cancel pinned OpenCode turn: %v", controlErr)
	}
	if controlReceipt.Outcome != harness.ControlApplied || controlReceipt.Capability != harness.CapabilityCancel {
		t.Fatalf("cancel receipt = %+v, want an applied cancel capability", controlReceipt)
	}
	outcomeContext, outcomeCancel := context.WithTimeout(context.Background(), nativePromotionProofTimeout)
	defer outcomeCancel()
	turnOutcome, outcomeErr := nativePromotionProofWaitForTurnOutcome(outcomeContext, client, handle.ID)
	if outcomeErr != nil {
		t.Fatalf("observe pinned OpenCode turn outcome after cancel: %v", outcomeErr)
	}

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
	if result.Kind != harness.ResultUnknown || result.Semantic != nil || !result.Process.Terminated ||
		result.Process.SinkError != nil || result.Process.OutputError != nil ||
		result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatalf("pinned OpenCode promotion proof result = %+v, want unknown with bounded clean process termination", result)
	}

	sessionStarted, nativeFrames := sink.snapshot()
	receipt := map[string]any{
		"goos":                "windows",
		"tested_version":      TestedVersion,
		"executable_sha256":   digest,
		"executable_size":     size,
		"version_line":        versionLine,
		"relay_host_recorded": false,
		"session_started":     sessionStarted,
		"native_frames":       nativeFrames,
		"readiness":           map[string]any{"source_pid_accepted": true, "foreign_pid_refused": true},
		"control":             map[string]any{"kind": string(harness.ControlCancel), "outcome": string(controlReceipt.Outcome), "capability": string(controlReceipt.Capability)},
		"turn_outcome":        turnOutcome,
		"close":               map[string]any{"terminated": result.Process.Terminated, "result_kind": string(result.Kind)},
		"delivered_sha256":    nativePromotionProofDeliveredDigests(t),
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

func nativePromotionProofWaitForTurnOutcome(ctx context.Context, client *Client, sessionID string) (string, error) {
	deadline := time.Now().Add(nativePromotionProofTimeout)
	var outcome string
	for {
		status, body, err := client.request(ctx, http.MethodGet, "/api/session/"+sessionID, "", nil)
		if err != nil {
			return outcome, err
		}
		if status == http.StatusOK {
			var readback struct {
				Data struct {
					Outcome string `json:"outcome"`
				} `json:"data"`
			}
			if err := json.Unmarshal(body, &readback); err != nil {
				return outcome, fmt.Errorf("decode session readback: %w", err)
			}
			outcome = readback.Data.Outcome
			if outcome == "interrupted" {
				return outcome, nil
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return outcome, fmt.Errorf("session %s outcome = %q, want %q: %w", sessionID, outcome, "interrupted", ctxErr)
		}
		if time.Now().After(deadline) {
			return outcome, fmt.Errorf("session %s outcome = %q, want %q", sessionID, outcome, "interrupted")
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

func nativePromotionProofCredential(t *testing.T) string {
	t.Helper()
	path := strings.TrimSpace(os.Getenv(nativePromotionProofModelsEnv))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("resolve the operator home directory: %v", err)
		}
		path = filepath.Join(home, ".omp", "agent", "models.yml")
	}
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
func nativePromotionProofWriteConfig(t *testing.T, path string) {
	t.Helper()
	sourcePath := strings.TrimSpace(os.Getenv(nativePromotionProofSourceEnv))
	if sourcePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("resolve the operator home directory: %v", err)
		}
		sourcePath = filepath.Join(home, ".config", "opencode", "opencode.json")
	}
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
