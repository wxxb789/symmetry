//go:build linux

package state

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestLegacyJournalDecodeDoesNotInventCloneEvidence(t *testing.T) {
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
		decoded.LinuxCloneCheckoutReceipt != nil || decoded.LinuxClonePromotionReceipt != nil {
		t.Fatalf("legacy journal decode fabricated Linux clone evidence: %#v", decoded)
	}
}

func TestLinuxCloneAbortFencesLaunchAndRetiresOnlyAfterAbsenceProof(t *testing.T) {
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-abort-run")
	journal.LeaseExpiresAt = time.Now().Add(time.Hour)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, intent); err != nil {
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
	if _, err := store.ReserveLinuxCgroup(key, testStateLinuxCgroupV2Reservation()); err == nil {
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

	baseRef, err := authority.LinuxCloneBaseRef(key.RunID, key.Generation)
	if err != nil {
		t.Fatal(err)
	}
	promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil {
		t.Fatal(err)
	}
	proof := authority.LinuxCloneAbortProof{
		Version: authority.LinuxCloneVersion, Intent: intent, SeedReceipt: &seed,
		BaseRef: baseRef, PromotionRef: promotionRef,
		TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
	}
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
	store := mustStore(t)
	journal := stoppedTestJournal("clone-test-never-created", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	reservation := testStateLinuxCgroupV2Reservation()
	if _, err := store.ReserveLinuxCgroup(journal.Key(), reservation); err != nil {
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
	if err := store.DeleteJournal(journal.Key()); err != nil {
		t.Fatalf("DeleteJournal() after exact no-target abort: %v", err)
	}

	startedStore := mustStore(t)
	startedJournal := stoppedTestJournal("clone-test-create-started", 1)
	if err := startedStore.SaveJournal(startedJournal); err != nil {
		t.Fatal(err)
	}
	startedReservation := testStateLinuxCgroupV2Reservation()
	startedReservation.LeafName = strings.Repeat("7", authority.TokenBytes*2)
	if _, err := startedStore.ReserveLinuxCgroup(startedJournal.Key(), startedReservation); err != nil {
		t.Fatal(err)
	}
	if _, err := startedStore.MarkLinuxCgroupCreateStarted(startedJournal.Key(), startedReservation); err != nil {
		t.Fatal(err)
	}
	startedReservation = loadStateJournal(t, startedStore, startedJournal.Key()).ContainmentCgroup.Clone()
	if _, err := startedStore.AbortNeverCreatedLinuxCgroupReservation(startedJournal.Key(), startedJournal.Fence(), startedReservation); err == nil {
		t.Fatal("AbortNeverCreatedLinuxCgroupReservation() accepted CreateStarted=true")
	}
}

func TestLinuxCloneAbortAcceptsOnlyExactCgroupNoTargetTombstones(t *testing.T) {
	requireLinuxCloneAMD64(t)
	for _, createStarted := range []bool{false, true} {
		name := "before-create"
		if createStarted {
			name = "removed-empty-leaf"
		}
		t.Run(name, func(t *testing.T) {
			store := mustStore(t)
			journal, intent := newLinuxCloneJournal(t, "clone-cgroup-abort-"+name)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ReserveLinuxCloneIntent(journal.Key(), journal.Fence(), intent); err != nil {
				t.Fatal(err)
			}
			reservation := testStateLinuxCgroupV2Reservation()
			if _, err := store.ReserveLinuxCgroup(journal.Key(), reservation); err != nil {
				t.Fatal(err)
			}
			if _, err := store.FenceLinuxCloneBeforeCheckout(journal.Key(), journal.Fence(), intent); err == nil {
				t.Fatal("clone abort fence accepted an unresolved cgroup reservation")
			}
			if createStarted {
				if _, err := store.MarkLinuxCgroupCreateStarted(journal.Key(), reservation); err != nil {
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

func TestLinuxCloneAbortNormalReleaseRequiresExactPhysicalStopTombstone(t *testing.T) {
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-normal-release-abort")
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveLinuxCloneIntent(journal.Key(), journal.Fence(), intent); err != nil {
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
	if err := proof.Validate(); err != nil {
		t.Fatalf("normal-release abort proof invalid: %v", err)
	}
	stopped := loadStateJournal(t, store, journal.Key())
	stopped.PID, stopped.ProcessIdentity, stopped.StartedAt = 0, "", time.Time{}
	stopped.ContainmentAuthority = nil
	stopped.ContainmentHandoff = nil
	stopped.ContainmentCgroup = nil
	stopped.ContainmentCgroupReleaseProof = &releaseProof
	if !linuxCloneAbortHistoryMatches(stopped, &proof) {
		t.Fatal("exact normal v2 release tombstone did not authorize the post-stop no-checkout proof path")
	}
	wrongRelease := proof.Clone()
	wrongRelease.NormalReleaseProof.LeafDisposition = authority.LinuxCgroupLeafAbsentAfterReceipt
	if err := wrongRelease.Validate(); err != nil {
		t.Fatalf("alternate valid release disposition failed structural validation: %v", err)
	}
	if linuxCloneAbortHistoryMatches(stopped, &wrongRelease) {
		t.Fatal("clone abort accepted a different durable normal-release tombstone")
	}
	missingTargetProof := proof.Clone()
	missingTargetProof.TargetAbsent = false
	if missingTargetProof.Validate() == nil {
		t.Fatal("normal release proof implied target absence without an independent target check")
	}
	missingRelease := stopped
	missingRelease.ContainmentCgroupReleaseProof = nil
	if linuxCloneAbortHistoryMatches(missingRelease, &proof) {
		t.Fatal("clone abort accepted normal stop proof without the journal's durable release tombstone")
	}
}

func TestLinuxCloneJournalCASRejectsStaleOrChangedEvidence(t *testing.T) {
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-cas-run")
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()

	staleFence := fence
	staleFence.LeaseToken = "other-lease"
	if _, err := store.ReserveLinuxCloneIntent(key, staleFence, intent); err == nil {
		t.Fatal("ReserveLinuxCloneIntent() accepted a stale fence")
	}
	if _, err := store.ReserveLinuxCloneIntent(RunKey{RunID: key.RunID, Generation: key.Generation + 1}, fence, intent); err == nil {
		t.Fatal("ReserveLinuxCloneIntent() accepted a stale RunKey")
	}

	replayUnknownStateMutation(t, store, func() error {
		_, err := store.ReserveLinuxCloneIntent(key, fence, intent)
		return err
	})
	changedIntent := intent
	changedIntent.Nonce = strings.Repeat("d", authority.LinuxCloneNonceBytes*2)
	if _, err := store.ReserveLinuxCloneIntent(key, fence, changedIntent); err == nil {
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
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	firstJournal, firstIntent := newLinuxCloneJournal(t, "clone-path-first")
	secondJournal, secondIntent := newLinuxCloneJournal(t, "clone-path-seed-alias")
	thirdJournal, thirdIntent := newLinuxCloneJournal(t, "clone-path-target-alias")
	fourthJournal, fourthIntent := newLinuxCloneJournal(t, "clone-path-cross-alias")
	for _, journal := range []RunJournal{firstJournal, secondJournal, thirdJournal, fourthJournal} {
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
	}
	key, fence := firstJournal.Key(), firstJournal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, firstIntent); err != nil {
		t.Fatal(err)
	}

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

	secondIntent.SeedPath = firstIntent.SeedPath
	if _, err := store.ReserveLinuxCloneIntent(secondJournal.Key(), secondJournal.Fence(), secondIntent); err == nil {
		t.Fatal("ReserveLinuxCloneIntent() reserved a seed path already owned by another RunKey")
	}
	thirdIntent.Target = firstIntent.Target
	if _, err := store.ReserveLinuxCloneIntent(thirdJournal.Key(), thirdJournal.Fence(), thirdIntent); err == nil {
		t.Fatal("ReserveLinuxCloneIntent() reserved a target path already owned by another RunKey")
	}
	fourthIntent.SeedPath = firstIntent.Target
	if _, err := store.ReserveLinuxCloneIntent(fourthJournal.Key(), fourthJournal.Fence(), fourthIntent); err == nil {
		t.Fatal("ReserveLinuxCloneIntent() allowed one run's seed path to alias another run's target")
	}
}

func TestLinuxCloneCleanupRequiresDeliveredCandidateArtifact(t *testing.T) {
	journal, intent := newLinuxCloneJournal(t, "clone-artifact-gate")
	journal.Work.Input = linuxCloneAdmissionInput(t, protocol.Subject{
		ResourceID: intent.ResourceID, Commit: intent.BaseCommit, TreeDigest: intent.BaseTreeDigest,
	})
	journal.GoalDeliveryEnabled = true
	journal.TerminalState = "completed"
	journal.TerminalTaskResultKind = protocol.TaskResultCandidateCompletion
	if !linuxCloneArtifactReceiptRequired(journal) {
		t.Fatal("completed candidate clone was cleanup-eligible without delivered artifact evidence")
	}
	evidence := protocol.Evidence{Kind: protocol.EvidenceArtifact}
	journal.DeliveredGoalDeliveries = []GoalDelivery{{Kind: GoalDeliveryEvidence, Evidence: &evidence}}
	if linuxCloneArtifactReceiptRequired(journal) {
		t.Fatal("durably delivered artifact evidence did not satisfy candidate cleanup gate")
	}
}

func TestDeleteJournalKeepsCandidateCloneWithoutDeliveredArtifact(t *testing.T) {
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-candidate-artifact-gate")
	journal.GoalDeliveryEnabled = true
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FenceLinuxCloneBeforeCheckout(key, fence, intent); err != nil {
		t.Fatal(err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(key.RunID, key.Generation)
	if err != nil {
		t.Fatal(err)
	}
	promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil {
		t.Fatal(err)
	}
	proof := authority.LinuxCloneAbortProof{
		Version: authority.LinuxCloneVersion, Intent: intent, BaseRef: baseRef, PromotionRef: promotionRef,
		TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
	}
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

func TestLinuxCloneCheckoutAndPromotionPreserveContainmentAndReplayExactly(t *testing.T) {
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	journal, intent := newLinuxCloneJournal(t, "clone-staged-run")
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key, fence := journal.Key(), journal.Fence()
	if _, err := store.ReserveLinuxCloneIntent(key, fence, intent); err != nil {
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
	promotion := authority.LinuxClonePromotionReceipt{
		Version: authority.LinuxCloneVersion, Commit: strings.Repeat("e", 40),
		TreeDigest: "sha256:" + strings.Repeat("f", 64), Ref: promotionRef,
	}
	if _, err := store.RecordLinuxClonePromotion(key, fence, intent, promotion); err == nil {
		t.Fatal("RecordLinuxClonePromotion() accepted a candidate before durable stop proof")
	}
	owner := owned.ContainmentAuthority.Clone()
	stopReceipt := testStateLinuxCgroupV2Receipt(owner, authority.LinuxCgroupStopProofHelper, nil)
	if _, err := store.RecordContainmentStopReceipt(key, owner.TargetPID, owner.TargetIdentity, stopReceipt); err != nil {
		t.Fatalf("record exact v2 stop proof: %v", err)
	}
	wrongRef := promotion
	wrongRef.Ref = "refs/heads/symmetry/clone-v2/base/" + strings.Repeat("a", 64)
	if _, err := store.RecordLinuxClonePromotion(key, fence, intent, wrongRef); err == nil {
		t.Fatal("RecordLinuxClonePromotion() accepted a base ref or candidate ref mismatch")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordLinuxClonePromotion(key, fence, intent, promotion)
		return err
	})
	loaded := loadStateJournal(t, store, key)
	if loaded.LinuxClonePromotionReceipt == nil || !loaded.LinuxClonePromotionReceipt.Equal(promotion) ||
		loaded.ContainmentAuthority == nil || loaded.ContainmentCgroup == nil || !loaded.HasPendingContainment() {
		t.Fatalf("promotion changed or cleared containment authority: %#v", loaded)
	}

	changedPromotion := promotion
	changedPromotion.Commit = strings.Repeat("0", 40)
	if _, err := store.RecordLinuxClonePromotion(key, fence, intent, changedPromotion); err == nil {
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
		name   string
		mutate func() error
	}{
		{name: "intent", mutate: func() error { _, err := store.ReserveLinuxCloneIntent(key, fence, intent); return err }},
		{name: "seed", mutate: func() error { _, err := store.BindLinuxCloneSeed(key, fence, intent, seed); return err }},
		{name: "checkout", mutate: func() error { _, err := store.RecordLinuxCloneCheckoutReady(key, fence, intent, checkout); return err }},
		{name: "promotion", mutate: func() error { _, err := store.RecordLinuxClonePromotion(key, fence, intent, promotion); return err }},
	} {
		t.Run("terminal replay "+replay.name, func(t *testing.T) {
			replayUnknownStateMutation(t, store, replay.mutate)
		})
	}
}

func newLinuxCloneJournal(t *testing.T, runID string) (RunJournal, authority.LinuxCloneIntent) {
	t.Helper()
	journal := stoppedTestJournal(runID, 1)
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
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: runID, Generation: 1, BindingKey: "primary", ResourceID: subject.ResourceID,
		BaseCommit: subject.Commit, BaseTreeDigest: subject.TreeDigest,
		Nonce:    strings.Repeat("c", authority.LinuxCloneNonceBytes*2),
		SeedPath: filepath.Join(t.TempDir(), "seed.bundle"),
		Target:   filepath.Join(t.TempDir(), "checkout"),
	}
	return journal, intent
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

func requireLinuxCloneAMD64(t *testing.T) {
	t.Helper()
	if runtime.GOARCH != "amd64" {
		t.Skip("Linux clone-v2 state is Linux/amd64 only")
	}
}
