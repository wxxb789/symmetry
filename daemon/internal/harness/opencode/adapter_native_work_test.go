//go:build linux || windows

package opencode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const (
	nativeAdmissionReplayEnabledEnv       = "SYMMETRY_OPENCODE_NATIVE_ADMISSION_REPLAY"
	nativeAdmissionReplayExecutableEnv    = "SYMMETRY_OPENCODE_NATIVE_ADMISSION_REPLAY_EXECUTABLE"
	nativeSyntheticToolEnabledEnv         = "SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL"
	nativeSyntheticToolExecutableEnv      = "SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL_EXECUTABLE"
	nativeRealRepositoryTaskEnabledEnv    = "SYMMETRY_OPENCODE_NATIVE_REPOSITORY_TASK"
	nativeRealRepositoryTaskExecutableEnv = "SYMMETRY_OPENCODE_NATIVE_REPOSITORY_TASK_EXECUTABLE"
	nativeRealRepositoryTaskBaseURLEnv    = "SYMMETRY_OPENCODE_NATIVE_REPOSITORY_TASK_BASE_URL"
	nativeResumeCaptureEnabledEnv         = "SYMMETRY_OPENCODE_NATIVE_RESUME_CAPTURE"
	nativeResumeCaptureExecutableEnv      = "SYMMETRY_OPENCODE_NATIVE_RESUME_CAPTURE_EXECUTABLE"
	nativeResumeCaptureBaseURLEnv         = "SYMMETRY_OPENCODE_NATIVE_RESUME_CAPTURE_BASE_URL"
	nativeOpenCodeObserverMaxRequestBytes = 8 << 20
	nativeOpenCodeEventCaptureMaximum     = 16
	nativeAdmissionReplayTimeout          = 45 * time.Second
	nativeRepositoryTaskTimeout           = 2 * time.Minute
	nativeRepositoryTaskCloseTimeout      = 30 * time.Second
	nativeRepositoryTaskGitTimeout        = 10 * time.Second
	nativeRepositoryTaskArtifact          = "opencode-native-evidence.txt"
	nativeRepositoryTaskContent           = "native opencode repository task evidence\n"
	nativeSyntheticProvider               = "symmetry-native-loopback"
	nativeSyntheticModel                  = "symmetry-opencode-test"
	nativeSyntheticAPIKey                 = "symmetry-opencode-loopback-test-key"
	nativeRealProvider                    = "openai"
	nativeRealModel                       = "gpt-5.6-terra"
	nativeRealAPIKeyEnv                   = "SYMMETRY_OPENCODE_LOOPBACK_KEY"
	nativeRealAPIKey                      = "dummy"
)

// TestNativeSyntheticGatewayToolIntegration is deliberately opt-in and deferred.
// Direct Resume:false prompts only record admission; the production StartTurn
// work path sends explicit Resume:true, while terminal semantics remain
// fail-closed. The test proves only a synthetic loopback tool interaction by
// the real OpenCode binary in an isolated repository. It is not native resume,
// real-model, or Goal evidence.
func TestNativeSyntheticGatewayToolIntegration(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("native OpenCode synthetic tool integration supports Linux and Windows only")
	}
	if os.Getenv(nativeSyntheticToolEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL=1 to run the synthetic OpenCode tool integration")
	}
	executable := strings.TrimSpace(os.Getenv(nativeSyntheticToolExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute OpenCode %s executable", nativeSyntheticToolExecutableEnv, TestedVersion)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("native OpenCode executable must be an existing regular file")
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create native OpenCode workspace: %v", err)
	}
	gitEnvironment := nativeOpenCodeRepositoryTaskGitEnvironment(t, root)
	gitContext, gitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	nativeOpenCodeRepositoryTaskInitializeGit(t, gitContext, gitEnvironment, workspace)
	baselineHead := nativeOpenCodeRepositoryTaskGitOutput(t, gitContext, gitEnvironment, workspace, "rev-parse", "HEAD")
	gitCancel()

	target := filepath.Join(workspace, nativeRepositoryTaskArtifact)
	gateway := newNativeOpenCodeRepositoryTaskGateway(t, target)
	defer gateway.Close()
	environment := nativeOpenCodeRepositoryTaskEnvironment(t, root, gateway.baseURL)
	nativeOpenCodeRepositoryTaskCheckEnvironment(t, environment, gateway.baseURL)
	nativeOpenCodeRepositoryTaskCheckVersion(t, executable, workspace, environment)

	processRecord := filepath.Join(root, "process-identity.json")
	var persistMutex sync.Mutex
	persistedPID := 0
	persistedIdentity := ""
	sink := &nativeOpenCodeRepositoryTaskSink{}
	startContext, startCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer startCancel()
	session, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace: workspace,
		Invocation: execution.Invocation{
			Env: environment,
		},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			persistMutex.Lock()
			persistedPID = pid
			persistedIdentity = identity
			persistMutex.Unlock()
			return nativeOpenCodeRepositoryTaskWriteJSON(processRecord, map[string]any{
				"pid":      pid,
				"identity": identity,
			})
		},
	}, sink)
	if err != nil {
		t.Fatalf("start native OpenCode repository task: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Errorf("cleanup native OpenCode repository task: %v", err)
		}
		if _, err := session.Wait(cleanupContext); err != nil {
			t.Errorf("wait for native OpenCode repository task cleanup: %v", err)
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native OpenCode adapter did not return a staged session")
	}
	persistMutex.Lock()
	gotPersistedPID, gotPersistedIdentity := persistedPID, persistedIdentity
	persistMutex.Unlock()
	pid, identity := staged.ProcessDetails()
	if gotPersistedPID != pid || gotPersistedIdentity != identity || pid <= 0 || identity == "" {
		t.Fatal("native OpenCode process identity was not persisted before session exposure")
	}
	persistedBytes, err := os.ReadFile(processRecord)
	if err != nil {
		t.Fatalf("read persisted native OpenCode process identity: %v", err)
	}
	var persisted struct {
		PID      int    `json:"pid"`
		Identity string `json:"identity"`
	}
	if err := json.Unmarshal(persistedBytes, &persisted); err != nil || persisted.PID != pid || persisted.Identity != identity {
		t.Fatalf("persisted native OpenCode process identity = %+v, want pid=%d identity=%q", persisted, pid, identity)
	}

	openContext, openCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer openCancel()
	handle, err := staged.Open(openContext)
	if err != nil {
		t.Fatalf("open native OpenCode repository session: %v", err)
	}
	if handle.ID == "" {
		t.Fatal("native OpenCode session returned an empty identity")
	}
	replayedHandle, err := staged.Open(openContext)
	if err != nil || replayedHandle != handle {
		t.Fatalf("replayed Open() = %+v, %v; want stable native identity", replayedHandle, err)
	}

	turnContext, turnCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer turnCancel()
	if err := staged.StartTurn(turnContext, harness.TurnRequest{
		Goal:    fmt.Sprintf("Use the write tool exactly once to create %s with exactly these bytes: %q. Do not read files, run commands, or edit anything else.", nativeRepositoryTaskArtifact, nativeRepositoryTaskContent),
		Context: json.RawMessage(fmt.Sprintf(`{"artifact":"%s","content":"%s"}`, nativeRepositoryTaskArtifact, strings.TrimSuffix(nativeRepositoryTaskContent, "\n"))),
	}); err != nil {
		t.Fatalf("start native OpenCode prompt: %v", err)
	}

	if err := nativeOpenCodeRepositoryTaskWaitForArtifact(turnContext, target, []byte(nativeRepositoryTaskContent)); err != nil {
		t.Fatalf("wait for exact native OpenCode artifact: %v; gateway=%v", err, gateway.validate())
	}
	if err := gateway.waitForCompletion(turnContext); err != nil {
		t.Fatalf("%v; gateway=%v", err, gateway.validate())
	}
	if !sink.has(harness.EventSessionStarted) || !sink.has(harness.EventNativeFrame) {
		t.Fatalf("events = %#v; want session_started and native-frame evidence", sink.snapshot())
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatalf("close native OpenCode repository task: %v", err)
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatalf("wait native OpenCode repository task: %v", err)
	}
	closed = true
	if result.Kind != harness.ResultUnknown || result.Semantic != nil || !result.Process.Terminated ||
		result.Process.SinkError != nil || result.Process.OutputError != nil ||
		result.Process.TerminationError != nil || result.Process.ContainmentError != nil || result.Process.WaitError != nil {
		t.Fatalf("native OpenCode repository task result = %+v; want unknown with clean bounded process stop", result)
	}

	verificationContext, verificationCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer verificationCancel()
	if head := nativeOpenCodeRepositoryTaskGitOutput(t, verificationContext, gitEnvironment, workspace, "rev-parse", "HEAD"); head != baselineHead {
		t.Fatalf("native OpenCode repository task changed HEAD: got %s, want %s", head, baselineHead)
	}
	status := nativeOpenCodeRepositoryTaskGitOutput(t, verificationContext, gitEnvironment, workspace, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching")
	if status != "?? "+nativeRepositoryTaskArtifact {
		t.Fatalf("native OpenCode repository task changed unexpected paths: %q", status)
	}
}

// TestNativePromptAdmissionReplaysAfterPost is deliberately opt-in. It uses
// the production Start/Open path, posts one Resume:false prompt directly, and
// proves that the native durable admission event can be read and replayed from
// after=0. It never calls StartTurn, starts a model loop, or claims task evidence.
func TestNativePromptAdmissionReplaysAfterPost(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("native OpenCode admission replay supports Linux and Windows only")
	}
	if os.Getenv(nativeAdmissionReplayEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_NATIVE_ADMISSION_REPLAY=1 to run the native OpenCode admission replay")
	}
	executable := strings.TrimSpace(os.Getenv(nativeAdmissionReplayExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute OpenCode %s executable", nativeAdmissionReplayExecutableEnv, TestedVersion)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("native OpenCode executable must be an existing regular file")
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create native OpenCode workspace: %v", err)
	}
	environment := nativeOpenCodeSmokeEnvironment(t, root)
	nativeOpenCodeRepositoryTaskCheckVersion(t, executable, workspace, environment)

	startContext, startCancel := context.WithTimeout(context.Background(), nativeAdmissionReplayTimeout)
	defer startCancel()
	session, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace:  workspace,
		Invocation: execution.Invocation{Env: environment},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			return nil
		},
	}, harness.EventSinkFunc(func(context.Context, harness.Event) error { return nil }))
	if err != nil {
		t.Fatalf("start native OpenCode admission replay: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeAdmissionReplayTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Errorf("cleanup native OpenCode admission replay: %v", err)
		}
		if _, err := session.Wait(cleanupContext); err != nil {
			t.Errorf("wait for native OpenCode admission replay cleanup: %v", err)
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native OpenCode adapter did not return a staged session")
	}
	openContext, openCancel := context.WithTimeout(context.Background(), nativeAdmissionReplayTimeout)
	defer openCancel()
	handle, err := staged.Open(openContext)
	if err != nil {
		t.Fatalf("open native OpenCode admission replay session: %v", err)
	}
	if handle.ID == "" {
		t.Fatal("native OpenCode admission replay session returned an empty identity")
	}

	native, ok := session.(*nativeSession)
	if !ok {
		t.Fatalf("native OpenCode session = %T, want *nativeSession", session)
	}
	native.mu.Lock()
	client, ok := native.client.(*Client)
	native.mu.Unlock()
	if !ok || client == nil {
		t.Fatal("native OpenCode session did not retain its verified production client after Open")
	}

	prompt := PromptRequest{
		ID:       randomID("msg_"),
		Text:     "Symmetry native admission replay probe " + randomID("probe_"),
		Delivery: "steer",
		Resume:   false,
	}
	promptContext, promptCancel := context.WithTimeout(context.Background(), nativeAdmissionReplayTimeout)
	defer promptCancel()
	admission, err := client.Prompt(promptContext, handle.ID, prompt)
	if err != nil {
		t.Fatalf("post native OpenCode prompt admission: %v", err)
	}
	if admission.ID != prompt.ID || admission.SessionID != handle.ID || admission.Text != prompt.Text || admission.Delivery != prompt.Delivery || admission.AdmittedSeq == 0 {
		t.Fatalf("prompt admission = %+v, want request identity and a positive admitted sequence", admission)
	}

	firstContext, firstCancel := context.WithTimeout(context.Background(), nativeAdmissionReplayTimeout)
	defer firstCancel()
	firstStream, err := client.OpenSessionEvents(firstContext, handle.ID, 0)
	if err != nil {
		t.Fatalf("open native OpenCode admission stream after prompt: %v", err)
	}
	first, err := nativeOpenCodeReadPromptAdmittedEvent(firstContext, firstStream, handle.ID, prompt)
	_ = firstStream.Close()
	if err != nil {
		t.Fatalf("read native OpenCode prompt admission event: %v", err)
	}
	if first.Durable == nil || first.Durable.Seq != admission.AdmittedSeq {
		t.Fatalf("prompt event durable cursor = %+v, want admission sequence %d", first.Durable, admission.AdmittedSeq)
	}

	replayContext, replayCancel := context.WithTimeout(context.Background(), nativeAdmissionReplayTimeout)
	defer replayCancel()
	replayStream, err := client.OpenSessionEvents(replayContext, handle.ID, 0)
	if err != nil {
		t.Fatalf("reopen native OpenCode admission stream: %v", err)
	}
	replayed, err := nativeOpenCodeReadPromptAdmittedEvent(replayContext, replayStream, handle.ID, prompt)
	_ = replayStream.Close()
	if err != nil {
		t.Fatalf("replay native OpenCode prompt admission event: %v", err)
	}
	if replayed.Durable == nil || first.Durable == nil || replayed.ID != first.ID || replayed.Durable.Seq != first.Durable.Seq || replayed.Durable.Version != first.Durable.Version || replayed.Text != first.Text || replayed.MessageID != first.MessageID {
		t.Fatalf("replayed prompt event = %+v, want same durable identity and text as %+v", replayed, first)
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeAdmissionReplayTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatalf("close native OpenCode admission replay: %v", err)
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatalf("wait native OpenCode admission replay: %v", err)
	}
	closed = true
	// Close intentionally terminates the server; a platform-specific non-zero
	// WaitError is expected once Terminated is true and is not a cleanup fault.
	if result.Kind != harness.ResultUnknown || result.Semantic != nil || !result.Process.Terminated || result.Process.SinkError != nil || result.Process.OutputError != nil || result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatalf("native OpenCode admission replay result = %+v; want unknown with clean bounded process stop", result)
	}
}

// TestNativeResumeEventCapture is deliberately opt-in and is a capture scaffold,
// not Goal evidence. Resume:true here is only OpenCode's raw prompt option; it
// does not prove Symmetry native resume, terminal outcome, usage, handoff,
// cancellation, or any capability. The test keeps the OpenCode child in a
// fresh HOME/config/data/cache/temp tree, sends no parent credential into that
// child, and logs only redacted event metadata. Its loopback observer forwards
// the real /v1/responses request without supplying a gateway response.
func TestNativeResumeEventCapture(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("native OpenCode resume event capture supports Linux and Windows only")
	}
	if os.Getenv(nativeResumeCaptureEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_NATIVE_RESUME_CAPTURE=1 to run the native OpenCode Resume:true event capture scaffold")
	}
	executable := strings.TrimSpace(os.Getenv(nativeResumeCaptureExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute OpenCode %s executable", nativeResumeCaptureExecutableEnv, TestedVersion)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("native OpenCode executable must be an existing regular file")
	}
	upstream := nativeOpenCodeRequiredLoopbackUpstream(t, nativeResumeCaptureBaseURLEnv)

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create isolated native OpenCode resume capture workspace: %v", err)
	}
	observer := newNativeOpenCodeRepositoryTaskObserver(t, upstream)
	defer observer.Close()
	environment := nativeOpenCodeRepositoryTaskEnvironmentWithConfig(t, root, nativeOpenCodeRepositoryTaskRealConfig(observer.baseURL), nativeRealAPIKeyEnv+"="+nativeRealAPIKey)
	nativeOpenCodeRepositoryTaskCheckEnvironment(t, environment, observer.baseURL)
	nativeOpenCodeResumeCaptureCheckIsolation(t, root, environment)
	nativeOpenCodeRepositoryTaskCheckVersion(t, executable, workspace, environment)

	startContext, startCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer startCancel()
	session, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace:  workspace,
		Invocation: execution.Invocation{Env: environment},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			return nil
		},
	}, harness.EventSinkFunc(func(context.Context, harness.Event) error { return nil }))
	if err != nil {
		t.Fatalf("start native OpenCode Resume:true event capture: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Errorf("cleanup native OpenCode Resume:true event capture: %v", err)
		}
		if _, err := session.Wait(cleanupContext); err != nil {
			t.Errorf("wait for native OpenCode Resume:true event capture cleanup: %v", err)
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native OpenCode adapter did not return a staged session")
	}
	openContext, openCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer openCancel()
	handle, err := staged.Open(openContext)
	if err != nil {
		t.Fatalf("open native OpenCode Resume:true event capture session: %v", err)
	}
	if handle.ID == "" {
		t.Fatal("native OpenCode Resume:true event capture session returned an empty identity")
	}
	native, ok := session.(*nativeSession)
	if !ok {
		t.Fatalf("native OpenCode session = %T, want *nativeSession", session)
	}
	native.mu.Lock()
	client, ok := native.client.(*Client)
	native.mu.Unlock()
	if !ok || client == nil {
		t.Fatal("native OpenCode session did not retain its verified production client after Open")
	}

	prompt := PromptRequest{
		ID:       randomID("msg_"),
		Text:     "Symmetry native Resume:true event capture probe " + randomID("probe_"),
		Delivery: "steer",
		Resume:   true,
	}
	promptContext, promptCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer promptCancel()
	admission, err := client.Prompt(promptContext, handle.ID, prompt)
	if err != nil {
		t.Fatalf("post native OpenCode Resume:true prompt: %v", err)
	}
	if admission.ID != prompt.ID || admission.SessionID != handle.ID || admission.Text != prompt.Text || admission.Delivery != prompt.Delivery || admission.AdmittedSeq == 0 {
		t.Fatalf("native OpenCode Resume:true prompt admission = %+v, want request identity and a positive admitted sequence", admission)
	}

	// Keep the capture order explicit: admit Resume:true first, then replay the
	// retained session stream from the beginning through the private client.
	eventsContext, eventsCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	eventStream, err := client.OpenSessionEvents(eventsContext, handle.ID, 0)
	if err != nil {
		eventsCancel()
		t.Fatalf("open native OpenCode Resume:true event stream: %v", err)
	}
	captured := make(chan nativeOpenCodeEventCaptureResult, 1)
	captureJoined := false
	t.Cleanup(func() {
		eventsCancel()
		_ = eventStream.Close()
		if captureJoined {
			return
		}
		joinContext, joinCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer joinCancel()
		select {
		case <-captured:
		case <-joinContext.Done():
			t.Errorf("join native OpenCode Resume:true event capture cleanup: %v", joinContext.Err())
		}
	})
	go func() {
		capture := nativeOpenCodeCaptureResumeEventMetadata(eventsContext, eventStream, nativeOpenCodeEventCaptureMaximum, handle.ID, prompt.ID, admission.AdmittedSeq)
		captured <- capture
	}()
	if err := observer.waitForRequest(promptContext); err != nil {
		t.Fatal(err)
	}

	var capture nativeOpenCodeEventCaptureResult
	select {
	case capture = <-captured:
		captureJoined = true
	case <-eventsContext.Done():
		t.Fatalf("capture native OpenCode Resume:true event metadata: %v", eventsContext.Err())
	}
	eventsCancel()
	_ = eventStream.Close()
	if capture.err != nil {
		t.Logf("native OpenCode Resume:true capture stopped with diagnostic error: %v", capture.err)
	}
	if !capture.admissionObserved {
		t.Fatalf("native OpenCode Resume:true capture did not bind the admitted prompt: %s", nativeOpenCodeEventCaptureSummary(capture))
	}
	t.Logf("native OpenCode Resume:true capture characterization: %s", nativeOpenCodeEventCaptureSummary(capture))
	for _, event := range capture.events {
		t.Logf("native OpenCode Resume:true event metadata (not Goal evidence): type=%q kind=%q cursor=%d version=%d event=%q aggregate=%q session=%q message=%q assistant=%q data_bytes=%d", event.Type, event.Kind, event.Cursor, event.Version, event.EventID, event.AggregateID, event.SessionID, event.MessageID, event.AssistantMessageID, event.DataBytes)
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatalf("close native OpenCode Resume:true event capture: %v", err)
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatalf("wait native OpenCode Resume:true event capture: %v", err)
	}
	closed = true
	if result.Kind != harness.ResultUnknown || result.Semantic != nil || !result.Process.Terminated ||
		result.Process.SinkError != nil || result.Process.OutputError != nil ||
		result.Process.TerminationError != nil || result.Process.ContainmentError != nil || result.Process.WaitError != nil {
		t.Fatalf("native OpenCode Resume:true event capture result = %+v, want unknown with bounded process stop", result)
	}
}

type nativeOpenCodeEventCaptureStopReason string

const (
	nativeOpenCodeCaptureStopTerminal         nativeOpenCodeEventCaptureStopReason = "terminal_candidate"
	nativeOpenCodeCaptureStopStreamEnded      nativeOpenCodeEventCaptureStopReason = "stream_ended_without_terminal"
	nativeOpenCodeCaptureStopTimeout          nativeOpenCodeEventCaptureStopReason = "timeout"
	nativeOpenCodeCaptureStopCancelled        nativeOpenCodeEventCaptureStopReason = "cancelled"
	nativeOpenCodeCaptureStopDisconnected     nativeOpenCodeEventCaptureStopReason = "stream_disconnected"
	nativeOpenCodeCaptureStopMaximum          nativeOpenCodeEventCaptureStopReason = "maximum_events"
	nativeOpenCodeCaptureStopAdmissionMissing nativeOpenCodeEventCaptureStopReason = "admission_missing"
	nativeOpenCodeCaptureStopNextAdmission    nativeOpenCodeEventCaptureStopReason = "next_admission"
	nativeOpenCodeCaptureStopDecodeError      nativeOpenCodeEventCaptureStopReason = "decode_error"
	nativeOpenCodeCaptureStopInvalidInput     nativeOpenCodeEventCaptureStopReason = "invalid_input"
)

const (
	nativeOpenCodeEventCaptureTypeMaximum  = 96
	nativeOpenCodeEventCaptureErrorMaximum = 256
	nativeOpenCodeEventCaptureDataMaximum  = 64 * 1024
)

type nativeOpenCodeEventCaptureResult struct {
	events             []nativeOpenCodeEventMetadata
	err                error
	stopReason         nativeOpenCodeEventCaptureStopReason
	admissionObserved  bool
	terminalObserved   bool
	observedEvents     int
	preAdmissionEvents int
	lastCursor         uint64
	lastType           string
}

type nativeOpenCodeEventMetadata struct {
	Type               string
	Kind               SessionEventKind
	EventID            string
	Cursor             uint64
	Version            uint64
	AggregateID        string
	SessionID          string
	MessageID          string
	AssistantMessageID string
	DataBytes          int
}

var errNativeOpenCodeResumeCaptureMissingAdmission = errors.New("native OpenCode resume capture did not observe the matching admission event")

func nativeOpenCodeCaptureSessionEventMetadata(ctx context.Context, stream io.ReadCloser, maximum int) ([]nativeOpenCodeEventMetadata, error) {
	return nativeOpenCodeCaptureSessionEventMetadataUntil(ctx, stream, maximum, func(events []nativeOpenCodeEventMetadata) bool {
		return len(events) >= maximum
	})
}

func nativeOpenCodeCaptureResumeEventMetadata(ctx context.Context, stream io.ReadCloser, maximum int, sessionID, messageID string, cursor uint64) nativeOpenCodeEventCaptureResult {
	capture := nativeOpenCodeEventCaptureResult{}
	if ctx == nil || stream == nil || maximum <= 0 || !isID(sessionID, "ses_") || !isID(messageID, "msg_") || cursor == 0 {
		capture.stopReason = nativeOpenCodeCaptureStopInvalidInput
		capture.err = errors.New("native OpenCode resume event capture input is invalid")
		return capture
	}
	capacity := maximum
	if capacity > nativeOpenCodeEventCaptureMaximum {
		capacity = nativeOpenCodeEventCaptureMaximum
	}
	capture.events = make([]nativeOpenCodeEventMetadata, 0, capacity)
	validator, err := NewSessionEventValidator(sessionID, 0)
	if err != nil {
		capture.stopReason = nativeOpenCodeCaptureStopInvalidInput
		capture.err = nativeOpenCodeBoundedError(err, nativeOpenCodeEventCaptureErrorMaximum)
		return capture
	}
	decoder := NewDecoder(defaultMaxFrameBytes)
	bufferSize := streamReadChunkSize
	if bufferSize <= 0 {
		bufferSize = 32 * 1024
	}
	stopOnCancel := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stopOnCancel()
	if err := ctx.Err(); err != nil {
		capture.stopReason = nativeOpenCodeCaptureStopForContext(ctx)
		capture.err = nativeOpenCodeBoundedError(err, nativeOpenCodeEventCaptureErrorMaximum)
		_ = stream.Close()
		return capture
	}

	stop := func(reason nativeOpenCodeEventCaptureStopReason, stopErr error) {
		capture.stopReason = reason
		if stopErr != nil {
			capture.err = nativeOpenCodeBoundedError(stopErr, nativeOpenCodeEventCaptureErrorMaximum)
		}
	}
	observe := func(frames []Frame) bool {
		for _, frame := range frames {
			if capture.observedEvents >= maximum {
				stop(nativeOpenCodeCaptureStopMaximum, nil)
				return true
			}
			capture.observedEvents++
			event, observeErr := validator.ObserveEvent(frame)
			if observeErr != nil {
				stop(nativeOpenCodeCaptureStopDecodeError, observeErr)
				return true
			}
			capture.lastCursor = event.Durable.Seq
			capture.lastType = nativeOpenCodeBoundedString(event.Type, nativeOpenCodeEventCaptureTypeMaximum)
			if !capture.admissionObserved {
				if event.Kind == SessionEventPromptAdmitted && event.PromptAdmitted != nil &&
					event.Durable.Seq == cursor && event.PromptAdmitted.MessageID == messageID {
					capture.admissionObserved = true
					capture.events = append(capture.events, nativeOpenCodeResumeEventMetadata(event))
					if len(capture.events) >= maximum {
						stop(nativeOpenCodeCaptureStopMaximum, nil)
						return true
					}
				} else {
					capture.preAdmissionEvents++
				}
				continue
			}
			if event.Kind == SessionEventPromptAdmitted {
				stop(nativeOpenCodeCaptureStopNextAdmission, nil)
				return true
			}
			capture.events = append(capture.events, nativeOpenCodeResumeEventMetadata(event))
			if nativeOpenCodeResumeEventIsTerminalCandidate(event) {
				capture.terminalObserved = true
				stop(nativeOpenCodeCaptureStopTerminal, nil)
				return true
			}
			if len(capture.events) >= maximum {
				stop(nativeOpenCodeCaptureStopMaximum, nil)
				return true
			}
		}
		return false
	}

	for {
		if err := ctx.Err(); err != nil {
			stop(nativeOpenCodeCaptureStopForContext(ctx), err)
			return capture
		}
		buffer := make([]byte, bufferSize)
		count, readErr := stream.Read(buffer)
		if count > 0 {
			if err := ctx.Err(); err != nil {
				stop(nativeOpenCodeCaptureStopForContext(ctx), err)
				return capture
			}
			frames, feedErr := decoder.Feed(buffer[:count])
			if feedErr != nil {
				stop(nativeOpenCodeCaptureStopDecodeError, feedErr)
				return capture
			}
			if observe(frames) {
				if err := ctx.Err(); err != nil {
					stop(nativeOpenCodeCaptureStopForContext(ctx), err)
				}
				return capture
			}
		}
		if readErr == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			stop(nativeOpenCodeCaptureStopForContext(ctx), err)
			return capture
		}
		if !errors.Is(readErr, io.EOF) {
			stop(nativeOpenCodeCaptureStopDisconnected, readErr)
			return capture
		}
		frames, closeErr := decoder.Close()
		if closeErr != nil {
			stop(nativeOpenCodeCaptureStopDecodeError, closeErr)
			return capture
		}
		if observe(frames) {
			return capture
		}
		if !capture.admissionObserved {
			stop(nativeOpenCodeCaptureStopAdmissionMissing, errNativeOpenCodeResumeCaptureMissingAdmission)
			return capture
		}
		stop(nativeOpenCodeCaptureStopStreamEnded, nil)
		return capture
	}
}

func nativeOpenCodeResumeEventMetadata(event DecodedSessionEvent) nativeOpenCodeEventMetadata {
	metadata := nativeOpenCodeEventMetadata{
		Type:      nativeOpenCodeBoundedString(event.Type, nativeOpenCodeEventCaptureTypeMaximum),
		Kind:      event.Kind,
		EventID:   nativeOpenCodeRedactIdentity(event.ID),
		DataBytes: nativeOpenCodeBoundedByteCount(len(event.Data), nativeOpenCodeEventCaptureDataMaximum),
	}
	if event.Durable != nil {
		metadata.Cursor = event.Durable.Seq
		metadata.Version = event.Durable.Version
		metadata.AggregateID = nativeOpenCodeRedactIdentity(event.Durable.AggregateID)
	}
	metadata.SessionID = nativeOpenCodeRedactIdentity(event.SessionID)
	switch event.Kind {
	case SessionEventPromptAdmitted:
		if event.PromptAdmitted != nil {
			metadata.MessageID = nativeOpenCodeRedactIdentity(event.PromptAdmitted.MessageID)
		}
	case SessionEventStepStarted:
		if event.StepStarted != nil {
			metadata.AssistantMessageID = nativeOpenCodeRedactIdentity(event.StepStarted.AssistantMessageID)
		}
	case SessionEventStepEnded:
		if event.StepEnded != nil {
			metadata.AssistantMessageID = nativeOpenCodeRedactIdentity(event.StepEnded.AssistantMessageID)
		}
	case SessionEventStepFailed:
		if event.StepFailed != nil {
			metadata.AssistantMessageID = nativeOpenCodeRedactIdentity(event.StepFailed.AssistantMessageID)
		}
	case SessionEventTextEnded:
		if event.TextEnded != nil {
			metadata.AssistantMessageID = nativeOpenCodeRedactIdentity(event.TextEnded.AssistantMessageID)
		}
	case SessionEventToolCalled:
		if event.ToolCalled != nil {
			metadata.AssistantMessageID = nativeOpenCodeRedactIdentity(event.ToolCalled.AssistantMessageID)
		}
	case SessionEventToolSuccess:
		if event.ToolSuccess != nil {
			metadata.AssistantMessageID = nativeOpenCodeRedactIdentity(event.ToolSuccess.AssistantMessageID)
		}
	case SessionEventToolFailed:
		if event.ToolFailed != nil {
			metadata.AssistantMessageID = nativeOpenCodeRedactIdentity(event.ToolFailed.AssistantMessageID)
		}
	}
	return metadata
}

func nativeOpenCodeResumeEventIsTerminalCandidate(event DecodedSessionEvent) bool {
	switch event.Kind {
	case SessionEventStepFailed:
		return true
	case SessionEventStepEnded:
		return event.StepEnded != nil && strings.EqualFold(strings.TrimSpace(event.StepEnded.Finish), "stop")
	default:
		return false
	}
}

func nativeOpenCodeCaptureStopForContext(ctx context.Context) nativeOpenCodeEventCaptureStopReason {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nativeOpenCodeCaptureStopTimeout
	}
	return nativeOpenCodeCaptureStopCancelled
}

func nativeOpenCodeBoundedString(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if maximum <= 0 || len(value) <= maximum {
		return value
	}
	if maximum <= 3 {
		return value[:maximum]
	}
	return value[:maximum-3] + "..."
}

func nativeOpenCodeBoundedError(err error, maximum int) error {
	if err == nil {
		return nil
	}
	return nativeOpenCodeCaptureBoundedError{message: nativeOpenCodeBoundedString(nativeOpenCodeRedactError(err.Error()), maximum), cause: err}
}

var nativeOpenCodeCaptureIdentityPattern = regexp.MustCompile(`\b(?:evt|ses|msg|call)_[A-Za-z0-9_-]+\b`)

func nativeOpenCodeRedactError(message string) string {
	return nativeOpenCodeCaptureIdentityPattern.ReplaceAllStringFunc(message, nativeOpenCodeRedactIdentity)
}

type nativeOpenCodeCaptureBoundedError struct {
	message string
	cause   error
}

func (err nativeOpenCodeCaptureBoundedError) Error() string {
	return err.message
}

func (err nativeOpenCodeCaptureBoundedError) Unwrap() error {
	return err.cause
}

func nativeOpenCodeBoundedByteCount(count, maximum int) int {
	if count < 0 {
		return 0
	}
	if maximum > 0 && count > maximum {
		return maximum
	}
	return count
}

func nativeOpenCodeEventCaptureSummary(capture nativeOpenCodeEventCaptureResult) string {
	return fmt.Sprintf("stop_reason=%q admission_observed=%t terminal_candidate=%t observed_events=%d recorded_events=%d pre_admission_events=%d last_cursor=%d last_type=%q err=%v", capture.stopReason, capture.admissionObserved, capture.terminalObserved, capture.observedEvents, len(capture.events), capture.preAdmissionEvents, capture.lastCursor, capture.lastType, capture.err)
}

func nativeOpenCodeCaptureSessionEventMetadataUntil(ctx context.Context, stream io.ReadCloser, maximum int, complete func([]nativeOpenCodeEventMetadata) bool) ([]nativeOpenCodeEventMetadata, error) {
	if ctx == nil {
		return nil, errors.New("native OpenCode event capture context is nil")
	}
	if stream == nil {
		return nil, errors.New("native OpenCode event capture stream is nil")
	}
	if maximum <= 0 {
		return nil, errors.New("native OpenCode event capture maximum must be positive")
	}
	if complete == nil {
		return nil, errors.New("native OpenCode event capture completion predicate is nil")
	}
	decoder := NewDecoder(defaultMaxFrameBytes)
	bufferSize := streamReadChunkSize
	if bufferSize <= 0 {
		bufferSize = 32 * 1024
	}
	if err := ctx.Err(); err != nil {
		_ = stream.Close()
		return nil, err
	}
	stopOnCancel := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stopOnCancel()
	events := make([]nativeOpenCodeEventMetadata, 0, maximum)
	observe := func(frames []Frame) error {
		for _, frame := range frames {
			event, err := nativeOpenCodeDecodeEventMetadata(frame)
			if err != nil {
				return err
			}
			events = append(events, event)
			if len(events) >= maximum || complete(events) {
				return nil
			}
		}
		return nil
	}
	for len(events) < maximum && !complete(events) {
		buffer := make([]byte, bufferSize)
		count, readErr := stream.Read(buffer)
		if count > 0 {
			frames, err := decoder.Feed(buffer[:count])
			if err != nil {
				return events, err
			}
			if err := observe(frames); err != nil {
				return events, err
			}
			if complete(events) {
				return events, nil
			}
		}
		if readErr == nil {
			continue
		}
		if ctx.Err() != nil {
			return events, ctx.Err()
		}
		if !errors.Is(readErr, io.EOF) {
			return events, readErr
		}
		frames, err := decoder.Close()
		if err != nil {
			return events, err
		}
		if err := observe(frames); err != nil {
			return events, err
		}
		if complete(events) {
			return events, nil
		}
		if len(events) == 0 {
			return events, errStreamEndedUnknown
		}
		return events, nil
	}
	return events, nil
}

func nativeOpenCodeDecodeEventMetadata(frame Frame) (nativeOpenCodeEventMetadata, error) {
	object, err := strictObject(frame.Data)
	if err != nil {
		return nativeOpenCodeEventMetadata{}, fmt.Errorf("decode native OpenCode event metadata: %w", err)
	}
	typeValue, ok := requiredString(object, "type")
	if !ok || strings.TrimSpace(typeValue) == "" {
		return nativeOpenCodeEventMetadata{}, errors.New("native OpenCode event capture observed an event without a type")
	}
	metadata := nativeOpenCodeEventMetadata{Type: strings.TrimSpace(typeValue)}
	if value, ok := requiredString(object, "id"); ok {
		metadata.EventID = nativeOpenCodeRedactIdentity(value)
	}
	if raw, exists := object["durable"]; exists {
		cursor, err := decodeDurableCursor(raw)
		if err != nil {
			return nativeOpenCodeEventMetadata{}, fmt.Errorf("decode native OpenCode event cursor: %w", err)
		}
		metadata.Cursor = cursor.Seq
		metadata.Version = cursor.Version
		metadata.AggregateID = nativeOpenCodeRedactIdentity(cursor.AggregateID)
	}
	if raw, exists := object["data"]; exists {
		if data, err := strictObject(raw); err == nil {
			if value, ok := requiredString(data, "sessionID"); ok {
				metadata.SessionID = nativeOpenCodeRedactIdentity(value)
			}
			if value, ok := requiredString(data, "messageID"); ok {
				metadata.MessageID = nativeOpenCodeRedactIdentity(value)
			}
		}
	}
	return metadata, nil
}

func nativeOpenCodeRedactIdentity(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"evt_", "ses_", "msg_"} {
		if strings.HasPrefix(value, prefix) {
			digest := sha256.Sum256([]byte(value))
			return fmt.Sprintf("%s[redacted:%x]", prefix, digest[:6])
		}
	}
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("[redacted:%x]", digest[:6])
}

func TestNativeOpenCodeResumeCaptureMetadataRedactsPayload(t *testing.T) {
	stream := io.NopCloser(strings.NewReader("data: {\"id\":\"evt_private\",\"type\":\"session.next.prompt.admitted\",\"durable\":{\"aggregateID\":\"ses_private\",\"seq\":7,\"version\":2},\"data\":{\"sessionID\":\"ses_private\",\"messageID\":\"msg_private\",\"prompt\":{\"text\":\"do not record this\"}}}\n\n"))
	events, err := nativeOpenCodeCaptureSessionEventMetadata(context.Background(), stream, 1)
	if err != nil {
		t.Fatalf("capture event metadata: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("captured %d events, want 1", len(events))
	}
	event := events[0]
	if event.Type != "session.next.prompt.admitted" || event.Cursor != 7 || event.Version != 2 {
		t.Fatalf("event metadata = %+v, want type/cursor/version", event)
	}
	for _, identity := range []string{event.EventID, event.AggregateID, event.SessionID, event.MessageID} {
		if strings.Contains(identity, "private") {
			t.Fatalf("event metadata leaked identity: %q", identity)
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", event), "do not record this") {
		t.Fatal("event metadata leaked prompt text")
	}
}

func TestNativeOpenCodeResumeCaptureBindsPromptAndRecordsLifecycle(t *testing.T) {
	const sessionID = "ses_capture"
	const messageID = "msg_capture"
	stream := strings.Join([]string{
		nativeOpenCodeCaptureFrame(`{"id":"evt_prior","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_capture","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_capture","messageID":"msg_other","prompt":{"text":"private prior prompt"},"delivery":"steer"}}`),
		nativeOpenCodeCaptureFrame(`{"id":"evt_admission","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_capture","seq":2,"version":1},"data":{"timestamp":2,"sessionID":"ses_capture","messageID":"msg_capture","prompt":{"text":"private target prompt"},"delivery":"steer"}}`),
		nativeOpenCodeCaptureFrame(`{"id":"evt_step_started","type":"session.next.step.started","durable":{"aggregateID":"ses_capture","seq":3,"version":1},"data":{"timestamp":3,"sessionID":"ses_capture","assistantMessageID":"msg_assistant","agent":"build","model":{"id":"model","providerID":"provider"}}}`),
		nativeOpenCodeCaptureFrame(`{"id":"evt_tool_called","type":"session.next.tool.called","durable":{"aggregateID":"ses_capture","seq":4,"version":1},"data":{"timestamp":4,"sessionID":"ses_capture","assistantMessageID":"msg_assistant","callID":"call_1","tool":"read","input":{"path":"private-path"},"provider":{"executed":true}}}`),
		nativeOpenCodeCaptureFrame(`{"id":"evt_tool_failed","type":"session.next.tool.failed","durable":{"aggregateID":"ses_capture","seq":5,"version":1},"data":{"timestamp":5,"sessionID":"ses_capture","assistantMessageID":"msg_assistant","callID":"call_1","error":{"type":"unknown","message":"private-error"},"provider":{"executed":false}}}`),
		nativeOpenCodeCaptureFrame(`{"id":"evt_retried","type":"session.next.retried","durable":{"aggregateID":"ses_capture","seq":6,"version":1},"data":{"timestamp":6,"sessionID":"ses_capture","attempt":1,"error":{"message":"private-retry","isRetryable":true}}}`),
		nativeOpenCodeCaptureFrame(`{"id":"evt_text_ended","type":"session.next.text.ended","durable":{"aggregateID":"ses_capture","seq":7,"version":1},"data":{"timestamp":7,"sessionID":"ses_capture","assistantMessageID":"msg_assistant","textID":"part_1","text":"private text"}}`),
		nativeOpenCodeCaptureFrame(`{"id":"evt_step_ended","type":"session.next.step.ended","durable":{"aggregateID":"ses_capture","seq":8,"version":2},"data":{"timestamp":8,"sessionID":"ses_capture","assistantMessageID":"msg_assistant","finish":"stop","cost":0,"tokens":{"input":1,"output":1,"reasoning":0,"cache":{"read":0,"write":0}}}}`),
	}, "")
	capture := nativeOpenCodeCaptureResumeEventMetadata(context.Background(), io.NopCloser(strings.NewReader(stream)), 16, sessionID, messageID, 2)
	if capture.err != nil || capture.stopReason != nativeOpenCodeCaptureStopTerminal || !capture.admissionObserved || !capture.terminalObserved {
		t.Fatalf("capture = %s", nativeOpenCodeEventCaptureSummary(capture))
	}
	wantKinds := []SessionEventKind{SessionEventPromptAdmitted, SessionEventStepStarted, SessionEventToolCalled, SessionEventToolFailed, SessionEventRetried, SessionEventTextEnded, SessionEventStepEnded}
	if len(capture.events) != len(wantKinds) {
		t.Fatalf("recorded %d events, want %d: %s", len(capture.events), len(wantKinds), nativeOpenCodeEventCaptureSummary(capture))
	}
	for index, event := range capture.events {
		if event.Kind != wantKinds[index] || event.AggregateID != nativeOpenCodeRedactIdentity(sessionID) || event.SessionID != nativeOpenCodeRedactIdentity(sessionID) {
			t.Fatalf("event[%d] = %+v, want kind=%q and bound session", index, event, wantKinds[index])
		}
		if strings.Contains(fmt.Sprintf("%+v", event), "private") {
			t.Fatalf("event[%d] leaked lifecycle payload: %+v", index, event)
		}
		if len(event.Type) > nativeOpenCodeEventCaptureTypeMaximum || event.DataBytes > nativeOpenCodeEventCaptureDataMaximum {
			t.Fatalf("event[%d] exceeded metadata bounds: %+v", index, event)
		}
	}
	if capture.preAdmissionEvents != 1 {
		t.Fatalf("pre-admission events = %d, want 1", capture.preAdmissionEvents)
	}
}

func TestNativeOpenCodeResumeCaptureRecordsNoTerminalAtStreamEnd(t *testing.T) {
	stream := nativeOpenCodeCaptureFrame(`{"id":"evt_admission","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_capture","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_capture","messageID":"msg_capture","prompt":{"text":"prompt"},"delivery":"steer"}}`) +
		nativeOpenCodeCaptureFrame(`{"id":"evt_text","type":"session.next.text.ended","durable":{"aggregateID":"ses_capture","seq":2,"version":1},"data":{"timestamp":2,"sessionID":"ses_capture","assistantMessageID":"msg_assistant","textID":"part_1","text":"not a semantic result"}}`)
	capture := nativeOpenCodeCaptureResumeEventMetadata(context.Background(), io.NopCloser(strings.NewReader(stream)), 16, "ses_capture", "msg_capture", 1)
	if capture.err != nil || capture.stopReason != nativeOpenCodeCaptureStopStreamEnded || !capture.admissionObserved || capture.terminalObserved {
		t.Fatalf("capture = %s", nativeOpenCodeEventCaptureSummary(capture))
	}
	if len(capture.events) != 2 || capture.events[1].Kind != SessionEventTextEnded {
		t.Fatalf("events = %+v, want admission plus text lifecycle", capture.events)
	}
}

func TestNativeOpenCodeResumeCaptureReportsTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	capture := nativeOpenCodeCaptureResumeEventMetadata(ctx, io.NopCloser(strings.NewReader("")), 16, "ses_capture", "msg_capture", 1)
	if capture.stopReason != nativeOpenCodeCaptureStopTimeout || !errors.Is(capture.err, context.DeadlineExceeded) {
		t.Fatalf("capture = %s, want timeout evidence", nativeOpenCodeEventCaptureSummary(capture))
	}
}

func TestNativeOpenCodeResumeCapturePrefersDeadlineAfterRead(t *testing.T) {
	ctx := newNativeOpenCodeResumeCaptureDeadlineContext()
	terminal := nativeOpenCodeCaptureFrame(`{"id":"evt_terminal","type":"session.next.step.ended","durable":{"aggregateID":"ses_capture","seq":2,"version":2},"data":{"timestamp":2,"sessionID":"ses_capture","assistantMessageID":"msg_assistant","finish":"stop","cost":0,"tokens":{"input":1,"output":1,"reasoning":0,"cache":{"read":0,"write":0}}}}`)
	reader := &nativeOpenCodeResumeCaptureCancellingReader{data: []byte(terminal), afterRead: ctx.expire}
	capture := nativeOpenCodeCaptureResumeEventMetadata(ctx, io.NopCloser(reader), 16, "ses_capture", "msg_capture", 1)
	if capture.stopReason != nativeOpenCodeCaptureStopTimeout || !errors.Is(capture.err, context.DeadlineExceeded) || capture.terminalObserved {
		t.Fatalf("capture = %s, want deadline to win after Read", nativeOpenCodeEventCaptureSummary(capture))
	}
}

func TestNativeOpenCodeResumeCaptureReportsDisconnect(t *testing.T) {
	stream := nativeOpenCodeCaptureFrame(`{"id":"evt_admission","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_capture","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_capture","messageID":"msg_capture","prompt":{"text":"prompt"},"delivery":"steer"}}`)
	streamReader := io.MultiReader(strings.NewReader(stream), nativeOpenCodeResumeCaptureErrorReader{})
	capture := nativeOpenCodeCaptureResumeEventMetadata(context.Background(), io.NopCloser(streamReader), 16, "ses_capture", "msg_capture", 1)
	if capture.stopReason != nativeOpenCodeCaptureStopDisconnected || !capture.admissionObserved || capture.err == nil || !strings.Contains(capture.err.Error(), "synthetic disconnect") {
		t.Fatalf("capture = %s, want disconnect evidence", nativeOpenCodeEventCaptureSummary(capture))
	}
}

func TestNativeOpenCodeResumeCaptureClassifiesTruncatedFrameAsDecodeError(t *testing.T) {
	capture := nativeOpenCodeCaptureResumeEventMetadata(context.Background(), io.NopCloser(strings.NewReader("data: {")), 16, "ses_capture", "msg_capture", 1)
	if capture.stopReason != nativeOpenCodeCaptureStopDecodeError || capture.err == nil || !strings.Contains(capture.err.Error(), "incomplete") {
		t.Fatalf("capture = %s, want decode-error evidence", nativeOpenCodeEventCaptureSummary(capture))
	}
}

func TestNativeOpenCodeResumeCaptureRedactsIdentityInDecodeError(t *testing.T) {
	stream := nativeOpenCodeCaptureFrame(`{"id":"evt_admission","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_other","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_other","messageID":"msg_other","prompt":{"text":"prompt"},"delivery":"steer"}}`)
	capture := nativeOpenCodeCaptureResumeEventMetadata(context.Background(), io.NopCloser(strings.NewReader(stream)), 16, "ses_capture", "msg_capture", 1)
	if capture.stopReason != nativeOpenCodeCaptureStopDecodeError || capture.err == nil {
		t.Fatalf("capture = %s, want identity decode error", nativeOpenCodeEventCaptureSummary(capture))
	}
	if strings.Contains(capture.err.Error(), "ses_other") || strings.Contains(capture.err.Error(), "ses_capture") {
		t.Fatalf("capture error leaked raw identity: %v", capture.err)
	}
}

func TestNativeOpenCodeResumeCaptureStopsAtMaximumEvents(t *testing.T) {
	stream := nativeOpenCodeCaptureFrame(`{"id":"evt_admission","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_capture","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_capture","messageID":"msg_capture","prompt":{"text":"prompt"},"delivery":"steer"}}`) +
		nativeOpenCodeCaptureFrame(`{"id":"evt_text","type":"session.next.text.ended","durable":{"aggregateID":"ses_capture","seq":2,"version":1},"data":{"timestamp":2,"sessionID":"ses_capture","assistantMessageID":"msg_assistant","textID":"part_1","text":"bounded"}}`) +
		nativeOpenCodeCaptureFrame(`{"id":"evt_retry","type":"session.next.retried","durable":{"aggregateID":"ses_capture","seq":3,"version":1},"data":{"timestamp":3,"sessionID":"ses_capture","attempt":1,"error":{"message":"retry","isRetryable":true}}}`)
	capture := nativeOpenCodeCaptureResumeEventMetadata(context.Background(), io.NopCloser(strings.NewReader(stream)), 2, "ses_capture", "msg_capture", 1)
	if capture.err != nil || capture.stopReason != nativeOpenCodeCaptureStopMaximum || len(capture.events) != 2 || !capture.admissionObserved || capture.terminalObserved {
		t.Fatalf("capture = %s, want bounded maximum-event evidence", nativeOpenCodeEventCaptureSummary(capture))
	}
}

func nativeOpenCodeCaptureFrame(data string) string {
	return "data: " + data + "\n\n"
}

type nativeOpenCodeResumeCaptureErrorReader struct{}

func (nativeOpenCodeResumeCaptureErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic disconnect")
}

type nativeOpenCodeResumeCaptureCancellingReader struct {
	data      []byte
	afterRead func()
	done      bool
}

func (reader *nativeOpenCodeResumeCaptureCancellingReader) Read(data []byte) (int, error) {
	if reader.done {
		return 0, io.EOF
	}
	reader.done = true
	count := copy(data, reader.data)
	if reader.afterRead != nil {
		reader.afterRead()
	}
	return count, nil
}

type nativeOpenCodeResumeCaptureDeadlineContext struct {
	done chan struct{}
	once sync.Once
	err  error
}

func newNativeOpenCodeResumeCaptureDeadlineContext() *nativeOpenCodeResumeCaptureDeadlineContext {
	return &nativeOpenCodeResumeCaptureDeadlineContext{done: make(chan struct{})}
}

func (ctx *nativeOpenCodeResumeCaptureDeadlineContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *nativeOpenCodeResumeCaptureDeadlineContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *nativeOpenCodeResumeCaptureDeadlineContext) Err() error {
	return ctx.err
}

func (*nativeOpenCodeResumeCaptureDeadlineContext) Value(any) any {
	return nil
}

func (ctx *nativeOpenCodeResumeCaptureDeadlineContext) expire() {
	ctx.once.Do(func() {
		ctx.err = context.DeadlineExceeded
		close(ctx.done)
	})
}

func TestNativeOpenCodeEndToEndHeadersDropHopByHopFields(t *testing.T) {
	source := make(http.Header)
	source.Add("Connection", "keep-alive, X-Connection-Only")
	source.Set("Keep-Alive", "timeout=5")
	source.Set("X-Connection-Only", "drop")
	source.Set("Authorization", "Bearer synthetic")
	source.Set("Content-Type", "application/json")
	source.Set("X-Trace", "preserve")

	filtered := nativeOpenCodeEndToEndHeaders(source)
	for _, name := range []string{"Connection", "Keep-Alive", "X-Connection-Only"} {
		if value := filtered.Get(name); value != "" {
			t.Fatalf("filtered %s = %q, want omitted", name, value)
		}
	}
	if filtered.Get("Authorization") != "Bearer synthetic" || filtered.Get("Content-Type") != "application/json" || filtered.Get("X-Trace") != "preserve" {
		t.Fatalf("filtered end-to-end headers = %#v, lost a permitted header", filtered)
	}
}

func TestNativeOpenCodeObserverRequestLimitRejectsOversizedRequest(t *testing.T) {
	observer := &nativeOpenCodeRepositoryTaskObserver{}
	request := &http.Request{
		Method:        http.MethodPost,
		URL:           &url.URL{Scheme: "http", Host: "127.0.0.1:1", Path: "/v1/responses"},
		Body:          io.NopCloser(strings.NewReader("")),
		ContentLength: nativeOpenCodeObserverMaxRequestBytes + 1,
		Header:        make(http.Header),
	}
	response := httptest.NewRecorder()
	observer.handle(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized observer request status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := observer.waitForRequest(ctx); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("wait after oversized request = %v, want bounded observer error", err)
	}
}

func TestNativeOpenCodeObserverRejectsUpstreamRedirect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Location", "https://provider.invalid/redirect")
		response.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL + "/v1")
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	observer := &nativeOpenCodeRepositoryTaskObserver{
		upstream: upstreamURL,
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:1/v1/responses", strings.NewReader(`{"model":"gpt-5.6-terra","reasoning":{"effort":"high"}}`))
	response := httptest.NewRecorder()
	observer.handle(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("upstream redirect status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("observer forwarded upstream Location header %q", location)
	}
}

func TestNativeOpenCodeRealConfigUsesLoopbackObserverEndpoint(t *testing.T) {
	requestPath := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestPath <- request.URL.Path
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"id":"offline-observer"}`)
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL + "/v1")
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	observer := newNativeOpenCodeRepositoryTaskObserver(t, upstreamURL)
	defer observer.Close()

	configJSON := nativeOpenCodeRepositoryTaskRealConfig(observer.baseURL)
	nativeOpenCodeAssertRealConfigLoopbackEndpoints(t, configJSON, observer.baseURL)

	request, err := http.NewRequest(http.MethodPost, observer.baseURL+"/responses", strings.NewReader(`{"model":"gpt-5.6-terra","reasoning":{"effort":"high"}}`))
	if err != nil {
		t.Fatalf("create offline observer request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send offline observer request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("offline observer response status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	select {
	case path := <-requestPath:
		if path != "/v1/responses" {
			t.Fatalf("upstream request path = %q, want /v1/responses", path)
		}
	case <-time.After(time.Second):
		t.Fatal("offline observer did not receive the request")
	}
	waitContext, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := observer.waitForRequest(waitContext); err != nil {
		t.Fatalf("wait for offline observer request: %v", err)
	}
}

func nativeOpenCodeAssertRealConfigLoopbackEndpoints(t *testing.T, configJSON, expectedBaseURL string) {
	t.Helper()
	if strings.Contains(configJSON, "api.openai.com") {
		t.Fatal("native OpenCode real config contains the public OpenAI endpoint")
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("decode native OpenCode real config: %v", err)
	}
	providers, ok := config["provider"].(map[string]any)
	if !ok {
		t.Fatal("native OpenCode real config provider object is missing")
	}
	provider, ok := providers[nativeRealProvider].(map[string]any)
	if !ok {
		t.Fatalf("native OpenCode real config provider %q is missing", nativeRealProvider)
	}
	options, ok := provider["options"].(map[string]any)
	if !ok {
		t.Fatal("native OpenCode real config provider options are missing")
	}
	models, ok := provider["models"].(map[string]any)
	if !ok {
		t.Fatal("native OpenCode real config models are missing")
	}
	model, ok := models[nativeRealModel].(map[string]any)
	if !ok {
		t.Fatalf("native OpenCode real config model %q is missing", nativeRealModel)
	}
	nestedProvider, ok := model["provider"].(map[string]any)
	if !ok {
		t.Fatal("native OpenCode real config model provider is missing")
	}
	for field, value := range map[string]any{
		"options.baseURL":    options["baseURL"],
		"model.provider.api": nestedProvider["api"],
	} {
		got, ok := value.(string)
		if !ok || got != expectedBaseURL {
			t.Fatalf("native OpenCode real config %s = %v, want loopback %q", field, value, expectedBaseURL)
		}
		parsed, err := url.Parse(got)
		if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Path != "/v1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			t.Fatalf("native OpenCode real config %s is not a numeric loopback endpoint: %q", field, got)
		}
	}
}

func nativeOpenCodeResumeCaptureCheckIsolation(t *testing.T, root string, environment []string) {
	t.Helper()
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}
	for name, want := range map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"USERPROFILE":     filepath.Join(root, "home"),
		"APPDATA":         filepath.Join(root, "appdata"),
		"LOCALAPPDATA":    filepath.Join(root, "localappdata"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"TEMP":            filepath.Join(root, "tmp"),
		"TMP":             filepath.Join(root, "tmp"),
	} {
		if got := values[name]; got != want {
			t.Fatalf("native OpenCode Resume:true capture %s = %q, want isolated %q", name, got, want)
		}
	}
	for name, value := range values {
		upper := strings.ToUpper(name)
		if !strings.Contains(upper, "KEY") && !strings.Contains(upper, "TOKEN") && !strings.Contains(upper, "SECRET") && !strings.Contains(upper, "CREDENTIAL") && !strings.Contains(upper, "AUTH") {
			continue
		}
		if name != nativeRealAPIKeyEnv || value != nativeRealAPIKey {
			t.Fatalf("native OpenCode Resume:true capture copied credential-like environment variable %s", name)
		}
	}
}

func nativeOpenCodeReadPromptAdmittedEvent(ctx context.Context, stream io.ReadCloser, sessionID string, prompt PromptRequest) (PromptAdmittedEvent, error) {
	if ctx == nil {
		return PromptAdmittedEvent{}, errors.New("native OpenCode admission stream context is nil")
	}
	if stream == nil {
		return PromptAdmittedEvent{}, errors.New("native OpenCode admission stream is nil")
	}
	validator, err := NewSessionEventValidator(sessionID, 0)
	if err != nil {
		return PromptAdmittedEvent{}, err
	}
	decoder := NewDecoder(defaultMaxFrameBytes)
	bufferSize := streamReadChunkSize
	if bufferSize <= 0 {
		bufferSize = 32 * 1024
	}
	if err := ctx.Err(); err != nil {
		_ = stream.Close()
		return PromptAdmittedEvent{}, err
	}
	stopOnCancel := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stopOnCancel()
	for {
		buffer := make([]byte, bufferSize)
		count, readErr := stream.Read(buffer)

		if count > 0 {
			frames, feedErr := decoder.Feed(buffer[:count])
			if feedErr != nil {
				return PromptAdmittedEvent{}, feedErr
			}
			if len(frames) > 0 {
				if len(frames) != 1 {
					return PromptAdmittedEvent{}, fmt.Errorf("native OpenCode admission stream returned %d frames in one read", len(frames))
				}
				event, observeErr := validator.Observe(frames[0])
				if observeErr != nil {
					return PromptAdmittedEvent{}, observeErr
				}
				if event.MessageID != prompt.ID || event.Text != prompt.Text || event.Delivery != prompt.Delivery {
					return PromptAdmittedEvent{}, fmt.Errorf("native OpenCode prompt event does not match request: %+v", event)
				}
				return event, nil
			}
		}
		if readErr == nil {
			continue
		}
		if ctx.Err() != nil {
			return PromptAdmittedEvent{}, ctx.Err()
		}
		if !errors.Is(readErr, io.EOF) {
			return PromptAdmittedEvent{}, readErr
		}
		frames, closeErr := decoder.Close()
		if closeErr != nil {
			return PromptAdmittedEvent{}, closeErr
		}
		if len(frames) == 1 {
			event, observeErr := validator.Observe(frames[0])
			if observeErr != nil {
				return PromptAdmittedEvent{}, observeErr
			}
			if event.MessageID != prompt.ID || event.Text != prompt.Text || event.Delivery != prompt.Delivery {
				return PromptAdmittedEvent{}, fmt.Errorf("native OpenCode prompt event does not match request: %+v", event)
			}
			return event, nil
		}
		return PromptAdmittedEvent{}, errStreamEndedUnknown
	}
}

// TestNativeRepositoryTask is deliberately opt-in, deferred, and requires an
// operator-supplied numeric-loopback upstream. StartTurn sends explicit
// Resume:true to request work, but native resume capability and later native
// terminal semantics remain unverified. It must not be treated as green
// evidence until real-model lifecycle behavior is independently captured and
// verified. It observes and transparently forwards the real OpenCode Responses
// request to that upstream; it never injects a model response or tool call.
// The served model identity, upstream auth and provider accounting remain
// unattested. This still proves no terminal semantics, usage, handoff,
// cancellation, Control acceptance, or capability promotion.
func TestNativeRepositoryTask(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("native OpenCode repository task supports Linux and Windows only")
	}
	if os.Getenv(nativeRealRepositoryTaskEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_NATIVE_REPOSITORY_TASK=1 to run the real native OpenCode repository task")
	}
	executable := strings.TrimSpace(os.Getenv(nativeRealRepositoryTaskExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute OpenCode %s executable", nativeRealRepositoryTaskExecutableEnv, TestedVersion)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("native OpenCode executable must be an existing regular file")
	}
	upstream := nativeOpenCodeRequiredLoopbackUpstream(t, nativeRealRepositoryTaskBaseURLEnv)

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create native OpenCode workspace: %v", err)
	}
	gitEnvironment := nativeOpenCodeRepositoryTaskGitEnvironment(t, root)
	gitContext, gitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	nativeOpenCodeRepositoryTaskInitializeGit(t, gitContext, gitEnvironment, workspace)
	baselineHead := nativeOpenCodeRepositoryTaskGitOutput(t, gitContext, gitEnvironment, workspace, "rev-parse", "HEAD")
	gitCancel()

	observer := newNativeOpenCodeRepositoryTaskObserver(t, upstream)
	defer observer.Close()
	configJSON := nativeOpenCodeRepositoryTaskRealConfig(observer.baseURL)
	environment := nativeOpenCodeRepositoryTaskEnvironmentWithConfig(t, root, configJSON,
		nativeRealAPIKeyEnv+"="+nativeRealAPIKey,
	)
	nativeOpenCodeRepositoryTaskCheckEnvironment(t, environment, observer.baseURL)
	nativeOpenCodeRepositoryTaskCheckVersion(t, executable, workspace, environment)

	target := filepath.Join(workspace, nativeRepositoryTaskArtifact)
	processRecord := filepath.Join(root, "process-identity.json")
	var persistMutex sync.Mutex
	persistedPID := 0
	persistedIdentity := ""
	sink := &nativeOpenCodeRepositoryTaskSink{}
	startContext, startCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer startCancel()
	session, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace:  workspace,
		Invocation: execution.Invocation{Env: environment},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			persistMutex.Lock()
			persistedPID, persistedIdentity = pid, identity
			persistMutex.Unlock()
			return nativeOpenCodeRepositoryTaskWriteJSON(processRecord, map[string]any{"pid": pid, "identity": identity})
		},
	}, sink)
	if err != nil {
		t.Fatalf("start real native OpenCode repository task: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Errorf("cleanup real native OpenCode repository task: %v", err)
		}
		if _, err := session.Wait(cleanupContext); err != nil {
			t.Errorf("wait for real native OpenCode repository task cleanup: %v", err)
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native OpenCode adapter did not return a staged session")
	}
	persistMutex.Lock()
	gotPersistedPID, gotPersistedIdentity := persistedPID, persistedIdentity
	persistMutex.Unlock()
	pid, identity := staged.ProcessDetails()
	if gotPersistedPID != pid || gotPersistedIdentity != identity || pid <= 0 || identity == "" {
		t.Fatal("real native OpenCode process identity was not persisted before session exposure")
	}
	if persisted, err := os.ReadFile(processRecord); err != nil {
		t.Fatalf("read persisted real native OpenCode process identity: %v", err)
	} else {
		var record struct {
			PID      int    `json:"pid"`
			Identity string `json:"identity"`
		}
		if err := json.Unmarshal(persisted, &record); err != nil || record.PID != pid || record.Identity != identity {
			t.Fatalf("persisted real native OpenCode process identity = %+v, want pid=%d identity=%q", record, pid, identity)
		}
	}

	turnContext, turnCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer turnCancel()
	openContext, openCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer openCancel()
	handle, err := staged.Open(openContext)
	if err != nil {
		t.Fatalf("open real native OpenCode repository session: %v", err)
	}
	if handle.ID == "" {
		t.Fatal("real native OpenCode session returned an empty identity")
	}
	replayedHandle, err := staged.Open(openContext)
	if err != nil || replayedHandle != handle {
		t.Fatalf("replayed real Open() = %+v, %v; want stable native identity", replayedHandle, err)
	}
	if err := staged.StartTurn(turnContext, harness.TurnRequest{
		Goal:    fmt.Sprintf("In the current repository, use the write tool to create %s with exactly these bytes: %q. Do not read files, run commands, or edit anything else.", nativeRepositoryTaskArtifact, nativeRepositoryTaskContent),
		Context: json.RawMessage(fmt.Sprintf(`{"artifact":"%s","content":"%s"}`, nativeRepositoryTaskArtifact, strings.TrimSuffix(nativeRepositoryTaskContent, "\n"))),
	}); err != nil {
		t.Fatalf("start real native OpenCode prompt: %v", err)
	}
	if err := nativeOpenCodeRepositoryTaskWaitForArtifact(turnContext, target, []byte(nativeRepositoryTaskContent)); err != nil {
		t.Fatalf("wait for exact real native OpenCode artifact: %v", err)
	}
	if err := observer.waitForRequest(turnContext); err != nil {
		t.Fatal(err)
	}
	if !sink.has(harness.EventSessionStarted) || !sink.has(harness.EventNativeFrame) {
		t.Fatalf("events = %#v; want session_started and native-frame evidence", sink.snapshot())
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatalf("close real native OpenCode repository task: %v", err)
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatalf("wait real native OpenCode repository task: %v", err)
	}
	closed = true
	if result.Kind != harness.ResultUnknown || result.Semantic != nil || !result.Process.Terminated ||
		result.Process.SinkError != nil || result.Process.OutputError != nil ||
		result.Process.TerminationError != nil || result.Process.ContainmentError != nil || result.Process.WaitError != nil {
		t.Fatalf("real native OpenCode repository task result = %+v; want unknown with clean bounded process stop", result)
	}

	verificationContext, verificationCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer verificationCancel()
	if head := nativeOpenCodeRepositoryTaskGitOutput(t, verificationContext, gitEnvironment, workspace, "rev-parse", "HEAD"); head != baselineHead {
		t.Fatalf("real native OpenCode repository task changed HEAD: got %s, want %s", head, baselineHead)
	}
	status := nativeOpenCodeRepositoryTaskGitOutput(t, verificationContext, gitEnvironment, workspace, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching")
	if status != "?? "+nativeRepositoryTaskArtifact {
		t.Fatalf("real native OpenCode repository task changed unexpected paths: %q", status)
	}
}

func nativeOpenCodeRepositoryTaskWriteJSON(filename string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, append(encoded, '\n'), 0o600)
}

func nativeOpenCodeRepositoryTaskEnvironment(t *testing.T, root, baseURL string) []string {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Path != "/v1" || parsed.User != nil {
		t.Fatalf("invalid native OpenCode gateway URL: %q", baseURL)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || host != "127.0.0.1" || net.ParseIP(host) == nil || port == "" {
		t.Fatalf("native OpenCode gateway must be numeric IPv4 loopback: %q", baseURL)
	}
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("create native OpenCode config directory: %v", err)
	}
	configJSON := nativeOpenCodeRepositoryTaskConfig(baseURL)
	return nativeOpenCodeRepositoryTaskEnvironmentWithConfig(t, root, configJSON)
}

func nativeOpenCodeRepositoryTaskEnvironmentWithConfig(t *testing.T, root, configJSON string, extra ...string) []string {
	t.Helper()
	if strings.Contains(configJSON, "${") || strings.Contains(configJSON, "OPENAI_API_KEY") || strings.Contains(configJSON, "ANTHROPIC_API_KEY") {
		t.Fatal("native OpenCode test config contains credential expansion")
	}
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("create native OpenCode config directory: %v", err)
	}
	managedConfigDir := filepath.Join(root, "test-managed-config")
	if err := os.MkdirAll(managedConfigDir, 0o700); err != nil {
		t.Fatalf("create test-managed OpenCode config directory: %v", err)
	}
	environment := nativeOpenCodeSmokeEnvironment(t, root)
	environment = append(environment,
		"OPENCODE_CONFIG_DIR="+configDir,
		"OPENCODE_CONFIG_CONTENT="+configJSON,
		"OPENCODE_TEST_MANAGED_CONFIG_DIR="+managedConfigDir,
		"OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"OPENCODE_DISABLE_MODELS_FETCH=1",
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS=1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS=1",
		"OPENCODE_DISABLE_CLAUDE_CODE_SKILLS=1",
		"OPENCODE_PURE=1",
	)
	environment = append(environment, extra...)
	return environment
}

func nativeOpenCodeRepositoryTaskConfig(baseURL string) string {
	targetPattern := nativeRepositoryTaskArtifact
	config := map[string]any{
		"$schema":     "https://opencode.ai/config.json",
		"model":       nativeSyntheticProvider + "/" + nativeSyntheticModel,
		"small_model": nativeSyntheticProvider + "/" + nativeSyntheticModel,
		"enabled_providers": []string{
			nativeSyntheticProvider,
		},
		"provider": map[string]any{
			nativeSyntheticProvider: map[string]any{
				"name": nativeSyntheticProvider,
				"npm":  "@ai-sdk/openai-compatible",
				"options": map[string]any{
					"baseURL": baseURL,
					"apiKey":  nativeSyntheticAPIKey,
				},
				"models": map[string]any{
					nativeSyntheticModel: map[string]any{
						"name": nativeSyntheticModel,
						"provider": map[string]any{
							"npm": "@ai-sdk/openai-compatible",
							"api": baseURL,
						},
						"limit": map[string]any{
							"context": 32768,
							"output":  4096,
						},
						"modalities": map[string]any{
							"input":  []string{"text"},
							"output": []string{"text"},
						},
						"tool_call":   true,
						"reasoning":   false,
						"temperature": false,
					},
				},
			},
		},
		"permission": map[string]any{
			"read":               "deny",
			"list":               "deny",
			"glob":               "deny",
			"grep":               "deny",
			"bash":               "deny",
			"task":               "deny",
			"skill":              "deny",
			"external_directory": "deny",
			"edit": map[string]string{
				"*":           "deny",
				targetPattern: "allow",
			},
		},
	}
	encoded, _ := json.Marshal(config)
	return string(encoded)
}

func nativeOpenCodeRepositoryTaskRealConfig(baseURL string) string {
	config := map[string]any{
		"$schema":           "https://opencode.ai/config.json",
		"model":             nativeRealProvider + "/" + nativeRealModel,
		"small_model":       nativeRealProvider + "/" + nativeRealModel,
		"enabled_providers": []string{nativeRealProvider},
		"provider": map[string]any{
			nativeRealProvider: map[string]any{
				"name":      nativeRealProvider,
				"npm":       "@ai-sdk/openai",
				"env":       []string{nativeRealAPIKeyEnv},
				"whitelist": []string{nativeRealModel},
				"options": map[string]any{
					"apiKey":          nativeRealAPIKey,
					"baseURL":         baseURL,
					"reasoningEffort": "high",
				},
				"models": map[string]any{
					nativeRealModel: map[string]any{
						"name":              nativeRealModel,
						"attachment":        false,
						"reasoning":         true,
						"tool_call":         true,
						"structured_output": true,
						"temperature":       true,
						"modalities": map[string]any{
							"input":  []string{"text"},
							"output": []string{"text"},
						},
						"limit": map[string]any{
							"context": 114688,
							"output":  16384,
						},
						"provider": map[string]any{
							"npm": "@ai-sdk/openai",
							"api": baseURL,
						},
					},
				},
			},
		},
		"permission": map[string]any{
			"read":               "deny",
			"list":               "deny",
			"glob":               "deny",
			"grep":               "deny",
			"bash":               "deny",
			"task":               "deny",
			"skill":              "deny",
			"external_directory": "deny",
			"edit": map[string]string{
				"*":                          "deny",
				nativeRepositoryTaskArtifact: "allow",
			},
		},
	}
	encoded, _ := json.Marshal(config)
	return string(encoded)
}

func nativeOpenCodeRequiredLoopbackUpstream(t *testing.T, environmentName string) *url.URL {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(environmentName))
	if raw == "" {
		t.Fatalf("%s must provide the fixed upstream base URL", environmentName)
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "/v1" {
		t.Fatalf("%s must be an http(s) numeric loopback URL ending in /v1", environmentName)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || host != "127.0.0.1" || net.ParseIP(host) == nil || port == "" {
		t.Fatalf("%s must use numeric 127.0.0.1:<port>/v1: %q", environmentName, raw)
	}
	return parsed
}

func nativeOpenCodeRepositoryTaskCheckEnvironment(t *testing.T, environment []string, baseURL string) {
	t.Helper()
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		switch strings.ToUpper(key) {
		case "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY":
			t.Fatalf("native OpenCode test environment contains credential variable %s", key)
		}
	}
	if !strings.Contains(strings.Join(environment, "\n"), "OPENCODE_CONFIG_CONTENT=") {
		t.Fatal("native OpenCode test environment is missing OPENCODE_CONFIG_CONTENT")
	}
	if !strings.Contains(strings.Join(environment, "\n"), baseURL) {
		t.Fatal("native OpenCode test environment is missing the configured loopback base URL")
	}
}

func nativeOpenCodeRepositoryTaskCheckVersion(t *testing.T, executable, workspace string, environment []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--version")
	command.Dir = workspace
	command.Env = environment
	output, err := command.Output()
	if err != nil {
		t.Fatalf("run native OpenCode version check: %v", err)
	}
	if strings.TrimSpace(string(output)) != TestedVersion {
		t.Fatalf("native OpenCode version = %q, want %s", strings.TrimSpace(string(output)), TestedVersion)
	}
}

func nativeOpenCodeRepositoryTaskWaitForArtifact(ctx context.Context, path string, expected []byte) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if content, err := os.ReadFile(path); err == nil && string(content) == string(expected) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func nativeOpenCodeRepositoryTaskGitEnvironment(t *testing.T, root string) []string {
	t.Helper()
	environment := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + root,
		"USERPROFILE=" + root,
		"TEMP=" + filepath.Join(root, "tmp"),
		"TMP=" + filepath.Join(root, "tmp"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(root, "missing-global-gitconfig"),
	}
	if runtime.GOOS == "windows" {
		for _, name := range []string{"SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
			if value := os.Getenv(name); value != "" {
				environment = append(environment, name+"="+value)
			}
		}
	}
	for _, directory := range []string{filepath.Join(root, "tmp")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("create native OpenCode git environment directory: %v", err)
		}
	}
	return environment
}

func nativeOpenCodeRepositoryTaskInitializeGit(t *testing.T, ctx context.Context, environment []string, workspace string) {
	t.Helper()
	nativeOpenCodeRepositoryTaskGit(t, ctx, environment, workspace, "init")
	if err := os.WriteFile(filepath.Join(workspace, "baseline.txt"), []byte("baseline\n"), 0o600); err != nil {
		t.Fatalf("write native OpenCode baseline: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".opencode"), 0o700); err != nil {
		t.Fatalf("create native OpenCode project directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".opencode", ".gitignore"), []byte("*\n!.gitignore\n"), 0o600); err != nil {
		t.Fatalf("write native OpenCode project gitignore: %v", err)
	}
	nativeOpenCodeRepositoryTaskGit(t, ctx, environment, workspace, "add", "baseline.txt", ".opencode/.gitignore")
	nativeOpenCodeRepositoryTaskGit(t, ctx, environment, workspace, "-c", "user.name=Symmetry Native Test", "-c", "user.email=symmetry-native-test@example.invalid", "commit", "-m", "baseline")
}

func nativeOpenCodeRepositoryTaskGit(t *testing.T, ctx context.Context, environment []string, workspace string, args ...string) {
	t.Helper()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = workspace
	command.Env = environment
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

func nativeOpenCodeRepositoryTaskGitOutput(t *testing.T, ctx context.Context, environment []string, workspace string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = workspace
	command.Env = environment
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

type nativeOpenCodeRepositoryTaskGateway struct {
	server   *http.Server
	listener net.Listener
	baseURL  string
	target   string

	mu        sync.Mutex
	requests  int
	toolSeen  bool
	finalSeen bool
	err       error
}

type nativeOpenCodeRepositoryTaskObserver struct {
	server   *http.Server
	listener net.Listener
	upstream *url.URL
	baseURL  string

	mu           sync.Mutex
	sawModel     bool
	sawReasoning bool
	lastStatus   int
	err          error
	requestDone  chan struct{}
	doneOnce     sync.Once
	serveDone    chan struct{}
	closeOnce    sync.Once
}

type nativeOpenCodeRepositoryTaskSink struct {
	mu     sync.Mutex
	events []harness.Event
}

func (sink *nativeOpenCodeRepositoryTaskSink) Handle(_ context.Context, event harness.Event) error {
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	sink.mu.Unlock()
	return nil
}

func (sink *nativeOpenCodeRepositoryTaskSink) has(kind harness.EventKind) bool {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, event := range sink.events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func (sink *nativeOpenCodeRepositoryTaskSink) snapshot() []harness.Event {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]harness.Event(nil), sink.events...)
}

func newNativeOpenCodeRepositoryTaskGateway(t *testing.T, target string) *nativeOpenCodeRepositoryTaskGateway {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen native OpenCode loopback gateway: %v", err)
	}
	gateway := &nativeOpenCodeRepositoryTaskGateway{
		listener: listener,
		baseURL:  "http://" + listener.Addr().String() + "/v1",
		target:   target,
	}
	gateway.server = &http.Server{Handler: http.HandlerFunc(gateway.handle)}
	go func() {
		if err := gateway.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			gateway.mu.Lock()
			gateway.err = err
			gateway.mu.Unlock()
		}
	}()
	return gateway
}

func (gateway *nativeOpenCodeRepositoryTaskGateway) handle(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/chat/completions") {
		gateway.recordError(fmt.Errorf("unexpected native OpenCode provider route: %s %s", request.Method, request.URL.Path))
		http.Error(response, "unexpected route", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		gateway.recordError(fmt.Errorf("read native OpenCode provider request: %w", err))
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	var envelope struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		gateway.recordError(fmt.Errorf("decode native OpenCode provider request: %w", err))
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	host, _, _ := net.SplitHostPort(request.RemoteAddr)
	if host != "127.0.0.1" || request.Header.Get("Authorization") != "Bearer "+nativeSyntheticAPIKey || envelope.Model != nativeSyntheticModel {
		gateway.recordError(errors.New("native OpenCode provider request escaped the fixed loopback/model/auth boundary"))
		http.Error(response, "invalid request", http.StatusForbidden)
		return
	}
	toolAvailable := false
	for _, tool := range envelope.Tools {
		if tool.Function.Name == "write" {
			toolAvailable = true
			break
		}
	}
	if !toolAvailable {
		gateway.recordError(errors.New("native OpenCode provider request did not expose the write tool"))
		http.Error(response, "missing write tool", http.StatusBadRequest)
		return
	}
	hasToolResult := false
	for _, message := range envelope.Messages {
		if message.Role == "tool" {
			hasToolResult = true
			break
		}
	}
	gateway.mu.Lock()
	gateway.requests++
	if hasToolResult {
		gateway.finalSeen = true
	} else {
		gateway.toolSeen = true
	}
	gateway.mu.Unlock()
	arguments, _ := json.Marshal(map[string]string{"content": nativeRepositoryTaskContent, "filePath": gateway.target})
	if hasToolResult {
		nativeOpenCodeRepositoryTaskWriteChatResponse(response, envelope.Stream, nativeSyntheticModel, "done", "stop", nil)
		return
	}
	nativeOpenCodeRepositoryTaskWriteChatResponse(response, envelope.Stream, nativeSyntheticModel, "", "tool_calls", arguments)
}

func (gateway *nativeOpenCodeRepositoryTaskGateway) recordError(err error) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.err == nil {
		gateway.err = err
	}
}

func (gateway *nativeOpenCodeRepositoryTaskGateway) validate() error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.err != nil {
		return gateway.err
	}
	if gateway.requests < 2 || !gateway.toolSeen || !gateway.finalSeen {
		return fmt.Errorf("native OpenCode provider observed requests=%d tool_seen=%t final_seen=%t; want tool and follow-up requests", gateway.requests, gateway.toolSeen, gateway.finalSeen)
	}
	return nil
}

func (gateway *nativeOpenCodeRepositoryTaskGateway) waitForCompletion(ctx context.Context) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		gateway.mu.Lock()
		err := gateway.err
		complete := gateway.requests >= 2 && gateway.toolSeen && gateway.finalSeen
		gateway.mu.Unlock()
		if err != nil {
			return err
		}
		if complete {
			return gateway.validate()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for native OpenCode provider follow-up: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (gateway *nativeOpenCodeRepositoryTaskGateway) Close() {
	if gateway == nil || gateway.server == nil {
		return
	}
	contextValue, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = gateway.server.Shutdown(contextValue)
}

func newNativeOpenCodeRepositoryTaskObserver(t *testing.T, upstream *url.URL) *nativeOpenCodeRepositoryTaskObserver {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen real native OpenCode observer: %v", err)
	}
	observer := &nativeOpenCodeRepositoryTaskObserver{
		listener:    listener,
		upstream:    upstream,
		baseURL:     "http://" + listener.Addr().String() + "/v1",
		requestDone: make(chan struct{}),
		serveDone:   make(chan struct{}),
	}
	observer.server = &http.Server{Handler: http.HandlerFunc(observer.handle)}
	go func() {
		defer close(observer.serveDone)
		if err := observer.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			observer.recordError(err)
		}
	}()
	return observer
}

func (observer *nativeOpenCodeRepositoryTaskObserver) handle(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" || request.URL.RawQuery != "" {
		observer.recordError(fmt.Errorf("real OpenCode observer rejected non-Responses route: %s %s", request.Method, request.URL.RequestURI()))
		http.Error(response, "only /v1/responses is permitted", http.StatusNotFound)
		return
	}
	if request.ContentLength > nativeOpenCodeObserverMaxRequestBytes {
		observer.recordError(fmt.Errorf("real OpenCode observer request exceeds %d-byte limit", nativeOpenCodeObserverMaxRequestBytes))
		http.Error(response, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, nativeOpenCodeObserverMaxRequestBytes+1))
	if err != nil {
		observer.recordError(fmt.Errorf("read real OpenCode observer request: %w", err))
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > nativeOpenCodeObserverMaxRequestBytes {
		observer.recordError(fmt.Errorf("real OpenCode observer request exceeds %d-byte limit", nativeOpenCodeObserverMaxRequestBytes))
		http.Error(response, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var envelope struct {
		Model     string          `json:"model"`
		Reasoning json.RawMessage `json:"reasoning"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		observer.recordError(fmt.Errorf("decode real OpenCode Responses request: %w", err))
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	if envelope.Model != nativeRealModel {
		observer.recordError(fmt.Errorf("real OpenCode Responses model = %q, want %q", envelope.Model, nativeRealModel))
		http.Error(response, "unexpected model", http.StatusBadRequest)
		return
	}
	if len(envelope.Reasoning) == 0 || string(envelope.Reasoning) == "null" {
		observer.recordError(errors.New("real OpenCode Responses request omitted reasoning effort"))
		http.Error(response, "missing reasoning effort", http.StatusBadRequest)
		return
	}
	var reasoning struct {
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal(envelope.Reasoning, &reasoning); err != nil || reasoning.Effort != "high" {
		observer.recordError(errors.New("real OpenCode Responses reasoning effort was not high"))
		http.Error(response, "unexpected reasoning effort", http.StatusBadRequest)
		return
	}
	observer.mu.Lock()
	observer.sawReasoning = true
	observer.mu.Unlock()

	forwardURL := *observer.upstream
	forwardURL.Path = request.URL.Path
	forwardURL.RawPath = ""
	forwardURL.RawQuery = ""
	forward, err := http.NewRequestWithContext(request.Context(), request.Method, forwardURL.String(), bytes.NewReader(body))
	if err != nil {
		observer.recordError(fmt.Errorf("create forwarded Responses request: %w", err))
		http.Error(response, "forwarding failed", http.StatusBadGateway)
		return
	}
	// This observer is a one-shot transparent forwarder: do not let the
	// transport replay a buffered POST after a connection failure.
	forward.GetBody = nil
	forward.Header = nativeOpenCodeEndToEndHeaders(request.Header)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	transport.DisableKeepAlives = true
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	upstreamResponse, err := client.Do(forward)
	if err != nil {
		observer.recordError(fmt.Errorf("forward Responses request: %w", err))
		http.Error(response, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer upstreamResponse.Body.Close()
	if upstreamResponse.StatusCode >= http.StatusMultipleChoices && upstreamResponse.StatusCode < http.StatusBadRequest {
		observer.recordError(fmt.Errorf("real OpenCode upstream returned redirect status %d", upstreamResponse.StatusCode))
		http.Error(response, "upstream redirect rejected", http.StatusBadGateway)
		return
	}
	observer.mu.Lock()
	observer.lastStatus = upstreamResponse.StatusCode
	observer.sawModel = true
	observer.mu.Unlock()
	observer.signal()
	if upstreamResponse.StatusCode < 200 || upstreamResponse.StatusCode >= 300 {
		observer.recordError(fmt.Errorf("real OpenCode upstream status = %d", upstreamResponse.StatusCode))
	}
	for key, values := range nativeOpenCodeEndToEndHeaders(upstreamResponse.Header) {
		response.Header()[key] = append([]string(nil), values...)
	}
	response.WriteHeader(upstreamResponse.StatusCode)
	flusher, _ := response.(http.Flusher)
	buffer := make([]byte, 32*1024)
	for {
		count, readErr := upstreamResponse.Body.Read(buffer)
		if count > 0 {
			if _, writeErr := response.Write(buffer[:count]); writeErr != nil {
				observer.recordError(fmt.Errorf("write forwarded Responses response: %w", writeErr))
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				observer.recordError(fmt.Errorf("read forwarded Responses response: %w", readErr))
			}
			return
		}
	}
}

func (observer *nativeOpenCodeRepositoryTaskObserver) recordError(err error) {
	observer.mu.Lock()
	if observer.err == nil && err != nil {
		observer.err = err
	}
	observer.mu.Unlock()
	observer.signal()
}

func (observer *nativeOpenCodeRepositoryTaskObserver) waitForRequest(ctx context.Context) error {
	for {
		observer.mu.Lock()
		err := observer.err
		sawModel := observer.sawModel
		sawReasoning := observer.sawReasoning
		status := observer.lastStatus
		done := observer.requestDone
		observer.mu.Unlock()
		if err != nil {
			return err
		}
		if sawModel && sawReasoning && status >= 200 && status < 300 {
			return nil
		}
		if done == nil {
			return errors.New("real OpenCode observer completion signal is unavailable")
		}
		select {
		case <-done:
			continue
		case <-ctx.Done():
			return fmt.Errorf("wait for real OpenCode Responses request: %w", ctx.Err())
		}
	}
}

func (observer *nativeOpenCodeRepositoryTaskObserver) signal() {
	if observer == nil || observer.requestDone == nil {
		return
	}
	observer.doneOnce.Do(func() { close(observer.requestDone) })
}

func nativeOpenCodeEndToEndHeaders(source http.Header) http.Header {
	connectionTokens := make(map[string]struct{})
	for _, value := range source.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if canonical := http.CanonicalHeaderKey(strings.TrimSpace(token)); canonical != "" {
				connectionTokens[canonical] = struct{}{}
			}
		}
	}
	endToEnd := make(http.Header)
	for key, values := range source {
		canonical := http.CanonicalHeaderKey(key)
		if nativeOpenCodeIsHopByHopHeader(canonical) {
			continue
		}
		if _, nominated := connectionTokens[canonical]; nominated {
			continue
		}
		endToEnd[canonical] = append([]string(nil), values...)
	}
	return endToEnd
}

func nativeOpenCodeIsHopByHopHeader(header string) bool {
	switch http.CanonicalHeaderKey(header) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection", "TE", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func (observer *nativeOpenCodeRepositoryTaskObserver) Close() {
	if observer == nil || observer.server == nil {
		return
	}
	observer.closeOnce.Do(func() {
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		shutdownErr := observer.server.Shutdown(shutdownContext)
		shutdownCancel()
		if shutdownErr != nil {
			_ = observer.server.Close()
		}
		if observer.serveDone == nil {
			return
		}
		joinContext, joinCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer joinCancel()
		select {
		case <-observer.serveDone:
		case <-joinContext.Done():
			_ = observer.server.Close()
		}
	})
}

func nativeOpenCodeRepositoryTaskWriteChatResponse(response http.ResponseWriter, stream bool, model, content, finishReason string, toolArguments []byte) {
	if !stream {
		message := map[string]any{"role": "assistant"}
		if toolArguments != nil {
			message["tool_calls"] = []any{map[string]any{"id": "call_write", "type": "function", "function": map[string]any{"name": "write", "arguments": string(toolArguments)}}}
		} else {
			message["content"] = content
		}
		writeJSON := map[string]any{"id": "chatcmpl-symmetry", "object": "chat.completion", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishReason}}, "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(writeJSON)
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	flusher, _ := response.(http.Flusher)
	delta := map[string]any{"role": "assistant"}
	if toolArguments != nil {
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "call_write", "type": "function", "function": map[string]any{"name": "write", "arguments": string(toolArguments)}}}
	} else {
		delta["content"] = content
	}
	chunk := map[string]any{"id": "chatcmpl-symmetry", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}}
	encoded, _ := json.Marshal(chunk)
	_, _ = fmt.Fprintf(response, "data: %s\n\n", encoded)
	finish := map[string]any{"id": "chatcmpl-symmetry", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}}}
	encoded, _ = json.Marshal(finish)
	_, _ = fmt.Fprintf(response, "data: %s\n\ndata: [DONE]\n\n", encoded)
	if flusher != nil {
		flusher.Flush()
	}
}
