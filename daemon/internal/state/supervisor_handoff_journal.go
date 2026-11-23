package state

import (
	"errors"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

// PrepareSupervisorHandoff durably records the immutable launch binding before
// an independently running helper can outlive the daemon. Replaying the same
// launch preserves the record and performs a fresh persistence barrier so an
// unknown prior write is not upgraded by readback alone. A different launch
// cannot replace an unresolved record. Helper identity, scan progress, and stop
// receipt are advanced only by their dedicated CAS mutations.
func (store *Store) PrepareSupervisorHandoff(key RunKey, handoff authority.SupervisorHandoff) (RunJournal, error) {
	if err := handoff.Validate(); err != nil {
		return RunJournal{}, err
	}
	if handoff.Version == authority.SupervisorHandoffVersionV2 {
		return store.prepareLinuxCgroupSupervisorHandoff(key, handoff)
	}
	if handoff.ScanUpdate != nil {
		return store.recordSupervisorScanUpdate(key, handoff)
	}
	if handoff.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff stop receipt requires dedicated mutation")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.LinuxCloneIntent != nil {
			return errors.New("Linux clone run requires v2 cgroup supervisor handoff")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("supervisor handoff is fenced by Linux clone abort")
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot prepare supervisor handoff during cleanup")
		}
		if journal.HasProcessDetails() || journal.ContainmentAuthority != nil {
			return errors.New("supervisor handoff conflicts with committed containment")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("supervisor handoff cannot be recreated after v2 containment release")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("supervisor handoff cannot be recreated after v2 pre-gate release")
		}
		if journal.ContainmentHandoff == nil {
			cloned := handoff.Clone()
			journal.ContainmentHandoff = &cloned
			return nil
		}
		current := journal.ContainmentHandoff
		if !current.SameLaunch(handoff) {
			return errors.New("supervisor handoff conflicts with journal")
		}
		if current.SupervisorPID != 0 && handoff.SupervisorPID != 0 &&
			(current.SupervisorPID != handoff.SupervisorPID || current.SupervisorIdentity != handoff.SupervisorIdentity) {
			return errors.New("supervisor handoff helper identity conflicts with journal")
		}
		if current.SupervisorPID == 0 && handoff.SupervisorPID != 0 {
			return errors.New("supervisor handoff helper identity requires dedicated bind")
		}
		// Replaying the pre-helper form must not erase a helper identity or a
		// receipt that a later phase durably recorded.
		return nil
	})
}

func (store *Store) prepareLinuxCgroupSupervisorHandoff(key RunKey, handoff authority.SupervisorHandoff) (RunJournal, error) {
	if handoff.Version != authority.SupervisorHandoffVersionV2 || handoff.SupervisorPID != 0 ||
		handoff.SupervisorIdentity != "" || handoff.Mirror != nil || handoff.StopReceipt != nil {
		return RunJournal{}, errors.New("Linux cgroup supervisor handoff prepare value is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() || journal.LocalState == "cleanup_pending" || journal.HasProcessDetails() ||
			journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil ||
			journal.ContainmentAuthority != nil || journal.ContainmentScans != nil || journal.ContainmentUnproven ||
			journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("Linux cgroup supervisor handoff conflicts with journal")
		}
		if journal.ContainmentCgroup == nil ||
			!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, handoff, false) {
			return errors.New("Linux cgroup supervisor handoff does not match a complete leaf reservation")
		}
		if journal.ContainmentHandoff == nil {
			cloned := handoff.Clone()
			journal.ContainmentHandoff = &cloned
			return nil
		}
		current := journal.ContainmentHandoff
		if !current.Equal(handoff) || current.StopReceipt != nil ||
			!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, *current, current.SupervisorPID > 0) {
			return errors.New("Linux cgroup supervisor handoff conflicts with journal")
		}
		return nil
	})
}

// BindSupervisorHandoffV2 binds the reservation's already durable helper and
// the exact daemon mirror identity in the prepared v2 handoff. It intentionally
// initializes no v1 process-group scan ledger.
func (store *Store) BindSupervisorHandoffV2(key RunKey, expected authority.SupervisorHandoff, helper authority.LinuxCgroupProcessIdentity, mirror authority.LinuxCgroupProcessIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version != authority.SupervisorHandoffVersionV2 || expected.SupervisorPID != 0 ||
		expected.SupervisorIdentity != "" || expected.Mirror != nil || expected.StopReceipt != nil {
		return RunJournal{}, errors.New("Linux cgroup supervisor bind expected value is invalid")
	}
	if err := helper.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := mirror.Validate(); err != nil {
		return RunJournal{}, err
	}
	after := expected.Clone()
	after.SupervisorPID = helper.PID
	after.SupervisorIdentity = helper.Identity
	after.Mirror = &mirror
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() || journal.LocalState == "cleanup_pending" || journal.ContainmentScans != nil ||
			journal.ContainmentAuthority != nil || journal.HasProcessDetails() || journal.ContainmentUnproven ||
			journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Helper == nil ||
			!journal.ContainmentCgroup.Helper.Equal(helper) ||
			journal.ContainmentCgroup.Gate == nil || journal.ContainmentCgroup.Mirror == nil ||
			!journal.ContainmentCgroup.Mirror.Equal(mirror) {
			return errors.New("Linux cgroup supervisor bind has no matching producer reservation")
		}
		current := journal.ContainmentHandoff
		if current == nil || !current.SameLaunch(expected) || current.StopReceipt != nil {
			return errors.New("Linux cgroup supervisor handoff compare-and-set mismatch")
		}
		if current.Equal(after) {
			if journal.ContainmentCgroup.Mirror == nil || !journal.ContainmentCgroup.Mirror.Equal(mirror) {
				return errors.New("Linux cgroup mirror identity conflicts with reservation")
			}
			return nil
		}
		if !current.Equal(expected) {
			return errors.New("Linux cgroup supervisor handoff compare-and-set mismatch")
		}
		cloned := after.Clone()
		journal.ContainmentHandoff = &cloned
		return nil
	})
}

func (store *Store) recordSupervisorScanUpdate(key RunKey, request authority.SupervisorHandoff) (RunJournal, error) {
	update := request.ScanUpdate
	if update == nil {
		return RunJournal{}, errors.New("supervisor scan update is missing")
	}
	expected := request.Clone()
	expected.ScanUpdate = nil
	expected.SupervisorPID = update.SupervisorPID
	expected.SupervisorIdentity = update.SupervisorIdentity
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LocalState == "cleanup_pending" {
			return errors.New("Linux containment scan update arrived during cleanup")
		}
		if journal.ContainmentUnproven && update.Phase != authority.SupervisorScanPhaseUnproven {
			return errors.New("Linux containment observation is already unresolved")
		}
		if journal.ContainmentHandoff != nil {
			current := journal.ContainmentHandoff
			if current.StopReceipt != nil || !current.Equal(expected) {
				return errors.New("supervisor scan update handoff identity mismatch")
			}
		} else {
			value, err := expected.ToSupervisor()
			if err != nil {
				return err
			}
			current := journal.ContainmentAuthority
			if current == nil || current.StopReceipt != nil || journal.PID != value.TargetPID || journal.ProcessIdentity != value.TargetIdentity {
				return errors.New("supervisor scan update has no active containment authority")
			}
			currentBinding := current.Clone()
			currentBinding.StopReceipt = nil
			if !currentBinding.Equal(value) {
				return errors.New("supervisor scan update authority identity mismatch")
			}
		}
		scans := journal.ContainmentScans
		if scans == nil || !scans.matchesHandoff(expected) || scans.MirrorIdentity != update.MirrorIdentity {
			return errors.New("supervisor scan update fence is missing or mismatched")
		}
		progress := &scans.Helper
		if update.Owner == authority.SupervisorScanOwnerMirror {
			progress = &scans.Mirror
		}
		switch update.Phase {
		case authority.SupervisorScanPhaseBegin:
			if progress.SealedSequence != 0 || progress.UnprovenSequence != 0 || progress.PendingSequence != 0 && progress.PendingSequence != update.Sequence {
				return errors.New("supervisor scan begin conflicts with unresolved sequence")
			}
			if progress.PendingSequence == update.Sequence {
				if progress.CleanResultSequence != 0 {
					return errors.New("supervisor scan is awaiting fence retirement")
				}
				return nil
			}
			if progress.CompletedSequence == ^uint64(0) || update.Sequence != progress.CompletedSequence+1 {
				return errors.New("supervisor scan begin sequence is stale or out of order")
			}
			progress.PendingSequence = update.Sequence
		case authority.SupervisorScanPhaseClean:
			if progress.SealedSequence != 0 || progress.UnprovenSequence != 0 || journal.ContainmentUnproven {
				return errors.New("supervisor scan cannot clear unresolved observation")
			}
			if progress.PendingSequence == update.Sequence {
				if progress.CleanResultSequence != 0 && progress.CleanResultSequence != update.Sequence {
					return errors.New("supervisor clean scan result conflicts with pending sequence")
				}
				progress.CleanResultSequence = update.Sequence
				return nil
			}
			if progress.PendingSequence == 0 && progress.CleanResultSequence == 0 && progress.CompletedSequence == update.Sequence {
				return nil
			}
			return errors.New("supervisor clean scan sequence is not pending")
		case authority.SupervisorScanPhaseRetire:
			if progress.SealedSequence != 0 || progress.UnprovenSequence != 0 || journal.ContainmentUnproven {
				return errors.New("supervisor scan fence cannot retire unresolved observation")
			}
			if progress.PendingSequence == update.Sequence && progress.CleanResultSequence == update.Sequence {
				progress.PendingSequence = 0
				progress.CleanResultSequence = 0
				progress.CompletedSequence = update.Sequence
				return nil
			}
			if progress.PendingSequence == 0 && progress.CleanResultSequence == 0 && progress.CompletedSequence == update.Sequence {
				return nil
			}
			return errors.New("supervisor scan result is not durably ready for retirement")
		case authority.SupervisorScanPhaseSeal:
			if progress.SealedSequence == update.Sequence && progress.sealed() {
				return nil
			}
			if progress.SealedSequence != 0 || progress.PendingSequence != 0 || progress.CleanResultSequence != 0 ||
				progress.UnprovenSequence != 0 || journal.ContainmentUnproven || progress.CompletedSequence == 0 ||
				progress.CompletedSequence != update.Sequence || progress.FailureKind != "" {
				return errors.New("supervisor scan seal does not match a final clean sequence")
			}
			progress.SealedSequence = update.Sequence
		case authority.SupervisorScanPhaseUnproven:
			if progress.UnprovenSequence == update.Sequence && progress.FailureKind == update.FailureKind {
				return nil
			}
			preScanBeginFailure := update.FailureKind == "pre_scan_ack_failure" && progress.PendingSequence == 0 &&
				progress.CleanResultSequence == 0 && progress.SealedSequence == 0 && progress.CompletedSequence != ^uint64(0) &&
				update.Sequence == progress.CompletedSequence+1
			cleanRetireFailure := progress.PendingSequence == 0 && progress.CompletedSequence == update.Sequence &&
				update.FailureKind == "clean_retire_ack_failure"
			if progress.SealedSequence != 0 || progress.UnprovenSequence != 0 ||
				(progress.PendingSequence != update.Sequence && !cleanRetireFailure && !preScanBeginFailure) {
				return errors.New("supervisor unproven scan sequence is not pending")
			}
			progress.PendingSequence = 0
			progress.CleanResultSequence = 0
			progress.UnprovenSequence = update.Sequence
			progress.FailureKind = update.FailureKind
			journal.ContainmentUnproven = true
		case authority.SupervisorScanPhaseAssertClean:
			if update.Sequence != progress.CompletedSequence || journal.ContainmentUnproven || !scans.clean() {
				return errors.New("Linux containment scan evidence is unresolved")
			}
		case authority.SupervisorScanPhaseAssertSealed:
			if update.Sequence != progress.SealedSequence || journal.ContainmentUnproven || !progress.sealed() {
				return errors.New("Linux containment scan owner seal is unresolved")
			}
		case authority.SupervisorScanPhaseAssertAllSealed:
			if update.Sequence != progress.SealedSequence || journal.ContainmentUnproven || !scans.sealed() {
				return errors.New("Linux containment scan seals are unresolved")
			}
		default:
			return errors.New("supervisor scan update phase is invalid")
		}
		return nil
	})
}

// BindSupervisorHandoff adds the exact helper PID/creation identity and the
// first helper/mirror scan fences to a prepared handoff. The expected handoff
// is an exact CAS value, so a stale writer cannot retarget a retained helper.
// Exact replays perform a fresh persistence barrier after readback of an
// unknown prior write.
func (store *Store) BindSupervisorHandoff(key RunKey, expected authority.SupervisorHandoff, pid int, identity string) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version == authority.SupervisorHandoffVersionV2 {
		return RunJournal{}, errors.New("v2 handoff requires BindSupervisorHandoffV2")
	}
	scanUpdate := expected.ScanUpdate
	baseExpected := expected.Clone()
	baseExpected.ScanUpdate = nil
	if expected.OwnerKind == authority.OwnerKindLinuxHelper {
		if scanUpdate == nil || scanUpdate.Phase != authority.SupervisorScanPhaseInitialize ||
			scanUpdate.Owner != authority.SupervisorScanOwnerMirror || scanUpdate.Sequence != 1 || scanUpdate.MirrorIdentity == "" {
			return RunJournal{}, errors.New("Linux supervisor bind requires an exact mirror scan identity")
		}
	} else if scanUpdate != nil {
		return RunJournal{}, errors.New("non-Linux supervisor bind cannot carry scan state")
	}
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("supervisor handoff helper identity is invalid")
	}
	if expected.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff helper cannot be rebound after stop receipt")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff == nil {
			return errors.New("supervisor handoff is not pending")
		}
		current := journal.ContainmentHandoff
		if !current.Equal(baseExpected) {
			// A write error after rename leaves the helper binding durable while
			// the caller still holds the pre-bind expected value. Replaying the
			// same binding is safe and must not be mistaken for retargeting.
			if baseExpected.SupervisorPID == 0 && baseExpected.SupervisorIdentity == "" && baseExpected.StopReceipt == nil &&
				current.SameLaunch(baseExpected) && current.SupervisorPID == pid && current.SupervisorIdentity == identity && current.StopReceipt == nil {
				if current.OwnerKind == authority.OwnerKindLinuxHelper &&
					(journal.ContainmentScans == nil || !journal.ContainmentScans.matchesHandoff(*current) || journal.ContainmentScans.MirrorIdentity != scanUpdate.MirrorIdentity) {
					return errors.New("Linux supervisor bind scan identity conflicts with journal")
				}
				return nil
			}
			return errors.New("supervisor handoff compare-and-set mismatch")
		}
		if current.SupervisorPID != 0 || current.SupervisorIdentity != "" {
			if current.SupervisorPID == pid && current.SupervisorIdentity == identity {
				if current.OwnerKind == authority.OwnerKindLinuxHelper &&
					(journal.ContainmentScans == nil || !journal.ContainmentScans.matchesHandoff(*current) || journal.ContainmentScans.MirrorIdentity != scanUpdate.MirrorIdentity) {
					return errors.New("Linux supervisor handoff scan fence is missing or mismatched")
				}
				return nil
			}
			return errors.New("supervisor handoff helper identity conflicts with journal")
		}
		if current.OwnerKind == authority.OwnerKindLinuxHelper {
			if journal.ContainmentUnproven || journal.ContainmentScans != nil {
				return errors.New("Linux supervisor handoff cannot bind over unresolved scan state")
			}
			scans := ContainmentScanJournal{
				Version: 1, LaunchToken: current.LaunchToken, TargetPID: current.TargetPID,
				TargetIdentity: current.TargetIdentity, SupervisorPID: pid, SupervisorIdentity: identity,
				MirrorIdentity: scanUpdate.MirrorIdentity,
				Helper:         ContainmentScanProgress{PendingSequence: 1},
				Mirror:         ContainmentScanProgress{PendingSequence: 1},
			}
			journal.ContainmentScans = &scans
		}
		current.SupervisorPID = pid
		current.SupervisorIdentity = identity
		return nil
	})
}

// CommitSupervisorHandoff atomically promotes an exact prepared handoff into
// the existing process marker plus Supervisor authority. The handoff is
// cleared in the same journal replacement, so recovery observes either the
// old pending record or the complete committed pair. Exact replays perform a
// fresh persistence barrier after readback of an unknown prior write.
func (store *Store) CommitSupervisorHandoff(key RunKey, expected authority.SupervisorHandoff, startedAt time.Time) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff stop receipt cannot commit")
	}
	if startedAt.IsZero() {
		return RunJournal{}, errors.New("supervisor handoff start time is invalid")
	}
	value, err := expected.ToSupervisor()
	if err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if value.Version == authority.SupervisorVersionV2 {
			if journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Gate == nil ||
				!journal.ContainmentCgroup.Gate.Process.Equal(authority.LinuxCgroupProcessIdentity{PID: value.TargetPID, Identity: value.TargetIdentity}) ||
				!startedAt.Equal(journal.ContainmentCgroup.Gate.ObservedAt) {
				return errors.New("v2 supervisor handoff commit time must match durable gate observation")
			}
		}
		if journal.ContainmentHandoff == nil {
			if journal.PID == value.TargetPID && journal.ProcessIdentity == value.TargetIdentity &&
				!journal.StartedAt.IsZero() && journal.ContainmentAuthority != nil {
				if value.Version == authority.SupervisorVersionV2 && !journal.StartedAt.Equal(startedAt) {
					return errors.New("v2 supervisor handoff commit timestamp changed during replay")
				}
				if value.Version == authority.SupervisorVersionV2 && journal.ContainmentAuthority.StopReceipt != nil {
					return errors.New("v2 handoff commit replay must precede receipt advancement")
				}
				existing := journal.ContainmentAuthority.Clone()
				existing.StopReceipt = nil
				candidate := value.Clone()
				candidate.StopReceipt = nil
				if existing.Equal(candidate) {
					if value.Version == authority.SupervisorVersionV2 {
						if journal.ContainmentUnproven || journal.ContainmentCgroup == nil ||
							!linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *journal.ContainmentAuthority) {
							return errors.New("v2 committed authority has no matching cgroup reservation")
						}
					} else {
						if err := journal.requireLinuxScansCleanForAuthority(*journal.ContainmentAuthority); err != nil {
							return err
						}
						if journal.ContainmentUnproven {
							return errors.New("Linux supervisor initial scan observation is unresolved")
						}
					}
					return nil
				}
			}
			return errors.New("supervisor handoff is not pending")
		}
		if !journal.ContainmentHandoff.Equal(expected) {
			return errors.New("supervisor handoff compare-and-set mismatch")
		}
		if journal.HasProcessDetails() || journal.ContainmentAuthority != nil {
			return errors.New("supervisor handoff conflicts with committed containment")
		}
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if value.OwnerKind == authority.OwnerKindLinuxHelper {
			if value.Version == authority.SupervisorVersionV2 {
				if journal.ContainmentUnproven || journal.ContainmentScans != nil || journal.ContainmentCgroup == nil ||
					journal.ContainmentCgroup.HelperExit != nil ||
					!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, expected, true) {
					return errors.New("Linux cgroup initial producer fences are unresolved")
				}
			} else if journal.ContainmentUnproven || journal.ContainmentScans == nil ||
				!journal.ContainmentScans.matchesHandoff(expected) || !journal.ContainmentScans.clean() {
				return errors.New("Linux supervisor initial scan fences are unresolved")
			}
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot commit supervisor handoff during cleanup")
		}
		journal.PID = value.TargetPID
		journal.ProcessIdentity = value.TargetIdentity
		journal.StartedAt = startedAt
		cloned := value.Clone()
		journal.ContainmentAuthority = &cloned
		journal.ContainmentHandoff = nil
		return nil
	})
}

// RecordSupervisorHandoffStopReceipt records an exact positive stop witness
// while the handoff is pending. It also accepts the commit-unknown replay
// state, where the handoff rename succeeded before the caller observed an
// error, and records the witness on the committed authority instead. Every
// accepted replay performs a fresh journal write so a prior unknown result is
// never upgraded by readback alone.
func (store *Store) RecordSupervisorHandoffStopReceipt(key RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version == authority.SupervisorHandoffVersionV2 {
		return store.recordLinuxCgroupSupervisorStopReceipt(key, expected, receipt)
	}
	if expected.SupervisorPID <= 0 || expected.SupervisorIdentity == "" {
		return RunJournal{}, errors.New("supervisor handoff stop receipt requires a bound helper")
	}
	if !receipt.ValidForHandoff(expected) {
		return RunJournal{}, errors.New("supervisor handoff stop receipt does not match handoff")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil {
			current := journal.ContainmentHandoff
			if !current.Equal(expected) {
				if !current.SameLaunch(expected) || current.SupervisorPID != expected.SupervisorPID || current.SupervisorIdentity != expected.SupervisorIdentity || current.StopReceipt == nil || *current.StopReceipt != receipt {
					return errors.New("supervisor handoff compare-and-set mismatch")
				}
				if journal.ContainmentUnproven {
					return errors.New("supervisor handoff scan observation is unresolved")
				}
				if err := journal.requireLinuxScansSealedForHandoff(*current); err != nil {
					return err
				}
				return nil
			}
			if journal.ContainmentUnproven {
				return errors.New("supervisor handoff scan observation is unresolved")
			}
			if err := journal.requireLinuxScansSealedForHandoff(*current); err != nil {
				return err
			}
			if current.StopReceipt != nil {
				if *current.StopReceipt == receipt {
					return nil
				}
				return errors.New("supervisor handoff stop receipt cannot replace an existing witness")
			}
			cloned := receipt
			current.StopReceipt = &cloned
			return nil
		}
		value, err := expected.ToSupervisor()
		if err != nil {
			return err
		}
		if journal.ContainmentAuthority == nil || journal.PID != value.TargetPID || journal.ProcessIdentity != value.TargetIdentity {
			return errors.New("supervisor handoff is not pending or committed")
		}
		if journal.ContainmentUnproven {
			return errors.New("supervisor handoff scan observation is unresolved")
		}
		if err := journal.requireLinuxScansSealedForAuthority(*journal.ContainmentAuthority); err != nil {
			return err
		}
		existing := journal.ContainmentAuthority.Clone()
		existing.StopReceipt = nil
		candidate := value.Clone()
		candidate.StopReceipt = nil
		if !existing.Equal(candidate) {
			return errors.New("supervisor handoff committed authority conflicts with handoff")
		}
		if journal.ContainmentAuthority.StopReceipt != nil {
			if *journal.ContainmentAuthority.StopReceipt == receipt {
				return nil
			}
			return errors.New("containment stop receipt cannot replace an existing witness")
		}
		cloned := receipt
		journal.ContainmentAuthority.StopReceipt = &cloned
		return nil
	})
}

func (store *Store) recordLinuxCgroupSupervisorStopReceipt(key RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (RunJournal, error) {
	if expected.SupervisorPID <= 0 || expected.SupervisorIdentity == "" || expected.Mirror == nil ||
		!receipt.ValidForHandoff(expected) {
		return RunJournal{}, errors.New("v2 supervisor stop receipt does not match handoff")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentCgroup == nil || journal.ContainmentScans != nil ||
			!linuxCgroupReceiptMatchesReservation(*journal.ContainmentCgroup, receipt) {
			return errors.New("v2 stop receipt has no matching cgroup producer reservation")
		}
		if journal.ContainmentHandoff != nil {
			current := journal.ContainmentHandoff
			if !sameBoundV2Handoff(current, &expected) {
				return errors.New("v2 supervisor handoff compare-and-set mismatch")
			}
			if current.StopReceipt != nil {
				if current.StopReceipt.Equal(receipt) {
					return nil
				}
				return errors.New("v2 supervisor stop receipt cannot replace an existing witness")
			}
			if !current.Equal(expected) {
				return errors.New("v2 supervisor handoff compare-and-set mismatch")
			}
			copy := receipt
			copy.CgroupIdentity = cloneStateCgroupIdentity(receipt.CgroupIdentity)
			copy.LinuxProof = cloneStateStopProof(receipt.LinuxProof)
			current.StopReceipt = &copy
			journal.ContainmentUnproven = false
			return nil
		}
		value, err := expected.ToSupervisor()
		if err != nil {
			return err
		}
		current := journal.ContainmentAuthority
		if current == nil || current.Version != authority.SupervisorVersionV2 ||
			journal.PID != value.TargetPID || journal.ProcessIdentity != value.TargetIdentity {
			return errors.New("v2 supervisor handoff is not pending or committed")
		}
		candidate := value.Clone()
		candidate.StopReceipt = nil
		currentBinding := current.Clone()
		currentBinding.StopReceipt = nil
		if !currentBinding.Equal(candidate) || !linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *current) {
			return errors.New("v2 committed authority conflicts with handoff")
		}
		if current.StopReceipt != nil {
			if current.StopReceipt.Equal(receipt) {
				return nil
			}
			return errors.New("v2 supervisor stop receipt cannot replace an existing witness")
		}
		copy := receipt
		copy.CgroupIdentity = cloneStateCgroupIdentity(receipt.CgroupIdentity)
		copy.LinuxProof = cloneStateStopProof(receipt.LinuxProof)
		current.StopReceipt = &copy
		journal.ContainmentUnproven = false
		return nil
	})
}

// ClearSupervisorHandoff preserves the v1 release path. V2 Linux handoffs are
// rejected here and must use the Linux-only opaque platform release API.
func (store *Store) ClearSupervisorHandoff(key RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version == authority.SupervisorHandoffVersionV2 {
		return RunJournal{}, errors.New("v2 supervisor handoff release requires a platform cgroup release")
	}
	if expected.StopReceipt == nil {
		return RunJournal{}, errors.New("supervisor handoff stop receipt is required before release")
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !proof.ValidFor(expected) {
		return RunJournal{}, errors.New("supervisor handoff release proof does not match handoff")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff == nil {
			if journal.ContainmentAuthority == nil && !journal.HasProcessDetails() {
				return nil
			}
			return errors.New("supervisor handoff is not pending")
		}
		if !journal.ContainmentHandoff.Equal(expected) {
			return errors.New("supervisor handoff compare-and-set mismatch")
		}
		if journal.ContainmentUnproven {
			return errors.New("supervisor handoff has unresolved containment observation")
		}
		if err := journal.requireLinuxScansSealedForHandoff(expected); err != nil {
			return err
		}
		current := journal.ContainmentHandoff
		if !proof.ValidFor(*current) {
			return errors.New("supervisor handoff release proof does not match journal")
		}
		if current.StopReceipt == nil {
			return errors.New("supervisor handoff stop receipt is required before release")
		}
		if !current.StopReceipt.ValidForHandoff(*current) || *current.StopReceipt != *expected.StopReceipt {
			return errors.New("supervisor handoff stop receipt is invalid")
		}
		journal.ContainmentHandoff = nil
		journal.ContainmentScans = nil
		return nil
	})
}

// ClearSupervisorHandoffAfterAbort clears a prepared handoff only when the
// exact current record is still pending and an independent platform proof
// identifies a definitive pre-authority abort. This path is intentionally
// separate from receipt/release cleanup: an abort proof is not a StopReceipt.
//
// Production callers may use this only while the app owns the Store lifetime
// lock, the old writer is quiesced, and the platform has proved the named Job
// and every exact target/helper process identity are absent. A caller boolean
// or a missing response is not an abort proof and must not be converted into
// one.
func (store *Store) ClearSupervisorHandoffAfterAbort(key RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffAbortProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version == authority.SupervisorHandoffVersionV2 {
		return RunJournal{}, errors.New("v2 cgroup handoff cannot use the v1 pre-authority abort path")
	}
	if expected.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff abort cannot carry a stop receipt")
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !proof.ValidFor(expected) {
		return RunJournal{}, errors.New("supervisor handoff abort proof does not match handoff")
	}
	return store.mutateJournalIfChanged(key, func(journal *RunJournal) (bool, error) {
		if journal.ContainmentHandoff == nil {
			return false, errors.New("supervisor handoff is not pending")
		}
		if !journal.ContainmentHandoff.Equal(expected) {
			return false, errors.New("supervisor handoff compare-and-set mismatch")
		}
		current := journal.ContainmentHandoff
		if current.StopReceipt != nil {
			return false, errors.New("supervisor handoff abort cannot clear a durable stop receipt")
		}
		if journal.ContainmentAuthority != nil || journal.HasProcessDetails() {
			return false, errors.New("supervisor handoff abort conflicts with committed containment")
		}
		if !proof.ValidFor(*current) {
			return false, errors.New("supervisor handoff abort proof does not match journal")
		}
		if journal.ContainmentUnproven {
			return false, errors.New("supervisor handoff has unresolved containment observation")
		}
		if expected.OwnerKind == authority.OwnerKindLinuxHelper {
			if expected.SupervisorPID == 0 {
				if journal.ContainmentScans != nil {
					return false, errors.New("unbound Linux handoff unexpectedly has scan state")
				}
			} else if journal.ContainmentScans == nil || !journal.ContainmentScans.matchesHandoff(expected) ||
				!initialPrecommitLinuxScansAbortable(*journal.ContainmentScans) {
				return false, errors.New("Linux supervisor pre-commit scan state is unresolved")
			}
		}
		journal.ContainmentHandoff = nil
		journal.ContainmentScans = nil
		return true, nil
	})
}

func sameBoundV2Handoff(left, right *authority.SupervisorHandoff) bool {
	return left != nil && right != nil && left.Version == authority.SupervisorHandoffVersionV2 &&
		right.Version == authority.SupervisorHandoffVersionV2 && left.SameLaunch(*right) &&
		left.SupervisorPID == right.SupervisorPID && left.SupervisorIdentity == right.SupervisorIdentity &&
		equalAuthorityProcess(left.Mirror, right.Mirror)
}
