//go:build linux || windows

package opencode

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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	nativeOpenCodeTerminalCandidateEnabledEnv        = "SYMMETRY_OPENCODE_NATIVE_TERMINAL_CANDIDATE"
	nativeOpenCodeTerminalCandidateExecutableEnv     = "SYMMETRY_OPENCODE_NATIVE_TERMINAL_EXECUTABLE"
	nativeOpenCodeTerminalCandidateSHA256Env         = "SYMMETRY_OPENCODE_NATIVE_TERMINAL_EXECUTABLE_SHA256"
	nativeOpenCodeTerminalCandidateDefaultExecutable = `C:\Users\lhan\AppData\Local\mise\installs\opencode\1.18.30\opencode.exe`
	nativeOpenCodeTerminalCandidateSHA256            = "c1bdbb18767048e1853af4238311b3f7e16ff2f91b68fb4c7ced3c5175347eea"
	nativeOpenCodeTerminalCandidateProvider          = "symmetry-opencode-terminal"
	nativeOpenCodeTerminalCandidateModel             = "symmetry-opencode-terminal"
	nativeOpenCodeTerminalCandidateAPIKey            = "symmetry-opencode-terminal-test-key"
	nativeOpenCodeTerminalCandidateArtifact          = "opencode-terminal-candidate.txt"
	nativeOpenCodeTerminalCandidateContent           = "native opencode terminal candidate\n"
	nativeOpenCodeTerminalCandidateTimeout           = 45 * time.Second
	nativeOpenCodeTerminalCandidateCloseTimeout      = 15 * time.Second
	nativeOpenCodeTerminalCandidateMaxBodyBytes      = 1 << 20
	nativeOpenCodeTerminalCandidateMaxCaptureBytes   = 8 << 20
	nativeOpenCodeTerminalCandidateMaxGlobalFrames   = 256
	nativeOpenCodeTerminalCandidateMaxSessionFrames  = 512
)

type nativeOpenCodeTerminalCandidateScenario string

const (
	nativeOpenCodeTerminalStop      nativeOpenCodeTerminalCandidateScenario = "terminal_stop"
	nativeOpenCodeProviderError     nativeOpenCodeTerminalCandidateScenario = "provider_error"
	nativeOpenCodeInterrupt         nativeOpenCodeTerminalCandidateScenario = "interrupt"
	nativeOpenCodeStepContinuation  nativeOpenCodeTerminalCandidateScenario = "step_ended_continuation"
	nativeOpenCodeStepFailure       nativeOpenCodeTerminalCandidateScenario = "step_ended_failure"
	nativeOpenCodeTruncatedProvider nativeOpenCodeTerminalCandidateScenario = "truncated_provider_eof"
)

type nativeOpenCodeTerminalCandidateScenarioSpec struct {
	name                     nativeOpenCodeTerminalCandidateScenario
	expectedProviderRequests int
	needsFollowUp            bool
	requireIdle              bool
	requireError             bool
	requireStepEnd           bool
	expectCancelled          bool
}

var nativeOpenCodeTerminalCandidateScenarios = []nativeOpenCodeTerminalCandidateScenarioSpec{
	{name: nativeOpenCodeTerminalStop, expectedProviderRequests: 1, requireIdle: true, requireStepEnd: true},
	{name: nativeOpenCodeProviderError, expectedProviderRequests: 3, requireError: true},
	{name: nativeOpenCodeInterrupt, expectedProviderRequests: 2, needsFollowUp: true, expectCancelled: true},
	{name: nativeOpenCodeStepContinuation, expectedProviderRequests: 2, needsFollowUp: true, requireStepEnd: true},
	{name: nativeOpenCodeStepFailure, expectedProviderRequests: 4, needsFollowUp: true, requireStepEnd: true},
	{name: nativeOpenCodeTruncatedProvider, expectedProviderRequests: 1, requireStepEnd: false},
}

// TestNativeOpenCodeTerminalCandidateWitness is an opt-in, Windows-only
// candidate witness for the exact pinned OpenCode binary. It deliberately
// does not change the production terminal mapping: Step.Ended, idle, session
// error, process exit, and SSE EOF are observed as evidence, never promoted to
// a Symmetry success receipt.
func TestNativeOpenCodeTerminalCandidateWitness(t *testing.T) {
	if os.Getenv(nativeOpenCodeTerminalCandidateEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_NATIVE_TERMINAL_CANDIDATE=1 to run the pinned OpenCode terminal candidate witness")
	}
	if runtime.GOOS != "windows" {
		t.Fatalf("native OpenCode terminal candidate witness requires the exact Windows binary; explicit opt-in cannot be skipped on %s", runtime.GOOS)
	}
	executable := nativeOpenCodeTerminalCandidateExecutable(t)
	for _, scenario := range nativeOpenCodeTerminalCandidateScenarios {
		scenario := scenario
		t.Run(string(scenario.name), func(t *testing.T) {
			nativeOpenCodeRunTerminalCandidateScenario(t, executable, scenario)
		})
	}
}

func nativeOpenCodeTerminalCandidateExecutable(t *testing.T) string {
	t.Helper()
	executable := strings.TrimSpace(os.Getenv(nativeOpenCodeTerminalCandidateExecutableEnv))
	if executable == "" {
		executable = nativeOpenCodeTerminalCandidateDefaultExecutable
	}
	configuredSHA := strings.TrimSpace(os.Getenv(nativeOpenCodeTerminalCandidateSHA256Env))
	if err := nativeOpenCodeTerminalCandidateValidateExecutable(
		executable,
		nativeOpenCodeTerminalCandidateDefaultExecutable,
		configuredSHA,
		nativeOpenCodeTerminalCandidateSHA256,
	); err != nil {
		t.Fatalf("validate exact pinned OpenCode executable: %v", err)
	}
	return filepath.Clean(executable)
}

func nativeOpenCodeTerminalCandidateValidateExecutable(executable, expectedExecutable, configuredSHA, expectedSHA string) error {
	executable = filepath.Clean(strings.TrimSpace(executable))
	expectedExecutable = filepath.Clean(strings.TrimSpace(expectedExecutable))
	if executable == "." || expectedExecutable == "." {
		return errors.New("pinned OpenCode executable path must be non-empty")
	}
	if !strings.EqualFold(executable, expectedExecutable) {
		return fmt.Errorf("%s must name the exact pinned executable %q, got %q", nativeOpenCodeTerminalCandidateExecutableEnv, expectedExecutable, executable)
	}
	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("exact pinned OpenCode executable is unavailable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("pinned OpenCode executable is not a regular file: %q", executable)
	}
	if len(expectedSHA) != sha256.Size*2 {
		return fmt.Errorf("%s must contain the expected 64-character SHA-256", nativeOpenCodeTerminalCandidateSHA256Env)
	}
	if _, err := hex.DecodeString(expectedSHA); err != nil {
		return fmt.Errorf("%s is not hexadecimal: %w", nativeOpenCodeTerminalCandidateSHA256Env, err)
	}
	if configuredSHA != "" && !strings.EqualFold(strings.TrimPrefix(configuredSHA, "sha256:"), expectedSHA) {
		return fmt.Errorf("%s must equal the pinned SHA-256 %s", nativeOpenCodeTerminalCandidateSHA256Env, expectedSHA)
	}
	if err := nativeOpenCodeTerminalCandidateVerifyExecutableSHA256(executable, "sha256:"+expectedSHA); err != nil {
		return fmt.Errorf("verify exact pinned OpenCode executable: %w", err)
	}
	return nil
}

// TestNativeOpenCodeTerminalCandidateOptInPreflightFailsClosed proves that an
// explicitly enabled candidate cannot become green without executing the exact
// pinned binary. This is deliberately a pure preflight check: no provider
// listener is created and no child process is started on any failing branch.
func TestNativeOpenCodeTerminalCandidateOptInPreflightFailsClosed(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "candidate.exe")
	if err := os.WriteFile(fixture, []byte("not the pinned binary\n"), 0o700); err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.exe")
	if err := nativeOpenCodeTerminalCandidateValidateExecutable(missing, missing, "", nativeOpenCodeTerminalCandidateSHA256); err == nil {
		t.Fatal("explicit opt-in preflight accepted a missing executable")
	}
	if err := nativeOpenCodeTerminalCandidateValidateExecutable(fixture, fixture, "sha256:"+strings.Repeat("0", 64), nativeOpenCodeTerminalCandidateSHA256); err == nil {
		t.Fatal("explicit opt-in preflight accepted a wrong executable SHA-256")
	}
}

func nativeOpenCodeRunTerminalCandidateScenario(t *testing.T, executable string, scenario nativeOpenCodeTerminalCandidateScenarioSpec) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create isolated OpenCode workspace: %v", err)
	}
	timeline := newNativeOpenCodeTerminalCandidateTimeline()
	gateway := newNativeOpenCodeTerminalCandidateGateway(t, scenario.name, nativeOpenCodeTerminalCandidateModel, timeline)
	defer gateway.Close()
	config := nativeOpenCodeTerminalCandidateConfig(gateway.baseURL, nativeOpenCodeTerminalCandidateModel)
	environment := nativeOpenCodeRepositoryTaskEnvironmentWithConfig(t, root, config)
	nativeOpenCodeTerminalCandidateAssertEnvironment(t, environment, root, gateway.baseURL)
	nativeOpenCodeTerminalCandidateCheckVersion(t, executable, workspace, environment)

	sink := &nativeOpenCodeTerminalCandidateSink{timeline: timeline}
	adapter := NewAdapter(executable)
	productionNewAPI := adapter.newAPI
	apiReady := make(chan *nativeOpenCodeTerminalCandidateAPI, 1)
	adapter.newAPI = func(config Config) (api, error) {
		clientAPI, err := productionNewAPI(config)
		if err != nil {
			return nil, err
		}
		client, ok := clientAPI.(*Client)
		if !ok || client == nil {
			return nil, fmt.Errorf("production OpenCode API factory returned %T, want *Client", clientAPI)
		}
		witnessAPI := &nativeOpenCodeTerminalCandidateAPI{
			Client:         client,
			sessionCapture: newNativeOpenCodeTerminalCandidateCollector(nativeOpenCodeTerminalCandidateMaxCaptureBytes),
			promptObserved: make(chan struct{}),
			timeline:       timeline,
		}
		select {
		case apiReady <- witnessAPI:
		default:
			return nil, errors.New("OpenCode terminal candidate API factory was called more than once")
		}
		return witnessAPI, nil
	}

	startContext, startCancel := context.WithTimeout(context.Background(), nativeOpenCodeTerminalCandidateTimeout)
	defer startCancel()
	session, err := adapter.Start(startContext, harness.StartRequest{
		Workspace:  workspace,
		Invocation: execution.Invocation{Env: environment},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			return nil
		},
	}, sink)
	if err != nil {
		if cleanupErr := nativeOpenCodeTerminalCandidateCloseFailedStart(session, nativeOpenCodeTerminalCandidateCloseTimeout); cleanupErr != nil {
			t.Errorf("cleanup failed OpenCode terminal candidate start: %v", cleanupErr)
		}
		t.Fatalf("start OpenCode terminal candidate: %v", err)
	}
	if session == nil {
		t.Fatal("OpenCode terminal candidate returned a nil session")
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		if err := nativeOpenCodeTerminalCandidateCloseAndWait(session, nativeOpenCodeTerminalCandidateCloseTimeout); err != nil {
			t.Errorf("cleanup OpenCode terminal candidate session: %v", err)
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("OpenCode terminal candidate did not return a staged session")
	}
	openContext, openCancel := context.WithTimeout(context.Background(), nativeOpenCodeTerminalCandidateTimeout)
	handle, err := staged.Open(openContext)
	openCancel()
	if err != nil {
		t.Fatalf("open OpenCode terminal candidate session: %v", err)
	}
	if !strings.HasPrefix(handle.ID, "ses_") {
		t.Fatalf("OpenCode terminal candidate session identity = %q, want ses_ identity", handle.ID)
	}
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "session.opened", SessionID: handle.ID})
	gateway.BindSession(handle.ID)
	witnessAPI := nativeOpenCodeTerminalCandidateWaitAPI(t, apiReady)

	globalContext, globalCancel := context.WithCancel(context.Background())
	globalBody, err := witnessAPI.Client.OpenGlobalEvents(globalContext)
	if err != nil {
		globalCancel()
		t.Fatalf("open concrete OpenCode global event stream: %v", err)
	}
	globalCapture := newNativeOpenCodeTerminalCandidateGlobalCapture(globalBody, globalContext, timeline)
	globalCapture.Start()
	globalStopped := false
	t.Cleanup(func() {
		if globalStopped {
			return
		}
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.stop.called", SessionID: handle.ID})
		globalCapture.RequestStop()
		globalCancel()
		_ = globalBody.Close()
		globalCapture.Wait(t)
	})

	turnContext, turnCancel := context.WithTimeout(context.Background(), nativeOpenCodeTerminalCandidateTimeout)
	defer turnCancel()
	turnDone := make(chan error, 1)
	turnRequest := harness.TurnRequest{
		Goal:    fmt.Sprintf("For this bounded OpenCode terminal witness, use the write tool exactly once to create %s with exactly these bytes: %q. Do not read files, run commands, or edit anything else.", nativeOpenCodeTerminalCandidateArtifact, nativeOpenCodeTerminalCandidateContent),
		Context: json.RawMessage(fmt.Sprintf(`{"artifact":%q,"content":%q}`, nativeOpenCodeTerminalCandidateArtifact, strings.TrimSuffix(nativeOpenCodeTerminalCandidateContent, "\n"))),
	}
	if scenario.name == nativeOpenCodeInterrupt {
		go func() {
			timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "start_turn.called"})
			err := staged.StartTurn(turnContext, turnRequest)
			timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "start_turn.returned", Detail: nativeOpenCodeTerminalCandidateErrorString(err)})
			turnDone <- err
		}()
		nativeOpenCodeTerminalCandidateWait(t, turnContext, gateway.firstRequestSeen, "first provider request")
		nativeOpenCodeTerminalCandidateWait(t, turnContext, gateway.followUpActive, "interrupt follow-up provider request")
		controlContext, controlCancel := context.WithTimeout(context.Background(), nativeOpenCodeTerminalCandidateTimeout)
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "control.cancel.called", SessionID: handle.ID})
		receipt, controlErr := staged.Control(controlContext, harness.ControlRequest{CommandID: "opencode-terminal-candidate-cancel", Kind: harness.ControlCancel})
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "control.cancel.returned", SessionID: handle.ID, Detail: nativeOpenCodeTerminalCandidateErrorString(controlErr)})
		controlCancel()
		if controlErr != nil {
			t.Fatalf("OpenCode terminal candidate interrupt: %v", controlErr)
		}
		if receipt.Outcome != harness.ControlApplied || receipt.Capability != harness.CapabilityCancel {
			t.Fatalf("OpenCode terminal candidate interrupt receipt = %+v, want applied cancel", receipt)
		}
		nativeOpenCodeTerminalCandidateWait(t, turnContext, gateway.followUpCancelled, "provider cancellation barrier")
	} else {
		go func() {
			timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "start_turn.called"})
			err := staged.StartTurn(turnContext, turnRequest)
			timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "start_turn.returned", Detail: nativeOpenCodeTerminalCandidateErrorString(err)})
			turnDone <- err
		}()
		nativeOpenCodeTerminalCandidateWait(t, turnContext, gateway.firstRequestSeen, "first provider request")
	}

	if scenario.needsFollowUp {
		nativeOpenCodeTerminalCandidateWait(t, turnContext, gateway.followUpSeen, "follow-up provider request")
	}
	if err := gateway.WaitForExpectedCompletion(turnContext, scenario.expectedProviderRequests); err != nil {
		t.Fatalf("wait OpenCode terminal candidate provider exchange: %v", err)
	}
	if scenario.name != nativeOpenCodeTruncatedProvider {
		frameContext, frameCancel := context.WithTimeout(turnContext, nativeOpenCodeTerminalCandidateTimeout)
		if err := nativeOpenCodeTerminalCandidateWaitScenarioFrames(frameContext, timeline, scenario); err != nil {
			frameCancel()
			t.Fatalf("wait OpenCode terminal candidate lifecycle frame barrier: %v", err)
		}
		frameCancel()
	}

	missingIdleBoundary := false
	if scenario.requireIdle {
		idleContext, idleCancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := nativeOpenCodeTerminalCandidateWaitGlobalIdle(idleContext, globalCapture, handle.ID)
		idleCancel()
		if err != nil {
			if scenario.name == nativeOpenCodeTerminalStop && errors.Is(err, errNativeOpenCodeTerminalCandidateMissingIdle) {
				missingIdleBoundary = true
				timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.idle.boundary_missing", SessionID: handle.ID, Detail: err.Error()})
				t.Logf("OpenCode terminal_stop has no exact matching idle marker; retaining bounded unknown: %v", err)
			} else {
				t.Fatalf("wait matching OpenCode session idle event: %v; gateway=%v global=%s", err, gateway.Err(), nativeOpenCodeTerminalCandidateGlobalSummary(globalCapture, handle.ID))
			}
		}
	}

	waitTurnContext, waitTurnCancel := context.WithTimeout(context.Background(), nativeOpenCodeTerminalCandidateTimeout)
	var waitTurnErr error
	if scenario.name == nativeOpenCodeTruncatedProvider {
		if err := timeline.WaitFor(waitTurnContext, func(events []nativeOpenCodeTerminalCandidateTimelineEvent) bool {
			_, ok := nativeOpenCodeTerminalCandidateTimelineFirst(events, "provider.response.truncated_eof", 1)
			return ok
		}); err != nil {
			waitTurnCancel()
			t.Fatalf("wait for test-owned provider truncated response barrier: %v", err)
		}
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "wait_turn.called"})
		waitTurnErr = staged.WaitTurn(waitTurnContext)
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "wait_turn.returned", Detail: nativeOpenCodeTerminalCandidateErrorString(waitTurnErr)})
	} else {
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "wait_turn.called"})
		waitTurnErr = staged.WaitTurn(waitTurnContext)
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "wait_turn.returned", Detail: nativeOpenCodeTerminalCandidateErrorString(waitTurnErr)})
	}
	waitTurnCancel()
	if waitTurnErr == nil {
		t.Fatal("OpenCode terminal candidate WaitTurn returned nil; terminal evidence must remain unverified")
	}
	if errors.Is(waitTurnErr, context.Canceled) || errors.Is(waitTurnErr, context.DeadlineExceeded) {
		t.Fatalf("OpenCode terminal candidate WaitTurn was context-bounded instead of fail-closed: %v", waitTurnErr)
	}
	if scenario.name == nativeOpenCodeTruncatedProvider && !errors.Is(waitTurnErr, errTurnUnverified) {
		t.Fatalf("OpenCode truncated provider EOF WaitTurn error = %v, want exact conservative errTurnUnverified", waitTurnErr)
	}

	if scenario.name == nativeOpenCodeTruncatedProvider {
		if err := nativeOpenCodeTerminalCandidateAssertSessionStreamLive(session, witnessAPI); err != nil {
			t.Fatalf("OpenCode truncated provider session stream was not live before explicit shutdown: %v", err)
		}
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "session.capture.live", SessionID: handle.ID})
	}
	if err := globalCapture.AssertLive(); err != nil {
		t.Fatalf("OpenCode global lifecycle capture was not live immediately before shutdown: %v", err)
	}
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.capture.live", SessionID: handle.ID})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.stop.called", SessionID: handle.ID})
	globalCapture.RequestStop()
	globalCancel()
	_ = globalBody.Close()
	globalCapture.Wait(t)
	globalStopped = true
	globalBoundary := nativeOpenCodeTerminalCandidateExpectedCloseError(globalCapture.Err())
	if globalBoundary {
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.close.boundary", SessionID: handle.ID, Detail: globalCapture.Err().Error()})
		t.Logf("OpenCode global lifecycle ended at a physical close boundary; retaining non-success evidence: %v", globalCapture.Err())
	}
	if err := nativeOpenCodeTerminalCandidateValidateGlobalCapture(globalCapture, handle.ID); err != nil {
		t.Fatalf("OpenCode terminal candidate global lifecycle validation: %v", err)
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeOpenCodeTerminalCandidateCloseTimeout)
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "test.close.called", SessionID: handle.ID})
	closeErr := staged.Close(closeContext)
	result, waitErr := staged.Wait(closeContext)
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "test.close.returned", SessionID: handle.ID, Detail: nativeOpenCodeTerminalCandidateErrorString(closeErr)})
	closeCancel()
	if closeErr != nil {
		t.Fatalf("close OpenCode terminal candidate session: %v", closeErr)
	}
	if waitErr != nil {
		t.Fatalf("wait OpenCode terminal candidate session: %v", waitErr)
	}
	closed = true
	if scenario.name == nativeOpenCodeInterrupt && !scenario.expectCancelled {
		t.Fatal("invalid interrupt candidate scenario")
	}
	if result.Semantic != nil {
		t.Fatalf("OpenCode terminal candidate promoted unverified evidence: %+v", result)
	}
	if result.Usage.State != harness.UsageUnknown {
		t.Fatalf("OpenCode terminal candidate usage = %+v, want unknown", result.Usage)
	}
	if scenario.expectCancelled {
		if result.Kind != harness.ResultCancelled {
			t.Fatalf("OpenCode interrupt candidate result = %+v, want exactly ResultCancelled without semantic result", result)
		}
		if result.Reason == nil || *result.Reason != protocol.TaskResultReasonCancelled {
			t.Fatalf("OpenCode interrupt candidate reason = %v, want TaskResultReasonCancelled", result.Reason)
		}
	} else if result.Kind != harness.ResultUnknown {
		t.Fatalf("OpenCode non-interrupt candidate result = %+v, want exactly ResultUnknown", result)
	}
	if !result.Process.Terminated || result.Process.SinkError != nil || result.Process.OutputError != nil || result.Process.OutputTruncated || result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatalf("OpenCode terminal candidate process result = %+v, want bounded clean termination", result.Process)
	}

	turnDoneObserved := false
	select {
	case turnErr := <-turnDone:
		turnDoneObserved = true
		if turnErr != nil && scenario.name != nativeOpenCodeTruncatedProvider && scenario.name != nativeOpenCodeProviderError && scenario.name != nativeOpenCodeStepFailure {
			t.Fatalf("OpenCode terminal candidate StartTurn returned: %v", turnErr)
		}
	case <-time.After(nativeOpenCodeTerminalCandidateCloseTimeout):
		t.Fatal("OpenCode terminal candidate StartTurn did not drain after Close")
	}
	if !turnDoneObserved {
		t.Fatal("OpenCode terminal candidate StartTurn completion was not observed")
	}

	state := nativeOpenCodeTerminalCandidateParseSessionCapture(t, witnessAPI.sessionCapture, handle.ID, witnessAPI.PromptObservation(), timeline)
	if state.captureOverflow {
		t.Fatalf("OpenCode terminal candidate session capture exceeded its bounded memory limit")
	}
	if !state.admissionObserved || state.admissionCount != 1 {
		t.Fatalf("OpenCode terminal candidate session SSE admission count = %d observed=%t, want exactly one matching prompt admission: %+v", state.admissionCount, state.admissionObserved, state)
	}
	if state.identityError != nil {
		t.Fatalf("OpenCode terminal candidate session identity mismatch: %v", state.identityError)
	}
	if state.sequenceError != nil {
		t.Fatalf("OpenCode terminal candidate session sequence regression: %v", state.sequenceError)
	}
	if state.admissionSeq == 0 || state.admissionSeq != witnessAPI.PromptObservation().AdmittedSeq {
		t.Fatalf("OpenCode terminal candidate prompt durable cursor = %d, want intercepted admission sequence %d", state.admissionSeq, witnessAPI.PromptObservation().AdmittedSeq)
	}
	if witnessAPI.ReplayAfter() != 0 {
		t.Fatalf("OpenCode terminal candidate replay cursor = %d, want an actual replay from cursor 0", witnessAPI.ReplayAfter())
	}
	if scenario.requireStepEnd && state.stepEnded == 0 {
		t.Fatalf("OpenCode terminal candidate did not capture Step.Ended after provider exchange: %+v", state)
	}
	if scenario.requireError && !nativeOpenCodeTerminalCandidateHasMatchingError(globalCapture, handle.ID) {
		t.Fatalf("OpenCode terminal candidate did not capture a matching session.error for %s; global=%s", scenario.name, nativeOpenCodeTerminalCandidateGlobalSummary(globalCapture, handle.ID))
	}
	if scenario.name == nativeOpenCodeStepFailure && gateway.ProviderErrorCount() == 0 {
		t.Fatalf("OpenCode Step.Ended-followed-by-failure candidate did not observe the test-owned provider failure")
	}
	if scenario.name == nativeOpenCodeTruncatedProvider {
		nativeOpenCodeTerminalCandidateRecordTruncatedDownstreamBoundary(timeline, globalCapture, sink, handle.ID)
	}
	if missingIdleBoundary && result.Kind != harness.ResultUnknown {
		t.Fatalf("OpenCode terminal_stop missing-idle boundary = %v, want bounded unknown result: %+v", missingIdleBoundary, result)
	}
	if globalBoundary && (result.Kind == harness.ResultSucceeded || result.Semantic != nil) {
		t.Fatalf("OpenCode global close boundary was not fail-closed: %+v", result)
	}
	if scenario.name == nativeOpenCodeTruncatedProvider {
		t.Logf("OpenCode truncated provider candidate remains conservatively unknown: session_events=%d global_events=%d", state.events, nativeOpenCodeTerminalCandidateGlobalEventCount(globalCapture))
	}
	if gatewayErr := gateway.Err(); gatewayErr != nil {
		t.Fatalf("OpenCode terminal candidate provider gateway: %v", gatewayErr)
	}
	if observed := witnessAPI.PromptObservation(); observed.SessionID != handle.ID || !strings.HasPrefix(observed.ID, "msg_") || observed.Text == "" || observed.Delivery != "steer" {
		t.Fatalf("OpenCode terminal candidate prompt identity session=%q id=%q delivery=%q, want matching session/msg admission", nativeOpenCodeTerminalCandidateRedactIdentity(observed.SessionID), nativeOpenCodeTerminalCandidateRedactIdentity(observed.ID), observed.Delivery)
	}
	nativeOpenCodeTerminalCandidateAssertProviderBindings(t, gateway, witnessAPI.PromptObservation(), handle.ID, scenario, state, timeline)
	nativeOpenCodeTerminalCandidateAssertCausality(t, scenario, state, timeline)
	if events := sink.Snapshot(); !nativeOpenCodeTerminalCandidateSinkSequencesMonotonic(events) {
		t.Fatalf("OpenCode terminal candidate normalized event sequence regressed: %+v", events)
	}
	t.Logf("OpenCode terminal candidate scenario=%s session=%s prompt=%s events=%d step_ended=%d global_events=%d result=%s wait_turn=%v", scenario.name, nativeOpenCodeTerminalCandidateRedactIdentity(handle.ID), nativeOpenCodeTerminalCandidateRedactIdentity(witnessAPI.PromptObservation().ID), state.events, state.stepEnded, nativeOpenCodeTerminalCandidateGlobalEventCount(globalCapture), result.Kind, waitTurnErr)
}

func nativeOpenCodeTerminalCandidateCheckVersion(t *testing.T, executable, workspace string, environment []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--version")
	command.Dir = workspace
	command.Env = environment
	output, err := command.Output()
	if err != nil {
		t.Fatalf("run pinned OpenCode --version: %v", err)
	}
	if strings.TrimSpace(string(output)) != TestedVersion {
		t.Fatalf("pinned OpenCode version = %q, want %s", strings.TrimSpace(string(output)), TestedVersion)
	}
}

func nativeOpenCodeTerminalCandidateAssertEnvironment(t *testing.T, environment []string, root, baseURL string) {
	t.Helper()
	if !strings.Contains(strings.Join(environment, "\n"), baseURL) {
		t.Fatalf("OpenCode terminal candidate environment omitted numeric loopback provider URL")
	}
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		upper := strings.ToUpper(key)
		if strings.HasSuffix(upper, "_PROXY") || upper == "NO_PROXY" || upper == "ALL_PROXY" {
			t.Fatalf("OpenCode terminal candidate inherited proxy variable %s", key)
		}
		switch upper {
		case "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY":
			t.Fatalf("OpenCode terminal candidate inherited provider credential %s", key)
		}
		if strings.HasPrefix(upper, "HOME") || upper == "USERPROFILE" || upper == "APPDATA" || upper == "LOCALAPPDATA" || strings.HasPrefix(upper, "XDG_") || upper == "TEMP" || upper == "TMP" || upper == "TMPDIR" {
			if value == "" || !strings.HasPrefix(filepath.Clean(value), filepath.Clean(root)) {
				t.Fatalf("OpenCode terminal candidate environment %s escaped isolated root: %q", key, value)
			}
		}
	}
	for key, want := range map[string]string{
		"OPENCODE_PURE":                       "1",
		"OPENCODE_DISABLE_MODELS_FETCH":       "1",
		"OPENCODE_DISABLE_AUTOUPDATE":         "1",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS":    "1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS":    "1",
		"OPENCODE_DISABLE_CLAUDE_CODE_SKILLS": "1",
	} {
		if got := nativeOpenCodeTerminalCandidateEnvironmentValue(environment, key); got != want {
			t.Fatalf("OpenCode terminal candidate %s = %q, want %q", key, got, want)
		}
	}
}

type nativeOpenCodeTerminalCandidatePrompt struct {
	SessionID   string
	ID          string
	Text        string
	Delivery    string
	AdmittedSeq uint64
}

type nativeOpenCodeTerminalCandidateTimelineEvent struct {
	Index         uint64
	At            time.Time
	Kind          string
	SessionID     string
	MessageID     string
	Delivery      string
	Model         string
	RequestNumber int
	DurableSeq    uint64
	Cursor        uint64
	ToolCallID    string
	Detail        string
}

type nativeOpenCodeTerminalCandidateTimeline struct {
	mu      sync.Mutex
	next    uint64
	events  []nativeOpenCodeTerminalCandidateTimelineEvent
	changed chan struct{}
}

func newNativeOpenCodeTerminalCandidateTimeline() *nativeOpenCodeTerminalCandidateTimeline {
	return &nativeOpenCodeTerminalCandidateTimeline{changed: make(chan struct{})}
}

func (timeline *nativeOpenCodeTerminalCandidateTimeline) Record(event nativeOpenCodeTerminalCandidateTimelineEvent) uint64 {
	if timeline == nil {
		return 0
	}
	timeline.mu.Lock()
	timeline.next++
	event.Index = timeline.next
	event.At = time.Now()
	timeline.events = append(timeline.events, event)
	previous := timeline.changed
	timeline.changed = make(chan struct{})
	close(previous)
	timeline.mu.Unlock()
	return event.Index
}

func (timeline *nativeOpenCodeTerminalCandidateTimeline) Snapshot() []nativeOpenCodeTerminalCandidateTimelineEvent {
	if timeline == nil {
		return nil
	}
	timeline.mu.Lock()
	defer timeline.mu.Unlock()
	return append([]nativeOpenCodeTerminalCandidateTimelineEvent(nil), timeline.events...)
}

func (timeline *nativeOpenCodeTerminalCandidateTimeline) WaitFor(ctx context.Context, predicate func([]nativeOpenCodeTerminalCandidateTimelineEvent) bool) error {
	if timeline == nil {
		return errors.New("OpenCode candidate timeline is nil")
	}
	if ctx == nil {
		return errors.New("OpenCode candidate timeline context is nil")
	}
	for {
		timeline.mu.Lock()
		events := append([]nativeOpenCodeTerminalCandidateTimelineEvent(nil), timeline.events...)
		changed := timeline.changed
		timeline.mu.Unlock()
		if predicate(events) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func nativeOpenCodeTerminalCandidateTimelineFirst(events []nativeOpenCodeTerminalCandidateTimelineEvent, kind string, requestNumber int) (nativeOpenCodeTerminalCandidateTimelineEvent, bool) {
	for _, event := range events {
		if event.Kind == kind && (requestNumber == 0 || event.RequestNumber == requestNumber) {
			return event, true
		}
	}
	return nativeOpenCodeTerminalCandidateTimelineEvent{}, false
}

func nativeOpenCodeTerminalCandidateTimelineFirstByDurableSeq(events []nativeOpenCodeTerminalCandidateTimelineEvent, kind string, durableSeq uint64) (nativeOpenCodeTerminalCandidateTimelineEvent, bool) {
	for _, event := range events {
		if event.Kind == kind && event.DurableSeq == durableSeq {
			return event, true
		}
	}
	return nativeOpenCodeTerminalCandidateTimelineEvent{}, false
}

func nativeOpenCodeTerminalCandidateTimelineCount(events []nativeOpenCodeTerminalCandidateTimelineEvent, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func nativeOpenCodeTerminalCandidateErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func nativeOpenCodeTerminalCandidateFrameType(frame Frame) string {
	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(frame.Data, &envelope) != nil {
		return ""
	}
	return envelope.Type
}

func nativeOpenCodeTerminalCandidateFrameSessionID(frame Frame) string {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(frame.Data, &envelope) != nil {
		return ""
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(envelope.Data, &object) != nil {
		return ""
	}
	return nativeOpenCodeTerminalCandidateString(object, "sessionID", "sessionId")
}

func nativeOpenCodeTerminalCandidateFrameDurableSeq(frame Frame) uint64 {
	var envelope struct {
		Durable struct {
			Seq uint64 `json:"seq"`
		} `json:"durable"`
	}
	if json.Unmarshal(frame.Data, &envelope) != nil {
		return 0
	}
	return envelope.Durable.Seq
}

func cloneNativeOpenCodeTerminalCandidateJSONMap(source map[string]json.RawMessage) map[string]json.RawMessage {
	if source == nil {
		return nil
	}
	clone := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func cloneNativeOpenCodeTerminalCandidateJSONValues(source []json.RawMessage) []json.RawMessage {
	if source == nil {
		return nil
	}
	clone := make([]json.RawMessage, 0, len(source))
	for _, value := range source {
		clone = append(clone, append(json.RawMessage(nil), value...))
	}
	return clone
}

type nativeOpenCodeTerminalCandidateAPI struct {
	*Client
	sessionCapture *nativeOpenCodeTerminalCandidateCollector
	promptObserved chan struct{}
	timeline       *nativeOpenCodeTerminalCandidateTimeline

	mu          sync.Mutex
	prompt      nativeOpenCodeTerminalCandidatePrompt
	replayAfter uint64
	replayOpen  bool
	stream      *nativeOpenCodeTerminalCandidateTeeReadCloser
}

func (api *nativeOpenCodeTerminalCandidateAPI) Prompt(ctx context.Context, sessionID string, request PromptRequest) (PromptAdmission, error) {
	if api.timeline != nil {
		api.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "prompt.request", SessionID: sessionID, MessageID: request.ID, Delivery: request.Delivery})
	}
	api.mu.Lock()
	api.prompt = nativeOpenCodeTerminalCandidatePrompt{SessionID: sessionID, ID: request.ID, Text: request.Text, Delivery: request.Delivery}
	api.mu.Unlock()
	select {
	case <-api.promptObserved:
	default:
		close(api.promptObserved)
	}
	admission, err := api.Client.Prompt(ctx, sessionID, request)
	if err == nil {
		api.mu.Lock()
		api.prompt.AdmittedSeq = admission.AdmittedSeq
		api.mu.Unlock()
		if api.timeline != nil {
			api.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "prompt.admitted.response", SessionID: admission.SessionID, MessageID: admission.ID, Delivery: admission.Delivery, DurableSeq: admission.AdmittedSeq})
		}
	} else if api.timeline != nil {
		api.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "prompt.admission.error", SessionID: sessionID, MessageID: request.ID, Delivery: request.Delivery, Detail: err.Error()})
	}
	return admission, err
}

func (api *nativeOpenCodeTerminalCandidateAPI) OpenSessionEvents(ctx context.Context, sessionID string, after uint64) (io.ReadCloser, error) {
	api.mu.Lock()
	if api.replayOpen {
		api.mu.Unlock()
		return nil, errors.New("OpenCode terminal candidate opened the session replay stream more than once")
	}
	api.replayOpen = true
	api.replayAfter = after
	api.mu.Unlock()
	if api.timeline != nil {
		api.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "session.replay.open", SessionID: sessionID, Cursor: after})
	}
	body, err := api.Client.OpenSessionEvents(ctx, sessionID, after)
	if err != nil {
		return nil, err
	}
	reader := &nativeOpenCodeTerminalCandidateTeeReadCloser{
		ReadCloser:  body,
		collector:   api.sessionCapture,
		timeline:    api.timeline,
		eofObserved: make(chan struct{}),
	}
	api.mu.Lock()
	api.stream = reader
	api.mu.Unlock()
	return reader, nil
}

func (api *nativeOpenCodeTerminalCandidateAPI) PromptObservation() nativeOpenCodeTerminalCandidatePrompt {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.prompt
}

func (api *nativeOpenCodeTerminalCandidateAPI) ReplayAfter() uint64 {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.replayAfter
}

func (api *nativeOpenCodeTerminalCandidateAPI) SessionStream() *nativeOpenCodeTerminalCandidateTeeReadCloser {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.stream
}

func nativeOpenCodeTerminalCandidateWaitAPI(t *testing.T, ready <-chan *nativeOpenCodeTerminalCandidateAPI) *nativeOpenCodeTerminalCandidateAPI {
	t.Helper()
	select {
	case api := <-ready:
		return api
	case <-time.After(nativeOpenCodeTerminalCandidateTimeout):
		t.Fatal("wait for concrete OpenCode Client factory")
		return nil
	}
}

type nativeOpenCodeTerminalCandidateCollector struct {
	mu       sync.Mutex
	maximum  int
	bytes    []byte
	overflow bool
	changed  chan struct{}
}

func newNativeOpenCodeTerminalCandidateCollector(maximum int) *nativeOpenCodeTerminalCandidateCollector {
	return &nativeOpenCodeTerminalCandidateCollector{maximum: maximum, changed: make(chan struct{}, 1)}
}

func (collector *nativeOpenCodeTerminalCandidateCollector) Append(chunk []byte) {
	if collector == nil || len(chunk) == 0 {
		return
	}
	collector.mu.Lock()
	if len(collector.bytes)+len(chunk) > collector.maximum {
		collector.overflow = true
		remaining := collector.maximum - len(collector.bytes)
		if remaining > 0 {
			collector.bytes = append(collector.bytes, chunk[:remaining]...)
		}
	} else {
		collector.bytes = append(collector.bytes, chunk...)
	}
	collector.mu.Unlock()
	select {
	case collector.changed <- struct{}{}:
	default:
	}
}

func (collector *nativeOpenCodeTerminalCandidateCollector) Snapshot() (data []byte, overflow bool) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	return append([]byte(nil), collector.bytes...), collector.overflow
}

type nativeOpenCodeTerminalCandidateTeeReadCloser struct {
	io.ReadCloser
	collector   *nativeOpenCodeTerminalCandidateCollector
	timeline    *nativeOpenCodeTerminalCandidateTimeline
	decoder     *Decoder
	eof         bool
	eofOnce     sync.Once
	eofObserved chan struct{}
}

func (reader *nativeOpenCodeTerminalCandidateTeeReadCloser) Read(data []byte) (int, error) {
	count, err := reader.ReadCloser.Read(data)
	if count > 0 {
		reader.collector.Append(data[:count])
		if reader.timeline != nil {
			reader.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "session.bytes", Detail: fmt.Sprintf("bytes=%d", count)})
		}
		reader.recordFrames(data[:count])
	}
	if err != nil && !reader.eof {
		reader.eof = true
		if errors.Is(err, io.EOF) {
			if reader.decoder == nil {
				reader.decoder = NewDecoder(defaultMaxFrameBytes)
			}
			frames, closeErr := reader.decoder.Close()
			reader.recordDecodedFrames(frames)
			if reader.timeline != nil {
				kind := "session.eof"
				if closeErr != nil {
					kind = "session.eof.error"
				}
				reader.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: kind, Detail: nativeOpenCodeTerminalCandidateErrorString(closeErr)})
			}
			reader.eofOnce.Do(func() {
				if reader.eofObserved != nil {
					close(reader.eofObserved)
				}
			})
		} else if reader.timeline != nil {
			reader.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "session.read.error", Detail: nativeOpenCodeTerminalCandidateErrorString(err)})
		}
	}
	return count, err
}

func (reader *nativeOpenCodeTerminalCandidateTeeReadCloser) recordFrames(data []byte) {
	if len(data) == 0 {
		return
	}
	if reader.decoder == nil {
		reader.decoder = NewDecoder(defaultMaxFrameBytes)
	}
	frames, err := reader.decoder.Feed(data)
	if err != nil {
		if reader.timeline != nil {
			reader.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "session.decode.error", Detail: err.Error()})
		}
		return
	}
	for _, frame := range frames {
		reader.recordDecodedFrames([]Frame{frame})
	}
}

func (reader *nativeOpenCodeTerminalCandidateTeeReadCloser) recordDecodedFrames(frames []Frame) {
	if reader.timeline == nil {
		return
	}
	for _, frame := range frames {
		reader.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "session.frame", DurableSeq: nativeOpenCodeTerminalCandidateFrameDurableSeq(frame), Cursor: frame.Sequence, SessionID: nativeOpenCodeTerminalCandidateFrameSessionID(frame), Detail: nativeOpenCodeTerminalCandidateFrameType(frame)})
	}
}

func nativeOpenCodeTerminalCandidateAssertSessionStreamLive(session harness.Session, api *nativeOpenCodeTerminalCandidateAPI) error {
	if api == nil {
		return errors.New("OpenCode terminal candidate API is nil")
	}
	stream := api.SessionStream()
	if stream == nil || stream.eofObserved == nil {
		return errors.New("OpenCode terminal candidate session stream did not expose a live-state barrier")
	}
	select {
	case <-stream.eofObserved:
		return errors.New("OpenCode terminal candidate session stream already observed EOF")
	default:
	}

	production, ok := session.(*nativeSession)
	if !ok || production == nil {
		return fmt.Errorf("OpenCode terminal candidate returned %T, want *nativeSession for session live-state barrier", session)
	}
	production.mu.Lock()
	streamDone := production.streamDone
	streamErr := production.streamErr
	turnErr := production.turnErr
	closed := production.closed
	production.mu.Unlock()
	if streamDone == nil {
		return errors.New("OpenCode terminal candidate production session watcher was not registered")
	}
	select {
	case <-streamDone:
		return errors.New("OpenCode terminal candidate production session watcher already terminated")
	default:
	}
	if closed {
		return errors.New("OpenCode terminal candidate production session was already closed")
	}
	if streamErr != nil {
		return fmt.Errorf("OpenCode terminal candidate production session stream already has a terminal error: %v", streamErr)
	}
	if turnErr != nil {
		return fmt.Errorf("OpenCode terminal candidate production turn already has a terminal error: %v", turnErr)
	}
	return nil
}

type nativeOpenCodeTerminalCandidateGlobalCapture struct {
	body      io.ReadCloser
	context   context.Context
	collector *nativeOpenCodeTerminalCandidateCollector
	timeline  *nativeOpenCodeTerminalCandidateTimeline
	done      chan struct{}

	mu       sync.Mutex
	err      error
	frames   []Frame
	started  bool
	stopping bool
}

var errNativeOpenCodeTerminalCandidateGlobalEOFBeforeStop = errors.New("OpenCode global lifecycle stream ended before explicit test shutdown")

func newNativeOpenCodeTerminalCandidateGlobalCapture(body io.ReadCloser, context context.Context, timeline *nativeOpenCodeTerminalCandidateTimeline) *nativeOpenCodeTerminalCandidateGlobalCapture {
	return &nativeOpenCodeTerminalCandidateGlobalCapture{body: body, context: context, collector: newNativeOpenCodeTerminalCandidateCollector(nativeOpenCodeTerminalCandidateMaxCaptureBytes), timeline: timeline, done: make(chan struct{})}
}

func (capture *nativeOpenCodeTerminalCandidateGlobalCapture) Start() {
	capture.mu.Lock()
	if capture.started {
		capture.mu.Unlock()
		return
	}
	capture.started = true
	capture.mu.Unlock()
	go capture.run()
}

func (capture *nativeOpenCodeTerminalCandidateGlobalCapture) RequestStop() {
	if capture == nil {
		return
	}
	capture.mu.Lock()
	capture.stopping = true
	capture.mu.Unlock()
}

func (capture *nativeOpenCodeTerminalCandidateGlobalCapture) AssertLive() error {
	if capture == nil {
		return errors.New("OpenCode global lifecycle capture is nil")
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if !capture.started {
		return errors.New("OpenCode global lifecycle capture was not started")
	}
	if capture.stopping {
		return errors.New("OpenCode global lifecycle capture was already stopping")
	}
	if capture.context == nil {
		return errors.New("OpenCode global lifecycle capture context is nil")
	}
	if err := capture.context.Err(); err != nil {
		return fmt.Errorf("OpenCode global lifecycle capture context ended: %w", err)
	}
	if capture.err != nil {
		return fmt.Errorf("OpenCode global lifecycle capture already has a terminal error: %w", capture.err)
	}
	select {
	case <-capture.done:
		return errors.New("OpenCode global lifecycle capture goroutine already terminated")
	default:
	}
	if capture.timeline != nil {
		for _, event := range capture.timeline.Snapshot() {
			if nativeOpenCodeTerminalCandidateIsGlobalTerminalEvent(event.Kind) {
				return fmt.Errorf("OpenCode global lifecycle capture already observed terminal event %q at timeline index %d", event.Kind, event.Index)
			}
		}
	}
	return nil
}

func (capture *nativeOpenCodeTerminalCandidateGlobalCapture) run() {
	defer close(capture.done)
	stop := context.AfterFunc(capture.context, func() { _ = capture.body.Close() })
	defer stop()
	decoder := NewDecoder(defaultMaxFrameBytes)
	buffer := make([]byte, streamReadChunkSize)
	for {
		count, err := capture.body.Read(buffer)
		if count > 0 {
			capture.collector.Append(buffer[:count])
			frames, feedErr := decoder.Feed(buffer[:count])
			if feedErr != nil {
				capture.mu.Lock()
				capture.err = feedErr
				capture.frames = append(capture.frames, frames...)
				capture.mu.Unlock()
				return
			}
			capture.mu.Lock()
			if len(capture.frames)+len(frames) <= nativeOpenCodeTerminalCandidateMaxGlobalFrames {
				capture.frames = append(capture.frames, frames...)
			} else {
				capture.err = errors.New("global OpenCode event capture exceeded frame limit")
			}
			capture.mu.Unlock()
			for _, frame := range frames {
				if capture.timeline != nil {
					capture.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.frame", DurableSeq: nativeOpenCodeTerminalCandidateFrameDurableSeq(frame), Cursor: frame.Sequence, SessionID: nativeOpenCodeTerminalCandidateFrameSessionID(frame), Detail: nativeOpenCodeTerminalCandidateFrameType(frame)})
				}
			}
		}
		if err == nil {
			continue
		}
		capture.mu.Lock()
		if capture.stopping || capture.context.Err() != nil {
			capture.err = capture.context.Err()
			if capture.err == nil {
				capture.err = context.Canceled
			}
		} else if errors.Is(err, io.EOF) {
			frames, closeErr := decoder.Close()
			stopped := capture.stopping || capture.context.Err() != nil
			if closeErr != nil {
				capture.err = closeErr
			} else if len(capture.frames)+len(frames) <= nativeOpenCodeTerminalCandidateMaxGlobalFrames {
				capture.frames = append(capture.frames, frames...)
			}
			if !stopped {
				capture.err = errors.Join(errNativeOpenCodeTerminalCandidateGlobalEOFBeforeStop, capture.err)
			}
			for _, frame := range frames {
				if capture.timeline != nil {
					capture.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.frame", DurableSeq: nativeOpenCodeTerminalCandidateFrameDurableSeq(frame), Cursor: frame.Sequence, SessionID: nativeOpenCodeTerminalCandidateFrameSessionID(frame), Detail: nativeOpenCodeTerminalCandidateFrameType(frame)})
				}
			}
			if capture.timeline != nil {
				kind := "global.eof"
				if closeErr != nil {
					kind = "global.eof.error"
				}
				capture.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: kind, Detail: nativeOpenCodeTerminalCandidateErrorString(closeErr)})
			}
		} else {
			capture.err = err
			if capture.timeline != nil {
				capture.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.read.error", Detail: err.Error()})
			}
		}
		capture.mu.Unlock()
		return
	}
}

func (capture *nativeOpenCodeTerminalCandidateGlobalCapture) Wait(t *testing.T) {
	t.Helper()
	select {
	case <-capture.done:
	case <-time.After(nativeOpenCodeTerminalCandidateCloseTimeout):
		t.Fatalf("global OpenCode event capture did not stop")
	}
}

func (capture *nativeOpenCodeTerminalCandidateGlobalCapture) Snapshot() ([]Frame, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]Frame(nil), capture.frames...), capture.err
}

func (capture *nativeOpenCodeTerminalCandidateGlobalCapture) Err() error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.err
}

type nativeOpenCodeTerminalCandidateGlobalState struct {
	busy             bool
	idle             bool
	sessionLifecycle bool
	missingIdentity  bool
	mismatched       bool
	errors           int
	events           int
	captureErr       error
}

var errNativeOpenCodeTerminalCandidateMissingIdle = errors.New("OpenCode global lifecycle did not expose an exact matching idle marker")

func nativeOpenCodeTerminalCandidateWaitGlobalIdle(ctx context.Context, capture *nativeOpenCodeTerminalCandidateGlobalCapture, sessionID string) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	lastFrameCount := -1
	stableSince := time.Time{}
	for {
		state := nativeOpenCodeTerminalCandidateGlobalStateFromFrames(capture, sessionID)
		if state.captureErr != nil && !errors.Is(state.captureErr, context.Canceled) {
			return fmt.Errorf("global OpenCode event capture failed: %w", state.captureErr)
		}
		if state.missingIdentity {
			return errors.New("global OpenCode lifecycle event omitted its session identity")
		}
		if state.mismatched {
			return errors.New("global OpenCode event carried a different session identity")
		}
		frameCount := nativeOpenCodeTerminalCandidateGlobalEventCount(capture)
		if state.sessionLifecycle && state.busy && state.idle {
			if frameCount != lastFrameCount {
				lastFrameCount = frameCount
				stableSince = time.Now()
			} else if !stableSince.IsZero() && time.Since(stableSince) >= 150*time.Millisecond {
				return nil
			}
		} else {
			lastFrameCount = frameCount
			stableSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			if state.sessionLifecycle && state.busy && !state.idle {
				return fmt.Errorf("%w: session=%s events=%d errors=%d", errNativeOpenCodeTerminalCandidateMissingIdle, nativeOpenCodeTerminalCandidateRedactIdentity(sessionID), state.events, state.errors)
			}
			return fmt.Errorf("wait for OpenCode global idle: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func nativeOpenCodeTerminalCandidateGlobalStateFromFrames(capture *nativeOpenCodeTerminalCandidateGlobalCapture, sessionID string) nativeOpenCodeTerminalCandidateGlobalState {
	// OpenCode 1.18.30's pure-mode global stream may omit an exact idle marker.
	// Only a matching session.status idle observation is accepted; step settlement
	// and error events remain non-terminal evidence.
	frames, captureErr := capture.Snapshot()
	state := nativeOpenCodeTerminalCandidateGlobalState{}
	state.captureErr = captureErr
	for _, frame := range frames {
		var envelope struct {
			Type       string          `json:"type"`
			Data       json.RawMessage `json:"data"`
			Properties json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(frame.Data, &envelope) != nil {
			continue
		}
		state.events++
		data := envelope.Data
		if len(bytes.TrimSpace(data)) == 0 {
			data = envelope.Properties
		}
		object := map[string]json.RawMessage{}
		if json.Unmarshal(data, &object) != nil {
			continue
		}
		if strings.HasPrefix(envelope.Type, "session.") {
			state.sessionLifecycle = true
			candidate := nativeOpenCodeTerminalCandidateString(object, "sessionID", "sessionId")
			if candidate == "" {
				state.missingIdentity = true
				continue
			}
			if candidate != sessionID {
				state.mismatched = true
				continue
			}
		}
		switch envelope.Type {
		case "session.status", "session.status.updated":
			state.busy = true
			switch nativeOpenCodeTerminalCandidateStatus(object) {
			case "idle":
				state.idle = true
			case "busy":
				state.idle = false
			}
		case "session.error", "session.next.step.failed":
			state.busy = true
			state.errors++
		default:
			if strings.HasPrefix(envelope.Type, "session.next.") {
				state.busy = true
				switch envelope.Type {
				case "session.next.prompt.admitted", "session.next.prompted", "session.next.step.started", "session.next.text.started", "session.next.text.delta":
					state.idle = false
				}
			}
		}
	}
	return state
}

func nativeOpenCodeTerminalCandidateValidateGlobalCapture(capture *nativeOpenCodeTerminalCandidateGlobalCapture, sessionID string) error {
	state := nativeOpenCodeTerminalCandidateGlobalStateFromFrames(capture, sessionID)
	if state.captureErr != nil && !errors.Is(state.captureErr, context.Canceled) && !nativeOpenCodeTerminalCandidateExpectedCloseError(state.captureErr) {
		return fmt.Errorf("capture error: %w", state.captureErr)
	}
	if capture == nil || capture.timeline == nil {
		return errors.New("global lifecycle capture omitted the shared shutdown timeline")
	}
	events := capture.timeline.Snapshot()
	stop, ok := nativeOpenCodeTerminalCandidateTimelineFirst(events, "global.stop.called", 0)
	if !ok {
		return errors.New("global lifecycle capture omitted the test-initiated shutdown barrier")
	}
	for _, event := range events {
		if nativeOpenCodeTerminalCandidateIsGlobalTerminalEvent(event.Kind) && event.Index <= stop.Index {
			return fmt.Errorf("global lifecycle terminal event %q at timeline index %d occurred before shutdown initiation index %d", event.Kind, event.Index, stop.Index)
		}
	}
	if !state.sessionLifecycle {
		return errors.New("global lifecycle capture omitted all session-scoped events")
	}
	if state.missingIdentity {
		return errors.New("global lifecycle capture omitted a session identity")
	}
	if state.mismatched {
		return errors.New("global lifecycle capture contained a mismatched session identity")
	}
	return nil
}

func nativeOpenCodeTerminalCandidateIsGlobalTerminalEvent(kind string) bool {
	switch kind {
	case "global.eof", "global.eof.error", "global.read.error":
		return true
	default:
		return false
	}
}

func nativeOpenCodeTerminalCandidateExpectedCloseError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) {
		return errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed)
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "forcibly closed") || strings.Contains(message, "use of closed network connection")
}

func TestNativeOpenCodeTerminalCandidateGlobalLifecycleIdentityFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name         string
		frame        Frame
		wantMissing  bool
		wantMismatch bool
	}{
		{
			name:        "missing session identity",
			frame:       Frame{Data: json.RawMessage(`{"type":"session.status","data":{"status":"idle"}}`)},
			wantMissing: true,
		},
		{
			name:         "mismatched session identity",
			frame:        Frame{Data: json.RawMessage(`{"type":"session.status","data":{"sessionID":"ses_other","status":"idle"}}`)},
			wantMismatch: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture := newNativeOpenCodeTerminalCandidateGlobalCapture(io.NopCloser(strings.NewReader("")), context.Background(), nil)
			capture.mu.Lock()
			capture.frames = []Frame{test.frame}
			capture.mu.Unlock()
			state := nativeOpenCodeTerminalCandidateGlobalStateFromFrames(capture, "ses_expected")
			if state.missingIdentity != test.wantMissing || state.mismatched != test.wantMismatch {
				t.Fatalf("global lifecycle identity state = %+v, want missing=%t mismatch=%t", state, test.wantMissing, test.wantMismatch)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := nativeOpenCodeTerminalCandidateWaitGlobalIdle(ctx, capture, "ses_expected"); err == nil {
				t.Fatal("global lifecycle identity failure was accepted as idle")
			}
		})
	}

	capture := newNativeOpenCodeTerminalCandidateGlobalCapture(io.NopCloser(strings.NewReader("")), context.Background(), nil)
	capture.mu.Lock()
	capture.err = errors.New("synthetic global decoder failure")
	capture.mu.Unlock()
	if err := nativeOpenCodeTerminalCandidateWaitGlobalIdle(context.Background(), capture, "ses_expected"); err == nil {
		t.Fatal("global lifecycle capture error was accepted as idle")
	}
}

func TestNativeOpenCodeTerminalCandidateGlobalCaptureRejectsEarlyEOF(t *testing.T) {
	timeline := newNativeOpenCodeTerminalCandidateTimeline()
	capture := newNativeOpenCodeTerminalCandidateGlobalCapture(io.NopCloser(strings.NewReader("")), context.Background(), timeline)
	capture.Start()
	capture.Wait(t)
	if !errors.Is(capture.Err(), errNativeOpenCodeTerminalCandidateGlobalEOFBeforeStop) {
		t.Fatalf("early global EOF error = %v, want errNativeOpenCodeTerminalCandidateGlobalEOFBeforeStop", capture.Err())
	}
	eof, ok := nativeOpenCodeTerminalCandidateTimelineFirst(timeline.Snapshot(), "global.eof", 0)
	if !ok {
		t.Fatal("early global EOF did not record a global.eof timeline event")
	}
	if _, ok := nativeOpenCodeTerminalCandidateTimelineFirst(timeline.Snapshot(), "global.stop.called", 0); ok {
		t.Fatalf("early global EOF timeline unexpectedly contained a shutdown barrier after event %+v", eof)
	}
	if err := nativeOpenCodeTerminalCandidateValidateGlobalCapture(capture, "ses_meta"); err == nil {
		t.Fatal("global lifecycle validation accepted EOF before test-initiated shutdown")
	}
}

func TestNativeOpenCodeTerminalCandidateTruncatedCausalityRequiresExplicitBoundary(t *testing.T) {
	timeline := newNativeOpenCodeTerminalCandidateTimeline()
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "provider.response.truncated_eof", RequestNumber: 1})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "wait_turn.called"})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "wait_turn.returned", Detail: "errTurnUnverified"})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "session.capture.live"})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.capture.live"})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "global.stop.called"})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "truncated.downstream.eof.unverified"})
	if err := nativeOpenCodeTerminalCandidateValidateTruncatedBoundary(timeline.Snapshot()); err != nil {
		t.Fatalf("valid conservative truncated boundary rejected: %v", err)
	}

	timeline = newNativeOpenCodeTerminalCandidateTimeline()
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "provider.response.truncated_eof", RequestNumber: 1})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "wait_turn.called"})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "wait_turn.returned", Detail: "errTurnUnverified"})
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "truncated.downstream.eof.consumed"})
	if err := nativeOpenCodeTerminalCandidateValidateTruncatedBoundary(timeline.Snapshot()); err != nil {
		t.Fatalf("valid downstream truncated boundary rejected: %v", err)
	}
}

func nativeOpenCodeTerminalCandidateHasMatchingError(capture *nativeOpenCodeTerminalCandidateGlobalCapture, sessionID string) bool {
	// A direct session.error is accepted when emitted. The pinned provider-error
	// path may instead expose session.next.step.failed, which is still checked
	// for the exact session identity and remains non-terminal evidence.
	frames, _ := capture.Snapshot()
	for _, frame := range frames {
		var envelope struct {
			Type       string          `json:"type"`
			Data       json.RawMessage `json:"data"`
			Properties json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(frame.Data, &envelope) != nil || (envelope.Type != "session.error" && envelope.Type != "session.next.step.failed") {
			continue
		}
		data := envelope.Data
		if len(bytes.TrimSpace(data)) == 0 {
			data = envelope.Properties
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(data, &object) != nil {
			continue
		}
		return nativeOpenCodeTerminalCandidateString(object, "sessionID", "sessionId") == sessionID
	}
	return false
}

func nativeOpenCodeTerminalCandidateSinkHasCode(sink *nativeOpenCodeTerminalCandidateSink, code string) bool {
	if sink == nil || code == "" {
		return false
	}
	for _, event := range sink.Snapshot() {
		if event.Code == code {
			return true
		}
	}
	return false
}

func nativeOpenCodeTerminalCandidateRecordTruncatedDownstreamBoundary(timeline *nativeOpenCodeTerminalCandidateTimeline, capture *nativeOpenCodeTerminalCandidateGlobalCapture, sink *nativeOpenCodeTerminalCandidateSink, sessionID string) {
	if timeline == nil {
		return
	}
	if nativeOpenCodeTerminalCandidateHasMatchingError(capture, sessionID) && nativeOpenCodeTerminalCandidateSinkHasCode(sink, "opencode_stream_error") {
		timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "truncated.downstream.eof.consumed", SessionID: sessionID, Detail: "matching same-session durable error and production sink event"})
		return
	}
	timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "truncated.downstream.eof.unverified", SessionID: sessionID, Detail: "matching same-session durable error and production sink event not both observed"})
}

func nativeOpenCodeTerminalCandidateStatus(object map[string]json.RawMessage) string {
	if value := nativeOpenCodeTerminalCandidateString(object, "status"); value != "" {
		return strings.ToLower(value)
	}
	if raw := object["status"]; len(raw) > 0 {
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil {
			return strings.ToLower(nativeOpenCodeTerminalCandidateString(nested, "type", "status"))
		}
	}
	return ""
}

func nativeOpenCodeTerminalCandidateString(object map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		if raw, ok := object[name]; ok {
			var value string
			if json.Unmarshal(raw, &value) == nil {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

type nativeOpenCodeTerminalCandidateSessionState struct {
	events            int
	stepStarted       int
	stepEnded         int
	stepFailed        int
	toolCalled        int
	toolSucceeded     int
	unknown           int
	admissionObserved bool
	admissionCount    int
	admissionSeq      uint64
	identityError     error
	sequenceError     error
	captureOverflow   bool
	evidence          []nativeOpenCodeTerminalCandidateSessionEvidence
}

type nativeOpenCodeTerminalCandidateSessionEvidence struct {
	Kind               SessionEventKind
	DurableSeq         uint64
	TimelineIndex      uint64
	SessionID          string
	MessageID          string
	AssistantMessageID string
	CallID             string
	Tool               string
	ToolInput          map[string]json.RawMessage
	ToolContent        []json.RawMessage
	ToolResult         json.RawMessage
	OutputPaths        []string
}

func nativeOpenCodeTerminalCandidateParseSessionCapture(t *testing.T, capture *nativeOpenCodeTerminalCandidateCollector, sessionID string, prompt nativeOpenCodeTerminalCandidatePrompt, timeline *nativeOpenCodeTerminalCandidateTimeline) nativeOpenCodeTerminalCandidateSessionState {
	t.Helper()
	state := nativeOpenCodeTerminalCandidateSessionState{}
	data, overflow := capture.Snapshot()
	state.captureOverflow = overflow
	decoder := NewDecoder(defaultMaxFrameBytes)
	frames, err := decoder.Feed(data)
	if err == nil {
		last, closeErr := decoder.Close()
		frames = append(frames, last...)
		if closeErr != nil {
			// A physical EOF/truncation is evidence for unknown, never success.
			state.identityError = fmt.Errorf("session SSE decoder EOF: %w", closeErr)
		}
	} else {
		state.identityError = err
	}
	if len(frames) > nativeOpenCodeTerminalCandidateMaxSessionFrames {
		state.identityError = errors.New("session OpenCode event capture exceeded frame limit")
		frames = frames[:nativeOpenCodeTerminalCandidateMaxSessionFrames]
	}
	validator, validatorErr := NewSessionEventValidator(sessionID, 0)
	if validatorErr != nil {
		state.identityError = validatorErr
		return state
	}
	for _, frame := range frames {
		state.events++
		event, observeErr := validator.ObserveEvent(frame)
		if observeErr != nil {
			state.sequenceError = observeErr
			continue
		}
		if event.SessionID != sessionID || event.Durable == nil || event.Durable.AggregateID != sessionID {
			aggregateID := ""
			if event.Durable != nil {
				aggregateID = event.Durable.AggregateID
			}
			state.identityError = fmt.Errorf("event session=%q aggregate=%q, want %q", nativeOpenCodeTerminalCandidateRedactIdentity(event.SessionID), nativeOpenCodeTerminalCandidateRedactIdentity(aggregateID), nativeOpenCodeTerminalCandidateRedactIdentity(sessionID))
			if event.Durable == nil {
				continue
			}
		}
		evidence := nativeOpenCodeTerminalCandidateSessionEvidence{Kind: event.Kind, DurableSeq: event.Durable.Seq, SessionID: event.SessionID}
		if timelineEvent, ok := nativeOpenCodeTerminalCandidateTimelineFirstByDurableSeq(timeline.Snapshot(), "session.frame", event.Durable.Seq); ok {
			evidence.TimelineIndex = timelineEvent.Index
		}
		switch event.Kind {
		case SessionEventPromptAdmitted:
			state.admissionCount++
			if state.admissionCount > 1 {
				state.identityError = errors.New("session replay contained more than one prompt admission")
			}
			if event.PromptAdmitted != nil && event.PromptAdmitted.MessageID == prompt.ID && event.PromptAdmitted.Text == prompt.Text && event.PromptAdmitted.Delivery == prompt.Delivery {
				state.admissionObserved = true
				state.admissionSeq = event.Durable.Seq
				evidence.MessageID = event.PromptAdmitted.MessageID
			} else {
				state.identityError = errors.New("prompt admission event did not match intercepted prompt identity")
			}
		case SessionEventStepStarted:
			state.stepStarted++
			if event.StepStarted != nil {
				evidence.AssistantMessageID = event.StepStarted.AssistantMessageID
			}
		case SessionEventStepEnded:
			state.stepEnded++
			if event.StepEnded != nil {
				evidence.AssistantMessageID = event.StepEnded.AssistantMessageID
			}
		case SessionEventStepFailed:
			state.stepFailed++
			if event.StepFailed != nil {
				evidence.AssistantMessageID = event.StepFailed.AssistantMessageID
			}
		case SessionEventToolCalled:
			state.toolCalled++
			if event.ToolCalled != nil {
				evidence.AssistantMessageID = event.ToolCalled.AssistantMessageID
				evidence.CallID = event.ToolCalled.CallID
				evidence.Tool = event.ToolCalled.Tool
				evidence.ToolInput = cloneNativeOpenCodeTerminalCandidateJSONMap(event.ToolCalled.Input)
			}
		case SessionEventToolSuccess:
			state.toolSucceeded++
			if event.ToolSuccess != nil {
				evidence.AssistantMessageID = event.ToolSuccess.AssistantMessageID
				evidence.CallID = event.ToolSuccess.CallID
				evidence.ToolContent = cloneNativeOpenCodeTerminalCandidateJSONValues(event.ToolSuccess.Content)
				evidence.ToolResult = append(json.RawMessage(nil), event.ToolSuccess.Result...)
				evidence.OutputPaths = append([]string(nil), event.ToolSuccess.OutputPaths...)
			}
		case SessionEventUnknown:
			state.unknown++
		}
		state.evidence = append(state.evidence, evidence)
	}
	return state
}

func nativeOpenCodeTerminalCandidateGlobalEventCount(capture *nativeOpenCodeTerminalCandidateGlobalCapture) int {
	frames, _ := capture.Snapshot()
	return len(frames)
}

func nativeOpenCodeTerminalCandidateGlobalSummary(capture *nativeOpenCodeTerminalCandidateGlobalCapture, sessionID string) string {
	frames, err := capture.Snapshot()
	parts := make([]string, 0, 16)
	typeCounts := make(map[string]int)
	for index, frame := range frames {
		var envelope struct {
			Type       string          `json:"type"`
			Data       json.RawMessage `json:"data"`
			Properties json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(frame.Data, &envelope) != nil {
			typeCounts["invalid"]++
			continue
		}
		typeCounts[envelope.Type]++
		if index >= 96 {
			continue
		}
		data := envelope.Data
		if len(bytes.TrimSpace(data)) == 0 {
			data = envelope.Properties
		}
		var object map[string]json.RawMessage
		_ = json.Unmarshal(data, &object)
		parts = append(parts, fmt.Sprintf("%s/%s/%s", envelope.Type, nativeOpenCodeTerminalCandidateRedactIdentity(nativeOpenCodeTerminalCandidateString(object, "sessionID", "sessionId")), nativeOpenCodeTerminalCandidateStatus(object)))
	}
	counts := make([]string, 0, len(typeCounts))
	for eventType, count := range typeCounts {
		counts = append(counts, fmt.Sprintf("%s:%d", eventType, count))
	}
	if err != nil {
		counts = append(counts, "capture="+nativeOpenCodeBoundedString(err.Error(), 96))
	}
	return fmt.Sprintf("frames=%d first=%s types=%s", len(frames), strings.Join(parts, ","), strings.Join(counts, ","))
}

func nativeOpenCodeTerminalCandidateWait(t *testing.T, ctx context.Context, done <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("wait for %s: %v", label, ctx.Err())
	}
}

func nativeOpenCodeTerminalCandidateWaitScenarioFrames(ctx context.Context, timeline *nativeOpenCodeTerminalCandidateTimeline, scenario nativeOpenCodeTerminalCandidateScenarioSpec) error {
	return timeline.WaitFor(ctx, func(events []nativeOpenCodeTerminalCandidateTimelineEvent) bool {
		count := func(detail string) int {
			return nativeOpenCodeTerminalCandidateTimelineCountDetail(events, "session.frame", detail)
		}
		switch scenario.name {
		case nativeOpenCodeTerminalStop:
			return count("session.next.step.ended") >= 1
		case nativeOpenCodeProviderError:
			return count("session.next.step.failed") >= 1
		case nativeOpenCodeInterrupt:
			return count("session.next.tool.success") >= 1 && count("session.next.step.ended") >= 1
		case nativeOpenCodeStepContinuation:
			return count("session.next.tool.success") >= 1 && count("session.next.step.ended") >= 1 && count("session.next.step.started") >= 2
		case nativeOpenCodeStepFailure:
			return count("session.next.tool.success") >= 1 && count("session.next.step.ended") >= 1 && count("session.next.step.failed") >= 1
		default:
			return false
		}
	})
}

func nativeOpenCodeTerminalCandidateTimelineCountDetail(events []nativeOpenCodeTerminalCandidateTimelineEvent, kind, detail string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind && event.Detail == detail {
			count++
		}
	}
	return count
}

func nativeOpenCodeTerminalCandidateLastTimeline(events []nativeOpenCodeTerminalCandidateTimelineEvent, kind string) (nativeOpenCodeTerminalCandidateTimelineEvent, bool) {
	var found nativeOpenCodeTerminalCandidateTimelineEvent
	seen := false
	for _, event := range events {
		if event.Kind == kind {
			found = event
			seen = true
		}
	}
	return found, seen
}

func nativeOpenCodeTerminalCandidateEvidence(state nativeOpenCodeTerminalCandidateSessionState, kind SessionEventKind, ordinal int) (nativeOpenCodeTerminalCandidateSessionEvidence, bool) {
	seen := 0
	for _, evidence := range state.evidence {
		if evidence.Kind != kind {
			continue
		}
		if seen == ordinal {
			return evidence, true
		}
		seen++
	}
	return nativeOpenCodeTerminalCandidateSessionEvidence{}, false
}

func nativeOpenCodeTerminalCandidateAssertProviderBindings(t *testing.T, gateway *nativeOpenCodeTerminalCandidateGateway, prompt nativeOpenCodeTerminalCandidatePrompt, sessionID string, scenario nativeOpenCodeTerminalCandidateScenarioSpec, state nativeOpenCodeTerminalCandidateSessionState, timeline *nativeOpenCodeTerminalCandidateTimeline) {
	t.Helper()
	requests := gateway.ProviderRequests()
	if len(requests) != scenario.expectedProviderRequests {
		t.Fatalf("OpenCode terminal candidate provider request count = %d, want exactly %d", len(requests), scenario.expectedProviderRequests)
	}
	if prompt.AdmittedSeq == 0 || state.admissionSeq != prompt.AdmittedSeq {
		t.Fatalf("OpenCode terminal candidate admission binding = prompt_seq:%d session_seq:%d, want one positive equal cursor", prompt.AdmittedSeq, state.admissionSeq)
	}
	if prompt.SessionID != sessionID || prompt.ID == "" || prompt.Delivery == "" || prompt.Text == "" {
		t.Fatalf("OpenCode terminal candidate intercepted prompt = %+v, want exact session/id/body/delivery", prompt)
	}
	toolExpected := scenario.name == nativeOpenCodeInterrupt || scenario.name == nativeOpenCodeStepContinuation || scenario.name == nativeOpenCodeStepFailure
	wantToolEvents := 0
	if toolExpected {
		wantToolEvents = 1
	}
	if state.toolCalled != wantToolEvents || state.toolSucceeded != wantToolEvents {
		t.Fatalf("OpenCode terminal candidate tool event counts = called:%d succeeded:%d, want %d each", state.toolCalled, state.toolSucceeded, wantToolEvents)
	}
	for index, request := range requests {
		if request.Number != index+1 || request.SessionID != sessionID || request.Model != nativeOpenCodeTerminalCandidateModel || !request.Stream || len(request.Body) == 0 {
			t.Fatalf("OpenCode terminal candidate provider request[%d] binding = number:%d session:%q model:%q stream:%t body_bytes:%d", index, request.Number, request.SessionID, request.Model, request.Stream, len(request.Body))
		}
		if !nativeOpenCodeTerminalCandidateProviderRequestContainsPrompt(request.Messages, prompt.Text) {
			t.Fatalf("OpenCode terminal candidate provider request[%d] omitted the exact intercepted prompt body", index+1)
		}
		wantToolMessages := 0
		if toolExpected && index == 1 {
			wantToolMessages = 1
		}
		if scenario.name == nativeOpenCodeStepFailure && index >= 1 {
			wantToolMessages = 1
		}
		if request.ToolMessages != wantToolMessages {
			t.Fatalf("OpenCode terminal candidate provider request[%d] tool messages = %d, want %d", index+1, request.ToolMessages, wantToolMessages)
		}
		if wantToolMessages == 1 {
			called, ok := nativeOpenCodeTerminalCandidateEvidence(state, SessionEventToolCalled, 0)
			succeeded, successOK := nativeOpenCodeTerminalCandidateEvidence(state, SessionEventToolSuccess, 0)
			if !ok || !successOK || called.CallID == "" || called.CallID != succeeded.CallID || called.Tool != "write" {
				t.Fatalf("OpenCode terminal candidate tool result is not bound to one write call: called=%+v succeeded=%+v", called, succeeded)
			}
			if string(called.ToolInput["path"]) != fmt.Sprintf("%q", nativeOpenCodeTerminalCandidateArtifact) || string(called.ToolInput["content"]) != fmt.Sprintf("%q", nativeOpenCodeTerminalCandidateContent) {
				t.Fatalf("OpenCode terminal candidate tool input = %#v, want exact artifact/content", called.ToolInput)
			}
			if request.ToolCallID != called.CallID {
				t.Fatalf("OpenCode terminal candidate provider tool_call_id = %q, want session call %q", request.ToolCallID, called.CallID)
			}
			toolText := nativeOpenCodeTerminalCandidateEvidenceText(succeeded)
			if toolText == "" || !strings.Contains(request.ToolContent, toolText) {
				t.Fatalf("OpenCode terminal candidate provider tool result = %q, want actual session tool result containing %q", request.ToolContent, toolText)
			}
		}
	}
	events := timeline.Snapshot()
	if nativeOpenCodeTerminalCandidateTimelineCount(events, "provider.response.eof")+nativeOpenCodeTerminalCandidateTimelineCount(events, "provider.response.error")+nativeOpenCodeTerminalCandidateTimelineCount(events, "provider.response.cancelled")+nativeOpenCodeTerminalCandidateTimelineCount(events, "provider.response.truncated_eof") != scenario.expectedProviderRequests {
		t.Fatalf("OpenCode terminal candidate response barrier count does not match requests: requests=%d timeline=%v", len(requests), events)
	}
	admissionResponse, ok := nativeOpenCodeTerminalCandidateTimelineFirst(events, "prompt.admitted.response", 0)
	if !ok || admissionResponse.SessionID != sessionID || admissionResponse.MessageID != prompt.ID || admissionResponse.DurableSeq != prompt.AdmittedSeq {
		t.Fatalf("OpenCode terminal candidate admission response timeline = %+v, want exact prompt/session/cursor binding", admissionResponse)
	}
	admissionFrame, ok := nativeOpenCodeTerminalCandidateTimelineFirstByDurableSeq(events, "session.frame", prompt.AdmittedSeq)
	if !ok || admissionFrame.SessionID != sessionID || admissionFrame.Detail != "session.next.prompt.admitted" {
		t.Fatalf("OpenCode terminal candidate durable admission frame = %+v, want exact session/cursor frame", admissionFrame)
	}
	if requests[0].TimelineIndex <= admissionFrame.Index {
		t.Fatalf("OpenCode terminal candidate provider request[1] index=%d did not follow admitted durable frame index=%d", requests[0].TimelineIndex, admissionFrame.Index)
	}
}

func nativeOpenCodeTerminalCandidateProviderRequestContainsPrompt(messages []nativeOpenCodeTerminalCandidateProviderMessage, prompt string) bool {
	for _, message := range messages {
		if (message.Role == "user" || message.Role == "system") && nativeOpenCodeTerminalCandidateRawContainsExactText(message.Content, prompt) {
			return true
		}
	}
	return false
}

func nativeOpenCodeTerminalCandidateEvidenceText(evidence nativeOpenCodeTerminalCandidateSessionEvidence) string {
	parts := make([]string, 0, len(evidence.ToolContent)+1)
	for _, content := range evidence.ToolContent {
		var object map[string]json.RawMessage
		if json.Unmarshal(content, &object) == nil {
			if text := nativeOpenCodeTerminalCandidateString(object, "text"); text != "" {
				parts = append(parts, text)
				continue
			}
		}
		if text := nativeOpenCodeTerminalCandidateJSONText(content); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 && len(evidence.ToolResult) > 0 {
		if text := nativeOpenCodeTerminalCandidateJSONText(evidence.ToolResult); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func nativeOpenCodeTerminalCandidateAssertCausality(t *testing.T, scenario nativeOpenCodeTerminalCandidateScenarioSpec, state nativeOpenCodeTerminalCandidateSessionState, timeline *nativeOpenCodeTerminalCandidateTimeline) {
	t.Helper()
	events := timeline.Snapshot()
	firstEnded, hasFirstEnded := nativeOpenCodeTerminalCandidateEvidence(state, SessionEventStepEnded, 0)
	secondRequest, hasSecondRequest := nativeOpenCodeTerminalCandidateTimelineFirst(events, "provider.request", 2)
	if scenario.name == nativeOpenCodeStepContinuation || scenario.name == nativeOpenCodeStepFailure {
		if !hasFirstEnded || firstEnded.TimelineIndex == 0 || !hasSecondRequest {
			t.Fatalf("OpenCode %s lacks the first Step.Ended and second provider request causality evidence: state=%+v events=%+v", scenario.name, state, events)
		}
		if firstEnded.TimelineIndex >= secondRequest.Index {
			t.Fatalf("OpenCode %s first Step.Ended index=%d did not precede second provider request index=%d", scenario.name, firstEnded.TimelineIndex, secondRequest.Index)
		}
		if nextStarted, ok := nativeOpenCodeTerminalCandidateEvidence(state, SessionEventStepStarted, 1); ok {
			if nextStarted.TimelineIndex <= secondRequest.Index {
				t.Fatalf("OpenCode %s second provider request index=%d did not precede next Step.Started index=%d", scenario.name, secondRequest.Index, nextStarted.TimelineIndex)
			}
		} else if scenario.name == nativeOpenCodeStepContinuation {
			t.Fatal("OpenCode continuation candidate omitted the next Step.Started after the second provider request")
		}
		if scenario.name == nativeOpenCodeStepFailure {
			failed, ok := nativeOpenCodeTerminalCandidateEvidence(state, SessionEventStepFailed, 0)
			if !ok || failed.TimelineIndex <= firstEnded.TimelineIndex || failed.TimelineIndex <= secondRequest.Index {
				t.Fatalf("OpenCode failure candidate did not prove later Step.Failed causality: %+v", failed)
			}
			failureResponse, ok := nativeOpenCodeTerminalCandidateTimelineFirst(events, "provider.response.error", 2)
			if !ok || failureResponse.Index <= firstEnded.TimelineIndex || failureResponse.Index >= failed.TimelineIndex {
				t.Fatalf("OpenCode failure candidate provider error timeline = %+v, want after first Step.Ended and before Step.Failed", failureResponse)
			}
		}
	}
	if scenario.name == nativeOpenCodeTruncatedProvider {
		if err := nativeOpenCodeTerminalCandidateValidateTruncatedBoundary(events); err != nil {
			t.Fatalf("OpenCode truncated candidate downstream boundary: %v", err)
		}
	}
}

func nativeOpenCodeTerminalCandidateValidateTruncatedBoundary(events []nativeOpenCodeTerminalCandidateTimelineEvent) error {
	consumed, consumedOK := nativeOpenCodeTerminalCandidateTimelineFirst(events, "truncated.downstream.eof.consumed", 0)
	unverified, unverifiedOK := nativeOpenCodeTerminalCandidateTimelineFirst(events, "truncated.downstream.eof.unverified", 0)
	if consumedOK == unverifiedOK {
		return fmt.Errorf("truncated downstream EOF boundary must be exactly one of consumed or unverified: consumed=%+v unverified=%+v", consumed, unverified)
	}
	return nil
}

type nativeOpenCodeTerminalCandidateSink struct {
	mu       sync.Mutex
	events   []harness.Event
	timeline *nativeOpenCodeTerminalCandidateTimeline
}

func (sink *nativeOpenCodeTerminalCandidateSink) Handle(_ context.Context, event harness.Event) error {
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	sink.mu.Unlock()
	if sink.timeline != nil {
		sink.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "sink.event", DurableSeq: event.Sequence, Detail: event.Code})
	}
	return nil
}

func (sink *nativeOpenCodeTerminalCandidateSink) Snapshot() []harness.Event {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]harness.Event(nil), sink.events...)
}

func nativeOpenCodeTerminalCandidateSinkSequencesMonotonic(events []harness.Event) bool {
	var last uint64
	for _, event := range events {
		if event.Sequence != 0 && event.Sequence < last {
			return false
		}
		if event.Sequence > last {
			last = event.Sequence
		}
	}
	return true
}

type nativeOpenCodeTerminalCandidateGateway struct {
	server   *http.Server
	listener net.Listener
	baseURL  string
	model    string
	scenario nativeOpenCodeTerminalCandidateScenario
	timeline *nativeOpenCodeTerminalCandidateTimeline

	firstRequestSeen  chan struct{}
	firstReturned     chan struct{}
	followUpSeen      chan struct{}
	followUpActive    chan struct{}
	followUpReturned  chan struct{}
	followUpCancelled chan struct{}
	closeOnce         sync.Once

	mu                  sync.Mutex
	requests            int
	providerErrors      int
	err                 error
	sessionID           string
	providerRequests    []nativeOpenCodeTerminalCandidateProviderRequest
	requestCountChanged chan struct{}
}

type nativeOpenCodeTerminalCandidateProviderMessage struct {
	Role       string
	ToolCallID string
	Content    json.RawMessage
	Raw        json.RawMessage
}

type nativeOpenCodeTerminalCandidateProviderRequest struct {
	Number        int
	TimelineIndex uint64
	SessionID     string
	Model         string
	Stream        bool
	Body          []byte
	Messages      []nativeOpenCodeTerminalCandidateProviderMessage
	ToolMessages  int
	ToolCallID    string
	ToolContent   string
}

func newNativeOpenCodeTerminalCandidateGateway(t *testing.T, scenario nativeOpenCodeTerminalCandidateScenario, model string, timeline *nativeOpenCodeTerminalCandidateTimeline) *nativeOpenCodeTerminalCandidateGateway {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen numeric OpenCode terminal candidate gateway: %v", err)
	}
	gateway := &nativeOpenCodeTerminalCandidateGateway{
		listener:            listener,
		baseURL:             "http://" + listener.Addr().String() + "/v1",
		model:               model,
		scenario:            scenario,
		timeline:            timeline,
		firstRequestSeen:    make(chan struct{}),
		firstReturned:       make(chan struct{}),
		followUpSeen:        make(chan struct{}),
		followUpActive:      make(chan struct{}),
		followUpReturned:    make(chan struct{}),
		followUpCancelled:   make(chan struct{}),
		requestCountChanged: make(chan struct{}),
	}
	gateway.server = &http.Server{Handler: http.HandlerFunc(gateway.handle)}
	go func() {
		if err := gateway.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			gateway.recordError(err)
		}
	}()
	return gateway
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) BindSession(sessionID string) {
	if gateway == nil {
		return
	}
	gateway.mu.Lock()
	gateway.sessionID = sessionID
	gateway.mu.Unlock()
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) boundSession() string {
	if gateway == nil {
		return ""
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return gateway.sessionID
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) ProviderRequests() []nativeOpenCodeTerminalCandidateProviderRequest {
	if gateway == nil {
		return nil
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	requests := append([]nativeOpenCodeTerminalCandidateProviderRequest(nil), gateway.providerRequests...)
	for index := range requests {
		requests[index].Body = append([]byte(nil), requests[index].Body...)
		requests[index].Messages = append([]nativeOpenCodeTerminalCandidateProviderMessage(nil), requests[index].Messages...)
	}
	return requests
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) recordProviderRequest(body []byte, model string, stream bool, messages []nativeOpenCodeTerminalCandidateProviderMessage) int {
	gateway.mu.Lock()
	gateway.requests++
	requestNumber := gateway.requests
	sessionID := gateway.sessionID
	requestChanged := gateway.requestCountChanged
	gateway.requestCountChanged = make(chan struct{})
	gateway.providerRequests = append(gateway.providerRequests, nativeOpenCodeTerminalCandidateProviderRequest{
		Number:    requestNumber,
		SessionID: sessionID,
		Model:     model,
		Stream:    stream,
		Body:      append([]byte(nil), body...),
		Messages:  append([]nativeOpenCodeTerminalCandidateProviderMessage(nil), messages...),
	})
	gateway.mu.Unlock()
	close(requestChanged)
	return requestNumber
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) recordProviderRequestDetails(requestNumber int, toolMessages int, toolCallID, toolContent string, timelineIndex uint64) {
	gateway.mu.Lock()
	if requestNumber > 0 && requestNumber <= len(gateway.providerRequests) {
		request := &gateway.providerRequests[requestNumber-1]
		request.ToolMessages = toolMessages
		request.ToolCallID = toolCallID
		request.ToolContent = toolContent
		request.TimelineIndex = timelineIndex
	}
	gateway.mu.Unlock()
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) recordProviderError() {
	gateway.mu.Lock()
	gateway.providerErrors++
	gateway.mu.Unlock()
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) recordProviderResponse(requestNumber int, kind string, detail string) {
	if gateway != nil && gateway.timeline != nil {
		gateway.timeline.Record(nativeOpenCodeTerminalCandidateTimelineEvent{Kind: kind, SessionID: gateway.boundSession(), Model: gateway.model, RequestNumber: requestNumber, Detail: detail})
	}
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) handle(response http.ResponseWriter, request *http.Request) {
	isChat := request.Method == http.MethodPost && request.URL.Path == "/v1/chat/completions" && request.URL.RawQuery == ""
	remoteHost, _, _ := net.SplitHostPort(request.RemoteAddr)
	if !isChat || remoteHost != "127.0.0.1" {
		gateway.recordError(fmt.Errorf("unexpected terminal candidate provider route: %s %s", request.Method, request.URL.RequestURI()))
		http.Error(response, "only numeric loopback chat completions are permitted", http.StatusNotFound)
		return
	}
	if request.Header.Get("Authorization") != "Bearer "+nativeOpenCodeTerminalCandidateAPIKey {
		gateway.recordError(errors.New("terminal candidate provider credential did not match test key"))
		http.Error(response, "unexpected provider credential", http.StatusForbidden)
		return
	}
	var envelope struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Messages []json.RawMessage `json:"messages"`
	}
	body, err := nativeOpenCodeTerminalCandidateReadGatewayBody(request, nativeOpenCodeTerminalCandidateMaxBodyBytes)
	if err != nil {
		gateway.recordError(fmt.Errorf("decode terminal candidate provider request: %w", err))
		http.Error(response, "invalid request", http.StatusRequestEntityTooLarge)
		return
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Model != gateway.model || !envelope.Stream {
		gateway.recordError(fmt.Errorf("terminal candidate provider model/stream mismatch model=%q stream=%t", envelope.Model, envelope.Stream))
		http.Error(response, "unexpected model or stream mode", http.StatusBadRequest)
		return
	}
	messages, err := nativeOpenCodeTerminalCandidateDecodeProviderMessages(envelope.Messages)
	if err != nil {
		gateway.recordError(fmt.Errorf("decode terminal candidate provider messages: %w", err))
		http.Error(response, "invalid messages", http.StatusBadRequest)
		return
	}
	toolMessages := 0
	toolCallID := ""
	toolContent := ""
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		toolMessages++
		if toolCallID == "" {
			toolCallID = message.ToolCallID
			toolContent = nativeOpenCodeTerminalCandidateJSONText(message.Content)
		}
	}
	hasToolResult := toolMessages > 0
	requestNumber := gateway.recordProviderRequest(body, envelope.Model, envelope.Stream, messages)
	requestEvent := nativeOpenCodeTerminalCandidateTimelineEvent{Kind: "provider.request", SessionID: gateway.boundSession(), Model: envelope.Model, RequestNumber: requestNumber, Detail: fmt.Sprintf("tool_messages=%d", toolMessages)}
	requestIndex := uint64(0)
	if gateway.timeline != nil {
		requestIndex = gateway.timeline.Record(requestEvent)
	}
	gateway.recordProviderRequestDetails(requestNumber, toolMessages, toolCallID, toolContent, requestIndex)
	responseKind := "provider.response.eof"
	responseDetail := "complete"
	defer func() {
		gateway.recordProviderResponse(requestNumber, responseKind, responseDetail)
	}()
	if requestNumber == 1 {
		close(gateway.firstRequestSeen)
		defer close(gateway.firstReturned)
	} else if gateway.scenario == nativeOpenCodeProviderError && requestNumber <= 3 {
		// The pinned provider wrapper retries one deterministic provider error.
		// Keep every retry bounded and return the same error without accepting
		// any new lifecycle work.
		if requestNumber == 3 {
			defer close(gateway.followUpReturned)
		}
	} else if gateway.scenario == nativeOpenCodeStepFailure && requestNumber <= 4 {
		if requestNumber == 2 {
			close(gateway.followUpSeen)
			close(gateway.followUpActive)
		}
		if requestNumber == 4 {
			defer close(gateway.followUpReturned)
		}
	} else if requestNumber == 2 {
		close(gateway.followUpSeen)
		close(gateway.followUpActive)
		defer close(gateway.followUpReturned)
	} else {
		gateway.recordError(fmt.Errorf("terminal candidate provider made unexpected request number %d", requestNumber))
		http.Error(response, "unexpected retry", http.StatusConflict)
		return
	}

	switch gateway.scenario {
	case nativeOpenCodeTerminalStop:
		if requestNumber != 1 {
			gateway.recordError(errors.New("terminal stop made a follow-up request"))
			return
		}
		nativeOpenCodeTerminalCandidateWriteChatResponse(response, gateway.model, nativeOpenCodeTerminalCandidateResultJSON, "stop", nil)
	case nativeOpenCodeProviderError:
		responseKind = "provider.response.error"
		gateway.writeProviderError(response, "provider error candidate")
	case nativeOpenCodeStepContinuation:
		if requestNumber == 1 && !hasToolResult {
			nativeOpenCodeTerminalCandidateWriteChatResponse(response, gateway.model, "", "tool_calls", nativeOpenCodeTerminalCandidateToolArguments())
			return
		}
		if requestNumber == 2 && hasToolResult {
			nativeOpenCodeTerminalCandidateWriteChatResponse(response, gateway.model, nativeOpenCodeTerminalCandidateResultJSON, "stop", nil)
			return
		}
		gateway.recordError(errors.New("continuation candidate did not observe expected tool-result ordering"))
	case nativeOpenCodeStepFailure:
		if requestNumber == 1 && !hasToolResult {
			nativeOpenCodeTerminalCandidateWriteChatResponse(response, gateway.model, "", "tool_calls", nativeOpenCodeTerminalCandidateToolArguments())
			return
		}
		if (requestNumber == 2 || requestNumber == 3 || requestNumber == 4) && hasToolResult {
			responseKind = "provider.response.error"
			gateway.writeProviderError(response, "post-Step.Ended provider failure candidate")
			return
		}
		gateway.recordError(errors.New("failure candidate did not observe expected tool-result ordering"))
	case nativeOpenCodeInterrupt:
		if requestNumber == 1 && !hasToolResult {
			nativeOpenCodeTerminalCandidateWriteChatResponse(response, gateway.model, "", "tool_calls", nativeOpenCodeTerminalCandidateToolArguments())
			return
		}
		if requestNumber == 2 && hasToolResult {
			responseKind = "provider.response.cancelled"
			responseDetail = "provider request context cancelled"
			<-request.Context().Done()
			select {
			case <-gateway.followUpCancelled:
			default:
				close(gateway.followUpCancelled)
			}
			return
		}
		gateway.recordError(errors.New("interrupt candidate did not observe expected tool-result ordering"))
	case nativeOpenCodeTruncatedProvider:
		if requestNumber != 1 {
			gateway.recordError(errors.New("truncated provider candidate made a follow-up request"))
			return
		}
		responseKind = "provider.response.truncated_eof"
		responseDetail = "truncated provider SSE body"
		response.Header().Set("Connection", "close")
		nativeOpenCodeTerminalCandidateWriteTruncatedChatResponse(response, gateway.model)
	default:
		gateway.recordError(fmt.Errorf("unsupported terminal candidate scenario %q", gateway.scenario))
	}
}

func nativeOpenCodeTerminalCandidateDecodeProviderMessages(rawMessages []json.RawMessage) ([]nativeOpenCodeTerminalCandidateProviderMessage, error) {
	messages := make([]nativeOpenCodeTerminalCandidateProviderMessage, 0, len(rawMessages))
	for _, raw := range rawMessages {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		role := nativeOpenCodeTerminalCandidateString(object, "role")
		if role == "" {
			return nil, errors.New("provider message omitted role")
		}
		message := nativeOpenCodeTerminalCandidateProviderMessage{
			Role:    role,
			Raw:     append(json.RawMessage(nil), raw...),
			Content: append(json.RawMessage(nil), object["content"]...),
		}
		message.ToolCallID = nativeOpenCodeTerminalCandidateString(object, "tool_call_id", "toolCallId")
		messages = append(messages, message)
	}
	return messages, nil
}

func nativeOpenCodeTerminalCandidateJSONText(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	parts := make([]string, 0, 8)
	var walk func(any)
	walk = func(candidate any) {
		switch typed := candidate.(type) {
		case string:
			parts = append(parts, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	return strings.Join(parts, "\n")
}

func nativeOpenCodeTerminalCandidateRawContainsExactText(raw json.RawMessage, expected string) bool {
	if strings.TrimSpace(expected) == "" || len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(candidate any) bool {
		switch typed := candidate.(type) {
		case string:
			return typed == expected
		case []any:
			for _, item := range typed {
				if walk(item) {
					return true
				}
			}
		case map[string]any:
			for _, item := range typed {
				if walk(item) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) writeProviderError(response http.ResponseWriter, message string) {
	gateway.recordProviderError()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(response, `{"error":{"type":"server_error","message":"`+message+`"}}`)
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) ProviderErrorCount() int {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return gateway.providerErrors
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) WaitForExpectedCompletion(ctx context.Context, expected int) error {
	if gateway == nil {
		return errors.New("terminal candidate gateway is nil")
	}
	if expected <= 0 {
		return errors.New("terminal candidate gateway expected provider request count must be positive")
	}
	if err := gateway.WaitForRequestCount(ctx, expected); err != nil {
		return err
	}
	if expected == 1 {
		select {
		case <-gateway.firstReturned:
		case <-ctx.Done():
			return ctx.Err()
		}
	} else {
		select {
		case <-gateway.followUpReturned:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return gateway.Err()
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) WaitForRequestCount(ctx context.Context, expected int) error {
	if gateway == nil {
		return errors.New("terminal candidate gateway is nil")
	}
	for {
		gateway.mu.Lock()
		count := gateway.requests
		err := gateway.err
		changed := gateway.requestCountChanged
		gateway.mu.Unlock()
		if err != nil {
			return err
		}
		if count >= expected {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) Err() error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return gateway.err
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) recordError(err error) {
	if err == nil {
		return
	}
	gateway.mu.Lock()
	if gateway.err == nil {
		gateway.err = err
	}
	gateway.mu.Unlock()
}

func (gateway *nativeOpenCodeTerminalCandidateGateway) Close() {
	if gateway == nil || gateway.server == nil {
		return
	}
	gateway.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), nativeOpenCodeTerminalCandidateCloseTimeout)
		defer cancel()
		if err := gateway.server.Shutdown(ctx); err != nil {
			_ = gateway.server.Close()
		}
	})
}

func nativeOpenCodeTerminalCandidateToolArguments() []byte {
	arguments, _ := json.Marshal(map[string]string{"path": nativeOpenCodeTerminalCandidateArtifact, "content": nativeOpenCodeTerminalCandidateContent})
	return arguments
}

const nativeOpenCodeTerminalCandidateResultJSON = `{"schema_version":"symmetry.task_result.v1","result_id":"00000000-0000-4000-8000-000000000001","kind":"progress","summary":"candidate witness response","subject":{"resource_id":"00000000-0000-4000-8000-000000000002","commit":"0000000000000000000000000000000000000000","tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"subject_hash":"sha256:0000000000000000000000000000000000000000000000000000000000000000","evidence_refs":[],"blocker":null,"proposed_next_action":null,"proposal":null,"reason":null,"diagnostics":[]}`

func nativeOpenCodeTerminalCandidateWriteChatResponse(response http.ResponseWriter, model, content, finishReason string, toolArguments []byte) {
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.WriteHeader(http.StatusOK)
	flusher, _ := response.(http.Flusher)
	delta := map[string]any{"role": "assistant"}
	if toolArguments != nil {
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "call_terminal_candidate", "type": "function", "function": map[string]any{"name": "write", "arguments": string(toolArguments)}}}
	} else {
		delta["content"] = content
	}
	chunk := map[string]any{"id": "chatcmpl-terminal-candidate", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}}
	encoded, _ := json.Marshal(chunk)
	_, _ = fmt.Fprintf(response, "data: %s\n\n", encoded)
	finish := map[string]any{"id": "chatcmpl-terminal-candidate", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}}}
	encoded, _ = json.Marshal(finish)
	_, _ = fmt.Fprintf(response, "data: %s\n\ndata: [DONE]\n\n", encoded)
	if flusher != nil {
		flusher.Flush()
	}
}

func nativeOpenCodeTerminalCandidateWriteTruncatedChatResponse(response http.ResponseWriter, model string) {
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.WriteHeader(http.StatusOK)
	chunk := map[string]any{"id": "chatcmpl-terminal-candidate", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "{\"schema_version\":\"symmetry.task_result.v1\""}, "finish_reason": nil}}}
	encoded, _ := json.Marshal(chunk)
	_, _ = fmt.Fprintf(response, "data: %s\n\n", encoded)
	if flusher, ok := response.(http.Flusher); ok {
		flusher.Flush()
	}
	if hijacker, ok := response.(http.Hijacker); ok {
		connection, _, err := hijacker.Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}
}

func nativeOpenCodeTerminalCandidateConfig(baseURL, model string) string {
	config := map[string]any{
		"$schema":           "https://opencode.ai/config.json",
		"model":             nativeOpenCodeTerminalCandidateProvider + "/" + model,
		"small_model":       nativeOpenCodeTerminalCandidateProvider + "/" + model,
		"enabled_providers": []string{nativeOpenCodeTerminalCandidateProvider},
		"provider": map[string]any{
			nativeOpenCodeTerminalCandidateProvider: map[string]any{
				"name": nativeOpenCodeTerminalCandidateProvider,
				"npm":  "@ai-sdk/openai-compatible",
				"options": map[string]any{
					"baseURL": baseURL,
					"apiKey":  nativeOpenCodeTerminalCandidateAPIKey,
				},
				"models": map[string]any{
					model: map[string]any{
						"name": model,
						"provider": map[string]any{
							"npm": "@ai-sdk/openai-compatible",
							"api": baseURL,
						},
						"limit":       map[string]any{"context": 32768, "output": 4096},
						"modalities":  map[string]any{"input": []string{"text"}, "output": []string{"text"}},
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
			"todowrite":          "deny",
			"webfetch":           "deny",
			"external_directory": "deny",
			"edit": map[string]string{
				"*":                                     "deny",
				nativeOpenCodeTerminalCandidateArtifact: "allow",
			},
		},
	}
	encoded, _ := json.Marshal(config)
	return string(encoded)
}

func nativeOpenCodeTerminalCandidateVerifyExecutableSHA256(executable, expected string) error {
	const prefix = "sha256:"
	if expected != strings.TrimSpace(expected) || !strings.HasPrefix(expected, prefix) || len(expected) != len(prefix)+sha256.Size*2 {
		return errors.New("expected executable digest must be sha256:<64 lowercase hex characters>")
	}
	digestText := expected[len(prefix):]
	if strings.ToLower(digestText) != digestText {
		return errors.New("expected executable digest must be lowercase hexadecimal")
	}
	if _, err := hex.DecodeString(digestText); err != nil {
		return fmt.Errorf("decode expected executable digest: %w", err)
	}
	file, err := os.Open(executable)
	if err != nil {
		return fmt.Errorf("open executable: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("executable must be a regular file")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("hash executable: %w", err)
	}
	actual := prefix + hex.EncodeToString(digest.Sum(nil))
	if actual != expected {
		return fmt.Errorf("executable SHA-256 = %s, want %s", actual, expected)
	}
	return nil
}

func nativeOpenCodeTerminalCandidateCloseFailedStart(session harness.Session, timeout time.Duration) error {
	return nativeOpenCodeTerminalCandidateCloseAndWait(session, timeout)
}

func nativeOpenCodeTerminalCandidateCloseAndWait(session harness.Session, timeout time.Duration) error {
	if session == nil {
		return nil
	}
	if timeout <= 0 {
		return errors.New("native OpenCode terminal candidate cleanup timeout must be positive")
	}
	deadline := time.Now().Add(timeout)
	var closeErr error
	for attempt := 0; attempt < 2; attempt++ {
		cleanupContext, cleanupCancel := context.WithDeadline(context.Background(), deadline)
		closeErr = session.Close(cleanupContext)
		cleanupCancel()
		if closeErr == nil || time.Until(deadline) <= 0 {
			break
		}
	}
	waitContext, waitCancel := context.WithDeadline(context.Background(), deadline)
	result, waitErr := session.Wait(waitContext)
	waitCancel()
	if waitErr != nil {
		return errors.Join(closeErr, waitErr)
	}
	if result.Kind != harness.ResultUnknown || result.Usage.State != harness.UsageUnknown ||
		!result.Process.Terminated || result.Process.SinkError != nil || result.Process.OutputError != nil ||
		result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		return errors.Join(closeErr, fmt.Errorf("native OpenCode terminal candidate cleanup result = %+v; want unknown with clean terminated process", result))
	}
	return closeErr
}

func nativeOpenCodeTerminalCandidateEnvironmentValue(environment []string, name string) string {
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found && key == name {
			return value
		}
	}
	return ""
}

func nativeOpenCodeTerminalCandidateReadGatewayBody(request *http.Request, limit int64) ([]byte, error) {
	if request == nil || request.Body == nil {
		return nil, errors.New("native OpenCode terminal candidate request body is unavailable")
	}
	if limit <= 0 {
		return nil, errors.New("native OpenCode terminal candidate request body limit must be positive")
	}
	defer request.Body.Close()
	if request.ContentLength > limit {
		return nil, fmt.Errorf("declared request length %d exceeds %d", request.ContentLength, limit)
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("request body length %d exceeds %d", len(body), limit)
	}
	return body, nil
}

func nativeOpenCodeTerminalCandidateRedactIdentity(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:4] + "..." + value[len(value)-4:]
}
