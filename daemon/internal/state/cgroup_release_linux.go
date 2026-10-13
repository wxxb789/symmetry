//go:build linux

package state

import (
	"errors"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

// RecordLinuxCgroupPreGateStopReceipt persists only an authenticated launcher
// result saying clone3 did not produce a gate, alongside the platform's exact
// cgroup stop attestation. It never infers no-gate from a missing PID.
func (store *Store) RecordLinuxCgroupPreGateStopReceipt(key RunKey, expected LinuxCgroupReservation, receipt authority.LinuxCgroupPreGateStopReceipt) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := receipt.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.PreGateStopReceipt != nil || !linuxCgroupPreGateReceiptMatchesReservation(expected, receipt) {
		return RunJournal{}, errors.New("pre-gate stop receipt does not match the exact reservation")
	}
	if err := linuxgate.VerifyGateNeverProducedAuthenticator(expected.Secret, receipt.Proof.GateOutcome); err != nil {
		return RunJournal{}, errors.New("pre-gate root launcher outcome authentication failed")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil || journal.ContainmentScans != nil ||
			journal.HasProcessDetails() || journal.ContainmentUnproven || journal.ContainmentCgroup == nil {
			return errors.New("pre-gate stop receipt has an active or mismatched owner")
		}
		after := expected.Clone()
		copy := receipt.Clone()
		after.PreGateStopReceipt = &copy
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		if !journal.ContainmentCgroup.Equal(expected) {
			return errors.New("pre-gate stop receipt compare-and-set mismatch")
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// ClearSupervisorHandoffV2 clears a prepared v2 handoff only with the opaque
// result of a successful pinned-leaf platform removal. A durable receipt DTO
// or a caller-constructed release proof is not itself physical release proof.
func (store *Store) ClearSupervisorHandoffV2(key RunKey, expected authority.SupervisorHandoff, release platform.LinuxCgroupV2Release) (RunJournal, error) {
	proof, err := linuxCgroupReleaseSnapshot(release)
	if err != nil {
		return RunJournal{}, err
	}
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version != authority.SupervisorHandoffVersionV2 || expected.StopReceipt == nil ||
		!expected.StopReceipt.ValidForHandoff(expected) || !proof.ValidFor(expected) {
		return RunJournal{}, errors.New("v2 supervisor handoff release does not match its durable receipt")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff == nil {
			if journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.Equal(proof) &&
				journal.ContainmentAuthority == nil && !journal.HasProcessDetails() &&
				journal.ContainmentCgroup == nil && journal.ContainmentScans == nil {
				return nil
			}
			return errors.New("v2 supervisor handoff is not pending")
		}
		current := journal.ContainmentHandoff
		if !sameBoundV2Handoff(current, &expected) || current.StopReceipt == nil ||
			!current.StopReceipt.Equal(*expected.StopReceipt) || journal.ContainmentCgroup == nil ||
			!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, *current, true) ||
			!linuxCgroupReceiptMatchesReservation(*journal.ContainmentCgroup, *current.StopReceipt) ||
			!proof.ValidFor(*current) {
			return errors.New("v2 supervisor handoff release compare-and-set mismatch")
		}
		if journal.ContainmentUnproven || journal.ContainmentScans != nil || journal.ContainmentAuthority != nil || journal.HasProcessDetails() {
			return errors.New("v2 supervisor handoff release state is unresolved")
		}
		journal.ContainmentHandoff = nil
		journal.ContainmentCgroup = nil
		journal.ContainmentScans = nil
		clonedProof := proof.Clone()
		journal.ContainmentCgroupReleaseProof = &clonedProof
		return nil
	})
}

// ClearProcessDetailsV2 clears a committed v2 process, authority and
// reservation only with an opaque platform release obtained after exact
// receipt-backed pinned-leaf removal/absence. The same token binds an unknown
// clear response to a durable replay tombstone.
func (store *Store) ClearProcessDetailsV2(key RunKey, pid int, identity string, release platform.LinuxCgroupV2Release) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("process details are invalid")
	}
	proof, err := linuxCgroupReleaseSnapshot(release)
	if err != nil {
		return RunJournal{}, err
	}
	if proof.Version != authority.SupervisorHandoffVersionV2 || proof.TargetPID != pid || proof.TargetIdentity != identity {
		return RunJournal{}, errors.New("v2 process release does not match the expected target")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.HasProcessDetails() && journal.PID == 0 && journal.ProcessIdentity == "" && journal.StartedAt.IsZero() &&
			journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil && journal.ContainmentCgroup == nil {
			if journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.Equal(proof) {
				return nil
			}
			return errors.New("v2 process release replay does not match the durable release tombstone")
		}
		if journal.ContainmentHandoff != nil || journal.PID != pid || journal.ProcessIdentity != identity ||
			journal.ContainmentAuthority == nil || journal.ContainmentAuthority.Version != authority.SupervisorVersionV2 ||
			journal.ContainmentCgroup == nil || journal.ContainmentScans != nil || journal.ContainmentUnproven {
			return errors.New("v2 process clear identity is unresolved or mismatched")
		}
		current := journal.ContainmentAuthority
		if current.TargetPID != pid || current.TargetIdentity != identity || current.StopReceipt == nil ||
			!current.StopReceipt.ValidFor(*current) || !proof.ValidForSupervisor(*current) ||
			!linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *current) ||
			!linuxCgroupReceiptMatchesReservation(*journal.ContainmentCgroup, *current.StopReceipt) {
			return errors.New("v2 process clear requires exact durable receipt and platform release")
		}
		journal.PID = 0
		journal.ProcessIdentity = ""
		journal.StartedAt = time.Time{}
		journal.ContainmentAuthority = nil
		journal.ContainmentCgroup = nil
		journal.ContainmentScans = nil
		clonedProof := proof.Clone()
		journal.ContainmentCgroupReleaseProof = &clonedProof
		return nil
	})
}

// ClearLinuxCgroupReservationV2 clears a grant-armed reservation whose
// authenticated launcher result proves no A gate was produced. It requires
// the exact durable pre-gate stop receipt and the separate opaque platform
// removal capability; no target PID or absent path is sufficient.
func (store *Store) ClearLinuxCgroupReservationV2(key RunKey, expected LinuxCgroupReservation, release platform.LinuxCgroupV2Release) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.PreGateStopReceipt == nil || !linuxCgroupPreGateReceiptMatchesReservation(expected, *expected.PreGateStopReceipt) {
		return RunJournal{}, errors.New("pre-gate reservation has no exact durable stop receipt")
	}
	proof, issued := release.SnapshotPreGate()
	if !issued {
		return RunJournal{}, errors.New("platform has not issued a pre-gate cgroup release")
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !proof.ValidFor(*expected.PreGateStopReceipt) {
		return RunJournal{}, errors.New("pre-gate platform release does not match its durable stop receipt")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentCgroup == nil {
			if journal.ContainmentCgroupPreGateReleaseProof != nil &&
				journal.ContainmentCgroupPreGateReleaseProof.Equal(proof) &&
				journal.ContainmentCgroupReleaseProof == nil && journal.ContainmentAuthority == nil &&
				journal.ContainmentHandoff == nil && journal.ContainmentScans == nil &&
				!journal.HasProcessDetails() && journal.PID == 0 && !journal.ContainmentUnproven {
				return nil
			}
			return errors.New("pre-gate cgroup reservation is not pending")
		}
		current := journal.ContainmentCgroup
		if !current.Equal(expected) || journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil ||
			journal.ContainmentScans != nil || journal.HasProcessDetails() || journal.PID != 0 || journal.ContainmentUnproven ||
			journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil ||
			current.PreGateStopReceipt == nil ||
			!current.PreGateStopReceipt.Equal(*expected.PreGateStopReceipt) ||
			!linuxCgroupPreGateReceiptMatchesReservation(*current, *current.PreGateStopReceipt) {
			return errors.New("pre-gate reservation release compare-and-set mismatch")
		}
		journal.ContainmentCgroup = nil
		cloned := proof.Clone()
		journal.ContainmentCgroupPreGateReleaseProof = &cloned
		return nil
	})
}

func linuxCgroupReleaseSnapshot(release platform.LinuxCgroupV2Release) (authority.SupervisorHandoffReleaseProof, error) {
	proof, issued := release.Snapshot()
	if !issued {
		return authority.SupervisorHandoffReleaseProof{}, errors.New("platform has not issued a Linux cgroup v2 release")
	}
	if err := proof.Validate(); err != nil {
		return authority.SupervisorHandoffReleaseProof{}, err
	}
	return proof, nil
}
