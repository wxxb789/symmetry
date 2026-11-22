//go:build linux && amd64

package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// journalStoreLinuxAndAMD64Methods is what the linux/amd64 owner path adds to journalStoreBase:
// the cgroup-v2 and clone-abort Store methods this target alone selects.
const journalStoreLinuxAndAMD64Methods = `AbortLinuxCgroupReservation
AbortNeverCreatedLinuxCgroupReservation
ArmLinuxCgroupLauncherGrant
BindLinuxCgroupGate
BindLinuxCgroupHelper
BindLinuxCgroupLauncher
BindLinuxCgroupLeaf
BindLinuxCloneSeed
BindSupervisorHandoffV2
ClearLinuxCgroupReservationV2
ClearLinuxCloneNoTargetRetention
ClearProcessDetailsV2
ClearSupervisorHandoffV2
CompleteLinuxCloneAbort
FenceLinuxCloneBeforeCheckout
MarkLinuxCgroupCreateStarted
RecordLinuxCgroupHelperExit
RecordLinuxCgroupLauncherExit
RecordLinuxCgroupPreGateStopReceipt
RecordLinuxCloneCheckoutReady
RecordLinuxClonePromotion
ReserveLinuxCgroup
ReserveLinuxCloneIntent
ReserveLinuxClonePromotion`

func TestJournalStoreMethodSetIsExactOnLinuxAMD64(t *testing.T) {
	want := append(strings.Split(journalStoreBaseMethods, "\n"), strings.Split(journalStoreLinuxAndAMD64Methods, "\n")...)
	slices.Sort(want)
	got := interfaceMethodNames(reflect.TypeOf((*journalStore)(nil)).Elem())
	if !slices.Equal(got, want) {
		t.Fatalf("journalStore members = %s\nwant %s\nadded %s\nremoved %s", got, want, difference(got, want), difference(want, got))
	}
}

type journalStoreDropProbeProcess struct {
	linuxV2WitnessDropStatusFakeProcess
	calls int
}

func (process *journalStoreDropProbeProcess) LinuxV2WitnessDropStatus(context.Context) (execution.LinuxV2WitnessDropStatus, error) {
	process.calls++
	return execution.LinuxV2WitnessDropStatus{}, errors.New("storeless probe must not consult the drop-status reader")
}

func TestStorelessDaemonTakesEveryLinuxStoreNilBranch(t *testing.T) {
	key := state.RunKey{RunID: "run", Generation: 1}
	tests := []struct {
		site string
		run  func(t *testing.T, daemon *daemon)
	}{
		{"linux_owner_v2_linux.go:1180 loadLinuxCloneRunJournal", func(t *testing.T, daemon *daemon) {
			_, err := daemon.loadLinuxCloneRunJournal(key, protocol.Fence{}, authority.LinuxCloneGrant{}, nil)
			requireErrorMessage(t, err, "Linux clone Store readback is incomplete")
		}},
		{"linux_owner_v2_linux.go:1209 reconcileLinuxClonePromotion", func(t *testing.T, daemon *daemon) {
			err := daemon.reconcileLinuxClonePromotion(context.Background(), state.RunJournal{}, platform.LinuxCgroupV2Release{})
			requireErrorMessage(t, err, "Linux clone promotion reconciliation is incomplete")
		}},
		{"linux_owner_v2_linux.go:1549 linuxCloneAbortHasRetentionOwner", func(t *testing.T, daemon *daemon) {
			retained, err := daemon.linuxCloneAbortHasRetentionOwner(state.RunJournal{})
			if retained || err == nil || err.Error() != "Linux clone retention check lacks the exact stopped no-checkout journal" {
				t.Fatalf("storeless retention check = %v, %v", retained, err)
			}
		}},
		{"linux_owner_witness_linux.go:1416 summarizeResponseLost", func(t *testing.T, daemon *daemon) {
			witness := &linuxV2NativeWitnessRun{request: linuxV2NativeWitnessRequest{Case: "stage_replay"}}
			err := witness.summarizeResponseLost(daemon.store, "")
			requireErrorMessage(t, err, "response-loss summary requires an exact StageReplay Store")
		}},
		{"linux_owner_witness_linux.go:2479 captureHelperDropStatus", func(t *testing.T, daemon *daemon) {
			process := &journalStoreDropProbeProcess{}
			witness := &linuxV2NativeWitnessRun{}
			if err := witness.captureHelperDropStatus(daemon.store, process); err != nil {
				t.Fatalf("storeless drop-status capture = %v, want nil", err)
			}
			if process.calls != 0 {
				t.Fatalf("drop-status reader calls = %d, want 0", process.calls)
			}
		}},
		{"linux_owner_witness_linux.go:2599 recoverLinuxV2NativeWitnessRunKey", func(t *testing.T, daemon *daemon) {
			err := recoverLinuxV2NativeWitnessRunKey(context.Background(), config.Config{}, daemon.store, state.RunJournal{})
			requireErrorMessage(t, err, "exact owner-witness recovery requires its live context, locked Store, and selected RunJournal")
		}},
	}
	for _, test := range tests {
		t.Run(test.site, func(t *testing.T) {
			test.run(t, &daemon{})
		})
	}
}
