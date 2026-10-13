//go:build linux && amd64

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

func TestLinuxV2CallbacksBindClaimFenceAndReplayPostRenameReceipt(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1,
		ClaimID: "claim-1", LocalState: "claiming", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease",
		LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"},
	}); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	app := linuxV2AppTestDaemon(t, store, key, false)
	callbacks := app.linuxV2JournalCallbacks(key, fence)
	reservation := testAppLinuxV2Reservation()

	wrongFence := fence
	wrongFence.LeaseToken = "another-lease"
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	err = app.linuxV2JournalCallbacks(key, wrongFence).Reserve(reservation)
	restore()
	if err == nil || writes != 0 {
		t.Fatalf("wrong-fence reservation = %v, writes=%d; want rejection before Store mutation", err, writes)
	}
	unchanged, err := store.LoadJournal(key)
	if err != nil || unchanged.ContainmentCgroup != nil {
		t.Fatalf("wrong-fence reservation changed journal: %#v, error=%v", unchanged.ContainmentCgroup, err)
	}
	handoff, authorityValue, committedReservation := bindAppLinuxV2Owner(t, store, key, reservation)
	otherKey := state.RunKey{RunID: "run-other", Generation: 2}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: otherKey, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1,
		ClaimID: "claim-other", LocalState: "claiming", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(otherKey, protocol.ClaimResponse{
		RunID: otherKey.RunID, Generation: otherKey.Generation, ClaimID: "claim-other", LeaseToken: "lease-other",
		LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"},
	}); err != nil {
		t.Fatal(err)
	}
	writes = 0
	restore = store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	err = app.linuxV2JournalCallbacks(otherKey, fence).Reserve(reservation)
	restore()
	otherJournal, otherErr := store.LoadJournal(otherKey)
	if err == nil || writes != 0 || otherErr != nil || otherJournal.ContainmentCgroup != nil {
		t.Fatalf("wrong RunKey reservation = %v, writes=%d, journal=%#v, read error=%v", err, writes, otherJournal.ContainmentCgroup, otherErr)
	}

	receipt := appLinuxV2StopReceipt(t, authorityValue)
	writes = 0
	restore = store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		if err := writeAppStateAtomic(path, data); err != nil {
			return err
		}
		if writes == 1 {
			return errors.New("injected post-rename stop receipt acknowledgement loss")
		}
		return nil
	})
	if err := callbacks.RecordStopReceipt(handoff, receipt); err != nil {
		restore()
		t.Fatalf("RecordStopReceipt() did not replay exact unknown mutation: %v", err)
	}
	restore()
	if writes != 2 {
		t.Fatalf("stop receipt write attempts = %d, want initial rename plus exact replay barrier", writes)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContainmentAuthority == nil || loaded.ContainmentAuthority.StopReceipt == nil ||
		!loaded.ContainmentAuthority.StopReceipt.Equal(receipt) || loaded.ContainmentCgroup == nil ||
		!loaded.ContainmentCgroup.Equal(committedReservation) || loaded.PID != authorityValue.TargetPID ||
		loaded.ProcessIdentity != authorityValue.TargetIdentity {
		t.Fatalf("receipt replay journal = %#v, want exact receipt with marker/reservation retained", loaded)
	}
}

func TestLinuxV2AppReleaseAdmissionCallbackRejectsForeignUnresolvedOwner(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	foreignKey := state.RunKey{RunID: "foreign-release-admission", Generation: 1}
	foreign := addUnresolvedLinuxV2WitnessJournal(t, store, foreignKey)
	key := state.RunKey{RunID: "run-1", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1,
		ClaimID: "claim-1", LocalState: "claiming", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease",
		LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"},
	}); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := journal.Fence()
	if !foreign.HasPendingContainment() {
		t.Fatal("foreign release-admission fixture is not unresolved")
	}
	callbacks := linuxV2AppTestDaemon(t, store, key, false).linuxV2JournalCallbacks(key, fence)
	if callbacks.CheckNativeAdmission == nil {
		t.Fatal("App Linux v2 callbacks omitted the pre-RELEASE admission check")
	}
	before, err := json.Marshal(loadWitnessJournal(t, store, foreignKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := callbacks.CheckNativeAdmission(); err == nil {
		t.Fatal("App native RELEASE admission passed an unresolved foreign owner")
	}
	after, err := json.Marshal(loadWitnessJournal(t, store, foreignKey))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("App native RELEASE admission mutated the unresolved foreign journal")
	}
}

func TestLinuxV2AdmissionCallbackRejectsLocallyCancelledOrExpiredTarget(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	app := linuxV2AppTestDaemon(t, store, key, false)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := app.linuxV2JournalCallbacks(key, journal.Fence())
	if err := callbacks.CheckNativeAdmission(); err != nil {
		t.Fatalf("healthy exact target failed pre-RELEASE admission: %v", err)
	}
	app.mu.Lock()
	active := app.running[key]
	active.cancelled = true
	app.mu.Unlock()
	if err := callbacks.CheckNativeAdmission(); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("locally cancelled target admission = %v; want fail-closed target refusal", err)
	}
	app.mu.Lock()
	active.cancelled = false
	active.localLeaseDeadlineAt = app.localNow().Add(leaseSafetyMargin - time.Nanosecond)
	app.mu.Unlock()
	if err := callbacks.CheckNativeAdmission(); !errors.Is(err, errLeaseDeadlineReached) {
		t.Fatalf("locally expired target admission = %v; want errLeaseDeadlineReached", err)
	}
}

func TestLinuxV2CloneNoCheckoutWithoutTaskResultRetainsExactRun(t *testing.T) {
	store, key, admission, grant, fence := linuxCloneCallbackFixture(t)
	defer store.Close()
	app := &daemon{
		store:     store,
		workspace: &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}},
		running:   make(map[state.RunKey]*runningRun),
	}
	cloneRun := &linuxCloneRun{workspace: app.workspace.(linuxV2CloneWorkspace), grant: grant}
	callbacks := execution.LinuxV2JournalCallbacks{}
	if err := app.installLinuxCloneRunnerCallbacks(key, fence, admission, cloneRun, &callbacks); err != nil {
		t.Fatalf("install Linux clone callbacks: %v", err)
	}
	commit, proposed, err := callbacks.LinuxCloneProposedCommit(context.Background(), execution.LinuxCloneRunOutcome{
		ExitCode: 0, StopReceiptValid: true, LeafRemovalProven: true,
	})
	if err != nil || proposed || commit != "" {
		t.Fatalf("LinuxCloneProposedCommit() = %q, %t, %v; want no candidate without a persisted TaskResult", commit, proposed, err)
	}
	if err := callbacks.RetainUnpromotedLinuxClone(context.Background()); err != nil {
		t.Fatalf("RetainUnpromotedLinuxClone() without checkout/result: %v", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace || journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(grant.Intent) ||
		journal.LinuxCloneSeedReceipt == nil || !journal.LinuxCloneSeedReceipt.Equal(grant.Seed) ||
		journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionIntent != nil ||
		journal.LinuxClonePromotionReceipt != nil {
		t.Fatalf("no-checkout retention journal = %#v; want only exact intent/seed plus RetainWorkspace", journal)
	}
}

func TestLinuxV2CloneCandidateWithFailedRunnerOutcomeCannotBePromoted(t *testing.T) {
	store, key, admission, grant, fence := linuxCloneCallbackFixture(t)
	defer store.Close()
	_, _, _ = bindAppLinuxV2Owner(t, store, key, testAppLinuxV2Reservation())
	workspaceService := &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}}
	cloneRun := &linuxCloneRun{workspace: workspaceService, grant: grant}
	app := &daemon{
		store: store, workspace: workspaceService,
		options: options{newID: ids(), clock: time.Now},
		running: make(map[state.RunKey]*runningRun),
	}
	app.running[key] = &runningRun{linuxClone: cloneRun}
	callbacks := execution.LinuxV2JournalCallbacks{}
	if err := app.installLinuxCloneRunnerCallbacks(key, fence, admission, cloneRun, &callbacks); err != nil {
		t.Fatalf("install Linux clone callbacks: %v", err)
	}
	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: key.RunID, Generation: key.Generation,
		BindingKey: grant.Intent.BindingKey, ResourceID: grant.Intent.ResourceID, Target: grant.Intent.Target,
		Nonce: grant.Intent.Nonce, SeedDigest: grant.Seed.Digest, BaseCommit: grant.Intent.BaseCommit,
		TargetDevice: 1, TargetInode: 2,
	}
	if err := callbacks.RecordLinuxCloneCheckoutReady(checkout); err != nil {
		t.Fatalf("record exact checkout receipt: %v", err)
	}
	candidate := validNativeTaskResult(t, admission)
	candidate.Kind = protocol.TaskResultCandidateCompletion
	candidate.Subject.Commit = strings.Repeat("1", len(admission.Subject.Commit))
	candidate.Subject.TreeDigest = "sha256:" + strings.Repeat("2", 64)
	subjectHash, err := candidate.Subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	candidate.SubjectHash = subjectHash
	payload, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.queueNativeEvent(key, harness.Event{Kind: harness.EventTaskResult, Payload: payload}); err != nil {
		t.Fatalf("persist candidate TaskResult event: %v", err)
	}
	commit, proposed, err := callbacks.LinuxCloneProposedCommit(context.Background(), execution.LinuxCloneRunOutcome{
		ExitCode: 17, WaitError: errors.New("native process exited unsuccessfully"),
		StopReceiptValid: true, LeafRemovalProven: true,
	})
	if err != nil || proposed || commit != "" {
		t.Fatalf("failed Runner outcome proposal = %q, %t, %v; want no promotion candidate", commit, proposed, err)
	}
	if err := callbacks.RetainUnpromotedLinuxClone(context.Background()); err != nil {
		t.Fatalf("retain candidate after failed Runner outcome: %v", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace || journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil || journal.LinuxCloneCheckoutReceipt == nil ||
		!journal.LinuxCloneCheckoutReceipt.Equal(checkout) {
		t.Fatalf("failed candidate settlement journal = %#v; want retained exact checkout without promotion", journal)
	}
}

func bindAppLinuxCloneCheckoutAndStop(
	t *testing.T,
	store *state.Store,
	key state.RunKey,
	grant authority.LinuxCloneGrant,
	fence protocol.Fence,
) (authority.SupervisorHandoff, authority.Supervisor, authority.LinuxCloneCheckoutReceipt, authority.StopReceipt) {
	t.Helper()
	handoff, owner, _ := bindAppLinuxV2Owner(t, store, key, testAppLinuxV2Reservation())
	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: key.RunID, Generation: key.Generation,
		BindingKey: grant.Intent.BindingKey, ResourceID: grant.Intent.ResourceID, Target: grant.Intent.Target,
		Nonce: grant.Intent.Nonce, SeedDigest: grant.Seed.Digest, BaseCommit: grant.Intent.BaseCommit,
		TargetDevice: 1, TargetInode: 2,
	}
	cloneCallbacks := (&daemon{store: store}).linuxCloneJournalCallbacks(key, fence)
	if err := cloneCallbacks.CheckoutReady(grant.Intent, checkout); err != nil {
		t.Fatalf("record exact checkout before promotion fixture: %v", err)
	}
	stopReceipt := appLinuxV2StopReceipt(t, owner)
	if err := (&daemon{store: store}).linuxV2JournalCallbacks(key, fence).RecordStopReceipt(handoff, stopReceipt); err != nil {
		t.Fatalf("record exact stop receipt before promotion fixture: %v", err)
	}
	return handoff, owner, checkout, stopReceipt
}

func TestLinuxClonePromotionIntentAndReceiptCallbacksReplayAfterRestart(t *testing.T) {
	// The exact receipt below represents a Workspace A-ref readback completed
	// before daemon loss; the Workspace package owns the real Git-effect replay.
	directory := t.TempDir()
	store, key, _, grant, fence := linuxCloneCallbackFixtureAt(t, directory)
	currentStore := store
	t.Cleanup(func() {
		if currentStore != nil {
			_ = currentStore.Close()
		}
	})
	_, _, checkout, stopReceipt := bindAppLinuxCloneCheckoutAndStop(t, store, key, grant, fence)
	candidate := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: grant.Intent.ResourceID,
		Commit: strings.Repeat("1", len(grant.Intent.BaseCommit)), TreeDigest: "sha256:" + strings.Repeat("d", 64),
	}
	if err := candidate.Validate(); err != nil {
		t.Fatalf("promotion intent fixture is invalid: %v", err)
	}
	app := linuxV2AppTestDaemon(t, store, key, false)
	callbacks := app.linuxCloneJournalCallbacks(key, fence)
	intentWrites := 0
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		var journal state.RunJournal
		if err := json.Unmarshal(data, &journal); err == nil && journal.Key() == key &&
			journal.LinuxClonePromotionIntent != nil && journal.LinuxClonePromotionIntent.Equal(candidate) {
			intentWrites++
		}
		if err := writeAppStateAtomic(path, data); err != nil {
			return err
		}
		if intentWrites == 1 {
			return errors.New("injected post-rename promotion-intent acknowledgement loss")
		}
		return nil
	})
	if err := callbacks.ReservePromotion(grant.Intent, candidate); err != nil {
		restoreWriter()
		t.Fatalf("ReservePromotion() did not replay the exact intent after an unknown write: %v", err)
	}
	restoreWriter()
	if intentWrites != 2 {
		t.Fatalf("promotion-intent write attempts = %d, want initial rename plus exact replay", intentWrites)
	}
	reserved, err := store.LoadJournal(key)
	if err != nil || reserved.LinuxClonePromotionIntent == nil || !reserved.LinuxClonePromotionIntent.Equal(candidate) ||
		reserved.LinuxClonePromotionReceipt != nil || reserved.ContainmentAuthority == nil || reserved.ContainmentCgroup == nil {
		t.Fatalf("reserved promotion journal = %#v, error=%v; want exact intent and containment retained", reserved, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	currentStore = nil
	restarted, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	currentStore = restarted
	app = &daemon{store: restarted}
	callbacks = app.linuxCloneJournalCallbacks(key, fence)
	promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil {
		t.Fatal(err)
	}
	receipt := authority.LinuxClonePromotionReceipt{
		Version: authority.LinuxCloneVersion, Commit: candidate.Commit, TreeDigest: candidate.TreeDigest, Ref: promotionRef,
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("A-ref readback receipt fixture is invalid: %v", err)
	}
	receiptWrites := 0
	restoreWriter = restarted.SetAtomicWriterForTesting(func(path string, data []byte) error {
		var journal state.RunJournal
		if err := json.Unmarshal(data, &journal); err == nil && journal.Key() == key &&
			journal.LinuxClonePromotionIntent != nil && journal.LinuxClonePromotionIntent.Equal(candidate) &&
			journal.LinuxClonePromotionReceipt != nil && journal.LinuxClonePromotionReceipt.Equal(receipt) {
			receiptWrites++
		}
		if err := writeAppStateAtomic(path, data); err != nil {
			return err
		}
		if receiptWrites == 1 {
			return errors.New("injected post-rename promotion-receipt acknowledgement loss")
		}
		return nil
	})
	if err := callbacks.RecordPromotion(grant.Intent, candidate, receipt); err != nil {
		restoreWriter()
		t.Fatalf("RecordPromotion() did not replay the exact ref receipt after restart: %v", err)
	}
	restoreWriter()
	if receiptWrites != 2 {
		t.Fatalf("promotion-receipt write attempts = %d, want initial rename plus exact replay", receiptWrites)
	}
	loaded, err := restarted.LoadJournal(key)
	if err != nil || loaded.LinuxClonePromotionIntent == nil || !loaded.LinuxClonePromotionIntent.Equal(candidate) ||
		loaded.LinuxClonePromotionReceipt == nil || !loaded.LinuxClonePromotionReceipt.Equal(receipt) ||
		loaded.ContainmentAuthority == nil || loaded.ContainmentCgroup == nil || !loaded.HasPendingContainment() ||
		loaded.LinuxCloneCheckoutReceipt == nil || !loaded.LinuxCloneCheckoutReceipt.Equal(checkout) ||
		loaded.ContainmentAuthority.StopReceipt == nil || !loaded.ContainmentAuthority.StopReceipt.Equal(stopReceipt) {
		t.Fatalf("promotion receipt journal after restart = %#v, error=%v; want exact A-ref receipt and live containment retained", loaded, err)
	}
}

func TestLinuxClonePromotionCannotReserveWithoutOpaqueRelease(t *testing.T) {
	store, key, admission, grant, fence := linuxCloneCallbackFixture(t)
	defer store.Close()
	_, _, checkout, stopReceipt := bindAppLinuxCloneCheckoutAndStop(t, store, key, grant, fence)
	workspaceService := &linuxClonePromotionProbeWorkspace{linuxCloneWorkspaceCallbackFixture: &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}}}
	cloneRun := &linuxCloneRun{workspace: workspaceService, grant: grant}
	app := linuxV2AppTestDaemon(t, store, key, false)
	app.workspace = workspaceService
	app.options.newID = ids()
	app.running[key].linuxClone = cloneRun
	callbacks := &execution.LinuxV2JournalCallbacks{}
	if err := app.installLinuxCloneRunnerCallbacks(key, fence, admission, cloneRun, callbacks); err != nil {
		t.Fatalf("install Linux clone runner callbacks: %v", err)
	}
	if err := callbacks.RecordLinuxCloneCheckoutReady(checkout); err != nil {
		t.Fatalf("read back exact checkout through App callback: %v", err)
	}
	candidate := validNativeTaskResult(t, admission)
	candidate.Kind = protocol.TaskResultCandidateCompletion
	candidate.Subject.Commit = strings.Repeat("1", len(admission.Subject.Commit))
	candidate.Subject.TreeDigest = "sha256:" + strings.Repeat("2", 64)
	subjectHash, err := candidate.Subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	candidate.SubjectHash = subjectHash
	payload, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.queueNativeEvent(key, harness.Event{Kind: harness.EventTaskResult, Payload: payload}); err != nil {
		t.Fatalf("persist candidate TaskResult event: %v", err)
	}
	commit, proposed, err := callbacks.LinuxCloneProposedCommit(context.Background(), execution.LinuxCloneRunOutcome{
		ExitCode: 0, StopReceiptValid: true, LeafRemovalProven: true,
	})
	if err != nil || !proposed || commit != candidate.Subject.Commit {
		t.Fatalf("LinuxCloneProposedCommit() = %q, %t, %v; want the exact successful candidate", commit, proposed, err)
	}
	writes := 0
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	_, err = callbacks.PromoteLinuxClone(context.Background(), grant, checkout, commit, stopReceipt, platform.LinuxCgroupV2Release{})
	restoreWriter()
	if err == nil || workspaceService.promoteCalls != 0 || writes != 0 {
		t.Fatalf("unissued-release promotion = %v, Workspace calls=%d, Store writes=%d; want rejection before both effects", err, workspaceService.promoteCalls, writes)
	}
	journal, err := store.LoadJournal(key)
	if err != nil || journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil {
		t.Fatalf("unissued release changed promotion authority: %#v, error=%v", journal, err)
	}
	promotionIntent := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: candidate.Subject.ResourceID,
		Commit: candidate.Subject.Commit, TreeDigest: candidate.Subject.TreeDigest,
	}
	if err := app.linuxCloneJournalCallbacks(key, fence).ReservePromotion(grant.Intent, promotionIntent); err != nil {
		t.Fatalf("persist exact crash-recovery intent fixture: %v", err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil || journal.LinuxClonePromotionIntent == nil || !journal.LinuxClonePromotionIntent.Equal(promotionIntent) {
		t.Fatalf("promotion intent fixture did not read back: %#v, error=%v", journal.LinuxClonePromotionIntent, err)
	}
	if err := app.reconcileLinuxClonePromotion(context.Background(), journal, platform.LinuxCgroupV2Release{}); err == nil ||
		workspaceService.readCalls != 0 || workspaceService.promoteCalls != 0 {
		t.Fatalf("restart promotion reconciliation without physical release = %v, Workspace reads=%d promotes=%d; want fail before effects",
			err, workspaceService.readCalls, workspaceService.promoteCalls)
	}
}

func TestLinuxClonePromotionRefMissingAfterLeaseExpiryRemainsMissing(t *testing.T) {
	fixture := newLinuxClonePromotionRecoveryFixture(t, true)
	defer fixture.store.Close()
	workspaceService := &linuxClonePromotionRecoveryWorkspace{
		linuxCloneWorkspaceCallbackFixture: &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}},
		readErr:                            workspace.ErrLinuxClonePromotionRefMissing,
	}
	app := &daemon{store: fixture.store, workspace: workspaceService}
	journal, err := fixture.store.LoadJournal(fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.reconcileLinuxClonePromotion(context.Background(), journal, platform.LinuxCgroupV2Release{}); err == nil ||
		workspaceService.readCalls != 1 || workspaceService.promoteCalls != 0 || workspaceService.refPresent {
		t.Fatalf("expired missing-ref recovery = %v, reads=%d promotes=%d refPresent=%t; want read-only refusal",
			err, workspaceService.readCalls, workspaceService.promoteCalls, workspaceService.refPresent)
	}

	// Exercise the same last-moment target gate used immediately before the live
	// and recovery Workspace.PromoteLinuxClone calls. After the caller's physical
	// release checks, an expired State lease still keeps the missing A ref absent.
	effectApp := linuxV2AppTestDaemon(t, fixture.store, fixture.key, false)
	effectCalls := 0
	err = effectApp.withLinuxV2EffectReadyTarget(context.Background(), fixture.key, fixture.fence, func() error {
		effectCalls++
		workspaceService.refPresent = true
		return nil
	})
	if err == nil || effectCalls != 0 || workspaceService.refPresent {
		t.Fatalf("expired promotion effect = %v, calls=%d refPresent=%t; want no A-owned Git mutation",
			err, effectCalls, workspaceService.refPresent)
	}
	latest, err := fixture.store.LoadJournal(fixture.key)
	if err != nil || latest.LinuxClonePromotionIntent == nil ||
		!latest.LinuxClonePromotionIntent.Equal(fixture.candidate) || latest.LinuxClonePromotionReceipt != nil {
		t.Fatalf("expired missing-ref journal = %#v, error=%v; want intent retained and no receipt", latest, err)
	}
}

func TestLinuxClonePromotionExistingRefIsReadOnlyAfterLeaseExpiry(t *testing.T) {
	fixture := newLinuxClonePromotionRecoveryFixture(t, true)
	defer fixture.store.Close()
	workspaceService := &linuxClonePromotionRecoveryWorkspace{
		linuxCloneWorkspaceCallbackFixture: &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}},
		readReceipt:                        fixture.receipt,
	}
	app := &daemon{store: fixture.store, workspace: workspaceService}
	journal, err := fixture.store.LoadJournal(fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.reconcileLinuxClonePromotion(context.Background(), journal, platform.LinuxCgroupV2Release{}); err != nil {
		t.Fatalf("read-only exact-ref recovery after lease expiry: %v", err)
	}
	if workspaceService.readCalls != 1 || workspaceService.promoteCalls != 0 || !workspaceService.refPresent {
		t.Fatalf("expired existing-ref reconciliation: reads=%d promotes=%d refPresent=%t; want one read and no A write",
			workspaceService.readCalls, workspaceService.promoteCalls, workspaceService.refPresent)
	}
	latest, err := fixture.store.LoadJournal(fixture.key)
	if err != nil || latest.LinuxClonePromotionReceipt == nil || !latest.LinuxClonePromotionReceipt.Equal(fixture.receipt) {
		t.Fatalf("existing promotion receipt readback = %#v, error=%v", latest.LinuxClonePromotionReceipt, err)
	}
}

func TestLinuxClonePromotionEffectRejectsStaleFenceBeforeGitMutation(t *testing.T) {
	fixture := newLinuxClonePromotionRecoveryFixture(t, false)
	defer fixture.store.Close()
	app := linuxV2AppTestDaemon(t, fixture.store, fixture.key, false)
	staleFence := fixture.fence
	staleFence.LeaseToken = "stale-lease-token"
	effectCalls := 0
	err := app.withLinuxV2EffectReadyTarget(context.Background(), fixture.key, staleFence, func() error {
		effectCalls++
		return nil
	})
	if err == nil || effectCalls != 0 {
		t.Fatalf("stale-fence promotion effect = %v, calls=%d; want rejection before Git mutation", err, effectCalls)
	}
}

type linuxClonePromotionRecoveryFixture struct {
	store     *state.Store
	key       state.RunKey
	grant     authority.LinuxCloneGrant
	fence     protocol.Fence
	candidate authority.LinuxClonePromotionIntent
	receipt   authority.LinuxClonePromotionReceipt
}

func newLinuxClonePromotionRecoveryFixture(t *testing.T, expireLease bool) linuxClonePromotionRecoveryFixture {
	t.Helper()
	directory := t.TempDir()
	store, key, _, grant, fence := linuxCloneCallbackFixtureAt(t, directory)
	handoff, _, _, stopReceipt := bindAppLinuxCloneCheckoutAndStop(t, store, key, grant, fence)
	candidate := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: grant.Intent.ResourceID,
		Commit: strings.Repeat("1", len(grant.Intent.BaseCommit)), TreeDigest: "sha256:" + strings.Repeat("d", 64),
	}
	app := linuxV2AppTestDaemon(t, store, key, false)
	if err := app.linuxCloneJournalCallbacks(key, fence).ReservePromotion(grant.Intent, candidate); err != nil {
		store.Close()
		t.Fatalf("reserve exact promotion intent before recovery fault: %v", err)
	}
	promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	receipt := authority.LinuxClonePromotionReceipt{
		Version: authority.LinuxCloneVersion, Commit: candidate.Commit,
		TreeDigest: candidate.TreeDigest, Ref: promotionRef,
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if expireLease {
		journal.LeaseExpiresAt = time.Now().Add(-time.Minute)
	}
	releaseProof := linuxCloneAppReleaseProof(t, handoff, stopReceipt)
	journal.ContainmentCgroup = nil
	journal.ContainmentAuthority = nil
	journal.ContainmentHandoff = nil
	journal.ContainmentScans = nil
	journal.ContainmentUnproven = false
	journal.PID, journal.ProcessIdentity, journal.StartedAt = 0, "", time.Time{}
	releaseCopy := releaseProof.Clone()
	journal.ContainmentCgroupReleaseProof = &releaseCopy
	if err := store.Close(); err != nil {
		t.Fatalf("close promotion recovery fixture Store: %v", err)
	}
	writeAppRunJournalFixture(t, directory, journal)
	store, err = state.New(directory)
	if err != nil {
		t.Fatalf("reopen promotion recovery fixture Store: %v", err)
	}
	return linuxClonePromotionRecoveryFixture{
		store: store, key: key, grant: grant, fence: fence, candidate: candidate, receipt: receipt,
	}
}

type linuxClonePromotionRecoveryWorkspace struct {
	*linuxCloneWorkspaceCallbackFixture
	readReceipt  authority.LinuxClonePromotionReceipt
	readErr      error
	readCalls    int
	promoteCalls int
	refPresent   bool
}

func (service *linuxClonePromotionRecoveryWorkspace) ReadExistingLinuxClonePromotion(
	context.Context,
	authority.LinuxCloneIntent,
	authority.LinuxClonePromotionIntent,
	state.RunJournal,
	platform.LinuxCgroupV2Release,
) (authority.LinuxClonePromotionReceipt, error) {
	service.readCalls++
	if service.readErr != nil {
		return authority.LinuxClonePromotionReceipt{}, service.readErr
	}
	service.refPresent = true
	return service.readReceipt, nil
}

func (service *linuxClonePromotionRecoveryWorkspace) PromoteLinuxClone(
	context.Context,
	authority.LinuxCloneGrant,
	authority.LinuxCloneCheckoutReceipt,
	authority.LinuxClonePromotionIntent,
	platform.LinuxCgroupV2Release,
) (authority.LinuxClonePromotionReceipt, error) {
	service.promoteCalls++
	service.refPresent = true
	return service.readReceipt, nil
}

func TestLinuxCloneCleanupRequiresCompletedPromotionBeforeWorkspaceEffect(t *testing.T) {
	store, key, _, grant, fence := linuxCloneCallbackFixture(t)
	defer store.Close()
	_, _, checkout, _ := bindAppLinuxCloneCheckoutAndStop(t, store, key, grant, fence)
	candidate := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: grant.Intent.ResourceID,
		Commit: strings.Repeat("1", len(grant.Intent.BaseCommit)), TreeDigest: "sha256:" + strings.Repeat("d", 64),
	}
	if err := linuxV2AppTestDaemon(t, store, key, false).linuxCloneJournalCallbacks(key, fence).ReservePromotion(grant.Intent, candidate); err != nil {
		t.Fatalf("persist promotion intent without issuing release: %v", err)
	}
	workspaceService := &linuxCloneCleanupCallsWorkspace{
		linuxCloneWorkspaceCallbackFixture: &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}},
	}
	app := &daemon{config: testConfig(t), store: store, workspace: workspaceService}
	journal, err := store.LoadJournal(key)
	if err != nil || journal.Key() != key || journal.Fence() != fence ||
		journal.LinuxCloneCheckoutReceipt == nil || !journal.LinuxCloneCheckoutReceipt.Equal(checkout) ||
		journal.LinuxClonePromotionIntent == nil || journal.LinuxClonePromotionReceipt != nil ||
		journal.ContainmentCgroupReleaseProof != nil {
		t.Fatalf("incomplete cleanup fixture = %#v, error=%v", journal, err)
	}
	writes := 0
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	cleanupErr := app.cleanupRecoveredWorkspace(context.Background(), journal, true)
	restoreWriter()
	after, loadErr := store.LoadJournal(key)
	if cleanupErr == nil || workspaceService.cleanupCalls != 0 || writes != 0 || loadErr != nil ||
		after.LinuxCloneCleanupIntent != nil || after.LinuxCloneCleanupPending || after.LinuxCloneCleanupProof != nil {
		t.Fatalf("cleanup without durable promotion/release = %v, Workspace calls=%d, Store writes=%d, journal=%#v, load error=%v",
			cleanupErr, workspaceService.cleanupCalls, writes, after, loadErr)
	}
}

type linuxCloneAppCleanupFixture struct {
	store     *state.Store
	key       state.RunKey
	fence     protocol.Fence
	grant     authority.LinuxCloneGrant
	checkout  authority.LinuxCloneCheckoutReceipt
	cleanup   authority.LinuxCloneCleanupIntent
	rootProof authority.LinuxCloneTargetRemovalProof
	directory string
}

func newLinuxCloneAppCleanupFixture(t *testing.T) *linuxCloneAppCleanupFixture {
	t.Helper()
	return newLinuxCloneAppCleanupFixtureAt(t, t.TempDir())
}

func newLinuxCloneAppCleanupFixtureAt(t *testing.T, directory string) *linuxCloneAppCleanupFixture {
	return newLinuxCloneAppCleanupFixtureMode(t, directory, true)
}

func newLinuxCloneAppRetainedNonGoalFixtureAt(t *testing.T, directory string) *linuxCloneAppCleanupFixture {
	return newLinuxCloneAppCleanupFixtureMode(t, directory, false)
}

func newLinuxCloneAppCleanupFixtureMode(t *testing.T, directory string, goalManaged bool) *linuxCloneAppCleanupFixture {
	t.Helper()
	store, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	storeOpen := true
	t.Cleanup(func() {
		if storeOpen {
			_ = store.Close()
		}
	})
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse cleanup fixture admission: present=%t err=%v", present, err)
	}
	key := state.RunKey{RunID: "00000000-0000-4000-8000-000000000077", Generation: 1}
	work := protocol.Work{Goal: "cleanup candidate", AgentProfile: "local", Workspace: "local", Input: validAdmissionInput()}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1,
		ClaimID: "claim-1", LocalState: "claiming", Work: work, WorkspaceBindingKey: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease-1",
		LeaseExpiresAt: time.Now().Add(time.Minute), Work: work,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(key, "running"); err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: "runtime-1", RuntimeEpoch: 1, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease-1",
	}
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: key.RunID, Generation: key.Generation,
		BindingKey: "local", ResourceID: admission.Subject.ResourceID,
		BaseCommit: admission.Subject.Commit, BaseTreeDigest: admission.Subject.TreeDigest,
		Nonce:    strings.Repeat("a", authority.LinuxCloneNonceBytes*2),
		SeedPath: "/var/lib/symmetry/seeds/cleanup-fixture.bundle",
		Target:   "/home/native-b/workspaces/binding-local/run-00000000-0000-4000-8000-000000000077/generation-1",
	}
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("c", 64), Size: 128, Device: 1, Inode: 2,
	}
	grant := authority.LinuxCloneGrant{Intent: intent, Seed: seed}
	if err := grant.Validate(); err != nil {
		t.Fatalf("validate exact clone cleanup fixture grant: %v", err)
	}
	cloneCallbacks := linuxV2AppTestDaemon(t, store, key, false).linuxCloneJournalCallbacks(key, fence)
	if err := cloneCallbacks.Reserve(intent); err != nil {
		t.Fatalf("reserve cleanup fixture intent: %v", err)
	}
	if err := cloneCallbacks.BindSeed(intent, seed); err != nil {
		t.Fatalf("bind cleanup fixture seed: %v", err)
	}
	handoff, _, checkout, stopReceipt := bindAppLinuxCloneCheckoutAndStop(t, store, key, grant, fence)
	if goalManaged {
		promotionIntent := authority.LinuxClonePromotionIntent{
			Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
			Commit: strings.Repeat("1", len(intent.BaseCommit)), TreeDigest: "sha256:" + strings.Repeat("d", 64),
		}
		promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
		if err != nil {
			t.Fatal(err)
		}
		promotionReceipt := authority.LinuxClonePromotionReceipt{
			Version: authority.LinuxCloneVersion, Commit: promotionIntent.Commit,
			TreeDigest: promotionIntent.TreeDigest, Ref: promotionRef,
		}
		cloneCallbacks = linuxV2AppTestDaemon(t, store, key, false).linuxCloneJournalCallbacks(key, fence)
		if err := cloneCallbacks.ReservePromotion(intent, promotionIntent); err != nil {
			t.Fatalf("reserve cleanup fixture promotion: %v", err)
		}
		if err := cloneCallbacks.RecordPromotion(intent, promotionIntent, promotionReceipt); err != nil {
			t.Fatalf("record cleanup fixture promotion: %v", err)
		}
		artifactSubject := protocol.Subject{
			ResourceID: promotionIntent.ResourceID, Commit: promotionIntent.Commit, TreeDigest: promotionIntent.TreeDigest,
		}
		evidence := linuxCloneCleanupArtifactEvidence(t, key.RunID, artifactSubject)
		queued, err := store.QueueGoalEvidence(key, evidence)
		if err != nil || len(queued.PendingGoalDeliveries) != 1 {
			t.Fatalf("queue cleanup artifact evidence: pending=%d err=%v", len(queued.PendingGoalDeliveries), err)
		}
		delivery := queued.PendingGoalDeliveries[0]
		if _, err := store.MarkGoalDeliveryDelivered(key, delivery.Kind, delivery.DeliveryID, delivery.PayloadDigest); err != nil {
			t.Fatalf("deliver cleanup artifact evidence: %v", err)
		}
	} else {
		if _, err := store.RetainWorkspace(key); err != nil {
			t.Fatalf("retain physically stopped non-Goal checkout: %v", err)
		}
	}
	terminalAt := time.Date(2026, 9, 29, 1, 2, 3, 0, time.UTC)
	resultKind := protocol.TaskResultCandidateCompletion
	transitionID := "cleanup-candidate-completed"
	if !goalManaged {
		resultKind = protocol.TaskResultProgress
		transitionID = "retained-nongoal-completed"
	}
	transition := protocol.StateTransitionRequest{
		TransitionID: transitionID, State: "completed",
		Payload: json.RawMessage(fmt.Sprintf(`{"task_result":{"kind":%q}}`, resultKind)),
	}
	if _, err := store.QueueTerminalTransitionAt(key, transition, terminalAt); err != nil {
		t.Fatalf("queue completed cleanup transition: %v", err)
	}
	if _, err := store.MarkTransitionsDelivered(key, []string{transition.TransitionID}); err != nil {
		t.Fatalf("deliver completed cleanup transition: %v", err)
	}
	if _, err := store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, terminalAt.Add(time.Second)); err != nil {
		t.Fatalf("resolve completed cleanup transition: %v", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	releaseProof := linuxCloneAppReleaseProof(t, handoff, stopReceipt)
	journal.ContainmentCgroup = nil
	journal.ContainmentAuthority = nil
	journal.ContainmentHandoff = nil
	journal.ContainmentScans = nil
	journal.ContainmentUnproven = false
	journal.PID, journal.ProcessIdentity, journal.StartedAt = 0, "", time.Time{}
	releaseCopy := releaseProof.Clone()
	journal.ContainmentCgroupReleaseProof = &releaseCopy
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	storeOpen = false
	writeAppRunJournalFixture(t, directory, journal)
	store, err = state.New(directory)
	if err != nil {
		t.Fatalf("reopen cleanup fixture Store: %v", err)
	}
	storeOpen = true
	latest, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("read reloaded cleanup fixture Store: %v", err)
	}
	if goalManaged {
		latest, err = store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, terminalAt.Add(time.Second))
		if err != nil {
			t.Fatalf("advance released cleanup fixture to cleanup_pending: %v", err)
		}
	}
	if !goalManaged {
		if latest.GoalDeliveryEnabled || latest.TerminalTaskResultKind != protocol.TaskResultProgress ||
			!latest.RetainWorkspace || latest.TerminalState != "completed" || latest.LinuxCloneIntent == nil ||
			latest.LinuxCloneCheckoutReceipt == nil || latest.ContainmentCgroupReleaseProof == nil ||
			latest.WorkspacePath != intent.Target || !latest.WorkspaceRecoveryRequired ||
			latest.ContainmentCgroup != nil || latest.ContainmentAuthority != nil || latest.ContainmentHandoff != nil ||
			latest.LinuxCloneCleanupPending || latest.LinuxCloneCleanupIntent != nil ||
			latest.LinuxCloneTargetRemovalProof != nil || latest.LinuxCloneCleanupProof != nil {
			t.Fatalf("non-Goal retained checkout fixture lacks exact stop/release retention state: %#v", latest)
		}
		return &linuxCloneAppCleanupFixture{store: store, key: key, fence: fence, grant: grant, checkout: checkout, directory: directory}
	}
	if !latest.LinuxCloneCleanupBeginEligible(key, fence) {
		t.Fatalf("cleanup fixture does not satisfy the durable Begin gate: eligible=%t", latest.LinuxCloneCleanupBeginEligible(key, fence))
	}
	cleanupIntent, err := state.LinuxCloneCleanupIntentForJournal(latest)
	if err != nil {
		t.Fatalf("derive exact cleanup intent fixture: %v", err)
	}
	rootProof := linuxCloneAppTargetRemovalProof(t, cleanupIntent)
	return &linuxCloneAppCleanupFixture{
		store: store, key: key, fence: fence, grant: grant, checkout: checkout,
		cleanup: cleanupIntent, rootProof: rootProof, directory: directory,
	}
}

func linuxCloneCleanupArtifactEvidence(t *testing.T, runID string, subject protocol.Subject) protocol.Evidence {
	t.Helper()
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	resourceID, commit, path := subject.ResourceID, subject.Commit, "result.txt"
	profile, predicateID := "artifact-publisher", "artifact-publish"
	contentDigest := "sha256:" + strings.Repeat("b", 64)
	subjectCopy := subject
	evidence := protocol.Evidence{
		SchemaVersion: protocol.EvidenceSchemaVersion,
		EvidenceID:    "00000000-0000-4000-8000-000000000011", RunID: runID,
		EvidenceKey: "candidate-artifact", Kind: protocol.EvidenceArtifact,
		Subject: subject, SubjectHash: subjectHash,
		SourceRef: protocol.SourceRef{
			Kind: protocol.EvidenceArtifact, Ref: "artifact-publish", SubjectHash: &subjectHash,
			ResourceID: &resourceID, Commit: &commit, Path: &path,
		},
		SourceRevision: "artifact-publisher", ValidatorProfile: &profile,
		Verdict: protocol.EvidencePassed,
		Payload: protocol.Receipt{
			PredicateID: &predicateID, Subject: &subjectCopy, SubjectHash: &subjectHash,
			ResourceID: &resourceID, Commit: &commit, Path: &path, ContentDigest: &contentDigest,
		},
		ObservedAt: "2026-09-10T01:01:00Z",
	}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("validate cleanup artifact evidence: %v", err)
	}
	return evidence
}

func linuxCloneAppReleaseProof(t *testing.T, handoff authority.SupervisorHandoff, receipt authority.StopReceipt) authority.SupervisorHandoffReleaseProof {
	t.Helper()
	handoff = handoff.Clone()
	receiptCopy := cloneLinuxV2StopReceipt(receipt)
	handoff.StopReceipt = &receiptCopy
	proof := authority.SupervisorHandoffReleaseProof{
		Version: authority.SupervisorHandoffVersionV2, Status: "released", LaunchToken: handoff.LaunchToken,
		OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext,
		TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity,
		OwnerUID: handoff.OwnerUID, LauncherUID: handoff.LauncherUID,
		PipeToken: handoff.PipeToken, JobID: handoff.JobID,
		SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
		TargetBirthKind: handoff.TargetBirthKind, CgroupIdentity: handoff.CgroupIdentity,
		AnchorPID: handoff.AnchorPID, AnchorIdentity: handoff.AnchorIdentity,
		LauncherPID: handoff.LauncherPID, LauncherIdentity: handoff.LauncherIdentity,
		LauncherExit: handoff.LauncherExit, Mirror: handoff.Mirror,
		LeafDisposition: authority.LinuxCgroupLeafRemoved, StopReceipt: handoff.StopReceipt,
		CreatorSessionID: handoff.CreatorSessionID,
	}
	if err := proof.Validate(); err != nil || !proof.ValidFor(handoff) {
		t.Fatalf("validate pre-existing v2 release tombstone fixture: %v", err)
	}
	return proof.Clone()
}

func linuxCloneAppTargetRemovalProof(t *testing.T, cleanup authority.LinuxCloneCleanupIntent) authority.LinuxCloneTargetRemovalProof {
	t.Helper()
	witness := authority.LinuxCgroupProcessIdentity{
		PID: cleanup.NormalReleaseProof.AnchorPID, Identity: cleanup.NormalReleaseProof.AnchorIdentity,
	}
	rootDeleter := authority.LinuxCgroupProcessIdentity{PID: 901, Identity: "root-clone-cleaner:901:start"}
	proof := authority.LinuxCloneTargetRemovalProof{
		Version: authority.LinuxCloneVersion, Intent: cleanup.Clone(), RootDevice: 9, RootInode: 11,
		TargetDevice: cleanup.CheckoutReceipt.TargetDevice, TargetInode: cleanup.CheckoutReceipt.TargetInode,
		Disposition: authority.LinuxCloneTargetDispositionRemoved, PathRewalkVerified: true,
		BOwnedMountConfined: true, TargetNlinkZero: true, RootDeleterUID: 0, RootDeleter: rootDeleter,
		RootDeleterExit: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: rootDeleter, Witness: witness, Waited: true, Reaped: true,
		},
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("validate deterministic App root-removal fixture: %v", err)
	}
	return proof
}

func writeAppRunJournalFixture(t *testing.T, directory string, journal state.RunJournal) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(directory, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	path := ""
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "journal-") && strings.HasSuffix(entry.Name(), ".json") {
			if path != "" {
				t.Fatal("App release fixture StateDir has multiple run journals")
			}
			path = filepath.Join(directory, "runs", entry.Name())
		}
	}
	if path == "" {
		t.Fatal("App release fixture StateDir has no run journal")
	}
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAppStateAtomic(path, encoded); err != nil {
		t.Fatalf("seed exact persisted release tombstone fixture: %v", err)
	}
}

type linuxCloneCleanupPhaseWorkspace struct {
	*linuxCloneWorkspaceCallbackFixture
	fixture                *linuxCloneAppCleanupFixture
	targetPresent          bool
	aMaterialPresent       bool
	rootCalls              int
	removeCalls            int
	cleanupCalls           int
	aEffects               int
	failRootAfterDelete    bool
	failCleanupBeforeAOnce bool
	failCleanupAfterAOnce  bool
}

func newLinuxCloneCleanupPhaseWorkspace(fixture *linuxCloneAppCleanupFixture) *linuxCloneCleanupPhaseWorkspace {
	return &linuxCloneCleanupPhaseWorkspace{
		linuxCloneWorkspaceCallbackFixture: &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}},
		fixture:                            fixture, targetPresent: true, aMaterialPresent: true,
	}
}

func installLinuxCloneCleanupRootDeleter(t *testing.T, service *linuxCloneCleanupPhaseWorkspace) {
	t.Helper()
	previous := linuxCloneTargetDeleterForRun
	linuxCloneTargetDeleterForRun = func(
		_ *daemon,
		grant authority.LinuxCloneGrant,
		cleanup authority.LinuxCloneCleanupIntent,
		prior *authority.LinuxCloneTargetRemovalProof,
	) (workspace.LinuxCloneTargetDeleter, error) {
		if grant.Validate() != nil || !grant.Equal(service.fixture.grant) ||
			!cleanup.Equal(service.fixture.cleanup) || prior != nil {
			return nil, errors.New("App cleanup test root grant differs from exact fixture")
		}
		return func(
			ctx context.Context,
			rootFD, targetFD int,
			callbackGrant authority.LinuxCloneGrant,
			callbackCleanup authority.LinuxCloneCleanupIntent,
			callbackPrior *authority.LinuxCloneTargetRemovalProof,
		) (authority.LinuxCloneTargetRemovalProof, error) {
			service.rootCalls++
			if ctx == nil || rootFD < 0 || targetFD < 0 || callbackPrior != nil ||
				!callbackGrant.Equal(service.fixture.grant) || !callbackCleanup.Equal(service.fixture.cleanup) {
				return authority.LinuxCloneTargetRemovalProof{}, errors.New("App cleanup test root request changed its exact pinned authority")
			}
			if service.failRootAfterDelete {
				service.failRootAfterDelete = false
				service.targetPresent = false
				return authority.LinuxCloneTargetRemovalProof{}, linuxgate.ErrCloneCleanupOutcomeUnknown
			}
			return service.fixture.rootProof.Clone(), nil
		}, nil
	}
	t.Cleanup(func() { linuxCloneTargetDeleterForRun = previous })
}

func (service *linuxCloneCleanupPhaseWorkspace) RemoveLinuxCloneTarget(
	ctx context.Context,
	grant authority.LinuxCloneGrant,
	checkout authority.LinuxCloneCheckoutReceipt,
	journal state.RunJournal,
	deleter workspace.LinuxCloneTargetDeleter,
) (authority.LinuxCloneTargetRemovalProof, error) {
	service.removeCalls++
	if ctx == nil || grant.Validate() != nil || !grant.Equal(service.fixture.grant) ||
		!checkout.Equal(service.fixture.checkout) || journal.Key() != service.fixture.key ||
		journal.LinuxCloneCleanupIntent == nil || !journal.LinuxCloneCleanupIntent.Equal(service.fixture.cleanup) ||
		!journal.LinuxCloneCleanupPending {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Workspace removal differs from exact pending App cleanup state")
	}
	if stored := journal.LinuxCloneTargetRemovalProof; stored != nil {
		if service.targetPresent || stored.Validate() != nil || !stored.Equal(service.fixture.rootProof) {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("stored ROOT proof does not match the absent original B target")
		}
		return stored.Clone(), nil
	}
	if !service.targetPresent {
		return authority.LinuxCloneTargetRemovalProof{}, linuxgate.ErrCloneCleanupOutcomeUnknown
	}
	if deleter == nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("fresh B target removal has no anchored root deleter")
	}
	proof, err := deleter(ctx, 10, 11, grant.Clone(), journal.LinuxCloneCleanupIntent.Clone(), nil)
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	if proof.Validate() != nil || !proof.Equal(service.fixture.rootProof) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("root callback returned a non-exact test proof")
	}
	service.targetPresent = false
	return proof.Clone(), nil
}

func (service *linuxCloneCleanupPhaseWorkspace) CleanupLinuxClone(
	ctx context.Context,
	grant authority.LinuxCloneGrant,
	checkout authority.LinuxCloneCheckoutReceipt,
	journal state.RunJournal,
) (authority.LinuxCloneCleanupProof, error) {
	service.cleanupCalls++
	if ctx == nil || grant.Validate() != nil || !grant.Equal(service.fixture.grant) ||
		!checkout.Equal(service.fixture.checkout) || journal.Key() != service.fixture.key ||
		journal.LinuxCloneCleanupIntent == nil || !journal.LinuxCloneCleanupIntent.Equal(service.fixture.cleanup) ||
		journal.LinuxCloneTargetRemovalProof == nil || !journal.LinuxCloneTargetRemovalProof.Equal(service.fixture.rootProof) ||
		service.targetPresent {
		return authority.LinuxCloneCleanupProof{}, errors.New("A cleanup started without the exact persisted B removal proof")
	}
	latest, err := service.fixture.store.LoadJournal(service.fixture.key)
	if err != nil || latest.LinuxCloneTargetRemovalProof == nil ||
		!latest.LinuxCloneTargetRemovalProof.Equal(service.fixture.rootProof) {
		return authority.LinuxCloneCleanupProof{}, errors.Join(errors.New("Workspace observed no durable target proof before A effects"), err)
	}
	if journal.LinuxCloneCleanupProof != nil {
		if !journal.LinuxCloneCleanupProof.Equal(service.fixture.cleanupProof()) || service.aMaterialPresent {
			return authority.LinuxCloneCleanupProof{}, errors.New("completed cleanup replay differs from the exact read-only A state")
		}
		return journal.LinuxCloneCleanupProof.Clone(), nil
	}
	if service.failCleanupBeforeAOnce {
		service.failCleanupBeforeAOnce = false
		return authority.LinuxCloneCleanupProof{}, errors.New("injected crash after durable target proof and before A cleanup")
	}
	if service.aMaterialPresent {
		service.aMaterialPresent = false
		service.aEffects++
	}
	if service.failCleanupAfterAOnce {
		service.failCleanupAfterAOnce = false
		return authority.LinuxCloneCleanupProof{}, errors.New("injected lost response after A cleanup and before Complete")
	}
	return service.fixture.cleanupProof(), nil
}

func (fixture *linuxCloneAppCleanupFixture) cleanupProof() authority.LinuxCloneCleanupProof {
	return authority.LinuxCloneCleanupProof{
		Version: authority.LinuxCloneVersion, Intent: fixture.cleanup.Clone(),
		TargetRemovalProof: fixture.rootProof.Clone(),
		SeedAbsent:         true, BaseRefAbsent: true, ScratchAbsent: true, PromotionRefPresent: true,
	}
}

func TestLinuxCloneCleanupRootOutcomeUnknownNeverAuthorizesAEffects(t *testing.T) {
	fixture := newLinuxCloneAppCleanupFixture(t)
	workspace := newLinuxCloneCleanupPhaseWorkspace(fixture)
	workspace.failRootAfterDelete = true
	installLinuxCloneCleanupRootDeleter(t, workspace)
	app := &daemon{config: testConfig(t), store: fixture.store, workspace: workspace}
	journal, err := fixture.store.LoadJournal(fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	firstErr := app.cleanupRecoveredWorkspace(context.Background(), journal, true)
	if !errors.Is(firstErr, linuxgate.ErrCloneCleanupOutcomeUnknown) {
		t.Fatalf("post-request root loss = %v; want the exact unknown cleanup outcome", firstErr)
	}
	interrupted, err := fixture.store.LoadJournal(fixture.key)
	if err != nil || !interrupted.LinuxCloneCleanupPending || interrupted.LinuxCloneTargetRemovalProof != nil ||
		interrupted.LinuxCloneCleanupProof != nil || workspace.targetPresent || workspace.rootCalls != 1 || workspace.aEffects != 0 {
		t.Fatalf("unknown root result changed App cleanup authority: err=%v journal=%#v target=%t root=%d A=%d",
			err, interrupted, workspace.targetPresent, workspace.rootCalls, workspace.aEffects)
	}
	replayErr := app.cleanupRecoveredWorkspace(context.Background(), interrupted, true)
	if !errors.Is(replayErr, linuxgate.ErrCloneCleanupOutcomeUnknown) || workspace.rootCalls != 1 || workspace.aEffects != 0 {
		t.Fatalf("absent target without persisted proof was accepted on replay: err=%v root=%d A=%d",
			replayErr, workspace.rootCalls, workspace.aEffects)
	}
	after, err := fixture.store.LoadJournal(fixture.key)
	if err != nil || after.LinuxCloneTargetRemovalProof != nil || !after.LinuxCloneCleanupPending ||
		after.LinuxCloneCleanupProof != nil || workspace.aMaterialPresent == false {
		t.Fatalf("unknown root replay did not retain pending A material: journal=%#v, err=%v, A-present=%t", after, err, workspace.aMaterialPresent)
	}
}

func TestLinuxCloneCleanupTargetProofCASFailurePreservesAAndCleanupPending(t *testing.T) {
	fixture := newLinuxCloneAppCleanupFixture(t)
	workspace := newLinuxCloneCleanupPhaseWorkspace(fixture)
	installLinuxCloneCleanupRootDeleter(t, workspace)
	app := &daemon{config: testConfig(t), store: fixture.store, workspace: workspace}
	proofWrites := 0
	restoreWriter := fixture.store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		var current state.RunJournal
		if json.Unmarshal(data, &current) == nil && current.Key() == fixture.key &&
			current.LinuxCloneTargetRemovalProof != nil && current.LinuxCloneCleanupProof == nil {
			proofWrites++
			return errors.New("injected persistent target-proof CAS failure")
		}
		return writeAppStateAtomic(path, data)
	})
	journal, err := fixture.store.LoadJournal(fixture.key)
	if err != nil {
		restoreWriter()
		t.Fatal(err)
	}
	cleanupErr := app.cleanupRecoveredWorkspace(context.Background(), journal, true)
	restoreWriter()
	interrupted, readErr := fixture.store.LoadJournal(fixture.key)
	if cleanupErr == nil || proofWrites != 2 || readErr != nil || !interrupted.LinuxCloneCleanupPending ||
		interrupted.LinuxCloneTargetRemovalProof != nil || interrupted.LinuxCloneCleanupProof != nil ||
		workspace.rootCalls != 1 || workspace.targetPresent || workspace.aEffects != 0 ||
		!workspace.aMaterialPresent || workspace.cleanupCalls != 0 {
		t.Fatalf("failed target-proof CAS proceeded past ROOT phase: err=%v read=%v writes=%d root=%d target=%t A=%d/%t cleanup=%d journal=%#v",
			cleanupErr, readErr, proofWrites, workspace.rootCalls, workspace.targetPresent,
			workspace.aEffects, workspace.aMaterialPresent, workspace.cleanupCalls, interrupted)
	}
	replayErr := app.cleanupRecoveredWorkspace(context.Background(), interrupted, true)
	if !errors.Is(replayErr, linuxgate.ErrCloneCleanupOutcomeUnknown) || workspace.rootCalls != 1 ||
		workspace.aEffects != 0 || !workspace.aMaterialPresent {
		t.Fatalf("target ENOENT without durable proof authorized A cleanup: err=%v root=%d A=%d present=%t",
			replayErr, workspace.rootCalls, workspace.aEffects, workspace.aMaterialPresent)
	}
}

func TestLinuxCloneCleanupReplaysPostRenameTargetProofAckBeforeAEffects(t *testing.T) {
	fixture := newLinuxCloneAppCleanupFixture(t)
	workspace := newLinuxCloneCleanupPhaseWorkspace(fixture)
	installLinuxCloneCleanupRootDeleter(t, workspace)
	app := &daemon{config: testConfig(t), store: fixture.store, workspace: workspace}
	proofWrites := 0
	restoreWriter := fixture.store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		var current state.RunJournal
		if json.Unmarshal(data, &current) == nil && current.Key() == fixture.key &&
			current.LinuxCloneTargetRemovalProof != nil && current.LinuxCloneCleanupProof == nil {
			proofWrites++
			if err := writeAppStateAtomic(path, data); err != nil {
				return err
			}
			if proofWrites == 1 {
				return errors.New("injected post-rename target-proof ACK loss")
			}
			return nil
		}
		return writeAppStateAtomic(path, data)
	})
	journal, err := fixture.store.LoadJournal(fixture.key)
	if err != nil {
		restoreWriter()
		t.Fatal(err)
	}
	cleanupErr := app.cleanupRecoveredWorkspace(context.Background(), journal, true)
	restoreWriter()
	completed, readErr := fixture.store.LoadJournal(fixture.key)
	if cleanupErr != nil || readErr != nil || proofWrites != 2 || workspace.rootCalls != 1 || workspace.aEffects != 1 ||
		completed.LinuxCloneTargetRemovalProof == nil || !completed.LinuxCloneTargetRemovalProof.Equal(fixture.rootProof) ||
		completed.LinuxCloneCleanupProof == nil || !completed.LinuxCloneCleanupProof.Equal(fixture.cleanupProof()) ||
		completed.LinuxCloneCleanupPending {
		t.Fatalf("target-proof post-rename replay: cleanup=%v read=%v writes=%d root=%d A=%d journal=%#v",
			cleanupErr, readErr, proofWrites, workspace.rootCalls, workspace.aEffects, completed)
	}
}

func TestLinuxCloneCleanupRecoversAcrossAAndCompleteFaults(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		failBeforeA       bool
		failAfterA        bool
		failCompleteWrite bool
		wantFirstError    bool
	}{
		{name: "target proof stored before A cleanup", failBeforeA: true, wantFirstError: true},
		{name: "A cleanup applied before Complete", failAfterA: true, wantFirstError: true},
		{name: "Complete response lost after rename", failCompleteWrite: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newLinuxCloneAppCleanupFixture(t)
			workspace := newLinuxCloneCleanupPhaseWorkspace(fixture)
			workspace.failCleanupBeforeAOnce = scenario.failBeforeA
			workspace.failCleanupAfterAOnce = scenario.failAfterA
			installLinuxCloneCleanupRootDeleter(t, workspace)
			app := &daemon{config: testConfig(t), store: fixture.store, workspace: workspace}
			completeWrites := 0
			restoreWriter := func() {}
			if scenario.failCompleteWrite {
				restoreWriter = fixture.store.SetAtomicWriterForTesting(func(path string, data []byte) error {
					var current state.RunJournal
					if json.Unmarshal(data, &current) == nil && current.Key() == fixture.key && current.LinuxCloneCleanupProof != nil {
						completeWrites++
						if err := writeAppStateAtomic(path, data); err != nil {
							return err
						}
						if completeWrites == 1 {
							return errors.New("injected post-rename Complete ACK loss")
						}
						return nil
					}
					return writeAppStateAtomic(path, data)
				})
			}
			journal, err := fixture.store.LoadJournal(fixture.key)
			if err != nil {
				restoreWriter()
				t.Fatal(err)
			}
			firstErr := app.cleanupRecoveredWorkspace(context.Background(), journal, true)
			restoreWriter()
			if (firstErr != nil) != scenario.wantFirstError {
				t.Fatalf("first cleanup error = %v, wantErr=%t", firstErr, scenario.wantFirstError)
			}
			middle, err := fixture.store.LoadJournal(fixture.key)
			if err != nil || middle.LinuxCloneTargetRemovalProof == nil ||
				!middle.LinuxCloneTargetRemovalProof.Equal(fixture.rootProof) {
				t.Fatalf("target removal proof did not persist before A cleanup: journal=%#v err=%v", middle, err)
			}
			if scenario.wantFirstError && middle.LinuxCloneCleanupProof != nil {
				t.Fatal("failed A/Complete boundary falsely completed cleanup")
			}
			if firstErr != nil {
				if err := app.cleanupRecoveredWorkspace(context.Background(), middle, true); err != nil {
					t.Fatalf("same-key cleanup replay: %v", err)
				}
			}
			completed, err := fixture.store.LoadJournal(fixture.key)
			if err != nil || completed.LinuxCloneCleanupProof == nil ||
				!completed.LinuxCloneCleanupProof.Equal(fixture.cleanupProof()) ||
				!completed.LinuxCloneCleanupProof.TargetRemovalProof.Equal(*completed.LinuxCloneTargetRemovalProof) ||
				workspace.rootCalls != 1 || workspace.aEffects != 1 ||
				scenario.failCompleteWrite && completeWrites != 2 {
				t.Fatalf("replayed cleanup mismatch: journal=%#v err=%v root=%d A=%d CompleteWrites=%d",
					completed, err, workspace.rootCalls, workspace.aEffects, completeWrites)
			}
		})
	}
}

func TestLinuxCloneAbortAfterNormalReleaseRetainsUnresolvedJournal(t *testing.T) {
	directory := t.TempDir()
	store, key, _, grant, fence := linuxCloneCallbackFixtureAt(t, directory)
	storeOpen := true
	t.Cleanup(func() {
		if storeOpen {
			_ = store.Close()
		}
	})
	handoff, owner, _ := bindAppLinuxV2Owner(t, store, key, testAppLinuxV2Reservation())
	stopReceipt := appLinuxV2StopReceipt(t, owner)
	if err := (&daemon{store: store}).linuxV2JournalCallbacks(key, fence).RecordStopReceipt(handoff, stopReceipt); err != nil {
		t.Fatalf("record physical stop before abort regression fixture: %v", err)
	}
	releaseProof := linuxCloneAppReleaseProof(t, handoff, stopReceipt)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	journal.ContainmentCgroup = nil
	journal.ContainmentAuthority = nil
	journal.ContainmentHandoff = nil
	journal.ContainmentScans = nil
	journal.ContainmentUnproven = false
	journal.PID, journal.ProcessIdentity, journal.StartedAt = 0, "", time.Time{}
	releaseCopy := releaseProof.Clone()
	journal.ContainmentCgroupReleaseProof = &releaseCopy
	journal.LinuxCloneAbortPending = true
	journal.LinuxCloneAbortProof = nil
	journal.RetainWorkspace = true
	journal.WorkspacePath = grant.Intent.Target
	journal.WorkspaceBindingKey = grant.Intent.BindingKey
	journal.WorkspaceRecoveryRequired = true
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	storeOpen = false
	writeAppRunJournalFixture(t, directory, journal)
	store, err = state.New(directory)
	if err != nil {
		t.Fatalf("reopen abort-race State fixture: %v", err)
	}
	storeOpen = true
	persisted, err := store.LoadJournal(key)
	if err != nil || !persisted.LinuxCloneAbortPending || !persisted.RetainWorkspace ||
		persisted.ContainmentCgroupReleaseProof == nil || !persisted.ContainmentCgroupReleaseProof.Equal(releaseProof) {
		t.Fatalf("normal-release abort race fixture = %#v, error=%v", persisted, err)
	}
	workspaceService := &linuxCloneAbortProbeWorkspace{
		linuxCloneWorkspaceCallbackFixture: &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}},
	}
	app := &daemon{config: testConfig(t), store: store, workspace: workspaceService}
	writes := 0
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	cleanupErr := app.cleanupRecoveredWorkspace(context.Background(), persisted, false)
	restoreWriter()
	after, loadErr := store.LoadJournal(key)
	if cleanupErr == nil || workspaceService.abortCalls != 0 || workspaceService.removeCalls != 0 ||
		workspaceService.cleanupCalls != 0 || writes != 0 || loadErr != nil ||
		!after.LinuxCloneAbortPending || after.LinuxCloneAbortProof != nil || !after.RetainWorkspace ||
		after.ContainmentCgroupReleaseProof == nil || !after.ContainmentCgroupReleaseProof.Equal(releaseProof) ||
		after.LinuxCloneSeedReceipt == nil || !after.LinuxCloneSeedReceipt.Equal(grant.Seed) ||
		after.LinuxCloneCheckoutReceipt != nil || after.LinuxCloneTargetRemovalProof != nil ||
		after.LinuxCloneCleanupIntent != nil || after.LinuxCloneCleanupProof != nil ||
		after.WorkspacePath != grant.Intent.Target || !after.WorkspaceRecoveryRequired {
		t.Fatalf("post-RELEASE no-checkout abort changed A/B ownership: err=%v abort=%d remove=%d cleanup=%d writes=%d journal=%#v load=%v",
			cleanupErr, workspaceService.abortCalls, workspaceService.removeCalls, workspaceService.cleanupCalls, writes, after, loadErr)
	}
}

func TestLinuxCloneCleanupReplaysAcrossAAndCompleteFaults(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		failBeforeAOnce bool
		failAfterAOnce  bool
	}{
		{name: "stored proof before A cleanup", failBeforeAOnce: true},
		{name: "A cleanup effect before Complete", failAfterAOnce: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newLinuxCloneAppCleanupFixture(t)
			workspace := newLinuxCloneCleanupPhaseWorkspace(fixture)
			workspace.failCleanupBeforeAOnce = scenario.failBeforeAOnce
			workspace.failCleanupAfterAOnce = scenario.failAfterAOnce
			installLinuxCloneCleanupRootDeleter(t, workspace)
			app := &daemon{config: testConfig(t), store: fixture.store, workspace: workspace}
			journal, err := fixture.store.LoadJournal(fixture.key)
			if err != nil {
				t.Fatal(err)
			}
			firstErr := app.cleanupRecoveredWorkspace(context.Background(), journal, true)
			if firstErr == nil {
				t.Fatal("injected cleanup boundary returned success before durable Complete")
			}
			interrupted, err := fixture.store.LoadJournal(fixture.key)
			if err != nil || !interrupted.LinuxCloneCleanupPending || interrupted.LinuxCloneCleanupProof != nil ||
				interrupted.LinuxCloneTargetRemovalProof == nil || !interrupted.LinuxCloneTargetRemovalProof.Equal(fixture.rootProof) ||
				workspace.rootCalls != 1 {
				t.Fatalf("interrupted cleanup state = %#v err=%v root=%d", interrupted, err, workspace.rootCalls)
			}
			if scenario.failBeforeAOnce && (workspace.aEffects != 0 || !workspace.aMaterialPresent) {
				t.Fatalf("A effects ran before their fault point: effects=%d A-present=%t", workspace.aEffects, workspace.aMaterialPresent)
			}
			if scenario.failAfterAOnce && (workspace.aEffects != 1 || workspace.aMaterialPresent) {
				t.Fatalf("A-effect boundary was not preserved: effects=%d A-present=%t", workspace.aEffects, workspace.aMaterialPresent)
			}
			if err := app.cleanupRecoveredWorkspace(context.Background(), interrupted, true); err != nil {
				t.Fatalf("same-key cleanup replay: %v", err)
			}
			completed, err := fixture.store.LoadJournal(fixture.key)
			if err != nil || completed.LinuxCloneCleanupProof == nil ||
				!completed.LinuxCloneCleanupProof.Equal(fixture.cleanupProof()) ||
				!completed.LinuxCloneCleanupProof.TargetRemovalProof.Equal(*completed.LinuxCloneTargetRemovalProof) ||
				workspace.rootCalls != 1 || workspace.aEffects != 1 || workspace.aMaterialPresent {
				t.Fatalf("same-key cleanup did not converge: journal=%#v err=%v root=%d A=%d present=%t",
					completed, err, workspace.rootCalls, workspace.aEffects, workspace.aMaterialPresent)
			}
		})
	}
}

func TestLinuxCloneTargetRemovalRejectsStaleFenceBeforeStoreEffect(t *testing.T) {
	fixture := newLinuxCloneAppCleanupFixture(t)
	if _, err := fixture.store.BeginLinuxCloneCleanup(fixture.key, fixture.fence); err != nil {
		t.Fatalf("persist cleanup Begin fixture: %v", err)
	}
	staleFence := fixture.fence
	staleFence.LeaseToken = "stale-lease-token"
	app := &daemon{store: fixture.store}
	writeAttempts := 0
	restoreWriter := fixture.store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writeAttempts++
		return writeAppStateAtomic(path, data)
	})
	err := app.persistLinuxCloneTargetRemoval(fixture.key, staleFence, fixture.cleanup, fixture.rootProof)
	restoreWriter()
	after, loadErr := fixture.store.LoadJournal(fixture.key)
	if err == nil || loadErr != nil || writeAttempts != 0 || !after.LinuxCloneCleanupPending ||
		after.LinuxCloneTargetRemovalProof != nil || after.LinuxCloneCleanupProof != nil {
		t.Fatalf("stale target-proof fence mutated cleanup state: err=%v read=%v writes=%d journal=%#v",
			err, loadErr, writeAttempts, after)
	}
}

func TestLinuxCloneSeedTempAbortReplaysAfterWorkspaceEffectCrash(t *testing.T) {
	directory := t.TempDir()
	store, key, _, grant, fence := linuxCloneIntentOnlyFixtureAt(t, directory)
	currentStore := store
	t.Cleanup(func() {
		if currentStore != nil {
			_ = currentStore.Close()
		}
	})
	workspaceService := &linuxCloneSeedTempRecoveryWorkspace{
		linuxCloneWorkspaceCallbackFixture: &linuxCloneWorkspaceCallbackFixture{fakeWorkspace: &fakeWorkspace{}},
		store:                              store, key: key, intent: grant.Intent.Clone(),
		tempName: ".clone-seed-" + grant.Intent.Nonce, tempPresent: true,
	}
	app := &daemon{config: testConfig(t), store: store, workspace: workspaceService}
	journal, err := store.LoadJournal(key)
	if err != nil || journal.Key() != key || journal.Fence() != fence || journal.LinuxCloneIntent == nil ||
		!journal.LinuxCloneIntent.Equal(grant.Intent) || journal.LinuxCloneSeedReceipt != nil || journal.LinuxCloneCheckoutReceipt != nil {
		t.Fatalf("seed-temp recovery fixture = %#v, error=%v; want reserved intent without a seed receipt", journal, err)
	}
	if err := app.cleanupRecoveredWorkspace(context.Background(), journal, false); err == nil {
		t.Fatal("cleanup succeeded after Workspace lost its response immediately after orphan-temp removal")
	}
	interrupted, err := store.LoadJournal(key)
	if err != nil || !interrupted.LinuxCloneAbortPending || interrupted.LinuxCloneAbortProof != nil ||
		!interrupted.RetainWorkspace || workspaceService.tempPresent || workspaceService.abortCalls != 1 {
		t.Fatalf("interrupted seed-temp abort = %#v, error=%v, temp=%t calls=%d; want fenced retained replay", interrupted, err,
			workspaceService.tempPresent, workspaceService.abortCalls)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	currentStore = nil
	restarted, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	currentStore = restarted
	workspaceService.store = restarted
	app = &daemon{config: testConfig(t), store: restarted, workspace: workspaceService}
	journal, err = restarted.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.cleanupRecoveredWorkspace(context.Background(), journal, false); err != nil {
		t.Fatalf("replay exact no-checkout abort after orphan-temp removal: %v", err)
	}
	completed, err := restarted.LoadJournal(key)
	if err != nil || completed.LinuxCloneAbortProof == nil || !completed.LinuxCloneAbortProof.TargetAbsent ||
		!completed.LinuxCloneAbortProof.SeedAbsent || !completed.LinuxCloneAbortProof.BaseRefAbsent ||
		!completed.LinuxCloneAbortProof.PromotionRefAbsent || completed.LinuxCloneAbortPending || completed.RetainWorkspace ||
		completed.WorkspacePath != "" || completed.WorkspaceRecoveryRequired || workspaceService.tempPresent ||
		workspaceService.abortCalls != 2 {
		t.Fatalf("replayed seed-temp abort = %#v, error=%v, temp=%t calls=%d; want exact tombstone",
			completed, err, workspaceService.tempPresent, workspaceService.abortCalls)
	}
}

func TestLinuxV2PreGateReceiptRejectsWrongLauncherMAC(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	app := linuxV2AppTestDaemon(t, store, key, false)
	callbacks := app.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2PreGateReservation(t, store, key, callbacks)
	receipt := appLinuxV2PreGateReceipt(t, reservation, strings.Repeat("f", 64))
	if err := receipt.Validate(); err != nil {
		t.Fatalf("invalid-MAC fixture is structurally invalid: %v", err)
	}
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	err = callbacks.RecordPreGateStopReceipt(reservation, receipt)
	restore()
	if err == nil || writes != 0 {
		t.Fatalf("invalid no-gate MAC receipt = %v, writes=%d; want rejection before durability", err, writes)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil || loaded.ContainmentCgroup == nil || loaded.ContainmentCgroup.PreGateStopReceipt != nil ||
		loaded.HasProcessDetails() || loaded.ContainmentAuthority != nil || loaded.ContainmentHandoff != nil {
		t.Fatalf("invalid MAC changed pre-gate journal: %#v, error=%v", loaded, err)
	}
}

func TestLinuxV2PreGateReceiptPersistsAuthenticatedNoGateAtPIDZero(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	app := linuxV2AppTestDaemon(t, store, key, false)
	callbacks := app.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2PreGateReservation(t, store, key, callbacks)
	authenticator, err := linuxgate.SignGateNeverProducedAuthenticator(reservation.Secret, reservation.LaunchToken, *reservation.Launcher)
	if err != nil {
		t.Fatal(err)
	}
	receipt := appLinuxV2PreGateReceipt(t, reservation, authenticator)
	if err := callbacks.RecordPreGateStopReceipt(reservation, receipt); err != nil {
		t.Fatalf("persist authenticated no-gate stop receipt: %v", err)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContainmentCgroup == nil || loaded.ContainmentCgroup.PreGateStopReceipt == nil ||
		!loaded.ContainmentCgroup.PreGateStopReceipt.Equal(receipt) || loaded.PID != 0 || loaded.HasProcessDetails() ||
		!loaded.HasPendingContainment() {
		t.Fatalf("authenticated pre-gate receipt journal = %#v, want PID-zero reservation pending opaque removal", loaded)
	}
}

func TestRecoveryOnlyReplaysMissingLauncherExitBeforePreGateReceipt(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	recovery := linuxV2AppTestDaemon(t, store, key, true)
	callbacks := recovery.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2LauncherGrantedReservationWithoutExit(t, store, key, callbacks)
	grant := linuxV2GrantFromJournal(key, fence, reservation)
	evidence := appLinuxV2NoGateStopEvidence(t, grant, reservation, true)
	control := &appNoPrepareRecoveryControl{stopEvidence: evidence, fenceEvidence: *evidence.PreGate}
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		if err := writeAppStateAtomic(path, data); err != nil {
			return err
		}
		if writes == 1 {
			return errors.New("injected post-rename launcher exit acknowledgement loss")
		}
		return nil
	})
	updatedJournal, updatedReservation, measured, preGate, err := bindRecoveredV2UnhandedHelperStop(
		context.Background(), recovery, journal, reservation, grant, control, callbacks,
	)
	restore()
	if err != nil {
		t.Fatalf("bind recovered no-gate evidence after lost launcher-exit ACK: %v", err)
	}
	if control.stopCalls != 1 || !preGate || measured.PreGate == nil ||
		updatedReservation.LauncherExit == nil || updatedReservation.PreGateStopReceipt == nil ||
		!updatedReservation.LauncherExit.Equal(*measured.PreGate.LauncherExit) ||
		updatedJournal.ContainmentCgroup == nil || updatedJournal.ContainmentCgroup.LauncherExit == nil ||
		updatedJournal.ContainmentCgroup.PreGateStopReceipt == nil || updatedJournal.PID != 0 ||
		updatedJournal.ContainmentAuthority != nil || updatedJournal.ContainmentHandoff != nil {
		t.Fatalf("recovery-only no-gate readback = %#v, want exact launcher exit + authenticated receipt before opaque clear", updatedJournal)
	}
	if writes != 3 {
		t.Fatalf("launcher-exit replay and pre-gate receipt write count = %d, want lost-ACK replay plus receipt", writes)
	}
}

func TestRecoveryOnlyBindsMeasuredGateBeforeHandoffWhenCommitWasNotJournaled(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	recovery := linuxV2AppTestDaemon(t, store, key, true)
	callbacks := recovery.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2LauncherGrantedReservationWithoutExit(t, store, key, callbacks)
	grant := linuxV2GrantFromJournal(key, fence, reservation)
	evidence := appLinuxV2GateStopEvidence(t, grant, reservation)
	control := &appNoPrepareRecoveryControl{stopEvidence: evidence}
	updatedJournal, updatedReservation, _, preGate, err := bindRecoveredV2UnhandedHelperStop(
		context.Background(), recovery, journal, reservation, grant, control, callbacks,
	)
	if err != nil {
		t.Fatalf("recovery-only bind of measured pre-handoff gate: %v", err)
	}
	if control.stopCalls != 1 || preGate || updatedJournal.ContainmentHandoff == nil ||
		updatedJournal.ContainmentAuthority != nil || updatedJournal.HasProcessDetails() ||
		updatedReservation.Gate == nil || !updatedReservation.Gate.Equal(evidence.Gate) ||
		updatedReservation.LauncherExit == nil || !updatedReservation.LauncherExit.Equal(evidence.Proof.LauncherExit) ||
		updatedJournal.ContainmentHandoff.StopReceipt != nil {
		t.Fatalf("recovery-only measured gate bind = journal:%#v reservation:%#v preGate:%t Stop calls:%d",
			updatedJournal, updatedReservation, preGate, control.stopCalls)
	}
}

func TestRecoveryOnlyRejectsReleaseAttemptBeforeHandoffCommit(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: key.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	recovery := linuxV2AppTestDaemon(t, store, key, true)
	callbacks := recovery.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2LauncherGrantedReservationWithoutExit(t, store, key, callbacks)
	grant := linuxV2GrantFromJournal(key, fence, reservation)
	evidence := appLinuxV2GateStopEvidence(t, grant, reservation)
	evidence.AnchorStop.ProducerFence.ReleaseAttempted = true
	if err := evidence.ValidateFor(grant, *reservation.Helper, *reservation.Launcher, evidence.Gate); err != nil {
		t.Fatalf("pre-handoff release-attempt stop fixture is otherwise invalid: %v", err)
	}
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	_, _, err = bindRecoveredV2Gate(context.Background(), recovery, journal, reservation, evidence, callbacks)
	restore()
	latest, loadErr := store.LoadJournal(key)
	if err == nil || writes != 0 || loadErr != nil || latest.ContainmentCgroup == nil ||
		latest.ContainmentCgroup.LauncherExit != nil || latest.ContainmentCgroup.Gate != nil || latest.ContainmentHandoff != nil {
		t.Fatalf("pre-handoff RELEASE attempt was accepted or persisted: err=%v writes=%d journal=%#v load=%v",
			err, writes, latest, loadErr)
	}
}

func TestRecoveryOnlyPersistsAnchorMirrorGateBeforeV2StopReceipt(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	recovery := linuxV2AppTestDaemon(t, store, key, true)
	callbacks := recovery.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2LauncherGrantedReservationWithoutExit(t, store, key, callbacks)
	grant := linuxV2GrantFromJournal(key, fence, reservation)
	helperEvidence := appLinuxV2GateStopEvidence(t, grant, reservation)
	helperExit := testAppLinuxV2Exit(*reservation.Helper, reservation.Anchor)
	helperExitForChild := helperExit.Clone()
	gateExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helperEvidence.Gate.Process,
		Witness: reservation.Anchor, Waited: true, Reaped: true, HelperExit: &helperExitForChild,
	}
	reapedProcess := platform.LinuxOwnerAnchorReapEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Helper: *reservation.Helper,
		Process: helperEvidence.Gate.Process, ProcessRole: "gate", HelperExit: helperExit, ProcessExit: gateExit,
	}
	reaped := appLinuxV2AnchorRunReapResultWithProcesses(
		t, grant, *reservation.Helper, *helperEvidence.AnchorStop, []platform.LinuxOwnerAnchorReapEvidence{reapedProcess},
	)
	updatedJournal, updatedReservation, err := bindRecoveredV2AnchorGate(
		context.Background(), recovery, journal, reservation, grant, reaped, callbacks,
	)
	if err != nil {
		t.Fatalf("bind exact mirror-reaped gate: %v", err)
	}
	if updatedReservation.HelperExit == nil || !updatedReservation.HelperExit.Equal(helperExit) ||
		updatedReservation.LauncherExit == nil || !updatedReservation.LauncherExit.Equal(helperEvidence.Proof.LauncherExit) ||
		updatedReservation.Gate == nil || !updatedReservation.Gate.Equal(helperEvidence.Gate) ||
		updatedJournal.ContainmentHandoff == nil {
		t.Fatalf("anchor gate binding omitted exact exit/producer state: journal=%#v reservation=%#v", updatedJournal, updatedReservation)
	}
	facts, err := linuxV2AnchorReapFactsForReservation(grant, updatedReservation, reaped)
	if err != nil {
		t.Fatalf("validate exact mirror run-reap facts: %v", err)
	}
	proof, err := linuxV2MirrorStopProofFromAnchorReap(updatedReservation, reaped, facts)
	if err != nil {
		t.Fatalf("assemble exact mirror stop proof: %v", err)
	}
	handoff, _, _, err := linuxV2JournalOwner(updatedJournal)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	stopReceipt := linuxV2StopReceipt(supervisor, proof)
	if !stopReceipt.ValidForHandoff(handoff) {
		t.Fatal("mirror stop receipt does not match the exact pending handoff")
	}
	if err := callbacks.RecordStopReceipt(handoff, stopReceipt); err != nil {
		t.Fatalf("persist mirror stop receipt after helper-exit CAS: %v", err)
	}
	latest, err := store.LoadJournal(key)
	if err != nil || latest.ContainmentCgroup == nil || latest.ContainmentCgroup.HelperExit == nil ||
		!latest.ContainmentCgroup.HelperExit.Equal(helperExit) || latest.ContainmentHandoff == nil ||
		latest.ContainmentHandoff.StopReceipt == nil || !latest.ContainmentHandoff.StopReceipt.Equal(stopReceipt) ||
		latest.ContainmentHandoff.StopReceipt.LinuxProof == nil ||
		latest.ContainmentHandoff.StopReceipt.LinuxProof.Kind != authority.LinuxCgroupStopProofMirror {
		t.Fatalf("mirror receipt/helper-exit readback = journal:%#v error:%v", latest, err)
	}
}

func TestRecoveryOnlyRejectsNativeReapBeforeCommittedAuthority(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: key.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	recovery := linuxV2AppTestDaemon(t, store, key, true)
	callbacks := recovery.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2LauncherGrantedReservationWithoutExit(t, store, key, callbacks)
	grant := linuxV2GrantFromJournal(key, fence, reservation)
	helperEvidence := appLinuxV2GateStopEvidence(t, grant, reservation)
	stop := helperEvidence.AnchorStop.Clone()
	stop.ProducerFence = nil // helper died before the cached producer fence
	stop.GateReleaseAuthorized = true
	helperExit := testAppLinuxV2Exit(*reservation.Helper, reservation.Anchor)
	processes := []platform.LinuxOwnerAnchorReapEvidence{
		appLinuxV2AnchorReapEvidence(grant, *reservation.Helper, *reservation.Launcher, "launcher", helperExit),
		appLinuxV2AnchorReapEvidence(grant, *reservation.Helper, helperEvidence.Gate.Process, "native", helperExit),
	}
	reaped := appLinuxV2AnchorRunReapResultWithProcesses(t, grant, *reservation.Helper, stop, processes)
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	_, _, err = bindRecoveredV2AnchorGate(context.Background(), recovery, journal, reservation, grant, reaped, callbacks)
	restore()
	latest, loadErr := store.LoadJournal(key)
	if err == nil || writes != 0 || loadErr != nil || latest.ContainmentCgroup == nil ||
		latest.ContainmentCgroup.HelperExit != nil || latest.ContainmentCgroup.LauncherExit != nil ||
		latest.ContainmentCgroup.Gate != nil || latest.ContainmentHandoff != nil || latest.ContainmentAuthority != nil {
		t.Fatalf("anchor native reap bypassed committed-handoff gate: err=%v writes=%d journal=%#v load=%v",
			err, writes, latest, loadErr)
	}
}

func TestRecoveryOnlyPersistsNativeMirrorReceiptAfterCommittedAuthority(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: key.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	recovery := linuxV2AppTestDaemon(t, store, key, true)
	callbacks := recovery.linuxV2JournalCallbacks(key, fence)
	_, _, reservation := bindAppLinuxV2Owner(t, store, key, testAppLinuxV2Reservation())
	grant := linuxV2GrantFromJournal(key, fence, reservation)
	helperEvidence := appLinuxV2GateStopEvidence(t, grant, reservation)
	stop := helperEvidence.AnchorStop.Clone()
	stop.ProducerFence = nil // the anchor retained its one-use release authorization
	stop.GateReleaseAuthorized = true
	helperExit := testAppLinuxV2Exit(*reservation.Helper, reservation.Anchor)
	processes := []platform.LinuxOwnerAnchorReapEvidence{
		appLinuxV2AnchorReapEvidence(grant, *reservation.Helper, helperEvidence.Gate.Process, "native", helperExit),
	}
	reaped := appLinuxV2AnchorRunReapResultWithProcesses(t, grant, *reservation.Helper, stop, processes)
	if !linuxV2AnchorReportsGateRelease(reaped.Stop) {
		t.Fatal("anchor mirror result omitted its exact stored gate-release authorization")
	}
	facts, err := linuxV2AnchorReapFactsForReservation(grant, reservation, reaped)
	if err != nil {
		t.Fatalf("validate committed native anchor reaping: %v", err)
	}
	updatedJournal, updatedReservation, err := persistRecoveredV2HelperExit(
		context.Background(), recovery, journal, reservation, facts.helperExit, callbacks,
	)
	if err != nil {
		t.Fatalf("persist anchor helper exit before native receipt: %v", err)
	}
	proof, err := linuxV2MirrorStopProofFromAnchorReap(updatedReservation, reaped, facts)
	if err != nil {
		t.Fatalf("assemble authorized native mirror receipt: %v", err)
	}
	handoff, supervisor, oldReceipt, err := linuxV2JournalOwner(updatedJournal)
	if err != nil || supervisor == nil || oldReceipt != nil {
		t.Fatalf("read exact committed authority before native receipt: handoff=%#v supervisor=%#v receipt=%#v error=%v",
			handoff, supervisor, oldReceipt, err)
	}
	stopReceipt := linuxV2StopReceipt(*supervisor, proof)
	if !stopReceipt.ValidFor(*supervisor) {
		t.Fatal("authorized native mirror stop receipt differs from committed Authority")
	}
	if err := callbacks.RecordStopReceipt(handoff, stopReceipt); err != nil {
		t.Fatalf("persist authorized native mirror receipt: %v", err)
	}
	latest, err := store.LoadJournal(key)
	if err != nil || latest.ContainmentCgroup == nil || latest.ContainmentCgroup.HelperExit == nil ||
		!latest.ContainmentCgroup.HelperExit.Equal(helperExit) || latest.ContainmentAuthority == nil ||
		latest.ContainmentAuthority.StopReceipt == nil || !latest.ContainmentAuthority.StopReceipt.Equal(stopReceipt) {
		t.Fatalf("committed native mirror receipt readback = journal:%#v error:%v", latest, err)
	}
}

func TestRecoveryOnlyPersistsAnchorMirrorPreGateReceiptWithoutPid(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: key.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	recovery := linuxV2AppTestDaemon(t, store, key, true)
	callbacks := recovery.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2LauncherGrantedReservationWithoutExit(t, store, key, callbacks)
	grant := linuxV2GrantFromJournal(key, fence, reservation)
	evidence := appLinuxV2NoGateStopEvidence(t, grant, reservation, true)
	helperExit := testAppLinuxV2Exit(*reservation.Helper, reservation.Anchor)
	reaped := appLinuxV2AnchorRunReapResult(t, grant, *reservation.Helper, *evidence.PreGate.AnchorStop)
	if reaped.Stop.HelperExit == nil || !reaped.Stop.HelperExit.Equal(helperExit) {
		t.Fatal("anchor run-reap fixture lost its exact helper wait proof")
	}
	latest, updatedReservation, err := bindRecoveredV2AnchorPreGateStopReceipt(
		context.Background(), recovery, journal, reservation, grant, reaped, callbacks,
	)
	if err != nil {
		t.Fatalf("persist exact anchor mirror no-gate receipt: %v", err)
	}
	if updatedReservation.HelperExit == nil || !updatedReservation.HelperExit.Equal(helperExit) ||
		updatedReservation.LauncherExit == nil || !updatedReservation.LauncherExit.Equal(*evidence.PreGate.LauncherExit) ||
		updatedReservation.PreGateStopReceipt == nil || updatedReservation.PreGateStopReceipt.Proof.Kind != authority.LinuxCgroupStopProofMirror ||
		updatedReservation.PreGateStopReceipt.Proof.HelperExit == nil || !updatedReservation.PreGateStopReceipt.Proof.HelperExit.Equal(helperExit) ||
		latest.PID != 0 || latest.HasProcessDetails() || latest.ContainmentAuthority != nil || latest.ContainmentHandoff != nil ||
		latest.ContainmentCgroupPreGateReleaseProof != nil {
		t.Fatalf("anchor no-gate receipt did not remain an exact PID-zero reservation: journal=%#v reservation=%#v", latest, updatedReservation)
	}
}

func TestRecoveryOnlyPersistsNoPrepareHelperExitBeforeLeafAbort(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	recovery := linuxV2AppTestDaemon(t, store, key, true)
	callbacks := recovery.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2HelperBoundNoLauncher(t, store, key, callbacks)
	grant := linuxV2GrantFromJournal(key, fence, reservation)
	stopEvidence := appLinuxV2NoPrepareStopEvidence(t, grant, reservation)
	control := &appNoPrepareRecoveryControl{stopEvidence: stopEvidence, fenceEvidence: *stopEvidence.PreGate}
	waiter := &appNoPrepareRecoveryWaiter{result: appLinuxV2AnchorRunReapResult(
		t, grant, *reservation.Helper, *stopEvidence.PreGate.AnchorStop,
	)}
	measured, err := control.Stop(context.Background())
	if err != nil {
		t.Fatalf("helper Stop(): %v", err)
	}
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		if err := writeAppStateAtomic(path, data); err != nil {
			return err
		}
		if writes == 1 {
			return errors.New("injected post-rename helper-exit acknowledgement loss")
		}
		return nil
	})
	updatedJournal, updatedReservation, helperExit, err := persistRecoveredV2NoPrepareHelperExit(
		context.Background(), recovery, control, waiter, grant, journal, reservation, &measured, callbacks,
	)
	if err != nil {
		restore()
		t.Fatalf("persist measured no-Prepare helper reap after lost ACK: %v", err)
	}
	if updatedReservation.HelperExit == nil || !updatedReservation.HelperExit.Equal(helperExit) ||
		updatedJournal.ContainmentCgroup == nil || updatedJournal.ContainmentCgroup.HelperExit == nil ||
		!updatedJournal.ContainmentCgroup.HelperExit.Equal(helperExit) || updatedJournal.ContainmentCgroup.AbortProof != nil ||
		updatedJournal.ContainmentCgroup.LauncherGrantPossible || updatedJournal.ContainmentCgroup.Launcher != nil ||
		updatedJournal.ContainmentCgroup.Gate != nil {
		restore()
		t.Fatalf("no-Prepare recovery journal = %#v, want only exact helper wait proof before leaf abort", updatedJournal.ContainmentCgroup)
	}
	if writes != 2 || control.fenceCalls != 1 || waiter.calls != 1 {
		restore()
		t.Fatalf("recovery interactions: Store writes=%d helper fences=%d anchor waits=%d; want replayed CAS and one exact fence/wait", writes, control.fenceCalls, waiter.calls)
	}
	restore()

	// Restart after the helper reap is durable but before leaf removal. The
	// same Store fence and anchor wait proof replay without re-running helper
	// Stop/Fence or fabricating an abort receipt.
	latest, err := store.LoadJournal(key)
	if err != nil || latest.ContainmentCgroup == nil {
		t.Fatalf("reload helper-exit crash window: %#v, error=%v", latest.ContainmentCgroup, err)
	}
	controlWrites := writes
	_, replayedReservation, replayedExit, err := persistRecoveredV2NoPrepareHelperExit(
		context.Background(), recovery, nil, waiter, grant, latest, latest.ContainmentCgroup.Clone(), nil, callbacks,
	)
	if err != nil || replayedReservation.HelperExit == nil || !replayedReservation.HelperExit.Equal(helperExit) ||
		!replayedExit.Equal(helperExit) || writes != controlWrites {
		t.Fatalf("replay helper-exit crash window = reservation:%#v exit:%#v writes:%d error:%v", replayedReservation, replayedExit, writes, err)
	}
}

func TestLinuxV2HelperExitAndAbortCallbacksRemainProducerBound(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
	app := linuxV2AppTestDaemon(t, store, key, false)
	callbacks := app.linuxV2JournalCallbacks(key, fence)
	reservation := prepareAppV2PreGateReservation(t, store, key, callbacks)
	helperExit := testAppLinuxV2Exit(*reservation.Helper, reservation.Anchor)
	if err := callbacks.RecordHelperExit(reservation, helperExit); err != nil {
		t.Fatalf("record exact helper wait/reap: %v", err)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil || loaded.ContainmentCgroup == nil || loaded.ContainmentCgroup.HelperExit == nil ||
		!loaded.ContainmentCgroup.HelperExit.Equal(helperExit) {
		t.Fatalf("helper exit journal = %#v, error=%v", loaded.ContainmentCgroup, err)
	}
	wrongExit := helperExit
	wrongExit.Witness = authority.LinuxCgroupProcessIdentity{PID: 51, Identity: "other-anchor:51:start"}
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAppStateAtomic(path, data)
	})
	if err := callbacks.RecordHelperExit(reservation, wrongExit); err == nil || writes != 0 {
		restore()
		t.Fatalf("wrong helper exit proof = %v, writes=%d; want rejection before mutation", err, writes)
	}
	restore()
	if err := callbacks.AbortReservation(reservation, authority.LinuxCgroupReservationAbortProof{}); err == nil {
		t.Fatal("AbortReservation() accepted a reservation after the launcher grant became possible")
	}
}

func TestLinuxV2PreflightRejectsMissingDelegationBeforeProbeOrNativeStart(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = present:%t error:%v", present, err)
	}
	capabilities := testVerifiedNativeCapabilities(harness.KindPi)
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(
		t, admission, validNativeTaskResult(t, admission), nil,
		harness.KindPi, config.RuntimeHarnessPi, capabilities,
	)
	defer store.Close()
	owner := &fakeLinuxV2NativeOwner{kind: harness.KindPi, preflightErr: errors.New("delegated cgroup v2 parent is not delegated")}
	app.options.linuxV2NativeOwnerFactory = func(config.Config) (linuxV2NativeOwner, error) { return owner, nil }
	adapterValue, err := app.harnessRegistry.Lookup(harness.KindPi)
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*fakeNativeGoalAdapter)
	app.startAssignment(context.Background(), protocol.Assignment{
		RunID: "run-1", Generation: 1, Work: controlClient.work,
	})
	app.workers.Wait()
	if owner.preflights != 1 || owner.invocations != 0 || adapter.probeCalls != 0 || len(session.callsSnapshot()) != 0 {
		t.Fatalf("missing delegation crossed native boundary: preflight=%d build=%d probe=%d native=%#v",
			owner.preflights, owner.invocations, adapter.probeCalls, session.callsSnapshot())
	}
	if fake := app.workspace.(*fakeWorkspace); fake.prepareCalls != 0 || fake.recoverCalls != 0 {
		t.Fatalf("missing delegation crossed workspace boundary: %#v", fake)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("missing delegation persisted native session intents: %#v, error=%v", sessions, err)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasProcessDetails() || journal.PID != 0 || journal.WorkspacePath != "" {
		t.Fatalf("missing delegation persisted process/workspace authority: %#v", journal)
	}
}

func TestLinuxV2PreflightRejectsLongRecoveryEndpointBeforeOwnerOrWorkspaceEffects(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = present:%t error:%v", present, err)
	}
	capabilities := testVerifiedNativeCapabilities(harness.KindPi)
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(
		t, admission, validNativeTaskResult(t, admission), nil,
		harness.KindPi, config.RuntimeHarnessPi, capabilities,
	)
	defer store.Close()
	longStateDir := filepath.Join(t.TempDir(), strings.Repeat("s", 96))
	key := state.RunKey{RunID: "run-1", Generation: 1}
	if _, err := linuxV2RecoveryEndpoint(longStateDir, key); err == nil || !strings.Contains(err.Error(), "StateDir is too long") {
		t.Fatalf("long StateDir recovery endpoint error = %v, want early socket-path rejection", err)
	}
	app.config.StateDir = longStateDir
	owner := &fakeLinuxV2NativeOwner{kind: harness.KindPi}
	factoryCalls := 0
	app.options.linuxV2NativeOwnerFactory = func(config.Config) (linuxV2NativeOwner, error) {
		factoryCalls++
		return owner, nil
	}
	adapterValue, err := app.harnessRegistry.Lookup(harness.KindPi)
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*fakeNativeGoalAdapter)
	app.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: controlClient.work})
	app.workers.Wait()
	if factoryCalls != 0 || owner.preflights != 0 || owner.invocations != 0 || adapter.probeCalls != 0 || len(session.callsSnapshot()) != 0 {
		t.Fatalf("long recovery endpoint crossed owner/native boundary: factories=%d preflight=%d build=%d probe=%d native=%#v",
			factoryCalls, owner.preflights, owner.invocations, adapter.probeCalls, session.callsSnapshot())
	}
	if fake := app.workspace.(*fakeWorkspace); fake.prepareCalls != 0 || fake.recoverCalls != 0 {
		t.Fatalf("long recovery endpoint crossed workspace boundary: %#v", fake)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LinuxCloneIntent != nil || journal.ContainmentCgroup != nil || journal.WorkspacePath != "" || journal.HasProcessDetails() {
		t.Fatalf("long recovery endpoint persisted clone/cgroup/process effects: %#v", journal)
	}
}

func TestLinuxV2RecoveryEndpointRetainsFullRunKeySHA256(t *testing.T) {
	key := state.RunKey{RunID: "run-1", Generation: 3}
	endpoint, err := linuxV2RecoveryEndpoint(linuxV2NativeOwnerWitnessStateDir, key)
	if err != nil {
		t.Fatalf("fixed installed StateDir endpoint: %v", err)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", key.RunID, key.Generation)))
	want := filepath.Join(linuxV2NativeOwnerWitnessStateDir, "owner-"+hex.EncodeToString(digest[:])+".sock")
	if endpoint != want || len(endpoint) >= 100 || len(filepath.Base(endpoint)) != len("owner-")+sha256.Size*2+len(".sock") {
		t.Fatalf("recovery endpoint = %q (len %d), want full-SHA256 path %q below the Unix socket limit", endpoint, len(endpoint), want)
	}
}

func TestLinuxV2NativeAssignmentIsWithheldForUnresolvedForeignOwner(t *testing.T) {
	for _, test := range []struct {
		name       string
		addForeign func(*testing.T, *state.Store, state.RunKey) state.RunJournal
	}{
		{
			name: "terminal empty-pid unremoved leaf",
			addForeign: func(t *testing.T, store *state.Store, key state.RunKey) state.RunJournal {
				journal := addUnresolvedLinuxV2WitnessJournal(t, store, key)
				if journal.PID != 0 || journal.HasProcessDetails() || journal.ContainmentCgroup == nil || journal.ContainmentCgroup.LeafIdentity == nil {
					t.Fatal("terminal leaf fixture does not have an empty process marker and bound unremoved leaf")
				}
				if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{
					TransitionID: "foreign-failed-terminal", State: "failed", Payload: json.RawMessage(`{}`),
				}, time.Now().UTC()); err != nil {
					t.Fatalf("record terminal failure over unresolved foreign leaf: %v", err)
				}
				journal, err := store.LoadJournal(key)
				if err != nil || journal.TerminalState != "failed" || journal.PID != 0 || journal.ContainmentCgroup == nil {
					t.Fatalf("read terminal unresolved foreign leaf: %#v error=%v", journal, err)
				}
				return journal
			},
		},
		{
			name: "migration unproven marker",
			addForeign: func(t *testing.T, store *state.Store, key state.RunKey) state.RunJournal {
				work := protocol.Work{Goal: "foreign migration owner", AgentProfile: "local", Workspace: "local", Input: json.RawMessage(`{}`)}
				intent := state.ClaimIntent{
					Key: key, RuntimeKey: "foreign-runtime", RuntimeID: "foreign-runtime-id", RuntimeEpoch: 1,
					ClaimID: "foreign-migration-claim", LocalState: "claiming", Work: work, WorkspaceBindingKey: work.Workspace,
				}
				if _, err := store.SaveClaimIntent(intent); err != nil {
					t.Fatalf("save migration owner claim: %v", err)
				}
				if _, err := store.SaveClaimGrant(key, protocol.ClaimResponse{
					RunID: key.RunID, Generation: key.Generation, ClaimID: intent.ClaimID,
					LeaseToken: "foreign-migration-lease", LeaseExpiresAt: time.Now().Add(time.Hour), Work: work,
				}); err != nil {
					t.Fatalf("save migration owner grant: %v", err)
				}
				const pid = 260
				const identity = "foreign-migration:260:start"
				if _, err := store.SetProcessDetails(key, pid, identity, time.Now().UTC()); err != nil {
					t.Fatalf("persist migration owner marker: %v", err)
				}
				if _, err := store.MarkContainmentUnproven(key, pid, identity); err != nil {
					t.Fatalf("persist migration uncertainty: %v", err)
				}
				journal, err := store.LoadJournal(key)
				if err != nil || !journal.ContainmentUnproven || !journal.HasProcessDetails() {
					t.Fatalf("read migration owner uncertainty: %#v error=%v", journal, err)
				}
				return journal
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = present:%t error:%v", present, err)
			}
			app, store, session, controlClient := nativeAdmissionDaemonForHarness(
				t, admission, validNativeTaskResult(t, admission), nil,
				harness.KindPi, config.RuntimeHarnessPi, testVerifiedNativeCapabilities(harness.KindPi),
			)
			defer store.Close()
			foreignKey := state.RunKey{RunID: "foreign-unresolved-" + strings.ReplaceAll(test.name, " ", "-"), Generation: 1}
			foreign := test.addForeign(t, store, foreignKey)
			before := snapshotLinuxV2NativeWitnessJournals(t, app.config.StateDir)
			owner := &fakeLinuxV2NativeOwner{kind: harness.KindPi}
			ownerFactoryCalls := 0
			app.options.linuxV2NativeOwnerFactory = func(config.Config) (linuxV2NativeOwner, error) {
				ownerFactoryCalls++
				return owner, nil
			}
			app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
			app.workers.Wait()
			if ownerFactoryCalls != 0 || owner.preflights != 0 || owner.invocations != 0 || len(session.callsSnapshot()) != 0 ||
				controlClient.claimCalls != 0 || len(controlClient.calls) != 0 {
				t.Fatalf("unresolved foreign owner crossed native dispatch gate: factories=%d preflight=%d build=%d claims=%d calls=%#v control=%#v",
					ownerFactoryCalls, owner.preflights, owner.invocations, controlClient.claimCalls, session.callsSnapshot(), controlClient.calls)
			}
			if fake := app.workspace.(*fakeWorkspace); fake.prepareCalls != 0 || fake.recoverCalls != 0 {
				t.Fatalf("unresolved foreign owner crossed workspace boundary: %#v", fake)
			}
			if _, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1}); !state.IsNotFound(err) {
				t.Fatalf("native assignment was claimed despite unresolved foreign owner: %v", err)
			}
			assertLinuxV2NativeWitnessJournalsUnchanged(t, before, snapshotLinuxV2NativeWitnessJournals(t, app.config.StateDir))
			latest, err := store.LoadJournal(foreignKey)
			if err != nil || latest.Key() != foreign.Key() {
				t.Fatalf("foreign owner row changed or disappeared: %#v error=%v", latest, err)
			}
		})
	}
}

func TestLinuxV2AdmissionAllowsOnlyCurrentLiveForeignOwners(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	bindAppLinuxV2Owner(t, store, key, testAppLinuxV2Reservation())
	journal, err := store.LoadJournal(key)
	if err != nil || journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Helper == nil || journal.ContainmentAuthority == nil {
		t.Fatalf("read committed active v2 Journal: %#v error=%v", journal, err)
	}
	status := execution.LinuxV2LiveOwnerStatus{
		RunKey: key, Fence: journal.Fence(), Helper: *journal.ContainmentCgroup.Helper,
		Target:          authority.LinuxCgroupProcessIdentity{PID: journal.PID, Identity: journal.ProcessIdentity},
		HelperPIDFDLive: true, TargetPIDFDLive: true, LeaseSequence: 1, LeaseLive: true,
		CommitAcknowledged: true, ReleaseAttempted: true, ReleaseSucceeded: true,
	}
	process := &linuxV2AdmissionStatusFakeProcess{status: status}
	active := &runningRun{
		process: process, nativeSession: &fakeNativeGoalSession{}, claimed: true,
		starting: true, nativeStartInFlight: true,
		localLeaseDeadlineAt: time.Now().Add(time.Minute), leaseSequence: 1,
	}
	app := &daemon{
		store: store, runtimeID: journal.RuntimeID, runtimeEpoch: journal.ClaimedRuntimeEpoch,
		running: map[state.RunKey]*runningRun{key: active},
		options: options{clock: time.Now, localClock: time.Now},
	}
	if err := app.checkLinuxV2NativeAdmissionBeforeClaim(context.Background()); err != nil {
		t.Fatalf("exact live committed/released foreign owner was rejected: %v", err)
	}
	process.status.LeaseLive = false
	if err := app.checkLinuxV2NativeAdmissionBeforeClaim(context.Background()); err == nil {
		t.Fatal("foreign owner with an unacknowledged/expired helper lease was admitted")
	}
	process.status.LeaseLive = true
	app.mu.Lock()
	active.cancelled = true
	app.mu.Unlock()
	if err := app.checkLinuxV2NativeAdmissionBeforeClaim(context.Background()); err == nil {
		t.Fatal("foreign owner with a pending cancellation was admitted as healthy")
	}
}

func TestLinuxV2CapacityTwoWaitsForReleaseThenRechecksOwnerState(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		firstUnproven   bool
		firstStale      bool
		wantSecondStart bool
	}{
		{name: "healthy released first owner", wantSecondStart: true},
		{name: "unproven first owner remains blocking", firstUnproven: true},
		{name: "stale first owner remains blocking", firstUnproven: true, firstStale: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = present:%t error:%v", present, err)
			}
			app, store, template, controlClient := nativeAdmissionDaemonForHarness(
				t, admission, validNativeTaskResult(t, admission), nil,
				harness.KindPi, config.RuntimeHarnessPi, testVerifiedNativeCapabilities(harness.KindPi),
			)
			defer store.Close()
			app.workspace = &linuxV2CapacityWorkspace{fakeWorkspace: app.workspace.(*fakeWorkspace)}
			app.slots = make(chan struct{}, 2)
			ctx, cancel := context.WithCancel(context.Background())
			firstOwnerReady := make(chan struct{})
			firstOwnerRelease := make(chan struct{})
			runWaitRelease := make(chan struct{})
			firstTurnEntered := make(chan struct{}, 1)
			secondTurnEntered := make(chan struct{}, 1)
			secondOwnerReady := make(chan error, 1)
			firstOwnerReleased := false
			defer func() {
				if !firstOwnerReleased {
					close(firstOwnerRelease)
				}
				cancel()
				stopped := make(chan struct{})
				go func() {
					app.workers.Wait()
					close(stopped)
				}()
				select {
				case <-stopped:
				case <-time.After(10 * time.Second):
					t.Error("capacity-two workers did not stop after cancellation")
				}
			}()

			adapterValue, err := app.harnessRegistry.Lookup(harness.KindPi)
			if err != nil {
				t.Fatal(err)
			}
			adapter := adapterValue.(*fakeNativeGoalAdapter)
			adapter.startEntered = make(chan struct{}, 2)
			adapter.sessionFactory = func(request harness.StartRequest) *fakeNativeGoalSession {
				threadID := "native-thread-1"
				turnEntered := firstTurnEntered
				if request.Invocation.LinuxV2 != nil && request.Invocation.LinuxV2.RunKey.RunID == "run-second" {
					threadID = "native-thread-2"
					turnEntered = secondTurnEntered
				}
				return &fakeNativeGoalSession{
					handle: harness.NativeSessionHandle{ID: threadID}, nativeThreadID: threadID,
					result: template.result, waitGate: runWaitRelease, suppressTaskResult: true,
					turnStarted: make(chan struct{}), waitTurnEntered: turnEntered, skipProcessPersist: true,
				}
			}
			adapter.sessionWrapper = func(session *fakeNativeGoalSession) harness.Session {
				return &linuxV2AdmissionStatusHarnessSession{fakeNativeGoalSession: session, status: session.linuxV2Status}
			}
			adapter.onStart = func(request harness.StartRequest, session *fakeNativeGoalSession) error {
				if request.Invocation.LinuxV2 == nil {
					return errors.New("capacity-two test did not receive an explicit Linux v2 invocation")
				}
				if request.Invocation.LinuxV2.RunKey.RunID != "run-first" {
					err := bindAppLinuxV2OwnerWithCallbacks(
						store, request.Invocation.LinuxV2.RunKey, request.Invocation.LinuxV2.Callbacks, session,
					)
					secondOwnerReady <- err
					return err
				}
				if scenario.firstUnproven {
					callbacks := request.Invocation.LinuxV2.Callbacks
					reservation := testAppLinuxV2Reservation()
					if err := callbacks.Reserve(reservation); err != nil {
						return err
					}
					if err := callbacks.MarkCreateStarted(reservation); err != nil {
						return err
					}
					close(firstOwnerReady)
					<-firstOwnerRelease
					return errors.New("simulated unproven pre-RELEASE owner")
				}
				if err := bindAppLinuxV2OwnerWithCallbacks(
					store, request.Invocation.LinuxV2.RunKey, request.Invocation.LinuxV2.Callbacks, session,
				); err != nil {
					return err
				}
				close(firstOwnerReady)
				<-firstOwnerRelease
				return nil
			}

			owner := &fakeLinuxV2NativeOwner{kind: harness.KindPi}
			app.options.linuxV2NativeOwnerFactory = func(config.Config) (linuxV2NativeOwner, error) { return owner, nil }
			admission2 := admission
			admission2.AdmissionID = "00000000-0000-4000-8000-000000000023"
			admission2.ContextSnapshotID = "00000000-0000-4000-8000-000000000024"
			workItem2 := "00000000-0000-4000-8000-000000000025"
			admission2.WorkItemID = &workItem2
			admission2Input, err := json.Marshal(admission2)
			if err != nil {
				t.Fatal(err)
			}
			workSecond := controlClient.work
			workSecond.Input = admission2Input
			controlClient.admissionForRun = func(runID string) protocol.Admission {
				if runID == "run-second" {
					return admission2
				}
				return admission
			}
			controlClient.workForRun = func(runID string) protocol.Work {
				if runID == "run-second" {
					return workSecond
				}
				return controlClient.work
			}
			controlClient.taskIDForRun = func(runID string) string {
				if runID == "run-second" {
					return "task-second"
				}
				return "task-first"
			}
			controlClient.sessionIDForRun = func(runID string) string {
				if runID == "run-second" {
					return "00000000-0000-4000-8000-000000000022"
				}
				return "00000000-0000-4000-8000-000000000021"
			}
			controlClient.bindingIDForRun = func(runID string) string {
				if runID == "run-second" {
					return "00000000-0000-4000-8000-000000000027"
				}
				return "00000000-0000-4000-8000-000000000026"
			}
			controlClient.attachmentIDForRun = func(runID string) string {
				if runID == "run-second" {
					return "00000000-0000-4000-8000-000000000029"
				}
				return "00000000-0000-4000-8000-000000000028"
			}
			gateWaiters := make(chan time.Duration, 2)
			app.options.newTimer = func(delay time.Duration) deadlineTimer {
				gateWaiters <- delay
				return &manualDeadlineTimer{channel: make(chan time.Time), delay: delay}
			}

			app.startAssignment(ctx, protocol.Assignment{RunID: "run-first", Generation: 1, Work: controlClient.work})
			firstWorkerDone := make(chan struct{})
			go func() {
				app.workers.Wait()
				close(firstWorkerDone)
			}()
			select {
			case <-adapter.startEntered:
			case <-firstWorkerDone:
				journal, loadErr := store.LoadJournal(state.RunKey{RunID: "run-first", Generation: 1})
				t.Fatalf("first capacity-two Run exited before native Start: journal=%#v err=%v", journal, loadErr)
			case <-time.After(10 * time.Second):
				t.Fatal("first capacity-two Run did not enter native Start")
			}
			select {
			case <-firstOwnerReady:
			case <-firstWorkerDone:
				journal, loadErr := store.LoadJournal(state.RunKey{RunID: "run-first", Generation: 1})
				t.Fatalf("first capacity-two Run exited before establishing owner state: journal=%#v err=%v", journal, loadErr)
			case <-time.After(10 * time.Second):
				t.Fatal("first capacity-two Run did not establish owner state")
			}
			app.startAssignment(ctx, protocol.Assignment{RunID: "run-second", Generation: 1, Work: workSecond})
			select {
			case delay := <-gateWaiters:
				if delay <= leaseSafetyMargin {
					t.Fatalf("second Run waited with only %s remaining before lease safety margin", delay)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("second Run did not wait for the first launch phase")
			}
			if controlClient.claimCalls != 2 {
				t.Fatalf("capacity-two claims while first is pre-RELEASE = %d, want 2", controlClient.claimCalls)
			}
			secondKey := state.RunKey{RunID: "run-second", Generation: 1}
			beforeRelease, err := store.LoadJournal(secondKey)
			if err != nil || beforeRelease.TerminalState != "" || beforeRelease.LinuxCloneIntent != nil ||
				beforeRelease.ContainmentCgroup != nil || beforeRelease.HasProcessDetails() {
				t.Fatalf("second claim crossed the pending first launch phase: journal=%#v err=%v", beforeRelease, err)
			}

			if scenario.firstStale {
				if _, err := store.SetLocalState(state.RunKey{RunID: "run-first", Generation: 1}, "stale"); err != nil {
					t.Fatalf("mark first owner stale while its launch phase remains held: %v", err)
				}
			}
			close(firstOwnerRelease)
			firstOwnerReleased = true
			if scenario.wantSecondStart {
				// RELEASE only ends the serialized launch phase. Wait for the
				// first live session to be published while its task remains blocked
				// before the second owner rechecks cross-RunKey state.
				select {
				case <-firstTurnEntered:
				case <-time.After(10 * time.Second):
					t.Fatal("first capacity-two Run did not enter its live turn after RELEASE")
				}
				firstKey := state.RunKey{RunID: "run-first", Generation: 1}
				liveOwners, err := app.linuxV2LiveOwnerFences(ctx, state.RunKey{})
				if err != nil || liveOwners[firstKey] == (protocol.Fence{}) {
					t.Fatalf("first capacity-two owner was not live after RELEASE: owners=%#v err=%v", liveOwners, err)
				}
				select {
				case <-adapter.startEntered:
				case <-firstWorkerDone:
					firstJournal, firstErr := store.LoadJournal(state.RunKey{RunID: "run-first", Generation: 1})
					secondJournal, secondErr := store.LoadJournal(secondKey)
					t.Fatalf("capacity-two workers exited before second native Start: first=%#v (%v), second=%#v (%v)", firstJournal, firstErr, secondJournal, secondErr)
				case <-time.After(10 * time.Second):
					t.Fatal("second capacity-two Run did not enter native Start after first RELEASE")
				}
				select {
				case err := <-secondOwnerReady:
					if err != nil {
						t.Fatalf("bind second capacity-two owner after first RELEASE: %v", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("second capacity-two owner did not finish binding")
				}
				select {
				case <-secondTurnEntered:
				case <-time.After(10 * time.Second):
					t.Fatal("second capacity-two Run did not enter its live turn")
				}
				for _, key := range []state.RunKey{{RunID: "run-first", Generation: 1}, secondKey} {
					journal, err := store.LoadJournal(key)
					if err != nil || journal.TerminalState != "" || journal.LocalState != "running" ||
						journal.LinuxCloneIntent == nil || journal.ContainmentAuthority == nil {
						t.Fatalf("released capacity-two Run %s did not remain active: journal=%#v err=%v", key.RunID, journal, err)
					}
				}
				if owner.preflights != 2 || owner.invocations != 2 {
					t.Fatalf("healthy capacity-two starts = preflights:%d invocations:%d, want 2 each", owner.preflights, owner.invocations)
				}
			} else {
				select {
				case <-firstWorkerDone:
				case <-time.After(10 * time.Second):
					t.Fatal("capacity-two workers did not reject the unresolved first owner")
				}
				firstKey := state.RunKey{RunID: "run-first", Generation: 1}
				firstJournal, firstErr := store.LoadJournal(firstKey)
				secondJournal, secondErr := store.LoadJournal(secondKey)
				if firstErr != nil || firstJournal.ContainmentCgroup == nil || !firstJournal.ContainmentCgroup.CreateStarted ||
					secondErr != nil || secondJournal.LinuxCloneIntent != nil || secondJournal.ContainmentCgroup != nil ||
					len(adapter.startEntered) != 0 || owner.preflights != 1 || owner.invocations != 1 {
					t.Fatalf("unproven first owner did not block the second effect: first=%#v (%v), second=%#v (%v), starts=%d owner=%#v",
						firstJournal, firstErr, secondJournal, secondErr, len(adapter.startEntered), owner)
				}
			}
		})
	}
}

func TestLinuxV2LaunchPhaseWaitStopsAtClaimLeaseSafetyMargin(t *testing.T) {
	ownerKey := state.RunKey{RunID: "run-owner", Generation: 1}
	waiterKey := state.RunKey{RunID: "run-waiter", Generation: 1}
	clock := time.Now
	timers := make(chan *manualDeadlineTimer, 1)
	app := &daemon{
		running: map[state.RunKey]*runningRun{
			ownerKey:  {localLeaseDeadlineAt: clock().Add(time.Minute)},
			waiterKey: {localLeaseDeadlineAt: clock().Add(leaseSafetyMargin + time.Second)},
		},
		options: options{clock: clock, localClock: clock, newTimer: func(delay time.Duration) deadlineTimer {
			timer := &manualDeadlineTimer{channel: make(chan time.Time), delay: delay}
			timers <- timer
			return timer
		}},
	}
	releaseOwner, err := app.acquireLinuxV2LaunchPhase(context.Background(), ownerKey, protocol.ClaimResponse{})
	if err != nil {
		t.Fatalf("acquire first native launch phase: %v", err)
	}
	waiterResult := make(chan error, 1)
	go func() {
		_, err := app.acquireLinuxV2LaunchPhase(context.Background(), waiterKey, protocol.ClaimResponse{})
		waiterResult <- err
	}()
	timer := <-timers
	if timer.delay <= 0 {
		releaseOwner()
		t.Fatalf("claim-bounded launch wait timer = %s, want positive time before the safety margin", timer.delay)
	}
	timer.channel <- clock()
	if err := <-waiterResult; !errors.Is(err, errLeaseDeadlineReached) {
		releaseOwner()
		t.Fatalf("launch phase waiter error = %v, want errLeaseDeadlineReached", err)
	}
	releaseOwner()
}

type linuxV2CapacityWorkspace struct {
	*fakeWorkspace
}

func (*linuxV2CapacityWorkspace) PlanLinuxCloneIntent(bindingKey string, run workspace.RunRef, subject protocol.Subject) (authority.LinuxCloneIntent, error) {
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: run.RunID, Generation: run.Generation,
		BindingKey: bindingKey, ResourceID: subject.ResourceID, BaseCommit: subject.Commit,
		BaseTreeDigest: subject.TreeDigest, Nonce: strings.Repeat("a", authority.LinuxCloneNonceBytes*2),
		SeedPath: filepath.Join("/tmp", "symmetry-app-test-seeds", bindingKey, "run-"+run.RunID+"-generation-"+fmt.Sprint(run.Generation)+".bundle"),
		Target:   filepath.Join("/tmp", "symmetry-app-test-clones", "binding-"+bindingKey, "run-"+run.RunID, "generation-"+fmt.Sprint(run.Generation)),
	}
	if err := intent.Validate(); err != nil {
		return authority.LinuxCloneIntent{}, err
	}
	return intent, nil
}

type linuxV2AdmissionStatusFakeProcess struct {
	fakeProcess
	status execution.LinuxV2LiveOwnerStatus
}

func (process *linuxV2AdmissionStatusFakeProcess) ProcessDetails() (int, string) {
	return process.status.Target.PID, process.status.Target.Identity
}

func (process *linuxV2AdmissionStatusFakeProcess) LinuxV2LiveOwnerStatus(context.Context) (execution.LinuxV2LiveOwnerStatus, error) {
	return process.status, nil
}

type linuxV2AdmissionStatusHarnessSession struct {
	*fakeNativeGoalSession
	status execution.LinuxV2LiveOwnerStatus
}

func (session *linuxV2AdmissionStatusHarnessSession) LinuxV2LiveOwnerStatus(ctx context.Context) (execution.LinuxV2LiveOwnerStatus, error) {
	if err := ctx.Err(); err != nil {
		return execution.LinuxV2LiveOwnerStatus{}, err
	}
	return session.status, nil
}

func TestLinuxV2AdmissionGateDoesNotSuppressLegacyCodexAssignment(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = present:%t error:%v", present, err)
	}
	app, store, _, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	foreignKey := state.RunKey{RunID: "foreign-unresolved-native-run", Generation: 1}
	if journal := addUnresolvedLinuxV2WitnessJournal(t, store, foreignKey); !journal.HasPendingContainment() {
		t.Fatal("foreign native-owner fixture is not unresolved")
	}
	app.startAssignment(context.Background(), protocol.Assignment{RunID: "legacy-codex-run", Generation: 1, Work: controlClient.work})
	app.workers.Wait()
	if controlClient.claimCalls != 1 {
		t.Fatalf("unrelated Codex assignment claims = %d, want one dispatch despite unresolved v2 owner", controlClient.claimCalls)
	}
	if _, err := store.LoadJournal(state.RunKey{RunID: "legacy-codex-run", Generation: 1}); err != nil {
		t.Fatalf("legacy Codex assignment did not retain its own RunJournal: %v", err)
	}
}

func TestLinuxV2PiResumeRequiresSeparateCloneReuseGrant(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = present:%t error:%v", present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	bindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := testVerifiedNativeCapabilities(harness.KindPi)
	capabilities.Resume = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(
		t, admission, validNativeTaskResult(t, admission), nil,
		harness.KindPi, config.RuntimeHarnessPi, capabilities,
	)
	defer store.Close()
	controlClient.harnessSessionID = &controlSessionID
	controlClient.harnessBindingID = &bindingID
	owner := &fakeLinuxV2NativeOwner{kind: harness.KindPi}
	app.options.linuxV2NativeOwnerFactory = func(config.Config) (linuxV2NativeOwner, error) { return owner, nil }
	adapterValue, err := app.harnessRegistry.Lookup(harness.KindPi)
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*fakeNativeGoalAdapter)
	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()
	if owner.preflights != 0 || owner.invocations != 0 || adapter.probeCalls != 0 || len(session.callsSnapshot()) != 0 ||
		len(controlClient.calls) != 0 || app.workspace.(*fakeWorkspace).prepareCalls != 0 || app.workspace.(*fakeWorkspace).recoverCalls != 0 {
		t.Fatalf("Linux v2 retained-session resume crossed the separately fenced clone-reuse boundary: preflight=%d build=%d probe=%d native=%#v control=%#v workspace=%#v",
			owner.preflights, owner.invocations, adapter.probeCalls, session.callsSnapshot(), controlClient.calls, app.workspace)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil || len(journal.PendingTransitions) != 1 ||
		!strings.Contains(string(journal.PendingTransitions[0].Payload), string(protocol.TaskResultReasonResumeRejected)) ||
		journal.LinuxCloneIntent != nil || journal.WorkspacePath != "" {
		t.Fatalf("Linux v2 resume refusal journal = %#v, error=%v", journal, err)
	}
}

func TestLinuxPiLegacyClaimCannotUseGenericRunner(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	configValue := testConfig(t)
	configValue.Runtime.HarnessKind = config.RuntimeHarnessPi
	workspaceService := &countingWorkspace{}
	starts := 0
	app := &daemon{
		config: configValue, store: store, control: &fakeControl{}, workspace: workspaceService,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			starts++
			return fakeProcess{result: execution.Result{}}, nil
		},
		options:   options{newID: ids(), clock: time.Now},
		runtimeID: "runtime-1", runtimeEpoch: 1,
		running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1),
	}
	work := protocol.Work{Goal: "legacy Pi work", AgentProfile: "local", Workspace: "local"}
	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-legacy-pi", Generation: 1, Work: work})
	app.workers.Wait()
	if starts != 0 || workspaceService.prepareCalls != 0 {
		t.Fatalf("legacy Linux Pi claim executed without v2 admission: starts=%d workspace prepares=%d", starts, workspaceService.prepareCalls)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-legacy-pi", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || journal.HasProcessDetails() || journal.WorkspacePath != "" {
		t.Fatalf("legacy Linux Pi refusal journal = %#v, want failed before workspace/process", journal)
	}
}

func TestLinuxV2PIDZeroReservationBlocksWorkspaceCleanup(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetLocalState(key, "running"); err != nil {
		t.Fatal(err)
	}
	reservation := testAppLinuxV2Reservation()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := journal.Fence()
	if _, err := store.ReserveLinuxCgroup(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.PID != 0 || journal.HasProcessDetails() || !journal.HasPendingContainment() {
		t.Fatalf("pre-gate reservation journal = %#v, want PID-zero pending containment", journal)
	}
	cleanupWorkspace := &cleanupGuardWorkspace{}
	app := &daemon{store: store, workspace: cleanupWorkspace, config: testConfig(t), options: options{clock: time.Now}}
	cleanupView := journal
	cleanupView.LocalState = "cleanup_pending"
	if err := app.cleanupPending(context.Background(), cleanupView); err == nil || !strings.Contains(err.Error(), "containment state remains pending") {
		t.Fatalf("cleanupPending() error = %v, want pending-containment guard", err)
	}
	if cleanupWorkspace.recoverCalls != 0 || cleanupWorkspace.cleanupCalls != 0 {
		t.Fatalf("PID-zero reservation reached workspace cleanup: recover=%d cleanup=%d", cleanupWorkspace.recoverCalls, cleanupWorkspace.cleanupCalls)
	}
	latest, err := store.LoadJournal(key)
	if err != nil || latest.ContainmentCgroup == nil || latest.ContainmentCgroup.CreateStarted || latest.PID != 0 {
		t.Fatalf("PID-zero reservation after blocked cleanup = %#v, error=%v", latest, err)
	}
}

func TestRecoverContainmentOnlyUsesTheRunStoreLockWithoutDaemonSetup(t *testing.T) {
	directory := t.TempDir()
	store, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecoverContainmentOnly(context.Background(), config.Config{StateDir: directory}); err == nil {
		t.Fatal("RecoverContainmentOnly() succeeded while the normal Store held its exclusive StateDir lock")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RecoverContainmentOnly(context.Background(), config.Config{StateDir: directory}); err != nil {
		t.Fatalf("empty recovery-only Store scan required daemon registration/setup: %v", err)
	}
}

func prepareAppV2PreGateReservation(t *testing.T, store *state.Store, key state.RunKey, callbacks execution.LinuxV2JournalCallbacks) state.LinuxCgroupReservation {
	t.Helper()
	reservation := prepareAppV2LauncherGrantedReservationWithoutExit(t, store, key, callbacks)
	launcherExit := testAppLinuxV2Exit(*reservation.Launcher, *reservation.Helper)
	if err := callbacks.RecordLauncherExit(reservation, launcherExit); err != nil {
		t.Fatal(err)
	}
	return loadAppLinuxV2Reservation(t, store, key)
}

func prepareAppV2LauncherGrantedReservationWithoutExit(t *testing.T, store *state.Store, key state.RunKey, callbacks execution.LinuxV2JournalCallbacks) state.LinuxCgroupReservation {
	t.Helper()
	reservation := testAppLinuxV2Reservation()
	if err := callbacks.Reserve(reservation); err != nil {
		t.Fatal(err)
	}
	if err := callbacks.MarkCreateStarted(reservation); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if err := callbacks.BindLeaf(reservation, testAppLinuxV2Leaf(reservation)); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if err := callbacks.BindHelper(reservation, testAppLinuxV2Helper()); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if err := callbacks.BindLauncher(reservation, testAppLinuxV2Launcher()); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if err := callbacks.ArmLauncherGrant(reservation); err != nil {
		t.Fatal(err)
	}
	return loadAppLinuxV2Reservation(t, store, key)
}

func prepareAppV2HelperBoundNoLauncher(t *testing.T, store *state.Store, key state.RunKey, callbacks execution.LinuxV2JournalCallbacks) state.LinuxCgroupReservation {
	t.Helper()
	reservation := testAppLinuxV2Reservation()
	if err := callbacks.Reserve(reservation); err != nil {
		t.Fatal(err)
	}
	if err := callbacks.MarkCreateStarted(reservation); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if err := callbacks.BindLeaf(reservation, testAppLinuxV2Leaf(reservation)); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if err := callbacks.BindHelper(reservation, testAppLinuxV2Helper()); err != nil {
		t.Fatal(err)
	}
	return loadAppLinuxV2Reservation(t, store, key)
}

func appLinuxV2PreGateReceipt(t *testing.T, reservation state.LinuxCgroupReservation, authenticator string) authority.LinuxCgroupPreGateStopReceipt {
	t.Helper()
	if reservation.LeafIdentity == nil || reservation.Helper == nil || reservation.Launcher == nil ||
		reservation.LauncherExit == nil || reservation.Mirror == nil {
		t.Fatal("pre-gate fixture lacks exact leaf and producer identities")
	}
	gateOutcome := authority.LinuxCgroupGateProductionResult{
		Version: authority.LinuxCgroupIdentityVersion, LaunchToken: reservation.LaunchToken,
		Launcher: *reservation.Launcher, Outcome: authority.LinuxCgroupGateOutcomeNeverCreated,
		Authenticator: authenticator,
	}
	proof := authority.LinuxCgroupPreGateStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
		Issuer: *reservation.Helper, Reaper: *reservation.Helper, PinnedLeafFD: true,
		CgroupKillAcknowledged: true, CgroupPopulatedZero: true, NoMigrationObserved: true,
		ProducerFenced: true, DescendantsReaped: true, LauncherExit: *reservation.LauncherExit,
		GateOutcome: gateOutcome,
	}
	receipt := authority.LinuxCgroupPreGateStopReceipt{
		Version: authority.LinuxCgroupIdentityVersion, LaunchToken: reservation.LaunchToken,
		CgroupIdentity: *reservation.LeafIdentity, OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
		Anchor: reservation.Anchor, Helper: *reservation.Helper, Launcher: *reservation.Launcher,
		Mirror: *reservation.Mirror, Proof: proof,
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("pre-gate receipt fixture is structurally invalid: %v", err)
	}
	return receipt
}

func TestContainmentOnlyJournalLoopKeepsLegacyLinuxRecoveryPath(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	const legacyIdentity = "linux:01234567-89ab-cdef-0123-456789abcdef:777:999"
	if _, err := store.SetProcessDetails(key, 777, legacyIdentity, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	stops := 0
	recovery := &daemon{
		store: store,
		options: options{
			clock: time.Now,
			terminatePersist: func(pid int, identity string) error {
				stops++
				if pid != 777 || identity != legacyIdentity {
					t.Fatalf("legacy stop target = %d/%q", pid, identity)
				}
				return nil
			},
		},
		recoveryOnly:   true,
		running:        make(map[state.RunKey]*runningRun),
		recoveredStops: make(map[state.RunKey]recoveredProcessStop),
	}
	if err := recoverContainmentOnlyJournals(context.Background(), recovery, []state.RunJournal{journal}); err != nil {
		t.Fatalf("recover legacy Linux group journal: %v", err)
	}
	latest, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if stops != 1 || latest.HasProcessDetails() || latest.ContainmentAuthority != nil || latest.ContainmentCgroup != nil {
		t.Fatalf("legacy recovery result: stops=%d journal=%#v", stops, latest)
	}
}

type cleanupGuardWorkspace struct {
	recoverCalls int
	cleanupCalls int
}

func (*cleanupGuardWorkspace) Prepare(context.Context, string, workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("unexpected workspace prepare")
}

func (service *cleanupGuardWorkspace) Recover(_ context.Context, binding string, run workspace.RunRef, path string) (workspace.Prepared, error) {
	service.recoverCalls++
	return workspace.Prepared{BindingKey: binding, Run: run, Path: path}, nil
}

func (service *cleanupGuardWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error {
	service.cleanupCalls++
	return nil
}

func testAppLinuxV2Reservation() state.LinuxCgroupReservation {
	mirror := authority.LinuxCgroupProcessIdentity{PID: 50, Identity: "daemon:50:start"}
	return state.LinuxCgroupReservation{
		Version:     state.LinuxCgroupReservationVersion,
		LaunchToken: strings.Repeat("a", authority.TokenBytes*2),
		Secret:      strings.Repeat("b", authority.SecretBytes*2),
		PipeToken:   strings.Repeat("c", authority.TokenBytes*2),
		JobID:       strings.Repeat("d", authority.TokenBytes*2),
		OwnerUID:    1000, LauncherUID: 0, RecoveryEndpoint: "/run/symmetry/recovery.sock",
		Parent: authority.LinuxCgroupParentIdentity{
			BootID: "00000000-0000-4000-8000-000000000001", MountID: 42, Device: 84, Inode: 126,
			Path: "/system.slice/symmetry.slice/delegated",
		},
		LeafName: strings.Repeat("e", authority.TokenBytes*2),
		Anchor:   authority.LinuxCgroupProcessIdentity{PID: 10, Identity: "anchor:10:start"},
		Mirror:   &mirror,
	}
}

func linuxV2AppTestDaemon(t *testing.T, store *state.Store, key state.RunKey, recoveryOnly bool) *daemon {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("load Linux v2 App target fixture: %v", err)
	}
	if journal.TerminalState == "" && journal.LocalState != "claimed" && journal.LocalState != "running" {
		journal, err = store.SetLocalState(key, "running")
		if err != nil {
			t.Fatalf("make Linux v2 App target effect-ready: %v", err)
		}
	}
	return &daemon{
		store: store, runtimeID: journal.RuntimeID, runtimeEpoch: journal.ClaimedRuntimeEpoch,
		running: map[state.RunKey]*runningRun{
			key: {claimed: true, localLeaseDeadlineAt: time.Now().Add(time.Minute)},
		},
		options: options{clock: time.Now, localClock: time.Now}, recoveryOnly: recoveryOnly,
	}
}

func bindAppLinuxV2Owner(t *testing.T, store *state.Store, key state.RunKey, reservation state.LinuxCgroupReservation) (authority.SupervisorHandoff, authority.Supervisor, state.LinuxCgroupReservation) {
	t.Helper()
	if _, err := store.SetLocalState(key, "running"); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := journal.Fence()
	if _, err := store.ReserveLinuxCgroup(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if _, err := store.BindLinuxCgroupLeaf(key, reservation, testAppLinuxV2Leaf(reservation)); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	helper := testAppLinuxV2Helper()
	if _, err := store.BindLinuxCgroupHelper(key, reservation, helper); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	launcher := testAppLinuxV2Launcher()
	if _, err := store.BindLinuxCgroupLauncher(key, reservation, launcher); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if _, err := store.ArmLinuxCgroupLauncherGrant(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	launcherExit := testAppLinuxV2Exit(launcher, helper)
	if _, err := store.RecordLinuxCgroupLauncherExit(key, reservation, launcherExit); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	gate := authority.LinuxCgroupGateIdentity{
		Process: authority.LinuxCgroupProcessIdentity{PID: 40, Identity: "gate:40:start"},
		HostUID: reservation.OwnerUID, ObservedAt: time.Date(2026, 9, 26, 6, 7, 8, 0, time.UTC),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: *reservation.LeafIdentity,
		BornInLeaf: true, TrustedImage: true,
	}
	if _, err := store.BindLinuxCgroupGate(key, reservation, gate); err != nil {
		t.Fatal(err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	handoff, err := linuxV2HandoffFromReservation(reservation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoffV2(key, handoff, helper, *reservation.Mirror)
	if err != nil {
		t.Fatal(err)
	}
	handoff = bound.ContainmentHandoff.Clone()
	if _, err := store.CommitSupervisorHandoff(key, handoff, gate.ObservedAt); err != nil {
		t.Fatal(err)
	}
	committed, err := store.LoadJournal(key)
	if err != nil || committed.ContainmentAuthority == nil || committed.ContainmentCgroup == nil {
		t.Fatalf("committed v2 fixture = %#v, error=%v", committed, err)
	}
	return handoff, committed.ContainmentAuthority.Clone(), committed.ContainmentCgroup.Clone()
}

func bindAppLinuxV2OwnerWithCallbacks(
	store *state.Store,
	key state.RunKey,
	callbacks execution.LinuxV2JournalCallbacks,
	session *fakeNativeGoalSession,
) error {
	if store == nil || session == nil || callbacks.Reserve == nil || callbacks.MarkCreateStarted == nil ||
		callbacks.BindLeaf == nil || callbacks.BindHelper == nil || callbacks.BindLauncher == nil ||
		callbacks.ArmLauncherGrant == nil || callbacks.RecordLauncherExit == nil || callbacks.BindGate == nil ||
		callbacks.PrepareHandoff == nil || callbacks.BindHandoffV2 == nil || callbacks.CommitHandoff == nil {
		return errors.New("App test runner lacks its exact Linux v2 owner callback chain")
	}
	loadReservation := func() (state.LinuxCgroupReservation, error) {
		journal, err := store.LoadJournal(key)
		if err != nil {
			return state.LinuxCgroupReservation{}, err
		}
		if journal.ContainmentCgroup == nil {
			return state.LinuxCgroupReservation{}, errors.New("Linux v2 test runner lost its reserved cgroup leaf")
		}
		return journal.ContainmentCgroup.Clone(), nil
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		return err
	}
	reservation := testAppLinuxV2Reservation()
	identityOffset := 0
	if key.RunID == "run-second" {
		identityOffset = 100
	}
	launchDigest := sha256.Sum256([]byte("launch\x00" + key.RunID))
	secretDigest := sha256.Sum256([]byte("secret\x00" + key.RunID))
	pipeDigest := sha256.Sum256([]byte("pipe\x00" + key.RunID))
	jobDigest := sha256.Sum256([]byte("job\x00" + key.RunID))
	reservation.LaunchToken = hex.EncodeToString(launchDigest[:authority.TokenBytes])
	reservation.Secret = hex.EncodeToString(secretDigest[:authority.SecretBytes])
	reservation.PipeToken = hex.EncodeToString(pipeDigest[:authority.TokenBytes])
	reservation.JobID = hex.EncodeToString(jobDigest[:authority.TokenBytes])
	reservation.LeafName = reservation.LaunchToken
	leaf := testAppLinuxV2Leaf(reservation)
	leaf.LeafInode += uint64(identityOffset)
	if err := callbacks.Reserve(reservation); err != nil {
		return fmt.Errorf("reserve fake released owner: %w", err)
	}
	if err := callbacks.MarkCreateStarted(reservation); err != nil {
		return fmt.Errorf("mark fake cgroup creation: %w", err)
	}
	reservation, err = loadReservation()
	if err != nil {
		return err
	}
	if err := callbacks.BindLeaf(reservation, leaf); err != nil {
		return fmt.Errorf("bind fake cgroup leaf: %w", err)
	}
	reservation, err = loadReservation()
	if err != nil {
		return err
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: 20 + identityOffset, Identity: fmt.Sprintf("helper:%d:start", 20+identityOffset)}
	if err := callbacks.BindHelper(reservation, helper); err != nil {
		return fmt.Errorf("bind fake cgroup helper: %w", err)
	}
	reservation, err = loadReservation()
	if err != nil {
		return err
	}
	launcher := authority.LinuxCgroupProcessIdentity{PID: 30 + identityOffset, Identity: fmt.Sprintf("launcher:%d:start", 30+identityOffset)}
	if err := callbacks.BindLauncher(reservation, launcher); err != nil {
		return fmt.Errorf("bind fake root launcher: %w", err)
	}
	reservation, err = loadReservation()
	if err != nil {
		return err
	}
	if err := callbacks.ArmLauncherGrant(reservation); err != nil {
		return fmt.Errorf("arm fake launcher grant: %w", err)
	}
	reservation, err = loadReservation()
	if err != nil {
		return err
	}
	if err := callbacks.RecordLauncherExit(reservation, testAppLinuxV2Exit(launcher, helper)); err != nil {
		return fmt.Errorf("record fake root launcher wait: %w", err)
	}
	reservation, err = loadReservation()
	if err != nil {
		return err
	}
	gate := authority.LinuxCgroupGateIdentity{
		Process: authority.LinuxCgroupProcessIdentity{PID: 40 + identityOffset, Identity: fmt.Sprintf("gate:%d:start", 40+identityOffset)},
		HostUID: reservation.OwnerUID, ObservedAt: time.Now().UTC(),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: *reservation.LeafIdentity,
		BornInLeaf: true, TrustedImage: true,
	}
	if err := callbacks.BindGate(reservation, gate); err != nil {
		return fmt.Errorf("bind fake trusted A gate: %w", err)
	}
	reservation, err = loadReservation()
	if err != nil {
		return err
	}
	handoff, err := linuxV2HandoffFromReservation(reservation)
	if err != nil {
		return err
	}
	if err := callbacks.PrepareHandoff(handoff); err != nil {
		return fmt.Errorf("prepare fake v2 handoff: %w", err)
	}
	if err := callbacks.BindHandoffV2(handoff, helper, *reservation.Mirror); err != nil {
		return fmt.Errorf("bind fake v2 handoff owners: %w", err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil || journal.ContainmentHandoff == nil {
		return errors.Join(errors.New("read back bound fake v2 handoff before commit"), err)
	}
	handoff = journal.ContainmentHandoff.Clone()
	if err := callbacks.CommitHandoff(handoff, gate.ObservedAt); err != nil {
		return fmt.Errorf("commit fake v2 handoff: %w", err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil || journal.ContainmentAuthority == nil || journal.ContainmentCgroup == nil || !journal.HasProcessDetails() {
		return errors.Join(errors.New("fake released owner did not commit its exact target identity"), err)
	}
	target := authority.LinuxCgroupProcessIdentity{PID: journal.PID, Identity: journal.ProcessIdentity}
	session.processPID, session.processIdentity = target.PID, target.Identity
	session.linuxV2Status = execution.LinuxV2LiveOwnerStatus{
		RunKey: key, Fence: journal.Fence(), Helper: *journal.ContainmentCgroup.Helper, Target: target,
		HelperPIDFDLive: true, TargetPIDFDLive: true, LeaseSequence: 1, LeaseLive: true,
		CommitAcknowledged: true, ReleaseAttempted: true, ReleaseSucceeded: true,
	}
	return nil
}

func appLinuxV2StopReceipt(t *testing.T, owner authority.Supervisor) authority.StopReceipt {
	t.Helper()
	proof := appLinuxV2StopProof(t, owner)
	receipt := linuxV2StopReceipt(owner, proof)
	if !receipt.ValidFor(owner) {
		t.Fatalf("v2 stop receipt fixture is invalid: %#v", receipt)
	}
	return receipt
}

func appLinuxV2StopProof(t *testing.T, owner authority.Supervisor) authority.LinuxCgroupStopProof {
	t.Helper()
	helper := authority.LinuxCgroupProcessIdentity{PID: owner.SupervisorPID, Identity: owner.SupervisorIdentity}
	gate := authority.LinuxCgroupProcessIdentity{PID: owner.TargetPID, Identity: owner.TargetIdentity}
	proof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
		Issuer: helper, Reaper: helper, PinnedLeafFD: true, CgroupKillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: *owner.LauncherExit,
		GateExit: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: gate, Witness: helper, Waited: true, Reaped: true,
		},
	}
	return proof
}

func appLinuxV2NoGateStopEvidence(
	t *testing.T,
	grant platform.LinuxOwnerAnchorGrant,
	reservation state.LinuxCgroupReservation,
	prepareAttempted bool,
) platform.LinuxOwnerHelperV2StopEvidence {
	t.Helper()
	if reservation.Helper == nil || reservation.LeafIdentity == nil {
		t.Fatal("no-gate helper evidence fixture lacks exact helper/leaf identities")
	}
	preGate := &platform.LinuxOwnerHelperV2PreGateStopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: *reservation.Helper,
		PrepareAttempted: prepareAttempted, NoProducerCanAct: true, PinnedLeafFD: true,
		CgroupKillAcknowledged: true, CgroupPopulatedZero: true, NoMigrationObserved: true,
		DescendantsReaped: true, DescendantDigest: strings.Repeat("0", 64),
	}
	if prepareAttempted {
		if reservation.Launcher == nil || reservation.Mirror == nil {
			t.Fatal("prepared no-gate evidence fixture lacks its exact launcher or mirror")
		}
		exit := testAppLinuxV2Exit(*reservation.Launcher, *reservation.Helper)
		authenticator, err := linuxgate.SignGateNeverProducedAuthenticator(reservation.Secret, reservation.LaunchToken, *reservation.Launcher)
		if err != nil {
			t.Fatal(err)
		}
		gateOutcome := authority.LinuxCgroupGateProductionResult{
			Version: authority.LinuxCgroupIdentityVersion, LaunchToken: reservation.LaunchToken,
			Launcher: *reservation.Launcher, Outcome: authority.LinuxCgroupGateOutcomeNeverCreated,
			Authenticator: authenticator,
		}
		proof := authority.LinuxCgroupPreGateStopProof{
			Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
			Issuer: *reservation.Helper, Reaper: *reservation.Helper, PinnedLeafFD: true,
			CgroupKillAcknowledged: true, CgroupPopulatedZero: true, NoMigrationObserved: true,
			ProducerFenced: true, DescendantsReaped: true, LauncherExit: exit, GateOutcome: gateOutcome,
		}
		receipt := authority.LinuxCgroupPreGateStopReceipt{
			Version: authority.LinuxCgroupIdentityVersion, LaunchToken: grant.LaunchToken,
			CgroupIdentity: grant.Leaf, OwnerUID: grant.OwnerUID, LauncherUID: grant.LauncherUID,
			Anchor: grant.Anchor, Helper: *reservation.Helper, Launcher: *reservation.Launcher,
			Mirror: *reservation.Mirror, Proof: proof,
		}
		if !receipt.ValidFor(
			grant.LaunchToken, grant.Leaf, grant.OwnerUID, grant.LauncherUID, grant.Anchor,
			*reservation.Helper, *reservation.Launcher, *reservation.Mirror, exit, nil,
		) {
			t.Fatal("no-gate helper Stop receipt fixture is invalid for its exact Run leaf and producer set")
		}
		launcher := *reservation.Launcher
		preGate.Launcher = &launcher
		preGate.LauncherExit = &exit
		preGate.GateOutcome = &gateOutcome
		preGate.Proof = &proof
	}
	fence := appLinuxV2AnchorProducerFence(
		t, grant, *reservation.Helper, prepareAttempted, preGate.Launcher, preGate.LauncherExit, nil, preGate.GateOutcome,
	)
	anchorStop := appLinuxV2AnchorStopEvidence(t, grant, *reservation.Helper, fence)
	preGate.AnchorStop = &anchorStop
	evidence := platform.LinuxOwnerHelperV2StopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: *reservation.Helper,
		PreGate: preGate, DescendantDigest: strings.Repeat("0", 64),
	}
	if err := evidence.ValidateFor(grant, *reservation.Helper, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}); err != nil {
		t.Fatalf("no-gate measured helper evidence is invalid: %v", err)
	}
	return evidence
}

func appLinuxV2GateStopEvidence(
	t *testing.T,
	grant platform.LinuxOwnerAnchorGrant,
	reservation state.LinuxCgroupReservation,
) platform.LinuxOwnerHelperV2StopEvidence {
	t.Helper()
	if reservation.Helper == nil || reservation.Launcher == nil || reservation.LeafIdentity == nil || reservation.Mirror == nil {
		t.Fatal("gate stop evidence fixture lacks exact helper/launcher/leaf identities")
	}
	launcherExit := testAppLinuxV2Exit(*reservation.Launcher, *reservation.Helper)
	gate := authority.LinuxCgroupGateIdentity{
		Process: authority.LinuxCgroupProcessIdentity{PID: 40, Identity: "gate:40:start"},
		HostUID: reservation.OwnerUID, ObservedAt: time.Date(2026, 9, 26, 6, 7, 8, 0, time.UTC),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: *reservation.LeafIdentity,
		BornInLeaf: true, TrustedImage: true,
	}
	temporary := reservation.Clone()
	temporary.LauncherExit = &launcherExit
	gateCopy := gate
	temporary.Gate = &gateCopy
	handoff, err := linuxV2HandoffFromReservation(temporary)
	if err != nil {
		t.Fatal(err)
	}
	handoff.SupervisorPID = reservation.Helper.PID
	handoff.SupervisorIdentity = reservation.Helper.Identity
	mirror := *reservation.Mirror
	handoff.Mirror = &mirror
	owner, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	fence := appLinuxV2AnchorProducerFence(t, grant, *reservation.Helper, true, reservation.Launcher, &launcherExit, &gate, nil)
	anchorStop := appLinuxV2AnchorStopEvidence(t, grant, *reservation.Helper, fence)
	evidence := platform.LinuxOwnerHelperV2StopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: *reservation.Helper,
		AnchorStop: &anchorStop, Launcher: *reservation.Launcher, Gate: gate, Proof: appLinuxV2StopProof(t, owner),
		DescendantDigest: strings.Repeat("0", 64),
	}
	if err := evidence.ValidateFor(grant, *reservation.Helper, *reservation.Launcher, gate); err != nil {
		t.Fatalf("gate measured helper evidence is invalid: %v", err)
	}
	return evidence
}

func appLinuxV2NoPrepareStopEvidence(
	t *testing.T,
	grant platform.LinuxOwnerAnchorGrant,
	reservation state.LinuxCgroupReservation,
) platform.LinuxOwnerHelperV2StopEvidence {
	t.Helper()
	if reservation.Helper == nil {
		t.Fatal("no-Prepare Stop evidence fixture lacks its exact helper identity")
	}
	preGate := &platform.LinuxOwnerHelperV2PreGateStopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: *reservation.Helper,
		NoProducerCanAct: true, PinnedLeafFD: true, CgroupKillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, DescendantsReaped: true,
		DescendantDigest: strings.Repeat("0", 64),
	}
	fence := appLinuxV2AnchorProducerFence(t, grant, *reservation.Helper, false, nil, nil, nil, nil)
	anchorStop := appLinuxV2AnchorStopEvidence(t, grant, *reservation.Helper, fence)
	preGate.AnchorStop = &anchorStop
	evidence := platform.LinuxOwnerHelperV2StopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: *reservation.Helper,
		PreGate: preGate, DescendantDigest: strings.Repeat("0", 64),
	}
	if err := evidence.ValidateFor(grant, *reservation.Helper, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}); err != nil {
		t.Fatalf("no-Prepare measured helper Stop evidence is invalid: %v", err)
	}
	return evidence
}

func appLinuxV2AnchorProducerFence(
	t *testing.T,
	grant platform.LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	prepareAttempted bool,
	launcher *authority.LinuxCgroupProcessIdentity,
	launcherExit *authority.LinuxCgroupExitProof,
	gate *authority.LinuxCgroupGateIdentity,
	gateOutcome *authority.LinuxCgroupGateProductionResult,
) platform.LinuxOwnerAnchorProducerFence {
	t.Helper()
	fence := platform.LinuxOwnerAnchorProducerFence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Helper: helper, StopLatched: true, LeaseStopped: true, PrepareAttempted: prepareAttempted,
		PrepareQuiesced: true, ReleaseQuiesced: true, LeafMembershipVerified: true,
	}
	if launcher != nil {
		copy := *launcher
		fence.Launcher = &copy
	}
	if launcherExit != nil {
		copy := launcherExit.Clone()
		fence.LauncherExit = &copy
	}
	if gate != nil {
		copy := *gate
		fence.Gate = &copy
		fence.UserNamespace = &platform.LinuxUserNamespaceIdentity{Device: 1, Inode: 1}
	}
	if gateOutcome != nil {
		copy := *gateOutcome
		fence.GateOutcome = &copy
	}
	if err := fence.ValidateFor(grant, helper); err != nil {
		t.Fatalf("anchor producer fence fixture is invalid: %v", err)
	}
	return fence
}

func appLinuxV2AnchorStopEvidence(
	t *testing.T,
	grant platform.LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	fence platform.LinuxOwnerAnchorProducerFence,
) platform.LinuxOwnerAnchorStopEvidence {
	t.Helper()
	fence = fence.Clone()
	stop := platform.LinuxOwnerAnchorStopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: helper,
		ProducerFence: &fence, PinnedLeafFD: true, KillAttempted: true, KillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true,
	}
	if fence.Launcher != nil {
		copy := *fence.Launcher
		stop.Launcher = &copy
	}
	if fence.Gate != nil {
		copy := *fence.Gate
		stop.Gate = &copy
	}
	if fence.UserNamespace != nil {
		copy := *fence.UserNamespace
		stop.UserNamespace = &copy
	}
	if fence.GateOutcome != nil {
		copy := *fence.GateOutcome
		stop.GateOutcome = &copy
	}
	if err := stop.ValidateFor(grant, helper, grant.Anchor, nil, false); err != nil {
		t.Fatalf("anchor StopLeaf evidence fixture is invalid: %v", err)
	}
	return stop
}

func appLinuxV2AnchorRunReapResult(
	t *testing.T,
	grant platform.LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	stop platform.LinuxOwnerAnchorStopEvidence,
) platform.LinuxOwnerAnchorRunReapResult {
	return appLinuxV2AnchorRunReapResultWithProcesses(t, grant, helper, stop, nil)
}

func appLinuxV2AnchorRunReapResultWithProcesses(
	t *testing.T,
	grant platform.LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	stop platform.LinuxOwnerAnchorStopEvidence,
	processes []platform.LinuxOwnerAnchorReapEvidence,
) platform.LinuxOwnerAnchorRunReapResult {
	t.Helper()
	encoded, err := json.Marshal(processes)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	stop = stop.Clone()
	helperExit := testAppLinuxV2Exit(helper, grant.Anchor)
	stop.HelperExit = &helperExit
	stop.DescendantsReaped = true
	stop.ReapCount = uint64(len(processes))
	stop.ReapDigest = hex.EncodeToString(digest[:])
	if err := stop.ValidateFor(grant, helper, grant.Anchor, processes, true); err != nil {
		t.Fatalf("anchor run-reap result fixture is invalid: %v", err)
	}
	return platform.LinuxOwnerAnchorRunReapResult{Processes: processes, Stop: stop}
}

func appLinuxV2AnchorReapEvidence(
	grant platform.LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	process authority.LinuxCgroupProcessIdentity,
	role string,
	helperExit authority.LinuxCgroupExitProof,
) platform.LinuxOwnerAnchorReapEvidence {
	helperExitCopy := helperExit.Clone()
	return platform.LinuxOwnerAnchorReapEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Helper: helper, Process: process, ProcessRole: role,
		HelperExit: helperExit, ProcessExit: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: process, Witness: grant.Anchor,
			Waited: true, Reaped: true, HelperExit: &helperExitCopy,
		},
	}
}

type appNoPrepareRecoveryControl struct {
	stopEvidence  platform.LinuxOwnerHelperV2StopEvidence
	fenceEvidence platform.LinuxOwnerHelperV2PreGateStopEvidence
	stopCalls     int
	fenceCalls    int
	stopErr       error
	fenceErr      error
}

func (control *appNoPrepareRecoveryControl) Stop(context.Context) (platform.LinuxOwnerHelperV2StopEvidence, error) {
	control.stopCalls++
	return control.stopEvidence, control.stopErr
}

func (control *appNoPrepareRecoveryControl) FenceBeforeAbort(context.Context) (platform.LinuxOwnerHelperV2PreGateStopEvidence, error) {
	control.fenceCalls++
	return control.fenceEvidence, control.fenceErr
}

type appNoPrepareRecoveryWaiter struct {
	result platform.LinuxOwnerAnchorRunReapResult
	calls  int
	err    error
}

func (waiter *appNoPrepareRecoveryWaiter) WaitReapRun(_ context.Context, grant platform.LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) (platform.LinuxOwnerAnchorRunReapResult, error) {
	waiter.calls++
	if err := grant.Validate(); err != nil || waiter.result.Stop.ValidateFor(
		grant, helper, grant.Anchor, waiter.result.Processes, true,
	) != nil {
		return platform.LinuxOwnerAnchorRunReapResult{}, errors.New("test anchor run-reap proof differs from the exact helper grant")
	}
	return waiter.result, waiter.err
}

func testAppLinuxV2Leaf(reservation state.LinuxCgroupReservation) authority.LinuxCgroupIdentity {
	return authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: reservation.Parent, LeafName: reservation.LeafName,
		LeafDevice: 168, LeafInode: 210,
	}
}

func testAppLinuxV2Helper() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 20, Identity: "helper:20:start"}
}

func testAppLinuxV2Launcher() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 30, Identity: "launcher:30:start"}
}

func testAppLinuxV2Exit(process, witness authority.LinuxCgroupProcessIdentity) authority.LinuxCgroupExitProof {
	return authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: process, Witness: witness, Waited: true, Reaped: true,
	}
}

func loadAppLinuxV2Reservation(t *testing.T, store *state.Store, key state.RunKey) state.LinuxCgroupReservation {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil || journal.ContainmentCgroup == nil {
		t.Fatalf("load Linux v2 reservation: %#v, error=%v", journal.ContainmentCgroup, err)
	}
	return journal.ContainmentCgroup.Clone()
}

type linuxCloneWorkspaceCallbackFixture struct {
	*fakeWorkspace
}

type linuxClonePromotionProbeWorkspace struct {
	*linuxCloneWorkspaceCallbackFixture
	promoteCalls int
	readCalls    int
}

type linuxCloneSeedTempRecoveryWorkspace struct {
	*linuxCloneWorkspaceCallbackFixture
	store       *state.Store
	key         state.RunKey
	intent      authority.LinuxCloneIntent
	tempName    string
	tempPresent bool
	abortCalls  int
}

func (service *linuxCloneSeedTempRecoveryWorkspace) AbortLinuxCloneBeforeCheckout(
	ctx context.Context,
	intent authority.LinuxCloneIntent,
	seed *authority.LinuxCloneSeedReceipt,
	preGate *authority.LinuxCgroupPreGateReleaseProof,
	cgroupAbort *authority.LinuxCgroupReservationAbortProof,
	normalRelease *authority.SupervisorHandoffReleaseProof,
) (authority.LinuxCloneAbortProof, error) {
	if service == nil || ctx == nil || service.store == nil || service.key != (state.RunKey{RunID: intent.RunID, Generation: intent.Generation}) ||
		!intent.Equal(service.intent) || seed != nil || preGate != nil || cgroupAbort != nil || normalRelease != nil ||
		service.tempName != ".clone-seed-"+intent.Nonce {
		return authority.LinuxCloneAbortProof{}, errors.New("seed-temp recovery differs from its exact run intent")
	}
	journal, err := service.store.LoadJournal(service.key)
	if err != nil || !journal.LinuxCloneAbortPending || journal.LinuxCloneIntent == nil ||
		!journal.LinuxCloneIntent.Equal(intent) || journal.LinuxCloneSeedReceipt != nil ||
		journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionIntent != nil ||
		journal.LinuxClonePromotionReceipt != nil || journal.WorkspacePath != intent.Target || !journal.WorkspaceRecoveryRequired {
		return authority.LinuxCloneAbortProof{}, errors.Join(errors.New("Workspace observed no exact durable seed-producer fence"), err)
	}
	service.abortCalls++
	if service.tempPresent {
		service.tempPresent = false
		return authority.LinuxCloneAbortProof{}, errors.New("injected Workspace crash after exact orphan-temp removal")
	}
	return service.linuxCloneWorkspaceCallbackFixture.fakeWorkspace.AbortLinuxCloneBeforeCheckout(
		ctx, intent, seed, preGate, cgroupAbort, normalRelease,
	)
}

func (service *linuxClonePromotionProbeWorkspace) PromoteLinuxClone(
	context.Context,
	authority.LinuxCloneGrant,
	authority.LinuxCloneCheckoutReceipt,
	authority.LinuxClonePromotionIntent,
	platform.LinuxCgroupV2Release,
) (authority.LinuxClonePromotionReceipt, error) {
	service.promoteCalls++
	return authority.LinuxClonePromotionReceipt{}, errors.New("unexpected Workspace promotion after unissued release")
}

func (service *linuxClonePromotionProbeWorkspace) ReadExistingLinuxClonePromotion(context.Context, authority.LinuxCloneIntent, authority.LinuxClonePromotionIntent, state.RunJournal, platform.LinuxCgroupV2Release) (authority.LinuxClonePromotionReceipt, error) {
	service.readCalls++
	return authority.LinuxClonePromotionReceipt{}, errors.New("unexpected Workspace readback without release evidence")
}

func (*linuxCloneWorkspaceCallbackFixture) PlanLinuxCloneIntent(string, workspace.RunRef, protocol.Subject) (authority.LinuxCloneIntent, error) {
	return authority.LinuxCloneIntent{}, errors.New("unexpected clone plan in callback fixture")
}

func (*linuxCloneWorkspaceCallbackFixture) PrepareLinuxCloneSeed(context.Context, authority.LinuxCloneIntent) (authority.LinuxCloneSeedReceipt, error) {
	return authority.LinuxCloneSeedReceipt{}, errors.New("unexpected clone seed in callback fixture")
}

func (*linuxCloneWorkspaceCallbackFixture) PrepareLinuxClone(context.Context, authority.LinuxCloneGrant) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("unexpected clone prepare in callback fixture")
}

func (*linuxCloneWorkspaceCallbackFixture) ValidateLinuxCloneCheckout(authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt) error {
	return nil
}

func (*linuxCloneWorkspaceCallbackFixture) PromoteLinuxClone(context.Context, authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt, authority.LinuxClonePromotionIntent, platform.LinuxCgroupV2Release) (authority.LinuxClonePromotionReceipt, error) {
	return authority.LinuxClonePromotionReceipt{}, errors.New("unexpected clone promotion in callback fixture")
}

func (*linuxCloneWorkspaceCallbackFixture) ReadExistingLinuxClonePromotion(context.Context, authority.LinuxCloneIntent, authority.LinuxClonePromotionIntent, state.RunJournal, platform.LinuxCgroupV2Release) (authority.LinuxClonePromotionReceipt, error) {
	return authority.LinuxClonePromotionReceipt{}, errors.New("unexpected clone promotion replay in callback fixture")
}

func (*linuxCloneWorkspaceCallbackFixture) RemoveLinuxCloneTarget(context.Context, authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt, state.RunJournal, workspace.LinuxCloneTargetDeleter) (authority.LinuxCloneTargetRemovalProof, error) {
	return authority.LinuxCloneTargetRemovalProof{}, errors.New("unexpected target removal in callback fixture")
}

func (*linuxCloneWorkspaceCallbackFixture) CleanupLinuxClone(context.Context, authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt, state.RunJournal) (authority.LinuxCloneCleanupProof, error) {
	return authority.LinuxCloneCleanupProof{}, errors.New("unexpected clone cleanup in callback fixture")
}

type linuxCloneCleanupCallsWorkspace struct {
	*linuxCloneWorkspaceCallbackFixture
	cleanupCalls int
}

func (service *linuxCloneCleanupCallsWorkspace) CleanupLinuxClone(
	context.Context,
	authority.LinuxCloneGrant,
	authority.LinuxCloneCheckoutReceipt,
	state.RunJournal,
) (authority.LinuxCloneCleanupProof, error) {
	service.cleanupCalls++
	return authority.LinuxCloneCleanupProof{}, errors.New("unexpected Workspace cleanup before complete release proof")
}

type linuxCloneAbortProbeWorkspace struct {
	*linuxCloneWorkspaceCallbackFixture
	abortCalls   int
	removeCalls  int
	cleanupCalls int
}

func (service *linuxCloneAbortProbeWorkspace) AbortLinuxCloneBeforeCheckout(
	context.Context,
	authority.LinuxCloneIntent,
	*authority.LinuxCloneSeedReceipt,
	*authority.LinuxCgroupPreGateReleaseProof,
	*authority.LinuxCgroupReservationAbortProof,
	*authority.SupervisorHandoffReleaseProof,
) (authority.LinuxCloneAbortProof, error) {
	service.abortCalls++
	return authority.LinuxCloneAbortProof{}, errors.New("unexpected Workspace abort after normal v2 release")
}

func (service *linuxCloneAbortProbeWorkspace) RemoveLinuxCloneTarget(
	context.Context,
	authority.LinuxCloneGrant,
	authority.LinuxCloneCheckoutReceipt,
	state.RunJournal,
	workspace.LinuxCloneTargetDeleter,
) (authority.LinuxCloneTargetRemovalProof, error) {
	service.removeCalls++
	return authority.LinuxCloneTargetRemovalProof{}, errors.New("unexpected target removal without a CheckoutReady receipt")
}

func (service *linuxCloneAbortProbeWorkspace) CleanupLinuxClone(
	context.Context,
	authority.LinuxCloneGrant,
	authority.LinuxCloneCheckoutReceipt,
	state.RunJournal,
) (authority.LinuxCloneCleanupProof, error) {
	service.cleanupCalls++
	return authority.LinuxCloneCleanupProof{}, errors.New("unexpected A cleanup without a durable B target proof")
}

func (*linuxCloneWorkspaceCallbackFixture) AbortLinuxCloneBeforeCheckout(context.Context, authority.LinuxCloneIntent, *authority.LinuxCloneSeedReceipt, *authority.LinuxCgroupPreGateReleaseProof, *authority.LinuxCgroupReservationAbortProof, *authority.SupervisorHandoffReleaseProof) (authority.LinuxCloneAbortProof, error) {
	return authority.LinuxCloneAbortProof{}, errors.New("unexpected no-checkout abort in callback fixture")
}

func linuxCloneCallbackFixture(t *testing.T) (*state.Store, state.RunKey, protocol.Admission, authority.LinuxCloneGrant, protocol.Fence) {
	return linuxCloneCallbackFixtureAt(t, t.TempDir())
}

func linuxCloneCallbackFixtureAt(t *testing.T, directory string) (*state.Store, state.RunKey, protocol.Admission, authority.LinuxCloneGrant, protocol.Fence) {
	return linuxCloneCallbackFixtureWithSeedAt(t, directory, true)
}

func linuxCloneIntentOnlyFixtureAt(t *testing.T, directory string) (*state.Store, state.RunKey, protocol.Admission, authority.LinuxCloneGrant, protocol.Fence) {
	return linuxCloneCallbackFixtureWithSeedAt(t, directory, false)
}

func linuxCloneCallbackFixtureWithSeedAt(t *testing.T, directory string, bindSeed bool) (*state.Store, state.RunKey, protocol.Admission, authority.LinuxCloneGrant, protocol.Fence) {
	t.Helper()
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse clone admission: present=%t error=%v", present, err)
	}
	admission.SessionMode = protocol.SessionModeFresh
	input := validAdmissionInput()
	work := protocol.Work{Goal: "g", AgentProfile: "local", Workspace: "local", Input: input}
	store, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1,
		ClaimID: "claim-1", LocalState: "claiming", Work: work, WorkspaceBindingKey: "local",
	}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease-1",
		LeaseExpiresAt: time.Now().Add(time.Minute), Work: work,
	}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(key, "running"); err != nil {
		store.Close()
		t.Fatal(err)
	}
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: key.RunID, Generation: key.Generation,
		BindingKey: "local", ResourceID: admission.Subject.ResourceID, BaseCommit: admission.Subject.Commit,
		BaseTreeDigest: admission.Subject.TreeDigest, Nonce: strings.Repeat("a", authority.LinuxCloneNonceBytes*2),
		SeedPath: "/var/lib/symmetry/seeds/run-1.bundle",
		Target:   "/home/native-b/workspaces/binding-local/run-run-1/generation-1",
	}
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: admission.Subject.Commit,
		Digest: "sha256:" + strings.Repeat("c", 64), Size: 128, Device: 1, Inode: 2,
	}
	grant := authority.LinuxCloneGrant{Intent: intent, Seed: seed}
	if err := grant.Validate(); err != nil {
		store.Close()
		t.Fatalf("validate clone fixture grant: %v", err)
	}
	fence := protocol.Fence{RuntimeID: "runtime-1", RuntimeEpoch: 1, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease-1"}
	callbacks := linuxV2AppTestDaemon(t, store, key, false).linuxCloneJournalCallbacks(key, fence)
	if err := callbacks.Reserve(intent); err != nil {
		store.Close()
		t.Fatalf("reserve clone fixture intent: %v", err)
	}
	if bindSeed {
		if err := callbacks.BindSeed(intent, seed); err != nil {
			store.Close()
			t.Fatalf("bind clone fixture seed: %v", err)
		}
	}
	return store, key, admission, grant, fence
}

func TestLinuxCloneNoTargetRetentionClearsOnlyWithConclusiveSessionOwnership(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		owner           bool
		corrupt         bool
		lostClearAck    bool
		wantRetain      bool
		wantErr         bool
		wantClearWrites int
	}{
		{name: "no owner", wantRetain: false},
		{name: "no owner with post-rename clear acknowledgement loss", lostClearAck: true, wantRetain: false, wantClearWrites: 2},
		{name: "retained GoalSession", owner: true, wantRetain: true, wantErr: true},
		{name: "unknown GoalSession state", owner: true, corrupt: true, wantRetain: true, wantErr: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store, key, admission, grant, fence := linuxCloneCallbackFixture(t)
			defer store.Close()
			if _, err := store.RetainWorkspace(key); err != nil {
				t.Fatalf("retain clone fixture before abort: %v", err)
			}
			if _, err := store.FenceLinuxCloneBeforeCheckout(key, fence, grant.Intent); err != nil {
				t.Fatalf("fence exact clone fixture: %v", err)
			}
			baseRef, err := authority.LinuxCloneBaseRef(key.RunID, key.Generation)
			if err != nil {
				t.Fatal(err)
			}
			promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
			if err != nil {
				t.Fatal(err)
			}
			seed := grant.Seed.Clone()
			proof := authority.LinuxCloneAbortProof{
				Version: authority.LinuxCloneVersion, Intent: grant.Intent.Clone(), SeedReceipt: &seed,
				BaseRef: baseRef, PromotionRef: promotionRef,
				TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
			}
			if err := proof.Validate(); err != nil {
				t.Fatalf("validate exact no-target abort fixture: %v", err)
			}
			if _, err := store.CompleteLinuxCloneAbort(key, fence, grant.Intent, proof); err != nil {
				t.Fatalf("persist exact no-target abort proof: %v", err)
			}
			if scenario.owner {
				saveLinuxCloneAbortGoalSession(t, store, key, admission, fence, grant, scenario.corrupt)
			}
			clearWrites := 0
			restoreWriter := func() {}
			if scenario.lostClearAck {
				restoreWriter = store.SetAtomicWriterForTesting(func(path string, data []byte) error {
					var candidate state.RunJournal
					if err := json.Unmarshal(data, &candidate); err == nil && candidate.Key() == key &&
						candidate.LinuxCloneAbortProof != nil && candidate.LinuxCloneAbortProof.Equal(proof) && !candidate.RetainWorkspace {
						clearWrites++
					}
					if err := writeAppStateAtomic(path, data); err != nil {
						return err
					}
					if scenario.lostClearAck && clearWrites == 1 {
						return errors.New("injected post-rename no-target retention acknowledgement loss")
					}
					return nil
				})
			}
			app := &daemon{config: testConfig(t), store: store}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			cleanupErr := app.cleanupRecoveredWorkspace(context.Background(), journal, false)
			restoreWriter()
			if (cleanupErr != nil) != scenario.wantErr {
				t.Fatalf("cleanupRecoveredWorkspace() error = %v, wantErr=%t", cleanupErr, scenario.wantErr)
			}
			after, err := store.LoadJournal(key)
			if err != nil || after.LinuxCloneAbortProof == nil || !after.LinuxCloneAbortProof.Equal(proof) ||
				after.RetainWorkspace != scenario.wantRetain {
				t.Fatalf("abort retention after cleanup = %#v, read error=%v; RetainWorkspace want %t", after, err, scenario.wantRetain)
			}
			if clearWrites != scenario.wantClearWrites {
				t.Fatalf("no-target retention CAS writes = %d, want %d", clearWrites, scenario.wantClearWrites)
			}
		})
	}
}

func saveLinuxCloneAbortGoalSession(
	t *testing.T,
	store *state.Store,
	key state.RunKey,
	admission protocol.Admission,
	fence protocol.Fence,
	grant authority.LinuxCloneGrant,
	corrupt bool,
) {
	t.Helper()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	intent := state.GoalSessionLaunchIntent{
		LaunchIntentID: "00000000-0000-4000-8000-000000000008", GoalID: admission.GoalID, GoalRevision: admission.GoalRevision,
		WorkItemID: admissionWorkItemIDValue(admission.WorkItemID), TaskID: "task-1", RunID: key.RunID,
		Generation: key.Generation, AdmissionID: admission.AdmissionID, LocalHandleID: sessionKey.LocalHandleID,
		RuntimeID: fence.RuntimeID, RuntimeEpoch: fence.RuntimeEpoch, HarnessKind: "pi", HarnessVersion: "1.2.0",
		AdapterVersion: "symmetry-adapter-test", AdapterProtocolVersion: 1,
		WorkspaceFingerprint: "sha256:" + strings.Repeat("a", 64), WorkspacePath: grant.Intent.Target,
		WorkspaceOwnerRunKey: state.WorkspaceOwnerRunKey{RunID: key.RunID, Generation: key.Generation},
		RepositoryResourceID: grant.Intent.ResourceID, SessionMode: state.GoalSessionModeFresh,
	}
	var sessionPath string
	if corrupt {
		restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
			if strings.HasPrefix(filepath.Base(path), "session-") {
				sessionPath = path
			}
			return writeAppStateAtomic(path, data)
		})
		if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
			restore()
			t.Fatalf("save GoalSession before corrupting ownership read: %v", err)
		}
		restore()
		if sessionPath == "" {
			t.Fatal("GoalSession save did not expose its exact persisted journal path")
		}
		if err := os.WriteFile(sessionPath, []byte("{"), 0o600); err != nil {
			t.Fatalf("corrupt exact GoalSession journal to exercise unknown-owner fail-closed path: %v", err)
		}
		return
	}
	if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
		t.Fatalf("save retained GoalSession owner: %v", err)
	}
}
