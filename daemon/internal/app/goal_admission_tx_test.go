package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
