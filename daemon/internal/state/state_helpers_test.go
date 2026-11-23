package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func mustStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func testJournal(runID string, generation int64) RunJournal {
	return RunJournal{
		RunID: runID, Generation: generation, RuntimeKey: "local", RuntimeID: "runtime-1", ClaimedRuntimeEpoch: 3,
		ClaimID: "claim-1", LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC),
		LocalState: "running", Work: protocol.Work{Goal: "implement", AgentProfile: "codex", Workspace: "isolated", Input: json.RawMessage(`{}`)},
		WorkspacePath: `C:\work\run-1`, WorkspaceBindingKey: "binding-1", PID: 42, ProcessIdentity: "windows:42:created-at", StartedAt: time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC), LastEventSequence: 1,
	}
}

func stoppedTestJournal(runID string, generation int64) RunJournal {
	journal := testJournal(runID, generation)
	journal.PID, journal.ProcessIdentity, journal.StartedAt = 0, "", time.Time{}
	return journal
}

func pinnedJournalLauncherBoundCgroupReservation() LinuxCgroupReservation {
	reservation := pinnedJournalHelperBoundCgroupReservation()
	launcher := pinnedJournalCgroupLauncher()
	reservation.Launcher = &launcher
	reservation.LauncherGrantPossible = true
	exit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher, Witness: *reservation.Helper,
		Waited: true, Reaped: true,
	}
	reservation.LauncherExit = &exit
	return reservation
}

func pinnedJournalAbortedCgroupReservation() LinuxCgroupReservation {
	reservation := pinnedJournalMinimalCgroupReservation()
	reservation.AbortProof = &authority.LinuxCgroupReservationAbortProof{
		Version:          authority.LinuxCgroupAbortProofVersion,
		LaunchToken:      reservation.LaunchToken,
		Parent:           reservation.Parent,
		LeafName:         reservation.LeafName,
		LeafDisposition:  authority.LinuxCgroupAbortLeafNeverCreated,
		OwnerUID:         reservation.OwnerUID,
		Anchor:           reservation.Anchor,
		Mirror:           *reservation.Mirror,
		Witness:          reservation.Anchor,
		CreatorFenced:    true,
		NoProducerCanAct: true,
		ParentUnchanged:  true,
		LeafAbsent:       true,
	}
	return reservation
}

func pinnedJournalPreGateStopReceipt(reservation LinuxCgroupReservation) *authority.LinuxCgroupPreGateStopReceipt {
	return &authority.LinuxCgroupPreGateStopReceipt{
		Version:        authority.LinuxCgroupIdentityVersion,
		LaunchToken:    reservation.LaunchToken,
		CgroupIdentity: *reservation.LeafIdentity,
		OwnerUID:       reservation.OwnerUID,
		Anchor:         reservation.Anchor,
		Helper:         *reservation.Helper,
		Launcher:       *reservation.Launcher,
		Mirror:         *reservation.Mirror,
		Proof: authority.LinuxCgroupPreGateStopProof{
			Version:                authority.LinuxCgroupIdentityVersion,
			Kind:                   authority.LinuxCgroupStopProofHelper,
			Issuer:                 *reservation.Helper,
			Reaper:                 *reservation.Helper,
			PinnedLeafFD:           true,
			CgroupKillAcknowledged: true,
			CgroupPopulatedZero:    true,
			NoMigrationObserved:    true,
			ProducerFenced:         true,
			DescendantsReaped:      true,
			LauncherExit:           *reservation.LauncherExit,
			GateOutcome: authority.LinuxCgroupGateProductionResult{
				Version:       authority.LinuxCgroupIdentityVersion,
				LaunchToken:   reservation.LaunchToken,
				Launcher:      *reservation.Launcher,
				Outcome:       authority.LinuxCgroupGateOutcomeNeverCreated,
				Authenticator: strings.Repeat("f", 64),
			},
		},
	}
}
