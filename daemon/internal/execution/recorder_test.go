package execution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestRecorderHandoffCapabilityMatchesPresenceGates(t *testing.T) {
	prepare := func(authority.SupervisorHandoff) error { return nil }
	bind := func(authority.SupervisorHandoff, int, string) error { return nil }
	commit := func(authority.SupervisorHandoff, time.Time) error { return nil }
	receipt := func(authority.SupervisorHandoff, authority.StopReceipt) error { return nil }
	clear := func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error { return nil }
	for mask := range 32 {
		writes := RecorderFuncs{}
		if mask&1 != 0 {
			writes.PrepareSupervisorHandoff = prepare
		}
		if mask&2 != 0 {
			writes.BindSupervisorHandoff = bind
		}
		if mask&4 != 0 {
			writes.CommitSupervisorHandoff = commit
		}
		if mask&8 != 0 {
			writes.RecordSupervisorHandoffStopReceipt = receipt
		}
		if mask&16 != 0 {
			writes.ClearSupervisorHandoff = clear
		}
		mirrors := Invocation{
			PrepareSupervisorHandoff:           writes.PrepareSupervisorHandoff,
			BindSupervisorHandoff:              writes.BindSupervisorHandoff,
			CommitSupervisorHandoff:            writes.CommitSupervisorHandoff,
			RecordSupervisorHandoffStopReceipt: writes.RecordSupervisorHandoffStopReceipt,
			ClearSupervisorHandoff:             writes.ClearSupervisorHandoff,
		}
		requested := mirrors.PrepareSupervisorHandoff != nil ||
			mirrors.BindSupervisorHandoff != nil ||
			mirrors.CommitSupervisorHandoff != nil ||
			mirrors.RecordSupervisorHandoffStopReceipt != nil ||
			mirrors.ClearSupervisorHandoff != nil
		complete := mirrors.PrepareSupervisorHandoff != nil &&
			mirrors.BindSupervisorHandoff != nil &&
			mirrors.CommitSupervisorHandoff != nil
		frozen := invocationWrites(mirrors).Capabilities()
		port := invocationWrites(Invocation{Recorder: NewRecorder(writes)}).Capabilities()
		if frozen.SupervisorHandoffRequested() != requested || frozen.SupervisorHandoffComplete() != complete {
			t.Errorf("mask %05b: frozen members decided requested=%t complete=%t, want requested=%t complete=%t",
				mask, frozen.SupervisorHandoffRequested(), frozen.SupervisorHandoffComplete(), requested, complete)
		}
		if port.SupervisorHandoffRequested() != requested || port.SupervisorHandoffComplete() != complete {
			t.Errorf("mask %05b: recorder decided requested=%t complete=%t, want requested=%t complete=%t",
				mask, port.SupervisorHandoffRequested(), port.SupervisorHandoffComplete(), requested, complete)
		}
		t.Logf("mask %05b requested=%t complete=%t", mask, requested, complete)
	}
}

func TestRecorderFuncsCapabilitiesMatchPresentWritesAndFailClosed(t *testing.T) {
	if capabilities := (RecorderFuncs{}).Capabilities(); capabilities != (RecorderCapabilities{}) {
		t.Fatalf("empty recorder capabilities = %+v, want the zero set", capabilities)
	}
	if err := NewRecorder(RecorderFuncs{}).PersistContainmentUnproven(1, "created:1"); !errors.Is(err, errRecorderWriteUnavailable) {
		t.Fatalf("absent write error = %v, want errRecorderWriteUnavailable", err)
	}
	noopHandoff := func(authority.SupervisorHandoff) error { return nil }
	noopBind := func(authority.SupervisorHandoff, int, string) error { return nil }
	noopCommit := func(authority.SupervisorHandoff, time.Time) error { return nil }
	noopReceipt := func(authority.SupervisorHandoff, authority.StopReceipt) error { return nil }
	noopClear := func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error { return nil }
	noopProcess := func(int, string) error { return nil }
	noopAuthority := func(int, string, *authority.Supervisor) error { return nil }
	noopStopReceipt := func(int, string, authority.StopReceipt) error { return nil }
	writes := RecorderFuncs{
		PrepareSupervisorHandoff:           noopHandoff,
		BindSupervisorHandoff:              noopBind,
		CommitSupervisorHandoff:            noopCommit,
		RecordSupervisorHandoffStopReceipt: noopReceipt,
		ClearSupervisorHandoff:             noopClear,
		PersistProcess:                     noopProcess,
		PersistProcessWithAuthority:        noopAuthority,
		PersistProcessAuthority:            noopAuthority,
		PersistContainmentStopReceipt:      noopStopReceipt,
		PersistContainmentUnproven:         noopProcess,
	}
	want := RecorderCapabilities{
		PrepareSupervisorHandoff:           true,
		BindSupervisorHandoff:              true,
		CommitSupervisorHandoff:            true,
		RecordSupervisorHandoffStopReceipt: true,
		ClearSupervisorHandoff:             true,
		PersistProcess:                     true,
		PersistProcessWithAuthority:        true,
		PersistProcessAuthority:            true,
		PersistContainmentStopReceipt:      true,
		PersistContainmentUnproven:         true,
	}
	if capabilities := writes.Capabilities(); capabilities != want {
		t.Fatalf("complete recorder capabilities = %+v, want %+v", capabilities, want)
	}
	if capabilities := NewRecorder(writes).Capabilities(); capabilities != want {
		t.Fatalf("port capabilities = %+v, want %+v", capabilities, want)
	}
}

func TestInvocationWritesReadTheRecorderBeforeTheFrozenMembers(t *testing.T) {
	var called []string
	frozen := func(int, string) error {
		called = append(called, "frozen")
		return nil
	}
	invocation := Invocation{
		Recorder: NewRecorder(RecorderFuncs{PersistProcess: func(int, string) error {
			called = append(called, "recorder")
			return nil
		}}),
		PersistProcess: frozen,
	}
	if err := invocationWrites(invocation).PersistProcess(7, "created:7"); err != nil {
		t.Fatalf("recorder write error = %v", err)
	}
	if !slices.Equal(called, []string{"recorder"}) {
		t.Fatalf("writes = %#v, want the recorder write", called)
	}
	if err := invocationWrites(Invocation{PersistProcess: frozen}).PersistProcess(7, "created:7"); err != nil {
		t.Fatalf("frozen write error = %v", err)
	}
	if !slices.Equal(called, []string{"recorder", "frozen"}) {
		t.Fatalf("writes = %#v, want the frozen member write second", called)
	}
	hidden := Invocation{Recorder: NewRecorder(RecorderFuncs{}), PersistProcess: frozen}
	if writes := invocationWrites(hidden); writes.PersistProcess != nil {
		t.Fatal("a recorder with no process write must hide the frozen member")
	}
}

func TestMirrorRecorderMembersCallsTheSameRecorderMethods(t *testing.T) {
	var calls []string
	writes := RecorderFuncs{
		PrepareSupervisorHandoff: func(authority.SupervisorHandoff) error { calls = append(calls, "prepare"); return nil },
		BindSupervisorHandoff:    func(authority.SupervisorHandoff, int, string) error { calls = append(calls, "bind"); return nil },
		CommitSupervisorHandoff:  func(authority.SupervisorHandoff, time.Time) error { calls = append(calls, "commit"); return nil },
		RecordSupervisorHandoffStopReceipt: func(authority.SupervisorHandoff, authority.StopReceipt) error {
			calls = append(calls, "receipt")
			return nil
		},
		ClearSupervisorHandoff: func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error {
			calls = append(calls, "clear")
			return nil
		},
		PersistProcess:                func(int, string) error { calls = append(calls, "marker"); return nil },
		PersistProcessWithAuthority:   func(int, string, *authority.Supervisor) error { calls = append(calls, "atomic"); return nil },
		PersistProcessAuthority:       func(int, string, *authority.Supervisor) error { calls = append(calls, "authority"); return nil },
		PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error { calls = append(calls, "stop"); return nil },
		PersistContainmentUnproven:    func(int, string) error { calls = append(calls, "unproven"); return nil },
	}
	invocation := Invocation{Recorder: NewRecorder(writes)}
	invocation.MirrorRecorderMembers()
	members := []struct {
		name string
		call func() error
	}{
		{"prepare", func() error { return invocation.PrepareSupervisorHandoff(authority.SupervisorHandoff{}) }},
		{"bind", func() error { return invocation.BindSupervisorHandoff(authority.SupervisorHandoff{}, 7, "bound:7") }},
		{"commit", func() error {
			return invocation.CommitSupervisorHandoff(authority.SupervisorHandoff{}, time.Unix(1, 0))
		}},
		{"receipt", func() error {
			return invocation.RecordSupervisorHandoffStopReceipt(authority.SupervisorHandoff{}, authority.StopReceipt{})
		}},
		{"clear", func() error {
			return invocation.ClearSupervisorHandoff(authority.SupervisorHandoff{}, authority.SupervisorHandoffReleaseProof{})
		}},
		{"marker", func() error { return invocation.PersistProcess(7, "created:7") }},
		{"atomic", func() error { return invocation.PersistProcessWithAuthority(7, "created:7", nil) }},
		{"authority", func() error { return invocation.PersistProcessAuthority(7, "created:7", nil) }},
		{"stop", func() error { return invocation.PersistContainmentStopReceipt(7, "created:7", authority.StopReceipt{}) }},
		{"unproven", func() error { return invocation.PersistContainmentUnproven(7, "created:7") }},
	}
	for _, member := range members {
		calls = nil
		if err := member.call(); err != nil {
			t.Fatalf("frozen member %s error = %v", member.name, err)
		}
		if !slices.Equal(calls, []string{member.name}) {
			t.Fatalf("frozen member %s reached %#v, want the same recorder method", member.name, calls)
		}
	}
}

func TestMirrorRecorderMembersKeepsAbsentWritesNil(t *testing.T) {
	var calls []string
	invocation := Invocation{Recorder: NewRecorder(RecorderFuncs{
		PersistProcess: func(int, string) error {
			calls = append(calls, "marker")
			return nil
		},
	})}
	invocation.MirrorRecorderMembers()
	if invocation.PrepareSupervisorHandoff != nil || invocation.BindSupervisorHandoff != nil ||
		invocation.CommitSupervisorHandoff != nil || invocation.RecordSupervisorHandoffStopReceipt != nil ||
		invocation.ClearSupervisorHandoff != nil || invocation.PersistProcessWithAuthority != nil ||
		invocation.PersistProcessAuthority != nil || invocation.PersistContainmentStopReceipt != nil ||
		invocation.PersistContainmentUnproven != nil {
		t.Fatal("an absent write was mirrored as present")
	}
	if err := invocation.PersistProcess(7, "created:7"); err != nil {
		t.Fatalf("mirrored write error = %v", err)
	}
	if !slices.Equal(calls, []string{"marker"}) {
		t.Fatalf("mirrored write reached %#v, want the recorder method", calls)
	}
}

type capabilityRecorder struct {
	capabilities RecorderCapabilities
	calls        []string
}

func (recorder *capabilityRecorder) Capabilities() RecorderCapabilities { return recorder.capabilities }

func (recorder *capabilityRecorder) note(name string) { recorder.calls = append(recorder.calls, name) }

func (recorder *capabilityRecorder) PrepareSupervisorHandoff(authority.SupervisorHandoff) error {
	recorder.note("prepare")
	return nil
}

func (recorder *capabilityRecorder) BindSupervisorHandoff(authority.SupervisorHandoff, int, string) error {
	recorder.note("bind")
	return nil
}

func (recorder *capabilityRecorder) CommitSupervisorHandoff(authority.SupervisorHandoff, time.Time) error {
	recorder.note("commit")
	return nil
}

func (recorder *capabilityRecorder) RecordSupervisorHandoffStopReceipt(authority.SupervisorHandoff, authority.StopReceipt) error {
	recorder.note("receipt")
	return nil
}

func (recorder *capabilityRecorder) ClearSupervisorHandoff(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error {
	recorder.note("clear")
	return nil
}

func (recorder *capabilityRecorder) PersistProcess(int, string) error {
	recorder.note("marker")
	return nil
}

func (recorder *capabilityRecorder) PersistProcessWithAuthority(int, string, *authority.Supervisor) error {
	recorder.note("atomic")
	return nil
}

func (recorder *capabilityRecorder) PersistProcessAuthority(int, string, *authority.Supervisor) error {
	recorder.note("authority")
	return nil
}

func (recorder *capabilityRecorder) PersistContainmentStopReceipt(int, string, authority.StopReceipt) error {
	recorder.note("stop")
	return nil
}

func (recorder *capabilityRecorder) PersistContainmentUnproven(int, string) error {
	recorder.note("unproven")
	return nil
}

func TestRecorderWritesMapsEveryCapabilityToItsMethod(t *testing.T) {
	complete := &capabilityRecorder{capabilities: RecorderCapabilities{
		PrepareSupervisorHandoff:           true,
		BindSupervisorHandoff:              true,
		CommitSupervisorHandoff:            true,
		RecordSupervisorHandoffStopReceipt: true,
		ClearSupervisorHandoff:             true,
		PersistProcess:                     true,
		PersistProcessWithAuthority:        true,
		PersistProcessAuthority:            true,
		PersistContainmentStopReceipt:      true,
		PersistContainmentUnproven:         true,
	}}
	writes := recorderWrites(complete)
	if writes.PrepareSupervisorHandoff == nil || writes.BindSupervisorHandoff == nil ||
		writes.CommitSupervisorHandoff == nil || writes.RecordSupervisorHandoffStopReceipt == nil ||
		writes.ClearSupervisorHandoff == nil || writes.PersistProcess == nil ||
		writes.PersistProcessWithAuthority == nil || writes.PersistProcessAuthority == nil ||
		writes.PersistContainmentStopReceipt == nil || writes.PersistContainmentUnproven == nil {
		t.Fatal("a true capability was left unmapped")
	}
	members := []struct {
		name string
		call func() error
	}{
		{"prepare", func() error { return writes.PrepareSupervisorHandoff(authority.SupervisorHandoff{}) }},
		{"bind", func() error { return writes.BindSupervisorHandoff(authority.SupervisorHandoff{}, 7, "bound:7") }},
		{"commit", func() error { return writes.CommitSupervisorHandoff(authority.SupervisorHandoff{}, time.Unix(1, 0)) }},
		{"receipt", func() error {
			return writes.RecordSupervisorHandoffStopReceipt(authority.SupervisorHandoff{}, authority.StopReceipt{})
		}},
		{"clear", func() error {
			return writes.ClearSupervisorHandoff(authority.SupervisorHandoff{}, authority.SupervisorHandoffReleaseProof{})
		}},
		{"marker", func() error { return writes.PersistProcess(7, "created:7") }},
		{"atomic", func() error { return writes.PersistProcessWithAuthority(7, "created:7", nil) }},
		{"authority", func() error { return writes.PersistProcessAuthority(7, "created:7", nil) }},
		{"stop", func() error { return writes.PersistContainmentStopReceipt(7, "created:7", authority.StopReceipt{}) }},
		{"unproven", func() error { return writes.PersistContainmentUnproven(7, "created:7") }},
	}
	for _, member := range members {
		complete.calls = nil
		if err := member.call(); err != nil {
			t.Fatalf("fallback write %s error = %v", member.name, err)
		}
		if !slices.Equal(complete.calls, []string{member.name}) {
			t.Fatalf("fallback write %s reached %#v, want its own method", member.name, complete.calls)
		}
	}
	sparse := recorderWrites(&capabilityRecorder{capabilities: RecorderCapabilities{PersistProcess: true}})
	if sparse.PersistProcess == nil {
		t.Fatal("a true capability was dropped")
	}
	if sparse.PrepareSupervisorHandoff != nil || sparse.BindSupervisorHandoff != nil ||
		sparse.CommitSupervisorHandoff != nil || sparse.RecordSupervisorHandoffStopReceipt != nil ||
		sparse.ClearSupervisorHandoff != nil || sparse.PersistProcessWithAuthority != nil ||
		sparse.PersistProcessAuthority != nil || sparse.PersistContainmentStopReceipt != nil ||
		sparse.PersistContainmentUnproven != nil {
		t.Fatal("a false capability was mapped to a write")
	}
}

func TestRecorderWritesLaunchExactlyLikeTheFrozenMembers(t *testing.T) {
	for _, test := range recorderParityCases() {
		t.Run(test.name, func(t *testing.T) {
			frozen := runRecorderParityLaunch(t, test.writes, false)
			port := runRecorderParityLaunch(t, test.writes, true)
			if !slices.Equal(frozen.events, port.events) {
				t.Fatalf("write sequences differ: frozen %#v, recorder %#v", frozen.events, port.events)
			}
			if !slices.Equal(frozen.events, test.wantEvents) {
				t.Fatalf("write sequence = %#v, want %#v", frozen.events, test.wantEvents)
			}
			if errorString(frozen.startErr) != errorString(port.startErr) {
				t.Fatalf("start errors differ: frozen %v, recorder %v", frozen.startErr, port.startErr)
			}
			if errorString(frozen.result.ContainmentError) != errorString(port.result.ContainmentError) {
				t.Fatalf("containment errors differ: frozen %v, recorder %v", frozen.result.ContainmentError, port.result.ContainmentError)
			}
			if frozen.result.Success() != port.result.Success() || frozen.result.ExitCode != port.result.ExitCode {
				t.Fatalf("results differ: frozen %#v, recorder %#v", frozen.result, port.result)
			}
			if frozen.cleanup != port.cleanup {
				t.Fatalf("containment cleanup differs: frozen %#v, recorder %#v", frozen.cleanup, port.cleanup)
			}
		})
	}
}

type recorderParityCase struct {
	name       string
	writes     RecorderFuncs
	wantEvents []string
}

type recorderParityOutcome struct {
	events   []string
	result   Result
	startErr error
	cleanup  recorderParityCleanup
}

type recorderParityCleanup struct {
	closeCalls   int
	releaseCalls int
	abortCalls   int
}

const parityWriteFailureText = "durable write failed on purpose"

func recorderParityCases() []recorderParityCase {
	fail := errors.New(parityWriteFailureText)
	return []recorderParityCase{
		{
			name: "process marker and legacy authority",
			writes: RecorderFuncs{
				PersistProcess:                func(int, string) error { return nil },
				PersistProcessAuthority:       func(int, string, *authority.Supervisor) error { return nil },
				PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error { return nil },
			},
			wantEvents: []string{
				"persist-process(4242,fake:4242)",
				"persist-process-authority(4242,fake:4242)",
				"persist-containment-stop-receipt(4242,fake:4242)",
			},
		},
		{
			name: "atomic process and containment authority",
			writes: RecorderFuncs{
				PersistProcessWithAuthority:   func(int, string, *authority.Supervisor) error { return nil },
				PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error { return nil },
			},
			wantEvents: []string{
				"persist-process-with-authority(4242,fake:4242)",
				"persist-containment-stop-receipt(4242,fake:4242)",
			},
		},
		{
			name: "atomic write failure stops the launch",
			writes: RecorderFuncs{
				PersistProcessWithAuthority:   func(int, string, *authority.Supervisor) error { return fail },
				PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error { return nil },
			},
			wantEvents: []string{
				"persist-process-with-authority(4242,fake:4242)",
				"persist-process-with-authority(4242,fake:4242)",
			},
		},
		{
			name: "marker without an authority write keeps the marker",
			writes: RecorderFuncs{
				PersistProcess:                func(int, string) error { return nil },
				PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error { return nil },
			},
			wantEvents: []string{
				"persist-process(4242,fake:4242)",
				"persist-containment-stop-receipt(4242,fake:4242)",
			},
		},
		{
			name:       "no durable write",
			writes:     RecorderFuncs{},
			wantEvents: []string{},
		},
	}
}

func runRecorderParityLaunch(t *testing.T, writes RecorderFuncs, useRecorder bool) recorderParityOutcome {
	t.Helper()
	containment := newP1AuthorityContainment()
	events := []string{}
	record := func(write string) { events = append(events, write) }
	recorded := writes
	recorded.PersistProcess = recordProcessWrite(record, "persist-process", writes.PersistProcess)
	recorded.PersistProcessWithAuthority = recordAuthorityWrite(record, "persist-process-with-authority", writes.PersistProcessWithAuthority)
	recorded.PersistProcessAuthority = recordAuthorityWrite(record, "persist-process-authority", writes.PersistProcessAuthority)
	recorded.PersistContainmentStopReceipt = recordStopWrite(record, "persist-containment-stop-receipt", writes.PersistContainmentStopReceipt)
	invocation := helperInvocation("args")
	if useRecorder {
		invocation.Recorder = NewRecorder(recorded)
	} else {
		invocation.PersistProcess = recorded.PersistProcess
		invocation.PersistProcessWithAuthority = recorded.PersistProcessWithAuthority
		invocation.PersistProcessAuthority = recorded.PersistProcessAuthority
		invocation.PersistContainmentStopReceipt = recorded.PersistContainmentStopReceipt
	}
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(_ context.Context, command *exec.Cmd, _ Invocation, _, _, _ *os.File) (*startedProcess, error) {
			return &startedProcess{
				command:     command,
				pid:         4242,
				identity:    "fake:4242",
				containment: containment,
				wait:        func() (int, error) { return 0, nil },
				kill:        func() error { return nil },
				close:       func() error { return containment.Close() },
			}, nil
		},
	}
	process, startErr := runner.Start(context.Background(), invocation, &recordingSink{})
	if process == nil {
		t.Fatalf("launch returned no process: %v", startErr)
	}
	result := waitForResult(t, process)
	return recorderParityOutcome{
		events:   events,
		result:   result,
		startErr: startErr,
		cleanup: recorderParityCleanup{
			closeCalls:   containment.closeCalls,
			releaseCalls: containment.releaseCalls,
			abortCalls:   containment.abortCalls,
		},
	}
}

func recordProcessWrite(record func(string), name string, write func(int, string) error) func(int, string) error {
	if write == nil {
		return nil
	}
	return func(pid int, identity string) error {
		record(name + "(" + strconv.Itoa(pid) + "," + identity + ")")
		return write(pid, identity)
	}
}

func recordAuthorityWrite(record func(string), name string, write func(int, string, *authority.Supervisor) error) func(int, string, *authority.Supervisor) error {
	if write == nil {
		return nil
	}
	return func(pid int, identity string, value *authority.Supervisor) error {
		record(name + "(" + strconv.Itoa(pid) + "," + identity + ")")
		return write(pid, identity, value)
	}
}

func recordStopWrite(record func(string), name string, write func(int, string, authority.StopReceipt) error) func(int, string, authority.StopReceipt) error {
	if write == nil {
		return nil
	}
	return func(pid int, identity string, receipt authority.StopReceipt) error {
		record(name + "(" + strconv.Itoa(pid) + "," + identity + ")")
		return write(pid, identity, receipt)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
