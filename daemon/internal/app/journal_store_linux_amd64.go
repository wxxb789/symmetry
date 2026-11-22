//go:build linux && amd64

package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

type journalStore interface {
	journalStoreBase
	AbortLinuxCgroupReservation(key state.RunKey, expected state.LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) (state.RunJournal, error)
	AbortNeverCreatedLinuxCgroupReservation(key state.RunKey, fence protocol.Fence, expected state.LinuxCgroupReservation) (state.RunJournal, error)
	ArmLinuxCgroupLauncherGrant(key state.RunKey, fence protocol.Fence, expected state.LinuxCgroupReservation, liveOwners map[state.RunKey]protocol.Fence) (state.RunJournal, error)
	BindLinuxCgroupGate(key state.RunKey, expected state.LinuxCgroupReservation, gate authority.LinuxCgroupGateIdentity) (state.RunJournal, error)
	BindLinuxCgroupHelper(key state.RunKey, expected state.LinuxCgroupReservation, helper authority.LinuxCgroupProcessIdentity) (state.RunJournal, error)
	BindLinuxCgroupLauncher(key state.RunKey, expected state.LinuxCgroupReservation, launcher authority.LinuxCgroupProcessIdentity) (state.RunJournal, error)
	BindLinuxCgroupLeaf(key state.RunKey, expected state.LinuxCgroupReservation, measured authority.LinuxCgroupIdentity) (state.RunJournal, error)
	BindLinuxCloneSeed(key state.RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, seed authority.LinuxCloneSeedReceipt) (state.RunJournal, error)
	BindSupervisorHandoffV2(key state.RunKey, expected authority.SupervisorHandoff, helper authority.LinuxCgroupProcessIdentity, mirror authority.LinuxCgroupProcessIdentity) (state.RunJournal, error)
	ClearLinuxCgroupReservationV2(key state.RunKey, expected state.LinuxCgroupReservation, release platform.LinuxCgroupV2Release) (state.RunJournal, error)
	ClearLinuxCloneNoTargetRetention(key state.RunKey, fence protocol.Fence, proof authority.LinuxCloneAbortProof) (state.RunJournal, error)
	ClearProcessDetailsV2(key state.RunKey, pid int, identity string, release platform.LinuxCgroupV2Release) (state.RunJournal, error)
	ClearSupervisorHandoffV2(key state.RunKey, expected authority.SupervisorHandoff, release platform.LinuxCgroupV2Release) (state.RunJournal, error)
	CompleteLinuxCloneAbort(key state.RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, proof authority.LinuxCloneAbortProof) (state.RunJournal, error)
	FenceLinuxCloneBeforeCheckout(key state.RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent) (state.RunJournal, error)
	MarkLinuxCgroupCreateStarted(key state.RunKey, fence protocol.Fence, expected state.LinuxCgroupReservation, liveOwners map[state.RunKey]protocol.Fence) (state.RunJournal, error)
	RecordLinuxCgroupHelperExit(key state.RunKey, expected state.LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) (state.RunJournal, error)
	RecordLinuxCgroupLauncherExit(key state.RunKey, expected state.LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) (state.RunJournal, error)
	RecordLinuxCgroupPreGateStopReceipt(key state.RunKey, expected state.LinuxCgroupReservation, receipt authority.LinuxCgroupPreGateStopReceipt) (state.RunJournal, error)
	RecordLinuxCloneCheckoutReady(key state.RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, receipt authority.LinuxCloneCheckoutReceipt) (state.RunJournal, error)
	RecordLinuxClonePromotion(key state.RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, candidate authority.LinuxClonePromotionIntent, receipt authority.LinuxClonePromotionReceipt) (state.RunJournal, error)
	ReserveLinuxCgroup(key state.RunKey, fence protocol.Fence, reservation state.LinuxCgroupReservation, liveOwners map[state.RunKey]protocol.Fence) (state.RunJournal, error)
	ReserveLinuxCloneIntent(key state.RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, liveOwners map[state.RunKey]protocol.Fence) (state.RunJournal, error)
	ReserveLinuxClonePromotion(key state.RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, candidate authority.LinuxClonePromotionIntent) (state.RunJournal, error)
}
