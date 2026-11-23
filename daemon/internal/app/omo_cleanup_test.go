package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/app/apptest"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/omo"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// These fixtures exercise the real adapter and daemon consumer, not native
// capability evidence. No subprocess or provider is invoked by this seam.
type omoCleanupReviewProbe struct{}

func (omoCleanupReviewProbe) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) != 1 {
		return nil, errors.New("unexpected OmO cleanup probe arguments")
	}
	switch args[0] {
	case "--version":
		return []byte(omo.TestedVersionLine), nil
	case "--help":
		return []byte("--mode <json|rpc>"), nil
	default:
		return nil, errors.New("unexpected OmO cleanup probe command")
	}
}

func (omoCleanupReviewProbe) ExecutableDigest(context.Context, string) (string, error) {
	if runtime.GOOS == "linux" {
		return omo.TestedLinuxExecutableHash, nil
	}
	return omo.TestedWindowsExecutableHash, nil
}

type omoCleanupReviewProcess struct {
	sink      execution.Sink
	filename  string
	workspace string
	semantic  *protocol.TaskResult
	result    execution.Result
	done      chan struct{}
	once      sync.Once
}

func (*omoCleanupReviewProcess) ProcessDetails() (int, string) { return 71, "omo-review:71" }
func (process *omoCleanupReviewProcess) finish()               { process.once.Do(func() { close(process.done) }) }
func (process *omoCleanupReviewProcess) Wait() execution.Result {
	<-process.done
	return process.result
}
func (process *omoCleanupReviewProcess) CloseInput() error {
	process.finish()
	return nil
}
func (process *omoCleanupReviewProcess) Terminate(context.Context, time.Duration) error {
	process.finish()
	return nil
}
func (process *omoCleanupReviewProcess) emit(frame any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return process.sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Data: append(data, '\n')})
}
func (process *omoCleanupReviewProcess) reply(request omo.Request, data any) error {
	return process.emit(map[string]any{"type": "response", "id": request.ID, "command": request.Type, "success": true, "data": data})
}
func (process *omoCleanupReviewProcess) WriteInputContext(_ context.Context, data []byte) error {
	var request omo.Request
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}
	switch request.Type {
	case omo.CommandProtocol:
		return process.reply(request, map[string]any{"protocolVersion": 1, "serverVersion": omo.TestedVersion, "engineVersion": omo.TestedEngineVersion, "mode": "classic", "capabilities": []string{"durable_client_message_id", "effect_ledger"}})
	case omo.CommandGetState:
		return process.reply(request, map[string]any{"sessionId": "native-1", "sessionFile": process.filename, "isStreaming": false, "isCompacting": false, "pendingMessageCount": 0, "steering": []string{}, "followUp": []string{}, "pendingDialogs": []any{}, "permissionPreset": "workspace", "permissionPresetStatus": "applied", "effectivePermissionPreset": "workspace"})
	case omo.CommandPrompt:
		header, err := json.Marshal(map[string]any{"type": "session", "version": 3, "id": "native-1", "cwd": process.workspace})
		if err != nil {
			return err
		}
		if err := os.WriteFile(process.filename, append(header, '\n'), 0600); err != nil {
			return err
		}
		if err := process.reply(request, map[string]any{"disposition": "started", "clientMessageId": request.ClientMessageID, "clientTurnId": request.ClientTurnID, "admission": map[string]any{"durableSessionId": "native-1", "clientMessageId": request.ClientMessageID, "clientTurnId": request.ClientTurnID, "state": "running", "disposition": "started"}}); err != nil {
			return err
		}
		if err := process.emit(map[string]any{"type": "agent_start"}); err != nil {
			return err
		}
		messages := []any{}
		if process.semantic != nil {
			encoded, err := json.Marshal(process.semantic)
			if err != nil {
				return err
			}
			message := map[string]any{"role": "assistant", "stopReason": "stop", "content": []any{map[string]any{"type": "text", "text": string(encoded)}}}
			if err := process.emit(map[string]any{"type": "message_end", "clientMessageId": request.ClientMessageID, "clientTurnId": request.ClientTurnID, "message": message}); err != nil {
				return err
			}
			messages = append(messages, message)
		}
		for _, frame := range []any{
			map[string]any{"type": "agent_end", "clientMessageId": request.ClientMessageID, "clientTurnId": request.ClientTurnID, "messages": messages, "willRetry": false},
			map[string]any{"type": "agent_settled"}, map[string]any{"type": "agent_idle"},
		} {
			if err := process.emit(frame); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unexpected OmO cleanup RPC: %s", request.Type)
	}
}

func TestOmOReviewRegressionStoppedResultsPermitOnlyExactDaemonCleanup(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OmO executable promotion is pinned only on Windows/Linux")
	}
	physicalFailure := errors.New("review physical failure")
	for _, test := range []struct {
		name         string
		semanticKind protocol.TaskResultKind
		physical     execution.Result
		changeMarker bool
	}{
		{name: "missing_result", physical: execution.Result{PID: 71}},
		{name: "blocked", semanticKind: protocol.TaskResultBlocked, physical: execution.Result{PID: 71}},
		{name: "failed", semanticKind: protocol.TaskResultFailed, physical: execution.Result{PID: 71}},
		{name: "process_failure", semanticKind: protocol.TaskResultBlocked, physical: execution.Result{PID: 71, ExitCode: 7, WaitError: physicalFailure}},
		{name: "sink_failure", semanticKind: protocol.TaskResultBlocked, physical: execution.Result{PID: 71, SinkError: physicalFailure}},
		{name: "output_failure", semanticKind: protocol.TaskResultBlocked, physical: execution.Result{PID: 71, OutputError: physicalFailure}},
		{name: "truncated_output", semanticKind: protocol.TaskResultBlocked, physical: execution.Result{PID: 71, OutputTruncated: true}},
		{name: "containment_unproven", semanticKind: protocol.TaskResultBlocked, physical: execution.Result{PID: 71, ContainmentError: physicalFailure}},
		{name: "termination_unproven", semanticKind: protocol.TaskResultBlocked, physical: execution.Result{PID: 71, TerminationError: physicalFailure}},
		{name: "replacement_marker", physical: execution.Result{PID: 71}, changeMarker: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedGoalDeliveryStore(t)
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission: present=%t error=%v", present, err)
			}
			workspace, sessionDirectory := t.TempDir(), t.TempDir()
			process := &omoCleanupReviewProcess{workspace: workspace, filename: filepath.Join(sessionDirectory, "native-1.jsonl"), result: test.physical, done: make(chan struct{})}
			if test.semanticKind != "" {
				semantic := validNativeTaskResult(t, admission)
				semantic.Kind = test.semanticKind
				if semantic.Kind == protocol.TaskResultBlocked {
					semantic.Blocker = &protocol.Blocker{Kind: protocol.BlockerEnvironment, Code: "missing_dependency", Detail: "A required dependency is unavailable"}
				} else {
					reason := protocol.TaskResultReasonAuth
					semantic.Reason = &reason
				}
				if err := semantic.Validate(); err != nil {
					t.Fatal(err)
				}
				process.semantic = &semantic
			}
			sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
			bindingID, controlSessionID := "00000000-0000-4000-8000-000000000009", "00000000-0000-4000-8000-000000000010"
			if _, err := store.SaveGoalSessionLaunchIntent(state.GoalSessionLaunchIntent{
				LaunchIntentID: "00000000-0000-4000-8000-000000000008", GoalID: admission.GoalID, GoalRevision: admission.GoalRevision,
				WorkItemID: admissionWorkItemIDValue(admission.WorkItemID), TaskID: "task-1", RunID: key.RunID, Generation: key.Generation, AdmissionID: admission.AdmissionID,
				LocalHandleID: sessionKey.LocalHandleID, BindingID: bindingID, MachineID: "machine-1", RuntimeID: "runtime-1", RuntimeEpoch: 1,
				HarnessKind: string(harness.KindOmO), HarnessVersion: omo.TestedVersion, AdapterVersion: omo.ImplementationVersion, AdapterProtocolVersion: 1,
				WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", WorkspacePath: workspace, SessionMode: state.GoalSessionModeFresh,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.MarkGoalSessionLaunchStarted(sessionKey); err != nil {
				t.Fatal(err)
			}
			startedAt := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
			request := harness.StartRequest{AdmissionID: admission.AdmissionID, Workspace: workspace, Invocation: execution.Invocation{Args: []string{"--session-dir", sessionDirectory}}, PersistProcess: func(pid int, identity string) error {
				_, err := store.SetProcessDetails(key, pid, identity, startedAt)
				return err
			}, StartProcess: func(_ context.Context, invocation execution.Invocation, sink execution.Sink) (harness.NativeProcess, error) {
				process.sink = sink
				pid, identity := process.ProcessDetails()
				if err := invocation.PersistProcess(pid, identity); err != nil {
					return process, err
				}
				return process, nil
			}}
			session, err := omo.NewAdapterWithRunner("recorded-omo", omoCleanupReviewProbe{}).Start(context.Background(), request, harness.EventSinkFunc(func(context.Context, harness.Event) error { return nil }))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { process.finish(); _ = session.Close(context.Background()) })
			staged := session.(harness.StagedSession)
			handle, err := staged.Open(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.PersistGoalSessionHandle(sessionKey, state.GoalSessionHandle{NativeSessionID: handle.ID, NativeSessionFilename: handle.Filename}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.PersistGoalSessionControlAttachment(sessionKey, controlSessionID, bindingID, "00000000-0000-4000-8000-000000000012", false); err != nil {
				t.Fatal(err)
			}
			if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Exercise stopped-result cleanup", Context: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			bounded, cancel := context.WithTimeout(context.Background(), testHangGuard)
			defer cancel()
			turnErr := staged.WaitTurn(bounded)
			if (process.semantic == nil && !errors.Is(turnErr, omo.ErrMissingResult)) || (process.semantic != nil && turnErr != nil) {
				t.Fatalf("turn failure classification changed: %v", turnErr)
			}
			process.finish()
			final, waitErr := session.Wait(bounded)
			if waitErr != nil || !reflect.DeepEqual(final.Process, test.physical) {
				t.Errorf("completed Wait did not preserve the complete physical result: %+v, %v", final, waitErr)
			}
			wantKind := harness.ResultSucceeded
			if process.semantic == nil || test.semanticKind == protocol.TaskResultFailed || !test.physical.Success() {
				wantKind = harness.ResultFailed
			}
			if final.Kind != wantKind || (!test.physical.Success() && (final.Semantic != nil || final.Reason == nil || *final.Reason != protocol.TaskResultReasonProcessFailure)) || (test.physical.Success() && process.semantic != nil && !reflect.DeepEqual(final.Semantic, process.semantic)) {
				t.Fatalf("joined result weakened semantic/process classification: %+v", final)
			}
			if process.semantic == nil && (final.Semantic != nil || final.Reason == nil || *final.Reason != protocol.TaskResultReasonMissingResult || final.Summary != "OmO settled without matching final output") {
				t.Fatalf("joined result rewrote the missing-result failure: %+v", final)
			}
			active := &runningRun{nativeSession: session, goalSession: &sessionKey}
			clearCalls := 0
			faultStore := apptest.NewFaultStoreFrom(store)
			faultStore.OnClearProcessDetails = func(clearedKey state.RunKey, pid int, identity string) (state.RunJournal, error) {
				clearCalls++
				if clearedKey != key || pid != 71 || identity != "omo-review:71" {
					t.Fatalf("marker clear lost the original process identity: %v %d %q", clearedKey, pid, identity)
				}
				if test.changeMarker {
					if _, err := store.ClearProcessDetails(key, 71, "omo-review:71"); err != nil {
						t.Fatalf("release proven-stopped fixture marker: %v", err)
					}
					if _, err := store.SetProcessDetails(key, 72, "replacement:72", startedAt); err != nil {
						t.Fatalf("register replacement fixture marker: %v", err)
					}
					journal, err := store.LoadJournal(key)
					if err != nil || journal.PID != 72 || journal.ProcessIdentity != "replacement:72" || !journal.StartedAt.Equal(startedAt) {
						t.Fatalf("replacement fixture marker was not persisted: %+v, %v", journal, err)
					}
				}
				journal, err := store.ClearProcessDetails(clearedKey, pid, identity)
				if test.changeMarker && (err == nil || err.Error() != "persisted process details changed during termination") {
					t.Fatalf("stale clear did not reach the exact-marker CAS rejection: %v", err)
				}
				return journal, err
			}
			app := &daemon{store: faultStore, running: map[state.RunKey]*runningRun{key: active}, options: options{clock: time.Now}}
			closeErr := app.closeNativeGoalSession(key, active, session)
			unprovenStop := test.physical.ContainmentError != nil || test.physical.TerminationError != nil
			if (closeErr != nil) != (unprovenStop || test.changeMarker) || (unprovenStop && !errors.Is(closeErr, physicalFailure)) {
				t.Fatalf("daemon stop proof = %v, unproven=%t markerChanged=%t", closeErr, unprovenStop, test.changeMarker)
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if unprovenStop {
				if journal.PID != 71 || journal.ProcessIdentity != "omo-review:71" || !journal.StartedAt.Equal(startedAt) || len(journal.PendingGoalDeliveries) != 0 || clearCalls != 0 || !active.cleanupBlocked || !active.nativeCloseRetryRequired {
					t.Fatalf("unproven stop released its durable cleanup barrier: %+v, clears=%d", journal, clearCalls)
				}
				return
			}
			if len(journal.PendingGoalDeliveries) != 1 {
				t.Fatalf("stopped result lost its exact stop outbox: %+v", journal)
			}
			stopped := journal.PendingGoalDeliveries[0]
			if stopped.Kind != state.GoalDeliverySessionStopped || stopped.SessionStopped == nil || stopped.SessionStopped.SessionID != controlSessionID || stopped.SessionStopped.LocalHandleID != sessionKey.LocalHandleID || stopped.SessionStopped.BindingID != bindingID || clearCalls != 1 {
				t.Fatalf("stop receipt/marker clear identities changed: %+v, clears=%d", stopped, clearCalls)
			}
			if test.changeMarker {
				if journal.PID != 72 || journal.ProcessIdentity != "replacement:72" || !journal.StartedAt.Equal(startedAt) || !active.cleanupBlocked || !active.nativeCloseRetryRequired {
					t.Fatalf("old process proof cleared a replacement marker: %+v", journal)
				}
			} else if journal.HasProcessDetails() || !journal.StartedAt.IsZero() {
				t.Fatalf("physically stopped result retained its original marker: %+v", journal)
			}
			goalSession, err := store.LoadGoalSession(sessionKey)
			if err != nil || goalSession.SessionState != state.GoalSessionStateUnavailable || goalSession.NativeSessionID != handle.ID || goalSession.NativeSessionFilename != handle.Filename {
				t.Fatalf("local stopped projection lost the retained native identity: %+v, %v", goalSession, err)
			}
		})
	}
}

func TestOmOReviewRegressionUnavailableOmOPathPermitsStartupRegistration(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())
	value := testConfig(t)
	value.Runtime.HarnessKind = config.RuntimeHarnessOmO
	value.Runtime.HarnessVersion = "configured-native-9"
	value.Runtime.AdapterVersion = "configured-adapter-9"
	value.Runtime.AdapterProtocolVersion = 1
	profile := value.AgentProfiles[value.Runtime.AgentProfile]
	profile.Command = "symmetry-omo-review-missing-executable"
	value.AgentProfiles[value.Runtime.AgentProfile] = profile
	app := &daemon{config: value}
	if err := app.ensureHarnessProbe(context.Background()); err != nil {
		t.Fatalf("expected missing production OmO command blocked startup: %v", err)
	}
	capabilities := app.harnessCapabilities
	if capabilities.Kind != harness.KindOmO || capabilities.ImplementationVersion != omo.ImplementationVersion || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume || capabilities.Handoff {
		t.Fatalf("startup cached an invalid or promoted unavailable projection: %+v", capabilities)
	}
	_, probeErr := app.harnessRegistry.Probe(context.Background(), harness.KindOmO)
	if !probeErrorContainsOnly(probeErr, harness.ErrHarnessUnavailable) {
		t.Fatalf("registry retained an independent fatal lookup leaf: %v", probeErr)
	}
	registration, _, err := buildRuntimeRegistration(app.config.Runtime, profile, capabilities)
	if err != nil {
		t.Fatalf("unavailable OmO projection could not register: %v", err)
	}
	adapter := registration.Capabilities.Adapter
	if registration.HarnessKind != config.RuntimeHarnessOmO || registration.HarnessVersion != unavailableNativeMetadataVersion || registration.AdapterVersion != omo.ImplementationVersion || adapter == nil || adapter.Kind != config.RuntimeHarnessOmO || adapter.NativeVersion != unavailableNativeMetadataVersion || adapter.ImplementationVersion != omo.ImplementationVersion || adapter.Operations.Start || adapter.Operations.Events || adapter.Operations.Cancel || adapter.Operations.Resume || adapter.Operations.Handoff {
		t.Fatalf("registration lost OmO identity or retained configured capability claims: %+v", registration)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := &daemon{config: value}
	if err := cancelled.ensureHarnessProbe(ctx); !errors.Is(err, context.Canceled) || errors.Is(err, harness.ErrHarnessUnavailable) || cancelled.harnessCapabilities.Kind != "" {
		t.Fatalf("caller cancellation became a tolerable unavailable startup: %v, cached=%+v", err, cancelled.harnessCapabilities)
	}
}
