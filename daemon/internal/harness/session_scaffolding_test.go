package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type scaffoldingProcess struct {
	pid       int
	identity  string
	renewable bool
	renewals  []uint64
	status    execution.LinuxV2LiveOwnerStatus
	statusErr error
}

func (process *scaffoldingProcess) ProcessDetails() (int, string) {
	return process.pid, process.identity
}

func (process *scaffoldingProcess) LeaseRenewalAvailable() bool { return process.renewable }

func (process *scaffoldingProcess) RenewLease(_ time.Duration, sequence uint64) error {
	process.renewals = append(process.renewals, sequence)
	return nil
}

func (process *scaffoldingProcess) LinuxV2LiveOwnerStatus(context.Context) (execution.LinuxV2LiveOwnerStatus, error) {
	return process.status, process.statusErr
}

type scaffoldingIdentity struct{}

func (scaffoldingIdentity) ProcessDetails() (int, string) { return 7, "identity-only" }

type scaffoldingSink struct {
	mutex  sync.Mutex
	events []Event
	err    error
}

func (sink *scaffoldingSink) Handle(_ context.Context, event Event) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	sink.events = append(sink.events, event)
	return sink.err
}

func (sink *scaffoldingSink) delivered() []Event {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return append([]Event(nil), sink.events...)
}

// Reports when a caller reached the wait on an in-flight close attempt, so the test
// releases the owner only after a joiner is provably inside the gate.
type scaffoldingJoinProbe struct {
	entered chan struct{}
	block   chan struct{}
	once    sync.Once
}

func newScaffoldingJoinProbe() *scaffoldingJoinProbe {
	return &scaffoldingJoinProbe{entered: make(chan struct{}), block: make(chan struct{})}
}

func (probe *scaffoldingJoinProbe) Done() <-chan struct{} {
	probe.once.Do(func() { close(probe.entered) })
	return probe.block
}

func (*scaffoldingJoinProbe) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*scaffoldingJoinProbe) Err() error                  { return nil }
func (*scaffoldingJoinProbe) Value(any) any               { return nil }

func TestIsNilInterfaceClassifiesTypedNil(t *testing.T) {
	var nilInterface ProcessIdentity
	typedNil := (*scaffoldingProcess)(nil)
	nonNil := &scaffoldingProcess{pid: 1}
	for _, test := range []struct {
		name  string
		value any
		want  bool
	}{
		{"nil interface", nil, true},
		{"typed nil pointer behind an interface", nilInterface, true},
		{"typed nil pointer", any(typedNil), true},
		{"typed nil map", any(map[string]string(nil)), true},
		{"typed nil slice", any([]string(nil)), true},
		{"typed nil func", any((func())(nil)), true},
		{"typed nil chan", any((chan struct{})(nil)), true},
		{"non-nil pointer", any(nonNil), false},
		{"non-nil interface", ProcessIdentity(nonNil), false},
		{"string", any("value"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsNilInterface(test.value); got != test.want {
				t.Fatalf("IsNilInterface(%#v) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}

func TestProcessForwarderForwardsOptionalProcessCapabilities(t *testing.T) {
	process := &scaffoldingProcess{pid: 42, identity: "test:42", renewable: true}
	statusErr := errors.New("live owner status unavailable")
	process.status = execution.LinuxV2LiveOwnerStatus{LeaseSequence: 3, LeaseLive: true}
	process.statusErr = statusErr
	var mutex sync.Mutex
	var owner ProcessIdentity = process
	forwarder := NewProcessForwarder(&mutex, &owner)

	if pid, identity := forwarder.Details(); pid != 42 || identity != "test:42" {
		t.Fatalf("Details() = (%d, %q), want (42, %q)", pid, identity, "test:42")
	}
	mutex.Lock()
	lockedPID, lockedIdentity := forwarder.DetailsLocked()
	mutex.Unlock()
	if lockedPID != 42 || lockedIdentity != "test:42" {
		t.Fatalf("DetailsLocked() = (%d, %q), want (42, %q)", lockedPID, lockedIdentity, "test:42")
	}
	if !forwarder.LeaseRenewalAvailable() {
		t.Fatal("LeaseRenewalAvailable() = false, want true for a renewable process")
	}
	if err := forwarder.RenewLease(time.Second, 9); err != nil {
		t.Fatalf("RenewLease() error = %v, want nil", err)
	}
	if len(process.renewals) != 1 || process.renewals[0] != 9 {
		t.Fatalf("renewals = %v, want one renewal with sequence 9", process.renewals)
	}
	status, err := forwarder.LiveOwnerStatus(context.Background())
	if !errors.Is(err, statusErr) || status != process.status {
		t.Fatalf("LiveOwnerStatus() = (%+v, %v), want the process's own observation and error", status, err)
	}
}

func TestProcessForwarderLeaseRenewalAvailableReportsTheProcessAnswer(t *testing.T) {
	// Must return the process's own answer, not merely that the method exists: reporting an
	// uncontained process as renewable would let the daemon renew a lease that cannot be renewed.
	var mutex sync.Mutex
	process := &scaffoldingProcess{pid: 11, identity: "test:11", renewable: false}
	var owner ProcessIdentity = process
	forwarder := NewProcessForwarder(&mutex, &owner)
	if forwarder.LeaseRenewalAvailable() {
		t.Fatal("LeaseRenewalAvailable() = true for a process that answers false")
	}
}

func TestProcessForwarderReportsUnsupportedWithoutCapabilities(t *testing.T) {
	var mutex sync.Mutex
	var owner ProcessIdentity = scaffoldingIdentity{}
	forwarder := NewProcessForwarder(&mutex, &owner)

	if pid, identity := forwarder.Details(); pid != 7 || identity != "identity-only" {
		t.Fatalf("Details() = (%d, %q), want (7, %q)", pid, identity, "identity-only")
	}
	if forwarder.LeaseRenewalAvailable() {
		t.Fatal("LeaseRenewalAvailable() = true for a process without lease support")
	}
	if err := forwarder.RenewLease(time.Second, 1); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("RenewLease() error = %v, want errors.ErrUnsupported", err)
	}
	if _, err := forwarder.LiveOwnerStatus(context.Background()); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("LiveOwnerStatus() error = %v, want errors.ErrUnsupported", err)
	}

	var empty ProcessIdentity
	emptyForwarder := NewProcessForwarder(&mutex, &empty)
	if pid, identity := emptyForwarder.Details(); pid != 0 || identity != "" {
		t.Fatalf("nil-owner Details() = (%d, %q), want (0, %q)", pid, identity, "")
	}
	typedNil := (*scaffoldingProcess)(nil)
	var typedNilOwner ProcessIdentity = typedNil
	typedNilForwarder := NewProcessForwarder(&mutex, &typedNilOwner)
	if pid, identity := typedNilForwarder.Details(); pid != 0 || identity != "" {
		t.Fatalf("typed-nil-owner Details() = (%d, %q), want (0, %q)", pid, identity, "")
	}
	if _, err := typedNilForwarder.LiveOwnerStatus(context.Background()); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("typed-nil-owner LiveOwnerStatus() error = %v, want errors.ErrUnsupported", err)
	}
	if typedNilForwarder.LeaseRenewalAvailable() {
		t.Fatal("typed-nil-owner LeaseRenewalAvailable() = true, want false")
	}
	if err := typedNilForwarder.RenewLease(time.Second, 1); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("typed-nil-owner RenewLease() error = %v, want errors.ErrUnsupported", err)
	}
}

func TestCloseGateRetriesFailuresAndStabilizesSuccess(t *testing.T) {
	gate := CloseGate{}
	attempts := 0
	firstErr := errors.New("termination failed")
	first := gate.Run(context.Background(), func(context.Context) error {
		attempts++
		return firstErr
	})
	if !errors.Is(first, firstErr) {
		t.Fatalf("failed close = %v, want %v", first, firstErr)
	}
	second := gate.Run(context.Background(), func(context.Context) error {
		attempts++
		return nil
	})
	if second != nil {
		t.Fatalf("retry close = %v, want nil", second)
	}
	third := gate.Run(context.Background(), func(context.Context) error {
		t.Error("a successful close must not run another attempt")
		return nil
	})
	if third != nil {
		t.Fatalf("repeated close = %v, want nil", third)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one failure, one success)", attempts)
	}
}

func TestCloseGateJoinsInFlightAttemptAndHonorsCallerCancellation(t *testing.T) {
	gate := CloseGate{}
	started := make(chan struct{})
	release := make(chan struct{})
	originErr := errors.New("termination failed")
	owner := make(chan error, 1)
	go func() {
		owner <- gate.Run(context.Background(), func(context.Context) error {
			close(started)
			<-release
			return originErr
		})
	}()
	<-started

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gate.Run(cancelled, func(context.Context) error {
		t.Error("a joining caller must not run the attempt")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("joining close = %v, want context.Canceled", err)
	}

	joined := make(chan error, 1)
	probe := newScaffoldingJoinProbe()
	go func() {
		joined <- gate.Run(probe, func(context.Context) error {
			t.Error("a joining caller must not run the attempt")
			return nil
		})
	}()
	select {
	case <-probe.entered:
	case err := <-joined:
		t.Fatalf("joiner did not wait on the in-flight attempt: %v", err)
	case <-time.After(testHangGuard):
		t.Fatal("joiner did not enter the close gate")
	}
	close(release)
	if err := <-owner; !errors.Is(err, originErr) {
		t.Fatalf("owner close = %v, want %v", err, originErr)
	}
	if err := <-joined; !errors.Is(err, originErr) {
		t.Fatalf("joined close = %v, want the owner's %v", err, originErr)
	}
}

func TestCloseGateKeepsPermanentFailureWithoutReAttempt(t *testing.T) {
	gate := CloseGate{}
	attempts := 0
	permanentErr := errors.New("process termination is not retryable")
	first := gate.Run(context.Background(), func(context.Context) error {
		attempts++
		return permanentCloseFailure{Err: permanentErr}
	})
	if !errors.Is(first, permanentErr) {
		t.Fatalf("permanent close = %v, want the unwrapped %v", first, permanentErr)
	}
	var marker permanentCloseFailure
	if errors.As(first, &marker) {
		t.Fatalf("permanent close = %#v, want the unwrapped error, not the marker", first)
	}
	if strings.Contains(first.Error(), "permanentCloseFailure") {
		t.Fatalf("permanent close text = %q, want the inner error text", first.Error())
	}
	second := gate.Run(context.Background(), func(context.Context) error {
		attempts++
		return nil
	})
	if !errors.Is(second, permanentErr) {
		t.Fatalf("repeated close = %v, want the stable %v", second, permanentErr)
	}
	if errors.As(second, &marker) {
		t.Fatalf("repeated close = %#v, want the stored inner error, not the marker", second)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1: a permanent failure must not be retried", attempts)
	}
}

func TestCloseGateTreatsAWrappedPermanentFailureAsRetryable(t *testing.T) {
	gate := CloseGate{}
	innerErr := errors.New("process termination is not retryable")
	outerErr := errors.New("close bookkeeping failed")
	attempts := 0
	first := gate.Run(context.Background(), func(context.Context) error {
		attempts++
		return errors.Join(permanentCloseFailure{Err: innerErr}, outerErr)
	})
	if !errors.Is(first, outerErr) {
		t.Fatalf("wrapped-marker close = %v, want the full attempt error including %v", first, outerErr)
	}
	second := gate.Run(context.Background(), func(context.Context) error {
		attempts++
		return nil
	})
	if second != nil {
		t.Fatalf("retry after a wrapped marker = %v, want the retry to succeed", second)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2: only the bare marker may make a failure permanent", attempts)
	}
}

func TestCloseGateDetachedCleanupOutlivesCallerCancellation(t *testing.T) {
	gate := CloseGate{}
	hookRan := make(chan struct{})
	entered := make(chan struct{})
	release := make(chan struct{})
	attemptContext := make(chan error, 1)
	attempts := 0

	callerContext, cancelCaller := context.WithCancel(context.Background())
	cancelCaller()
	err := gate.RunDetached(callerContext, time.Second, func() { close(hookRan) }, func(cleanupContext context.Context) error {
		attempts++
		attemptContext <- cleanupContext.Err()
		close(entered)
		<-release
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("detached close with a cancelled caller = %v, want context.Canceled", err)
	}
	select {
	case <-hookRan:
	default:
		t.Fatal("the start hook must run on the owning caller before RunDetached returns")
	}
	if err := <-attemptContext; err != nil {
		t.Fatalf("detached attempt context = %v, want a live cleanup context", err)
	}
	<-entered
	joined := make(chan error, 1)
	go func() {
		joined <- gate.Run(context.Background(), func(context.Context) error {
			t.Error("a joining caller must not run the attempt")
			return nil
		})
	}()
	close(release)
	if err := <-joined; err != nil {
		t.Fatalf("joined detached close = %v, want nil", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if err := gate.Run(context.Background(), func(context.Context) error {
		t.Error("a successful detached close must not run another attempt")
		return nil
	}); err != nil {
		t.Fatalf("repeated detached close = %v, want nil", err)
	}
}

func TestWaitResultPrefersTerminalAndHonorsCancellation(t *testing.T) {
	terminal := make(chan struct{})
	readCalled := false
	closed := make(chan struct{})
	close(closed)
	result, err := WaitResult(context.Background(), closed, func() (TaskResult, error) {
		readCalled = true
		return TaskResult{Kind: ResultSucceeded, Summary: "settled"}, nil
	}, TaskResult{Kind: ResultCancelled, Summary: "unused"})
	if err != nil || result.Kind != ResultSucceeded || !readCalled {
		t.Fatalf("settled wait = (%+v, %v), want the terminal read", result, err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = WaitResult(cancelled, terminal, func() (TaskResult, error) {
		t.Error("a cancelled wait must not read the terminal state")
		return TaskResult{}, nil
	}, TaskResult{Kind: ResultCancelled, Summary: "cancelled summary"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait error = %v, want context.Canceled", err)
	}
	if result.Kind != ResultCancelled || result.Summary != "cancelled summary" {
		t.Fatalf("cancelled wait = %+v, want the adapter's own cancellation value", result)
	}
}

func TestWaitTurnBarrierPrefersSettledTurnAndReportsSessionError(t *testing.T) {
	turnErr := errors.New("turn settled with a failure")
	settled := make(chan struct{})
	close(settled)
	sessionContext, cancelSession := context.WithCancel(context.Background())
	cancelSession()
	err := WaitTurnBarrier(context.Background(), sessionContext, settled, func() error { return turnErr }, func() error {
		t.Error("a settled turn must win over an ended session context")
		return nil
	})
	if !errors.Is(err, turnErr) {
		t.Fatalf("settled-turn wait = %v, want %v", err, turnErr)
	}

	sessionErr := errors.New("session failed without settling the turn")
	pending := make(chan struct{})
	endedSession, cancelEnded := context.WithCancel(context.Background())
	cancelEnded()
	err = WaitTurnBarrier(context.Background(), endedSession, pending, func() error {
		t.Error("an unsettled turn must not report a turn error")
		return nil
	}, func() error { return sessionErr })
	if !errors.Is(err, sessionErr) {
		t.Fatalf("ended-session wait = %v, want %v", err, sessionErr)
	}

	waitContext, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	err = WaitTurnBarrier(waitContext, context.Background(), pending, func() error {
		t.Error("a cancelled wait must not report a turn error")
		return nil
	}, func() error {
		t.Error("a cancelled wait must not report a session error")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait = %v, want context.Canceled", err)
	}
}

func TestWaitTurnBarrierKeepsSettledTurnWhenSessionEndsConcurrently(t *testing.T) {
	turnErr := errors.New("turn settled as the session ended")
	sessionErr := errors.New("session ended before the turn settled")
	settledTurn := make(chan struct{})
	close(settledTurn)
	sessionContext, cancelSession := context.WithCancel(context.Background())
	cancelSession()
	// Both barriers are closed, so the select is arbitrary; every round must still return
	// the settled turn error from the session-context arm's non-blocking re-check.
	for round := range 128 {
		err := WaitTurnBarrier(context.Background(), sessionContext, settledTurn, func() error { return turnErr }, func() error { return sessionErr })
		if !errors.Is(err, turnErr) {
			t.Fatalf("round %d = %v, want the settled turn error %v", round, err, turnErr)
		}
	}
}

func TestDeliverEventStampsMissingTimeAndForwardsSinkError(t *testing.T) {
	sink := &scaffoldingSink{}
	event := Event{Kind: EventOutput, Sequence: 3, Data: []byte("payload")}
	if err := DeliverEvent(context.Background(), sink, event); err != nil {
		t.Fatalf("DeliverEvent() error = %v, want nil", err)
	}
	delivered := sink.delivered()
	if len(delivered) != 1 || delivered[0].Sequence != 3 || string(delivered[0].Data) != "payload" {
		t.Fatalf("delivered events = %+v, want the stamped event", delivered)
	}
	if delivered[0].At.IsZero() {
		t.Fatal("DeliverEvent delivered a zero event time unstamped")
	}

	stamped := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	preserved := Event{Kind: EventSessionStarted, At: stamped}
	if err := DeliverEvent(context.Background(), sink, preserved); err != nil {
		t.Fatalf("DeliverEvent() error = %v, want nil", err)
	}
	delivered = sink.delivered()
	if len(delivered) != 2 || delivered[1].At != stamped {
		t.Fatalf("delivered events = %+v, want a preserved event time %v", delivered, stamped)
	}

	sinkErr := errors.New("journal unavailable")
	failing := &scaffoldingSink{err: sinkErr}
	// Identity, not errors.Is: adapters wrap this result in their own "persist <adapter>
	// native event: %w" literal, so a wrapper here would change every persisted text.
	if err := DeliverEvent(context.Background(), failing, event); err != sinkErr {
		t.Fatalf("DeliverEvent() error = %v, want the sink error identity %v", err, sinkErr)
	}
}

func TestCloneTaskResultIsolatesEveryProtocolFieldAndPreservesWireBytes(t *testing.T) {
	proposal := json.RawMessage(`{"steps":["inspect"]}`)
	reason := protocol.TaskResultReasonUnknownOutcome
	semanticReason := protocol.TaskResultReasonRateLimit
	severity := protocol.DiagnosticWarning
	semantic := protocol.TaskResult{
		EvidenceRefs: []string{"evidence-1"},
		Blocker: &protocol.Blocker{
			Kind:        protocol.BlockerDependency,
			WorkItemIDs: []string{"work-1"},
		},
		ProposedNextAction: &protocol.NextAction{
			Kind:    protocol.NextActionWait,
			Blocker: &protocol.Blocker{Kind: protocol.BlockerDependency, WorkItemIDs: []string{"next-work-1"}},
		},
		Proposal:    &proposal,
		Reason:      &semanticReason,
		Diagnostics: []protocol.Diagnostic{{Code: "observed", Message: "original", Severity: &severity}},
	}
	original := TaskResult{Kind: ResultSucceeded, Semantic: &semantic, Reason: &reason}
	cloned := CloneTaskResult(original)

	originalBytes, err := json.Marshal(original.Semantic)
	if err != nil {
		t.Fatalf("marshal original = %v", err)
	}
	clonedBytes, err := json.Marshal(cloned.Semantic)
	if err != nil {
		t.Fatalf("marshal clone = %v", err)
	}
	if string(originalBytes) != string(clonedBytes) {
		t.Fatalf("clone marshals to %s, want the original bytes %s", clonedBytes, originalBytes)
	}

	cloned.Semantic.EvidenceRefs[0] = "changed"
	cloned.Semantic.Blocker.WorkItemIDs[0] = "changed"
	cloned.Semantic.ProposedNextAction.Blocker.WorkItemIDs[0] = "changed"
	(*cloned.Semantic.Proposal)[0] = '['
	*cloned.Semantic.Reason = protocol.TaskResultReasonCancelled
	*cloned.Semantic.Diagnostics[0].Severity = protocol.DiagnosticError
	cloned.Semantic.Diagnostics[0].Message = "changed"
	*cloned.Reason = protocol.TaskResultReasonCancelled
	if semantic.EvidenceRefs[0] != "evidence-1" ||
		semantic.Blocker.WorkItemIDs[0] != "work-1" ||
		semantic.ProposedNextAction.Blocker.WorkItemIDs[0] != "next-work-1" ||
		string(*semantic.Proposal) != `{"steps":["inspect"]}` ||
		*semantic.Reason != protocol.TaskResultReasonRateLimit ||
		*semantic.Diagnostics[0].Severity != protocol.DiagnosticWarning ||
		semantic.Diagnostics[0].Message != "original" ||
		*original.Reason != protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("mutating the clone changed the original: %+v", semantic)
	}

	emptyClone := CloneTaskResult(TaskResult{Semantic: &protocol.TaskResult{EvidenceRefs: []string{}, Diagnostics: []protocol.Diagnostic{}}})
	encoded, err := json.Marshal(emptyClone.Semantic)
	if err != nil {
		t.Fatalf("marshal empty-slice clone = %v", err)
	}
	if !strings.Contains(string(encoded), `"evidence_refs":[]`) || !strings.Contains(string(encoded), `"diagnostics":[]`) {
		t.Fatalf("clone of empty slices = %s, want non-nil empty arrays", encoded)
	}
	nilClone := CloneTaskResult(TaskResult{Semantic: &protocol.TaskResult{}})
	encoded, err = json.Marshal(nilClone.Semantic)
	if err != nil {
		t.Fatalf("marshal nil-slice clone = %v", err)
	}
	if !strings.Contains(string(encoded), `"evidence_refs":null`) || !strings.Contains(string(encoded), `"diagnostics":null`) {
		t.Fatalf("clone of nil slices = %s, want null members", encoded)
	}

	if CloneTaskResultPointer(nil) != nil {
		t.Fatal("CloneTaskResultPointer(nil) must stay nil")
	}
	pointer := CloneTaskResultPointer(&original)
	if pointer == nil || pointer.Semantic == original.Semantic || *pointer.Reason != *original.Reason {
		t.Fatalf("CloneTaskResultPointer = %+v, want an independent copy", pointer)
	}
}
