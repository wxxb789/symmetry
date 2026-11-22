package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

type goalAdmissionCompensationProbe struct {
	names    []string
	entries  []*goalAdmissionCompensation
	executed []string
}

func newGoalAdmissionCompensationProbe(tx *goalAdmissionTx) *goalAdmissionCompensationProbe {
	return &goalAdmissionCompensationProbe{
		names: []string{"provider-bridge", "clone-refusal", "clone-reuse-refusal", "native-start-outcome", "linux-v2-launch-phase"},
		entries: []*goalAdmissionCompensation{
			&tx.bridgeCompensation,
			&tx.cloneCompensation,
			&tx.reuseCompensation,
			&tx.nativeOutcomeCompensation,
			&tx.launchPhaseCompensation,
		},
	}
}

func (probe *goalAdmissionCompensationProbe) observe(joinErrors bool) {
	for index, entry := range probe.entries {
		name := probe.names[index]
		entry.run = func(_ context.Context, returnErr error) error {
			probe.executed = append(probe.executed, name)
			if joinErrors {
				return errors.Join(returnErr, fmt.Errorf("compensation %s failed", name))
			}
			return nil
		}
	}
}

func (probe *goalAdmissionCompensationProbe) observeDelegate() {
	for index, entry := range probe.entries {
		name := probe.names[index]
		original := entry.run
		entry.run = func(ctx context.Context, returnErr error) error {
			probe.executed = append(probe.executed, name)
			return original(ctx, returnErr)
		}
	}
}

func (probe *goalAdmissionCompensationProbe) sequence() string {
	return strings.Join(probe.executed, ",")
}

func newBareGoalAdmissionTx() *goalAdmissionTx {
	return newGoalAdmissionTx(&daemon{}, state.RunKey{RunID: "run-1", Generation: 1}, protocol.ClaimResponse{}, protocol.Admission{}, nil)
}

func nativeAdmissionFixture(t *testing.T) (*daemon, *state.Store, *fakeNativeGoalSession, state.RunKey, protocol.ClaimResponse, protocol.Admission) {
	t.Helper()
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	t.Cleanup(func() { _ = store.Close() })
	key := state.RunKey{RunID: "run-1", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	claim := protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", Work: controlClient.work}
	return app, store, session, key, claim, admission
}

func TestGoalAdmissionTxCompensationOrderMatchesTheBaseDeferOrder(t *testing.T) {
	tx := newBareGoalAdmissionTx()
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observe(false)
	for _, entry := range probe.entries {
		entry.armed = true
	}
	if err := tx.abort(context.Background(), nil); err != nil {
		t.Fatalf("abort() error = %v", err)
	}
	if want := "provider-bridge,clone-refusal,clone-reuse-refusal,native-start-outcome,linux-v2-launch-phase"; probe.sequence() != want {
		t.Fatalf("compensation order = %q, want %q", probe.sequence(), want)
	}

	probe.executed = nil
	tx.cloneCompensation.released = true
	tx.bridgeCompensation.released = true
	if err := tx.abort(context.Background(), nil); err != nil {
		t.Fatalf("abort() after transfers error = %v", err)
	}
	if want := "clone-reuse-refusal,native-start-outcome,linux-v2-launch-phase"; probe.sequence() != want {
		t.Fatalf("compensation order after transfers = %q, want %q", probe.sequence(), want)
	}

	probe.executed = nil
	for _, entry := range probe.entries {
		entry.armed = false
		entry.released = false
	}
	if err := tx.abort(context.Background(), nil); err != nil {
		t.Fatalf("abort() with nothing armed error = %v", err)
	}
	if probe.sequence() != "" {
		t.Fatalf("compensation ran without an armed entry: %q", probe.sequence())
	}
}

func TestGoalAdmissionTxCompensationErrorsJoinInExecutionOrder(t *testing.T) {
	tx := newBareGoalAdmissionTx()
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observe(true)
	for _, entry := range probe.entries {
		entry.armed = true
	}
	seed := errors.New("seed failure")
	returnErr := tx.abort(context.Background(), seed)
	if !errors.Is(returnErr, seed) {
		t.Fatalf("abort() lost the seed error: %v", returnErr)
	}
	want := []string{
		"seed failure",
		"compensation provider-bridge failed",
		"compensation clone-refusal failed",
		"compensation clone-reuse-refusal failed",
		"compensation native-start-outcome failed",
		"compensation linux-v2-launch-phase failed",
	}
	if got := strings.Split(returnErr.Error(), "\n"); !slices.Equal(got, want) {
		t.Fatalf("compensation join order = %q, want %q", got, want)
	}
}

func TestGoalAdmissionTxSettlesFreshCancellationWithoutCompensation(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	launchStartedWrites := 0
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if strings.Contains(string(data), `"launch_state":"intent"`) {
			app.mu.Lock()
			if active := app.running[key]; active != nil {
				active.requestCancel("cancel-command")
			}
			app.mu.Unlock()
		}
		if strings.Contains(string(data), `"launch_state":"launching"`) {
			launchStartedWrites++
		}
		return writeAppStateAtomic(path, data)
	})
	defer restoreWriter()

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observe(false)
	runErr := tx.run(context.Background())
	if runErr != nil {
		t.Fatalf("cancelled fresh launch error = %v, want the compensated settle path", runErr)
	}
	if tx.nativeOutcomeCompensation.armed {
		t.Fatal("a cancelled fresh launch armed the native-start outcome compensation")
	}
	if calls := session.callsSnapshot(); len(calls) != 0 {
		t.Fatalf("cancelled fresh launch started the native adapter: %#v", calls)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || !goalSessionClosed(sessions[0]) {
		t.Fatalf("cancelled fresh launch compensation = %#v", sessions)
	}
	if launchStartedWrites != 0 {
		t.Fatalf("cancelled fresh launch published the launch-started marker %d times", launchStartedWrites)
	}
	if returnErr := tx.abort(context.Background(), runErr); returnErr != nil {
		t.Fatalf("abort() after a settled cancellation error = %v", returnErr)
	}
	if probe.sequence() != "" {
		t.Fatalf("settled cancellation ran compensation: %q", probe.sequence())
	}
}

func TestGoalAdmissionTxAbortsQueuedAttachBeforeNativeStart(t *testing.T) {
	app, _, _, key, claim, admission := nativeAdmissionFixture(t)
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true, cancelled: true}}
	aborted := 0
	discarded := 0
	app.options.abortGoalSessionLaunchBeforeNativeStart = func(sessionKey state.GoalSessionKey) (state.GoalSessionJournal, error) {
		aborted++
		return state.GoalSessionJournal{GoalID: sessionKey.GoalID, LocalHandleID: sessionKey.LocalHandleID}, nil
	}
	app.options.discardUnreadyGoalSessionAttachDelivery = func(state.RunKey, string) (state.RunJournal, error) {
		discarded++
		return state.RunJournal{}, nil
	}
	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	tx.sessionKey = state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-0000000000aa"}
	tx.attachQueued = true
	cancelled, err := tx.abortFreshLaunchIfCancelled()
	if !cancelled || err != nil {
		t.Fatalf("abortFreshLaunchIfCancelled() = %t, %v; want a compensated cancellation", cancelled, err)
	}
	if aborted != 1 || discarded != 1 {
		t.Fatalf("cancelled launch compensation = abort %d, discard %d; want 1 and 1", aborted, discarded)
	}

	abortFailure := errors.New("abort failed")
	app.options.abortGoalSessionLaunchBeforeNativeStart = func(state.GoalSessionKey) (state.GoalSessionJournal, error) {
		return state.GoalSessionJournal{}, abortFailure
	}
	cancelled, err = tx.abortFreshLaunchIfCancelled()
	if !cancelled || !errors.Is(err, abortFailure) {
		t.Fatalf("abortFreshLaunchIfCancelled() failure = %t, %v; want the abort failure", cancelled, err)
	}
	if reason, typed := taskResultFailureReason(err); !typed || reason != protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("failed launch compensation = %v (reason %q, typed %t); want the unknown outcome", err, reason, typed)
	}
}

func TestGoalAdmissionTxKeepsKnownPreNativeFailureUnwrapped(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	injected := errors.New("injected pre-native Goal session write failure")
	failed := false
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if !failed && strings.Contains(string(data), `"launch_state":"intent"`) {
			failed = true
			return injected
		}
		return writeAppStateAtomic(path, data)
	})
	defer restoreWriter()

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observe(false)
	runErr := tx.run(context.Background())
	if !failed {
		t.Fatal("the injected pre-native write fault was not exercised")
	}
	if !errors.Is(runErr, injected) {
		t.Fatalf("pre-native write failure = %v, want the injected failure", runErr)
	}
	if tx.nativeOutcomeCompensation.armed {
		t.Fatal("a pre-native failure armed the native-start outcome compensation")
	}
	if tx.bridgeCompensation.armed || tx.bridgeCompensation.released {
		t.Fatal("a fresh non-Pi admission armed the provider-bridge compensation")
	}
	if calls := session.callsSnapshot(); len(calls) != 0 {
		t.Fatalf("pre-native write failure started the native adapter: %#v", calls)
	}
	returnErr := tx.abort(context.Background(), runErr)
	if !errors.Is(returnErr, runErr) {
		t.Fatalf("abort dropped a compensated pre-native failure: %v", returnErr)
	}
	if returnErr.Error() != runErr.Error() {
		t.Fatalf("abort changed a compensated pre-native failure: %v", returnErr)
	}
	if reason, typed := taskResultFailureReason(returnErr); typed && reason == protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("known pre-native compensation was typed unknown: %v", returnErr)
	}
	if probe.sequence() != "" {
		t.Fatalf("pre-native failure ran compensation: %q", probe.sequence())
	}
}

func TestGoalAdmissionTxRunsOnlyTheOutcomeCompensationAfterNativeHandover(t *testing.T) {
	app, _, session, key, claim, admission := nativeAdmissionFixture(t)
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	session.handle = harness.NativeSessionHandle{}
	discardFailure := errors.New("discard write failed")
	app.options.discardUnreadyGoalSessionAttachDelivery = func(state.RunKey, string) (state.RunJournal, error) {
		return state.RunJournal{}, discardFailure
	}

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if !errors.Is(runErr, discardFailure) {
		t.Fatalf("post-handover failure = %v, want the discard failure", runErr)
	}
	if !tx.nativeOutcomeCompensation.armed {
		t.Fatal("the native start attempt did not arm the outcome compensation")
	}
	if !tx.bridgeCompensation.released {
		t.Fatal("the session handover did not release the provider-bridge compensation")
	}
	if tx.cloneCompensation.armed || tx.reuseCompensation.armed {
		t.Fatal("a fresh non-Linux-v2 admission armed clone compensation")
	}
	returnErr := tx.abort(context.Background(), runErr)
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("compensation sequence = %q, want %q", probe.sequence(), want)
	}
	if !errors.Is(returnErr, discardFailure) {
		t.Fatalf("abort() lost the run failure: %v", returnErr)
	}
	if reason, typed := taskResultFailureReason(returnErr); !typed || reason != protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("the outcome compensation did not type the failure: %v (reason %q, typed %t)", returnErr, reason, typed)
	}
}
