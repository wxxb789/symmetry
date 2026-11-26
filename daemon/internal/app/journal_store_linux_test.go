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

func TestJournalStoreMembersMatchRecordedListOnLinuxAMD64(t *testing.T) {
	want := append(strings.Split(journalStoreBaseMethods, "\n"), strings.Split(journalStoreLinuxAndAMD64Methods, "\n")...)
	slices.Sort(want)
	assertMethodSet(t, "journalStore", interfaceMethodNames(reflect.TypeOf((*journalStore)(nil)).Elem()), want)
}

type journalStoreDropProbeProcess struct {
	linuxV2WitnessDropStatusFakeProcess
}

func (*journalStoreDropProbeProcess) LinuxV2WitnessDropStatus(context.Context) (execution.LinuxV2WitnessDropStatus, error) {
	return execution.LinuxV2WitnessDropStatus{}, errors.New("storeless probe must not consult the drop-status reader")
}

func TestStorelessDaemonTakesEveryLinuxStoreNilBranch(t *testing.T) {
	key := state.RunKey{RunID: "run", Generation: 1}
	probes := []storelessProbe{
		{"loadLinuxCloneRunJournal", func(t *testing.T, daemon *daemon) {
			grant, _, _ := journalStoreValidCloneEvidence(t)
			_, err := daemon.loadLinuxCloneRunJournal(key, protocol.Fence{}, grant, nil)
			requireErrorMessage(t, err, "Linux clone Store readback is incomplete")
		}},
		{"reconcileLinuxClonePromotion", func(t *testing.T, daemon *daemon) {
			err := daemon.reconcileLinuxClonePromotion(context.Background(), state.RunJournal{}, platform.LinuxCgroupV2Release{})
			requireErrorMessage(t, err, "Linux clone promotion reconciliation is incomplete")
		}},
		{"linuxCloneAbortHasRetentionOwner", func(t *testing.T, daemon *daemon) {
			journal := state.RunJournal{
				RunID: key.RunID, Generation: key.Generation,
				LinuxCloneIntent:       &authority.LinuxCloneIntent{Version: authority.LinuxCloneVersion},
				LinuxCloneAbortPending: true,
			}
			retained, err := daemon.linuxCloneAbortHasRetentionOwner(journal)
			if retained || err == nil || err.Error() != "Linux clone retention check lacks the exact stopped no-checkout journal" {
				t.Fatalf("storeless retention check = %v, %v", retained, err)
			}
		}},
		{"summarizeResponseLost", func(t *testing.T, daemon *daemon) {
			witness := &linuxV2NativeWitnessRun{request: linuxV2NativeWitnessRequest{Case: "stage_replay"}}
			err := witness.summarizeResponseLost(daemon.store, "")
			requireErrorMessage(t, err, "response-loss summary requires an exact StageReplay Store")
		}},
		{"captureHelperDropStatus", func(t *testing.T, daemon *daemon) {
			process := &journalStoreDropProbeProcess{}
			witness := &linuxV2NativeWitnessRun{}
			if err := witness.captureHelperDropStatus(daemon.store, process); err != nil {
				t.Fatalf("storeless drop-status capture = %v, want nil", err)
			}
		}},
		{"recoverLinuxV2NativeWitnessRunKey", func(t *testing.T, daemon *daemon) {
			journal := state.RunJournal{RunID: key.RunID, Generation: key.Generation}
			_, err := recoverLinuxV2NativeWitnessRunKey(
				context.Background(), config.Config{}, daemon.store, journal, linuxV2NativeWitnessRequest{},
			)
			requireErrorMessage(t, err, "exact owner-witness recovery requires its live context, locked Store, and selected RunJournal")
		}},
		{"persistLinuxV2Mutation refuses a missing Store", func(t *testing.T, daemon *daemon) {
			err := daemon.persistLinuxV2Mutation(nil, key, protocol.Fence{}, "storeless probe", func() (state.RunJournal, error) {
				return state.RunJournal{}, nil
			}, func(state.RunJournal) bool { return true })
			if !errors.Is(err, errDaemonStoreClosing) {
				t.Fatalf("storeless v2 mutation = %v, want %v", err, errDaemonStoreClosing)
			}
		}},
	}
	runStorelessProbes(t, probes)
}
