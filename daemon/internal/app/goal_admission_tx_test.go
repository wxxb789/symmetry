package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

type goalAdmissionCompensationProbe struct {
	names    map[*goalAdmissionCompensation]string
	entries  []*goalAdmissionCompensation
	executed []string
}

func newGoalAdmissionCompensationProbe(tx *goalAdmissionTx) *goalAdmissionCompensationProbe {
	named := []struct {
		name  string
		entry *goalAdmissionCompensation
	}{
		{"provider-bridge", &tx.bridgeCompensation},
		{"clone-refusal", &tx.cloneCompensation},
		{"clone-reuse-refusal", &tx.reuseCompensation},
		{"native-start-outcome", &tx.nativeOutcomeCompensation},
		{"linux-v2-launch-phase", &tx.launchPhaseCompensation},
	}
	probe := &goalAdmissionCompensationProbe{names: make(map[*goalAdmissionCompensation]string, len(named))}
	for _, row := range named {
		probe.entries = append(probe.entries, row.entry)
		probe.names[row.entry] = row.name
	}
	return probe
}

func (probe *goalAdmissionCompensationProbe) observe(joinErrors bool) {
	for _, entry := range probe.entries {
		name := probe.names[entry]
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
	for _, entry := range probe.entries {
		name := probe.names[entry]
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

func TestGoalAdmissionTxCompletesAWholeLaunchWithoutEffectfulCompensation(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	if runErr := tx.run(context.Background()); runErr != nil {
		t.Fatalf("successful fresh launch error = %v, want the running path", runErr)
	}
	calls := session.callsSnapshot()
	if len(calls) < 4 || !slices.Equal(calls[:4], []string{"start", "details", "open", "start_turn"}) {
		t.Fatalf("successful launch native calls = %#v, want start/details/open/start_turn", calls)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "running" || journal.TerminalState != "" {
		t.Fatalf("successful launch journal = %q/%q, want the running publication", journal.LocalState, journal.TerminalState)
	}
	if !tx.nativeOutcomeCompensation.armed {
		t.Fatal("the successful launch never armed the native-start outcome compensation")
	}
	if returnErr := tx.abort(context.Background(), nil); returnErr != nil {
		t.Fatalf("abort() after a successful launch returned %v, want no compensation effect", returnErr)
	}
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("success-path compensation sequence = %q, want only the no-op %q", probe.sequence(), want)
	}
}

func TestGoalAdmissionTxSettlesLeaseExpiredProcessStart(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	leaseReason := "native start lease expired in the test watchdog"
	app.running[key] = &runningRun{runLifecycle: runLifecycle{
		starting: true, claimed: true, nativeStartLeaseExpired: true, nativeStartLeaseReason: leaseReason,
	}}

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if reason, typed := taskResultFailureReason(runErr); !typed || reason != protocol.TaskResultReasonProcessFailure {
		t.Fatalf("lease-expired process start error = %v (reason %q, typed %t); want the process-failure reason", runErr, reason, typed)
	}
	if !strings.Contains(runErr.Error(), leaseReason) {
		t.Fatalf("lease-expired process start error = %v, want the watchdog reason %q", runErr, leaseReason)
	}
	calls := session.callsSnapshot()
	if slices.Contains(calls, "open") {
		t.Fatalf("lease expiry settled after the native session Open: %#v", calls)
	}
	if !slices.Contains(calls, "close") {
		t.Fatalf("lease-expired process start did not close the native session: %#v", calls)
	}
	goalSession, err := store.LoadGoalSession(tx.sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if goalSession.LaunchState != state.GoalSessionLaunchStateUncertain || goalSession.SessionState != state.GoalSessionStateUnavailable ||
		!goalSession.RecoveryRequired || !strings.Contains(goalSession.UncertainReason, leaseReason) {
		t.Fatalf("lease-expired process start Goal session = %#v", goalSession)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace {
		t.Fatalf("lease-expired process start did not retain the unproven workspace: %#v", journal)
	}
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliverySessionAttach && delivery.DeliveryID == tx.localHandleID {
			t.Fatalf("lease-expired process start kept the unready attach delivery: %#v", delivery)
		}
	}
	returnErr := tx.abort(context.Background(), runErr)
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("post-expiry compensation sequence = %q, want only the no-op %q", probe.sequence(), want)
	}
	if returnErr.Error() != runErr.Error() {
		t.Fatalf("post-expiry abort changed the settled failure: %v", returnErr)
	}
}

func TestGoalAdmissionTxRunsPreCheckoutCloneAndReuseCompensations(t *testing.T) {
	tx := newBareGoalAdmissionTx()
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// The Linux clone body reads the journal; without a store it panics instead of
	// returning the platform error this case asserts through the wrapper prefix.
	tx.daemon.store = store
	tx.linuxClone = &linuxCloneRun{}
	tx.linuxCloneReuse = &authority.LinuxCloneReuseGrant{}
	tx.cloneCompensation.armed = true
	tx.reuseCompensation.armed = true
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	seed := errors.New("native launch refused")
	returnErr := tx.abort(context.Background(), seed)
	if want := "clone-refusal,clone-reuse-refusal"; probe.sequence() != want {
		t.Fatalf("clone compensation sequence = %q, want %q", probe.sequence(), want)
	}
	if !errors.Is(returnErr, seed) {
		t.Fatalf("clone compensation dropped the seed failure: %v", returnErr)
	}
	for _, prefix := range []string{
		"abort pre-checkout Linux clone after native-start refusal: ",
		"retire pre-native Linux clone reuse after launch refusal: ",
	} {
		if !strings.Contains(returnErr.Error(), prefix) {
			t.Fatalf("clone compensation error = %v, want the real body wrapper prefix %q", returnErr, prefix)
		}
	}
}

func TestGoalAdmissionTxHandsCloneCompensationsToTheNativeOwner(t *testing.T) {
	tx := newBareGoalAdmissionTx()
	tx.linuxClone = &linuxCloneRun{}
	tx.linuxCloneReuse = &authority.LinuxCloneReuseGrant{}
	tx.cloneCompensation.armed = true
	tx.reuseCompensation.armed = true
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	tx.handOffLaunchToNativeOwner()
	if !tx.cloneCompensation.released || !tx.reuseCompensation.released {
		t.Fatalf("handOffLaunchToNativeOwner() clone released = %t, reuse released = %t; want both transferred",
			tx.cloneCompensation.released, tx.reuseCompensation.released)
	}
	seed := errors.New("launch refused after the native-owner handover")
	returnErr := tx.abort(context.Background(), seed)
	if probe.sequence() != "" {
		t.Fatalf("post-handover abort ran transferred compensation: %q", probe.sequence())
	}
	if !errors.Is(returnErr, seed) || returnErr.Error() != seed.Error() {
		t.Fatalf("post-handover abort changed the seed failure: %v", returnErr)
	}
}

func TestGoalAdmissionTxReleasesLinuxV2LaunchPhaseOnce(t *testing.T) {
	tx := newBareGoalAdmissionTx()
	releases := 0
	tx.releaseLinuxV2LaunchPhase = func() { releases++ }
	tx.launchPhaseCompensation.armed = true
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	seed := errors.New("launch phase failure")
	if returnErr := tx.abort(context.Background(), seed); !errors.Is(returnErr, seed) || returnErr.Error() != seed.Error() {
		t.Fatalf("launch-phase abort changed the seed failure: %v", returnErr)
	}
	if want := "linux-v2-launch-phase"; probe.sequence() != want {
		t.Fatalf("launch-phase compensation sequence = %q, want %q", probe.sequence(), want)
	}
	if releases != 1 {
		t.Fatalf("launch-phase release calls = %d, want exactly 1", releases)
	}
	tx.launchPhaseCompensation.released = true
	if returnErr := tx.abort(context.Background(), seed); !errors.Is(returnErr, seed) || returnErr.Error() != seed.Error() {
		t.Fatalf("released launch-phase abort changed the seed failure: %v", returnErr)
	}
	if releases != 1 {
		t.Fatalf("released launch phase was released again: %d calls", releases)
	}
}

func TestGoalAdmissionTxClosesUnownedProviderBridgeForReal(t *testing.T) {
	if requiresLinuxV2NativeOwner(harness.KindPi) {
		t.Skip("Linux Pi admission requires the Linux v2 native owner and a Pi provider-extension share; no shared non-Linux fixture drives that path")
	}
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	admission.ProviderScope = &protocol.ProviderScope{
		ResourceIDs: []string{admission.Subject.ResourceID},
		OperationsByResource: map[string][]protocol.ProviderOperation{
			admission.Subject.ResourceID: {protocol.ProviderOperationResourceSync},
		},
	}
	capabilities := testVerifiedNativeCapabilities(harness.KindPi)
	capabilities.ProviderAccess = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
	t.Cleanup(func() { _ = store.Close() })
	profile := app.config.AgentProfiles[app.config.Runtime.AgentProfile]
	profile.ProviderAccess = true
	app.config.AgentProfiles[app.config.Runtime.AgentProfile] = profile
	controlClient.providerAccess = &protocol.ProviderAccess{
		Path:  "/api/v1/provider-actions",
		Token: "provider-token-do-not-persist",
		Grants: []protocol.ProviderGrant{{
			ResourceID: admission.Subject.ResourceID,
			Provider:   "github",
			Kind:       "repository",
			Operations: []string{"resource.sync"},
		}},
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	claim := protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", Work: controlClient.work, ProviderAccess: controlClient.providerAccess}
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	session.startErr = errors.New("pi native start refused after the provider bridge was prepared")

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if !tx.bridgeCompensation.armed || tx.bridgeCompensation.released {
		t.Fatalf("refused Pi launch bridge compensation = armed %t, released %t; want an unowned bridge", tx.bridgeCompensation.armed, tx.bridgeCompensation.released)
	}
	if tx.providerBridge == nil {
		t.Fatalf("refused Pi launch error = %v; no provider bridge was prepared", runErr)
	}
	if !providerBridgeReachable(t, tx.providerBridge) {
		t.Fatalf("the provider bridge was unreachable before the compensation ran: %v", runErr)
	}
	returnErr := tx.abort(context.Background(), runErr)
	if want := "provider-bridge,native-start-outcome"; probe.sequence() != want {
		t.Fatalf("unowned bridge compensation sequence = %q, want %q", probe.sequence(), want)
	}
	if !errors.Is(returnErr, runErr) {
		t.Fatalf("unowned bridge compensation lost the launch failure: %v", returnErr)
	}
	if providerBridgeReachable(t, tx.providerBridge) {
		t.Fatal("the real closeUnownedProviderBridge body left the provider bridge reachable")
	}
}

func TestGoalAdmissionTxSettlesNativeOpenFailureAsAnUnknownOutcome(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	openFailure := errors.New("native Goal session Open refused")
	session.onOpen = func() error { return openFailure }

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if runErr == nil || !errors.Is(runErr, openFailure) || !strings.Contains(runErr.Error(), "open native Goal session: ") {
		t.Fatalf("native Open failure = %v, want the open-phase wrapper around %v", runErr, openFailure)
	}
	if !tx.nativeOutcomeCompensation.armed || tx.nativeOutcomeCompensation.released {
		t.Fatalf("Open failure outcome compensation = armed %t, released %t; want an owned unknown-outcome wrap",
			tx.nativeOutcomeCompensation.armed, tx.nativeOutcomeCompensation.released)
	}
	calls := session.callsSnapshot()
	if !slices.Contains(calls, "close") {
		t.Fatalf("Open failure did not close the returned native session: %#v", calls)
	}
	if slices.Contains(calls, "start_turn") {
		t.Fatalf("Open failure crossed into the native turn: %#v", calls)
	}
	returnErr := tx.abort(context.Background(), runErr)
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("Open failure compensation sequence = %q, want %q", probe.sequence(), want)
	}
	if !errors.Is(returnErr, openFailure) {
		t.Fatalf("abort() lost the Open failure: %v", returnErr)
	}
	if reason, typed := taskResultFailureReason(returnErr); !typed || reason != protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("abort() left the Open failure untyped: %v (reason %q, typed %t)", returnErr, reason, typed)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := pendingSessionAttachDelivery(journal, tx.localHandleID); present {
		t.Fatalf("Open failure kept the unready attach delivery: %#v", journal.PendingGoalDeliveries)
	}
	if !journal.RetainWorkspace {
		t.Fatalf("Open failure did not retain the unproven workspace: %#v", journal)
	}
	assertGoalSessionUncertain(t, store, "Open failure")
}

func TestGoalAdmissionTxSettlesAttachFailureBeforeTheNativeOpen(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	active := &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	app.running[key] = active
	adapterValue, err := app.harnessRegistry.Lookup(harness.KindCodex)
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*fakeNativeGoalAdapter)
	adapter.onStart = func(harness.StartRequest, *fakeNativeGoalSession) error {
		app.mu.Lock()
		if app.running[key] == active {
			delete(app.running, key)
		}
		app.mu.Unlock()
		return nil
	}

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if runErr == nil || !strings.Contains(runErr.Error(), "Goal run is no longer active") {
		t.Fatalf("attach failure = %v, want the publication refusal of a torn-down run", runErr)
	}
	calls := session.callsSnapshot()
	if !slices.Contains(calls, "start") || !slices.Contains(calls, "close") {
		t.Fatalf("attach failure native calls = %#v, want Start and the compensation Close", calls)
	}
	if slices.Contains(calls, "open") || slices.Contains(calls, "start_turn") {
		t.Fatalf("attach failure crossed the Open boundary: %#v", calls)
	}
	assertGoalSessionUncertain(t, store, "attach failure")
	returnErr := tx.abort(context.Background(), runErr)
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("attach failure compensation sequence = %q, want %q", probe.sequence(), want)
	}
	if !errors.Is(returnErr, runErr) || returnErr.Error() != runErr.Error() {
		t.Fatalf("attach failure compensation changed the failure: %v", returnErr)
	}
	if reason, typed := taskResultFailureReason(returnErr); !typed || reason != protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("attach failure was left untyped: %v (reason %q, typed %t)", returnErr, reason, typed)
	}
}

func TestGoalAdmissionTxRejectsAMismatchedModelProfileBeforeAnyAcquisition(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	admission.ModelProfile = "other-profile"

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observe(false)
	runErr := tx.run(context.Background())
	want := fmt.Sprintf("Goal admission model_profile %q does not match configured runtime agent_profile %q", "other-profile", app.config.Runtime.AgentProfile)
	if runErr == nil || runErr.Error() != want {
		t.Fatalf("mismatched model profile = %v, want %q", runErr, want)
	}
	if calls := session.callsSnapshot(); len(calls) != 0 {
		t.Fatalf("mismatched model profile reached the native adapter: %#v", calls)
	}
	for _, entry := range probe.entries {
		if entry.armed || entry.released {
			t.Fatalf("mismatched model profile armed compensation %q: armed %t, released %t", probe.names[entry], entry.armed, entry.released)
		}
	}
	if returnErr := tx.abort(context.Background(), runErr); !errors.Is(returnErr, runErr) || returnErr.Error() != runErr.Error() {
		t.Fatalf("abort() changed the validation failure: %v", returnErr)
	}
	if reason, typed := taskResultFailureReason(runErr); typed {
		t.Fatalf("validation failure was typed %q: %v", reason, runErr)
	}
	if sessions, err := store.ListGoalSessions(); err != nil || len(sessions) != 0 {
		t.Fatalf("validation failure persisted a Goal session: %#v, error = %v", sessions, err)
	}
}

func TestGoalAdmissionTxResumesARetainedNativeSession(t *testing.T) {
	app, store, session, key, claim, admission, source := retainedResumeFixture(t)

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	if runErr := tx.run(context.Background()); runErr != nil {
		t.Fatalf("retained resume error = %v, want a completed launch without effectful compensation", runErr)
	}
	wantResume := harness.ResumeHandle{
		LocalHandleID: source.LocalHandleID, NativeSessionID: source.NativeSessionID,
		NativeSessionFilename: source.NativeSessionFilename,
		WorkspaceFingerprint:  source.WorkspaceFingerprint, NativeVersion: source.HarnessVersion,
	}
	if session.request.Resume == nil || *session.request.Resume != wantResume {
		t.Fatalf("retained resume request = %#v, want %#v", session.request.Resume, wantResume)
	}
	if tx.reuseCompensation.armed || tx.cloneCompensation.armed || tx.bridgeCompensation.armed {
		t.Fatalf("retained resume armed unrelated compensation: reuse %t, clone %t, bridge %t",
			tx.reuseCompensation.armed, tx.cloneCompensation.armed, tx.bridgeCompensation.armed)
	}
	if !tx.nativeOutcomeCompensation.armed {
		t.Fatal("retained resume never armed the native-start outcome compensation")
	}
	if returnErr := tx.abort(context.Background(), nil); returnErr != nil {
		t.Fatalf("abort() after a retained resume returned %v, want no compensation effect", returnErr)
	}
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("retained resume compensation sequence = %q, want only the no-op %q", probe.sequence(), want)
	}
	journal, err := store.LoadJournal(key)
	if err != nil || journal.LocalState != "running" {
		t.Fatalf("retained resume journal = %#v, error = %v", journal, err)
	}
}

func TestGoalAdmissionTxSettlesRetainedOpenFailureWithoutDiscardingItsReadyAttach(t *testing.T) {
	app, store, session, key, claim, admission, _ := retainedResumeFixture(t)
	openFailure := errors.New("retained native Goal session Open refused")
	session.onOpen = func() error { return openFailure }

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if runErr == nil || !errors.Is(runErr, openFailure) || runErr.Error() != "open native Goal session: "+openFailure.Error() {
		t.Fatalf("retained Open failure = %v, want only the open-phase wrapper around %v", runErr, openFailure)
	}
	calls := session.callsSnapshot()
	if !slices.Contains(calls, "close") {
		t.Fatalf("retained Open failure did not close the returned native session: %#v", calls)
	}
	if slices.Contains(calls, "start_turn") {
		t.Fatalf("retained Open failure crossed into the native turn: %#v", calls)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	delivery, present := pendingSessionAttachDelivery(journal, tx.localHandleID)
	if !present || !delivery.Ready {
		t.Fatalf("retained Open failure lost the ready attach barrier: present %t, delivery %#v", present, delivery)
	}
	assertGoalSessionUncertain(t, store, "retained Open failure")
	returnErr := tx.abort(context.Background(), runErr)
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("retained Open failure compensation sequence = %q, want %q", probe.sequence(), want)
	}
	if !errors.Is(returnErr, openFailure) {
		t.Fatalf("abort() lost the retained Open failure: %v", returnErr)
	}
	if reason, typed := taskResultFailureReason(returnErr); !typed || reason != protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("abort() left the retained Open failure untyped: %v (reason %q, typed %t)", returnErr, reason, typed)
	}
}

func TestGoalAdmissionTxSettlesPostLaunchCancellationBeforeTheTurn(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	active := &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	app.running[key] = active
	openFailure := errors.New("native Goal session Open refused after the cancellation")
	session.onOpen = func() error {
		app.mu.Lock()
		if app.running[key] == active {
			active.cancelled = true
			active.cancelCommandID = "cancel-open-failure"
		}
		app.mu.Unlock()
		return openFailure
	}

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if runErr != nil {
		t.Fatalf("post-launch cancellation error = %v, want the settled cancellation", runErr)
	}
	calls := session.callsSnapshot()
	if !slices.Contains(calls, "close") || slices.Contains(calls, "start_turn") {
		t.Fatalf("post-launch cancellation native calls = %#v, want a closed session without a native turn", calls)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil || len(sessions) != 1 || !goalSessionClosed(sessions[0]) {
		t.Fatalf("post-launch cancellation Goal session = %#v, error = %v", sessions, err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := pendingSessionAttachDelivery(journal, tx.localHandleID); present {
		t.Fatalf("settled cancellation kept the unready attach delivery: %#v", journal.PendingGoalDeliveries)
	}
	if returnErr := tx.abort(context.Background(), runErr); returnErr != nil {
		t.Fatalf("abort() after a settled cancellation returned %v, want no compensation error", returnErr)
	}
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("settled cancellation compensation sequence = %q, want only the no-op %q", probe.sequence(), want)
	}
}

func TestGoalAdmissionTxSettlesLeaseExpiryAfterTheNativeOpen(t *testing.T) {
	app, store, session, key, claim, admission := nativeAdmissionFixture(t)
	active := &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	app.running[key] = active
	leaseReason := "native start lease expired after the test Open"
	session.onOpen = func() error {
		app.mu.Lock()
		if app.running[key] == active {
			active.nativeStartLeaseExpired = true
			active.nativeStartLeaseReason = leaseReason
		}
		app.mu.Unlock()
		return nil
	}

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if reason, typed := taskResultFailureReason(runErr); !typed || reason != protocol.TaskResultReasonProcessFailure {
		t.Fatalf("post-Open lease expiry error = %v (reason %q, typed %t); want the process-failure reason", runErr, reason, typed)
	}
	if !strings.Contains(runErr.Error(), leaseReason) {
		t.Fatalf("post-Open lease expiry error = %v, want the watchdog reason %q", runErr, leaseReason)
	}
	calls := session.callsSnapshot()
	if !slices.Contains(calls, "open") || !slices.Contains(calls, "close") {
		t.Fatalf("post-Open lease expiry native calls = %#v, want the Open and its compensation Close", calls)
	}
	if slices.Contains(calls, "start_turn") {
		t.Fatalf("post-Open lease expiry crossed into the native turn: %#v", calls)
	}
	assertGoalSessionUncertain(t, store, "post-Open lease expiry")
	returnErr := tx.abort(context.Background(), runErr)
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("post-Open lease expiry compensation sequence = %q, want only the no-op %q", probe.sequence(), want)
	}
	if returnErr.Error() != runErr.Error() {
		t.Fatalf("post-Open lease expiry abort changed the settled failure: %v", returnErr)
	}
}

func TestGoalAdmissionTxHandsTheProviderBridgeToTheNativeOwner(t *testing.T) {
	if requiresLinuxV2NativeOwner(harness.KindPi) {
		t.Skip("Linux Pi admission requires the Linux v2 native owner and a Pi provider-extension share; no shared non-Linux fixture drives that path")
	}
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	admission.ProviderScope = &protocol.ProviderScope{
		ResourceIDs: []string{admission.Subject.ResourceID},
		OperationsByResource: map[string][]protocol.ProviderOperation{
			admission.Subject.ResourceID: {protocol.ProviderOperationResourceSync},
		},
	}
	capabilities := testVerifiedNativeCapabilities(harness.KindPi)
	capabilities.ProviderAccess = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
	t.Cleanup(func() { _ = store.Close() })
	profile := app.config.AgentProfiles[app.config.Runtime.AgentProfile]
	profile.ProviderAccess = true
	app.config.AgentProfiles[app.config.Runtime.AgentProfile] = profile
	controlClient.providerAccess = &protocol.ProviderAccess{
		Path:  "/api/v1/provider-actions",
		Token: "provider-token-do-not-persist",
		Grants: []protocol.ProviderGrant{{
			ResourceID: admission.Subject.ResourceID,
			Provider:   "github",
			Kind:       "repository",
			Operations: []string{"resource.sync"},
		}},
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	claim := protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", Work: controlClient.work, ProviderAccess: controlClient.providerAccess}
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	openFailure := errors.New("pi native session Open refused after the provider bridge transfer")
	session.onOpen = func() error { return openFailure }

	tx := newGoalAdmissionTx(app, key, claim, admission, nil)
	probe := newGoalAdmissionCompensationProbe(tx)
	probe.observeDelegate()
	runErr := tx.run(context.Background())
	if runErr == nil || !errors.Is(runErr, openFailure) {
		t.Fatalf("session-owned bridge Open failure = %v, want %v", runErr, openFailure)
	}
	if !tx.bridgeCompensation.armed || !tx.bridgeCompensation.released {
		t.Fatalf("provider bridge compensation = armed %t, released %t; want a bridge the session owns",
			tx.bridgeCompensation.armed, tx.bridgeCompensation.released)
	}
	calls := session.callsSnapshot()
	if !slices.Contains(calls, "close") {
		t.Fatalf("session-owned bridge Open failure did not close the native session: %#v", calls)
	}
	returnErr := tx.abort(context.Background(), runErr)
	if want := "native-start-outcome"; probe.sequence() != want {
		t.Fatalf("session-owned bridge compensation sequence = %q, want only the no-op %q", probe.sequence(), want)
	}
	if !errors.Is(returnErr, runErr) || returnErr.Error() != runErr.Error() {
		t.Fatalf("abort() changed the session-owned bridge failure: %v", returnErr)
	}
}

func retainedResumeFixture(t *testing.T) (*daemon, *state.Store, *fakeNativeGoalSession, state.RunKey, protocol.ClaimResponse, protocol.Admission, state.GoalSessionJournal) {
	t.Helper()
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := testVerifiedNativeCapabilities(harness.KindOMP)
	capabilities.Resume = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindOMP, config.RuntimeHarnessOMP, capabilities)
	t.Cleanup(func() { _ = store.Close() })
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailableNativeGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities, 1)
	session.handle = harness.NativeSessionHandle{ID: source.NativeSessionID, Filename: source.NativeSessionFilename}
	controlClient.attachSessionID = controlSessionID
	key := state.RunKey{RunID: "run-1", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	claim := protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", Work: controlClient.work,
		HarnessSessionID: &controlSessionID, HarnessBindingID: &resumeBindingID,
	}
	app.running[key] = &runningRun{runLifecycle: runLifecycle{starting: true, claimed: true}}
	return app, store, session, key, claim, admission, source
}

func pendingSessionAttachDelivery(journal state.RunJournal, localHandleID string) (state.GoalDelivery, bool) {
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliverySessionAttach && delivery.DeliveryID == localHandleID {
			return delivery, true
		}
	}
	return state.GoalDelivery{}, false
}

func assertGoalSessionUncertain(t *testing.T, store *state.Store, stage string) {
	t.Helper()
	sessions, err := store.ListGoalSessions()
	if err != nil || len(sessions) != 1 || sessions[0].LaunchState != state.GoalSessionLaunchStateUncertain {
		t.Fatalf("%s Goal session = %#v, error = %v; want one uncertain launch", stage, sessions, err)
	}
}

func providerBridgeReachable(t *testing.T, bridge *harness.ProviderBridgeLaunch) bool {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, bridge.URL, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Symmetry-Bridge-Nonce", bridge.Nonce)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	response, requestErr := client.Do(request)
	if requestErr != nil {
		return false
	}
	_ = response.Body.Close()
	return true
}
