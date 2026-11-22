package state

import (
	"errors"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

// RunContainmentState is the owned-process and containment block of RunJournal:
// process marker, supervisor authority or handoff, cgroup reservation and scan ledger.
type RunContainmentState struct {
	PID             int       `json:"pid"`
	ProcessIdentity string    `json:"process_identity"`
	StartedAt       time.Time `json:"started_at"`
	// ContainmentAuthority is optional for backwards compatibility with
	// journals created before the independent Windows supervisor existed. A
	// process marker without it remains intentionally unrecoverable on Windows.
	ContainmentAuthority *authority.Supervisor `json:"containment_authority,omitempty"`
	// ContainmentHandoff is the durable pre-authority launch fence. It is
	// mutually exclusive with ContainmentAuthority and is cleared only by the
	// dedicated commit or release-proof mutations.
	ContainmentHandoff *authority.SupervisorHandoff `json:"containment_handoff,omitempty"`
	// ContainmentCgroup is the additive v2 Linux run-leaf reservation and
	// producer ledger. It remains in this same RunJournal from pre-mkdir reserve
	// through receipt-backed release, including while PID is zero.
	ContainmentCgroup *LinuxCgroupReservation `json:"containment_cgroup,omitempty"`
	// ContainmentCgroupReleaseProof is the exact post-removal proof retained in
	// this same RunJournal after atomic v2 clear. It is a replay tombstone, not
	// pending containment; only the exact successful clear is acknowledged.
	ContainmentCgroupReleaseProof *authority.SupervisorHandoffReleaseProof `json:"containment_cgroup_release_proof,omitempty"`
	// ContainmentCgroupPreGateReleaseProof is the matching receipt-bound
	// tombstone for a gate-never-produced, PID-zero reservation clear.
	ContainmentCgroupPreGateReleaseProof *authority.LinuxCgroupPreGateReleaseProof `json:"containment_cgroup_pre_gate_release_proof,omitempty"`
	// ContainmentScans is the additive durable pre-scan/result ledger for Linux
	// helper and daemon-mirror observations. Missing is valid for journals
	// written before scan fencing existed; bound legacy Linux owners then remain
	// unresolved rather than being upgraded from readback or process absence.
	ContainmentScans *ContainmentScanJournal `json:"containment_scans,omitempty"`
	// ContainmentUnproven records that a local stop attempt could not prove the
	// complete owned process boundary. Recovery must retain the marker until a
	// later positive stop proof clears this flag explicitly.
	ContainmentUnproven bool `json:"containment_unproven,omitempty"`
}

func (journal RunContainmentState) HasProcessDetails() bool {
	return journal.PID != 0 || journal.ProcessIdentity != "" || !journal.StartedAt.IsZero()
}

// ContainmentScanProgress tracks one scanner's monotonic durable scan fence.
// PendingSequence remains set through CleanResultSequence until the scanner
// receives the durable clean-result ACK and retires that exact fence.
// SealedSequence is a one-way quiescence proof for the exact final clean
// sequence; zero (including a missing legacy field) is never sealed.
// UnprovenSequence is sticky for the lifetime of the process authority.
type ContainmentScanProgress struct {
	PendingSequence     uint64 `json:"pending_sequence,omitempty"`
	CleanResultSequence uint64 `json:"clean_result_sequence,omitempty"`
	CompletedSequence   uint64 `json:"completed_sequence,omitempty"`
	SealedSequence      uint64 `json:"sealed_sequence,omitempty"`
	UnprovenSequence    uint64 `json:"unproven_sequence,omitempty"`
	FailureKind         string `json:"failure_kind,omitempty"`
}

// ContainmentScanJournal binds helper and volatile mirror scan progress to the
// exact Linux launch and one mirror generation recorded by this RunJournal's
// run/generation key.
type ContainmentScanJournal struct {
	Version            int                     `json:"version"`
	LaunchToken        string                  `json:"launch_token"`
	TargetPID          int                     `json:"target_pid"`
	TargetIdentity     string                  `json:"target_identity"`
	SupervisorPID      int                     `json:"supervisor_pid"`
	SupervisorIdentity string                  `json:"supervisor_identity"`
	MirrorIdentity     string                  `json:"mirror_identity"`
	Helper             ContainmentScanProgress `json:"helper"`
	Mirror             ContainmentScanProgress `json:"mirror"`
}

func (scans ContainmentScanJournal) clean() bool {
	return scans.Helper.PendingSequence == 0 && scans.Helper.CleanResultSequence == 0 && scans.Helper.UnprovenSequence == 0 && scans.Helper.CompletedSequence > 0 &&
		scans.Mirror.PendingSequence == 0 && scans.Mirror.CleanResultSequence == 0 && scans.Mirror.UnprovenSequence == 0 && scans.Mirror.CompletedSequence > 0
}

func (progress ContainmentScanProgress) sealed() bool {
	return progress.CompletedSequence > 0 && progress.SealedSequence == progress.CompletedSequence &&
		progress.PendingSequence == 0 && progress.CleanResultSequence == 0 && progress.UnprovenSequence == 0 && progress.FailureKind == ""
}

func (scans ContainmentScanJournal) sealed() bool {
	return scans.Helper.sealed() && scans.Mirror.sealed()
}

func (scans ContainmentScanJournal) matchesHandoff(handoff authority.SupervisorHandoff) bool {
	return scans.Version == 1 && scans.LaunchToken == handoff.LaunchToken && scans.TargetPID == handoff.TargetPID &&
		scans.TargetIdentity == handoff.TargetIdentity && scans.SupervisorPID == handoff.SupervisorPID &&
		scans.SupervisorIdentity == handoff.SupervisorIdentity
}

func (scans ContainmentScanJournal) matchesAuthority(value authority.Supervisor) bool {
	return scans.Version == 1 && scans.LaunchToken == value.LaunchToken && scans.TargetPID == value.TargetPID &&
		scans.TargetIdentity == value.TargetIdentity && scans.SupervisorPID == value.SupervisorPID &&
		scans.SupervisorIdentity == value.SupervisorIdentity
}

// SetProcessDetails persists an execution process identity that recovery code
// can verify before acting on a retained PID.
func (store *Store) SetProcessDetails(key RunKey, pid int, identity string, startedAt time.Time) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) || startedAt.IsZero() {
		return RunJournal{}, errors.New("process details are invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("process launch is fenced by Linux clone abort")
		}
		if journal.LinuxCloneIntent != nil {
			return errors.New("Linux clone run requires v2 cgroup process registration")
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("process details require v2 cgroup handoff mutation")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("process details cannot be recreated after v2 containment release")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("process details cannot be recreated after v2 pre-gate release")
		}
		if journal.ContainmentHandoff != nil {
			return errors.New("process details require supervisor handoff mutation")
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot register process details during cleanup")
		}
		if journal.HasProcessDetails() {
			if journal.PID != pid || journal.ProcessIdentity != identity {
				return errors.New("process details cannot replace an uncleared owner")
			}
			return nil
		}
		journal.PID = pid
		journal.ProcessIdentity = identity
		journal.StartedAt = startedAt
		return nil
	})
}

// SetProcessDetailsWithAuthority atomically persists the process marker and
// its independently releasable containment authority. The single journal
// mutation prevents recovery from observing a marker without the authority
// required to verify and release that process.
func (store *Store) SetProcessDetailsWithAuthority(key RunKey, pid int, identity string, startedAt time.Time, value authority.Supervisor) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) || startedAt.IsZero() {
		return RunJournal{}, errors.New("process details are invalid")
	}
	if err := value.Validate(); err != nil {
		return RunJournal{}, err
	}
	if value.Version == authority.SupervisorVersionV2 {
		return RunJournal{}, errors.New("v2 containment authority requires supervisor handoff commit")
	}
	if value.TargetPID != pid || value.TargetIdentity != identity {
		return RunJournal{}, errors.New("containment authority target does not match process details")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("process launch is fenced by Linux clone abort")
		}
		if journal.LinuxCloneIntent != nil {
			return errors.New("Linux clone run requires v2 cgroup process registration")
		}
		if journal.ContainmentHandoff != nil {
			return errors.New("process details require supervisor handoff mutation")
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("process details require v2 cgroup handoff mutation")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("process details cannot be recreated after v2 containment release")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("process details cannot be recreated after v2 pre-gate release")
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot register process details during cleanup")
		}
		if journal.HasProcessDetails() {
			if journal.PID != pid || journal.ProcessIdentity != identity {
				return errors.New("process details cannot replace an uncleared owner")
			}
		} else {
			journal.PID = pid
			journal.ProcessIdentity = identity
			journal.StartedAt = startedAt
		}
		if journal.ContainmentAuthority != nil {
			if journal.ContainmentAuthority.Equal(value) {
				return nil
			}
			// A retry after a stop-receipt write must not erase the receipt by
			// replaying the original authority without its terminal witness.
			existing := journal.ContainmentAuthority.Clone()
			existing.StopReceipt = nil
			candidate := value.Clone()
			candidate.StopReceipt = nil
			if existing.Equal(candidate) && journal.ContainmentAuthority.StopReceipt != nil && value.StopReceipt == nil {
				return nil
			}
			return errors.New("containment authority cannot replace an uncleared owner")
		}
		cloned := value.Clone()
		journal.ContainmentAuthority = &cloned
		return nil
	})
}

// MarkContainmentUnproven retains an exact process marker when local cleanup
// cannot prove that the complete owned boundary stopped. It is idempotent for
// the same process identity and survives daemon restart.
func (store *Store) MarkContainmentUnproven(key RunKey, pid int, identity string) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("process details are invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.PID != pid || journal.ProcessIdentity != identity || !journal.HasProcessDetails() {
			return errors.New("containment uncertainty requires the exact process marker")
		}
		journal.ContainmentUnproven = true
		return nil
	})
}

// ClearContainmentUnproven clears a prior uncertainty marker only after the
// caller has completed a fresh identity-bound stop proof.
func (store *Store) ClearContainmentUnproven(key RunKey, pid int, identity string) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("process details are invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentCgroup != nil {
			return errors.New("v2 containment uncertainty clears only with an exact stop receipt")
		}
		if !journal.HasProcessDetails() {
			return nil
		}
		if journal.PID != pid || journal.ProcessIdentity != identity {
			return errors.New("persisted process details changed during containment proof")
		}
		if journal.ContainmentScans != nil && !journal.ContainmentScans.clean() {
			return errors.New("containment scan uncertainty is sticky")
		}
		if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.OwnerKind == authority.OwnerKindLinuxHelper && journal.ContainmentScans == nil {
			return errors.New("Linux containment scan fence is missing")
		}
		journal.ContainmentUnproven = false
		return nil
	})
}

// SetContainmentAuthority persists the typed binding for an already persisted
// process marker. It never replaces an uncleared authority with a different
// launch, so a stale callback cannot retarget recovery.
func (store *Store) SetContainmentAuthority(key RunKey, pid int, identity string, value authority.Supervisor) (RunJournal, error) {
	if pid <= 0 || strings.TrimSpace(identity) == "" {
		return RunJournal{}, errors.New("process details are invalid")
	}
	if err := value.Validate(); err != nil {
		return RunJournal{}, err
	}
	if value.Version == authority.SupervisorVersionV2 {
		return RunJournal{}, errors.New("v2 containment authority requires supervisor handoff commit")
	}
	if value.TargetPID != pid || value.TargetIdentity != identity {
		return RunJournal{}, errors.New("containment authority target does not match process details")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("process launch is fenced by Linux clone abort")
		}
		if journal.LinuxCloneIntent != nil {
			return errors.New("Linux clone run requires v2 cgroup process registration")
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("v2 containment authority requires supervisor handoff commit")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("v2 containment authority cannot be recreated after release")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("v2 pre-gate containment authority cannot be recreated after release")
		}
		if journal.ContainmentHandoff != nil {
			return errors.New("containment authority requires supervisor handoff mutation")
		}
		if journal.PID != pid || journal.ProcessIdentity != identity || journal.StartedAt.IsZero() {
			return errors.New("containment authority requires a persisted process marker")
		}
		if journal.ContainmentAuthority != nil {
			if journal.ContainmentAuthority.Equal(value) {
				return nil
			}
			// A retry after a stop-receipt write must not erase the receipt by
			// replaying the original authority without its terminal witness. The
			// binding itself is still idempotent, so preserve the existing record
			// when only its receipt differs.
			existing := journal.ContainmentAuthority.Clone()
			existing.StopReceipt = nil
			candidate := value.Clone()
			candidate.StopReceipt = nil
			if existing.Equal(candidate) && journal.ContainmentAuthority.StopReceipt != nil && value.StopReceipt == nil {
				return nil
			}
			return errors.New("containment authority cannot replace an uncleared owner")
		}
		cloned := value.Clone()
		journal.ContainmentAuthority = &cloned
		return nil
	})
}

// RecordContainmentStopReceipt durably records the exact positive helper
// witness before recovery is allowed to compare-and-clear the process marker.
// It is idempotent for the same authority and receipt, and rejects any
// replacement launch.
func (store *Store) RecordContainmentStopReceipt(key RunKey, pid int, identity string, receipt authority.StopReceipt) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil {
			return errors.New("containment stop receipt requires supervisor handoff mutation")
		}
		if journal.PID != pid || journal.ProcessIdentity != identity || journal.ContainmentAuthority == nil {
			return errors.New("containment stop receipt has no matching authority")
		}
		if journal.ContainmentAuthority.Version == authority.SupervisorVersionV2 {
			current := journal.ContainmentAuthority
			if journal.ContainmentCgroup == nil || journal.ContainmentScans != nil ||
				!linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *current) ||
				!linuxCgroupReceiptMatchesReservation(*journal.ContainmentCgroup, receipt) ||
				!receipt.ValidFor(*current) {
				return errors.New("v2 containment stop receipt does not match exact cgroup authority")
			}
			if current.StopReceipt != nil {
				if current.StopReceipt.Equal(receipt) {
					return nil
				}
				return errors.New("v2 containment stop receipt cannot replace an existing witness")
			}
			copy := receipt
			copy.CgroupIdentity = cloneStateCgroupIdentity(receipt.CgroupIdentity)
			copy.LinuxProof = cloneStateStopProof(receipt.LinuxProof)
			current.StopReceipt = &copy
			journal.ContainmentUnproven = false
			return nil
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("v1 containment receipt cannot act on a v2 cgroup reservation")
		}
		if journal.ContainmentUnproven {
			return errors.New("containment scan observation is unresolved")
		}
		if err := journal.requireLinuxScansSealedForAuthority(*journal.ContainmentAuthority); err != nil {
			return err
		}
		if !receipt.ValidFor(*journal.ContainmentAuthority) {
			return errors.New("containment stop receipt does not match containment authority")
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

// RecordSupervisorRecoveryScanSeal persists a helper's authenticated
// owner-loss terminal scan after a daemon restart. The caller may use this only
// after the exact helper recovery endpoint has attested a clean, joined
// terminal scan; the mirror must already have its own durable seal. A pending
// or unproven journal can never be advanced by this recovery-only mutation.
// Replaying the exact proof performs a fresh journal write barrier.
func (store *Store) RecordSupervisorRecoveryScanSeal(key RunKey, expected authority.Supervisor, sequence uint64) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.OwnerKind != authority.OwnerKindLinuxHelper || expected.StopReceipt != nil || sequence == 0 {
		return RunJournal{}, errors.New("Linux helper recovery scan seal identity is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		current := journal.ContainmentAuthority
		if current == nil || journal.PID != expected.TargetPID || journal.ProcessIdentity != expected.TargetIdentity || current.StopReceipt != nil {
			return errors.New("Linux helper recovery seal has no active matching authority")
		}
		currentBinding := current.Clone()
		currentBinding.StopReceipt = nil
		candidate := expected.Clone()
		candidate.StopReceipt = nil
		if !currentBinding.Equal(candidate) {
			return errors.New("Linux helper recovery seal authority identity mismatch")
		}
		scans := journal.ContainmentScans
		if scans == nil || !scans.matchesAuthority(expected) {
			return errors.New("Linux helper recovery seal scan fence is missing or mismatched")
		}
		if journal.ContainmentUnproven || !scans.Mirror.sealed() {
			return errors.New("Linux helper recovery seal requires a durable clean mirror seal")
		}
		progress := &scans.Helper
		if progress.SealedSequence == sequence && progress.sealed() {
			return nil
		}
		if progress.CompletedSequence == 0 || progress.CompletedSequence == ^uint64(0) || sequence != progress.CompletedSequence+1 ||
			progress.PendingSequence != 0 || progress.CleanResultSequence != 0 || progress.SealedSequence != 0 ||
			progress.UnprovenSequence != 0 || progress.FailureKind != "" {
			return errors.New("Linux helper recovery scan proof is not the exact next clean sequence")
		}
		progress.CompletedSequence = sequence
		progress.SealedSequence = sequence
		return nil
	})
}

// ClearProcessDetails records that the process identified by the expected PID
// and identity has been stopped. The compare-and-clear guard prevents recovery
// from erasing a newer process record if another owner replaced the process
// between termination and durable marker persistence. Replaying the same
// clear after the marker is already persisted is idempotent.
func (store *Store) ClearProcessDetails(key RunKey, pid int, identity string) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("process details are invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version == authority.SupervisorVersionV2 {
			return errors.New("v2 process details require ClearProcessDetailsV2")
		}
		if journal.ContainmentHandoff != nil {
			return errors.New("process details require supervisor handoff mutation")
		}
		if !journal.HasProcessDetails() {
			return nil
		}
		if journal.PID != pid || journal.ProcessIdentity != identity {
			return errors.New("persisted process details changed during termination")
		}
		if journal.ContainmentUnproven {
			return errors.New("containment stop proof is unresolved")
		}
		if journal.ContainmentAuthority != nil {
			if err := journal.requireLinuxScansSealedForAuthority(*journal.ContainmentAuthority); err != nil {
				return err
			}
			if journal.ContainmentAuthority.StopReceipt == nil || !journal.ContainmentAuthority.StopReceipt.ValidFor(*journal.ContainmentAuthority) {
				return errors.New("containment stop receipt is required before clearing process details")
			}
		}
		journal.PID = 0
		journal.ProcessIdentity = ""
		journal.StartedAt = time.Time{}
		journal.ContainmentAuthority = nil
		journal.ContainmentScans = nil
		return nil
	})
}

func sameContainmentAuthority(left, right *authority.Supervisor) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameContainmentHandoff(left, right *authority.SupervisorHandoff) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameContainmentScans(left, right *ContainmentScanJournal) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func initialPrecommitLinuxScansAbortable(scans ContainmentScanJournal) bool {
	return initialPrecommitLinuxScanProgressAbortable(scans.Helper) && initialPrecommitLinuxScanProgressAbortable(scans.Mirror)
}

func initialPrecommitLinuxScanProgressAbortable(progress ContainmentScanProgress) bool {
	if progress.UnprovenSequence != 0 || progress.FailureKind != "" || progress.CompletedSequence > 1 ||
		progress.PendingSequence > 1 || progress.CleanResultSequence > 1 || progress.SealedSequence > 1 {
		return false
	}
	if progress.CompletedSequence == 0 {
		return progress.PendingSequence == 1 && progress.SealedSequence == 0
	}
	return progress.PendingSequence == 0 && progress.CleanResultSequence == 0 &&
		(progress.SealedSequence == 0 || progress.SealedSequence == progress.CompletedSequence)
}

func (journal RunContainmentState) requireLinuxScansCleanForAuthority(value authority.Supervisor) error {
	if value.OwnerKind != authority.OwnerKindLinuxHelper {
		return nil
	}
	if journal.ContainmentScans == nil || !journal.ContainmentScans.matchesAuthority(value) {
		return errors.New("Linux supervisor scan fence is missing or mismatched")
	}
	if journal.ContainmentUnproven || !journal.ContainmentScans.clean() {
		return errors.New("Linux supervisor scan evidence is unresolved")
	}
	return nil
}

func (journal RunContainmentState) requireLinuxScansSealedForHandoff(handoff authority.SupervisorHandoff) error {
	if handoff.OwnerKind != authority.OwnerKindLinuxHelper {
		return nil
	}
	if handoff.SupervisorPID <= 0 || handoff.SupervisorIdentity == "" || journal.ContainmentScans == nil ||
		!journal.ContainmentScans.matchesHandoff(handoff) {
		return errors.New("Linux supervisor scan fence is missing or mismatched")
	}
	if journal.ContainmentUnproven || !journal.ContainmentScans.sealed() {
		return errors.New("Linux supervisor scan seals are unresolved")
	}
	return nil
}

func (journal RunContainmentState) requireLinuxScansSealedForAuthority(value authority.Supervisor) error {
	if value.OwnerKind != authority.OwnerKindLinuxHelper {
		return nil
	}
	if journal.ContainmentScans == nil || !journal.ContainmentScans.matchesAuthority(value) {
		return errors.New("Linux supervisor scan fence is missing or mismatched")
	}
	if journal.ContainmentUnproven || !journal.ContainmentScans.sealed() {
		return errors.New("Linux supervisor scan seals are unresolved")
	}
	return nil
}

func (journal RunContainmentState) validateContainmentAuthority() error {
	if journal.ContainmentAuthority != nil {
		if err := journal.ContainmentAuthority.Validate(); err != nil ||
			journal.ContainmentAuthority.TargetPID != journal.PID ||
			journal.ContainmentAuthority.TargetIdentity != journal.ProcessIdentity {
			return errors.New("run journal containment authority is invalid")
		}
		if journal.ContainmentAuthority.Version == authority.SupervisorVersionV2 {
			if journal.ContainmentCgroup == nil || journal.ContainmentScans != nil ||
				journal.ContainmentCgroupReleaseProof != nil ||
				!linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *journal.ContainmentAuthority) ||
				journal.ContainmentCgroup.Gate == nil || !journal.StartedAt.Equal(journal.ContainmentCgroup.Gate.ObservedAt) ||
				journal.ContainmentAuthority.StopReceipt != nil && journal.ContainmentUnproven {
				return errors.New("run journal v2 containment authority is invalid")
			}
		} else if journal.ContainmentCgroup != nil {
			return errors.New("v1 containment authority cannot use a v2 cgroup reservation")
		}
	}
	return nil
}

func (journal RunContainmentState) validateContainmentHandoff(granted bool) error {
	if journal.ContainmentHandoff != nil {
		if journal.ContainmentAuthority != nil || journal.HasProcessDetails() || !granted {
			return errors.New("run journal containment handoff is invalid")
		}
		if err := journal.ContainmentHandoff.Validate(); err != nil {
			return errors.New("run journal containment handoff is invalid")
		}
		if journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersionV2 {
			if journal.ContainmentCgroup == nil || journal.ContainmentScans != nil ||
				journal.ContainmentCgroupReleaseProof != nil ||
				!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, *journal.ContainmentHandoff, journal.ContainmentHandoff.SupervisorPID > 0) ||
				journal.ContainmentHandoff.StopReceipt != nil && journal.ContainmentUnproven {
				return errors.New("run journal v2 containment handoff is invalid")
			}
		} else if journal.ContainmentCgroup != nil {
			return errors.New("v1 containment handoff cannot use a v2 cgroup reservation")
		}
	}
	return nil
}

func (journal RunContainmentState) validateContainmentCgroup(granted bool) error {
	if journal.ContainmentCgroup != nil {
		reservation := journal.ContainmentCgroup
		if reservation.Validate() != nil || !granted || journal.ContainmentScans != nil {
			return errors.New("run journal v2 cgroup reservation is invalid")
		}
		if reservation.AbortProof != nil && (journal.ContainmentUnproven || journal.ContainmentAuthority != nil ||
			journal.ContainmentHandoff != nil || journal.HasProcessDetails()) {
			return errors.New("run journal aborted cgroup reservation has active owner state")
		}
		if reservation.PreGateStopReceipt != nil && (journal.ContainmentUnproven || journal.ContainmentAuthority != nil ||
			journal.ContainmentHandoff != nil || journal.HasProcessDetails()) {
			return errors.New("run journal pre-gate cgroup receipt has active owner state")
		}
		if journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil {
			if journal.HasProcessDetails() {
				return errors.New("uncommitted v2 cgroup reservation has process authority")
			}
		}
		if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version != authority.SupervisorVersionV2 {
			return errors.New("v2 cgroup reservation cannot accompany a v1 authority")
		}
		if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version != authority.SupervisorHandoffVersionV2 {
			return errors.New("v2 cgroup reservation cannot accompany a v1 handoff")
		}
	}
	return nil
}

func (journal RunContainmentState) validateContainmentScans() error {
	if journal.ContainmentScans != nil {
		scans := journal.ContainmentScans
		if journal.ContainmentCgroup != nil || scans.Version != 1 || len(scans.LaunchToken) != authority.TokenBytes*2 || !validHex(scans.LaunchToken) ||
			len(scans.MirrorIdentity) != authority.TokenBytes*2 || !validHex(scans.MirrorIdentity) || scans.TargetPID <= 0 ||
			!validRequiredString(scans.TargetIdentity, 4096) || scans.SupervisorPID <= 0 ||
			!validRequiredString(scans.SupervisorIdentity, 4096) ||
			!validContainmentScanProgress(scans.Helper) || !validContainmentScanProgress(scans.Mirror) {
			return errors.New("run journal containment scan state is invalid")
		}
		if journal.ContainmentHandoff != nil {
			handoff := journal.ContainmentHandoff
			if handoff.OwnerKind != authority.OwnerKindLinuxHelper || handoff.SupervisorPID <= 0 || !scans.matchesHandoff(*handoff) {
				return errors.New("run journal containment scan handoff is invalid")
			}
		} else if journal.ContainmentAuthority != nil {
			value := journal.ContainmentAuthority
			if value.OwnerKind != authority.OwnerKindLinuxHelper || !scans.matchesAuthority(*value) {
				return errors.New("run journal containment scan authority is invalid")
			}
		} else {
			return errors.New("run journal containment scan state has no Linux owner")
		}
		if (scans.Helper.UnprovenSequence != 0 || scans.Mirror.UnprovenSequence != 0) && !journal.ContainmentUnproven {
			return errors.New("run journal containment scan failure is not sticky")
		}
	}
	return nil
}

func (journal RunContainmentState) validateContainmentCgroupReleaseProof() error {
	if releaseProof := journal.ContainmentCgroupReleaseProof; releaseProof != nil {
		if releaseProof.Validate() != nil || releaseProof.Version != authority.SupervisorHandoffVersionV2 ||
			journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil || journal.ContainmentCgroup != nil ||
			journal.ContainmentScans != nil || journal.ContainmentCgroupPreGateReleaseProof != nil ||
			journal.HasProcessDetails() || journal.PID != 0 ||
			journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() || journal.ContainmentUnproven {
			return errors.New("run journal v2 cgroup release proof is invalid")
		}
	}
	return nil
}

func (journal RunContainmentState) validateContainmentCgroupPreGateReleaseProof() error {
	if releaseProof := journal.ContainmentCgroupPreGateReleaseProof; releaseProof != nil {
		if releaseProof.Validate() != nil || journal.ContainmentCgroupReleaseProof != nil ||
			journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil || journal.ContainmentCgroup != nil ||
			journal.ContainmentScans != nil || journal.HasProcessDetails() || journal.PID != 0 ||
			journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() || journal.ContainmentUnproven {
			return errors.New("run journal pre-gate cgroup release proof is invalid")
		}
	}
	return nil
}

func validContainmentScanProgress(progress ContainmentScanProgress) bool {
	if progress.SealedSequence != 0 &&
		(progress.CompletedSequence == 0 || progress.SealedSequence != progress.CompletedSequence ||
			progress.PendingSequence != 0 || progress.CleanResultSequence != 0 || progress.UnprovenSequence != 0 || progress.FailureKind != "") {
		return false
	}
	if progress.PendingSequence != 0 {
		if progress.UnprovenSequence != 0 || progress.CompletedSequence == ^uint64(0) ||
			progress.PendingSequence != progress.CompletedSequence+1 {
			return false
		}
	}
	if progress.CleanResultSequence != 0 &&
		(progress.PendingSequence != progress.CleanResultSequence || progress.CompletedSequence == ^uint64(0) ||
			progress.CleanResultSequence != progress.CompletedSequence+1 || progress.UnprovenSequence != 0) {
		return false
	}
	if progress.UnprovenSequence == 0 {
		return progress.FailureKind == ""
	}
	if progress.PendingSequence != 0 || progress.CleanResultSequence != 0 || !validRequiredString(progress.FailureKind, 128) {
		return false
	}
	return progress.CompletedSequence != ^uint64(0) && progress.UnprovenSequence == progress.CompletedSequence+1 ||
		progress.UnprovenSequence == progress.CompletedSequence && progress.FailureKind == "clean_retire_ack_failure"
}
