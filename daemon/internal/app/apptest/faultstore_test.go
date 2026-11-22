package apptest

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

var errFaultStoreHook = errors.New("fault hook")

type hookRecorder struct {
	calls map[string]int
	args  map[string][]any
}

func newHookRecorder() *hookRecorder {
	return &hookRecorder{calls: make(map[string]int), args: make(map[string][]any)}
}

func (recorder *hookRecorder) replaceRun(name string, args ...any) (state.RunJournal, error) {
	recorder.calls[name]++
	recorder.args[name] = args
	return state.RunJournal{}, errFaultStoreHook
}

func (recorder *hookRecorder) replaceGoalSession(name string, args ...any) (state.GoalSessionJournal, error) {
	recorder.calls[name]++
	recorder.args[name] = args
	return state.GoalSessionJournal{}, errFaultStoreHook
}

func (recorder *hookRecorder) total() int {
	total := 0
	for _, count := range recorder.calls {
		total += count
	}
	return total
}

type faultStoreWriteCase struct {
	name     string
	hook     func(*FaultStore, *hookRecorder)
	call     func(*FaultStore) (any, error)
	wantArgs []any
}

func faultStoreWriteCases() []faultStoreWriteCase {
	return []faultStoreWriteCase{
		{
			name: "SetWorkspacePath",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnSetWorkspacePath = func(key state.RunKey, path string) (state.RunJournal, error) {
					return recorder.replaceRun("SetWorkspacePath", key, path)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.SetWorkspacePath(state.RunKey{}, "")
			},
			wantArgs: []any{state.RunKey{}, ""},
		},
		{
			name: "SetProcessDetails",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnSetProcessDetails = func(key state.RunKey, pid int, identity string, startedAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("SetProcessDetails", key, pid, identity, startedAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.SetProcessDetails(state.RunKey{}, 0, "", time.Time{})
			},
			wantArgs: []any{state.RunKey{}, 0, "", time.Time{}},
		},
		{
			name: "SetProcessDetailsWithAuthority",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnSetProcessDetailsWithAuthority = func(key state.RunKey, pid int, identity string, startedAt time.Time, value authority.Supervisor) (state.RunJournal, error) {
					return recorder.replaceRun("SetProcessDetailsWithAuthority", key, pid, identity, startedAt, value)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.SetProcessDetailsWithAuthority(state.RunKey{}, 0, "", time.Time{}, authority.Supervisor{})
			},
			wantArgs: []any{state.RunKey{}, 0, "", time.Time{}, authority.Supervisor{}},
		},
		{
			name: "MarkContainmentUnproven",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnMarkContainmentUnproven = func(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
					return recorder.replaceRun("MarkContainmentUnproven", key, pid, identity)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.MarkContainmentUnproven(state.RunKey{}, 0, "")
			},
			wantArgs: []any{state.RunKey{}, 0, ""},
		},
		{
			name: "ClearContainmentUnproven",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnClearContainmentUnproven = func(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
					return recorder.replaceRun("ClearContainmentUnproven", key, pid, identity)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.ClearContainmentUnproven(state.RunKey{}, 0, "")
			},
			wantArgs: []any{state.RunKey{}, 0, ""},
		},
		{
			name: "RecordContainmentStopReceipt",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnRecordContainmentStopReceipt = func(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) (state.RunJournal, error) {
					return recorder.replaceRun("RecordContainmentStopReceipt", key, pid, identity, receipt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.RecordContainmentStopReceipt(state.RunKey{}, 0, "", authority.StopReceipt{})
			},
			wantArgs: []any{state.RunKey{}, 0, "", authority.StopReceipt{}},
		},
		{
			name: "PrepareSupervisorHandoff",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnPrepareSupervisorHandoff = func(key state.RunKey, handoff authority.SupervisorHandoff) (state.RunJournal, error) {
					return recorder.replaceRun("PrepareSupervisorHandoff", key, handoff)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.PrepareSupervisorHandoff(state.RunKey{}, authority.SupervisorHandoff{})
			},
			wantArgs: []any{state.RunKey{}, authority.SupervisorHandoff{}},
		},
		{
			name: "BindSupervisorHandoff",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnBindSupervisorHandoff = func(key state.RunKey, expected authority.SupervisorHandoff, pid int, identity string) (state.RunJournal, error) {
					return recorder.replaceRun("BindSupervisorHandoff", key, expected, pid, identity)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.BindSupervisorHandoff(state.RunKey{}, authority.SupervisorHandoff{}, 0, "")
			},
			wantArgs: []any{state.RunKey{}, authority.SupervisorHandoff{}, 0, ""},
		},
		{
			name: "CommitSupervisorHandoff",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnCommitSupervisorHandoff = func(key state.RunKey, expected authority.SupervisorHandoff, startedAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("CommitSupervisorHandoff", key, expected, startedAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.CommitSupervisorHandoff(state.RunKey{}, authority.SupervisorHandoff{}, time.Time{})
			},
			wantArgs: []any{state.RunKey{}, authority.SupervisorHandoff{}, time.Time{}},
		},
		{
			name: "RecordSupervisorHandoffStopReceipt",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnRecordSupervisorHandoffStopReceipt = func(key state.RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (state.RunJournal, error) {
					return recorder.replaceRun("RecordSupervisorHandoffStopReceipt", key, expected, receipt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.RecordSupervisorHandoffStopReceipt(state.RunKey{}, authority.SupervisorHandoff{}, authority.StopReceipt{})
			},
			wantArgs: []any{state.RunKey{}, authority.SupervisorHandoff{}, authority.StopReceipt{}},
		},
		{
			name: "ClearSupervisorHandoff",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnClearSupervisorHandoff = func(key state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) (state.RunJournal, error) {
					return recorder.replaceRun("ClearSupervisorHandoff", key, expected, proof)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.ClearSupervisorHandoff(state.RunKey{}, authority.SupervisorHandoff{}, authority.SupervisorHandoffReleaseProof{})
			},
			wantArgs: []any{state.RunKey{}, authority.SupervisorHandoff{}, authority.SupervisorHandoffReleaseProof{}},
		},
		{
			name: "ClearSupervisorHandoffAfterAbort",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnClearSupervisorHandoffAfterAbort = func(key state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffAbortProof) (state.RunJournal, error) {
					return recorder.replaceRun("ClearSupervisorHandoffAfterAbort", key, expected, proof)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.ClearSupervisorHandoffAfterAbort(state.RunKey{}, authority.SupervisorHandoff{}, authority.SupervisorHandoffAbortProof{})
			},
			wantArgs: []any{state.RunKey{}, authority.SupervisorHandoff{}, authority.SupervisorHandoffAbortProof{}},
		},
		{
			name: "QueueTerminalTransitionAt",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueTerminalTransitionAt = func(key state.RunKey, transition protocol.StateTransitionRequest, pendingAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("QueueTerminalTransitionAt", key, transition, pendingAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueTerminalTransitionAt(state.RunKey{}, protocol.StateTransitionRequest{}, time.Time{})
			},
			wantArgs: []any{state.RunKey{}, protocol.StateTransitionRequest{}, time.Time{}},
		},
		{
			name: "QueueGoalUsage",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueGoalUsage = func(key state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
					return recorder.replaceRun("QueueGoalUsage", key, usage)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueGoalUsage(state.RunKey{}, protocol.Usage{})
			},
			wantArgs: []any{state.RunKey{}, protocol.Usage{}},
		},
		{
			name: "QueueNativeUsageRecovery",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueNativeUsageRecovery = func(key state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
					return recorder.replaceRun("QueueNativeUsageRecovery", key, usage)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueNativeUsageRecovery(state.RunKey{}, protocol.Usage{})
			},
			wantArgs: []any{state.RunKey{}, protocol.Usage{}},
		},
		{
			name: "QueueNativeUsageRecoverySettlement",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueNativeUsageRecoverySettlement = func(key state.RunKey, usage protocol.Usage, settlement *state.NativeUsageTerminalRecovery) (state.RunJournal, error) {
					return recorder.replaceRun("QueueNativeUsageRecoverySettlement", key, usage, settlement)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueNativeUsageRecoverySettlement(state.RunKey{}, protocol.Usage{}, nil)
			},
			wantArgs: []any{state.RunKey{}, protocol.Usage{}, (*state.NativeUsageTerminalRecovery)(nil)},
		},
		{
			name: "MarkGoalSessionAttachDeliveryReady",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnMarkGoalSessionAttachDeliveryReady = func(key state.RunKey, localHandleID string) (state.RunJournal, error) {
					return recorder.replaceRun("MarkGoalSessionAttachDeliveryReady", key, localHandleID)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.MarkGoalSessionAttachDeliveryReady(state.RunKey{}, "")
			},
			wantArgs: []any{state.RunKey{}, ""},
		},
		{
			name: "DiscardUnreadyGoalSessionAttachDelivery",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnDiscardUnreadyGoalSessionAttachDelivery = func(key state.RunKey, localHandleID string) (state.RunJournal, error) {
					return recorder.replaceRun("DiscardUnreadyGoalSessionAttachDelivery", key, localHandleID)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.DiscardUnreadyGoalSessionAttachDelivery(state.RunKey{}, "")
			},
			wantArgs: []any{state.RunKey{}, ""},
		},
		{
			name: "AbortGoalSessionLaunchBeforeNativeStart",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnAbortGoalSessionLaunchBeforeNativeStart = func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
					return recorder.replaceGoalSession("AbortGoalSessionLaunchBeforeNativeStart", key)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.AbortGoalSessionLaunchBeforeNativeStart(state.GoalSessionKey{})
			},
			wantArgs: []any{state.GoalSessionKey{}},
		},
		{
			name: "MarkGoalSessionUncertain",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnMarkGoalSessionUncertain = func(key state.GoalSessionKey, reason string) (state.GoalSessionJournal, error) {
					return recorder.replaceGoalSession("MarkGoalSessionUncertain", key, reason)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.MarkGoalSessionUncertain(state.GoalSessionKey{}, "")
			},
			wantArgs: []any{state.GoalSessionKey{}, ""},
		},
		{
			name: "MarkCommandAcknowledgementsDelivered",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnMarkCommandAcknowledgementsDelivered = func(key state.RunKey, acknowledgementIDs []string) (state.RunJournal, error) {
					return recorder.replaceRun("MarkCommandAcknowledgementsDelivered", key, acknowledgementIDs)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.MarkCommandAcknowledgementsDelivered(state.RunKey{}, nil)
			},
			wantArgs: []any{state.RunKey{}, []string(nil)},
		},
		{
			name: "QueueTerminalTransitionAndAcknowledgementAt",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueTerminalTransitionAndAcknowledgementAt = func(key state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("QueueTerminalTransitionAndAcknowledgementAt", key, transition, acknowledgement, pendingAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueTerminalTransitionAndAcknowledgementAt(state.RunKey{}, protocol.StateTransitionRequest{}, protocol.CommandAcknowledgement{}, time.Time{})
			},
			wantArgs: []any{state.RunKey{}, protocol.StateTransitionRequest{}, protocol.CommandAcknowledgement{}, time.Time{}},
		},
		{
			name: "QueueCancelledTransitionAndAcknowledgementAt",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueCancelledTransitionAndAcknowledgementAt = func(key state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("QueueCancelledTransitionAndAcknowledgementAt", key, transition, acknowledgement, pendingAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueCancelledTransitionAndAcknowledgementAt(state.RunKey{}, protocol.StateTransitionRequest{}, protocol.CommandAcknowledgement{}, time.Time{})
			},
			wantArgs: []any{state.RunKey{}, protocol.StateTransitionRequest{}, protocol.CommandAcknowledgement{}, time.Time{}},
		},
		{
			name: "RetainWorkspace",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnRetainWorkspace = func(key state.RunKey) (state.RunJournal, error) {
					return recorder.replaceRun("RetainWorkspace", key)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.RetainWorkspace(state.RunKey{})
			},
			wantArgs: []any{state.RunKey{}},
		},
		{
			name: "LoadGoalSession",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnLoadGoalSession = func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
					return recorder.replaceGoalSession("LoadGoalSession", key)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.LoadGoalSession(state.GoalSessionKey{})
			},
			wantArgs: []any{state.GoalSessionKey{}},
		},
		{
			name: "CloseGoalSession",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnCloseGoalSession = func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
					return recorder.replaceGoalSession("CloseGoalSession", key)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.CloseGoalSession(state.GoalSessionKey{})
			},
			wantArgs: []any{state.GoalSessionKey{}},
		},
		{
			name: "ClearProcessDetails",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnClearProcessDetails = func(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
					return recorder.replaceRun("ClearProcessDetails", key, pid, identity)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.ClearProcessDetails(state.RunKey{}, 0, "")
			},
			wantArgs: []any{state.RunKey{}, 0, ""},
		},
	}
}

func TestFaultStoreHookReplacesEveryWrite(t *testing.T) {
	cases := faultStoreWriteCases()
	if len(cases) != 27 {
		t.Fatalf("write case count = %d, want 27", len(cases))
	}
	seen := make(map[string]struct{}, len(cases))
	for _, testCase := range cases {
		if _, exists := seen[testCase.name]; exists {
			t.Fatalf("duplicate write case %q", testCase.name)
		}
		seen[testCase.name] = struct{}{}
		t.Run(testCase.name, func(t *testing.T) {
			store, err := NewFaultStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewFaultStore() error = %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			recorder := newHookRecorder()
			testCase.hook(store, recorder)
			if _, err := testCase.call(store); !errors.Is(err, errFaultStoreHook) {
				t.Fatalf("%s() error = %v, want %v", testCase.name, err, errFaultStoreHook)
			}
			if count := recorder.calls[testCase.name]; count != 1 {
				t.Fatalf("%s hook ran %d times, want 1", testCase.name, count)
			}
			if args := recorder.args[testCase.name]; !reflect.DeepEqual(args, testCase.wantArgs) {
				t.Fatalf("%s hook arguments = %#v, want %#v", testCase.name, args, testCase.wantArgs)
			}
		})
	}
}

func TestFaultStoreNilHookDelegatesEveryWrite(t *testing.T) {
	cases := faultStoreWriteCases()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store, err := NewFaultStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewFaultStore() error = %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			recorder := newHookRecorder()
			for _, other := range cases {
				if other.name != testCase.name {
					other.hook(store, recorder)
				}
			}
			journal, err := testCase.call(store)
			if errors.Is(err, errFaultStoreHook) {
				t.Fatalf("%s() error = %v, want the embedded store result", testCase.name, err)
			}
			directJournal, directErr := directFaultStoreCall(t, store.Store, testCase)
			if !reflect.DeepEqual(journal, directJournal) {
				t.Fatalf("%s() journal = %#v, want embedded store journal %#v", testCase.name, journal, directJournal)
			}
			if errText, directText := faultStoreErrorText(err), faultStoreErrorText(directErr); errText != directText {
				t.Fatalf("%s() error = %v, want embedded store error %v", testCase.name, err, directErr)
			}
			if count := recorder.total(); count != 0 {
				t.Fatalf("%s() invoked %d hook(s) with no hook set, want 0", testCase.name, count)
			}
		})
	}
}

func TestFaultStoreDelegatesToTheEmbeddedStore(t *testing.T) {
	base, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	seed := faultStoreTestJournal("delegation-run", 1)
	if err := base.SaveJournal(seed); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	store := NewFaultStoreFrom(base)
	if store.Store != base {
		t.Fatalf("NewFaultStoreFrom() embedded store = %p, want %p", store.Store, base)
	}
	path := "delegated-workspace"
	got, err := store.SetWorkspacePath(seed.Key(), path)
	if err != nil {
		t.Fatalf("SetWorkspacePath() error = %v", err)
	}
	if got.WorkspacePath != path {
		t.Fatalf("SetWorkspacePath() workspace path = %q, want %q", got.WorkspacePath, path)
	}
	want, err := base.LoadJournal(seed.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if want.WorkspacePath != path {
		t.Fatalf("persisted workspace path = %q, want %q", want.WorkspacePath, path)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SetWorkspacePath() journal = %#v, want persisted journal %#v", got, want)
	}
	again, err := store.LoadJournal(seed.Key())
	if err != nil {
		t.Fatalf("LoadJournal() through FaultStore error = %v", err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("FaultStore journal = %#v, want persisted journal %#v", again, want)
	}
}

func TestFaultStoreHooksAreIndependent(t *testing.T) {
	cases := faultStoreWriteCases()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store, err := NewFaultStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewFaultStore() error = %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			recorder := newHookRecorder()
			testCase.hook(store, recorder)
			if _, err := testCase.call(store); !errors.Is(err, errFaultStoreHook) {
				t.Fatalf("%s() error = %v, want %v", testCase.name, err, errFaultStoreHook)
			}
			for _, other := range cases {
				if other.name == testCase.name {
					continue
				}
				if _, err := other.call(store); errors.Is(err, errFaultStoreHook) {
					t.Fatalf("%s() with only the %s hook set returned %v", other.name, testCase.name, err)
				}
			}
			if count := recorder.total(); count != 1 {
				t.Fatalf("%s hook ran %d times, want 1", testCase.name, count)
			}
		})
	}
}

func directFaultStoreCall(t *testing.T, store *state.Store, testCase faultStoreWriteCase) (any, error) {
	t.Helper()
	method := reflect.ValueOf(store).MethodByName(testCase.name)
	if !method.IsValid() {
		t.Fatalf("state.Store has no method %q", testCase.name)
	}
	args := make([]reflect.Value, 0, len(testCase.wantArgs))
	for _, arg := range testCase.wantArgs {
		args = append(args, reflect.ValueOf(arg))
	}
	results := method.Call(args)
	if len(results) != 2 {
		t.Fatalf("%s returned %d values, want 2", testCase.name, len(results))
	}
	journal := results[0].Interface()
	if results[1].IsNil() {
		return journal, nil
	}
	return journal, results[1].Interface().(error)
}

func faultStoreErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func faultStoreTestJournal(runID string, generation int64) state.RunJournal {
	return state.RunJournal{
		RunID: runID, Generation: generation, RuntimeKey: "local", RuntimeID: "runtime-1", ClaimedRuntimeEpoch: 3,
		ClaimID: "claim-1", LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC),
		LocalState:    "running",
		Work:          protocol.Work{Goal: "implement", AgentProfile: "codex", Workspace: "isolated", Input: json.RawMessage(`{}`)},
		WorkspacePath: "seed-workspace", WorkspaceBindingKey: "binding-1",
		PID: 42, ProcessIdentity: "windows:42:created-at", StartedAt: time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC),
		LastEventSequence: 1,
	}
}
