//go:build linux

package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestLegacyJournalDecodeDoesNotInventCloneEvidence(t *testing.T) {
	t.Parallel()
	legacy := testJournal("legacy-no-clone", 1)
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RunJournal
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode legacy journal: %v", err)
	}
	if err := validateJournal(decoded); err != nil {
		t.Fatalf("validate legacy journal: %v", err)
	}
	if decoded.LinuxCloneIntent != nil || decoded.LinuxCloneSeedReceipt != nil ||
		decoded.LinuxCloneCheckoutReceipt != nil || decoded.LinuxClonePromotionIntent != nil || decoded.LinuxClonePromotionReceipt != nil {
		t.Fatalf("legacy journal decode fabricated Linux clone evidence: %#v", decoded)
	}
}

func TestLegacyPromotedLinuxCloneJournalRestartsWithoutInventingIntent(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	for _, test := range []struct {
		name       string
		legacyPath bool
	}{
		{name: "pre-RunRef target layout", legacyPath: true},
		{name: "current target layout", legacyPath: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			store, err := New(directory)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			runID := "clone-legacy-current-target"
			if test.legacyPath {
				runID = "clone-legacy-old-target"
			}
			journal, intent, promotion, candidate := legacyPromotedLinuxCloneFixture(t, runID, test.legacyPath)
			encoded, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.journalPath(journal.Key()), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}

			restarted, err := New(directory)
			if err != nil {
				t.Fatalf("restart with pre-change promoted clone journal: %v", err)
			}
			t.Cleanup(func() { _ = restarted.Close() })
			loaded, err := restarted.LoadJournal(journal.Key())
			if err != nil {
				t.Fatalf("read pre-change promoted clone journal: %v", err)
			}
			if loaded.LinuxClonePromotionIntent != nil || loaded.LinuxClonePromotionReceipt == nil ||
				!loaded.LinuxClonePromotionReceipt.Equal(promotion) || loaded.LinuxCloneIntent == nil ||
				!loaded.LinuxCloneIntent.Equal(intent) || loaded.ContainmentCgroupReleaseProof == nil ||
				!loaded.ContainmentCgroupReleaseProof.Equal(*journal.ContainmentCgroupReleaseProof) {
				t.Fatalf("legacy promotion read fabricated or lost durable evidence: %#v", loaded)
			}
			if _, err := restarted.ReserveLinuxClonePromotion(journal.Key(), journal.Fence(), intent, candidate); err == nil {
				t.Fatal("ReserveLinuxClonePromotion() synthesized a pre-effect intent after a durable legacy receipt")
			}
			after, err := restarted.LoadJournal(journal.Key())
			if err != nil {
				t.Fatal(err)
			}
			if after.LinuxClonePromotionIntent != nil || after.LinuxClonePromotionReceipt == nil ||
				!after.LinuxClonePromotionReceipt.Equal(promotion) || after.ContainmentCgroupReleaseProof == nil ||
				!after.ContainmentCgroupReleaseProof.Equal(*journal.ContainmentCgroupReleaseProof) {
				t.Fatalf("rejected legacy promotion mutation changed stop/promotion evidence: %#v", after)
			}
		})
	}
}

func TestSaveJournalCannotClearLinuxCloneRetentionFromStaleSnapshot(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-retain-stale-snapshot")
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, intent, nil); err != nil {
		t.Fatal(err)
	}
	staleSnapshot := loadStateJournal(t, store, key)
	if staleSnapshot.RetainWorkspace {
		t.Fatal("fixture unexpectedly started with clone retention")
	}
	if retained, err := store.RetainWorkspace(key); err != nil || !retained.RetainWorkspace {
		t.Fatalf("RetainWorkspace() = %#v, %v", retained, err)
	}
	if err := store.SaveJournal(staleSnapshot); err == nil {
		t.Fatal("SaveJournal() cleared Linux clone retention from a stale snapshot")
	}
	loaded := loadStateJournal(t, store, key)
	if !loaded.RetainWorkspace {
		t.Fatal("rejected stale SaveJournal() lost the durable Linux clone retention barrier")
	}
}

func TestLinuxCloneAbortFencesLaunchAndRetiresOnlyAfterAbsenceProof(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-abort-run")
	journal.LeaseExpiresAt = time.Now().Add(time.Hour)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, intent, nil); err != nil {
		t.Fatal(err)
	}
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("a", 64), Size: 1024, Device: 5, Inode: 13,
	}
	if _, err := store.BindLinuxCloneSeed(key, fence, intent, seed); err != nil {
		t.Fatal(err)
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.FenceLinuxCloneBeforeCheckout(key, fence, intent)
		return err
	})
	fenced := loadStateJournal(t, store, key)
	if !fenced.LinuxCloneAbortPending || fenced.LinuxCloneAbortProof != nil {
		t.Fatalf("abort fence state = %#v", fenced)
	}
	if _, err := store.ReserveLinuxCgroup(key, fence, testStateLinuxCgroupV2Reservation(), nil); err == nil {
		t.Fatal("Linux cgroup launch reservation succeeded after clone abort fence")
	}
	if _, err := store.SetProcessDetails(key, 71, "late-clone-runner", time.Now()); err == nil {
		t.Fatal("process registration succeeded after clone abort fence")
	}
	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target, Nonce: intent.Nonce,
		SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit, TargetDevice: 17, TargetInode: 19,
	}
	if _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, checkout); err == nil {
		t.Fatal("checkout receipt was accepted after the durable abort fence")
	}
	if _, err := store.FenceLinuxCloneBeforeCheckout(key, fence, intent); err != nil {
		t.Fatalf("same abort fence replay failed: %v", err)
	}

	proof := pinnedLinuxCloneAbortProof(t, intent, &seed)
	invalidProof := proof
	invalidProof.TargetAbsent = false
	if _, err := store.CompleteLinuxCloneAbort(key, fence, intent, invalidProof); err == nil {
		t.Fatal("clone abort accepted an unverified target path")
	}
	if _, err := store.RetainWorkspace(key); err != nil {
		t.Fatal(err)
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.CompleteLinuxCloneAbort(key, fence, intent, proof)
		return err
	})
	completed := loadStateJournal(t, store, key)
	if completed.LinuxCloneAbortPending || completed.LinuxCloneAbortProof == nil ||
		!completed.LinuxCloneAbortProof.Equal(proof) || completed.WorkspacePath != "" || completed.WorkspaceRecoveryRequired ||
		!completed.RetainWorkspace || completed.LinuxCloneCheckoutReceipt != nil || completed.LinuxClonePromotionReceipt != nil ||
		completed.ContainmentAuthority != nil || completed.ContainmentHandoff != nil || completed.HasPendingContainment() {
		t.Fatalf("completed pre-checkout abort state fabricated run evidence or lost retention: %#v", completed)
	}
	if completed.LinuxCloneCleanupEligible(key, fence) {
		t.Fatal("abort tombstone permitted cleanup before terminal retirement")
	}

	if _, err := store.SetLocalState(key, "stale"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() ignored an unexpired stale lease")
	}
	terminalAt := time.Now().UTC()
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{
		TransitionID: "clone-abort-failed", State: "failed", Payload: json.RawMessage(`{}`),
	}, terminalAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, terminalAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.CompleteLinuxCloneAbort(key, fence, intent, proof)
		return err
	})
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() cleared a no-target abort while workspace retention remained set")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.ClearLinuxCloneNoTargetRetention(key, fence, proof)
		return err
	})
	clearedRetention := loadStateJournal(t, store, key)
	if clearedRetention.RetainWorkspace {
		t.Fatal("ClearLinuxCloneNoTargetRetention() left the exact no-target retention barrier set")
	}
	if err := store.DeleteJournal(key); err != nil {
		t.Fatalf("DeleteJournal() rejected a completed no-target abort after terminal cleanup gates: %v", err)
	}
}

func TestAbortNeverCreatedLinuxCgroupReservationUsesOnlyUnstartedFence(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := stoppedTestJournal("clone-test-never-created", 1)
	journal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	reservation := testStateLinuxCgroupV2Reservation()
	if _, err := store.ReserveLinuxCgroup(journal.Key(), journal.Fence(), reservation, nil); err != nil {
		t.Fatal(err)
	}
	fence := journal.Fence()
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.AbortNeverCreatedLinuxCgroupReservation(journal.Key(), fence, reservation)
		return err
	})
	cleared := loadStateJournal(t, store, journal.Key())
	if cleared.ContainmentCgroup == nil || cleared.ContainmentCgroup.AbortProof == nil ||
		cleared.ContainmentCgroup.AbortProof.Validate() != nil || cleared.ContainmentCgroup.AbortProof.CreateStarted ||
		cleared.ContainmentCgroup.AbortProof.LeafDisposition != authority.LinuxCgroupAbortLeafNeverCreated ||
		cleared.ContainmentCgroup.AbortProof.LeafNeverAppeared || cleared.ContainmentCgroupPreGateReleaseProof != nil ||
		cleared.ContainmentAuthority != nil || cleared.ContainmentHandoff != nil || cleared.HasPendingContainment() {
		t.Fatalf("never-created tombstone fabricated stop/release authority or remained pending: %#v", cleared)
	}
	staleFence := fence
	staleFence.LeaseToken = "stale-lease"
	if _, err := store.AbortNeverCreatedLinuxCgroupReservation(journal.Key(), staleFence, reservation); err == nil {
		t.Fatal("AbortNeverCreatedLinuxCgroupReservation() accepted a stale fence")
	}
	other := stoppedTestJournal("clone-test-after-never-created", 1)
	other.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(other); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckLinuxV2NativeAdmission(other.Key(), other.Fence(), time.Now().UTC(), nil); err != nil {
		t.Fatalf("exact no-target cgroup abort proof did not release admission: %v", err)
	}
	if err := store.DeleteJournal(journal.Key()); err != nil {
		t.Fatalf("DeleteJournal() after exact no-target abort: %v", err)
	}

	startedStore := mustStore(t)
	startedJournal := stoppedTestJournal("clone-test-create-started", 1)
	startedJournal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := startedStore.SaveJournal(startedJournal); err != nil {
		t.Fatal(err)
	}
	startedReservation := testStateLinuxCgroupV2Reservation()
	startedReservation.LeafName = strings.Repeat("7", authority.TokenBytes*2)
	if _, err := startedStore.ReserveLinuxCgroup(startedJournal.Key(), startedJournal.Fence(), startedReservation, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := startedStore.MarkLinuxCgroupCreateStarted(startedJournal.Key(), startedJournal.Fence(), startedReservation, nil); err != nil {
		t.Fatal(err)
	}
	startedReservation = loadStateJournal(t, startedStore, startedJournal.Key()).ContainmentCgroup.Clone()
	if _, err := startedStore.AbortNeverCreatedLinuxCgroupReservation(startedJournal.Key(), startedJournal.Fence(), startedReservation); err == nil {
		t.Fatal("AbortNeverCreatedLinuxCgroupReservation() accepted CreateStarted=true")
	}
}

func TestLinuxCloneAbortAcceptsOnlyExactCgroupNoTargetTombstones(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	for _, createStarted := range []bool{false, true} {
		name := "before-create"
		if createStarted {
			name = "removed-empty-leaf"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := mustStore(t)
			journal, intent := newLinuxCloneJournal(t, "clone-cgroup-abort-"+name)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ReserveLinuxCloneIntent(journal.Key(), journal.Fence(), intent, nil); err != nil {
				t.Fatal(err)
			}
			reservation := testStateLinuxCgroupV2Reservation()
			if _, err := store.ReserveLinuxCgroup(journal.Key(), journal.Fence(), reservation, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := store.FenceLinuxCloneBeforeCheckout(journal.Key(), journal.Fence(), intent); err == nil {
				t.Fatal("clone abort fence accepted an unresolved cgroup reservation")
			}
			if createStarted {
				if _, err := store.MarkLinuxCgroupCreateStarted(journal.Key(), journal.Fence(), reservation, nil); err != nil {
					t.Fatal(err)
				}
				reservation = loadStateJournal(t, store, journal.Key()).ContainmentCgroup.Clone()
				if _, err := store.BindLinuxCgroupLeaf(journal.Key(), reservation, testStateLinuxCgroupV2Leaf(reservation)); err != nil {
					t.Fatal(err)
				}
				reservation = loadStateJournal(t, store, journal.Key()).ContainmentCgroup.Clone()
				forged := testStateLinuxCgroupV2RemovedLeafAbortProof(reservation, authority.LinuxCgroupAbortLeafRemovedEmpty)
				forged.LeafEmpty = false
				if _, err := store.AbortLinuxCgroupReservation(journal.Key(), reservation, forged); err == nil {
					t.Fatal("cgroup abort accepted an empty-leaf claim without exact removal proof")
				}
				proof := testStateLinuxCgroupV2RemovedLeafAbortProof(reservation, authority.LinuxCgroupAbortLeafRemovedEmpty)
				if _, err := store.AbortLinuxCgroupReservation(journal.Key(), reservation, proof); err != nil {
					t.Fatalf("record exact empty-leaf abort: %v", err)
				}
			} else {
				if _, err := store.AbortNeverCreatedLinuxCgroupReservation(journal.Key(), journal.Fence(), reservation); err != nil {
					t.Fatalf("record never-created cgroup abort: %v", err)
				}
			}
			aborted := loadStateJournal(t, store, journal.Key())
			cgroupProof := aborted.ContainmentCgroup.AbortProof.Clone()
			if _, err := store.FenceLinuxCloneBeforeCheckout(journal.Key(), journal.Fence(), intent); err != nil {
				t.Fatalf("fence clone after cgroup no-target proof: %v", err)
			}
			baseRef, err := authority.LinuxCloneBaseRef(journal.RunID, journal.Generation)
			if err != nil {
				t.Fatal(err)
			}
			promotionRef, err := authority.LinuxClonePromotionRef(journal.RunID, journal.Generation)
			if err != nil {
				t.Fatal(err)
			}
			proof := authority.LinuxCloneAbortProof{
				Version: authority.LinuxCloneVersion, Intent: intent, CgroupAbortProof: &cgroupProof,
				BaseRef: baseRef, PromotionRef: promotionRef,
				TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
			}
			if _, err := store.CompleteLinuxCloneAbort(journal.Key(), journal.Fence(), intent, proof); err != nil {
				t.Fatalf("complete clone abort after cgroup no-target proof: %v", err)
			}
			completed := loadStateJournal(t, store, journal.Key())
			if completed.LinuxCloneAbortProof == nil || completed.LinuxCloneAbortProof.CgroupAbortProof == nil ||
				!completed.LinuxCloneAbortProof.CgroupAbortProof.Equal(cgroupProof) || completed.HasPendingContainment() ||
				completed.ContainmentAuthority != nil || completed.ContainmentHandoff != nil || completed.HasProcessDetails() {
				t.Fatalf("clone abort lost exact cgroup no-target tombstone: %#v", completed)
			}
		})
	}
}

func TestLinuxCloneAbortCannotUseNormalReleaseAsNoTargetProof(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-normal-release-abort")
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveLinuxCloneIntent(journal.Key(), journal.Fence(), intent, nil); err != nil {
		t.Fatal(err)
	}
	handoff, reservation := bindStateLinuxCgroupV2Handoff(t, store, journal.Key())
	startedAt := reservation.Gate.ObservedAt
	if _, err := store.CommitSupervisorHandoff(journal.Key(), handoff, startedAt); err != nil {
		t.Fatal(err)
	}
	owner := loadStateJournal(t, store, journal.Key()).ContainmentAuthority.Clone()
	stopReceipt := testStateLinuxCgroupV2Receipt(owner, authority.LinuxCgroupStopProofHelper, nil)
	if _, err := store.RecordContainmentStopReceipt(journal.Key(), owner.TargetPID, owner.TargetIdentity, stopReceipt); err != nil {
		t.Fatal(err)
	}
	releaseProof := testStateLinuxCgroupV2ReleaseDTO(handoff, stopReceipt)
	if err := releaseProof.Validate(); err != nil {
		t.Fatalf("normal release proof fixture invalid: %v", err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(journal.RunID, journal.Generation)
	if err != nil {
		t.Fatal(err)
	}
	promotionRef, err := authority.LinuxClonePromotionRef(journal.RunID, journal.Generation)
	if err != nil {
		t.Fatal(err)
	}
	proof := authority.LinuxCloneAbortProof{
		Version: authority.LinuxCloneVersion, Intent: intent, NormalReleaseProof: &releaseProof,
		BaseRef: baseRef, PromotionRef: promotionRef,
		TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
	}
	if err := proof.Validate(); err == nil {
		t.Fatal("normal release incorrectly authorized an absence-only clone abort")
	}
	if _, err := store.CompleteLinuxCloneAbort(journal.Key(), journal.Fence(), intent, proof); err == nil {
		t.Fatal("CompleteLinuxCloneAbort() accepted a normal release as proof B never received RELEASE")
	}
	if _, err := store.ClearLinuxCloneNoTargetRetention(journal.Key(), journal.Fence(), proof); err == nil {
		t.Fatal("ClearLinuxCloneNoTargetRetention() accepted a normal release as no-target proof")
	}
	stopped := loadStateJournal(t, store, journal.Key())
	stopped.PID, stopped.ProcessIdentity, stopped.StartedAt = 0, "", time.Time{}
	stopped.ContainmentAuthority = nil
	stopped.ContainmentHandoff = nil
	stopped.ContainmentCgroup = nil
	stopped.ContainmentCgroupReleaseProof = &releaseProof
	if linuxCloneAbortHistoryMatches(stopped, &proof) || linuxCloneAbortFenceAvailable(stopped) {
		t.Fatal("normal release authorized a no-target abort or a new abort fence")
	}
	pending := stopped
	pending.LinuxCloneAbortPending = true
	if err := validateJournal(pending); err != nil {
		t.Fatalf("legacy pending abort with normal release was not retained for recovery: %v", err)
	}
	legacyComplete := pending
	legacyProof := proof.Clone()
	legacyComplete.LinuxCloneAbortPending = false
	legacyComplete.LinuxCloneAbortProof = &legacyProof
	legacyComplete.WorkspacePath = ""
	legacyComplete.WorkspaceRecoveryRequired = false
	legacyComplete.LocalState = "cleanup_pending"
	legacyComplete.TerminalPendingAt = time.Now().UTC().Add(-time.Minute)
	legacyComplete.TerminalState = "failed"
	legacyComplete.TerminalVerdict = TerminalVerdictOwnershipLost
	legacyComplete.TerminalResolvedAt = time.Now().UTC()
	if err := validateJournal(legacyComplete); err != nil {
		t.Fatalf("legacy normal-release tombstone was not preserved as unresolved state: %v", err)
	}
	if legacyComplete.LinuxCloneCleanupEligible(journal.Key(), journal.Fence()) {
		t.Fatal("legacy normal-release abort tombstone authorized journal retirement")
	}
	missingTargetProof := proof.Clone()
	missingTargetProof.TargetAbsent = false
	if missingTargetProof.Validate() == nil {
		t.Fatal("normal release proof implied target absence without an independent target check")
	}
	missingRelease := stopped
	missingRelease.ContainmentCgroupReleaseProof = nil
	if linuxCloneAbortHistoryMatches(missingRelease, &proof) {
		t.Fatal("clone abort history accepted a normal release without its durable tombstone")
	}
}

func TestLinuxCloneJournalCASRejectsStaleOrChangedEvidence(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-cas-run")
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()

	staleFence := fence
	staleFence.LeaseToken = "other-lease"
	if _, err := store.ReserveLinuxCloneIntent(key, staleFence, intent, nil); err == nil {
		t.Fatal("ReserveLinuxCloneIntent() accepted a stale fence")
	}
	if _, err := store.ReserveLinuxCloneIntent(RunKey{RunID: key.RunID, Generation: key.Generation + 1}, fence, intent, nil); err == nil {
		t.Fatal("ReserveLinuxCloneIntent() accepted a stale RunKey")
	}

	replayUnknownStateMutation(t, store, func() error {
		_, err := store.ReserveLinuxCloneIntent(key, fence, intent, nil)
		return err
	})
	changedIntent := intent
	changedIntent.Nonce = strings.Repeat("d", authority.LinuxCloneNonceBytes*2)
	if _, err := store.ReserveLinuxCloneIntent(key, fence, changedIntent, nil); err == nil {
		t.Fatal("ReserveLinuxCloneIntent() replaced the reserved nonce")
	}

	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("c", 64), Size: 4096, Device: 9, Inode: 17,
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.BindLinuxCloneSeed(key, fence, intent, seed)
		return err
	})
	changedSeed := seed
	changedSeed.Digest = "sha256:" + strings.Repeat("e", 64)
	if _, err := store.BindLinuxCloneSeed(key, fence, intent, changedSeed); err == nil {
		t.Fatal("BindLinuxCloneSeed() replaced the durable seed digest")
	}
	if _, err := store.BindLinuxCloneSeed(key, staleFence, intent, seed); err == nil {
		t.Fatal("BindLinuxCloneSeed() accepted a stale fence")
	}
	changedSubjectJournal := loadStateJournal(t, store, key)
	changedSubject := protocol.Subject{
		ResourceID: "00000000-0000-4000-8000-000000000099",
		Commit:     intent.BaseCommit, TreeDigest: intent.BaseTreeDigest,
	}
	changedSubjectJournal.Work.Input = linuxCloneAdmissionInput(t, changedSubject)
	if err := store.SaveJournal(changedSubjectJournal); err == nil {
		t.Fatal("SaveJournal() accepted a changed admitted ResourceID with the same commit and tree digest")
	}

	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target, Nonce: intent.Nonce, SeedDigest: seed.Digest,
		BaseCommit: intent.BaseCommit, TargetDevice: 13, TargetInode: 29,
	}
	wrongNonce := checkout
	wrongNonce.Nonce = strings.Repeat("f", authority.LinuxCloneNonceBytes*2)
	if _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, wrongNonce); err == nil {
		t.Fatal("RecordLinuxCloneCheckoutReady() accepted a replayed checkout nonce")
	}
	wrongTarget := checkout
	wrongTarget.Target = filepath.Join(t.TempDir(), "other-target")
	if _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, wrongTarget); err == nil {
		t.Fatal("RecordLinuxCloneCheckoutReady() accepted a candidate target mismatch")
	}
	wrongBinding := checkout
	wrongBinding.BindingKey = "other-binding"
	if _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, wrongBinding); err == nil {
		t.Fatal("RecordLinuxCloneCheckoutReady() accepted a candidate binding mismatch")
	}
	wrongResource := checkout
	wrongResource.ResourceID = "00000000-0000-4000-8000-000000000099"
	if _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, wrongResource); err == nil {
		t.Fatal("RecordLinuxCloneCheckoutReady() accepted a candidate ResourceID mismatch")
	}
	wrongCommit := checkout
	wrongCommit.BaseCommit = strings.Repeat("1", 40)
	if _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, wrongCommit); err == nil {
		t.Fatal("RecordLinuxCloneCheckoutReady() accepted a candidate base-commit mismatch")
	}
	if _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, checkout); err == nil {
		t.Fatal("RecordLinuxCloneCheckoutReady() accepted checkout before committed v2 gate authority")
	}

	loaded := loadStateJournal(t, store, key)
	mutated := loaded
	changedJournalIntent := *loaded.LinuxCloneIntent
	changedJournalIntent.Nonce = strings.Repeat("1", authority.LinuxCloneNonceBytes*2)
	mutated.LinuxCloneIntent = &changedJournalIntent
	if err := store.SaveJournal(mutated); err == nil {
		t.Fatal("SaveJournal() bypassed dedicated immutable clone-intent CAS")
	}
	if loaded.LinuxCloneCleanupEligible(key, fence) {
		t.Fatal("checkout/seed evidence without promotion and v2 release authorized cleanup")
	}
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() removed an unpromoted Linux clone journal")
	}
}

func TestLinuxCloneIntentFreezesFenceAndReservesUniquePaths(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	firstJournal, firstIntent := newLinuxCloneJournal(t, "clone-path-first")
	firstJournal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(firstJournal); err != nil {
		t.Fatal(err)
	}
	key, fence := firstJournal.Key(), firstJournal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, firstIntent, nil); err != nil {
		t.Fatal(err)
	}
	handoff, reservation := bindStateLinuxCgroupV2Handoff(t, store, key)
	if _, err := store.CommitSupervisorHandoff(key, handoff, reservation.Gate.ObservedAt); err != nil {
		t.Fatal(err)
	}
	liveOwners := map[RunKey]protocol.Fence{key: fence}

	reserved := loadStateJournal(t, store, key)
	for _, test := range []struct {
		name   string
		mutate func(*RunJournal)
	}{
		{name: "runtime key", mutate: func(value *RunJournal) { value.RuntimeKey = "other-runtime" }},
		{name: "runtime ID", mutate: func(value *RunJournal) { value.RuntimeID = "other-runtime-id" }},
		{name: "runtime epoch", mutate: func(value *RunJournal) { value.ClaimedRuntimeEpoch++ }},
		{name: "claim ID", mutate: func(value *RunJournal) { value.ClaimID = "other-claim" }},
		{name: "lease token", mutate: func(value *RunJournal) { value.LeaseToken = "other-lease" }},
		{name: "lease expiry", mutate: func(value *RunJournal) { value.LeaseExpiresAt = value.LeaseExpiresAt.Add(time.Minute) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := reserved
			test.mutate(&changed)
			if err := store.SaveJournal(changed); err == nil {
				t.Fatal("SaveJournal() replaced a Linux clone RunKey fence")
			}
		})
	}

	for _, test := range []struct {
		name   string
		mutate func(*authority.LinuxCloneIntent, authority.LinuxCloneIntent)
	}{
		{name: "seed alias", mutate: func(candidate *authority.LinuxCloneIntent, first authority.LinuxCloneIntent) {
			candidate.SeedPath = first.SeedPath
		}},
		{name: "target alias", mutate: func(candidate *authority.LinuxCloneIntent, first authority.LinuxCloneIntent) {
			candidate.Target = first.Target
		}},
		{name: "cross alias", mutate: func(candidate *authority.LinuxCloneIntent, first authority.LinuxCloneIntent) {
			candidate.SeedPath = first.Target
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal, candidate := newLinuxCloneJournal(t, "clone-path-"+strings.ReplaceAll(test.name, " ", "-"))
			journal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			test.mutate(&candidate, firstIntent)
			if _, err := store.ReserveLinuxCloneIntent(journal.Key(), journal.Fence(), candidate, liveOwners); err == nil {
				t.Fatalf("ReserveLinuxCloneIntent() accepted %s with another RunKey", test.name)
			}
			if err := store.DeleteJournal(journal.Key()); err != nil {
				t.Fatalf("remove rejected clone path target: %v", err)
			}
		})
	}
}

func TestLinuxCloneCleanupRequiresDeliveredCandidateArtifact(t *testing.T) {
	t.Parallel()
	journal, intent := newLinuxCloneJournal(t, "00000000-0000-4000-8000-000000000041")
	journal.Work.Input = linuxCloneAdmissionInput(t, protocol.Subject{
		ResourceID: intent.ResourceID, Commit: intent.BaseCommit, TreeDigest: intent.BaseTreeDigest,
	})
	candidate := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
		Commit: strings.Repeat("e", 40), TreeDigest: "sha256:" + strings.Repeat("f", 64),
	}
	journal.LinuxClonePromotionIntent = &candidate
	journal.GoalDeliveryEnabled = true
	journal.TerminalState = "completed"
	journal.TerminalTaskResultKind = protocol.TaskResultCandidateCompletion
	if !linuxCloneArtifactReceiptRequired(journal) || linuxCloneExactArtifactDelivered(journal) {
		t.Fatal("completed candidate clone was cleanup-eligible without delivered artifact evidence")
	}
	subject := protocol.Subject{ResourceID: candidate.ResourceID, Commit: candidate.Commit, TreeDigest: candidate.TreeDigest}
	evidence := linuxCloneArtifactEvidence(t, journal.RunID, subject)
	delivery := GoalDelivery{Kind: GoalDeliveryEvidence, DeliveryID: evidence.EvidenceKey, Fence: journal.Fence(), Evidence: &evidence, Ready: true}
	if err := prepareGoalDelivery(journal.RunID, &delivery); err != nil {
		t.Fatal(err)
	}
	journal.DeliveredGoalDeliveries = []GoalDelivery{delivery}
	if linuxCloneArtifactReceiptRequired(journal) || !linuxCloneExactArtifactDelivered(journal) {
		t.Fatal("durably delivered artifact evidence did not satisfy candidate cleanup gate")
	}
}

func TestDeleteJournalKeepsCandidateCloneWithoutDeliveredArtifact(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-candidate-artifact-gate")
	journal.GoalDeliveryEnabled = true
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, intent, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FenceLinuxCloneBeforeCheckout(key, fence, intent); err != nil {
		t.Fatal(err)
	}
	proof := pinnedLinuxCloneAbortProof(t, intent, nil)
	if _, err := store.CompleteLinuxCloneAbort(key, fence, intent, proof); err != nil {
		t.Fatal(err)
	}
	terminalAt := time.Date(2026, 9, 27, 2, 3, 4, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{
		TransitionID: "clone-candidate-completed", State: "completed",
		Payload: json.RawMessage(`{"task_result":{"kind":"candidate_completion"}}`),
	}, terminalAt); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.LoadJournal(key)
	if err != nil || terminal.TerminalTaskResultKind != protocol.TaskResultCandidateCompletion {
		t.Fatalf("terminal candidate kind = %q, error = %v", terminal.TerminalTaskResultKind, err)
	}
	if _, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, terminalAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() discarded a candidate clone without delivered artifact evidence")
	}
}

func TestLinuxCloneCleanupBeginCompleteRequiresExactReadbacks(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	base := newPinnedLinuxCloneCleanupReady(t, "00000000-0000-4000-8000-000000000077", "clone-cleanup-completed")
	store, key, fence, intent, seed := base.store, base.key, base.fence, base.intent, base.seed
	ready := loadStateJournal(t, store, key)
	if ready.LinuxCloneCheckoutReceipt == nil || !ready.LinuxCloneCheckoutReceipt.Equal(base.checkout) {
		t.Fatalf("persisted checkout receipt = %#v, want the submitted %#v", ready.LinuxCloneCheckoutReceipt, base.checkout)
	}
	checkout := base.checkout
	retainedReady := ready
	retainedReady.RetainWorkspace = true
	if retainedReady.LinuxCloneCleanupBeginEligible(key, fence) {
		t.Fatal("pre-Begin clone retention failed to block normal cleanup")
	}
	retainedStore, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = retainedStore.Close() })
	retainedBytes, err := serializeRunJournal(retainedReady)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(retainedStore.journalPath(key), retainedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := retainedStore.RetainWorkspace(key); err != nil {
		t.Fatal(err)
	}
	if _, err := retainedStore.BeginLinuxCloneCleanup(key, fence); err == nil {
		t.Fatal("BeginLinuxCloneCleanup() ignored a pre-existing retention barrier")
	}
	cleanupIntent, err := LinuxCloneCleanupIntentForJournal(ready)
	if err != nil {
		t.Fatal(err)
	}
	grant := authority.LinuxCloneGrant{Intent: intent, Seed: seed}
	preBeginReader, err := OpenLinuxCloneCleanupReader(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := preBeginReader.Verify(key, grant, cleanupIntent, nil, true); err == nil {
		_ = preBeginReader.Close()
		t.Fatal("read-only State verifier accepted an unpersisted Begin cleanup intent")
	}
	if err := preBeginReader.Close(); err != nil {
		t.Fatal(err)
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.BeginLinuxCloneCleanup(key, fence)
		return err
	})
	pending := loadStateJournal(t, store, key)
	if !pending.LinuxCloneCleanupPending || pending.LinuxCloneCleanupIntent == nil ||
		!pending.LinuxCloneCleanupIntent.Equal(cleanupIntent) || pending.LinuxCloneTargetRemovalProof != nil || pending.LinuxCloneCleanupProof != nil {
		t.Fatalf("cleanup pending CAS state = %#v", pending)
	}
	reader, err := OpenLinuxCloneCleanupReader(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if err := reader.Verify(key, grant, cleanupIntent, nil, true); err != nil {
		t.Fatalf("read exact pending cleanup authority before root effect: %v", err)
	}
	stateDir := store.dir
	replacedStateDir := stateDir + ".pinned-test-old"
	if err := os.Rename(stateDir, replacedStateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		_ = os.Rename(replacedStateDir, stateDir)
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(stateDir, runsDirectoryName), 0o700); err != nil {
		_ = os.RemoveAll(stateDir)
		_ = os.Rename(replacedStateDir, stateDir)
		t.Fatal(err)
	}
	stateRetargetErr := reader.Verify(key, grant, cleanupIntent, nil, true)
	if err := os.RemoveAll(stateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacedStateDir, stateDir); err != nil {
		t.Fatal(err)
	}
	if stateRetargetErr == nil {
		t.Fatal("read-only State verifier accepted a same-name StateDir replacement")
	}
	runsDir := filepath.Join(stateDir, runsDirectoryName)
	replacedRunsDir := runsDir + ".pinned-test-old"
	if err := os.Rename(runsDir, replacedRunsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(runsDir, 0o700); err != nil {
		_ = os.Rename(replacedRunsDir, runsDir)
		t.Fatal(err)
	}
	runsRetargetErr := reader.Verify(key, grant, cleanupIntent, nil, true)
	if err := os.RemoveAll(runsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacedRunsDir, runsDir); err != nil {
		t.Fatal(err)
	}
	if runsRetargetErr == nil {
		t.Fatal("read-only State verifier accepted a same-name runs directory replacement")
	}
	if err := reader.Verify(key, grant, cleanupIntent, nil, false); err == nil {
		t.Fatal("read-only State verifier authorized absent target without prior removal proof")
	}
	wrongKey := key
	wrongKey.Generation++
	if err := reader.Verify(wrongKey, grant, cleanupIntent, nil, true); err == nil {
		t.Fatal("read-only State verifier accepted a stale RunKey")
	}
	forgedGrant := grant.Clone()
	forgedGrant.Seed.Digest = "sha256:" + strings.Repeat("9", 64)
	if err := reader.Verify(key, forgedGrant, cleanupIntent, nil, true); err == nil {
		t.Fatal("read-only State verifier accepted a forged clone grant not stored in RunJournal")
	}
	staleCleanupIntent := cleanupIntent.Clone()
	staleCleanupIntent.FenceDigest = "sha256:" + strings.Repeat("2", 64)
	if err := reader.Verify(key, grant, staleCleanupIntent, nil, true); err == nil {
		t.Fatal("read-only State verifier accepted a stale fence digest")
	}
	if _, err := store.RetainWorkspace(key); err == nil {
		t.Fatal("RetainWorkspace() changed cleanup ownership after the durable Begin CAS")
	}
	if loadStateJournal(t, store, key).RetainWorkspace {
		t.Fatal("rejected late RetainWorkspace() changed the durable cleanup decision")
	}
	lateRetentionSnapshot := pending
	lateRetentionSnapshot.RetainWorkspace = true
	if err := store.SaveJournal(lateRetentionSnapshot); err == nil {
		t.Fatal("SaveJournal() installed retention after the durable Begin CAS")
	}
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() removed a pending cleanup before effect proof")
	}
	forgedPending := pending
	forgedPending.LinuxCloneCleanupPending = false
	if err := store.SaveJournal(forgedPending); err == nil {
		t.Fatal("SaveJournal() accepted a pending cleanup import through the replacement seam")
	}

	rootDeleter := authority.LinuxCgroupProcessIdentity{PID: 901, Identity: "root-clone-cleaner:901:start"}
	witness := authority.LinuxCgroupProcessIdentity{
		PID: cleanupIntent.NormalReleaseProof.AnchorPID, Identity: cleanupIntent.NormalReleaseProof.AnchorIdentity,
	}
	targetProof := authority.LinuxCloneTargetRemovalProof{
		Version: authority.LinuxCloneVersion, Intent: cleanupIntent,
		RootDevice: 9, RootInode: 11,
		TargetDevice: checkout.TargetDevice, TargetInode: checkout.TargetInode,
		Disposition: authority.LinuxCloneTargetDispositionRemoved, PathRewalkVerified: true,
		BOwnedMountConfined: true, TargetNlinkZero: true,
		RootDeleterUID: 0, RootDeleter: rootDeleter,
		RootDeleterExit: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: rootDeleter, Witness: witness, Waited: true, Reaped: true,
		},
	}
	if err := targetProof.Validate(); err != nil {
		t.Fatalf("valid target-removal proof fixture: %v", err)
	}
	if err := reader.Verify(key, grant, cleanupIntent, &targetProof, false); err == nil {
		t.Fatal("read-only State verifier accepted a valid but unpersisted prior removal proof")
	}
	for _, test := range []struct {
		name  string
		key   RunKey
		fence protocol.Fence
		proof authority.LinuxCloneTargetRemovalProof
	}{
		{name: "stale RunKey", key: RunKey{RunID: key.RunID, Generation: key.Generation + 1}, fence: fence, proof: targetProof},
		{name: "stale fence", key: key, fence: protocol.Fence{RuntimeID: fence.RuntimeID, RuntimeEpoch: fence.RuntimeEpoch, Generation: fence.Generation, ClaimID: fence.ClaimID, LeaseToken: "stale-token"}, proof: targetProof},
		{name: "mutated cleanup intent", key: key, fence: fence, proof: func() authority.LinuxCloneTargetRemovalProof {
			mutated := targetProof.Clone()
			mutated.Intent.FenceDigest = "sha256:" + strings.Repeat("2", 64)
			return mutated
		}()},
		{name: "mutated checkout identity", key: key, fence: fence, proof: func() authority.LinuxCloneTargetRemovalProof {
			mutated := targetProof.Clone()
			mutated.Intent.CheckoutReceipt.TargetInode++
			mutated.TargetInode++
			return mutated
		}()},
		{name: "non-root worker", key: key, fence: fence, proof: func() authority.LinuxCloneTargetRemovalProof {
			mutated := targetProof.Clone()
			mutated.RootDeleterUID = 1000
			return mutated
		}()},
		{name: "already absent without prior receipt", key: key, fence: fence, proof: func() authority.LinuxCloneTargetRemovalProof {
			mutated := targetProof.Clone()
			mutated.Disposition = authority.LinuxCloneTargetDispositionAlreadyAbsent
			mutated.TargetNlinkZero = false
			return mutated
		}()},
		{name: "target link count not proven zero", key: key, fence: fence, proof: func() authority.LinuxCloneTargetRemovalProof {
			mutated := targetProof.Clone()
			mutated.TargetNlinkZero = false
			return mutated
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.RecordLinuxCloneTargetRemoval(test.key, test.fence, cleanupIntent, test.proof); err == nil {
				t.Fatal("RecordLinuxCloneTargetRemoval() accepted stale or altered authority")
			}
		})
	}
	injectedTarget := pending
	injectedTargetProof := targetProof.Clone()
	injectedTarget.LinuxCloneTargetRemovalProof = &injectedTargetProof
	if err := store.SaveJournal(injectedTarget); err == nil {
		t.Fatal("SaveJournal() injected a target-removal proof without its dedicated CAS")
	}
	proof := authority.LinuxCloneCleanupProof{
		Version: authority.LinuxCloneVersion, Intent: cleanupIntent, TargetRemovalProof: targetProof,
		SeedAbsent: true, BaseRefAbsent: true, ScratchAbsent: true, PromotionRefPresent: true,
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("valid cleanup proof fixture: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*authority.LinuxCloneCleanupProof)
	}{
		{name: "seed absence", mutate: func(invalid *authority.LinuxCloneCleanupProof) { invalid.SeedAbsent = false }},
		{name: "base-ref absence", mutate: func(invalid *authority.LinuxCloneCleanupProof) { invalid.BaseRefAbsent = false }},
		{name: "scratch absence", mutate: func(invalid *authority.LinuxCloneCleanupProof) { invalid.ScratchAbsent = false }},
		{name: "promotion-ref presence", mutate: func(invalid *authority.LinuxCloneCleanupProof) { invalid.PromotionRefPresent = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := proof
			test.mutate(&invalid)
			if _, err := store.CompleteLinuxCloneCleanup(key, fence, cleanupIntent, invalid); err == nil {
				t.Fatal("CompleteLinuxCloneCleanup() accepted an incomplete A-side cleanup readback")
			}
		})
	}
	if _, err := store.CompleteLinuxCloneCleanup(key, fence, cleanupIntent, proof); err == nil {
		t.Fatal("CompleteLinuxCloneCleanup() accepted target removal before its exact proof was persisted")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordLinuxCloneTargetRemoval(key, fence, cleanupIntent, targetProof)
		return err
	})
	stored := loadStateJournal(t, store, key)
	if stored.LinuxCloneTargetRemovalProof == nil || !stored.LinuxCloneTargetRemovalProof.Equal(targetProof) {
		t.Fatalf("target-removal CAS readback = %#v", stored.LinuxCloneTargetRemovalProof)
	}
	if err := reader.Verify(key, grant, cleanupIntent, &targetProof, false); err != nil {
		t.Fatalf("read exact prior removal proof after target absence: %v", err)
	}
	forgedPrior := targetProof.Clone()
	forgedPrior.RootDevice++
	if err := reader.Verify(key, grant, cleanupIntent, &forgedPrior, false); err == nil {
		t.Fatal("read-only State verifier accepted a forged prior removal receipt")
	}
	if err := reader.Verify(key, grant, cleanupIntent, nil, true); err == nil {
		t.Fatal("read-only State verifier accepted a fresh target after removal was persisted")
	}
	if err := reader.verify(key, grant, cleanupIntent, &targetProof, false, func() error {
		_, err := store.mutateJournal(key, func(current *RunJournal) error {
			current.DroppedOutputBytes++
			return nil
		})
		return err
	}); err == nil {
		t.Fatal("read-only State verifier accepted a journal rewrite before its fsync and reopen barrier")
	}
	journalPath := store.journalPath(key)
	originalJournalBytes, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.verify(key, grant, cleanupIntent, &targetProof, false, func() error {
		return os.WriteFile(journalPath, []byte(`{"run_id":`), 0o600)
	}); err == nil {
		t.Fatal("read-only State verifier accepted a corrupt journal replacement before reopen")
	}
	if err := writeAtomic(journalPath, originalJournalBytes); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(stateDir)
	if err != nil {
		t.Fatalf("restart after persisted target-removal proof: %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	store = restarted
	reader, err = OpenLinuxCloneCleanupReader(stateDir)
	if err != nil {
		t.Fatalf("reopen read-only cleanup reader after restart: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if err := reader.Verify(key, grant, cleanupIntent, &targetProof, false); err != nil {
		t.Fatalf("restart did not verify the exact persisted target-removal proof: %v", err)
	}
	stored = loadStateJournal(t, store, key)
	changedTargetProof := targetProof.Clone()
	changedTargetProof.RootDevice++
	if _, err := store.RecordLinuxCloneTargetRemoval(key, fence, cleanupIntent, changedTargetProof); err == nil {
		t.Fatal("RecordLinuxCloneTargetRemoval() changed a persisted proof during replay")
	}
	forgedStored := stored
	forgedStored.LinuxCloneTargetRemovalProof = nil
	if err := store.SaveJournal(forgedStored); err == nil {
		t.Fatal("SaveJournal() cleared a durable target-removal proof")
	}
	changedProof := proof
	changedProof.TargetRemovalProof.RootDevice++
	if _, err := store.CompleteLinuxCloneCleanup(key, fence, cleanupIntent, changedProof); err == nil {
		t.Fatal("CompleteLinuxCloneCleanup() accepted a target-removal proof differing from the durable CAS")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.CompleteLinuxCloneCleanup(key, fence, cleanupIntent, proof)
		return err
	})
	completed := loadStateJournal(t, store, key)
	if completed.LinuxCloneCleanupPending || completed.LinuxCloneCleanupProof == nil ||
		!completed.LinuxCloneCleanupProof.Equal(proof) || completed.WorkspacePath != "" || completed.WorkspaceRecoveryRequired {
		t.Fatalf("cleanup completion readback = %#v", completed)
	}
	if !completed.LinuxCloneCleanupEligible(key, fence) {
		t.Fatal("exact cleanup proof did not authorize terminal journal retirement")
	}
	if err := store.DeleteJournal(key); err != nil {
		t.Fatalf("DeleteJournal() with exact cleanup proof: %v", err)
	}
}

func TestLinuxCloneCheckoutAndPromotionPreserveContainmentAndReplayExactly(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-staged-run")
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, intent, nil); err != nil {
		t.Fatal(err)
	}
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("a", 64), Size: 2048, Device: 17, Inode: 31,
	}
	if _, err := store.BindLinuxCloneSeed(key, fence, intent, seed); err != nil {
		t.Fatal(err)
	}

	_, reservation := bindStateLinuxCgroupV2Handoff(t, store, key)
	if reservation.Gate == nil {
		t.Fatal("test v2 handoff has no trusted gate")
	}
	startedAt := reservation.Gate.ObservedAt
	bound := loadStateJournal(t, store, key).ContainmentHandoff.Clone()
	if _, err := store.CommitSupervisorHandoff(key, bound, startedAt); err != nil {
		t.Fatalf("commit v2 handoff: %v", err)
	}

	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target, Nonce: intent.Nonce, SeedDigest: seed.Digest,
		BaseCommit: intent.BaseCommit, TargetDevice: 23, TargetInode: 47,
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, checkout)
		return err
	})
	owned := loadStateJournal(t, store, key)
	if owned.LinuxCloneCheckoutReceipt == nil || !owned.LinuxCloneCheckoutReceipt.Equal(checkout) ||
		owned.ContainmentAuthority == nil || owned.ContainmentCgroup == nil || !owned.HasProcessDetails() || !owned.HasPendingContainment() {
		t.Fatalf("checkout-ready changed or cleared containment authority: %#v", owned)
	}

	promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil {
		t.Fatal(err)
	}
	candidate := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
		Commit: strings.Repeat("e", 40), TreeDigest: "sha256:" + strings.Repeat("f", 64),
	}
	promotion := authority.LinuxClonePromotionReceipt{
		Version: candidate.Version, Commit: candidate.Commit, TreeDigest: candidate.TreeDigest, Ref: promotionRef,
	}
	if _, err := store.ReserveLinuxClonePromotion(key, fence, intent, candidate); err == nil {
		t.Fatal("ReserveLinuxClonePromotion() accepted a candidate before durable stop proof")
	}
	owner := owned.ContainmentAuthority.Clone()
	stopReceipt := testStateLinuxCgroupV2Receipt(owner, authority.LinuxCgroupStopProofHelper, nil)
	if _, err := store.RecordContainmentStopReceipt(key, owner.TargetPID, owner.TargetIdentity, stopReceipt); err != nil {
		t.Fatalf("record exact v2 stop proof: %v", err)
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.ReserveLinuxClonePromotion(key, fence, intent, candidate)
		return err
	})
	reservedCandidate := loadStateJournal(t, store, key)
	if reservedCandidate.LinuxClonePromotionIntent == nil || !reservedCandidate.LinuxClonePromotionIntent.Equal(candidate) ||
		reservedCandidate.LinuxClonePromotionReceipt != nil {
		t.Fatalf("reserved candidate intent = %#v", reservedCandidate.LinuxClonePromotionIntent)
	}
	mutableCandidateJournal := reservedCandidate
	changedJournalCandidate := *reservedCandidate.LinuxClonePromotionIntent
	changedJournalCandidate.TreeDigest = "sha256:" + strings.Repeat("0", 64)
	mutableCandidateJournal.LinuxClonePromotionIntent = &changedJournalCandidate
	if err := store.SaveJournal(mutableCandidateJournal); err == nil {
		t.Fatal("SaveJournal() replaced the immutable Linux clone promotion intent")
	}
	changedCandidate := candidate
	changedCandidate.TreeDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := store.ReserveLinuxClonePromotion(key, fence, intent, changedCandidate); err == nil {
		t.Fatal("ReserveLinuxClonePromotion() replaced the immutable candidate intent")
	}
	wrongRef := promotion
	wrongRef.Ref = "refs/heads/symmetry/clone-v2/base/" + strings.Repeat("a", 64)
	if _, err := store.RecordLinuxClonePromotion(key, fence, intent, candidate, wrongRef); err == nil {
		t.Fatal("RecordLinuxClonePromotion() accepted a base ref or candidate ref mismatch")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordLinuxClonePromotion(key, fence, intent, candidate, promotion)
		return err
	})
	loaded := loadStateJournal(t, store, key)
	if loaded.LinuxClonePromotionReceipt == nil || !loaded.LinuxClonePromotionReceipt.Equal(promotion) ||
		loaded.LinuxClonePromotionIntent == nil || !loaded.LinuxClonePromotionIntent.Equal(candidate) ||
		loaded.ContainmentAuthority == nil || loaded.ContainmentCgroup == nil || !loaded.HasPendingContainment() {
		t.Fatalf("promotion changed or cleared containment authority: %#v", loaded)
	}

	changedPromotion := promotion
	changedPromotion.Commit = strings.Repeat("0", 40)
	if _, err := store.RecordLinuxClonePromotion(key, fence, intent, candidate, changedPromotion); err == nil {
		t.Fatal("RecordLinuxClonePromotion() replaced the immutable candidate receipt")
	}
	if loaded.LinuxCloneCleanupEligible(key, fence) {
		t.Fatal("stop receipt without exact release/clear authorized workspace cleanup")
	}
	terminalAt := time.Date(2026, 9, 27, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{
		TransitionID: "clone-terminal", State: "failed", Payload: json.RawMessage(`{}`),
	}, terminalAt); err != nil {
		t.Fatalf("persist terminal before replay: %v", err)
	}
	for _, replay := range []struct {
		name         string
		startsEffect bool
		mutate       func() error
	}{
		{name: "intent", startsEffect: true, mutate: func() error { _, err := store.ReserveLinuxCloneIntent(key, fence, intent, nil); return err }},
		{name: "seed", mutate: func() error { _, err := store.BindLinuxCloneSeed(key, fence, intent, seed); return err }},
		{name: "checkout", mutate: func() error { _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, checkout); return err }},
		{name: "promotion intent", startsEffect: true, mutate: func() error { _, err := store.ReserveLinuxClonePromotion(key, fence, intent, candidate); return err }},
		{name: "promotion", mutate: func() error {
			_, err := store.RecordLinuxClonePromotion(key, fence, intent, candidate, promotion)
			return err
		}},
	} {
		t.Run("terminal replay "+replay.name, func(t *testing.T) {
			if replay.startsEffect {
				before := readStateJournalBytes(t, store, key)
				if err := replay.mutate(); err == nil {
					t.Fatal("terminal replay reauthorized a native or promotion effect")
				}
				if after := readStateJournalBytes(t, store, key); !bytes.Equal(before, after) {
					t.Fatal("rejected terminal effect replay changed the durable journal")
				}
				return
			}
			replayUnknownStateMutation(t, store, replay.mutate)
		})
	}
}

func newLinuxCloneJournal(t *testing.T, runID string) (RunJournal, authority.LinuxCloneIntent) {
	t.Helper()
	journal := stoppedTestJournal(runID, 1)
	journal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	subject := protocol.Subject{
		ResourceID: "00000000-0000-4000-8000-000000000012",
		Commit:     strings.Repeat("a", 40),
		TreeDigest: "sha256:" + strings.Repeat("b", 64),
	}
	journal.Work.Input = linuxCloneAdmissionInput(t, subject)
	journal.Work.Workspace = "primary"
	journal.WorkspaceBindingKey = "primary"
	journal.WorkspacePath = ""
	journal.WorkspaceRecoveryRequired = false
	root := t.TempDir()
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: runID, Generation: 1, BindingKey: "primary", ResourceID: subject.ResourceID,
		BaseCommit: subject.Commit, BaseTreeDigest: subject.TreeDigest,
		Nonce:    strings.Repeat("c", authority.LinuxCloneNonceBytes*2),
		SeedPath: filepath.Join(t.TempDir(), "seed.bundle"),
		Target:   filepath.Join(root, "binding-primary", "run-"+runID, "generation-1"),
	}
	return journal, intent
}

func legacyPromotedLinuxCloneFixture(t *testing.T, runID string, legacyPath bool) (RunJournal, authority.LinuxCloneIntent, authority.LinuxClonePromotionReceipt, authority.LinuxClonePromotionIntent) {
	t.Helper()
	journal, intent := newLinuxCloneJournal(t, runID)
	if legacyPath {
		intent.Target = "/var/lib/symmetry/legacy-checkout"
	}
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("a", 64), Size: 1024, Device: 7, Inode: 11,
	}
	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target, Nonce: intent.Nonce,
		SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit, TargetDevice: 168, TargetInode: 210,
	}
	promotionRef, err := authority.LinuxClonePromotionRef(intent.RunID, intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	promotion := authority.LinuxClonePromotionReceipt{
		Version: authority.LinuxCloneVersion, Commit: strings.Repeat("e", 40),
		TreeDigest: "sha256:" + strings.Repeat("f", 64), Ref: promotionRef,
	}
	reservation := testStateLinuxCgroupV2ReadyReservation()
	handoff := testStateLinuxCgroupV2Handoff(reservation)
	handoff.SupervisorPID = reservation.Helper.PID
	handoff.SupervisorIdentity = reservation.Helper.Identity
	mirror := *reservation.Mirror
	handoff.Mirror = &mirror
	owner, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("legacy promotion release authority fixture: %v", err)
	}
	stopReceipt := testStateLinuxCgroupV2Receipt(owner, authority.LinuxCgroupStopProofHelper, nil)
	releaseProof := testStateLinuxCgroupV2ReleaseDTO(handoff, stopReceipt)
	if err := releaseProof.Validate(); err != nil {
		t.Fatalf("legacy promotion release proof fixture: %v", err)
	}
	candidate := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
		Commit: promotion.Commit, TreeDigest: promotion.TreeDigest,
	}
	journal.LinuxCloneIntent = &intent
	journal.LinuxCloneSeedReceipt = &seed
	journal.LinuxCloneCheckoutReceipt = &checkout
	journal.LinuxClonePromotionReceipt = &promotion
	journal.ContainmentCgroupReleaseProof = &releaseProof
	journal.WorkspacePath = intent.Target
	journal.WorkspaceRecoveryRequired = true
	return journal, intent, promotion, candidate
}

func linuxCloneAdmissionInput(t *testing.T, subject protocol.Subject) json.RawMessage {
	t.Helper()
	workItemID := "00000000-0000-4000-8000-000000000013"
	admission := protocol.Admission{
		SchemaVersion:     protocol.AdmissionSchemaVersion,
		AdmissionID:       "00000000-0000-4000-8000-000000000014",
		GoalID:            "00000000-0000-4000-8000-000000000015",
		GoalRevision:      1,
		WorkItemID:        &workItemID,
		Purpose:           protocol.AdmissionPurposeImplement,
		ContextSnapshotID: "00000000-0000-4000-8000-000000000016",
		ContextHash:       "sha256:" + strings.Repeat("c", 64),
		ModelProfile:      "default",
		SessionMode:       protocol.SessionModeFresh,
		Subject:           subject,
		Limits:            protocol.AdmissionLimits{MaxTurns: 1, DeadlineAt: "2026-10-01T00:00:00Z"},
	}
	encoded, err := json.Marshal(admission)
	if err != nil {
		t.Fatal(err)
	}
	parsed, present, err := protocol.ParseAdmissionInput(encoded)
	if err != nil || !present || parsed.Subject != subject {
		t.Fatalf("test admission input did not validate: present=%t err=%v", present, err)
	}
	return encoded
}

func linuxCloneArtifactEvidence(t *testing.T, runID string, subject protocol.Subject) protocol.Evidence {
	t.Helper()
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := "sha256:" + strings.Repeat("b", 64)
	evidence, err := protocol.ParseEvidence([]byte(fmt.Sprintf(`{
		"schema_version":"symmetry.evidence.v1",
		"evidence_id":"00000000-0000-4000-8000-000000000011",
		"run_id":"%s",
		"evidence_key":"candidate-artifact",
		"kind":"artifact",
		"subject":{"resource_id":"%s","commit":"%s","tree_digest":"%s"},
		"subject_hash":"%s",
		"source_ref":{"kind":"artifact","ref":"artifact-publish","subject_hash":"%s","resource_id":"%s","commit":"%s","path":"result.txt"},
		"source_revision":"artifact-publisher",
		"validator_profile":"artifact-publisher",
		"verdict":"passed",
		"payload":{"predicate_id":"artifact-publish","subject":{"resource_id":"%s","commit":"%s","tree_digest":"%s"},"subject_hash":"%s","resource_id":"%s","commit":"%s","path":"result.txt","content_digest":"%s"},
		"observed_at":"2026-09-10T01:01:00Z"
	}`, runID, subject.ResourceID, subject.Commit, subject.TreeDigest, subjectHash,
		subjectHash, subject.ResourceID, subject.Commit,
		subject.ResourceID, subject.Commit, subject.TreeDigest, subjectHash, subject.ResourceID, subject.Commit, contentDigest)))
	if err != nil {
		t.Fatalf("parse Linux clone artifact evidence: %v", err)
	}
	return evidence
}

func requireLinuxCloneAMD64(t *testing.T) {
	t.Helper()
	if runtime.GOARCH != "amd64" {
		t.Skip("Linux clone-v2 state is Linux/amd64 only")
	}
}

// linuxCloneReuseGrantFixture builds the reuse grant that both the
// alias-retirement test and the pinned reuse base consume; the grant owns its
// source target and varies only by the source, predecessor and consumer run
// IDs.
func linuxCloneReuseGrantFixture(t *testing.T, intent authority.LinuxCloneIntent, runID, predecessorRunID string) (authority.LinuxCloneSeedReceipt, authority.LinuxCloneReuseGrant) {
	t.Helper()
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("d", 64), Size: 128, Device: 7, Inode: 11,
	}
	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target,
		Nonce: intent.Nonce, SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit,
		TargetDevice: 9, TargetInode: 13,
	}
	return seed, authority.LinuxCloneReuseGrant{
		Version:        authority.LinuxCloneVersion,
		Source:         authority.LinuxCloneGrant{Intent: intent, Seed: seed},
		SourceCheckout: checkout,
		PredecessorStop: authority.LinuxCloneReuseStop{
			RunID: predecessorRunID, Generation: 1, SessionID: "00000000-0000-4000-8000-000000000090",
			LocalHandleID: "00000000-0000-4000-8000-000000000088", BindingID: "00000000-0000-4000-8000-000000000089",
			DeliveryDigest: strings.Repeat("e", 64), ReceiptID: "00000000-0000-4000-8000-000000000093",
		},
		RunID: runID, Generation: 1,
		WorkspaceFingerprint: "sha256:" + strings.Repeat("f", 64),
	}
}

func TestLinuxCloneReuseAliasRetiresOnlyAfterStableStop(t *testing.T) {
	t.Parallel()
	_, intent := newLinuxCloneJournal(t, "reuse-source")
	_, reuse := linuxCloneReuseGrantFixture(t, intent, "reuse-consumer", "run-source")
	consumer := stoppedTestJournal("reuse-consumer", 1)
	consumer.LocalState = "cleanup_pending"
	consumer.TerminalState = "completed"
	consumer.TerminalVerdict = "ownership_lost"
	consumer.LinuxCloneReuseGrant = &reuse
	consumer.WorkspacePath = intent.Target
	consumer.WorkspaceBindingKey = intent.BindingKey
	consumer.WorkspaceRecoveryRequired = true
	consumer.RetainWorkspace = true
	reservation := testStateLinuxCgroupV2ReadyReservation()
	handoff := testStateLinuxCgroupV2Handoff(reservation)
	handoff.SupervisorPID = reservation.Helper.PID
	handoff.SupervisorIdentity = reservation.Helper.Identity
	mirror := *reservation.Mirror
	handoff.Mirror = &mirror
	owner, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	stopReceipt := testStateLinuxCgroupV2Receipt(owner, authority.LinuxCgroupStopProofHelper, nil)
	proof := testStateLinuxCgroupV2ReleaseDTO(handoff, stopReceipt)
	consumer.ContainmentCgroupReleaseProof = &proof
	if !linuxCloneReuseAliasRetirable(consumer) {
		t.Fatal("stably stopped reuse alias was not retirable")
	}
	consumer.TerminalState = ""
	if linuxCloneReuseAliasRetirable(consumer) {
		t.Fatal("reuse alias without a terminal state was retirable")
	}
}

// pinnedLinuxCloneBase is one store-backed Linux clone journal built through
// the production mutation seam, plus the fixture values its cases mutate one at
// a time; the reuse-grant base is the documented exception, imported through
// the journal-path seam (see newPinnedLinuxCloneReuse). Cases may share one
// base store: validateJournal rejects a mutation before the write, so a failed
// case leaves the persisted journal untouched.
// checkout is the receipt submitted to the store, kept so tests can assert the
// persisted readback independently of the value they submit.
type pinnedLinuxCloneBase struct {
	store           *Store
	key             RunKey
	fence           protocol.Fence
	intent          authority.LinuxCloneIntent
	seed            authority.LinuxCloneSeedReceipt
	checkout        authority.LinuxCloneCheckoutReceipt
	promotionIntent authority.LinuxClonePromotionIntent
	handoff         authority.SupervisorHandoff
	stopReceipt     authority.StopReceipt
}

// pinnedLinuxCloneAbortProof builds the exact no-gate abort proof for one
// persisted intent; a nil seed models the pre-seed abort shape.
func pinnedLinuxCloneAbortProof(t *testing.T, intent authority.LinuxCloneIntent, seed *authority.LinuxCloneSeedReceipt) authority.LinuxCloneAbortProof {
	t.Helper()
	baseRef, err := authority.LinuxCloneBaseRef(intent.RunID, intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	promotionRef, err := authority.LinuxClonePromotionRef(intent.RunID, intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	return authority.LinuxCloneAbortProof{
		Version: authority.LinuxCloneVersion, Intent: intent, SeedReceipt: seed,
		BaseRef: baseRef, PromotionRef: promotionRef,
		TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
	}
}

// newPinnedLinuxCloneBase persists a clone journal through the same production
// mutations the sibling tests use. Stage "seed" stops after the seed receipt,
// "running" adds the committed v2 handoff, and "promoted" adds the checkout,
// the exact stop receipt, and the promotion intent and receipt.
func newPinnedLinuxCloneBase(t *testing.T, runID, stage string) pinnedLinuxCloneBase {
	t.Helper()
	base := pinnedLinuxCloneBase{store: mustStore(t)}
	journal, intent := newLinuxCloneJournal(t, runID)
	if err := base.store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	base.key, base.fence, base.intent = journal.Key(), journal.Fence(), intent
	if _, err := base.store.ReserveLinuxCloneIntent(base.key, base.fence, intent, nil); err != nil {
		t.Fatal(err)
	}
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("a", 64), Size: 2048, Device: 17, Inode: 31,
	}
	if _, err := base.store.BindLinuxCloneSeed(base.key, base.fence, intent, seed); err != nil {
		t.Fatal(err)
	}
	base.seed = seed
	if stage == "seed" {
		return base
	}
	handoff, reservation := bindStateLinuxCgroupV2Handoff(t, base.store, base.key)
	if _, err := base.store.CommitSupervisorHandoff(base.key, handoff, reservation.Gate.ObservedAt); err != nil {
		t.Fatal(err)
	}
	base.handoff = handoff
	if stage == "running" {
		return base
	}
	if stage != "promoted" {
		t.Fatalf("unknown pinned Linux clone stage %q", stage)
	}
	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target,
		Nonce: intent.Nonce, SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit,
		TargetDevice: 23, TargetInode: 47,
	}
	if _, err := base.store.RecordLinuxCloneCheckoutReady(base.key, base.fence, intent, checkout); err != nil {
		t.Fatal(err)
	}
	base.checkout = checkout
	owner := loadStateJournal(t, base.store, base.key).ContainmentAuthority.Clone()
	stopReceipt := testStateLinuxCgroupV2Receipt(owner, authority.LinuxCgroupStopProofHelper, nil)
	if _, err := base.store.RecordContainmentStopReceipt(base.key, owner.TargetPID, owner.TargetIdentity, stopReceipt); err != nil {
		t.Fatal(err)
	}
	base.stopReceipt = stopReceipt
	promotionRef, err := authority.LinuxClonePromotionRef(base.key.RunID, base.key.Generation)
	if err != nil {
		t.Fatal(err)
	}
	promotionIntent := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
		Commit: strings.Repeat("e", 40), TreeDigest: "sha256:" + strings.Repeat("f", 64),
	}
	promotion := authority.LinuxClonePromotionReceipt{
		Version: promotionIntent.Version, Commit: promotionIntent.Commit,
		TreeDigest: promotionIntent.TreeDigest, Ref: promotionRef,
	}
	if _, err := base.store.ReserveLinuxClonePromotion(base.key, base.fence, intent, promotionIntent); err != nil {
		t.Fatal(err)
	}
	if _, err := base.store.RecordLinuxClonePromotion(base.key, base.fence, intent, promotionIntent, promotion); err != nil {
		t.Fatal(err)
	}
	base.promotionIntent = promotionIntent
	return base
}

// newPinnedLinuxCloneAbortComplete persists the completed no-target abort
// tombstone that still carries its bound seed receipt.
func newPinnedLinuxCloneAbortComplete(t *testing.T, runID string) pinnedLinuxCloneBase {
	t.Helper()
	base := newPinnedLinuxCloneBase(t, runID, "seed")
	if _, err := base.store.FenceLinuxCloneBeforeCheckout(base.key, base.fence, base.intent); err != nil {
		t.Fatal(err)
	}
	proof := pinnedLinuxCloneAbortProof(t, base.intent, &base.seed)
	if _, err := base.store.CompleteLinuxCloneAbort(base.key, base.fence, base.intent, proof); err != nil {
		t.Fatal(err)
	}
	return base
}

// newPinnedLinuxCloneCleanupReady persists the promoted clone through the
// durable release tombstone, the exact candidate-artifact delivery and the
// terminal resolution, then requires the result to be eligible to begin normal
// cleanup; the platform tests cover issuance of that proof from the opaque
// LinuxCgroupV2Release capability.
func newPinnedLinuxCloneCleanupReady(t *testing.T, runID, transitionID string) pinnedLinuxCloneBase {
	t.Helper()
	base := newPinnedLinuxCloneBase(t, runID, "promoted")
	releaseProof := testStateLinuxCgroupV2ReleaseDTO(base.handoff, base.stopReceipt)
	if err := releaseProof.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := base.store.mutateJournal(base.key, func(current *RunJournal) error {
		current.ContainmentAuthority = nil
		current.ContainmentHandoff = nil
		current.ContainmentCgroup = nil
		current.PID, current.ProcessIdentity, current.StartedAt = 0, "", time.Time{}
		current.ContainmentCgroupReleaseProof = &releaseProof
		return nil
	}); err != nil {
		t.Fatalf("install structural release tombstone fixture: %v", err)
	}
	candidateSubject := protocol.Subject{
		ResourceID: base.promotionIntent.ResourceID, Commit: base.promotionIntent.Commit, TreeDigest: base.promotionIntent.TreeDigest,
	}
	evidence := linuxCloneArtifactEvidence(t, base.key.RunID, candidateSubject)
	queued, err := base.store.QueueGoalEvidence(base.key, evidence)
	if err != nil || len(queued.PendingGoalDeliveries) != 1 {
		t.Fatalf("queue exact candidate artifact evidence: deliveries=%d err=%v", len(queued.PendingGoalDeliveries), err)
	}
	delivery := queued.PendingGoalDeliveries[0]
	if _, err := base.store.MarkGoalDeliveryDelivered(base.key, delivery.Kind, delivery.DeliveryID, delivery.PayloadDigest); err != nil {
		t.Fatal(err)
	}
	terminalAt := time.Date(2026, 9, 27, 3, 4, 5, 0, time.UTC)
	transition := protocol.StateTransitionRequest{
		TransitionID: transitionID, State: "completed",
		Payload: json.RawMessage(`{"task_result":{"kind":"candidate_completion"}}`),
	}
	if _, err := base.store.QueueTerminalTransitionAt(base.key, transition, terminalAt); err != nil {
		t.Fatal(err)
	}
	if _, err := base.store.MarkTransitionsDelivered(base.key, []string{transition.TransitionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := base.store.ResolveTerminalForCleanup(base.key, TerminalVerdictOwnershipLost, terminalAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if !loadStateJournal(t, base.store, base.key).LinuxCloneCleanupBeginEligible(base.key, base.fence) {
		t.Fatal("exact promoted, delivered, stopped clone was not eligible to begin cleanup")
	}
	return base
}

// newPinnedLinuxCloneCleanup persists the cleanup-ready clone into a pending
// normal cleanup stage.
func newPinnedLinuxCloneCleanup(t *testing.T, runID string) pinnedLinuxCloneBase {
	t.Helper()
	base := newPinnedLinuxCloneCleanupReady(t, runID, "pinned-clone-cleanup-completed")
	if _, err := base.store.BeginLinuxCloneCleanup(base.key, base.fence); err != nil {
		t.Fatal(err)
	}
	return base
}

// newPinnedLinuxCloneReuse imports a reuse consumer whose grant still owns its
// source target; the journal carries no clone evidence of its own. A reuse
// grant can be installed only through BindLinuxCloneReuse, which additionally
// requires a stable-stopped GoalSession for the predecessor and a stopped
// source journal, so the fixture uses the documented recovery/import seam (a
// journal-path write as in
// TestLegacyPromotedLinuxCloneJournalRestartsWithoutInventingIntent) and
// proves the base through the production loader.
func newPinnedLinuxCloneReuse(t *testing.T, runID string) pinnedLinuxCloneBase {
	t.Helper()
	base := pinnedLinuxCloneBase{store: mustStore(t)}
	_, intent := newLinuxCloneJournal(t, "pinned-reuse-source")
	seed, reuse := linuxCloneReuseGrantFixture(t, intent, runID, "pinned-reuse-predecessor")
	if err := reuse.Validate(); err != nil {
		t.Fatalf("reuse grant fixture: %v", err)
	}
	consumer := stoppedTestJournal(runID, 1)
	consumer.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	consumer.Work.Input = linuxCloneAdmissionInput(t, protocol.Subject{
		ResourceID: intent.ResourceID, Commit: intent.BaseCommit, TreeDigest: intent.BaseTreeDigest,
	})
	consumer.LinuxCloneReuseGrant = &reuse
	consumer.WorkspacePath = intent.Target
	consumer.WorkspaceBindingKey = intent.BindingKey
	consumer.WorkspaceRecoveryRequired = true
	consumer.RetainWorkspace = true
	encoded, err := serializeRunJournal(consumer)
	if err != nil {
		t.Fatalf("serialize reuse consumer journal: %v", err)
	}
	if err := os.WriteFile(base.store.journalPath(consumer.Key()), encoded, 0o600); err != nil {
		t.Fatalf("import reuse consumer journal: %v", err)
	}
	if _, err := base.store.LoadJournal(consumer.Key()); err != nil {
		t.Fatalf("load reuse consumer journal: %v", err)
	}
	base.key, base.fence, base.intent = consumer.Key(), consumer.Fence(), intent
	base.seed = seed
	return base
}

// pinnedLinuxCloneCase is one first-error pin: introduce one scenario-relevant
// inconsistency on a validated base journal and assert the validator's first
// error string.
type pinnedLinuxCloneCase struct {
	name   string
	want   string
	mutate func(*RunJournal) error
}

// TestLinuxCloneValidatorFirstErrorStringsArePinned pins the exact first
// validateLinuxCloneJournal error of every reachable block. Every case is
// driven through the production persistence seam: mutateJournal loads, mutates,
// then runs validateJournal before writing, so a rejected case returns exactly
// the validator's first error and never touches the persisted journal. Cases
// therefore share one seeded store per base stage. Every base is produced by
// production mutations except the reuse-grant base, which is imported through
// the journal-path seam because no ordinary store mutation installs a reuse
// grant (see newPinnedLinuxCloneReuse).
func TestLinuxCloneValidatorFirstErrorStringsArePinned(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)

	promoted := newPinnedLinuxCloneBase(t, "pinned-clone-promoted", "promoted")
	running := newPinnedLinuxCloneBase(t, "pinned-clone-running", "running")
	seedOnly := newPinnedLinuxCloneBase(t, "pinned-clone-seed", "seed")
	abortComplete := newPinnedLinuxCloneAbortComplete(t, "pinned-clone-abort-complete")
	// The cleanup stage parses protocol evidence, whose run_id must be a UUID
	// and must equal the journal RunID for the delivered-artifact identity.
	cleanup := newPinnedLinuxCloneCleanup(t, "00000000-0000-4000-8000-000000000078")
	reuse := newPinnedLinuxCloneReuse(t, "pinned-reuse-consumer")

	normalReleaseProof := testStateLinuxCgroupV2ReleaseDTO(promoted.handoff, promoted.stopReceipt)
	if err := normalReleaseProof.Validate(); err != nil {
		t.Fatalf("legacy normal-release fixture: %v", err)
	}
	preGateReleaseProof := authority.LinuxCgroupPreGateReleaseProof{
		Version:         authority.LinuxCgroupIdentityVersion,
		Receipt:         *pinnedJournalPreGateStopReceipt(pinnedJournalLauncherBoundCgroupReservation()),
		LeafDisposition: authority.LinuxCgroupLeafRemoved,
	}
	if err := preGateReleaseProof.Validate(); err != nil {
		t.Fatalf("pre-gate release proof fixture: %v", err)
	}
	promotedMismatchedAdmission := linuxCloneAdmissionInput(t, protocol.Subject{
		ResourceID: "00000000-0000-4000-8000-000000000097",
		Commit:     promoted.intent.BaseCommit, TreeDigest: promoted.intent.BaseTreeDigest,
	})
	reuseMismatchedAdmission := linuxCloneAdmissionInput(t, protocol.Subject{
		ResourceID: "00000000-0000-4000-8000-000000000096",
		Commit:     reuse.intent.BaseCommit, TreeDigest: reuse.intent.BaseTreeDigest,
	})
	runningHistoryProof := pinnedLinuxCloneAbortProof(t, running.intent, &running.seed)
	legacyProof := pinnedLinuxCloneAbortProof(t, seedOnly.intent, &seedOnly.seed)
	legacyProof.NormalReleaseProof = &normalReleaseProof

	promotedCases := []pinnedLinuxCloneCase{
		{
			name: "evidence without intent",
			want: "run journal has Linux clone evidence without an intent",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneIntent = nil
				return nil
			},
		},
		{
			name: "intent binding mismatch",
			want: "run journal Linux clone intent is invalid",
			mutate: func(journal *RunJournal) error {
				changed := journal.LinuxCloneIntent.Clone()
				changed.BindingKey = "other-binding"
				journal.LinuxCloneIntent = &changed
				return nil
			},
		},
		{
			name: "intent subject mismatch",
			want: "run journal Linux clone intent does not match admitted Subject",
			mutate: func(journal *RunJournal) error {
				journal.Work.Input = promotedMismatchedAdmission
				return nil
			},
		},
		{
			name: "seed receipt mismatch",
			want: "run journal Linux clone seed receipt is invalid",
			mutate: func(journal *RunJournal) error {
				changed := journal.LinuxCloneSeedReceipt.Clone()
				changed.BaseCommit = strings.Repeat("9", 40)
				journal.LinuxCloneSeedReceipt = &changed
				return nil
			},
		},
		{
			name: "abort pending and complete",
			want: "run journal Linux clone abort is both pending and complete",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneAbortPending = true
				journal.LinuxCloneAbortProof = &authority.LinuxCloneAbortProof{}
				return nil
			},
		},
		{
			name: "abort state invalid",
			want: "run journal Linux clone abort state is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneAbortPending = true
				return nil
			},
		},
		{
			name: "promotion has no checkout receipt",
			want: "run journal Linux clone promotion has no checkout receipt",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneCheckoutReceipt = nil
				return nil
			},
		},
		{
			name: "checkout receipt mismatch",
			want: "run journal Linux clone checkout receipt is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneCheckoutReceipt.SeedDigest = "sha256:" + strings.Repeat("0", 64)
				return nil
			},
		},
		{
			name: "promotion receipt has no intent",
			want: "run journal Linux clone promotion receipt has no intent",
			mutate: func(journal *RunJournal) error {
				journal.LinuxClonePromotionIntent = nil
				journal.LinuxCloneCleanupPending = true
				return nil
			},
		},
		{
			name: "target path changed",
			want: "run journal Linux clone target path changed",
			mutate: func(journal *RunJournal) error {
				journal.LinuxClonePromotionIntent = nil
				journal.LinuxClonePromotionReceipt = nil
				journal.WorkspacePath = ""
				return nil
			},
		},
		{
			name: "legacy promotion receipt invalid",
			want: "run journal legacy Linux clone promotion receipt is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxClonePromotionIntent = nil
				journal.LinuxClonePromotionReceipt.Commit = strings.Repeat("9", 39)
				return nil
			},
		},
		{
			name: "promotion intent mismatch",
			want: "run journal Linux clone promotion intent is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxClonePromotionIntent.ResourceID = "00000000-0000-4000-8000-000000000098"
				return nil
			},
		},
		{
			name: "cleanup has no promotion receipt",
			want: "run journal Linux clone cleanup has no promotion receipt",
			mutate: func(journal *RunJournal) error {
				journal.LinuxClonePromotionReceipt = nil
				journal.WorkspacePath = ""
				return nil
			},
		},
		{
			name: "promotion receipt mismatch",
			want: "run journal Linux clone promotion receipt is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxClonePromotionReceipt.TreeDigest = "sha256:" + strings.Repeat("0", 64)
				return nil
			},
		},
		{
			name: "cleanup stage invalid",
			want: "run journal Linux clone cleanup stage is invalid",
			mutate: func(journal *RunJournal) error {
				journal.WorkspacePath = ""
				return nil
			},
		},
	}

	runningCases := []pinnedLinuxCloneCase{
		{
			name: "pending abort with live owner loses producer fence",
			want: "run journal Linux clone abort lost its producer fence",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneAbortPending = true
				return nil
			},
		},
		{
			name: "abort proof lacks no-gate history",
			want: "run journal Linux clone abort proof lacks exact no-gate history",
			mutate: func(journal *RunJournal) error {
				proof := runningHistoryProof
				journal.LinuxCloneAbortProof = &proof
				journal.WorkspacePath = ""
				journal.WorkspaceRecoveryRequired = false
				return nil
			},
		},
	}

	seedCases := []pinnedLinuxCloneCase{
		{
			name: "abort conflicts with normal cleanup",
			want: "run journal clone abort conflicts with normal cleanup",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneAbortPending = true
				journal.LinuxCloneCleanupPending = true
				return nil
			},
		},
		{
			name: "pending abort loses reserved target",
			want: "run journal pending clone abort lost its reserved target",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneAbortPending = true
				journal.WorkspacePath = ""
				return nil
			},
		},
		{
			name: "abort proof invalid",
			want: "run journal Linux clone abort proof is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneAbortProof = &authority.LinuxCloneAbortProof{}
				return nil
			},
		},
		{
			name: "legacy normal-release abort unresolved",
			want: "legacy normal-release Linux clone abort remains unresolved",
			mutate: func(journal *RunJournal) error {
				proof := legacyProof
				journal.LinuxCloneAbortProof = &proof
				journal.WorkspacePath = ""
				journal.WorkspaceRecoveryRequired = false
				return nil
			},
		},
	}

	abortCompleteCases := []pinnedLinuxCloneCase{
		{
			name: "abort proof lost bound seed receipt",
			want: "run journal Linux clone abort proof lost its bound seed receipt",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneAbortProof.SeedReceipt = nil
				return nil
			},
		},
	}

	cleanupCases := []pinnedLinuxCloneCase{
		{
			name: "cleanup intent invalid",
			want: "run journal Linux clone cleanup intent is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneCleanupIntent.RootPath = "/"
				return nil
			},
		},
		{
			name: "pending cleanup stage invalid",
			want: "run journal pending Linux clone cleanup stage is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneCleanupProof = &authority.LinuxCloneCleanupProof{}
				return nil
			},
		},
		{
			name: "pending target removal proof invalid",
			want: "run journal pending Linux clone target-removal proof is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneTargetRemovalProof = &authority.LinuxCloneTargetRemovalProof{}
				return nil
			},
		},
		{
			name: "cleanup proof invalid",
			want: "run journal Linux clone cleanup proof is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneCleanupPending = false
				return nil
			},
		},
	}

	reuseCases := []pinnedLinuxCloneCase{
		{
			name: "reuse grant invalid",
			want: "run journal Linux clone reuse grant is invalid",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneReuseGrant.RunID = "pinned-other-run"
				return nil
			},
		},
		{
			name: "reuse abort lacks retirement",
			want: "run journal Linux clone reuse abort lacks retirement",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneReuseAbortedBeforeNativeStart = true
				return nil
			},
		},
		{
			name: "reuse abort follows native effect",
			want: "run journal Linux clone reuse abort follows a native or containment effect",
			mutate: func(journal *RunJournal) error {
				journal.LinuxCloneReuseAbortedBeforeNativeStart = true
				journal.LinuxCloneReuseRetired = true
				journal.PID = 42
				journal.ProcessIdentity = "linux:42:start"
				journal.StartedAt = time.Date(2026, 9, 27, 1, 1, 1, 0, time.UTC)
				return nil
			},
		},
		{
			name: "reuse abort after pending containment",
			want: "run journal Linux clone reuse abort follows a native or containment effect",
			mutate: func(journal *RunJournal) error {
				handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
				journal.ContainmentHandoff = &handoff
				journal.LinuxCloneReuseAbortedBeforeNativeStart = true
				journal.LinuxCloneReuseRetired = true
				return nil
			},
		},
		{
			name: "reuse abort after cgroup reservation",
			want: "run journal Linux clone reuse abort follows a native or containment effect",
			mutate: func(journal *RunJournal) error {
				cgroup := pinnedJournalAbortedCgroupReservation()
				journal.ContainmentCgroup = &cgroup
				journal.LinuxCloneReuseAbortedBeforeNativeStart = true
				journal.LinuxCloneReuseRetired = true
				return nil
			},
		},
		{
			name: "reuse abort after cgroup release proof",
			want: "run journal Linux clone reuse abort follows a native or containment effect",
			mutate: func(journal *RunJournal) error {
				proof := normalReleaseProof
				journal.ContainmentCgroupReleaseProof = &proof
				journal.LinuxCloneReuseAbortedBeforeNativeStart = true
				journal.LinuxCloneReuseRetired = true
				return nil
			},
		},
		{
			name: "reuse abort after pre-gate release proof",
			want: "run journal Linux clone reuse abort follows a native or containment effect",
			mutate: func(journal *RunJournal) error {
				proof := preGateReleaseProof
				journal.ContainmentCgroupPreGateReleaseProof = &proof
				journal.LinuxCloneReuseAbortedBeforeNativeStart = true
				journal.LinuxCloneReuseRetired = true
				return nil
			},
		},
		{
			name: "reuse lost retained target",
			want: "run journal Linux clone reuse lost its retained target",
			mutate: func(journal *RunJournal) error {
				journal.WorkspacePath = ""
				return nil
			},
		},
		{
			name: "reuse subject mismatch",
			want: "run journal Linux clone reuse does not match admitted Subject",
			mutate: func(journal *RunJournal) error {
				journal.Work.Input = reuseMismatchedAdmission
				return nil
			},
		},
	}

	for _, group := range []struct {
		name  string
		store *Store
		key   RunKey
		cases []pinnedLinuxCloneCase
	}{
		{name: "promoted", store: promoted.store, key: promoted.key, cases: promotedCases},
		{name: "running", store: running.store, key: running.key, cases: runningCases},
		{name: "seed", store: seedOnly.store, key: seedOnly.key, cases: seedCases},
		{name: "abort-complete", store: abortComplete.store, key: abortComplete.key, cases: abortCompleteCases},
		{name: "cleanup", store: cleanup.store, key: cleanup.key, cases: cleanupCases},
		{name: "reuse", store: reuse.store, key: reuse.key, cases: reuseCases},
	} {
		for _, test := range group.cases {
			t.Run(group.name+": "+test.name, func(t *testing.T) {
				_, err := group.store.mutateJournal(group.key, test.mutate)
				if err == nil {
					t.Fatal("journal mutation unexpectedly passed validateJournal")
				}
				if err.Error() != test.want {
					t.Fatalf("first error = %q, want %q", err.Error(), test.want)
				}
			})
		}
	}
}
