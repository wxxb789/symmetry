//go:build linux

package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

func TestLinuxCgroupV2ReservationAndCommitReplayUseFreshWriteBarriers(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("linux-cgroup-v2-replay", 1)
	journal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	fence := journal.Fence()
	reservation := testStateLinuxCgroupV2Reservation()
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.ReserveLinuxCgroup(key, fence, reservation, nil)
		return err
	})

	loaded := loadStateJournal(t, store, key)
	if loaded.ContainmentCgroup == nil || loaded.ContainmentCgroup.CreateStarted || loaded.HasProcessDetails() || !loaded.HasPendingContainment() {
		t.Fatalf("reserved journal = %#v, want pending reservation before creation with no PID", loaded)
	}
	reservation = loaded.ContainmentCgroup.Clone()
	otherRun := stoppedTestJournal("linux-cgroup-v2-duplicate-leaf", 1)
	otherRun.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(otherRun); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveLinuxCgroup(otherRun.Key(), otherRun.Fence(), reservation, nil); err == nil {
		t.Fatal("ReserveLinuxCgroup() reserved the same exclusive parent/name for another RunKey")
	}
	if err := store.DeleteJournal(otherRun.Key()); err != nil {
		t.Fatalf("remove failed duplicate-leaf target: %v", err)
	}

	different := reservation
	different.LeafName = strings.Repeat("f", authority.TokenBytes*2)
	if _, err := store.ReserveLinuxCgroup(key, fence, different, nil); err == nil {
		t.Fatal("ReserveLinuxCgroup() replaced an existing immutable leaf reservation")
	}
	if _, err := store.ClearProcessDetails(key, 400, "gate:400:start"); err == nil {
		t.Fatal("v1 ClearProcessDetails() erased a v2 reservation without a PID")
	}
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() removed a pending v2 reservation without a PID")
	}

	createExpected := reservation.Clone()
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.MarkLinuxCgroupCreateStarted(key, fence, createExpected, nil)
		return err
	})
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if !reservation.CreateStarted || reservation.LeafIdentity != nil {
		t.Fatalf("creation-intent reservation = %#v, want durable intent before mkdir result", reservation)
	}

	leaf := testStateLinuxCgroupV2Leaf(reservation)
	badLeaf := leaf
	badLeaf.Parent.Inode++
	if _, err := store.BindLinuxCgroupLeaf(key, reservation, badLeaf); err == nil {
		t.Fatal("BindLinuxCgroupLeaf() accepted a leaf under another delegated-parent identity")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.BindLinuxCgroupLeaf(key, reservation, leaf)
		return err
	})
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if reservation.LeafIdentity == nil || !reservation.LeafIdentity.Equal(leaf) {
		t.Fatalf("leaf binding = %#v, want exact measured cgroup object", reservation.LeafIdentity)
	}
	clonedReservation := reservation.Clone()
	if !reservation.Equal(clonedReservation) {
		t.Fatal("LinuxCgroupReservation.Clone() did not preserve exact equality")
	}
	clonedReservation.LeafIdentity.LeafInode++
	if reservation.Equal(clonedReservation) || reservation.LeafIdentity.LeafInode == clonedReservation.LeafIdentity.LeafInode {
		t.Fatal("LinuxCgroupReservation.Clone() shared mutable leaf identity")
	}

	helper := testStateLinuxCgroupV2Helper()
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.BindLinuxCgroupHelper(key, reservation, helper)
		return err
	})
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()

	launcher := testStateLinuxCgroupV2Launcher()
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.BindLinuxCgroupLauncher(key, reservation, launcher)
		return err
	})
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.ArmLinuxCgroupLauncherGrant(key, fence, reservation, nil)
		return err
	})
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()

	launcherExit := testStateLinuxCgroupV2Exit(launcher, *reservation.Helper)
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordLinuxCgroupLauncherExit(key, reservation, launcherExit)
		return err
	})
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()

	gate := testStateLinuxCgroupV2Gate(reservation)
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.BindLinuxCgroupGate(key, reservation, gate)
		return err
	})
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()

	handoff := testStateLinuxCgroupV2Handoff(reservation)
	wrongUID := handoff.Clone()
	wrongUID.OwnerUID++
	if _, err := store.PrepareSupervisorHandoff(key, wrongUID); err == nil {
		t.Fatal("PrepareSupervisorHandoff() accepted a gate UID outside reservation owner UID-A")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.PrepareSupervisorHandoff(key, handoff)
		return err
	})
	boundResult := RunJournal{}
	replayUnknownStateMutation(t, store, func() error {
		var err error
		boundResult, err = store.BindSupervisorHandoffV2(key, handoff, helper, testStateLinuxCgroupV2Mirror())
		return err
	})
	if boundResult.ContainmentHandoff == nil || boundResult.ContainmentHandoff.Mirror == nil || boundResult.ContainmentScans != nil {
		t.Fatalf("v2 bound handoff = %#v, want mirror identity and no v1 scan ledger", boundResult)
	}
	boundHandoff := boundResult.ContainmentHandoff.Clone()
	startedAt := time.Date(2026, 9, 26, 6, 7, 8, 0, time.UTC)
	if _, err := store.CommitSupervisorHandoff(key, boundHandoff, startedAt.Add(time.Second)); err == nil {
		t.Fatal("CommitSupervisorHandoff() accepted a timestamp different from the durable gate observation")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.CommitSupervisorHandoff(key, boundHandoff, startedAt)
		return err
	})
	committed := loadStateJournal(t, store, key)
	if committed.ContainmentHandoff != nil || committed.ContainmentAuthority == nil ||
		committed.ContainmentAuthority.Version != authority.SupervisorVersionV2 || committed.ContainmentCgroup == nil ||
		committed.ContainmentScans != nil || committed.PID != gate.Process.PID || committed.ProcessIdentity != gate.Process.Identity {
		t.Fatalf("committed v2 journal = %#v, want exact authority and retained reservation", committed)
	}

	var unissuedRelease platform.LinuxCgroupV2Release
	if _, err := store.ClearProcessDetailsV2(key, gate.Process.PID, gate.Process.Identity, unissuedRelease); err == nil {
		t.Fatal("ClearProcessDetailsV2() accepted the unissued zero-value platform release")
	}
	value := committed.ContainmentAuthority.Clone()
	receipt := testStateLinuxCgroupV2Receipt(value, authority.LinuxCgroupStopProofHelper, nil)
	if _, err := store.RecordContainmentStopReceipt(key, value.TargetPID, value.TargetIdentity, testStateLinuxV1Receipt(value)); err == nil {
		t.Fatal("v1 group receipt was accepted for a v2 cgroup owner")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordContainmentStopReceipt(key, value.TargetPID, value.TargetIdentity, receipt)
		return err
	})
	withReceipt := loadStateJournal(t, store, key)
	if withReceipt.ContainmentAuthority == nil || withReceipt.ContainmentAuthority.StopReceipt == nil ||
		!withReceipt.ContainmentAuthority.StopReceipt.Equal(receipt) || withReceipt.ContainmentScans != nil ||
		withReceipt.ContainmentObservationUnresolved() {
		t.Fatalf("durable v2 stop receipt state = %#v, unresolved=%v", withReceipt, withReceipt.ContainmentObservationUnresolved())
	}
	if _, err := store.ClearProcessDetailsV2(key, value.TargetPID, value.TargetIdentity, unissuedRelease); err == nil {
		t.Fatal("ClearProcessDetailsV2() accepted an unissued release despite a durable stop receipt")
	}
	stillOwned := loadStateJournal(t, store, key)
	if stillOwned.ContainmentAuthority == nil || stillOwned.ContainmentCgroup == nil ||
		stillOwned.ContainmentAuthority.StopReceipt == nil || stillOwned.ContainmentCgroupReleaseProof != nil {
		t.Fatalf("unissued release changed durable owner state: %#v", stillOwned)
	}
}

func TestLinuxCgroupV2MirrorStopReceiptRequiresOpaqueRelease(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("linux-cgroup-v2-mirror", 1)
	journal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	bound, reservation := bindStateLinuxCgroupV2Handoff(t, store, key)
	helperExit := testStateLinuxCgroupV2Exit(*reservation.Helper, reservation.Anchor)
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordLinuxCgroupHelperExit(key, reservation, helperExit)
		return err
	})
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if _, err := store.CommitSupervisorHandoff(key, bound, time.Date(2026, 9, 26, 6, 7, 8, 0, time.UTC)); err == nil {
		t.Fatal("CommitSupervisorHandoff() authorized a gate after exact helper exit")
	}
	if _, err := store.ClearSupervisorHandoff(key, bound, bound.ReleaseProof()); err == nil {
		t.Fatal("generic ClearSupervisorHandoff() accepted a v2 authority DTO")
	}
	value, err := bound.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	receipt := testStateLinuxCgroupV2Receipt(value, authority.LinuxCgroupStopProofMirror, reservation.HelperExit)
	if _, err := store.RecordSupervisorHandoffStopReceipt(key, bound, testStateLinuxV1Receipt(value)); err == nil {
		t.Fatal("v1 stop receipt was accepted for a pending v2 handoff")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordSupervisorHandoffStopReceipt(key, bound, receipt)
		return err
	})
	withReceipt := loadStateJournal(t, store, key)
	if withReceipt.ContainmentHandoff == nil || withReceipt.ContainmentHandoff.StopReceipt == nil ||
		!withReceipt.ContainmentHandoff.StopReceipt.Equal(receipt) || withReceipt.ContainmentScans != nil ||
		withReceipt.ContainmentObservationUnresolved() {
		t.Fatalf("mirror stop receipt state = %#v", withReceipt)
	}
	directDTO := testStateLinuxCgroupV2ReleaseDTO(*withReceipt.ContainmentHandoff, receipt)
	if err := directDTO.Validate(); err != nil {
		t.Fatalf("structurally complete DTO fixture invalid: %v", err)
	}
	if _, err := store.ClearSupervisorHandoff(key, *withReceipt.ContainmentHandoff, directDTO); err == nil {
		t.Fatal("generic authority DTO cleared a receipt-bearing v2 handoff")
	}

	var unissuedRelease platform.LinuxCgroupV2Release
	if _, err := store.ClearSupervisorHandoffV2(key, *withReceipt.ContainmentHandoff, unissuedRelease); err == nil {
		t.Fatal("ClearSupervisorHandoffV2() accepted the unissued zero-value platform release")
	}
	stillPending := loadStateJournal(t, store, key)
	if stillPending.ContainmentHandoff == nil || stillPending.ContainmentHandoff.StopReceipt == nil ||
		stillPending.ContainmentCgroup == nil || stillPending.ContainmentCgroupReleaseProof != nil {
		t.Fatalf("unissued release changed pending v2 handoff: %#v", stillPending)
	}
}

func TestLinuxCgroupV2PreGateStopReceiptKeepsReservationPendingUntilOpaqueRemoval(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("linux-cgroup-v2-pre-gate-stop", 1)
	journal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	reservation := bindStateLinuxCgroupV2PreGateReservation(t, store, key)
	authenticator, err := linuxgate.SignGateNeverProducedAuthenticator(reservation.Secret, reservation.LaunchToken, *reservation.Launcher)
	if err != nil {
		t.Fatal(err)
	}
	proof := authority.LinuxCgroupPreGateStopProof{
		Version:      authority.LinuxCgroupIdentityVersion,
		Kind:         authority.LinuxCgroupStopProofHelper,
		Issuer:       *reservation.Helper,
		Reaper:       *reservation.Helper,
		PinnedLeafFD: true, CgroupKillAcknowledged: true, CgroupPopulatedZero: true,
		NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: *reservation.LauncherExit,
		GateOutcome: authority.LinuxCgroupGateProductionResult{
			Version: authority.LinuxCgroupIdentityVersion, LaunchToken: reservation.LaunchToken,
			Launcher: *reservation.Launcher, Outcome: authority.LinuxCgroupGateOutcomeNeverCreated,
			Authenticator: authenticator,
		},
	}
	receipt := authority.LinuxCgroupPreGateStopReceipt{
		Version: authority.LinuxCgroupIdentityVersion, LaunchToken: reservation.LaunchToken,
		CgroupIdentity: *reservation.LeafIdentity, OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
		Anchor: reservation.Anchor, Helper: *reservation.Helper, Launcher: *reservation.Launcher,
		Mirror: *reservation.Mirror, Proof: proof,
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("structurally complete pre-gate stop receipt invalid: %v", err)
	}
	badReceipt := receipt.Clone()
	badReceipt.Proof.GateOutcome.Authenticator = strings.Repeat("f", 64)
	if _, err := store.RecordLinuxCgroupPreGateStopReceipt(key, reservation, badReceipt); err == nil {
		t.Fatal("pre-gate stop receipt accepted a launcher MAC for another outcome")
	}
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.RecordLinuxCgroupPreGateStopReceipt(key, reservation, receipt)
		return err
	})
	stopped := loadStateJournal(t, store, key)
	if stopped.HasProcessDetails() || stopped.ContainmentCgroup == nil || stopped.ContainmentCgroup.Gate != nil ||
		stopped.ContainmentCgroup.PreGateStopReceipt == nil ||
		!stopped.ContainmentCgroup.PreGateStopReceipt.Equal(receipt) || !stopped.HasPendingContainment() ||
		stopped.ContainmentObservationUnresolved() {
		t.Fatalf("pre-gate receipt state lost PID-zero reservation or stop proof: %#v", stopped)
	}
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() removed a grant-armed pre-gate reservation before physical removal")
	}
	transition := protocol.StateTransitionRequest{TransitionID: "pre-gate-terminal", State: "failed", Payload: json.RawMessage(`{}`)}
	terminalAt := time.Date(2026, 9, 26, 9, 10, 11, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, transition, terminalAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, terminalAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(key); err == nil {
		t.Fatal("workspace cleanup advanced past pending pre-gate leaf removal")
	}
	var unissued platform.LinuxCgroupV2Release
	if _, err := store.ClearLinuxCgroupReservationV2(key, *stopped.ContainmentCgroup, unissued); err == nil {
		t.Fatal("PID-zero reservation cleared without an issued platform release")
	}
	if _, err := store.AbortLinuxCgroupReservation(key, *stopped.ContainmentCgroup,
		testStateLinuxCgroupV2AbortProof(*stopped.ContainmentCgroup)); err == nil {
		t.Fatal("grant-armed pre-gate receipt fell back to the no-target abort path")
	}
}

func TestLinuxCgroupReservationBlocksWorkspaceCleanupAndRequiresIndependentAbortProof(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("linux-cgroup-v2-abort", 1)
	journal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	fence := journal.Fence()
	reservation := testStateLinuxCgroupV2Reservation()
	if _, err := store.ReserveLinuxCgroup(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}

	loaded := loadStateJournal(t, store, key)
	if loaded.HasProcessDetails() || !loaded.HasPendingContainment() || !loaded.ContainmentObservationUnresolved() {
		t.Fatalf("reservation without PID was not pending containment: %#v", loaded)
	}
	transition := protocol.StateTransitionRequest{TransitionID: "terminal-failed", State: "failed", Payload: json.RawMessage(`{}`)}
	pendingAt := time.Date(2026, 9, 26, 8, 9, 10, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, transition, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, pendingAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(key); err == nil {
		t.Fatal("EnterCleanupPending() ignored a reservation without a PID")
	}
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() ignored a reservation without a PID")
	}
	journalWithoutReservation := loadStateJournal(t, store, key)
	journalWithoutReservation.ContainmentCgroup = nil
	if err := store.SaveJournal(journalWithoutReservation); err == nil {
		t.Fatal("SaveJournal() bypassed the dedicated v2 reservation clear")
	}

	proof := testStateLinuxCgroupV2AbortProof(reservation)
	proof.Parent.Inode++
	if _, err := store.AbortLinuxCgroupReservation(key, reservation, proof); err == nil {
		t.Fatal("AbortLinuxCgroupReservation() accepted a changed exclusive parent")
	}
	proof = testStateLinuxCgroupV2AbortProof(reservation)
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.AbortLinuxCgroupReservation(key, reservation, proof)
		return err
	})
	cleared := loadStateJournal(t, store, key)
	if cleared.ContainmentCgroup == nil || cleared.ContainmentCgroup.AbortProof == nil ||
		!cleared.ContainmentCgroup.AbortProof.Equal(proof) || cleared.HasPendingContainment() || cleared.HasProcessDetails() {
		t.Fatalf("pre-effect abort did not retain its exact non-pending replay receipt: %#v", cleared)
	}
	if _, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, pendingAt.Add(2*time.Second)); err != nil {
		t.Fatalf("terminal cleanup did not become eligible after exact abort: %v", err)
	}

	postIntent := stoppedTestJournal("linux-cgroup-v2-abort-after-intent", 1)
	postIntent.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(postIntent); err != nil {
		t.Fatal(err)
	}
	newReservation := testStateLinuxCgroupV2Reservation()
	newReservation.LaunchToken = strings.Repeat("1", authority.TokenBytes*2)
	newReservation.Secret = strings.Repeat("2", authority.SecretBytes*2)
	newReservation.PipeToken = strings.Repeat("3", authority.TokenBytes*2)
	newReservation.JobID = strings.Repeat("4", authority.TokenBytes*2)
	newReservation.LeafName = strings.Repeat("f", authority.TokenBytes*2)
	if _, err := store.ReserveLinuxCgroup(postIntent.Key(), postIntent.Fence(), newReservation, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(postIntent.Key(), postIntent.Fence(), newReservation, nil); err != nil {
		t.Fatal(err)
	}
	newReservation = loadStateJournal(t, store, postIntent.Key()).ContainmentCgroup.Clone()
	proofAfterIntent := testStateLinuxCgroupV2AbortProof(newReservation)
	proofAfterIntent.LeafNeverAppeared = false
	if _, err := store.AbortLinuxCgroupReservation(postIntent.Key(), newReservation, proofAfterIntent); err == nil {
		t.Fatal("post-intent abort inferred never-created from absence without independent proof")
	}
	proofAfterIntent = testStateLinuxCgroupV2AbortProof(newReservation)
	if _, err := store.AbortLinuxCgroupReservation(postIntent.Key(), newReservation, proofAfterIntent); err != nil {
		t.Fatalf("exact never-appeared abort proof error = %v", err)
	}
	if err := store.DeleteJournal(postIntent.Key()); err != nil {
		t.Fatalf("DeleteJournal() after exact never-created abort = %v", err)
	}

	t.Run("empty leaf rmdir after no-target proof", func(t *testing.T) {
		removalJournal := stoppedTestJournal("linux-cgroup-v2-abort-empty-leaf", 1)
		removalJournal.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
		if err := store.SaveJournal(removalJournal); err != nil {
			t.Fatal(err)
		}
		removalReservation := testStateLinuxCgroupV2Reservation()
		removalReservation.LeafName = strings.Repeat("7", authority.TokenBytes*2)
		removalReservation.LaunchToken = strings.Repeat("8", authority.TokenBytes*2)
		removalReservation.Secret = strings.Repeat("9", authority.SecretBytes*2)
		removalReservation.PipeToken = strings.Repeat("a", authority.TokenBytes*2)
		removalReservation.JobID = strings.Repeat("b", authority.TokenBytes*2)
		if _, err := store.ReserveLinuxCgroup(removalJournal.Key(), removalJournal.Fence(), removalReservation, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := store.MarkLinuxCgroupCreateStarted(removalJournal.Key(), removalJournal.Fence(), removalReservation, nil); err != nil {
			t.Fatal(err)
		}
		removalReservation = loadStateJournal(t, store, removalJournal.Key()).ContainmentCgroup.Clone()
		leaf := testStateLinuxCgroupV2Leaf(removalReservation)
		if _, err := store.BindLinuxCgroupLeaf(removalJournal.Key(), removalReservation, leaf); err != nil {
			t.Fatal(err)
		}
		removalReservation = loadStateJournal(t, store, removalJournal.Key()).ContainmentCgroup.Clone()
		helper := testStateLinuxCgroupV2Helper()
		if _, err := store.BindLinuxCgroupHelper(removalJournal.Key(), removalReservation, helper); err != nil {
			t.Fatal(err)
		}
		removalReservation = loadStateJournal(t, store, removalJournal.Key()).ContainmentCgroup.Clone()
		helperExit := testStateLinuxCgroupV2Exit(helper, removalReservation.Anchor)
		if _, err := store.RecordLinuxCgroupHelperExit(removalJournal.Key(), removalReservation, helperExit); err != nil {
			t.Fatal(err)
		}
		removalReservation = loadStateJournal(t, store, removalJournal.Key()).ContainmentCgroup.Clone()
		launcher := testStateLinuxCgroupV2Launcher()
		if _, err := store.BindLinuxCgroupLauncher(removalJournal.Key(), removalReservation, launcher); err != nil {
			t.Fatal(err)
		}
		removalReservation = loadStateJournal(t, store, removalJournal.Key()).ContainmentCgroup.Clone()
		if _, err := store.RecordLinuxCgroupLauncherExit(removalJournal.Key(), removalReservation, testStateLinuxCgroupV2Exit(launcher, *removalReservation.Helper)); err != nil {
			t.Fatal(err)
		}
		removalReservation = loadStateJournal(t, store, removalJournal.Key()).ContainmentCgroup.Clone()
		removalProof := testStateLinuxCgroupV2RemovedLeafAbortProof(removalReservation, authority.LinuxCgroupAbortLeafRemovedEmpty)
		removalProof.LeafEmpty = false
		if _, err := store.AbortLinuxCgroupReservation(removalJournal.Key(), removalReservation, removalProof); err == nil {
			t.Fatal("pre-effect empty-leaf abort accepted missing positive empty proof")
		}
		removalProof = testStateLinuxCgroupV2RemovedLeafAbortProof(removalReservation, authority.LinuxCgroupAbortLeafAbsentAfterRmdir)
		replayUnknownStateMutation(t, store, func() error {
			_, err := store.AbortLinuxCgroupReservation(removalJournal.Key(), removalReservation, removalProof)
			return err
		})
		aborted := loadStateJournal(t, store, removalJournal.Key())
		if aborted.ContainmentCgroup == nil || aborted.ContainmentCgroup.AbortProof == nil ||
			!aborted.ContainmentCgroup.AbortProof.Equal(removalProof) || aborted.HasPendingContainment() ||
			aborted.ContainmentScans != nil || aborted.ContainmentAuthority != nil || aborted.ContainmentHandoff != nil {
			t.Fatalf("unknown rmdir no-target abort did not persist exact absence proof: %#v", aborted)
		}
	})
}

func TestLinuxV2AdmissionRejectsTerminalUnremovedAndUnprovenForeignOwners(t *testing.T) {
	requireLinuxCloneAMD64(t)
	for _, scenario := range []string{"terminal-empty-pid-unremoved-leaf", "migration-unproven"} {
		t.Run(scenario, func(t *testing.T) {
			store := mustStore(t)
			target, targetIntent := newLinuxCloneJournal(t, "admission-target-"+scenario)
			target.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
			foreign := stoppedTestJournal("admission-foreign-"+scenario, 1)
			foreign.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
			if err := store.SaveJournal(foreign); err != nil {
				t.Fatal(err)
			}
			foreignReservation := reserveStateLinuxV2AdmissionLeaf(t, store, foreign, strings.Repeat("e", authority.TokenBytes*2))
			if foreignReservation.LeafIdentity == nil {
				t.Fatal("foreign fixture lacks a still-bound cgroup leaf")
			}
			switch scenario {
			case "terminal-empty-pid-unremoved-leaf":
				if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
					journal.LocalState = "cleanup_pending"
					journal.TerminalPendingAt = time.Now().UTC().Add(-time.Minute)
					journal.TerminalState = "failed"
					journal.TerminalVerdict = TerminalVerdictOwnershipLost
					journal.TerminalResolvedAt = time.Now().UTC()
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			case "migration-unproven":
				if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
					journal.ContainmentUnproven = true
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			loadedForeign := loadStateJournal(t, store, foreign.Key())
			if loadedForeign.PID != 0 || loadedForeign.ContainmentCgroup == nil || loadedForeign.ContainmentCgroup.LeafIdentity == nil ||
				loadedForeign.ContainmentCgroupReleaseProof != nil {
				t.Fatalf("foreign unresolved leaf fixture = %#v", loadedForeign)
			}

			foreignBefore := readStateJournalBytes(t, store, foreign.Key())
			now := time.Now().UTC()
			if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(now, nil); err == nil {
				t.Fatal("preclaim dispatch admitted a new native assignment with an unresolved foreign v2 leaf")
			}
			if err := store.SaveJournal(target); err != nil {
				t.Fatal(err)
			}
			targetBefore := readStateJournalBytes(t, store, target.Key())
			if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), now, nil); err == nil {
				t.Fatal("native admission accepted an unresolved foreign v2 leaf")
			}
			if _, err := store.ReserveLinuxCloneIntent(target.Key(), target.Fence(), targetIntent, nil); err == nil {
				t.Fatal("clone-intent CAS admitted A seed effects beside an unresolved foreign v2 owner")
			}
			targetReservation := testStateLinuxCgroupV2Reservation()
			targetReservation.LeafName = strings.Repeat("f", authority.TokenBytes*2)
			if _, err := store.ReserveLinuxCgroup(target.Key(), target.Fence(), targetReservation, nil); err == nil {
				t.Fatal("cgroup reservation CAS admitted a fresh native owner beside an unresolved foreign v2 leaf")
			}
			if after := readStateJournalBytes(t, store, target.Key()); !bytes.Equal(targetBefore, after) {
				t.Fatal("rejected Linux v2 admission changed the target RunJournal")
			}
			if after := readStateJournalBytes(t, store, foreign.Key()); !bytes.Equal(foreignBefore, after) {
				t.Fatal("rejected Linux v2 admission changed the foreign RunJournal")
			}
		})
	}
}

func TestLinuxV2AdmissionFailsClosedOnCorruptForeignJournal(t *testing.T) {
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	target, intent := newLinuxCloneJournal(t, "admission-corrupt-target")
	if err := store.SaveJournal(target); err != nil {
		t.Fatal(err)
	}
	foreign := stoppedTestJournal("admission-corrupt-foreign", 1)
	if err := store.SaveJournal(foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.journalPath(foreign.Key()), []byte(`{"run_id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	targetBefore := readStateJournalBytes(t, store, target.Key())
	if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(time.Now().UTC(), nil); err == nil {
		t.Fatal("preclaim State admission ignored a corrupt foreign RunJournal")
	}
	if _, err := store.ReserveLinuxCloneIntent(target.Key(), target.Fence(), intent, nil); err == nil {
		t.Fatal("clone-intent State CAS proceeded with a corrupt foreign RunJournal")
	}
	if after := readStateJournalBytes(t, store, target.Key()); !bytes.Equal(targetBefore, after) {
		t.Fatal("corrupt foreign-journal refusal changed the target RunJournal")
	}
}

func TestLinuxV2AdmissionRequiresLegacyReleaseEvidence(t *testing.T) {
	requireLinuxCloneAMD64(t)
	for _, scenario := range []string{"claiming-without-grant", "running-before-process-registration"} {
		t.Run(scenario, func(t *testing.T) {
			store := mustStore(t)
			foreign := stoppedTestJournal("admission-legacy-"+scenario, 1)
			foreign.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
			if scenario == "claiming-without-grant" {
				foreign.LocalState = "claiming"
				foreign.LeaseToken = ""
				foreign.LeaseExpiresAt = time.Time{}
				foreign.WorkspacePath = ""
			}
			if err := store.SaveJournal(foreign); err != nil {
				t.Fatal(err)
			}
			if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(time.Now().UTC(), nil); err == nil {
				t.Fatal("legacy row without a durable no-launch/release proof was admitted")
			}
		})
	}

	t.Run("bound v1 stop receipt only until normal clear and deletion", func(t *testing.T) {
		store := mustStore(t)
		foreign := testJournal("admission-v1-stop-receipt", 1)
		foreign.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
		if err := store.SaveJournal(foreign); err != nil {
			t.Fatal(err)
		}
		owner := testContainmentAuthority(foreign.PID, foreign.ProcessIdentity)
		if _, err := store.SetContainmentAuthority(foreign.Key(), foreign.PID, foreign.ProcessIdentity, owner); err != nil {
			t.Fatal(err)
		}
		receipt := authority.StopReceipt{
			Version: authority.SupervisorVersion, Status: "stopped", TargetPID: owner.TargetPID,
			TargetIdentity: owner.TargetIdentity, PipeToken: owner.PipeToken, JobID: owner.JobID,
			SupervisorPID: owner.SupervisorPID, SupervisorIdentity: owner.SupervisorIdentity,
		}
		if _, err := store.RecordContainmentStopReceipt(foreign.Key(), foreign.PID, foreign.ProcessIdentity, receipt); err != nil {
			t.Fatal(err)
		}
		terminalAt := time.Now().UTC()
		if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
			journal.LocalState = "terminal_pending"
			journal.TerminalPendingAt = terminalAt
			journal.TerminalState = "failed"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		boundReceiptBytes := readStateJournalBytes(t, store, foreign.Key())
		if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(time.Now().UTC(), nil); err != nil {
			t.Fatalf("exact durable v1 stop receipt did not retire its legacy row before claim: %v", err)
		}

		target, _ := newLinuxCloneJournal(t, "admission-v1-stop-target")
		target.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
		if err := store.SaveJournal(target); err != nil {
			t.Fatal(err)
		}
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), time.Now().UTC(), nil); err != nil {
			t.Fatalf("exact v1 stop receipt did not retire its bound legacy process: %v", err)
		}
		if after := readStateJournalBytes(t, store, foreign.Key()); !bytes.Equal(boundReceiptBytes, after) {
			t.Fatal("native admission rewrote or upgraded the bound v1 stop receipt")
		}
		if _, err := store.ClearProcessDetails(foreign.Key(), foreign.PID, foreign.ProcessIdentity); err != nil {
			t.Fatal(err)
		}
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), time.Now().UTC(), nil); err == nil {
			t.Fatal("cleared legacy row without retained stop evidence was treated as physically retired")
		}
		if err := store.DeleteJournal(foreign.Key()); err != nil {
			t.Fatalf("delete normally cleaned v1 row after its stop proof was consumed: %v", err)
		}
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), time.Now().UTC(), nil); err != nil {
			t.Fatalf("deleted legacy row continued to block native admission: %v", err)
		}
	})
}

func TestLinuxV2AdmissionAllowsLiveFencedAndPhysicallyReleasedOwners(t *testing.T) {
	requireLinuxCloneAMD64(t)
	t.Run("healthy active helper requires exact App liveness fence", func(t *testing.T) {
		store := mustStore(t)
		target, targetIntent := newLinuxCloneJournal(t, "admission-live-target")
		target.LeaseExpiresAt = time.Now().UTC().Add(3 * time.Hour)
		foreign := stoppedTestJournal("admission-live-foreign", 1)
		foreign.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
		if err := store.SaveJournal(foreign); err != nil {
			t.Fatal(err)
		}
		handoff, reservation := bindStateLinuxCgroupV2Handoff(t, store, foreign.Key())
		if _, err := store.CommitSupervisorHandoff(foreign.Key(), handoff, reservation.Gate.ObservedAt); err != nil {
			t.Fatal(err)
		}
		foreignFence := foreign.Fence()
		liveOwners := map[RunKey]protocol.Fence{foreign.Key(): foreignFence}
		now := time.Now().UTC()
		collision := stoppedTestJournal("admission-leaf-collision", 1)
		collision.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
		if err := store.SaveJournal(collision); err != nil {
			t.Fatal(err)
		}
		collisionReservation := testStateLinuxCgroupV2Reservation()
		collisionReservation.LeafName = reservation.LeafName
		if _, err := store.ReserveLinuxCgroup(collision.Key(), collision.Fence(), collisionReservation, liveOwners); err == nil {
			t.Fatal("healthy foreign admission bypassed exact cgroup leaf-name uniqueness")
		}
		if err := store.DeleteJournal(collision.Key()); err != nil {
			t.Fatalf("remove unreserved collision-test target: %v", err)
		}
		if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(now, nil); err == nil {
			t.Fatal("preclaim dispatch treated a durable lease/Authority as proof of a live helper")
		}
		if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(now, liveOwners); err != nil {
			t.Fatalf("preclaim dispatch rejected an exact active helper snapshot: %v", err)
		}
		if err := store.SaveJournal(target); err != nil {
			t.Fatal(err)
		}
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), now, nil); err == nil {
			t.Fatal("native admission treated a durable lease/Authority as proof of a live helper")
		}
		wrongLiveOwners := map[RunKey]protocol.Fence{foreign.Key(): foreignFence}
		wrongFence := foreignFence
		wrongFence.LeaseToken += "-stale"
		wrongLiveOwners[foreign.Key()] = wrongFence
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), now, wrongLiveOwners); err == nil {
			t.Fatal("native admission accepted an App liveness snapshot with a stale full fence")
		}
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), now, liveOwners); err != nil {
			t.Fatalf("exact active helper and fence did not permit unrelated native admission: %v", err)
		}
		expiredSnapshotTime := foreign.LeaseExpiresAt.Add(time.Second)
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), expiredSnapshotTime, liveOwners); err == nil {
			t.Fatal("foreign owner with an expired lease remained admissible under its previously live fence")
		}
		if _, err := store.ReserveLinuxCloneIntent(target.Key(), target.Fence(), targetIntent, liveOwners); err != nil {
			t.Fatalf("exact live-owner snapshot did not pass clone-intent CAS: %v", err)
		}
		targetReservation := testStateLinuxCgroupV2Reservation()
		targetReservation.LeafName = strings.Repeat("f", authority.TokenBytes*2)
		staleTargetFence := target.Fence()
		staleTargetFence.LeaseToken += "-stale"
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), staleTargetFence, now, liveOwners); err == nil {
			t.Fatal("post-claim admission accepted a stale target fence")
		}
		if _, err := store.ReserveLinuxCgroup(target.Key(), staleTargetFence, targetReservation, liveOwners); err == nil {
			t.Fatal("cgroup reservation CAS accepted a stale target fence")
		}
		before := readStateJournalBytes(t, store, target.Key())
		terminalAt := time.Now().UTC()
		if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
			journal.LocalState = "terminal_pending"
			journal.TerminalPendingAt = terminalAt
			journal.TerminalState = "failed"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), time.Now().UTC(), liveOwners); err == nil {
			t.Fatal("foreign owner becoming terminal remained admissible")
		}
		if _, err := store.ReserveLinuxCloneIntent(target.Key(), target.Fence(), targetIntent, liveOwners); err == nil {
			t.Fatal("clone-intent CAS replay crossed a foreign terminal transition")
		}
		blockedReservation := testStateLinuxCgroupV2Reservation()
		blockedReservation.LeafName = strings.Repeat("c", authority.TokenBytes*2)
		if _, err := store.ReserveLinuxCgroup(target.Key(), target.Fence(), blockedReservation, liveOwners); err == nil {
			t.Fatal("Reserve CAS crossed a foreign terminal transition")
		}
		after := readStateJournalBytes(t, store, target.Key())
		if !bytes.Equal(before, after) || loadStateJournal(t, store, target.Key()).ContainmentCgroup != nil {
			t.Fatal("rejected terminal-owner admission CAS mutated the target journal")
		}
		if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
			journal.LocalState = "running"
			journal.TerminalPendingAt = time.Time{}
			journal.TerminalState = ""
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		targetReservation = testStateLinuxCgroupV2Reservation()
		targetReservation.LeafName = strings.Repeat("f", authority.TokenBytes*2)
		if _, err := store.ReserveLinuxCgroup(target.Key(), target.Fence(), targetReservation, liveOwners); err != nil {
			t.Fatalf("exact live-owner snapshot did not pass cgroup reservation CAS: %v", err)
		}
		targetReservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		createExpected := targetReservation.Clone()
		if _, err := store.MarkLinuxCgroupCreateStarted(target.Key(), staleTargetFence, createExpected, liveOwners); err == nil {
			t.Fatal("CreateStarted CAS accepted a stale target fence")
		}
		before = readStateJournalBytes(t, store, target.Key())
		if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
			journal.LocalState = "stopped"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), time.Now().UTC(), liveOwners); err == nil {
			t.Fatal("foreign stopped owner remained admissible")
		}
		if _, err := store.ReserveLinuxCloneIntent(target.Key(), target.Fence(), targetIntent, liveOwners); err == nil {
			t.Fatal("clone-intent CAS replay crossed a foreign stopped owner")
		}
		if _, err := store.ReserveLinuxCgroup(target.Key(), target.Fence(), targetReservation, liveOwners); err == nil {
			t.Fatal("cgroup reservation replay crossed a foreign stopped owner")
		}
		if _, err := store.MarkLinuxCgroupCreateStarted(target.Key(), target.Fence(), createExpected, liveOwners); err == nil {
			t.Fatal("CreateStarted CAS crossed a foreign stopped owner")
		}
		after = readStateJournalBytes(t, store, target.Key())
		if !bytes.Equal(before, after) || loadStateJournal(t, store, target.Key()).ContainmentCgroup.CreateStarted {
			t.Fatal("rejected stopped-owner admission CAS mutated the target journal")
		}
		if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
			journal.LocalState = "running"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.MarkLinuxCgroupCreateStarted(target.Key(), target.Fence(), createExpected, liveOwners); err != nil {
			t.Fatalf("exact live-owner snapshot did not pass CreateStarted CAS: %v", err)
		}
		targetReservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		if _, err := store.BindLinuxCgroupLeaf(target.Key(), targetReservation, testStateLinuxCgroupV2Leaf(targetReservation)); err != nil {
			t.Fatal(err)
		}
		targetReservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		helper := testStateLinuxCgroupV2Helper()
		if _, err := store.BindLinuxCgroupHelper(target.Key(), targetReservation, helper); err != nil {
			t.Fatal(err)
		}
		targetReservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		launcher := testStateLinuxCgroupV2Launcher()
		if _, err := store.BindLinuxCgroupLauncher(target.Key(), targetReservation, launcher); err != nil {
			t.Fatal(err)
		}
		targetReservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		before = readStateJournalBytes(t, store, target.Key())
		if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
			journal.ContainmentUnproven = true
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), time.Now().UTC(), liveOwners); err == nil {
			t.Fatal("foreign owner becoming migration-unproven remained admissible")
		}
		if _, err := store.ArmLinuxCgroupLauncherGrant(target.Key(), target.Fence(), targetReservation, liveOwners); err == nil {
			t.Fatal("launcher grant CAS crossed a foreign unproven state before RELEASE")
		}
		after = readStateJournalBytes(t, store, target.Key())
		if !bytes.Equal(before, after) || loadStateJournal(t, store, target.Key()).ContainmentCgroup.LauncherGrantPossible {
			t.Fatal("rejected unproven-owner admission CAS mutated the target journal")
		}
	})

	for _, terminalState := range []string{"failed", "cancelled"} {
		t.Run("retained released "+terminalState, func(t *testing.T) {
			store := mustStore(t)
			target := stoppedTestJournal("admission-released-target-"+terminalState, 1)
			target.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
			foreign, intent := newLinuxCloneJournal(t, "admission-released-retained-"+terminalState)
			foreign.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
			seed := authority.LinuxCloneSeedReceipt{
				Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
				Digest: "sha256:" + strings.Repeat("a", 64), Size: 1024, Device: 7, Inode: 11,
			}
			checkout := authority.LinuxCloneCheckoutReceipt{
				Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
				BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target, Nonce: intent.Nonce,
				SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit, TargetDevice: 168, TargetInode: 210,
			}
			foreign.LinuxCloneIntent = &intent
			foreign.LinuxCloneSeedReceipt = &seed
			foreign.LinuxCloneCheckoutReceipt = &checkout
			foreign.WorkspacePath = intent.Target
			foreign.WorkspaceRecoveryRequired = true
			foreign.RetainWorkspace = true
			encoded, err := json.Marshal(foreign)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.journalPath(foreign.Key()), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			releaseProof := testStateLinuxCgroupV2ReleaseProofFixture(t)
			terminalAt := time.Now().UTC()
			if _, err := store.mutateJournal(foreign.Key(), func(journal *RunJournal) error {
				journal.ContainmentCgroupReleaseProof = &releaseProof
				journal.LocalState = "cleanup_pending"
				journal.TerminalPendingAt = terminalAt.Add(-time.Minute)
				journal.TerminalState = terminalState
				journal.TerminalVerdict = TerminalVerdictOwnershipLost
				journal.TerminalResolvedAt = terminalAt
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			foreignBefore := readStateJournalBytes(t, store, foreign.Key())
			if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(time.Now().UTC(), nil); err != nil {
				t.Fatalf("preclaim dispatch rejected an exact physically released retained row: %v", err)
			}
			if err := store.SaveJournal(target); err != nil {
				t.Fatal(err)
			}
			if err := store.CheckLinuxV2NativeAdmission(target.Key(), target.Fence(), time.Now().UTC(), nil); err != nil {
				t.Fatalf("exact physical release failed to admit unrelated run with retained artifacts: %v", err)
			}
			reservation := testStateLinuxCgroupV2Reservation()
			reservation.LeafName = strings.Repeat("f", authority.TokenBytes*2)
			if _, err := store.ReserveLinuxCgroup(target.Key(), target.Fence(), reservation, nil); err != nil {
				t.Fatalf("reservation blocked by physically released retained row: %v", err)
			}
			if after := readStateJournalBytes(t, store, foreign.Key()); !bytes.Equal(foreignBefore, after) {
				t.Fatal("admitting an unrelated run mutated the retained physically released journal")
			}
		})
	}
}

func TestLinuxV2TargetAdmissionRejectsUnsafeLifecycleAtEveryEffectCAS(t *testing.T) {
	requireLinuxCloneAMD64(t)
	stages := []string{"clone-intent", "cgroup-reserve", "create-start", "launcher-grant", "promotion-intent"}
	lifecycle := []string{"expired", "stale", "terminal", "cancelled", "cancelling", "uncertain"}
	for _, stage := range stages {
		for _, state := range lifecycle {
			t.Run(stage+"/"+state, func(t *testing.T) {
				store := mustStore(t)
				key, fence, intent, reservation := setupLinuxV2AdmissionStage(t, store, stage+"-"+state, stage)
				if state == "cancelled" {
					_, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
						Fence: fence, TransitionID: "cancel-" + stage + "-" + state,
						State: "cancelled", Payload: json.RawMessage(`{}`),
					})
					if err != nil {
						t.Fatal(err)
					}
				} else if _, err := store.mutateJournal(key, func(journal *RunJournal) error {
					switch state {
					case "expired":
						journal.LeaseExpiresAt = time.Now().UTC().Add(-time.Second)
					case "stale":
						journal.LocalState = "stale"
					case "terminal":
						journal.LocalState = "terminal_pending"
						journal.TerminalPendingAt = time.Now().UTC()
						journal.TerminalState = "failed"
					case "cancelling":
						journal.LocalState = "cancelling"
					case "uncertain":
						journal.NativeUsageRecoveryRequired = true
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				before := readStateJournalBytes(t, store, key)
				var err error
				switch stage {
				case "clone-intent":
					_, err = store.ReserveLinuxCloneIntent(key, fence, intent, nil)
				case "cgroup-reserve":
					_, err = store.ReserveLinuxCgroup(key, fence, reservation, nil)
				case "create-start":
					_, err = store.MarkLinuxCgroupCreateStarted(key, fence, reservation, nil)
				case "launcher-grant":
					_, err = store.ArmLinuxCgroupLauncherGrant(key, fence, reservation, nil)
				case "promotion-intent":
					candidate := authority.LinuxClonePromotionIntent{
						Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
						Commit: strings.Repeat("e", 40), TreeDigest: "sha256:" + strings.Repeat("f", 64),
					}
					_, err = store.ReserveLinuxClonePromotion(key, fence, intent, candidate)
				}
				if err == nil {
					t.Fatalf("%s CAS admitted target with %s lifecycle", stage, state)
				}
				if after := readStateJournalBytes(t, store, key); !bytes.Equal(before, after) {
					t.Fatalf("rejected %s CAS mutated the target journal for %s lifecycle", stage, state)
				}
			})
		}
	}
}

func setupLinuxV2AdmissionStage(t *testing.T, store *Store, runID, stage string) (RunKey, protocol.Fence, authority.LinuxCloneIntent, LinuxCgroupReservation) {
	t.Helper()
	target, intent := newLinuxCloneJournal(t, "admission-target-"+runID)
	target.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.SaveJournal(target); err != nil {
		t.Fatal(err)
	}
	if stage != "clone-intent" {
		if _, err := store.ReserveLinuxCloneIntent(target.Key(), target.Fence(), intent, nil); err != nil {
			t.Fatal(err)
		}
	}
	if stage == "promotion-intent" {
		seed := authority.LinuxCloneSeedReceipt{
			Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
			Digest: "sha256:" + strings.Repeat("a", 64), Size: 1024, Device: 7, Inode: 11,
		}
		if _, err := store.BindLinuxCloneSeed(target.Key(), target.Fence(), intent, seed); err != nil {
			t.Fatal(err)
		}
		handoff, reservation := bindStateLinuxCgroupV2Handoff(t, store, target.Key())
		if _, err := store.CommitSupervisorHandoff(target.Key(), handoff, reservation.Gate.ObservedAt); err != nil {
			t.Fatal(err)
		}
		checkout := authority.LinuxCloneCheckoutReceipt{
			Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
			BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target,
			Nonce: intent.Nonce, SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit,
			TargetDevice: 17, TargetInode: 23,
		}
		if _, err := store.RecordLinuxCloneCheckoutReady(target.Key(), target.Fence(), intent, checkout); err != nil {
			t.Fatal(err)
		}
		journal := loadStateJournal(t, store, target.Key())
		stopReceipt := testStateLinuxCgroupV2Receipt(*journal.ContainmentAuthority, authority.LinuxCgroupStopProofHelper, nil)
		if _, err := store.RecordContainmentStopReceipt(target.Key(), journal.PID, journal.ProcessIdentity, stopReceipt); err != nil {
			t.Fatal(err)
		}
		reservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		return target.Key(), target.Fence(), intent, reservation
	}
	reservation := testStateLinuxCgroupV2Reservation()
	reservation.LeafName = strings.Repeat("f", authority.TokenBytes*2)
	if stage == "create-start" || stage == "launcher-grant" {
		if _, err := store.ReserveLinuxCgroup(target.Key(), target.Fence(), reservation, nil); err != nil {
			t.Fatal(err)
		}
		reservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
	}
	if stage == "launcher-grant" {
		if _, err := store.MarkLinuxCgroupCreateStarted(target.Key(), target.Fence(), reservation, nil); err != nil {
			t.Fatal(err)
		}
		reservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		if _, err := store.BindLinuxCgroupLeaf(target.Key(), reservation, testStateLinuxCgroupV2Leaf(reservation)); err != nil {
			t.Fatal(err)
		}
		reservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		if _, err := store.BindLinuxCgroupHelper(target.Key(), reservation, testStateLinuxCgroupV2Helper()); err != nil {
			t.Fatal(err)
		}
		reservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
		if _, err := store.BindLinuxCgroupLauncher(target.Key(), reservation, testStateLinuxCgroupV2Launcher()); err != nil {
			t.Fatal(err)
		}
		reservation = loadStateJournal(t, store, target.Key()).ContainmentCgroup.Clone()
	}
	return target.Key(), target.Fence(), intent, reservation
}

func reserveStateLinuxV2AdmissionLeaf(t *testing.T, store *Store, journal RunJournal, leafName string) LinuxCgroupReservation {
	t.Helper()
	reservation := testStateLinuxCgroupV2Reservation()
	reservation.LeafName = leafName
	fence := journal.Fence()
	if _, err := store.ReserveLinuxCgroup(journal.Key(), fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(journal.Key(), fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, journal.Key()).ContainmentCgroup.Clone()
	if _, err := store.BindLinuxCgroupLeaf(journal.Key(), reservation, testStateLinuxCgroupV2Leaf(reservation)); err != nil {
		t.Fatal(err)
	}
	return loadStateJournal(t, store, journal.Key()).ContainmentCgroup.Clone()
}

func testStateLinuxCgroupV2ReleaseProofFixture(t *testing.T) authority.SupervisorHandoffReleaseProof {
	t.Helper()
	reservation := testStateLinuxCgroupV2ReadyReservation()
	handoff := testStateLinuxCgroupV2Handoff(reservation)
	handoff.SupervisorPID = reservation.Helper.PID
	handoff.SupervisorIdentity = reservation.Helper.Identity
	mirror := *reservation.Mirror
	handoff.Mirror = &mirror
	owner, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("v2 release owner fixture: %v", err)
	}
	receipt := testStateLinuxCgroupV2Receipt(owner, authority.LinuxCgroupStopProofHelper, nil)
	proof := testStateLinuxCgroupV2ReleaseDTO(handoff, receipt)
	if err := proof.Validate(); err != nil {
		t.Fatalf("v2 release proof fixture: %v", err)
	}
	return proof
}

func readStateJournalBytes(t *testing.T, store *Store, key RunKey) []byte {
	t.Helper()
	data, err := os.ReadFile(store.journalPath(key))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestV1LinuxGroupReceiptAndScansCannotAuthorizeV2CgroupState(t *testing.T) {
	store, key, handoff := committedLinuxScanHandoffForTest(t, "linux-v1-v2-isolation")
	for _, owner := range []string{authority.SupervisorScanOwnerHelper, authority.SupervisorScanOwnerMirror} {
		seal := testStateScanUpdate(handoff, owner, authority.SupervisorScanPhaseSeal, 1, "")
		if _, err := store.PrepareSupervisorHandoff(key, seal); err != nil {
			t.Fatal(err)
		}
	}
	loaded := loadStateJournal(t, store, key)
	v1 := loaded.ContainmentAuthority.Clone()
	v2Handoff := testStateLinuxCgroupV2Handoff(testStateLinuxCgroupV2ReadyReservation())
	v2Reservation := testStateLinuxCgroupV2ReadyReservation()
	v2Handoff.SupervisorPID = v2Reservation.Helper.PID
	v2Handoff.SupervisorIdentity = v2Reservation.Helper.Identity
	v2Handoff.Mirror = v2Reservation.Mirror
	v2, err := v2Handoff.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	v2Receipt := testStateLinuxCgroupV2Receipt(v2, authority.LinuxCgroupStopProofHelper, nil)
	if v2Receipt.ValidFor(v1) || testStateLinuxV1Receipt(v2).ValidFor(v2) {
		t.Fatal("version-mismatched physical receipt crossed the authority boundary")
	}
	if _, err := store.RecordContainmentStopReceipt(key, loaded.PID, loaded.ProcessIdentity, v2Receipt); err == nil {
		t.Fatal("v2 cgroup receipt used a v1 process-group journal")
	}
	unchanged := loadStateJournal(t, store, key)
	if unchanged.ContainmentAuthority == nil || unchanged.ContainmentAuthority.StopReceipt != nil ||
		unchanged.ContainmentScans == nil || !unchanged.ContainmentScans.sealed() {
		t.Fatalf("rejected v2 receipt changed the v1 scan journal: %#v", unchanged)
	}
	if _, err := store.RecordContainmentStopReceipt(key, loaded.PID, loaded.ProcessIdentity, testStateLinuxSupervisorStopReceipt(handoff)); err != nil {
		t.Fatalf("existing v1 group receipt stopped validating under its sealed scans: %v", err)
	}

	v2Journal := stoppedTestJournal("linux-v2-no-group-scans", 1)
	v2Journal.ContainmentCgroup = ptrStateLinuxCgroupReservation(testStateLinuxCgroupV2Reservation())
	if err := validateJournal(v2Journal); err != nil {
		t.Fatalf("valid reservation journal rejected: %v", err)
	}
	v2Journal.ContainmentScans = &ContainmentScanJournal{Version: 1}
	if err := validateJournal(v2Journal); err == nil {
		t.Fatal("v2 cgroup reservation accepted the v1 scan ledger")
	}
}

func replayUnknownStateMutation(t *testing.T, store *Store, mutate func() error) {
	t.Helper()
	restoreUnknown := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("injected post-rename unknown result")
	})
	err := mutate()
	restoreUnknown()
	if err == nil {
		t.Fatal("mutation hid an injected unknown persistence result")
	}
	writes := 0
	restoreBarrier := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	err = mutate()
	restoreBarrier()
	if err != nil {
		t.Fatalf("exact replay after unknown write failed: %v", err)
	}
	if writes != 1 {
		t.Fatalf("exact replay persistence barriers = %d, want 1", writes)
	}
}

func bindStateLinuxCgroupV2Handoff(t *testing.T, store *Store, key RunKey) (authority.SupervisorHandoff, LinuxCgroupReservation) {
	t.Helper()
	fence := loadStateJournal(t, store, key).Fence()
	reservation := testStateLinuxCgroupV2Reservation()
	if _, err := store.ReserveLinuxCgroup(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if _, err := store.BindLinuxCgroupLeaf(key, reservation, testStateLinuxCgroupV2Leaf(reservation)); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	helper := testStateLinuxCgroupV2Helper()
	if _, err := store.BindLinuxCgroupHelper(key, reservation, helper); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	launcher := testStateLinuxCgroupV2Launcher()
	if _, err := store.BindLinuxCgroupLauncher(key, reservation, launcher); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if _, err := store.ArmLinuxCgroupLauncherGrant(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if _, err := store.RecordLinuxCgroupLauncherExit(key, reservation, testStateLinuxCgroupV2Exit(launcher, *reservation.Helper)); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if _, err := store.BindLinuxCgroupGate(key, reservation, testStateLinuxCgroupV2Gate(reservation)); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	handoff := testStateLinuxCgroupV2Handoff(reservation)
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoffV2(key, handoff, helper, testStateLinuxCgroupV2Mirror())
	if err != nil {
		t.Fatal(err)
	}
	return *bound.ContainmentHandoff, bound.ContainmentCgroup.Clone()
}

func bindStateLinuxCgroupV2PreGateReservation(t *testing.T, store *Store, key RunKey) LinuxCgroupReservation {
	t.Helper()
	fence := loadStateJournal(t, store, key).Fence()
	reservation := testStateLinuxCgroupV2Reservation()
	if _, err := store.ReserveLinuxCgroup(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if _, err := store.BindLinuxCgroupLeaf(key, reservation, testStateLinuxCgroupV2Leaf(reservation)); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	helper := testStateLinuxCgroupV2Helper()
	if _, err := store.BindLinuxCgroupHelper(key, reservation, helper); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	launcher := testStateLinuxCgroupV2Launcher()
	if _, err := store.BindLinuxCgroupLauncher(key, reservation, launcher); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if _, err := store.ArmLinuxCgroupLauncherGrant(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	reservation = loadStateJournal(t, store, key).ContainmentCgroup.Clone()
	if _, err := store.RecordLinuxCgroupLauncherExit(key, reservation, testStateLinuxCgroupV2Exit(launcher, helper)); err != nil {
		t.Fatal(err)
	}
	return loadStateJournal(t, store, key).ContainmentCgroup.Clone()
}

func loadStateJournal(t *testing.T, store *Store, key RunKey) RunJournal {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func testStateLinuxCgroupV2Reservation() LinuxCgroupReservation {
	mirror := testStateLinuxCgroupV2Mirror()
	return LinuxCgroupReservation{
		Version:          LinuxCgroupReservationVersion,
		LaunchToken:      strings.Repeat("a", authority.TokenBytes*2),
		Secret:           strings.Repeat("b", authority.SecretBytes*2),
		PipeToken:        strings.Repeat("c", authority.TokenBytes*2),
		JobID:            strings.Repeat("d", authority.TokenBytes*2),
		OwnerUID:         1000,
		LauncherUID:      0,
		RecoveryEndpoint: "/run/symmetry/recovery.sock",
		Parent: authority.LinuxCgroupParentIdentity{
			BootID: "00000000-0000-4000-8000-000000000001", MountID: 42, Device: 84, Inode: 126,
			Path: "/system.slice/symmetry.slice/delegated",
		},
		LeafName: strings.Repeat("e", authority.TokenBytes*2),
		Anchor:   authority.LinuxCgroupProcessIdentity{PID: 10, Identity: "anchor:10:start"},
		Mirror:   &mirror,
	}
}

func testStateLinuxCgroupV2Leaf(reservation LinuxCgroupReservation) authority.LinuxCgroupIdentity {
	return authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: reservation.Parent, LeafName: reservation.LeafName,
		LeafDevice: 168, LeafInode: 210,
	}
}

func testStateLinuxCgroupV2Helper() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 20, Identity: "helper:20:start"}
}

func testStateLinuxCgroupV2Launcher() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 30, Identity: "launcher:30:start"}
}

func testStateLinuxCgroupV2Mirror() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 50, Identity: "daemon:50:start"}
}

func testStateLinuxCgroupV2Exit(process, witness authority.LinuxCgroupProcessIdentity) authority.LinuxCgroupExitProof {
	return authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: process, Witness: witness, Waited: true, Reaped: true,
	}
}

func testStateLinuxCgroupV2Gate(reservation LinuxCgroupReservation) authority.LinuxCgroupGateIdentity {
	return authority.LinuxCgroupGateIdentity{
		Process:    authority.LinuxCgroupProcessIdentity{PID: 40, Identity: "gate:40:start"},
		HostUID:    1000,
		ObservedAt: time.Date(2026, 9, 26, 6, 7, 8, 0, time.UTC),
		BirthKind:  authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: *reservation.LeafIdentity,
		BornInLeaf: true, TrustedImage: true,
	}
}

func testStateLinuxCgroupV2Handoff(reservation LinuxCgroupReservation) authority.SupervisorHandoff {
	gate := reservation.Gate
	return authority.SupervisorHandoff{
		Version:     authority.SupervisorHandoffVersionV2,
		LaunchToken: reservation.LaunchToken, Secret: reservation.Secret,
		OwnerKind: authority.OwnerKindLinuxHelper, OwnerContext: reservation.RecoveryEndpoint,
		TargetPID: gate.Process.PID, TargetIdentity: gate.Process.Identity,
		OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
		PipeToken: reservation.PipeToken, JobID: reservation.JobID,
		TargetBirthKind: gate.BirthKind, CgroupIdentity: cloneStateCgroupIdentity(reservation.LeafIdentity),
		AnchorPID: reservation.Anchor.PID, AnchorIdentity: reservation.Anchor.Identity,
		LauncherPID: reservation.Launcher.PID, LauncherIdentity: reservation.Launcher.Identity,
		LauncherExit: reservation.LauncherExit,
	}
}

func testStateLinuxCgroupV2ReadyReservation() LinuxCgroupReservation {
	reservation := testStateLinuxCgroupV2Reservation()
	reservation.CreateStarted = true
	leaf := testStateLinuxCgroupV2Leaf(reservation)
	reservation.LeafIdentity = &leaf
	helper := testStateLinuxCgroupV2Helper()
	reservation.Helper = &helper
	launcher := testStateLinuxCgroupV2Launcher()
	reservation.Launcher = &launcher
	reservation.LauncherGrantPossible = true
	exit := testStateLinuxCgroupV2Exit(launcher, helper)
	reservation.LauncherExit = &exit
	gate := testStateLinuxCgroupV2Gate(reservation)
	reservation.Gate = &gate
	mirror := testStateLinuxCgroupV2Mirror()
	reservation.Mirror = &mirror
	return reservation
}

func testStateLinuxCgroupV2Receipt(value authority.Supervisor, kind string, helperExit *authority.LinuxCgroupExitProof) authority.StopReceipt {
	helper := authority.LinuxCgroupProcessIdentity{PID: value.SupervisorPID, Identity: value.SupervisorIdentity}
	anchor := authority.LinuxCgroupProcessIdentity{PID: value.AnchorPID, Identity: value.AnchorIdentity}
	issuer, reaper := helper, helper
	var durableHelperExit *authority.LinuxCgroupExitProof
	if kind == authority.LinuxCgroupStopProofMirror {
		issuer = *value.Mirror
		reaper = anchor
		copy := *helperExit
		durableHelperExit = &copy
	}
	gateExit := testStateLinuxCgroupV2Exit(
		authority.LinuxCgroupProcessIdentity{PID: value.TargetPID, Identity: value.TargetIdentity}, reaper,
	)
	if kind == authority.LinuxCgroupStopProofMirror {
		gateExit.HelperExit = cloneStateExitProof(helperExit)
	}
	proof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: kind, Issuer: issuer, Reaper: reaper,
		PinnedLeafFD: true, CgroupKillAcknowledged: true, CgroupPopulatedZero: true,
		NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: *value.LauncherExit,
		GateExit:     gateExit,
		HelperExit:   durableHelperExit,
	}
	return authority.StopReceipt{
		Version: authority.SupervisorVersionV2, Status: "stopped", OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext,
		TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, PipeToken: value.PipeToken, JobID: value.JobID,
		OwnerUID: value.OwnerUID, LauncherUID: value.LauncherUID,
		SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, ActiveProcesses: 0,
		TargetBirthKind: value.TargetBirthKind, CgroupIdentity: cloneStateCgroupIdentity(value.CgroupIdentity), LinuxProof: &proof,
	}
}

func testStateLinuxCgroupV2ReleaseDTO(handoff authority.SupervisorHandoff, receipt authority.StopReceipt) authority.SupervisorHandoffReleaseProof {
	return authority.SupervisorHandoffReleaseProof{
		Version: authority.SupervisorHandoffVersionV2, Status: "released", LaunchToken: handoff.LaunchToken,
		OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext, TargetPID: handoff.TargetPID,
		TargetIdentity: handoff.TargetIdentity, OwnerUID: handoff.OwnerUID, LauncherUID: handoff.LauncherUID,
		PipeToken: handoff.PipeToken, JobID: handoff.JobID, SupervisorPID: handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity, TargetBirthKind: handoff.TargetBirthKind,
		CgroupIdentity: cloneStateCgroupIdentity(handoff.CgroupIdentity), AnchorPID: handoff.AnchorPID,
		AnchorIdentity: handoff.AnchorIdentity, LauncherPID: handoff.LauncherPID,
		LauncherIdentity: handoff.LauncherIdentity, LauncherExit: cloneStateExitProof(handoff.LauncherExit),
		Mirror: cloneStateProcessIdentity(handoff.Mirror), LeafDisposition: authority.LinuxCgroupLeafRemoved,
		StopReceipt: cloneStateStopReceipt(&receipt),
	}
}

func testStateLinuxV1Receipt(value authority.Supervisor) authority.StopReceipt {
	return authority.StopReceipt{
		Version: authority.SupervisorVersion, Status: "stopped", OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext,
		TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, PipeToken: value.PipeToken, JobID: value.JobID,
		SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, ActiveProcesses: 0,
	}
}

func testStateLinuxCgroupV2AbortProof(reservation LinuxCgroupReservation) authority.LinuxCgroupReservationAbortProof {
	return authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: reservation.LaunchToken,
		Parent: reservation.Parent, LeafName: reservation.LeafName, CreateStarted: reservation.CreateStarted,
		LeafDisposition: authority.LinuxCgroupAbortLeafNeverCreated,
		OwnerUID:        reservation.OwnerUID, Mirror: *reservation.Mirror,
		Anchor: reservation.Anchor, Witness: reservation.Anchor, CreatorFenced: true,
		NoProducerCanAct: true, ParentUnchanged: true, LeafAbsent: true,
		LeafNeverAppeared: reservation.CreateStarted,
	}
}

func testStateLinuxCgroupV2RemovedLeafAbortProof(reservation LinuxCgroupReservation, disposition string) authority.LinuxCgroupReservationAbortProof {
	proof := authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: reservation.LaunchToken,
		Parent: reservation.Parent, LeafName: reservation.LeafName, CreateStarted: reservation.CreateStarted,
		LeafDisposition: disposition, LeafIdentity: cloneStateCgroupIdentity(reservation.LeafIdentity),
		OwnerUID: reservation.OwnerUID, Mirror: *reservation.Mirror,
		Anchor: reservation.Anchor, Witness: reservation.Anchor,
		CreatorFenced: true, NoProducerCanAct: true, ParentUnchanged: true,
		LeafEmpty: true, LeafAbsent: true, RmdirSucceeded: disposition == authority.LinuxCgroupAbortLeafRemovedEmpty,
		RmdirOutcomeUnknown: disposition == authority.LinuxCgroupAbortLeafAbsentAfterRmdir,
	}
	if reservation.Helper != nil {
		helper := *reservation.Helper
		proof.Helper = &helper
	}
	if reservation.Launcher != nil {
		launcher := *reservation.Launcher
		proof.Launcher = &launcher
	}
	if reservation.Helper != nil {
		proof.HelperExit = cloneStateExitProof(reservation.HelperExit)
	}
	if reservation.Launcher != nil {
		proof.LauncherExit = cloneStateExitProof(reservation.LauncherExit)
	}
	return proof
}

func cloneStateExitProof(value *authority.LinuxCgroupExitProof) *authority.LinuxCgroupExitProof {
	if value == nil {
		return nil
	}
	copy := *value
	copy.HelperExit = cloneStateExitProof(value.HelperExit)
	return &copy
}

func cloneStateProcessIdentity(value *authority.LinuxCgroupProcessIdentity) *authority.LinuxCgroupProcessIdentity {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneStateStopReceipt(value *authority.StopReceipt) *authority.StopReceipt {
	if value == nil {
		return nil
	}
	copy := *value
	copy.CgroupIdentity = cloneStateCgroupIdentity(value.CgroupIdentity)
	if value.LinuxProof != nil {
		proof := *value.LinuxProof
		proof.HelperExit = cloneStateExitProof(value.LinuxProof.HelperExit)
		copy.LinuxProof = &proof
	}
	return &copy
}

func ptrStateLinuxCgroupReservation(value LinuxCgroupReservation) *LinuxCgroupReservation {
	cloned := value.Clone()
	return &cloned
}
