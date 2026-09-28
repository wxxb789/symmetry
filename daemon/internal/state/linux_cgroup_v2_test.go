//go:build linux

package state

import (
	"encoding/json"
	"errors"
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
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	reservation := testStateLinuxCgroupV2Reservation()
	replayUnknownStateMutation(t, store, func() error {
		_, err := store.ReserveLinuxCgroup(key, reservation)
		return err
	})

	loaded := loadStateJournal(t, store, key)
	if loaded.ContainmentCgroup == nil || loaded.ContainmentCgroup.CreateStarted || loaded.HasProcessDetails() || !loaded.HasPendingContainment() {
		t.Fatalf("reserved journal = %#v, want pending reservation before creation with no PID", loaded)
	}
	reservation = loaded.ContainmentCgroup.Clone()
	otherRun := stoppedTestJournal("linux-cgroup-v2-duplicate-leaf", 1)
	if err := store.SaveJournal(otherRun); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveLinuxCgroup(otherRun.Key(), reservation); err == nil {
		t.Fatal("ReserveLinuxCgroup() reserved the same exclusive parent/name for another RunKey")
	}

	different := reservation
	different.LeafName = strings.Repeat("f", authority.TokenBytes*2)
	if _, err := store.ReserveLinuxCgroup(key, different); err == nil {
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
		_, err := store.MarkLinuxCgroupCreateStarted(key, createExpected)
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
		_, err := store.ArmLinuxCgroupLauncherGrant(key, reservation)
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
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	reservation := testStateLinuxCgroupV2Reservation()
	if _, err := store.ReserveLinuxCgroup(key, reservation); err != nil {
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
	if err := store.SaveJournal(postIntent); err != nil {
		t.Fatal(err)
	}
	newReservation := testStateLinuxCgroupV2Reservation()
	newReservation.LaunchToken = strings.Repeat("1", authority.TokenBytes*2)
	newReservation.Secret = strings.Repeat("2", authority.SecretBytes*2)
	newReservation.PipeToken = strings.Repeat("3", authority.TokenBytes*2)
	newReservation.JobID = strings.Repeat("4", authority.TokenBytes*2)
	newReservation.LeafName = strings.Repeat("f", authority.TokenBytes*2)
	if _, err := store.ReserveLinuxCgroup(postIntent.Key(), newReservation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(postIntent.Key(), newReservation); err != nil {
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
		if err := store.SaveJournal(removalJournal); err != nil {
			t.Fatal(err)
		}
		removalReservation := testStateLinuxCgroupV2Reservation()
		removalReservation.LeafName = strings.Repeat("7", authority.TokenBytes*2)
		removalReservation.LaunchToken = strings.Repeat("8", authority.TokenBytes*2)
		removalReservation.Secret = strings.Repeat("9", authority.SecretBytes*2)
		removalReservation.PipeToken = strings.Repeat("a", authority.TokenBytes*2)
		removalReservation.JobID = strings.Repeat("b", authority.TokenBytes*2)
		if _, err := store.ReserveLinuxCgroup(removalJournal.Key(), removalReservation); err != nil {
			t.Fatal(err)
		}
		if _, err := store.MarkLinuxCgroupCreateStarted(removalJournal.Key(), removalReservation); err != nil {
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
	reservation := testStateLinuxCgroupV2Reservation()
	if _, err := store.ReserveLinuxCgroup(key, reservation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(key, reservation); err != nil {
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
	if _, err := store.ArmLinuxCgroupLauncherGrant(key, reservation); err != nil {
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
	reservation := testStateLinuxCgroupV2Reservation()
	if _, err := store.ReserveLinuxCgroup(key, reservation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(key, reservation); err != nil {
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
	if _, err := store.ArmLinuxCgroupLauncherGrant(key, reservation); err != nil {
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
