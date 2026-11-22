package omo

import (
	"context"
	"encoding/json"
	"errors"
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

// testHangGuard bounds how long a test waits for a scripted, in-process double
// to publish an observable. It only turns a hung harness into a failure; it is
// never a statement about how fast the adapter should be.
const testHangGuard = 30 * time.Second

type testProcess struct {
	sink             execution.Sink
	onWrite          func(Request)
	done             chan struct{}
	once             sync.Once
	mutex            sync.Mutex
	result           execution.Result
	terminations     int
	terminateError   error
	terminateStarted chan struct{}
	terminateGate    <-chan struct{}
	onCloseInput     func() error
}

func newTestProcess() *testProcess                         { return &testProcess{done: make(chan struct{})} }
func (process *testProcess) ProcessDetails() (int, string) { return 42, "recorded-process-42" }
func (process *testProcess) WriteInputContext(_ context.Context, data []byte) error {
	var request Request
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}
	if process.onWrite != nil {
		process.onWrite(request)
	}
	return nil
}
func (process *testProcess) Wait() execution.Result {
	<-process.done
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.result
}
func (process *testProcess) finish(result execution.Result) {
	process.once.Do(func() { process.mutex.Lock(); process.result = result; process.mutex.Unlock(); close(process.done) })
}
func (process *testProcess) CloseInput() error {
	if process.onCloseInput != nil {
		return process.onCloseInput()
	}
	return nil
}
func (process *testProcess) Terminate(ctx context.Context, _ time.Duration) error {
	process.mutex.Lock()
	process.terminations++
	err, started, gate := process.terminateError, process.terminateStarted, process.terminateGate
	process.mutex.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	process.finish(execution.Result{PID: 42, ExitCode: -1, Terminated: true, WaitError: errors.New("terminated")})
	return nil
}
func (process *testProcess) emit(t *testing.T, frame any) error {
	t.Helper()
	encoded, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	return process.sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Data: append(encoded, '\n')})
}
func (process *testProcess) reply(t *testing.T, request Request, data any) error {
	t.Helper()
	return process.emit(t, map[string]any{"type": "response", "id": request.ID, "command": request.Type, "success": true, "data": data})
}

type testSink struct {
	mutex    sync.Mutex
	events   []harness.Event
	failKind harness.EventKind
}

var errTestJournalUnavailable = errors.New("journal unavailable")

func (sink *testSink) Handle(_ context.Context, event harness.Event) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if event.Kind == sink.failKind {
		return errTestJournalUnavailable
	}
	sink.events = append(sink.events, event)
	return nil
}
func (sink *testSink) has(kind harness.EventKind) bool {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	for _, event := range sink.events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func candidateRequest(t *testing.T, process *testProcess) harness.StartRequest {
	t.Helper()
	return harness.StartRequest{AdmissionID: "durable-admission-1", Workspace: t.TempDir(), Invocation: execution.Invocation{Args: []string{"--session-dir", t.TempDir()}}, StartProcess: func(_ context.Context, invocation execution.Invocation, sink execution.Sink) (harness.NativeProcess, error) {
		process.sink = sink
		if invocation.PersistProcess != nil {
			if err := invocation.PersistProcess(42, "recorded-process-42"); err != nil {
				return process, err
			}
		}
		return process, nil
	}}
}

func openCandidate(t *testing.T, process *testProcess, request harness.StartRequest, sink harness.EventSink) *nativeSession {
	t.Helper()
	started, err := NewAdapter("recorded-omo").start(context.Background(), request, sink)
	if err != nil {
		t.Fatal(err)
	}
	session := started.(*nativeSession)
	t.Cleanup(func() {
		process.finish(execution.Result{PID: 42, ExitCode: 0})
		cleanup, cancel := context.WithTimeout(context.Background(), testHangGuard)
		defer cancel()
		_, _ = session.Wait(cleanup)
		_ = session.Close(cleanup)
	})
	if _, err := session.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	return session
}

func installOpenResponses(t *testing.T, process *testProcess, filename, workspace string, prompt func(Request)) {
	t.Helper()
	process.onWrite = func(request Request) {
		switch request.Type {
		case CommandProtocol:
			// Open owns the assertion on consumer acceptance, including replies
			// deliberately rejected by the negative lifecycle tests.
			_ = process.reply(t, request, protocolData())
		case CommandGetState:
			_ = process.reply(t, request, stateData(filename))
		case CommandPrompt:
			if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
				writeRetainedHeader(t, filename, "native-1", workspace)
			} else if err != nil {
				t.Fatal(err)
			}
			if prompt != nil {
				prompt(request)
			}
		}
	}
}

func TestCandidateStagesIdentityAndWaitsForPhysicalFinalBarrier(t *testing.T) {
	process, sink := newTestProcess(), &testSink{}
	request := candidateRequest(t, process)
	filename := filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl")
	persisted := false
	request.PersistProcess = func(int, string) error { persisted = true; return nil }
	installOpenResponses(t, process, filename, request.Workspace, func(prompt Request) {
		if err := process.reply(t, prompt, promptData(prompt)); err != nil {
			t.Fatal(err)
		}
		for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
			if err := process.emit(t, frame); err != nil {
				t.Fatal(err)
			}
		}
	})
	session := openCandidate(t, process, request, sink)
	if !persisted || sink.has(harness.EventSessionStarted) {
		t.Fatal("Open crossed a durable launch/handle barrier")
	}
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Change the requested artifact", Context: json.RawMessage(`{"subject":"canonical"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := session.Wait(bounded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait exposed result before stop: %v", err)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultSucceeded || result.Semantic == nil || result.Usage.State != harness.UsageUnknown {
		t.Fatalf("final = %+v, %v", result, err)
	}
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	for _, event := range sink.events {
		if event.Kind == harness.EventNativeFrame && (len(event.Payload) != 0 || len(event.Data) != 0 || event.Message != "") {
			t.Fatal("native payload escaped through frame metadata")
		}
	}
}

func TestCandidateBindsSemanticTerminalToNativeSessionHeader(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "retained"}[retained], func(t *testing.T) {
			for _, header := range []string{"valid", "missing file", "missing header", "wrong version", "wrong session", "wrong workspace"} {
				t.Run(header, func(t *testing.T) {
					process, sink := newTestProcess(), &testSink{}
					request := candidateRequest(t, process)
					filename := filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl")
					if retained {
						writeRetainedHeader(t, filename, "native-1", request.Workspace)
						request.Resume = &harness.ResumeHandle{LocalHandleID: "retained-local", NativeSessionID: "native-1", NativeSessionFilename: filename, WorkspaceFingerprint: "workspace-fingerprint", NativeVersion: TestedVersion}
					}
					var prompt Request
					installOpenResponses(t, process, filename, request.Workspace, func(value Request) {
						prompt = value
						if err := process.reply(t, value, promptData(value)); err != nil {
							t.Fatal(err)
						}
					})
					session := openCandidate(t, process, request, sink)
					if !retained {
						if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("fresh Open required or created a native journal: %v", err)
						}
					}
					if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Change artifact", Context: json.RawMessage(`{}`)}); err != nil {
						t.Fatal(err)
					}
					switch header {
					case "missing file":
						if err := os.Remove(filename); err != nil {
							t.Fatal(err)
						}
					case "missing header":
						if err := os.WriteFile(filename, nil, 0600); err != nil {
							t.Fatal(err)
						}
					case "wrong version":
						encoded, err := json.Marshal(map[string]any{"type": "session", "version": 2, "id": "native-1", "cwd": request.Workspace})
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filename, append(encoded, '\n'), 0600); err != nil {
							t.Fatal(err)
						}
					case "wrong session":
						writeRetainedHeader(t, filename, "native-other", request.Workspace)
					case "wrong workspace":
						writeRetainedHeader(t, filename, "native-1", t.TempDir())
					}
					var terminalErr error
					for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
						if terminalErr = process.emit(t, frame); terminalErr != nil {
							break
						}
					}
					turnErr := session.WaitTurn(context.Background())
					process.finish(execution.Result{PID: 42, ExitCode: 0})
					result, waitErr := session.Wait(context.Background())
					if header == "valid" {
						if terminalErr != nil || turnErr != nil || waitErr != nil || result.Kind != harness.ResultSucceeded || result.Semantic == nil || !sink.has(harness.EventTaskResult) {
							t.Fatalf("valid native header lost semantic success: %+v, terminal=%v, turn=%v, wait=%v", result, terminalErr, turnErr, waitErr)
						}
						return
					}
					if terminalErr == nil || turnErr == nil || waitErr != nil || result.Kind != harness.ResultFailed || result.Semantic != nil || result.Reason == nil || *result.Reason != protocol.TaskResultReasonUnknownOutcome || sink.has(harness.EventTaskResult) {
						t.Fatalf("unverified native header exposed semantic success: %+v, terminal=%v, turn=%v, wait=%v", result, terminalErr, turnErr, waitErr)
					}
					if header == "missing file" && !errors.Is(turnErr, os.ErrNotExist) {
						t.Fatalf("missing native header lost its filesystem error: turn=%v, wait=%v", turnErr, waitErr)
					}
				})
			}
		})
	}
}

func TestCandidateRejectsSettledProseAndProcessFailureAfterOutput(t *testing.T) {
	for _, mode := range []string{"prose", "process failure"} {
		t.Run(mode, func(t *testing.T) {
			process, sink := newTestProcess(), &testSink{}
			request := candidateRequest(t, process)
			installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(prompt Request) {
				if err := process.reply(t, prompt, promptData(prompt)); err != nil {
					t.Fatal(err)
				}
				message := assistantMessage(t)
				if mode == "prose" {
					message["content"] = []any{map[string]any{"type": "text", "text": "Done. The result is {}"}}
				}
				for _, frame := range completionEvents(t, prompt, message) {
					if err := process.emit(t, frame); err != nil {
						t.Fatal(err)
					}
				}
			})
			session := openCandidate(t, process, request, sink)
			if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Change artifact", Context: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			turnErr := session.WaitTurn(context.Background())
			if mode == "prose" && (turnErr == nil || sink.has(harness.EventTaskResult)) {
				t.Fatal("settled prose became semantic success")
			}
			if mode == "process failure" && turnErr != nil {
				t.Fatal(turnErr)
			}
			process.finish(execution.Result{PID: 42, ExitCode: 17, WaitError: errors.New("native failure")})
			result, err := session.Wait(context.Background())
			if err != nil || result.Kind != harness.ResultFailed || result.Semantic != nil || result.Process.ExitCode != 17 || result.Process.WaitError == nil {
				t.Fatalf("failed process retained success: %+v, %v", result, err)
			}
		})
	}
}

func TestCancellationRequiresIdleDrainNotOnlyAcknowledgement(t *testing.T) {
	for _, drained := range []bool{false, true} {
		t.Run(map[bool]string{false: "only ack", true: "drained"}[drained], func(t *testing.T) {
			process, sink := newTestProcess(), &testSink{}
			request := candidateRequest(t, process)
			filename := filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl")
			var prompt Request
			installOpenResponses(t, process, filename, request.Workspace, func(value Request) {
				prompt = value
				_ = process.reply(t, value, promptData(value))
				_ = process.emit(t, map[string]any{"type": "agent_start"})
			})
			original := process.onWrite
			process.onWrite = func(command Request) {
				if command.Type != CommandAbort {
					original(command)
					return
				}
				if command.EffectID != "cancel-command-1" || !command.AwaitIdle || !command.ClearQueue || command.GraceMS != 5000 {
					t.Fatal("cancellation did not send its bounded native effect")
				}
				if drained {
					end := identifiedEvent("agent_end", prompt)
					end["messages"], end["willRetry"], end["aborted"] = []any{}, false, true
					for _, frame := range []any{end, map[string]any{"type": "agent_settled"}, map[string]any{"type": "agent_idle"}} {
						if err := process.emit(t, frame); err != nil {
							t.Fatal(err)
						}
					}
				}
				state := "applied"
				if drained {
					state = "done"
				}
				_ = process.reply(t, command, map[string]any{"effectId": command.EffectID, "state": state, "stopped": drained, "idle": drained, "effect": map[string]any{"effectId": command.EffectID, "kind": "abort", "state": state, "durableSessionId": "native-1"}})
			}
			session := openCandidate(t, process, request, sink)
			if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Run until cancelled", Context: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-command-1", Kind: harness.ControlCancel})
			if !drained {
				if err == nil || receipt.Outcome == harness.ControlApplied {
					t.Fatalf("ACK became applied cancellation: %+v, %v", receipt, err)
				}
				return
			}
			if err != nil || receipt.Outcome != harness.ControlApplied {
				t.Fatalf("drained cancellation = %+v, %v", receipt, err)
			}
			if err := session.WaitTurn(context.Background()); err != nil {
				t.Fatal(err)
			}
			process.finish(execution.Result{PID: 42, ExitCode: 0})
			result, err := session.Wait(context.Background())
			if err != nil || result.Kind != harness.ResultCancelled || sink.has(harness.EventTaskResult) {
				t.Fatalf("cancel result = %+v, %v", result, err)
			}
		})
	}
}

func TestLostPromptACKUsesEffectReadbackWithoutNewPrompt(t *testing.T) {
	process, sink := newTestProcess(), &testSink{}
	request := candidateRequest(t, process)
	var prompt Request
	prompts := 0
	bounded, cancel := context.WithCancel(context.Background())
	defer cancel()
	installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(value Request) { prompts++; prompt = value; cancel() })
	original := process.onWrite
	process.onWrite = func(command Request) {
		if command.Type != CommandEffectStatus {
			original(command)
			return
		}
		_ = process.reply(t, command, map[string]any{"found": true, "effect": map[string]any{"effectId": prompt.ClientMessageID, "kind": "input", "state": "applied", "durableSessionId": "native-1"}})
		for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
			if err := process.emit(t, frame); err != nil {
				t.Fatal(err)
			}
		}
	}
	session := openCandidate(t, process, request, sink)
	turn := harness.TurnRequest{Goal: "Change once", Context: json.RawMessage(`{}`)}
	if err := session.StartTurn(bounded, turn); !errors.Is(err, ErrUnknownOutcome) {
		t.Fatalf("lost ACK = %v", err)
	}
	if err := session.StartTurn(context.Background(), turn); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prompts != 1 {
		t.Fatalf("unknown ACK outcome started %d native prompts", prompts)
	}
}

func TestSinkFailureAndPersistenceFailureRetainCleanupOwner(t *testing.T) {
	for _, mode := range []string{"native frame sink", "semantic sink", "process persistence"} {
		t.Run(mode, func(t *testing.T) {
			process, sink := newTestProcess(), &testSink{}
			request := candidateRequest(t, process)
			filename := filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl")
			installOpenResponses(t, process, filename, request.Workspace, func(prompt Request) {
				_ = process.reply(t, prompt, promptData(prompt))
				for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
					if err := process.emit(t, frame); err != nil {
						break
					}
				}
			})
			if mode == "native frame sink" {
				sink.failKind = harness.EventNativeFrame
			}
			if mode == "semantic sink" {
				sink.failKind = harness.EventTaskResult
			}
			if mode == "process persistence" {
				request.PersistProcess = func(int, string) error { return errors.New("fsync failed") }
			}
			started, startErr := NewAdapter("recorded-omo").start(context.Background(), request, sink)
			if started == nil {
				t.Fatal("post-launch error lost cleanup owner")
			}
			session := started.(*nativeSession)
			if mode == "process persistence" {
				if startErr == nil {
					t.Fatal("failed persistence acknowledged launch")
				}
			} else {
				if startErr != nil {
					t.Fatal(startErr)
				}
				handle, openErr := session.Open(context.Background())
				if mode == "native frame sink" {
					if !errors.Is(openErr, errTestJournalUnavailable) {
						t.Fatalf("Open did not propagate the journal failure: %v", openErr)
					}
					if handle != (harness.NativeSessionHandle{}) || sink.has(harness.EventSessionStarted) {
						t.Fatal("sink failure exposed a native handle or started event")
					}
				}
				if mode == "semantic sink" {
					if openErr != nil {
						t.Fatal(openErr)
					}
					_ = session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Change artifact", Context: json.RawMessage(`{}`)})
					if err := session.WaitTurn(context.Background()); !errors.Is(err, errTestJournalUnavailable) {
						t.Fatalf("WaitTurn did not propagate the result journal failure: %v", err)
					}
				}
			}
			if err := session.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			result, err := session.Wait(context.Background())
			if err != nil || result.Kind != harness.ResultFailed || result.Semantic != nil {
				t.Fatalf("sink/persistence failure became success: %+v, %v", result, err)
			}
		})
	}
}

func TestCloseSerializesCleanupAndRetriesFailedAttempt(t *testing.T) {
	process, sink := newTestProcess(), &testSink{}
	request := candidateRequest(t, process)
	installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, nil)
	session := openCandidate(t, process, request, sink)
	process.mutex.Lock()
	process.terminateError = errors.New("temporary containment failure")
	process.mutex.Unlock()
	if err := session.Close(context.Background()); err == nil {
		t.Fatal("failed cleanup became permanent success")
	}
	process.mutex.Lock()
	process.terminateError = nil
	process.mutex.Unlock()
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	process.mutex.Lock()
	count := process.terminations
	process.mutex.Unlock()
	if count != 2 {
		t.Fatalf("cleanup attempts = %d, want one retry", count)
	}
}

func TestCloseDrainsNativeOutputBeforeCancellingEventLifetime(t *testing.T) {
	for _, failLate := range []bool{false, true} {
		t.Run(map[bool]string{false: "drained diagnostic", true: "late sink failure"}[failLate], func(t *testing.T) {
			process := newTestProcess()
			request := candidateRequest(t, process)
			installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(prompt Request) {
				_ = process.reply(t, prompt, promptData(prompt))
				for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
					if err := process.emit(t, frame); err != nil {
						t.Fatal(err)
					}
				}
			})
			diagnostic := make(chan error, 1)
			sink := harness.EventSinkFunc(func(ctx context.Context, event harness.Event) error {
				if event.Kind != harness.EventDiagnostic {
					return nil
				}
				err := ctx.Err()
				diagnostic <- err
				if err == nil && failLate {
					err = errTestJournalUnavailable
				}
				return err
			})
			session := openCandidate(t, process, request, sink)
			if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Change artifact", Context: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			if err := session.WaitTurn(context.Background()); err != nil {
				t.Fatal(err)
			}
			inputClosed := false
			process.onCloseInput = func() error {
				inputClosed = true
				go func() {
					err := process.sink.Handle(context.Background(), execution.Event{Stream: execution.Stderr, Data: []byte("final diagnostic")})
					process.finish(execution.Result{PID: 42, ExitCode: 0, SinkError: err})
				}()
				return nil
			}
			if err := session.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !inputClosed {
				t.Fatal("Close terminated the process before attempting native EOF drain")
			}
			select {
			case err := <-diagnostic:
				if err != nil {
					t.Fatalf("final diagnostic received a cancelled event lifetime: %v", err)
				}
			default:
				t.Fatal("Close returned before the final native diagnostic reached the consumer")
			}
			result, err := session.Wait(context.Background())
			if failLate {
				if err != nil || !errors.Is(result.Process.SinkError, errTestJournalUnavailable) || result.Kind != harness.ResultFailed || result.Semantic != nil {
					t.Fatalf("late journal failure retained semantic success: %+v, %v", result, err)
				}
			} else if err != nil || result.Kind != harness.ResultSucceeded || result.Semantic == nil || !result.Process.Success() {
				t.Fatalf("drained close lost the persisted result: %+v, %v", result, err)
			}
			if session.context.Err() == nil {
				t.Fatal("Close did not release its event lifetime after drain")
			}
		})
	}
}

func TestCloseEOFBackpressureFallsBackOnCleanupCancellation(t *testing.T) {
	process := newTestProcess()
	request := candidateRequest(t, process)
	installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(prompt Request) {
		_ = process.reply(t, prompt, promptData(prompt))
		for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
			if err := process.emit(t, frame); err != nil {
				t.Fatal(err)
			}
		}
	})
	blocked, released := make(chan struct{}), make(chan struct{})
	sink := harness.EventSinkFunc(func(ctx context.Context, event harness.Event) error {
		if event.Kind != harness.EventDiagnostic {
			return nil
		}
		close(blocked)
		<-ctx.Done()
		close(released)
		return ctx.Err()
	})
	session := openCandidate(t, process, request, sink)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Change artifact", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	process.terminateGate = process.done
	process.onCloseInput = func() error {
		go func() {
			_ = process.sink.Handle(context.Background(), execution.Event{Stream: execution.Stderr, Data: []byte("blocked final diagnostic")})
			process.finish(execution.Result{PID: 42, ExitCode: -1, Terminated: true, WaitError: errors.New("terminated"), OutputTruncated: true})
		}()
		return nil
	}
	cleanup, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- session.closeOnce(cleanup) }()
	select {
	case <-blocked:
	case <-time.After(testHangGuard):
		t.Fatal("EOF did not reach the consumer before termination")
	}
	cancel()
	select {
	case <-released:
	case <-time.After(testHangGuard):
		t.Fatal("cleanup cancellation did not release native sink backpressure")
	}
	select {
	case err := <-closed:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Close fallback error = %v", err)
		}
	case <-time.After(testHangGuard):
		t.Fatal("cleanup cancellation left Close blocked")
	}
	bounded, cancelWait := context.WithTimeout(context.Background(), testHangGuard)
	defer cancelWait()
	result, err := session.Wait(bounded)
	if err != nil || result.Kind != harness.ResultFailed || result.Semantic != nil || !result.Process.OutputTruncated {
		t.Fatalf("cancelled drain retained semantic success: %+v, %v", result, err)
	}
	process.mutex.Lock()
	count := process.terminations
	process.mutex.Unlock()
	if count != 1 {
		t.Fatalf("bounded EOF fallback cleanup attempts = %d, want 1", count)
	}
}

func TestConcurrentCloseSharesOnePhysicalCleanup(t *testing.T) {
	process, sink := newTestProcess(), &testSink{}
	request := candidateRequest(t, process)
	installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, nil)
	session := openCandidate(t, process, request, sink)
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	process.mutex.Lock()
	process.terminateGate, process.terminateStarted = gate, started
	process.mutex.Unlock()
	results := make(chan error, 8)
	for range 8 {
		go func() { results <- session.Close(context.Background()) }()
	}
	<-started
	close(gate)
	for range 8 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	process.mutex.Lock()
	count := process.terminations
	process.mutex.Unlock()
	if count != 1 {
		t.Fatalf("concurrent cleanup stopped the same process %d times", count)
	}
}

func TestPreReadyOutputOverflowCannotAcknowledgeLaunch(t *testing.T) {
	process, sink := newTestProcess(), &testSink{}
	request := candidateRequest(t, process)
	request.StartProcess = func(_ context.Context, _ execution.Invocation, output execution.Sink) (harness.NativeProcess, error) {
		process.sink = output
		for range 65 {
			if err := process.emit(t, map[string]any{"type": "startup_diagnostic"}); err != nil {
				break
			}
		}
		if sink.has(harness.EventNativeFrame) {
			t.Fatal("native output was exposed before process persistence returned")
		}
		return process, nil
	}
	started, err := NewAdapter().start(context.Background(), request, sink)
	if started == nil || err == nil {
		t.Fatalf("overflowed launch = %v, %v", started, err)
	}
	session := started.(*nativeSession)
	if _, err := session.Open(context.Background()); err == nil {
		t.Fatal("overflowed transport exposed an Open handle")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, err := session.Wait(context.Background()); err != nil || result.Kind != harness.ResultFailed {
		t.Fatalf("overflow became success: %+v, %v", result, err)
	}
}

func TestAbsentDurableEffectAllowsOnlySameInputIdentityReplay(t *testing.T) {
	process, sink := newTestProcess(), &testSink{}
	request := candidateRequest(t, process)
	firstContext, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	var first Request
	prompts := 0
	installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(prompt Request) {
		prompts++
		if prompts == 1 {
			first = prompt
			cancelFirst()
			return
		}
		if prompt.ID == first.ID || prompt.ClientMessageID != first.ClientMessageID || prompt.ClientTurnID != first.ClientTurnID || prompt.Message != first.Message {
			t.Fatal("replay changed durable input identity or payload")
		}
		if err := process.reply(t, prompt, promptData(prompt)); err != nil {
			t.Fatal(err)
		}
		for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
			if err := process.emit(t, frame); err != nil {
				t.Fatal(err)
			}
		}
	})
	original := process.onWrite
	process.onWrite = func(command Request) {
		if command.Type != CommandEffectStatus {
			original(command)
			return
		}
		if err := process.reply(t, command, map[string]any{"found": false, "searched": "complete"}); err != nil {
			t.Fatal(err)
		}
	}
	session := openCandidate(t, process, request, sink)
	turn := harness.TurnRequest{Goal: "Change the artifact only once", Context: json.RawMessage(`{}`)}
	if err := session.StartTurn(firstContext, turn); !errors.Is(err, ErrUnknownOutcome) {
		t.Fatalf("lost first admission = %v", err)
	}
	if err := session.StartTurn(context.Background(), turn); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prompts != 2 {
		t.Fatalf("durably absent input deliveries = %d", prompts)
	}
}

func TestResumedOpenRejectsDifferentNativeID(t *testing.T) {
	process, sink := newTestProcess(), &testSink{}
	request := candidateRequest(t, process)
	filename := filepath.Join(profileSessionDirectory(request.Invocation.Args), "retained.jsonl")
	writeRetainedHeader(t, filename, "retained-expected", request.Workspace)
	request.Resume = &harness.ResumeHandle{LocalHandleID: "retained-local", NativeSessionID: "retained-expected", NativeSessionFilename: filename, WorkspaceFingerprint: "workspace-fingerprint", NativeVersion: TestedVersion}
	installOpenResponses(t, process, filename, request.Workspace, nil)
	started, err := NewAdapter("recorded-omo").start(context.Background(), request, sink)
	if err != nil {
		t.Fatal(err)
	}
	session := started.(*nativeSession)
	defer func() { _ = session.Close(context.Background()) }()
	handle, openErr := session.Open(context.Background())
	if !errors.Is(openErr, ErrResumeRejected) || !errors.Is(openErr, ErrCorrelation) {
		t.Fatalf("wrong reopened ID did not return a correlated resume rejection: %v", openErr)
	}
	if handle != (harness.NativeSessionHandle{}) {
		t.Fatalf("wrong retained identity exposed a native handle: %+v", handle)
	}
	if sink.has(harness.EventSessionStarted) {
		t.Fatal("wrong retained identity was exposed as started")
	}
}

func TestCandidateRejectsUnverifiedAccessBeforeLaunch(t *testing.T) {
	process := newTestProcess()
	request := candidateRequest(t, process)
	request.ProviderBridge = &harness.ProviderBridgeLaunch{URL: "http://local.invalid"}
	if started, err := NewAdapter().start(context.Background(), request, &testSink{}); started != nil || !errors.Is(err, harness.ErrUnsupportedCapability) {
		t.Fatalf("provider access = %v, %v", started, err)
	}
	request.ProviderBridge = nil
	value := "100"
	request.Limits.MaxCostMicrousd = &value
	if started, err := NewAdapter().start(context.Background(), request, &testSink{}); started != nil || !errors.Is(err, harness.ErrUnsupportedCapability) {
		t.Fatalf("hard cost = %v, %v", started, err)
	}
	request.Limits.MaxCostMicrousd = nil
	request.AdmissionID, request.LocalHandleID = "", ""
	if _, err := NewAdapter().start(context.Background(), request, &testSink{}); err == nil || !strings.Contains(err.Error(), "stable admission") {
		t.Fatalf("unstable identity = %v", err)
	}
}

// This context expires at an explicit test barrier rather than a wall-clock
// delay. Deadlines on waits below are watchdogs, not race synchronization.
type omoReviewDeadlineContext struct {
	context.Context
	done chan struct{}
}

func (ctx *omoReviewDeadlineContext) Done() <-chan struct{} { return ctx.done }
func (ctx *omoReviewDeadlineContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

type omoReviewCallContext struct {
	*omoReviewDeadlineContext
	checked chan struct{}
	once    sync.Once
}

func (ctx *omoReviewCallContext) Err() error {
	err := ctx.omoReviewDeadlineContext.Err()
	ctx.once.Do(func() { close(ctx.checked) })
	return err
}

func TestOmOReviewRegressionExpiryWhileWaitingToRegisterIsUnsent(t *testing.T) {
	process := newTestProcess()
	request := candidateRequest(t, process)
	prompts := 0
	installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(prompt Request) {
		prompts++
		_ = process.reply(t, prompt, promptData(prompt))
		for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
			if err := process.emit(t, frame); err != nil {
				t.Fatal(err)
			}
		}
	})
	session := openCandidate(t, process, request, &testSink{})
	turn := harness.TurnRequest{Goal: "Keep the unsent identity", Context: json.RawMessage(`{}`)}
	prompt, err := buildTurnPrompt(turn.Goal, turn.Context)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &omoReviewCallContext{omoReviewDeadlineContext: &omoReviewDeadlineContext{Context: context.Background(), done: make(chan struct{})}, checked: make(chan struct{})}
	called := make(chan error, 1)
	session.mutex.Lock()
	go func() {
		_, err := session.call(ctx, Request{Type: CommandPrompt, Message: prompt, ClientMessageID: session.clientMessageID, ClientTurnID: session.clientTurnID})
		called <- err
	}()
	<-ctx.checked
	close(ctx.done)
	session.mutex.Unlock()
	if err := <-called; !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnknownOutcome) || prompts != 0 {
		t.Fatalf("expired registration crossed the transport boundary: prompts=%d error=%v", prompts, err)
	}
	if err := session.StartTurn(context.Background(), turn); err != nil {
		t.Fatalf("unsent registration poisoned the same durable turn: %v", err)
	}
	if err := session.WaitTurn(context.Background()); err != nil || prompts != 1 {
		t.Fatalf("same input retry = prompts:%d error:%v", prompts, err)
	}
}

func TestOmOReviewRegressionPromptRetryDistinguishesUnsentAndUnknown(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired_before_registration", true: "lost_ack"}[sent], func(t *testing.T) {
			process, sink := newTestProcess(), &testSink{}
			request := candidateRequest(t, process)
			deadline := &omoReviewDeadlineContext{Context: context.Background(), done: make(chan struct{})}
			var prompt Request
			prompts, readbacks, starts := 0, 0, 0
			installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(value Request) {
				prompt = value
				prompts++
				if sent {
					close(deadline.done)
					return
				}
				if err := process.reply(t, value, promptData(value)); err != nil {
					t.Fatal(err)
				}
				for _, frame := range completionEvents(t, value, assistantMessage(t)) {
					if err := process.emit(t, frame); err != nil {
						t.Fatal(err)
					}
				}
			})
			original := process.onWrite
			process.onWrite = func(command Request) {
				if command.Type != CommandEffectStatus {
					original(command)
					return
				}
				readbacks++
				if !sent || command.EffectID != prompt.ClientMessageID {
					t.Fatal("definitely unsent input was queried, or readback changed durable identity")
				}
				if err := process.reply(t, command, map[string]any{"found": true, "effect": map[string]any{"effectId": prompt.ClientMessageID, "kind": "input", "state": "applied", "durableSessionId": "native-1"}}); err != nil {
					t.Fatal(err)
				}
				for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
					if err := process.emit(t, frame); err != nil {
						t.Fatal(err)
					}
				}
			}
			session := openCandidate(t, process, request, harness.EventSinkFunc(func(ctx context.Context, event harness.Event) error {
				if event.Kind == harness.EventSessionStarted {
					starts++
					if !sent {
						close(deadline.done)
					}
				}
				return sink.Handle(ctx, event)
			}))
			turn := harness.TurnRequest{Goal: "Preserve this admission", Context: json.RawMessage(`{}`)}
			inputID, turnID := session.clientMessageID, session.clientTurnID
			if err := session.StartTurn(deadline, turn); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnknownOutcome) != sent {
				t.Fatalf("first admission classification = %v, sent=%t", err, sent)
			}
			if !sent {
				receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-unsent", Kind: harness.ControlCancel})
				if err != nil || receipt.Outcome != harness.ControlRejected {
					t.Fatalf("prepared input attempted native cancellation: %+v, %v", receipt, err)
				}
			}
			if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Another turn", Context: json.RawMessage(`{}`)}); err == nil {
				t.Fatal("retry replaced its prepared durable payload")
			}
			if err := session.StartTurn(context.Background(), turn); err != nil {
				t.Fatal(err)
			}
			if err := session.WaitTurn(context.Background()); err != nil {
				t.Fatal(err)
			}
			wantReadbacks := 0
			if sent {
				wantReadbacks = 1
			}
			if prompts != 1 || readbacks != wantReadbacks || starts != 1 || prompt.ClientMessageID != inputID || prompt.ClientTurnID != turnID {
				t.Fatalf("retry effects = prompts:%d readbacks:%d starts:%d input:%q turn:%q", prompts, readbacks, starts, prompt.ClientMessageID, prompt.ClientTurnID)
			}
		})
	}
}

func TestOmOReviewRegressionCompletionAndCancelHaveOneOwner(t *testing.T) {
	for _, completionFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "completion_reserved", false: "cancel_reserved"}[completionFirst], func(t *testing.T) {
			process, sink := newTestProcess(), &testSink{}
			request := candidateRequest(t, process)
			var prompt Request
			installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(value Request) {
				prompt = value
				_ = process.reply(t, value, promptData(value))
			})
			reserved, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseOwner := func() { releaseOnce.Do(func() { close(release) }) }
			abortSent := make(chan struct{}, 1)
			original := process.onWrite
			process.onWrite = func(command Request) {
				if command.Type != CommandAbort {
					original(command)
					return
				}
				abortSent <- struct{}{}
				if !completionFirst {
					close(reserved)
					<-release
				}
				_ = process.reply(t, command, map[string]any{"effectId": command.EffectID, "state": "done", "stopped": true, "idle": true, "effect": map[string]any{"effectId": command.EffectID, "kind": "abort", "state": "done", "durableSessionId": "native-1"}})
			}
			session := openCandidate(t, process, request, harness.EventSinkFunc(func(ctx context.Context, event harness.Event) error {
				if completionFirst && event.Kind == harness.EventTaskResult {
					close(reserved)
					<-release
				}
				return sink.Handle(ctx, event)
			}))
			t.Cleanup(releaseOwner)
			if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Serialize terminal ownership", Context: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			frames := completionEvents(t, prompt, assistantMessage(t))
			type controlResult struct {
				receipt harness.ControlReceipt
				err     error
			}
			controlled := make(chan controlResult, 1)
			emitted := make(chan error, 1)
			var control controlResult
			controlReady, abortBeforeRelease := false, false
			cancelTurn := func() {
				receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-racing-terminal", Kind: harness.ControlCancel})
				controlled <- controlResult{receipt, err}
			}
			if completionFirst {
				for _, frame := range frames[:len(frames)-1] {
					if err := process.emit(t, frame); err != nil {
						t.Fatal(err)
					}
				}
				go func() { emitted <- process.emit(t, frames[len(frames)-1]) }()
				<-reserved
				go cancelTurn()
				// The baseline may send abort and block its ACK behind the result
				// sink. Release that sink only after observing either decision.
				select {
				case control = <-controlled:
					controlReady = true
				case <-abortSent:
					abortBeforeRelease = true
				}
			} else {
				go cancelTurn()
				<-reserved
				for _, frame := range frames {
					if err := process.emit(t, frame); err != nil {
						t.Fatal(err)
					}
				}
				if sink.has(harness.EventTaskResult) {
					t.Fatal("cancellation owner allowed successful result publication")
				}
			}
			releaseOwner()
			if !controlReady {
				control = <-controlled
			}
			if control.err != nil {
				t.Fatal(control.err)
			}
			if completionFirst {
				if err := <-emitted; err != nil {
					t.Fatal(err)
				}
				if control.receipt.Outcome != harness.ControlRejected || abortBeforeRelease || len(abortSent) != 0 {
					t.Fatalf("reserved completion also acknowledged cancel: %+v, aborts=%d", control.receipt, len(abortSent))
				}
			} else if control.receipt.Outcome != harness.ControlApplied || len(abortSent) != 1 {
				t.Fatalf("reserved cancellation lost its effect: %+v, aborts=%d", control.receipt, len(abortSent))
			}
			bounded, cancel := context.WithTimeout(context.Background(), testHangGuard)
			defer cancel()
			if err := session.WaitTurn(bounded); err != nil {
				t.Fatal(err)
			}
			process.finish(execution.Result{PID: 42, ExitCode: 0})
			result, err := session.Wait(bounded)
			wantKind := harness.ResultCancelled
			if completionFirst {
				wantKind = harness.ResultSucceeded
			}
			if err != nil || result.Kind != wantKind || sink.has(harness.EventTaskResult) != completionFirst {
				t.Fatalf("race result = %+v, %v, published=%t", result, err, sink.has(harness.EventTaskResult))
			}
		})
	}
}

func TestOmOReviewRegressionRejectedFinalResultAndCancelHaveOneOwner(t *testing.T) {
	for _, terminal := range []string{"missing_final_result", "schema_invalid_final_result"} {
		for _, completionFirst := range []bool{true, false} {
			t.Run(terminal+"/"+map[bool]string{true: "completion_committed", false: "cancel_reserved"}[completionFirst], func(t *testing.T) {
				process, sink := newTestProcess(), &testSink{}
				request := candidateRequest(t, process)
				var prompt Request
				installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(value Request) {
					prompt = value
					_ = process.reply(t, value, promptData(value))
				})
				reserved, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				releaseOwner := func() { releaseOnce.Do(func() { close(release) }) }
				abortSent := make(chan struct{}, 1)
				original := process.onWrite
				process.onWrite = func(command Request) {
					if command.Type != CommandAbort {
						original(command)
						return
					}
					abortSent <- struct{}{}
					close(reserved)
					<-release
					_ = process.reply(t, command, map[string]any{"effectId": command.EffectID, "state": "done", "stopped": true, "idle": true, "effect": map[string]any{"effectId": command.EffectID, "kind": "abort", "state": "done", "durableSessionId": "native-1"}})
				}
				session := openCandidate(t, process, request, sink)
				t.Cleanup(releaseOwner)
				if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Serialize rejected terminal ownership", Context: json.RawMessage(`{}`)}); err != nil {
					t.Fatal(err)
				}
				message := assistantMessage(t)
				message["content"] = []any{map[string]any{"type": "text", "text": `{"schema_version":"symmetry.task_result.v1"}`}}
				frames := completionEvents(t, prompt, message)
				if terminal == "missing_final_result" {
					frames[2]["messages"] = []any{}
					frames = append(frames[:1], frames[2:]...)
				}
				// Observe the drained native terminal without invoking tryFinalize.
				// Its detached parse result must still arbitrate at completeTurn.
				for _, frame := range frames {
					encoded, err := json.Marshal(frame)
					if err != nil {
						t.Fatal(err)
					}
					records, err := session.decoder.Feed(append(encoded, '\n'))
					if err != nil || len(records) != 1 || records[0].DecodeError != nil {
						t.Fatalf("terminal fixture did not decode: %+v, %v", records, err)
					}
					session.mutex.Lock()
					err = session.validator.Observe(records[0])
					session.mutex.Unlock()
					if err != nil {
						t.Fatal(err)
					}
				}
				session.mutex.Lock()
				final, terminalErr := session.validator.Finish()
				session.mutex.Unlock()
				if terminal == "schema_invalid_final_result" {
					if terminalErr != nil {
						t.Fatalf("schema-invalid output failed before semantic parsing: %v", terminalErr)
					}
					_, terminalErr = DecodeAssistantTaskResult(final)
				}
				if !errors.Is(terminalErr, ErrMissingResult) {
					t.Fatalf("fixture did not produce missing_result: %v", terminalErr)
				}
				reason := protocol.TaskResultReasonMissingResult
				detached := harness.TaskResult{Kind: harness.ResultFailed, Summary: "OmO terminal has no valid TaskResult", Reason: &reason, Usage: harness.Usage{State: harness.UsageUnknown}}
				type controlResult struct {
					receipt harness.ControlReceipt
					err     error
				}
				controlled := make(chan controlResult, 1)
				cancelTurn := func() {
					receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-racing-rejected-terminal", Kind: harness.ControlCancel})
					controlled <- controlResult{receipt, err}
				}
				prematureCompletion := false
				if completionFirst {
					completed := make(chan struct{})
					go func() {
						// This is normal terminal arbitration, not result publication.
						session.completeTurn(detached, terminalErr, true)
						close(completed)
					}()
					<-completed
					go cancelTurn()
					// A broken rejection must not strand Control behind the abort gate.
					select {
					case control := <-controlled:
						controlled <- control
					case <-reserved:
					}
				} else {
					go cancelTurn()
					<-reserved
					// The parse was detached before cancellation reserved the turn.
					// Calling tryFinalize here would skip the stale-decision window.
					// true denotes a normal terminal even without a semantic result.
					session.completeTurn(detached, terminalErr, true)
					select {
					case <-session.turnDone:
						prematureCompletion = true
					default:
					}
				}
				releaseOwner()
				control := <-controlled
				wantOutcome, wantKind, wantReason := harness.ControlApplied, harness.ResultCancelled, protocol.TaskResultReasonCancelled
				if completionFirst {
					wantOutcome, wantKind, wantReason = harness.ControlRejected, harness.ResultFailed, protocol.TaskResultReasonMissingResult
				}
				if control.err != nil || control.receipt.Outcome != wantOutcome || len(abortSent) != map[bool]int{true: 0, false: 1}[completionFirst] {
					t.Fatalf("terminal owner and cancellation receipt disagree: %+v, %v, aborts=%d", control.receipt, control.err, len(abortSent))
				}
				bounded, cancel := context.WithTimeout(context.Background(), testHangGuard)
				defer cancel()
				turnErr := session.WaitTurn(bounded)
				process.finish(execution.Result{PID: 42, ExitCode: 0})
				result, err := session.Wait(bounded)
				if (completionFirst && !errors.Is(turnErr, terminalErr)) || (!completionFirst && turnErr != nil) || prematureCompletion {
					t.Fatalf("cancellation receipt and terminal owner disagree: %+v, WaitTurn=%v, result=%+v, Wait=%v, closed while cancel reserved=%t", control.receipt, turnErr, result, err, prematureCompletion)
				}
				if err != nil || result.Kind != wantKind || result.Reason == nil || *result.Reason != wantReason || result.Semantic != nil || result.Usage.State != harness.UsageUnknown || result.Process.PID != 42 || !result.Process.Success() || sink.has(harness.EventTaskResult) {
					t.Fatalf("rejected terminal race result = %+v, %v, published=%t", result, err, sink.has(harness.EventTaskResult))
				}
			})
		}
	}
}

func TestOmOReviewRegressionFatalCompletionBypassesReservedCancel(t *testing.T) {
	for _, failure := range []string{"lifecycle_failure", "process_ended"} {
		t.Run(failure, func(t *testing.T) {
			process, sink := newTestProcess(), &testSink{}
			request := candidateRequest(t, process)
			installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(prompt Request) {
				_ = process.reply(t, prompt, promptData(prompt))
			})
			reserved, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseOwner := func() { releaseOnce.Do(func() { close(release) }) }
			original := process.onWrite
			process.onWrite = func(command Request) {
				if command.Type != CommandAbort {
					original(command)
					return
				}
				close(reserved)
				<-release
				// Physical/lifecycle failure cannot be upgraded to an abort ACK.
			}
			session := openCandidate(t, process, request, sink)
			t.Cleanup(releaseOwner)
			if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Fail closed while cancellation owns the turn", Context: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			type controlResult struct {
				receipt harness.ControlReceipt
				err     error
			}
			controlled := make(chan controlResult, 1)
			go func() {
				receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-racing-fatal-completion", Kind: harness.ControlCancel})
				controlled <- controlResult{receipt, err}
			}()
			<-reserved
			fatalErr := errors.New("recorded fatal native failure")
			physical := execution.Result{PID: 42, ExitCode: 0}
			wantReason := protocol.TaskResultReasonUnknownOutcome
			if failure == "lifecycle_failure" {
				session.fail(fatalErr)
			} else {
				physical.ExitCode, physical.WaitError = 17, fatalErr
				wantReason = protocol.TaskResultReasonProcessFailure
				process.finish(physical)
			}
			bounded, cancel := context.WithTimeout(context.Background(), testHangGuard)
			defer cancel()
			turnErr := session.WaitTurn(bounded)
			if turnErr == nil || errors.Is(turnErr, context.DeadlineExceeded) || (failure == "lifecycle_failure" && !errors.Is(turnErr, fatalErr)) {
				t.Fatalf("fatal completion waited for the cancellation owner: %v", turnErr)
			}
			process.finish(physical)
			result, err := session.Wait(bounded)
			if err != nil || result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != wantReason || result.Semantic != nil || result.Usage.State != harness.UsageUnknown || result.Process.PID != 42 || result.Process.ExitCode != physical.ExitCode || !errors.Is(result.Process.WaitError, physical.WaitError) || sink.has(harness.EventTaskResult) {
				t.Fatalf("fatal completion lost its physical/lifecycle failure: %+v, %v", result, err)
			}
			releaseOwner()
			control := <-controlled
			if control.err == nil || control.receipt.Outcome != harness.ControlFailed {
				t.Fatalf("fatal completion acknowledged a drained cancellation: %+v, %v", control.receipt, control.err)
			}
		})
	}
}

func TestOmOReviewRegressionFailedCancelReleasesAlreadyDrainedTurn(t *testing.T) {
	process, sink := newTestProcess(), &testSink{}
	request := candidateRequest(t, process)
	var prompt Request
	installOpenResponses(t, process, filepath.Join(profileSessionDirectory(request.Invocation.Args), "native-1.jsonl"), request.Workspace, func(value Request) {
		prompt = value
		_ = process.reply(t, value, promptData(value))
	})
	deadline := &omoReviewDeadlineContext{Context: context.Background(), done: make(chan struct{})}
	original := process.onWrite
	process.onWrite = func(command Request) {
		if command.Type != CommandAbort {
			original(command)
			return
		}
		for _, frame := range completionEvents(t, prompt, assistantMessage(t)) {
			if err := process.emit(t, frame); err != nil {
				t.Fatal(err)
			}
		}
		if sink.has(harness.EventTaskResult) {
			t.Fatal("terminal published before the cancellation decision")
		}
		// No abort ACK and no further native frames arrive after this timeout.
		close(deadline.done)
	}
	session := openCandidate(t, process, request, sink)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Release the observed terminal", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	receipt, err := session.Control(deadline, harness.ControlRequest{CommandID: "cancel-lost-ack", Kind: harness.ControlCancel})
	if receipt.Outcome != harness.ControlFailed || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrUnknownOutcome) {
		t.Fatalf("failed cancellation receipt was rewritten: %+v, %v", receipt, err)
	}
	bounded, cancel := context.WithTimeout(context.Background(), testHangGuard)
	defer cancel()
	if err := session.WaitTurn(bounded); err != nil {
		t.Fatalf("already drained terminal still waits for another frame: %v", err)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(bounded)
	if err != nil || result.Kind != harness.ResultSucceeded || result.Semantic == nil || !sink.has(harness.EventTaskResult) {
		t.Fatalf("failed cancel lost independently completed result: %+v, %v", result, err)
	}
}

type omoReviewPathProbe struct {
	recordedProbe
	programs []string
	hashed   string
}

func (runner *omoReviewPathProbe) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	runner.programs = append(runner.programs, executable)
	return runner.recordedProbe.Run(ctx, executable, args...)
}

func (runner *omoReviewPathProbe) ExecutableDigest(_ context.Context, executable string) (string, error) {
	runner.hashed = executable
	data, err := os.ReadFile(executable)
	if err != nil {
		return "", err
	}
	if string(data) != "daemon-pinned-fixture" {
		return runner.digest + "changed", nil
	}
	return runner.digest, nil
}

func TestOmOReviewRegressionRelativeExecutableUsesOneAbsoluteIdentity(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OmO executable promotion is pinned only on Windows/Linux")
	}
	process := newTestProcess()
	request := candidateRequest(t, process)
	daemonDirectory := t.TempDir()
	for directory, content := range map[string]string{daemonDirectory: "daemon-pinned-fixture", request.Workspace: "workspace-unpinned-fixture"} {
		if err := os.Mkdir(filepath.Join(directory, "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "bin", "omo"), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(daemonDirectory)
	digest := TestedWindowsExecutableHash
	if runtime.GOOS == "linux" {
		digest = TestedLinuxExecutableHash
	}
	runner := &omoReviewPathProbe{recordedProbe: recordedProbe{TestedVersionLine, "--mode <json|rpc>", digest}}
	adapter := NewAdapterWithRunner("./bin/omo", runner)
	var invocation execution.Invocation
	request.StartProcess = func(_ context.Context, launched execution.Invocation, sink execution.Sink) (harness.NativeProcess, error) {
		invocation, process.sink = launched, sink
		return process, nil
	}
	started, err := adapter.Start(context.Background(), request, &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		process.finish(execution.Result{PID: 42, ExitCode: 0})
		_ = started.Close(context.Background())
	})
	want := filepath.Join(daemonDirectory, "bin", "omo")
	if len(runner.programs) != 2 || runner.programs[0] != want || runner.programs[1] != want || runner.hashed != want || invocation.Program != want || invocation.Dir != request.Workspace {
		t.Fatalf("executable identity diverged: probes=%q hash=%q launch=%q dir=%q, want=%q", runner.programs, runner.hashed, invocation.Program, invocation.Dir, want)
	}
	if err := os.WriteFile(want, []byte("unpinned-fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	request.StartProcess = func(context.Context, execution.Invocation, execution.Sink) (harness.NativeProcess, error) {
		t.Fatal("changed executable bytes bypassed the pin gate")
		return nil, errors.New("unexpected launch")
	}
	if session, err := adapter.Start(context.Background(), request, &testSink{}); session != nil || !errors.Is(err, harness.ErrNativeUnverified) {
		t.Fatalf("changed relative executable classification = %v, %v", session, err)
	}
}

func requireOmOReviewUnavailableProjection(t *testing.T, capabilities harness.Capabilities, err error) {
	t.Helper()
	if validateErr := capabilities.Validate(); validateErr != nil {
		t.Fatalf("unavailable capability projection is invalid: %+v, %v", capabilities, validateErr)
	}
	if capabilities.Kind != harness.KindOmO || capabilities.ImplementationVersion != ImplementationVersion || capabilities.NativeVersion != "" || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume || capabilities.Handoff {
		t.Fatalf("unavailable projection lost its OmO identity or promoted an operation: %+v", capabilities)
	}
	leaf := err
	for errors.Unwrap(leaf) != nil {
		leaf = errors.Unwrap(leaf)
	}
	// A multi-error wrapper stops this walk and is not the expected sole leaf.
	if leaf != harness.ErrHarnessUnavailable {
		t.Fatalf("availability error retained an independent fatal leaf: %T %v", leaf, err)
	}
}

func TestOmOReviewRegressionProductionLookupFailureHasOnlyAvailabilityLeaf(t *testing.T) {
	for _, coincidentalCWD := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_command", true: "cwd_is_not_a_path_fallback"}[coincidentalCWD], func(t *testing.T) {
			cwd, pathDirectory := t.TempDir(), t.TempDir()
			t.Chdir(cwd)
			t.Setenv("PATH", pathDirectory)
			t.Setenv("GODEBUG", "execerrdot=1")
			command := "symmetry-omo-review-missing-executable"
			if coincidentalCWD {
				filename := command
				if runtime.GOOS == "windows" {
					filename += ".exe"
				}
				if err := os.WriteFile(filepath.Join(cwd, filename), []byte("not an OmO executable"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			_, lookupErr := exec.LookPath(command)
			if lookupErr == nil {
				t.Fatal("isolated PATH fixture unexpectedly resolved the command")
			}
			adapter := NewAdapter(command)
			capabilities, err := adapter.Probe(context.Background())
			requireOmOReviewUnavailableProjection(t, capabilities, err)
			if !strings.Contains(err.Error(), lookupErr.Error()) {
				t.Fatalf("lookup diagnostic was lost or cwd was used as fallback: %v", err)
			}
			registry := harness.NewRegistry()
			if err := registry.Register(harness.KindOmO, adapter); err != nil {
				t.Fatal(err)
			}
			capabilities, err = registry.Probe(context.Background(), harness.KindOmO)
			requireOmOReviewUnavailableProjection(t, capabilities, err)
			request := candidateRequest(t, newTestProcess())
			request.StartProcess = func(context.Context, execution.Invocation, execution.Sink) (harness.NativeProcess, error) {
				t.Fatal("unavailable production command reached the launcher")
				return nil, errors.New("unexpected launch")
			}
			session, startErr := adapter.Start(context.Background(), request, &testSink{})
			if session != nil {
				t.Fatal("unavailable command exposed a native session")
			}
			requireOmOReviewUnavailableProjection(t, capabilities, startErr)
		})
	}
}

func TestOmOReviewRegressionProductionLookupPreservesCallerContextFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())
	adapter := NewAdapter("symmetry-omo-review-missing-executable")
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindOmO, adapter); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer expire()
	for _, ctx := range []context.Context{cancelled, expired} {
		for _, probe := range []func(context.Context) (harness.Capabilities, error){adapter.Probe, func(ctx context.Context) (harness.Capabilities, error) { return registry.Probe(ctx, harness.KindOmO) }} {
			capabilities, err := probe(ctx)
			if err != ctx.Err() || errors.Is(err, harness.ErrHarnessUnavailable) || capabilities.Kind != harness.KindOmO || capabilities.ImplementationVersion != ImplementationVersion {
				t.Fatalf("caller context was demoted or joined with a projection failure: %+v, %v", capabilities, err)
			}
			if err := capabilities.Validate(); err != nil {
				t.Fatal(err)
			}
		}
		for _, start := range []func(context.Context, harness.StartRequest, harness.EventSink) (harness.Session, error){adapter.Start, adapter.start} {
			if session, err := start(ctx, harness.StartRequest{}, &testSink{}); session != nil || err != ctx.Err() || errors.Is(err, harness.ErrHarnessUnavailable) {
				t.Fatalf("Start demoted the caller context: %v, %v", session, err)
			}
		}
	}
}

func TestOmOReviewRegressionAbsoluteResolutionFailureHasOnlyAvailabilityLeaf(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows prevents removal of the current directory")
	}
	request := candidateRequest(t, newTestProcess())
	request.StartProcess = func(context.Context, execution.Invocation, execution.Sink) (harness.NativeProcess, error) {
		t.Fatal("unresolvable executable path reached the launcher")
		return nil, errors.New("unexpected launch")
	}
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Remove(cwd); err != nil {
		t.Fatal(err)
	}
	_, cwdErr := os.Getwd()
	if cwdErr == nil {
		t.Fatal("removed current directory remained resolvable")
	}
	adapter := NewAdapterWithRunner("./bin/omo", recordedProbe{})
	capabilities, err := adapter.Probe(context.Background())
	requireOmOReviewUnavailableProjection(t, capabilities, err)
	if !strings.Contains(err.Error(), cwdErr.Error()) {
		t.Fatalf("absolute-path failure lost its cwd diagnostic: %v", err)
	}
	for _, start := range []func(context.Context, harness.StartRequest, harness.EventSink) (harness.Session, error){adapter.Start, adapter.start} {
		session, err := start(context.Background(), request, &testSink{})
		if session != nil {
			t.Fatal("unresolvable path exposed a session")
		}
		requireOmOReviewUnavailableProjection(t, capabilities, err)
	}
}

func TestStartTurnBeforeOpenRefusesWithTheOmONotOpenedText(t *testing.T) {
	process := newTestProcess()
	started, err := NewAdapter("recorded-omo").start(context.Background(), candidateRequest(t, process), &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	session := started.(*nativeSession)
	t.Cleanup(func() {
		process.finish(execution.Result{PID: 42, ExitCode: 0})
		cleanup, cancel := context.WithTimeout(context.Background(), testHangGuard)
		defer cancel()
		_, _ = session.Wait(cleanup)
		_ = session.Close(cleanup)
	})
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Change artifact", Context: json.RawMessage(`{}`)}); err == nil || err.Error() != "OmO native handle has not opened" {
		t.Fatalf("StartTurn before Open = %v, want the handle barrier text", err)
	}
}
