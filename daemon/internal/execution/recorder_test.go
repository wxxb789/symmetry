package execution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	noopRecoverySeal := func(authority.Supervisor, uint64) error { return nil }
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
		RecordSupervisorRecoveryScanSeal:   noopRecoverySeal,
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
		RecordSupervisorRecoveryScanSeal:   true,
	}
	if capabilities := writes.Capabilities(); capabilities != want {
		t.Fatalf("complete recorder capabilities = %+v, want %+v", capabilities, want)
	}
	if capabilities := NewRecorder(writes).Capabilities(); capabilities != want {
		t.Fatalf("port capabilities = %+v, want %+v", capabilities, want)
	}
}

func TestRecorderFuncsEveryAbsentWriteFailsClosed(t *testing.T) {
	recorder := NewRecorder(RecorderFuncs{})
	handoff := authority.SupervisorHandoff{}
	receipt := authority.StopReceipt{}
	proof := authority.SupervisorHandoffReleaseProof{}
	value := &authority.Supervisor{}
	checks := []struct {
		name string
		call func() error
	}{
		{name: "PrepareSupervisorHandoff", call: func() error { return recorder.PrepareSupervisorHandoff(handoff) }},
		{name: "BindSupervisorHandoff", call: func() error { return recorder.BindSupervisorHandoff(handoff, 1, "created:1") }},
		{name: "CommitSupervisorHandoff", call: func() error { return recorder.CommitSupervisorHandoff(handoff, time.Time{}) }},
		{name: "RecordSupervisorHandoffStopReceipt", call: func() error { return recorder.RecordSupervisorHandoffStopReceipt(handoff, receipt) }},
		{name: "ClearSupervisorHandoff", call: func() error { return recorder.ClearSupervisorHandoff(handoff, proof) }},
		{name: "PersistProcess", call: func() error { return recorder.PersistProcess(1, "created:1") }},
		{name: "PersistProcessWithAuthority", call: func() error { return recorder.PersistProcessWithAuthority(1, "created:1", value) }},
		{name: "PersistProcessAuthority", call: func() error { return recorder.PersistProcessAuthority(1, "created:1", value) }},
		{name: "PersistContainmentStopReceipt", call: func() error { return recorder.PersistContainmentStopReceipt(1, "created:1", receipt) }},
		{name: "PersistContainmentUnproven", call: func() error { return recorder.PersistContainmentUnproven(1, "created:1") }},
		{name: "RecordSupervisorRecoveryScanSeal", call: func() error {
			return recorder.RecordSupervisorRecoveryScanSeal(authority.Supervisor{}, 2)
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, errRecorderWriteUnavailable) {
				t.Fatalf("absent %s error = %v, want errRecorderWriteUnavailable", check.name, err)
			}
		})
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

func (recorder *capabilityRecorder) RecordSupervisorRecoveryScanSeal(authority.Supervisor, uint64) error {
	recorder.note("recovery-seal")
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
		RecordSupervisorRecoveryScanSeal:   true,
	}}
	writes := recorderWrites(complete)
	if writes.PrepareSupervisorHandoff == nil || writes.BindSupervisorHandoff == nil ||
		writes.CommitSupervisorHandoff == nil || writes.RecordSupervisorHandoffStopReceipt == nil ||
		writes.ClearSupervisorHandoff == nil || writes.PersistProcess == nil ||
		writes.PersistProcessWithAuthority == nil || writes.PersistProcessAuthority == nil ||
		writes.PersistContainmentStopReceipt == nil || writes.PersistContainmentUnproven == nil ||
		writes.RecordSupervisorRecoveryScanSeal == nil {
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
		{"recovery-seal", func() error { return writes.RecordSupervisorRecoveryScanSeal(authority.Supervisor{}, 2) }},
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
		sparse.PersistContainmentUnproven != nil || sparse.RecordSupervisorRecoveryScanSeal != nil {
		t.Fatal("a false capability was mapped to a write")
	}
}

func TestRecorderWritesLaunchExactlyLikeTheFrozenMembers(t *testing.T) {
	for _, test := range recorderParityCases() {
		t.Run(test.name, func(t *testing.T) {
			frozen := runRecorderParityLaunch(t, test, false)
			port := runRecorderParityLaunch(t, test, true)
			checkRecorderParityEvents(t, frozen.events, test)
			checkRecorderParityEvents(t, port.events, test)
			if test.retriedFailure {
				t.Logf("failing authority write attempts: frozen=%d recorder=%d", len(frozen.events), len(port.events))
				requireFailedAuthorityStop(t, "frozen", frozen)
				requireFailedAuthorityStop(t, "recorder", port)
			}
			// The permanently failing shape records the failing attempt's
			// error through whichever finalization call Terminate joined, so
			// its error text, like its attempt count, is a scheduling artifact;
			// requireFailedAuthorityStop pins both representations' failures.
			if !test.retriedFailure {
				if errorString(frozen.startErr) != errorString(port.startErr) {
					t.Fatalf("start errors differ: frozen %v, recorder %v", frozen.startErr, port.startErr)
				}
				if errorString(frozen.result.ContainmentError) != errorString(port.result.ContainmentError) {
					t.Fatalf("containment errors differ: frozen %v, recorder %v", frozen.result.ContainmentError, port.result.ContainmentError)
				}
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

// checkRecorderParityEvents pins one representation's durable-write sequence.
// A permanently failing write is retried once per finalization call, and a
// failed finalization stays retryable by design, so the attempt count is a
// scheduling artifact within [2,3]; that shape pins every attempt's identity
// (and therefore the absence of every other write) plus the retry floor
// instead of the sequence length.
func checkRecorderParityEvents(t *testing.T, events []string, test recorderParityCase) {
	t.Helper()
	if !test.retriedFailure {
		if !slices.Equal(events, test.wantEvents) {
			t.Fatalf("write sequence = %#v, want %#v", events, test.wantEvents)
		}
		return
	}
	if len(test.wantEvents) != 1 {
		t.Fatalf("a retried-failure shape expects exactly one repeated write, got %#v", test.wantEvents)
	}
	if len(events) < 2 || len(events) > 3 {
		t.Fatalf("failing write attempted %d times, want the first attempt and one or two finalizer retries", len(events))
	}
	for _, event := range events {
		if event != test.wantEvents[0] {
			t.Fatalf("write %q, want only %q", event, test.wantEvents[0])
		}
	}
}

func requireFailedAuthorityStop(t *testing.T, label string, outcome recorderParityOutcome) {
	t.Helper()
	if !strings.Contains(errorString(outcome.startErr), parityWriteFailureText) {
		t.Fatalf("%s start error = %v, want the failing durable write", label, outcome.startErr)
	}
	if outcome.result.Success() {
		t.Fatalf("%s reported success after a permanently failing authority write", label)
	}
	if !strings.Contains(errorString(outcome.result.ContainmentError), parityWriteFailureText) {
		t.Fatalf("%s containment error = %v, want the failing durable write", label, outcome.result.ContainmentError)
	}
	if outcome.cleanup.closeCalls != 1 || outcome.cleanup.releaseCalls != 0 || outcome.cleanup.abortCalls != 0 {
		t.Fatalf("%s cleanup = %+v, want one close and no release or abort", label, outcome.cleanup)
	}
}

type recorderParityCase struct {
	name     string
	writes   RecorderFuncs
	handoff  bool
	unproven bool
	// retriedFailure marks a shape whose write fails on every attempt; its
	// wantEvents names the single repeated attempt (§ checkRecorderParityEvents).
	retriedFailure bool
	wantEvents     []string
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

const parityCloseFailureText = "containment close failed on purpose"

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
			retriedFailure: true,
			wantEvents: []string{
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
		{
			name: "atomic write wins over the production marker and authority pair",
			writes: RecorderFuncs{
				PersistProcess:                func(int, string) error { return nil },
				PersistProcessWithAuthority:   func(int, string, *authority.Supervisor) error { return nil },
				PersistProcessAuthority:       func(int, string, *authority.Supervisor) error { return nil },
				PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error { return nil },
			},
			wantEvents: []string{
				"persist-process-with-authority(4242,fake:4242)",
				"persist-containment-stop-receipt(4242,fake:4242)",
			},
		},
		{
			name: "supervisor handoff commits before the stop receipt",
			writes: RecorderFuncs{
				CommitSupervisorHandoff:       func(authority.SupervisorHandoff, time.Time) error { return nil },
				PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error { return nil },
			},
			handoff: true,
			wantEvents: []string{
				"commit-supervisor-handoff(4242,fake:4242,2026-11-22T05:06:07Z)",
				"persist-containment-stop-receipt(4242,fake:4242)",
			},
		},
		{
			name: "unproven containment persists after a failed close",
			writes: RecorderFuncs{
				PersistProcess:                func(int, string) error { return nil },
				PersistProcessAuthority:       func(int, string, *authority.Supervisor) error { return nil },
				PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error { return nil },
				PersistContainmentUnproven:    func(int, string) error { return nil },
			},
			unproven: true,
			wantEvents: []string{
				"persist-process(4242,fake:4242)",
				"persist-process-authority(4242,fake:4242)",
				"persist-containment-unproven(4242,fake:4242)",
			},
		},
	}
}

// parityUnprovenContainment fails every close and keeps the runner's
// uncertainty observer, so finalization must persist the unproven marker once.
type parityUnprovenContainment struct {
	*p1AuthorityContainment
	mutex            sync.Mutex
	unprovenCallback func() error
	closeCalls       int
}

func (containment *parityUnprovenContainment) SetContainmentUnprovenCallback(callback func() error) error {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	containment.unprovenCallback = callback
	return nil
}

func (containment *parityUnprovenContainment) Close() error {
	containment.mutex.Lock()
	containment.closeCalls++
	containment.mutex.Unlock()
	return errors.New(parityCloseFailureText)
}

func parityHandoffFixture() authority.SupervisorHandoff {
	return authority.SupervisorHandoff{
		Version:            authority.SupervisorHandoffVersion,
		LaunchToken:        strings.Repeat("a", authority.TokenBytes*2),
		Secret:             strings.Repeat("b", authority.SecretBytes*2),
		TargetPID:          4242,
		TargetIdentity:     "fake:4242",
		PipeToken:          strings.Repeat("c", authority.TokenBytes*2),
		JobID:              strings.Repeat("d", authority.TokenBytes*2),
		SupervisorPID:      99,
		SupervisorIdentity: "helper:99",
	}
}

func runRecorderParityLaunch(t *testing.T, test recorderParityCase, useRecorder bool) recorderParityOutcome {
	t.Helper()
	containment := newP1AuthorityContainment()
	unproven := &parityUnprovenContainment{p1AuthorityContainment: containment}
	events := []string{}
	record := func(write string) { events = append(events, write) }
	recorded := test.writes
	recorded.PersistProcess = recordProcessWrite(record, "persist-process", test.writes.PersistProcess)
	recorded.PersistProcessWithAuthority = recordAuthorityWrite(record, "persist-process-with-authority", test.writes.PersistProcessWithAuthority)
	recorded.PersistProcessAuthority = recordAuthorityWrite(record, "persist-process-authority", test.writes.PersistProcessAuthority)
	recorded.PersistContainmentStopReceipt = recordStopWrite(record, "persist-containment-stop-receipt", test.writes.PersistContainmentStopReceipt)
	recorded.CommitSupervisorHandoff = recordCommitWrite(record, "commit-supervisor-handoff", test.writes.CommitSupervisorHandoff)
	recorded.PersistContainmentUnproven = recordProcessWrite(record, "persist-containment-unproven", test.writes.PersistContainmentUnproven)
	invocation := helperInvocation("args")
	if useRecorder {
		invocation.Recorder = NewRecorder(recorded)
	} else {
		invocation.PersistProcess = recorded.PersistProcess
		invocation.PersistProcessWithAuthority = recorded.PersistProcessWithAuthority
		invocation.PersistProcessAuthority = recorded.PersistProcessAuthority
		invocation.PersistContainmentStopReceipt = recorded.PersistContainmentStopReceipt
		invocation.CommitSupervisorHandoff = recorded.CommitSupervisorHandoff
		invocation.PersistContainmentUnproven = recorded.PersistContainmentUnproven
	}
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(_ context.Context, command *exec.Cmd, _ Invocation, _, _, _ *os.File) (*startedProcess, error) {
			started := &startedProcess{
				command:     command,
				pid:         4242,
				identity:    "fake:4242",
				containment: containment,
				wait:        func() (int, error) { return 0, nil },
				kill:        func() error { return nil },
				close:       func() error { return containment.Close() },
			}
			if test.unproven {
				started.containment = unproven
				started.close = unproven.Close
			}
			if test.handoff {
				value := parityHandoffFixture()
				started.containmentHandoff = &value
				started.containmentHandoffStartedAt = time.Date(2026, 11, 22, 5, 6, 7, 0, time.UTC)
			}
			return started, nil
		},
	}
	process, startErr := runner.Start(context.Background(), invocation, &recordingSink{})
	if process == nil {
		t.Fatalf("launch returned no process: %v", startErr)
	}
	result := waitForResult(t, process)
	cleanup := recorderParityCleanup{
		closeCalls:   containment.closeCalls,
		releaseCalls: containment.releaseCalls,
		abortCalls:   containment.abortCalls,
	}
	if test.unproven {
		cleanup.closeCalls = unproven.closeCalls
	}
	return recorderParityOutcome{
		events:   events,
		result:   result,
		startErr: startErr,
		cleanup:  cleanup,
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

func recordCommitWrite(record func(string), name string, write func(authority.SupervisorHandoff, time.Time) error) func(authority.SupervisorHandoff, time.Time) error {
	if write == nil {
		return nil
	}
	return func(handoff authority.SupervisorHandoff, startedAt time.Time) error {
		record(name + "(" + strconv.Itoa(handoff.TargetPID) + "," + handoff.TargetIdentity + "," + startedAt.UTC().Format(time.RFC3339Nano) + ")")
		return write(handoff, startedAt)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
