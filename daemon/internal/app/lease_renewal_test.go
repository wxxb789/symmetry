package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestStartingRunLeaseRenewalPreservesLifecycleFences(t *testing.T) {
	key := state.RunKey{RunID: "run-1", Generation: 1}
	cases := []struct {
		name  string
		setup func(*runningRun)
		want  bool
	}{
		{name: "starting", setup: func(active *runningRun) { active.starting = true }, want: true},
		{name: "stale", setup: func(active *runningRun) { active.stale = true }},
		{name: "terminal", setup: func(active *runningRun) { active.terminal = true }},
		{name: "usage renewal blocked", setup: func(active *runningRun) { active.nativeUsageRenewalBlocked = true }},
		{name: "lease renewal closed", setup: func(active *runningRun) { active.leaseRenewalClosed = true }},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			process := &leaseRenewalProcess{available: true}
			active := &runningRun{process: process}
			test.setup(active)
			daemon := &daemon{running: map[state.RunKey]*runningRun{key: active}}

			supported, err := daemon.renewLocalLease(key, time.Second)
			if err != nil {
				t.Fatalf("renewLocalLease() error = %v", err)
			}
			if supported != test.want {
				t.Fatalf("renewLocalLease() supported = %t, want %t", supported, test.want)
			}
			if test.want {
				if process.calls != 1 || process.sequence != 1 || process.deadline != time.Second {
					t.Fatalf("renewal call = (%d, %d, %s), want (1, 1, 1s)", process.calls, process.sequence, process.deadline)
				}
			} else if process.calls != 0 {
				t.Fatalf("renewal calls = %d, want 0", process.calls)
			}
		})
	}
}

func TestInitialNativeLeaseArmAdvancesFirstRenewalSequence(t *testing.T) {
	key := state.RunKey{RunID: "run-native", Generation: 1}
	process := &leaseRenewalProcess{available: true}
	daemon := &daemon{
		running: map[state.RunKey]*runningRun{
			// Native Runner consumed sequence 1 while arming its initial
			// watchdog, so the daemon-side renewal starts at sequence 2.
			key: {process: process, leaseSequence: 1},
		},
	}

	supported, err := daemon.renewLocalLease(key, time.Second)
	if err != nil {
		t.Fatalf("renewLocalLease() error = %v", err)
	}
	if !supported {
		t.Fatal("renewLocalLease() supported = false, want true")
	}
	if process.calls != 1 || process.sequence != 2 || process.deadline != time.Second {
		t.Fatalf("renewal call = (%d, %d, %s), want (1, 2, 1s)", process.calls, process.sequence, process.deadline)
	}
}

func TestRelativeLeaseRenewalUsesRequestElapsedTimeDespiteAbsoluteClockSkew(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	base := time.Now()
	clock := &leaseTestClock{now: base}
	process := &leaseRenewalProcess{available: true}
	control := &delayedRelativeRenewalControl{
		response: protocol.LeaseHeartbeatResponse{
			// The absolute value is deliberately unrelated to the local clock.
			LeaseExpiresAt:   base.Add(24 * time.Hour),
			LeaseRemainingMS: 12_000,
		},
		advance: func() { clock.Advance(6 * time.Second) },
	}
	daemon := &daemon{
		store:   store,
		control: control,
		options: options{
			// A skewed daemon wall clock must not affect a relative lease.
			clock:      func() time.Time { return base.Add(24 * time.Hour) },
			localClock: clock.Now,
		},
		running: map[state.RunKey]*runningRun{
			key: {process: process, localLeaseDeadlineAt: base.Add(10 * time.Second)},
		},
		slots:         make(chan struct{}, 1),
		leaseDuration: 20 * time.Second,
	}

	daemon.renewLeases(context.Background())

	if control.renewCalls != 1 {
		t.Fatalf("RenewLease calls = %d, want 1", control.renewCalls)
	}
	// 12s server-relative budget - 6s request delay - 5s safety margin.
	if process.calls != 1 || process.deadline != time.Second || process.sequence != 1 {
		t.Fatalf("local watchdog renewal = (%d, %s, %d), want (1, 1s, 1)", process.calls, process.deadline, process.sequence)
	}
}

func TestFirstRelativeRenewalResponseIsNotRejectedByLegacyExpiryProjection(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	base := time.Now()
	if _, err := store.UpdateLeaseExpiry(key, base.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	clock := &leaseTestClock{now: base}
	wallCalls := 0
	process := &leaseRenewalProcess{available: true}
	control := &delayedRelativeRenewalControl{
		response: protocol.LeaseHeartbeatResponse{
			LeaseExpiresAt:   base.Add(24 * time.Hour),
			LeaseRemainingMS: 12_000,
		},
		advance: func() { clock.Advance(time.Second) },
	}
	daemon := &daemon{
		store:   store,
		control: control,
		options: options{
			clock: func() time.Time {
				wallCalls++
				if wallCalls <= 2 {
					return base
				}
				return base.Add(24 * time.Hour)
			},
			localClock: clock.Now,
		},
		running:       map[state.RunKey]*runningRun{key: {process: process}},
		slots:         make(chan struct{}, 1),
		leaseDuration: 20 * time.Second,
	}

	daemon.renewLeases(context.Background())

	if control.renewCalls != 1 {
		t.Fatalf("RenewLease calls = %d, want 1", control.renewCalls)
	}
	if process.calls != 1 || process.deadline != 6*time.Second {
		t.Fatalf("first relative watchdog renewal = (%d, %s), want (1, 6s)", process.calls, process.deadline)
	}
}

func TestRelativeLeaseDeadlineRejectsPastLocalAuthorityBeforeRenewal(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	base := time.Now()
	clock := &leaseTestClock{now: base}
	control := &delayedRelativeRenewalControl{}
	daemon := &daemon{
		store:   store,
		control: control,
		options: options{
			clock:      func() time.Time { return base.Add(-24 * time.Hour) },
			localClock: clock.Now,
		},
		running: map[state.RunKey]*runningRun{
			key: {process: &leaseRenewalProcess{available: true}, localLeaseDeadlineAt: base.Add(-time.Second)},
		},
		slots: make(chan struct{}, 1),
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}

	_, requestID, err := daemon.renewLease(context.Background(), journal)
	if !errors.Is(err, errLeaseDeadlineReached) {
		t.Fatalf("renewLease() error = %v, want errLeaseDeadlineReached", err)
	}
	if requestID != 0 || control.renewCalls != 0 {
		t.Fatalf("renewal after local deadline = (request %d, calls %d), want (0, 0)", requestID, control.renewCalls)
	}
}

func TestRelativeLeaseDeadlineIgnoresAbsoluteExpiry(t *testing.T) {
	requestStarted := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	claim := protocol.ClaimResponse{
		LeaseExpiresAt:   requestStarted.Add(-time.Hour),
		LeaseRemainingMS: 30_000,
	}

	deadline := leaseDeadlineAt(claim, requestStarted)
	want := requestStarted.Add(25 * time.Second)
	if !deadline.Equal(want) {
		t.Fatalf("leaseDeadlineAt() = %s, want %s", deadline, want)
	}
	if got := remainingLeaseDeadlineAt(deadline, requestStarted.Add(4*time.Second)); got != 21*time.Second {
		t.Fatalf("remaining relative lease = %s, want 21s", got)
	}
}

type leaseTestClock struct {
	now time.Time
}

func (clock *leaseTestClock) Now() time.Time { return clock.now }

func (clock *leaseTestClock) Advance(value time.Duration) { clock.now = clock.now.Add(value) }

type delayedRelativeRenewalControl struct {
	fakeControl
	response protocol.LeaseHeartbeatResponse
	advance  func()
}

func (control *delayedRelativeRenewalControl) RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	control.renewCalls++
	if control.advance != nil {
		control.advance()
	}
	return control.response, nil
}

type leaseRenewalProcess struct {
	available bool
	calls     int
	deadline  time.Duration
	sequence  uint64
}

func (*leaseRenewalProcess) WriteInput([]byte) error { return nil }

func (*leaseRenewalProcess) Terminate(context.Context, time.Duration) error { return nil }

func (*leaseRenewalProcess) Wait() execution.Result { return execution.Result{} }

func (*leaseRenewalProcess) ProcessDetails() (int, string) { return 1, "test:lease-renewal" }

func (process *leaseRenewalProcess) LeaseRenewalAvailable() bool { return process.available }

func (process *leaseRenewalProcess) RenewLease(deadline time.Duration, sequence uint64) error {
	process.calls++
	process.deadline = deadline
	process.sequence = sequence
	return nil
}
