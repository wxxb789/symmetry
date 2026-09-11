package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const (
	nativeRepositoryTaskEnabledEnv        = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK"
	nativeRepositoryTaskExecutableEnv     = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_EXECUTABLE"
	nativeRepositoryTaskProviderEnv       = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_PROVIDER"
	nativeRepositoryTaskModelEnv          = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_MODEL"
	nativeRepositoryTaskCredentialEnv     = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_CREDENTIAL_ENV"
	nativeRepositoryTaskBaseURLEnv        = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL"
	nativeRepositoryTaskTimeout           = 90 * time.Second
	nativeRepositoryTaskCloseTimeout      = 25 * time.Second
	nativeRepositoryTaskGitTimeout        = 10 * time.Second
	nativeRepositoryTaskArtifact          = "pi-native-evidence.txt"
	nativeRepositoryTaskArtifactContents  = "native pi repository task evidence\n"
	nativeRepositoryTaskResultSummary     = "created native pi repository task evidence"
	nativeRepositoryTaskResultID          = "00000000-0000-4000-8000-000000000106"
	nativeRepositoryTaskSubjectResourceID = "00000000-0000-4000-8000-000000000107"
	nativePiLoopbackProvider              = "symmetry-native-loopback"
	nativePiLoopbackModel                 = "gpt-5.6-terra"
	nativePiLoopbackThinking              = "high"
	nativePiLoopbackAPI                   = "openai-responses"
	nativePiLoopbackAPIKey                = "symmetry-native-loopback-test-key"
)

// TestNativeRepositoryTask is deliberately opt-in because it submits a real
// native Pi turn to the configured provider or loopback gateway. It does not
// attest the gateway's upstream authentication or served model. It proves one
// bounded repository mutation and the native turn/result lifecycle; it does not
// advertise Pi capabilities or prove
// cancellation, recovery, Control durability, or provider accounting.
func TestNativeRepositoryTask(t *testing.T) {
	configuration := nativePiRepositoryTaskLoadConfiguration(t)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	sessionDir := filepath.Join(root, "sessions")
	processRecord := filepath.Join(root, "process-identity.json")
	sessionRecord := filepath.Join(root, "session-identity.json")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("create native Pi session directory: %v", err)
	}
	gitEnvironment := nativePiRepositoryTaskGitEnvironment(t, root)
	gitContext, gitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	nativePiRepositoryTaskInitializeGit(t, gitContext, gitEnvironment, workspace)
	baselineHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, gitContext, gitEnvironment, workspace, "rev-parse", "HEAD"))
	subject := nativePiRepositoryTaskMaterializeSubject(t, gitContext, root, workspace, baselineHead)
	gitCancel()
	var environment []string
	if configuration.loopback {
		environment = nativePiRepositoryTaskLoopbackEnvironment(t, root, configuration.baseURL)
	} else {
		environment = nativePiRepositoryTaskEnvironment(t, root, configuration.credentialEnv)
	}

	versionContext, versionCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer versionCancel()
	versionCommand := exec.CommandContext(versionContext, configuration.executable, "--version")
	versionCommand.Dir = workspace
	versionCommand.Env = environment
	versionOutput, err := versionCommand.Output()
	if err != nil {
		t.Fatal("run native Pi version check failed")
	}
	if strings.TrimSpace(string(versionOutput)) != TestedVersion {
		t.Fatalf("native Pi version did not equal the tested version %s", TestedVersion)
	}

	var events nativePiRepositoryTaskEvents
	arguments := []string{
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-themes",
		"--no-context-files",
		"--no-approve",
		"--session-dir", sessionDir,
		"--provider", configuration.provider,
		"--model", configuration.model,
		"--tools", "write",
	}
	if configuration.loopback {
		arguments = append(arguments, "--thinking", nativePiLoopbackThinking)
		t.Logf("native Pi loopback request: provider=%s model=%s thinking=%s; served model/effort unknown", nativePiLoopbackProvider, nativePiLoopbackModel, nativePiLoopbackThinking)
	}
	turnContext, cancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer cancel()
	session, err := NewAdapter(configuration.executable).Start(turnContext, harness.StartRequest{
		Workspace: workspace,
		Invocation: execution.Invocation{
			Args: arguments,
			Env:  environment,
		},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			return nativePiRepositoryTaskWriteJSON(processRecord, map[string]any{
				"pid":      pid,
				"identity": identity,
			})
		},
	}, harness.EventSinkFunc(events.handle))
	if err != nil {
		t.Fatal("start native Pi repository task transport failed")
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Error("cleanup native Pi repository task session failed")
		}
		waitContext, waitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer waitCancel()
		if _, err := session.Wait(waitContext); err != nil {
			t.Error("wait for native Pi repository task cleanup failed")
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native Pi adapter did not return a staged session")
	}
	processPID, processIdentity := staged.ProcessDetails()
	var persistedProcess struct {
		PID      int    `json:"pid"`
		Identity string `json:"identity"`
	}
	nativePiRepositoryTaskReadJSON(t, processRecord, &persistedProcess)
	if persistedProcess.PID != processPID || persistedProcess.Identity != processIdentity || processPID <= 0 || processIdentity == "" {
		t.Fatal("native Pi process identity was not persisted before session exposure")
	}

	nativeFramesBeforeOpen := events.nativeFrameCount()
	handle, err := staged.Open(turnContext)
	if err != nil {
		t.Fatal("open native Pi repository task session failed")
	}
	if handle.ID == "" || handle.Filename == "" {
		t.Fatal("native Pi repository task returned an incomplete session handle")
	}
	if err := nativePiRepositoryTaskWriteJSON(sessionRecord, map[string]any{
		"id":       handle.ID,
		"filename": handle.Filename,
	}); err != nil {
		t.Fatalf("persist native Pi session identity: %v", err)
	}
	var persistedSession struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}
	nativePiRepositoryTaskReadJSON(t, sessionRecord, &persistedSession)
	if persistedSession.ID != handle.ID || persistedSession.Filename != handle.Filename {
		t.Fatal("native Pi session identity was not persisted before StartTurn")
	}
	relativeFilename, err := filepath.Rel(sessionDir, handle.Filename)
	if err != nil || relativeFilename == ".." || strings.HasPrefix(relativeFilename, ".."+string(os.PathSeparator)) || filepath.IsAbs(relativeFilename) {
		t.Fatal("native Pi session filename was not isolated under the repository task session directory")
	}
	nativeFramesAfterOpen := events.nativeFrameCount()
	if nativeFramesAfterOpen <= nativeFramesBeforeOpen {
		t.Fatal("native Pi Open did not produce a decoded native frame")
	}

	expected := nativePiRepositoryTaskExpectedResult(t, subject)
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal expected native Pi task result: %v", err)
	}
	nativeFramesBeforeTurn := events.nativeFrameCount()
	// StartTurn returns only after the adapter receives the correlated prompt
	// response; WaitTurn below additionally requires native settlement.
	if err := staged.StartTurn(turnContext, harness.TurnRequest{
		Goal:    nativePiRepositoryTaskGoal(string(expectedJSON)),
		Context: json.RawMessage(`{"task":"native_pi_repository_evidence"}`),
	}); err != nil {
		t.Fatal("start native Pi repository task turn failed")
	}
	if err := staged.WaitTurn(turnContext); err != nil {
		t.Fatal("native Pi repository task did not settle with a strict task result")
	}
	if nativeFramesAfterTurn := events.nativeFrameCount(); nativeFramesAfterTurn <= nativeFramesBeforeTurn {
		t.Fatal("native Pi repository task turn did not produce decoded native frames after prompt acknowledgement")
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatal("close native Pi repository task session failed")
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatal("wait for native Pi repository task session failed")
	}
	closed = true
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil || !reflect.DeepEqual(*result.Semantic, expected) {
		t.Fatalf("native Pi repository task semantic mismatch: kind=%s reason=%v semantic_present=%t evidence_refs_nil=%t diagnostics_nil=%t",
			result.Kind, result.Reason, result.Semantic != nil,
			result.Semantic != nil && result.Semantic.EvidenceRefs == nil,
			result.Semantic != nil && result.Semantic.Diagnostics == nil)
	}
	if !result.Process.Terminated || result.Process.SinkError != nil || result.Process.OutputError != nil || !result.Process.OutputTruncated || result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatal("native Pi repository task process did not complete a bounded termination barrier")
	}
	t.Logf("native Pi stop: output_truncated=%t after acknowledged semantic settlement; not a full output drain", result.Process.OutputTruncated)

	artifact, err := os.ReadFile(filepath.Join(workspace, nativeRepositoryTaskArtifact))
	if err != nil {
		t.Fatalf("read native Pi repository task artifact: %v", err)
	}
	if string(artifact) != nativeRepositoryTaskArtifactContents {
		t.Fatal("native Pi repository task artifact did not contain the exact requested bytes")
	}
	verificationContext, verificationCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer verificationCancel()
	if currentHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, verificationContext, gitEnvironment, workspace, "rev-parse", "HEAD")); currentHead != baselineHead {
		t.Fatal("native Pi repository task changed the repository HEAD")
	}
	status := nativePiRepositoryTaskGitOutput(t, verificationContext, gitEnvironment, workspace, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching")
	if status != "?? "+nativeRepositoryTaskArtifact+"\n" {
		t.Fatalf("native Pi repository task changed unexpected repository paths: %q", status)
	}
	if sessionStarted, taskResults := events.semanticCounts(); sessionStarted != 1 || taskResults != 1 {
		t.Fatalf("native Pi repository task events = session_started:%d task_results:%d, want one acknowledged turn and semantic result", sessionStarted, taskResults)
	}
}

type nativePiRepositoryTaskConfiguration struct {
	executable    string
	provider      string
	model         string
	credentialEnv string
	baseURL       string
	loopback      bool
}

func nativePiRepositoryTaskLoadConfiguration(t *testing.T) nativePiRepositoryTaskConfiguration {
	t.Helper()
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("native Pi repository task test supports Linux and Windows only")
	}
	if os.Getenv(nativeRepositoryTaskEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1 to run the native Pi repository task test")
	}
	baseURL := os.Getenv(nativeRepositoryTaskBaseURLEnv)
	configuration := nativePiRepositoryTaskConfiguration{
		executable: nativePiRepositoryTaskRequiredEnv(t, nativeRepositoryTaskExecutableEnv),
		provider:   nativePiRepositoryTaskRequiredEnv(t, nativeRepositoryTaskProviderEnv),
		model:      nativePiRepositoryTaskRequiredEnv(t, nativeRepositoryTaskModelEnv),
		baseURL:    baseURL,
		loopback:   baseURL != "",
	}
	if configuration.loopback {
		if err := nativePiRepositoryTaskValidateLoopbackConfiguration(
			configuration.baseURL,
			configuration.provider,
			configuration.model,
			strings.TrimSpace(os.Getenv(nativeRepositoryTaskCredentialEnv)),
		); err != nil {
			t.Fatalf("invalid native Pi loopback configuration: %v", err)
		}
	}
	if !filepath.IsAbs(configuration.executable) {
		t.Fatalf("%s must be absolute", nativeRepositoryTaskExecutableEnv)
	}
	info, err := os.Stat(configuration.executable)
	if err != nil {
		t.Fatalf("stat native Pi executable: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s must name a regular executable file", nativeRepositoryTaskExecutableEnv)
	}
	configuration.credentialEnv = strings.TrimSpace(os.Getenv(nativeRepositoryTaskCredentialEnv))
	if configuration.credentialEnv != "" {
		if !nativePiRepositoryTaskCredentialEnvironmentAllowed(configuration.credentialEnv) {
			t.Fatalf("%s must name a supported Pi provider credential variable", nativeRepositoryTaskCredentialEnv)
		}
		if value, ok := os.LookupEnv(configuration.credentialEnv); !ok || strings.TrimSpace(value) == "" {
			t.Fatalf("selected credential environment variable %s is not configured", configuration.credentialEnv)
		}
	}
	return configuration
}

func nativePiRepositoryTaskValidateLoopbackConfiguration(baseURL, provider, model, credentialEnv string) error {
	if err := nativePiRepositoryTaskValidateLoopbackURL(baseURL); err != nil {
		return err
	}
	if provider != nativePiLoopbackProvider {
		return fmt.Errorf("provider must be %q, got %q", nativePiLoopbackProvider, provider)
	}
	if model != nativePiLoopbackModel {
		return fmt.Errorf("model must be %q, got %q", nativePiLoopbackModel, model)
	}
	if credentialEnv != "" {
		return fmt.Errorf("credential environment variable %q is not allowed with loopback mode", credentialEnv)
	}
	return nil
}

func nativePiRepositoryTaskValidateLoopbackURL(value string) error {
	if value == "" {
		return fmt.Errorf("base URL must not be empty")
	}
	for _, character := range value {
		if unicode.IsSpace(character) {
			return fmt.Errorf("base URL must not contain whitespace")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("base URL scheme must be http or https")
	}
	if parsed.Opaque != "" {
		return fmt.Errorf("base URL must not be opaque")
	}
	if parsed.Host == "" {
		return fmt.Errorf("base URL must include a host")
	}
	if parsed.User != nil {
		return fmt.Errorf("base URL must not include userinfo")
	}
	if parsed.ForceQuery || parsed.RawQuery != "" || strings.Contains(value, "?") {
		return fmt.Errorf("base URL must not include a query")
	}
	if strings.Contains(value, "#") {
		return fmt.Errorf("base URL must not include a fragment")
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return fmt.Errorf("base URL must include a host")
	}
	if strings.Contains(hostname, "%") {
		return fmt.Errorf("base URL must not include an IPv6 zone")
	}
	parsedIP := net.ParseIP(hostname)
	if parsedIP == nil || !parsedIP.IsLoopback() {
		return fmt.Errorf("base URL host must be a numeric loopback IP")
	}
	if strings.Contains(hostname, ":") && !strings.HasPrefix(parsed.Host, "[") {
		return fmt.Errorf("IPv6 base URL hosts must use brackets")
	}
	return nativePiRepositoryTaskValidateLoopbackPort(parsed.Host)
}

func nativePiRepositoryTaskValidateLoopbackPort(host string) error {
	port := ""
	hasPort := false
	if strings.HasPrefix(host, "[") {
		closingBracket := strings.IndexByte(host, ']')
		if closingBracket < 0 {
			return fmt.Errorf("base URL has an invalid IPv6 host")
		}
		suffix := host[closingBracket+1:]
		if suffix == "" {
			return nil
		}
		if !strings.HasPrefix(suffix, ":") {
			return fmt.Errorf("base URL has an invalid host suffix")
		}
		hasPort = true
		port = suffix[1:]
	} else {
		separator := strings.LastIndexByte(host, ':')
		if separator < 0 {
			return nil
		}
		hasPort = true
		port = host[separator+1:]
	}
	if !hasPort || port == "" {
		return fmt.Errorf("base URL port must be numeric")
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("base URL port must be numeric")
		}
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 || parsedPort > 65535 {
		return fmt.Errorf("base URL port must be between 1 and 65535")
	}
	return nil
}

func TestNativeRepositoryTaskCredentialEnvironmentRejectsInvalidOrReservedNames(t *testing.T) {
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "OPENAI_API_KEY", want: true},
		{name: "ANTHROPIC_API_KEY", want: true},
		{name: "HF_TOKEN", want: true},
		{name: "AWS_BEARER_TOKEN_BEDROCK", want: true},
		{name: "NODE_OPTIONS", want: false},
		{name: "NODE_PATH", want: false},
		{name: "LD_PRELOAD", want: false},
		{name: "LD_LIBRARY_PATH", want: false},
		{name: "DYLD_INSERT_LIBRARIES", want: false},
		{name: "BASH_ENV", want: false},
		{name: "GIT_CONFIG_GLOBAL", want: false},
		{name: "UNKNOWN_API_KEY", want: false},
		{name: "", want: false},
		{name: "1API_KEY", want: false},
		{name: "API-KEY", want: false},
		{name: "PATH", want: false},
		{name: "path", want: false},
		{name: "SystemRoot", want: false},
		{name: "windir", want: false},
		{name: "ComSpec", want: false},
		{name: "PATHEXT", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nativePiRepositoryTaskCredentialEnvironmentAllowed(test.name); got != test.want {
				t.Fatalf("nativePiRepositoryTaskCredentialEnvironmentAllowed(%q) = %t, want %t", test.name, got, test.want)
			}
		})
	}
}

func TestNativeRepositoryTaskLoopbackURLValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "ipv4 with path", value: "http://127.0.0.1:8787/v1"},
		{name: "ipv6 with path", value: "https://[::1]:443/v1"},
		{name: "ipv4 default port", value: "http://127.0.0.1/v1"},
		{name: "empty", value: "", wantErr: true},
		{name: "leading whitespace", value: " http://127.0.0.1:8787/v1", wantErr: true},
		{name: "trailing whitespace", value: "http://127.0.0.1:8787/v1 ", wantErr: true},
		{name: "non-http scheme", value: "ftp://127.0.0.1:8787/v1", wantErr: true},
		{name: "userinfo", value: "http://user:pass@127.0.0.1:8787/v1", wantErr: true},
		{name: "query", value: "http://127.0.0.1:8787/v1?token=1", wantErr: true},
		{name: "empty query", value: "http://127.0.0.1:8787/v1?", wantErr: true},
		{name: "fragment", value: "http://127.0.0.1:8787/v1#section", wantErr: true},
		{name: "empty fragment", value: "http://127.0.0.1:8787/v1#", wantErr: true},
		{name: "opaque", value: "http:127.0.0.1:8787/v1", wantErr: true},
		{name: "missing host", value: "http:///v1", wantErr: true},
		{name: "non-loopback", value: "http://192.0.2.1:8787/v1", wantErr: true},
		{name: "ipv6 zone", value: "http://[::1%25lo0]:8787/v1", wantErr: true},
		{name: "bad port", value: "http://127.0.0.1:port/v1", wantErr: true},
		{name: "empty port", value: "http://127.0.0.1:/v1", wantErr: true},
		{name: "zero port", value: "http://127.0.0.1:0/v1", wantErr: true},
		{name: "out of range port", value: "http://127.0.0.1:65536/v1", wantErr: true},
		{name: "signed port", value: "http://127.0.0.1:+1/v1", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := nativePiRepositoryTaskValidateLoopbackURL(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("nativePiRepositoryTaskValidateLoopbackURL(%q) error = %v, wantErr %t", test.value, err, test.wantErr)
			}
		})
	}
}

func TestNativeRepositoryTaskLoopbackConfigurationRequiresFixedProviderAndModel(t *testing.T) {
	for _, test := range []struct {
		name       string
		provider   string
		model      string
		credential string
		wantErr    bool
	}{
		{name: "valid", provider: nativePiLoopbackProvider, model: nativePiLoopbackModel},
		{name: "provider alias", provider: "openai", model: nativePiLoopbackModel, wantErr: true},
		{name: "model alias", provider: nativePiLoopbackProvider, model: "gpt-5.6-luna", wantErr: true},
		{name: "credential environment", provider: nativePiLoopbackProvider, model: nativePiLoopbackModel, credential: "OPENAI_API_KEY", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := nativePiRepositoryTaskValidateLoopbackConfiguration(
				"http://127.0.0.1:8787/v1",
				test.provider,
				test.model,
				test.credential,
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("nativePiRepositoryTaskValidateLoopbackConfiguration() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestNativeRepositoryTaskLoopbackModelsJSONStructure(t *testing.T) {
	root := t.TempDir()
	baseURL := "http://127.0.0.1:8787/v1"
	environment := nativePiRepositoryTaskLoopbackEnvironment(t, root, baseURL)
	if got := nativePiRepositoryTaskEnvironmentValue(environment, "PI_CODING_AGENT_DIR"); got != filepath.Join(root, "agent") {
		t.Fatalf("PI_CODING_AGENT_DIR = %q, want %q", got, filepath.Join(root, "agent"))
	}
	encoded, err := os.ReadFile(filepath.Join(root, "agent", "models.json"))
	if err != nil {
		t.Fatalf("read loopback models.json: %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode loopback models.json: %v", err)
	}
	nativePiRepositoryTaskAssertJSONKeys(t, document, "providers")
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(document["providers"], &providers); err != nil {
		t.Fatalf("decode loopback providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("loopback provider count = %d, want 1", len(providers))
	}
	providerJSON, ok := providers[nativePiLoopbackProvider]
	if !ok {
		t.Fatalf("loopback provider %q was missing", nativePiLoopbackProvider)
	}
	var provider map[string]json.RawMessage
	if err := json.Unmarshal(providerJSON, &provider); err != nil {
		t.Fatalf("decode loopback provider: %v", err)
	}
	nativePiRepositoryTaskAssertJSONKeys(t, provider, "api", "apiKey", "baseUrl", "models")
	var api, apiKey, configuredBaseURL string
	for key, target := range map[string]*string{"api": &api, "apiKey": &apiKey, "baseUrl": &configuredBaseURL} {
		if err := json.Unmarshal(provider[key], target); err != nil {
			t.Fatalf("decode loopback provider %s: %v", key, err)
		}
	}
	if api != nativePiLoopbackAPI || apiKey != nativePiLoopbackAPIKey || configuredBaseURL != baseURL {
		t.Fatalf("loopback provider values = api:%q apiKey:%q baseUrl:%q", api, apiKey, configuredBaseURL)
	}
	if strings.HasPrefix(apiKey, "$") || strings.HasPrefix(apiKey, "!") {
		t.Fatal("loopback apiKey must be a literal nonsecret dummy value")
	}
	var models []map[string]json.RawMessage
	if err := json.Unmarshal(provider["models"], &models); err != nil {
		t.Fatalf("decode loopback models: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("loopback model count = %d, want 1", len(models))
	}
	nativePiRepositoryTaskAssertJSONKeys(t, models[0], "id", "reasoning", "thinkingLevelMap")
	var modelID string
	if err := json.Unmarshal(models[0]["id"], &modelID); err != nil {
		t.Fatalf("decode loopback model id: %v", err)
	}
	if modelID != nativePiLoopbackModel {
		t.Fatalf("loopback model id = %q, want %q", modelID, nativePiLoopbackModel)
	}
	var reasoning bool
	if err := json.Unmarshal(models[0]["reasoning"], &reasoning); err != nil {
		t.Fatalf("decode loopback reasoning: %v", err)
	}
	if !reasoning {
		t.Fatal("loopback model reasoning = false, want true")
	}
	var thinkingLevelMap map[string]string
	if err := json.Unmarshal(models[0]["thinkingLevelMap"], &thinkingLevelMap); err != nil {
		t.Fatalf("decode loopback thinkingLevelMap: %v", err)
	}
	if !reflect.DeepEqual(thinkingLevelMap, map[string]string{nativePiLoopbackThinking: nativePiLoopbackThinking}) {
		t.Fatalf("loopback thinkingLevelMap = %#v", thinkingLevelMap)
	}
}

func TestNativeRepositoryTaskStandardEnvironmentDoesNotWriteModels(t *testing.T) {
	root := t.TempDir()
	credentialEnv := "OPENAI_API_KEY"
	credentialValue := "nonsecret-test-value"
	t.Setenv(credentialEnv, credentialValue)
	environment := nativePiRepositoryTaskEnvironment(t, root, credentialEnv)
	if got := nativePiRepositoryTaskEnvironmentValue(environment, credentialEnv); got != credentialValue {
		t.Fatalf("standard credential environment value = %q, want %q", got, credentialValue)
	}
	if got := nativePiRepositoryTaskEnvironmentValue(environment, "PI_CODING_AGENT_DIR"); got != filepath.Join(root, "agent") {
		t.Fatalf("standard PI_CODING_AGENT_DIR = %q, want %q", got, filepath.Join(root, "agent"))
	}
	if _, err := os.Stat(filepath.Join(root, "agent", "models.json")); !os.IsNotExist(err) {
		t.Fatalf("standard environment models.json stat error = %v, want file absent", err)
	}
}

func TestNativeRepositoryTaskWindowsEnvironmentPreservesExecutableResolution(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable environment only")
	}

	parentLocalAppData := filepath.Join(t.TempDir(), "parent-localappdata")
	configuredMiseData := filepath.Join(t.TempDir(), "mise-data")
	t.Setenv("PATH", `C:\native-pi-test\bin`)
	t.Setenv("LOCALAPPDATA", parentLocalAppData)
	t.Setenv("MISE_DATA_DIR", configuredMiseData)
	t.Setenv("OPENAI_API_KEY", "must-not-be-copied")

	root := t.TempDir()
	environment := nativePiRepositoryTaskEnvironment(t, root, "")
	for name, want := range map[string]string{
		"PATH":                 `C:\native-pi-test\bin`,
		"MISE_DATA_DIR":        configuredMiseData,
		"PI_SKIP_VERSION_CHECK": "1",
		"USERPROFILE":           filepath.Join(root, "home"),
		"APPDATA":               filepath.Join(root, "appdata"),
		"LOCALAPPDATA":          filepath.Join(root, "localappdata"),
	} {
		if got := nativePiRepositoryTaskEnvironmentValue(environment, name); got != want {
			t.Fatalf("isolated native Pi %s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "PI_CONFIG_DIR"} {
		if got := nativePiRepositoryTaskEnvironmentValue(environment, name); got != "" {
			t.Fatalf("isolated native Pi environment copied %s=%q", name, got)
		}
	}
}

func TestNativeRepositoryTaskWindowsEnvironmentDerivesMiseDataDirectory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable environment only")
	}

	parentLocalAppData := filepath.Join(t.TempDir(), "parent-localappdata")
	t.Setenv("PATH", `C:\native-pi-test\bin`)
	t.Setenv("LOCALAPPDATA", parentLocalAppData)
	t.Setenv("MISE_DATA_DIR", "")

	environment := nativePiRepositoryTaskEnvironment(t, t.TempDir(), "")
	want := filepath.Join(parentLocalAppData, "mise")
	if got := nativePiRepositoryTaskEnvironmentValue(environment, "MISE_DATA_DIR"); got != want {
		t.Fatalf("derived native Pi MISE_DATA_DIR = %q, want %q", got, want)
	}
}

func nativePiRepositoryTaskAssertJSONKeys(t *testing.T, object map[string]json.RawMessage, expected ...string) {
	t.Helper()
	wanted := make(map[string]struct{}, len(expected))
	for _, key := range expected {
		wanted[key] = struct{}{}
	}
	if len(object) != len(wanted) {
		t.Fatalf("JSON keys = %#v, want exactly %#v", object, expected)
	}
	for key := range wanted {
		if _, ok := object[key]; !ok {
			t.Fatalf("JSON keys missing %q", key)
		}
	}
}

func nativePiRepositoryTaskEnvironmentValue(environment []string, name string) string {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func TestNativeRepositoryTaskSubjectUsesRepositoryBaseline(t *testing.T) {
	root := t.TempDir()
	gitEnvironment := nativePiRepositoryTaskGitEnvironment(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer cancel()
	repository := filepath.Join(root, "repository")
	nativePiRepositoryTaskInitializeGit(t, ctx, gitEnvironment, repository)
	baselineHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, ctx, gitEnvironment, repository, "rev-parse", "HEAD"))
	baseline := nativePiRepositoryTaskMaterializeSubject(t, ctx, root, repository, baselineHead)
	if baseline.ResourceID != nativeRepositoryTaskSubjectResourceID || baseline.Commit != baselineHead || baseline.TreeDigest == "" {
		t.Fatalf("baseline subject = %#v, want actual baseline commit and tree digest", baseline)
	}
	if result := nativePiRepositoryTaskExpectedResult(t, baseline); result.Subject != baseline {
		t.Fatalf("task result subject = %#v, want materialized baseline %#v", result.Subject, baseline)
	}
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("# Changed Native Pi repository task\n"), 0o600); err != nil {
		t.Fatalf("rewrite subject fixture: %v", err)
	}
	nativePiRepositoryTaskGit(t, ctx, gitEnvironment, repository, "add", "README.md")
	nativePiRepositoryTaskGit(t, ctx, gitEnvironment, repository, "commit", "-m", "Change native Pi repository task fixture")
	changedHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, ctx, gitEnvironment, repository, "rev-parse", "HEAD"))
	changed := nativePiRepositoryTaskMaterializeSubject(t, ctx, root, repository, changedHead)
	if changedHead == baselineHead || changed.Commit != changedHead || changed.TreeDigest == baseline.TreeDigest {
		t.Fatalf("changed subject = %#v, baseline = %#v; want distinct commit and tree digest", changed, baseline)
	}
}

func nativePiRepositoryTaskRequiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" || strings.IndexByte(value, 0) >= 0 {
		t.Fatalf("%s must be configured", name)
	}
	return value
}

func nativePiRepositoryTaskCredentialEnvironmentAllowed(name string) bool {
	// Credential names are pinned to Pi 0.85.1 docs/providers.md. Runtime and
	// loader configuration must never enter the child as a selected credential.
	switch name {
	case "AI_GATEWAY_API_KEY", "ANTHROPIC_API_KEY", "ANT_LING_API_KEY", "AWS_BEARER_TOKEN_BEDROCK",
		"AZURE_OPENAI_API_KEY", "BASETEN_API_KEY", "CEREBRAS_API_KEY", "CLOUDFLARE_API_KEY",
		"DEEPSEEK_API_KEY", "FIREWORKS_API_KEY", "GEMINI_API_KEY", "GROQ_API_KEY", "HF_TOKEN",
		"KIMI_API_KEY", "MINIMAX_API_KEY", "MINIMAX_CN_API_KEY", "MISTRAL_API_KEY", "NVIDIA_API_KEY",
		"OPENAI_API_KEY", "OPENCODE_API_KEY", "OPENROUTER_API_KEY", "QWEN_TOKEN_PLAN_API_KEY",
		"QWEN_TOKEN_PLAN_CN_API_KEY", "RADIUS_API_KEY", "TOGETHER_API_KEY", "XAI_API_KEY",
		"XIAOMI_API_KEY", "XIAOMI_TOKEN_PLAN_AMS_API_KEY", "XIAOMI_TOKEN_PLAN_CN_API_KEY",
		"XIAOMI_TOKEN_PLAN_SGP_API_KEY", "ZAI_API_KEY", "ZAI_CODING_CN_API_KEY":
		return true
	default:
		return false
	}
}

func nativePiRepositoryTaskLoopbackEnvironment(t *testing.T, root, baseURL string) []string {
	t.Helper()
	if err := nativePiRepositoryTaskValidateLoopbackURL(baseURL); err != nil {
		t.Fatalf("validate native Pi loopback base URL: %v", err)
	}
	environment := nativePiRepositoryTaskEnvironment(t, root, "")
	modelsFilename := filepath.Join(root, "agent", "models.json")
	if err := nativePiRepositoryTaskWriteJSON(modelsFilename, nativePiRepositoryTaskLoopbackModels(baseURL)); err != nil {
		t.Fatalf("write native Pi loopback models.json: %v", err)
	}
	return environment
}

func nativePiRepositoryTaskLoopbackModels(baseURL string) map[string]any {
	return map[string]any{
		"providers": map[string]any{
			nativePiLoopbackProvider: map[string]any{
				"baseUrl": baseURL,
				"api":     nativePiLoopbackAPI,
				"apiKey":  nativePiLoopbackAPIKey,
				"models": []any{
					map[string]any{
						"id":               nativePiLoopbackModel,
						"reasoning":        true,
						"thinkingLevelMap": map[string]string{nativePiLoopbackThinking: nativePiLoopbackThinking},
					},
				},
			},
		},
	}
}

func nativePiRepositoryTaskEnvironment(t *testing.T, root, credentialEnv string) []string {
	t.Helper()
	if credentialEnv != "" && !nativePiRepositoryTaskCredentialEnvironmentAllowed(credentialEnv) {
		t.Fatal("native Pi environment requires a supported provider credential variable")
	}
	home := filepath.Join(root, "home")
	agentDir := filepath.Join(root, "agent")
	cacheDir := filepath.Join(root, "cache")
	dataDir := filepath.Join(root, "data")
	tempDir := filepath.Join(root, "tmp")
	appDataDir := filepath.Join(root, "appdata")
	localAppDataDir := filepath.Join(root, "localappdata")
	for _, directory := range []string{home, agentDir, cacheDir, dataDir, tempDir, appDataDir, localAppDataDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal("create isolated native Pi repository task environment failed")
		}
	}
	path := nativePiRepositoryTaskRequiredEnv(t, "PATH")
	environment := []string{
		"PI_CODING_AGENT_DIR=" + agentDir,
		"HOME=" + home,
		"PATH=" + path,
		"TEMP=" + tempDir,
		"TMP=" + tempDir,
	}
	if credentialEnv != "" {
		environment = append(environment, credentialEnv+"="+os.Getenv(credentialEnv))
	}
	if runtime.GOOS == "windows" {
		miseDataDir := strings.TrimSpace(os.Getenv("MISE_DATA_DIR"))
		if miseDataDir == "" {
			miseDataDir = filepath.Join(nativePiRepositoryTaskRequiredEnv(t, "LOCALAPPDATA"), "mise")
		}
		environment = append(environment,
			"USERPROFILE="+home,
			"APPDATA="+appDataDir,
			"LOCALAPPDATA="+localAppDataDir,
			// The Windows mise shim needs its tool store after LOCALAPPDATA is
			// isolated; this does not expose Pi's configuration directory.
			"MISE_DATA_DIR="+miseDataDir,
			"PI_SKIP_VERSION_CHECK=1",
		)
		for _, name := range []string{"SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
			value := nativePiRepositoryTaskRequiredEnv(t, name)
			environment = append(environment, name+"="+value)
		}
		return environment
	}
	return append(environment,
		"XDG_CONFIG_HOME="+agentDir,
		"XDG_CACHE_HOME="+cacheDir,
		"XDG_DATA_HOME="+dataDir,
	)
}

func nativePiRepositoryTaskInitializeGit(t *testing.T, ctx context.Context, environment []string, workspace string) {
	t.Helper()
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create native Pi repository task workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# Native Pi repository task\n"), 0o600); err != nil {
		t.Fatalf("write native Pi repository task seed file: %v", err)
	}
	nativePiRepositoryTaskGit(t, ctx, environment, workspace, "init")
	if err := os.MkdirAll(filepath.Join(workspace, ".git", "hooks-disabled"), 0o700); err != nil {
		t.Fatalf("create disabled Git hooks directory: %v", err)
	}
	for _, arguments := range [][]string{
		{"config", "core.hooksPath", filepath.Join(workspace, ".git", "hooks-disabled")},
		{"config", "commit.gpgSign", "false"},
		{"config", "tag.gpgSign", "false"},
		{"config", "user.name", "Symmetry Native Test"},
		{"config", "user.email", "symmetry-native-test@example.invalid"},
		{"add", "README.md"},
		{"commit", "-m", "Initialize native Pi repository task"},
	} {
		nativePiRepositoryTaskGit(t, ctx, environment, workspace, arguments...)
	}
}

func nativePiRepositoryTaskGitEnvironment(t *testing.T, root string) []string {
	t.Helper()
	home := filepath.Join(root, "git-home")
	tempDir := filepath.Join(root, "git-tmp")
	templateDir := filepath.Join(root, "git-templates")
	globalConfig := filepath.Join(root, "gitconfig")
	appDataDir := filepath.Join(root, "git-appdata")
	localAppDataDir := filepath.Join(root, "git-localappdata")
	for _, directory := range []string{home, tempDir, templateDir, appDataDir, localAppDataDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal("create isolated Git environment failed")
		}
	}
	if err := os.WriteFile(globalConfig, nil, 0o600); err != nil {
		t.Fatalf("create isolated Git configuration: %v", err)
	}
	for name, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":   globalConfig,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TEMPLATE_DIR":    templateDir,
		"GIT_TERMINAL_PROMPT": "0",
	} {
		t.Setenv(name, value)
	}
	for _, name := range []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_WORK_TREE",
	} {
		nativePiRepositoryTaskUnsetEnvironment(t, name)
	}
	environment := []string{
		"HOME=" + home,
		"PATH=" + nativePiRepositoryTaskRequiredEnv(t, "PATH"),
		"TEMP=" + tempDir,
		"TMP=" + tempDir,
		"GIT_CONFIG_GLOBAL=" + globalConfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TEMPLATE_DIR=" + templateDir,
		"GIT_TERMINAL_PROMPT=0",
	}
	if runtime.GOOS == "windows" {
		environment = append(environment,
			"USERPROFILE="+home,
			"APPDATA="+appDataDir,
			"LOCALAPPDATA="+localAppDataDir,
		)
		for _, name := range []string{"SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
			environment = append(environment, name+"="+nativePiRepositoryTaskRequiredEnv(t, name))
		}
	}
	return environment
}

func nativePiRepositoryTaskUnsetEnvironment(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, previous)
			return
		}
		_ = os.Unsetenv(name)
	})
}

func nativePiRepositoryTaskGit(t *testing.T, ctx context.Context, environment []string, workspace string, arguments ...string) {
	t.Helper()
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = workspace
	command.Env = environment
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("git %s timed out: %v", arguments[0], ctx.Err())
		}
		t.Fatalf("git %s failed: %v", arguments[0], err)
	}
}

func nativePiRepositoryTaskGitOutput(t *testing.T, ctx context.Context, environment []string, workspace string, arguments ...string) string {
	t.Helper()
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = workspace
	command.Env = environment
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("git %s timed out: %v", arguments[0], ctx.Err())
		}
		t.Fatalf("git %s failed: %v", arguments[0], err)
	}
	return string(output)
}

func nativePiRepositoryTaskMaterializeSubject(t *testing.T, ctx context.Context, root, repository, commit string) protocol.Subject {
	t.Helper()
	manager := workspace.New(map[string]config.Workspace{
		"native": {
			Policy:     config.WorkspacePolicyGitWorktree,
			Repository: repository,
			Root:       filepath.Join(root, "subject-worktrees"),
			Ref:        "HEAD",
			Cleanup:    config.CleanupAlways,
		},
	})
	subject, err := manager.MaterializeSubject(ctx, "native", nativeRepositoryTaskSubjectResourceID, commit)
	if err != nil {
		t.Fatalf("materialize native Pi repository task subject: %v", err)
	}
	return subject
}

func nativePiRepositoryTaskWriteJSON(filename string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func nativePiRepositoryTaskReadJSON(t *testing.T, filename string, value any) {
	t.Helper()
	encoded, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read native Pi repository task identity: %v", err)
	}
	if err := json.Unmarshal(encoded, value); err != nil {
		t.Fatalf("decode native Pi repository task identity: %v", err)
	}
}

func nativePiRepositoryTaskExpectedResult(t *testing.T, subject protocol.Subject) protocol.TaskResult {
	t.Helper()
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatalf("hash native Pi repository task subject: %v", err)
	}
	result := protocol.TaskResult{
		SchemaVersion: protocol.TaskResultSchemaVersion,
		ResultID:      nativeRepositoryTaskResultID,
		Kind:          protocol.TaskResultProgress,
		Summary:       nativeRepositoryTaskResultSummary,
		Subject:       subject,
		SubjectHash:   subjectHash,
		EvidenceRefs:  []string{},
		Diagnostics:   []protocol.Diagnostic{},
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("validate expected native Pi task result: %v", err)
	}
	return result
}

func nativePiRepositoryTaskGoal(expectedResult string) string {
	return "Perform only this bounded repository task. Create " + nativeRepositoryTaskArtifact +
		" at the workspace root containing exactly this UTF-8 text, including its final newline:\n" +
		nativeRepositoryTaskArtifactContents +
		"Do not create, modify, or delete any other workspace file. Use the available write tool only. " +
		"After the file exists, return exactly the following JSON object as the entire final assistant text, with no Markdown, prose, or code fence:\n" +
		expectedResult
}

type nativePiRepositoryTaskEvents struct {
	mutex          sync.Mutex
	nativeFrames   int
	sessionStarted int
	taskResults    int
}

func (events *nativePiRepositoryTaskEvents) handle(_ context.Context, event harness.Event) error {
	events.mutex.Lock()
	defer events.mutex.Unlock()
	switch event.Kind {
	case harness.EventNativeFrame:
		events.nativeFrames++
	case harness.EventSessionStarted:
		events.sessionStarted++
	case harness.EventTaskResult:
		events.taskResults++
	}
	return nil
}

func (events *nativePiRepositoryTaskEvents) nativeFrameCount() int {
	events.mutex.Lock()
	defer events.mutex.Unlock()
	return events.nativeFrames
}

func (events *nativePiRepositoryTaskEvents) semanticCounts() (sessionStarted, taskResults int) {
	events.mutex.Lock()
	defer events.mutex.Unlock()
	return events.sessionStarted, events.taskResults
}
