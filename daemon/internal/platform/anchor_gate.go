//go:build linux && amd64

package platform

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

type linuxOwnerAnchorWitnessConfig struct {
	Version            int    `json:"version"`
	RunID              string `json:"run_id"`
	Generation         int64  `json:"generation"`
	DropResponse       string `json:"drop_response,omitempty"`
	HelperAction       string `json:"helper_action,omitempty"`
	HoldForReleaseFile bool   `json:"hold_for_release_file,omitempty"`
}

// LinuxOwnerAnchorGrant is the durable, run-bound input to the anchor. Secret, PipeToken,
// JobID and Fence.LeaseToken are local control material: never log them or put them in argv/environment.
type LinuxOwnerAnchorGrant struct {
	ReservationVersion int                                  `json:"reservation_version"`
	RunKey             LinuxOwnerAnchorRunKey               `json:"run_key"`
	Fence              protocol.Fence                       `json:"fence"`
	LaunchToken        string                               `json:"launch_token"`
	Secret             string                               `json:"secret"`
	PipeToken          string                               `json:"pipe_token"`
	JobID              string                               `json:"job_id"`
	OwnerUID           uint32                               `json:"owner_uid"`
	LauncherUID        uint32                               `json:"launcher_uid"`
	RecoveryEndpoint   string                               `json:"recovery_endpoint"`
	CreateStarted      bool                                 `json:"create_started"`
	Anchor             authority.LinuxCgroupProcessIdentity `json:"anchor"`
	Parent             authority.LinuxCgroupParentIdentity  `json:"parent"`
	Leaf               authority.LinuxCgroupIdentity        `json:"leaf"`
}

func readLinuxOwnerAnchorWitnessConfig(owner linuxgate.OwnerConfig) (*linuxOwnerAnchorWitnessConfig, error) {
	if owner.Validate() != nil {
		return nil, errors.New("Linux owner anchor witness config requires the fixed owner mapping")
	}
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open root for witness selector ancestry: %w", err)
	}
	defer unix.Close(rootFD)
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil || rootStat.Uid != 0 || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR || rootStat.Mode&0022 != 0 {
		return nil, fmt.Errorf("verify root witness selector ancestor: %w", errOr(err, errors.New("root ownership or mode is unsafe")))
	}
	etcFD, err := unix.Openat2(rootFD, "etc", &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if err != nil {
		return nil, fmt.Errorf("open root-controlled Linux owner config directory: %w", err)
	}
	defer unix.Close(etcFD)
	var etcStat unix.Stat_t
	if err := unix.Fstat(etcFD, &etcStat); err != nil || etcStat.Uid != 0 || etcStat.Mode&unix.S_IFMT != unix.S_IFDIR || etcStat.Mode&0022 != 0 {
		return nil, fmt.Errorf("verify root-controlled /etc directory: %w", errOr(err, ErrLinuxOwnerDirectoryOwnershipUnsafe))
	}
	symmetryFD, err := unix.Openat2(etcFD, "symmetry", &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open root-controlled /etc/symmetry directory: %w", err)
	}
	defer unix.Close(symmetryFD)
	var directoryStat unix.Stat_t
	if err := unix.Fstat(symmetryFD, &directoryStat); err != nil || directoryStat.Uid != 0 ||
		directoryStat.Mode&unix.S_IFMT != unix.S_IFDIR || directoryStat.Mode&0022 != 0 {
		return nil, fmt.Errorf("verify root-controlled /etc/symmetry directory: %w", errOr(err, ErrLinuxOwnerDirectoryOwnershipUnsafe))
	}
	witnessName := filepath.Base(linuxOwnerAnchorWitnessConfigPath)
	witnessFD, err := unix.Openat2(symmetryFD, witnessName, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open root-controlled Linux owner witness selector: %w", err)
	}
	witnessFile := os.NewFile(uintptr(witnessFD), "linux-owner-root-witness-config")
	if witnessFile == nil {
		_ = unix.Close(witnessFD)
		return nil, errors.New("wrap root-controlled Linux owner witness selector")
	}
	defer witnessFile.Close()
	var witnessStat unix.Stat_t
	if err := unix.Fstat(witnessFD, &witnessStat); err != nil || witnessStat.Uid != 0 || witnessStat.Gid != owner.AGID ||
		witnessStat.Mode&unix.S_IFMT != unix.S_IFREG || witnessStat.Mode&0777 != 0440 || witnessStat.Nlink != 1 ||
		witnessStat.Size <= 0 || witnessStat.Size > linuxOwnerAnchorWitnessConfigMaxBytes {
		return nil, fmt.Errorf("verify root-owned Linux owner witness selector: %w", errOr(err, errors.New("file ownership, mode, link count, or size is unsafe")))
	}
	contents, err := io.ReadAll(io.LimitReader(witnessFile, linuxOwnerAnchorWitnessConfigMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read root-controlled Linux owner witness selector: %w", err)
	}
	if len(contents) > linuxOwnerAnchorWitnessConfigMaxBytes {
		return nil, errors.New("root-controlled Linux owner witness selector exceeds its bound")
	}
	return parseLinuxOwnerAnchorWitnessConfig(contents)
}

func parseLinuxOwnerAnchorWitnessConfig(contents []byte) (*linuxOwnerAnchorWitnessConfig, error) {
	if len(contents) == 0 || len(contents) > linuxOwnerAnchorWitnessConfigMaxBytes {
		return nil, errors.New("root-controlled Linux owner witness selector has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var witness linuxOwnerAnchorWitnessConfig
	if err := decoder.Decode(&witness); err != nil {
		return nil, fmt.Errorf("decode root-controlled Linux owner witness selector: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("root-controlled Linux owner witness selector has trailing JSON")
	}
	if witness.Version != linuxOwnerAnchorWitnessConfigVersion || !validLinuxOwnerAnchorRunID(witness.RunID) || witness.Generation <= 0 ||
		(witness.DropResponse == "") == (witness.HelperAction == "") ||
		witness.DropResponse != "" && (witness.DropResponse != "stop" && witness.DropResponse != "ack-stop-receipt" && witness.DropResponse != "launcher-ready" || witness.HoldForReleaseFile) ||
		witness.HelperAction != "" && (witness.HelperAction != "crash_after_release_before_register" || !witness.HoldForReleaseFile) {
		return nil, errors.New("root-controlled Linux owner witness selector fields are invalid")
	}
	return &witness, nil
}

func (run *linuxOwnerAnchorRun) trackPreReleaseGatePhysicalOnlyChild(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	classification linuxOwnerAnchorRunChildClassification,
) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || run.helperExit == nil ||
		!run.helperExit.ValidFor(run.helper, run.grant.Anchor) || run.retired || run.stopRequested || run.stopLeafAttempted || run.stopFence != nil || run.gateIdentity != nil ||
		run.gateReleaseAuthorized || run.gateUserNamespaceFD != nil || run.gateImageFD != nil || run.gateReadyAuthenticated ||
		process.Validate() != nil || pidfd < 0 ||
		classification.ValidateFor(run.grant, run.helper, process) != nil ||
		classification.Origin != linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
		return errors.New("pre-RegisterGate physical-only child lacks exact helper-death and no-release evidence")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("verify physical-only gate pidfd: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("verify physical-only gate identity: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != run.grant.Anchor.PID {
		return fmt.Errorf("verify physical-only gate Anchor parent: %w", errOr(err, ErrLinuxOwnerParentPIDDiffers))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "gate", run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerRunChildMembership(process.PID, "gate", run.grant, run.leaf); err != nil {
		return err
	}
	if _, exists := run.children[process.PID]; exists {
		return errors.New("physical-only pre-release gate PID aliases a retained RunKey child")
	}
	launcherCount := 0
	for _, child := range run.children {
		if child.identity.Validate() != nil || child.role != "launcher" || child.creator.Validate() != nil || !child.creator.Equal(run.helper) || child.pidfd == nil ||
			child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
			child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
			return errors.New("physical-only pre-release gate lacks an exclusive exact root launcher producer")
		}
		ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
		if err != nil || !ready {
			return fmt.Errorf("physical-only gate root launcher remains live: %w", errOr(err, ErrLinuxOwnerLauncherPIDFDNotReady))
		}
		launcherCount++
	}
	if launcherCount != 1 {
		return errors.New("physical-only pre-release gate does not have exactly one registered root launcher")
	}
	if existing, exists := run.physicalOnlyChildren[process.PID]; exists {
		if !existing.identity.Equal(process) || !existing.classification.Equal(classification) || existing.pidfd == nil || existing.imageFD == nil {
			return errors.New("physical-only gate PID conflicts with its retained Anchor process identity")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(existing.pidfd.Fd()), process); err != nil {
			return err
		}
		return verifyLinuxOwnerAnchorGateImageFD(existing.imageFD, process, int(existing.pidfd.Fd()))
	}
	if len(run.physicalOnlyChildren) != 0 {
		return errors.New("run already has a different physical-only pre-release gate")
	}
	imageFD, err := pinLinuxOwnerAnchorGateImage(process, pidfd, run.owner)
	if err != nil {
		return fmt.Errorf("retain exact static gate image for physical-only containment: %w", err)
	}
	keepImage := false
	defer func() {
		if !keepImage {
			_ = imageFD.Close()
		}
	}()
	copyFD, err := unix.FcntlInt(uintptr(pidfd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("retain physical-only gate pidfd: %w", err)
	}
	pidfdFile := os.NewFile(uintptr(copyFD), "symmetry-owner-anchor-physical-only-gate-pidfd")
	if pidfdFile == nil {
		_ = unix.Close(copyFD)
		return errors.New("wrap physical-only gate pidfd")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(pidfdFile.Fd()), process); err != nil {
		_ = pidfdFile.Close()
		return fmt.Errorf("recheck retained physical-only gate pidfd: %w", err)
	}
	if err := verifyLinuxOwnerRunChildMembership(process.PID, "gate", run.grant, run.leaf); err != nil {
		_ = pidfdFile.Close()
		return fmt.Errorf("recheck physical-only gate membership after image pin: %w", err)
	}
	if err := verifyLinuxOwnerAnchorGateImageFD(imageFD, process, int(pidfdFile.Fd())); err != nil {
		_ = pidfdFile.Close()
		return fmt.Errorf("recheck retained physical-only gate image: %w", err)
	}
	if run.physicalOnlyChildren == nil {
		run.physicalOnlyChildren = make(map[int]linuxOwnerAnchorPhysicalOnlyChild)
	}
	run.physicalOnlyChildren[process.PID] = linuxOwnerAnchorPhysicalOnlyChild{
		identity: process, classification: classification, pidfd: pidfdFile, imageFD: imageFD,
	}
	keepImage = true
	return nil
}

func (run *linuxOwnerAnchorRun) registerGate(
	gate authority.LinuxCgroupGateIdentity,
	userNamespace LinuxUserNamespaceIdentity,
	pidfd int,
	anchorUserNamespaceFD *os.File,
) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil ||
		gate.Validate() != nil || gate.HostUID != run.owner.AUID || !gate.Cgroup.Equal(run.grant.Leaf) ||
		!userNamespace.valid() || pidfd < 0 || anchorUserNamespaceFD == nil || run.gateReleaseAuthorized || run.stopRequested {
		return errors.New("Linux owner anchor trusted A-gate registration is incomplete or too late")
	}
	if run.gateIdentity != nil && (!run.gateIdentity.Equal(gate) || run.gateUserNamespace != userNamespace) {
		return errors.New("Linux owner anchor refuses a different gate or user namespace for the same run")
	}
	gateNamespaceFD, capturedNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(gate.Process, pidfd)
	if err != nil {
		return fmt.Errorf("pin exact parked-gate user namespace: %w", err)
	}
	keepNamespace := false
	defer func() {
		if !keepNamespace {
			_ = gateNamespaceFD.Close()
		}
	}()
	if capturedNamespace != userNamespace {
		return errors.New("parked-gate user namespace differs from its helper-supplied device/inode")
	}
	gateImageFD, err := pinLinuxOwnerAnchorGateImage(gate.Process, pidfd, run.owner)
	if err != nil {
		return fmt.Errorf("pin trusted installed static gate after authenticated READY: %w", err)
	}
	keepImage := false
	defer func() {
		if !keepImage {
			_ = gateImageFD.Close()
		}
	}()
	anchorNamespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(anchorUserNamespaceFD)
	if err != nil {
		return fmt.Errorf("verify pinned Anchor user namespace: %w", err)
	}
	parentNamespace, err := linuxOwnerAnchorUserNamespaceParentIdentity(gateNamespaceFD)
	if err != nil || parentNamespace != anchorNamespace {
		return fmt.Errorf("verify gate user namespace is a direct child of the pinned Anchor namespace: %w", errOr(err, ErrLinuxOwnerParentNamespaceDiffers))
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(gate.Process.PID, userNamespace, run.owner); err != nil {
		return fmt.Errorf("verify exact parked-gate user namespace and B-only maps before registration: %w", err)
	}
	if err := run.registerChild(gate.Process, run.helper, "gate", pidfd); err != nil {
		return err
	}
	tracked, exists := run.children[gate.Process.PID]
	if !exists || !tracked.identity.Equal(gate.Process) || !tracked.creator.Equal(run.helper) || tracked.role != "gate" || tracked.pidfd == nil {
		return errors.New("Linux owner anchor gate registration lacks its exact retained pidfd")
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(gate.Process.PID, userNamespace, run.owner); err != nil {
		return fmt.Errorf("verify exact parked-gate user namespace and B-only maps: %w", err)
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), gate.Process); err != nil {
		return fmt.Errorf("recheck parked-gate pidfd after namespace verification: %w", err)
	}
	if actual, err := ProcessIdentity(gate.Process.PID); err != nil || actual != gate.Process.Identity {
		return fmt.Errorf("recheck parked-gate creation identity after namespace verification: %w", errOr(err, ErrLinuxOwnerIdentityChanged))
	}
	if actual, err := linuxOwnerAnchorUserNamespaceFDIdentity(gateNamespaceFD); err != nil || actual != userNamespace {
		return fmt.Errorf("recheck retained gate user namespace after registration: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeChanged))
	}
	if err := verifyLinuxOwnerAnchorGateImageFD(gateImageFD, gate.Process, pidfd); err != nil {
		return fmt.Errorf("recheck exact trusted static gate after registration: %w", err)
	}
	if run.gateUserNamespaceFD != nil {
		actual, err := linuxOwnerAnchorUserNamespaceFDIdentity(run.gateUserNamespaceFD)
		if err != nil || actual != userNamespace {
			return fmt.Errorf("replay gate namespace pin differs from the exact registration: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeChanged))
		}
		if run.gateImageFD == nil {
			return errors.New("replayed gate registration lost its exact image pin")
		}
		var oldImage, newImage unix.Stat_t
		if err := unix.Fstat(int(run.gateImageFD.Fd()), &oldImage); err != nil {
			return fmt.Errorf("stat retained gate image on registration replay: %w", err)
		}
		if err := unix.Fstat(int(gateImageFD.Fd()), &newImage); err != nil || oldImage.Dev != newImage.Dev || oldImage.Ino != newImage.Ino {
			return fmt.Errorf("replayed gate image differs from its exact retained pin: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeDiffers))
		}
		return nil
	}
	gateCopy := gate
	run.gateIdentity = &gateCopy
	run.gateUserNamespace = userNamespace
	run.gateUserNamespaceFD = gateNamespaceFD
	run.gateImageFD = gateImageFD
	run.gateReadyAuthenticated = true
	keepNamespace = true
	keepImage = true
	return nil
}

func (run *linuxOwnerAnchorRun) authorizeGateRelease(
	gate authority.LinuxCgroupGateIdentity,
	userNamespace LinuxUserNamespaceIdentity,
) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || run.helperExit != nil || run.stopRequested ||
		run.helperPIDFD == nil || run.gateIdentity == nil || !run.gateIdentity.Equal(gate) ||
		run.gateUserNamespace != userNamespace || !userNamespace.valid() || run.verifyPinnedGateUserNamespace() != nil {
		return errors.New("Linux owner anchor gate-release authorization differs from the registered gate")
	}
	tracked, exists := run.children[gate.Process.PID]
	if !exists || !tracked.identity.Equal(gate.Process) || !tracked.creator.Equal(run.helper) || tracked.role != "gate" || tracked.pidfd == nil ||
		tracked.classification.ValidateFor(run.grant, run.helper, gate.Process) != nil ||
		tracked.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
		return errors.New("Linux owner anchor gate-release authorization lacks its exact parked gate")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
		return fmt.Errorf("verify exact helper before gate-release authorization: %w", err)
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), gate.Process); err != nil {
		return fmt.Errorf("verify exact parked gate before release authorization: %w", err)
	}
	if actual, err := ProcessIdentity(gate.Process.PID); err != nil || actual != gate.Process.Identity {
		return fmt.Errorf("verify parked gate identity before release authorization: %w", errOr(err, ErrLinuxOwnerCreationIdentityChanged))
	}
	credentials, err := readLinuxOwnerProcessCredentials(gate.Process.PID)
	if err != nil || credentials.Parent != run.helper.PID {
		return fmt.Errorf("verify parked gate ancestry before release authorization: %w", errOr(err, errors.New("parent PID differs from exact helper")))
	}
	if err := verifyLinuxOwnerRunChildCredentials(gate.Process.PID, "gate", run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerRunChildMembership(gate.Process.PID, "gate", run.grant, run.leaf); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(gate.Process.PID, userNamespace, run.owner); err != nil {
		return fmt.Errorf("revalidate exact parked-gate user namespace before release: %w", err)
	}
	if run.gateReleaseAuthorized {
		return nil
	}
	run.gateReleaseAuthorized = true
	return nil
}

func (run *linuxOwnerAnchorRun) verifyRegisteredGateA(process authority.LinuxCgroupProcessIdentity, verifyMembership bool) error {
	if run == nil || run.grant.Validate() != nil || process.Validate() != nil || run.gateIdentity == nil ||
		!run.gateIdentity.Process.Equal(process) || !run.gateUserNamespace.valid() || run.verifyPinnedGateUserNamespace() != nil {
		return errors.New("Linux parked gate differs from its exact registered process and user namespace")
	}
	tracked, exists := run.children[process.PID]
	if !exists || !tracked.identity.Equal(process) || !tracked.creator.Equal(run.helper) || tracked.role != "gate" || tracked.pidfd == nil ||
		tracked.classification.ValidateFor(run.grant, run.helper, process) != nil ||
		tracked.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
		return errors.New("Linux parked gate lacks its exact retained A-gate pidfd")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("verify exact parked-gate pidfd during anchor reap: %w", err)
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("verify exact parked-gate creation identity during anchor reap: %w", errOr(err, ErrLinuxOwnerIdentityChanged))
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != run.grant.Anchor.PID {
		return fmt.Errorf("verify parked-gate anchor ancestry: %w", errOr(err, errors.New("parent PID differs from anchor")))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "gate", run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(process.PID, run.gateUserNamespace, run.owner); err != nil {
		return fmt.Errorf("verify parked-gate user namespace during anchor reap: %w", err)
	}
	if verifyMembership && !run.leafRemovedObserved {
		if err := verifyLinuxOwnerRunChildMembership(process.PID, "gate", run.grant, run.leaf); err != nil {
			return err
		}
	}
	if run.leafRemovedObserved {
		ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
		if err != nil || !ready {
			return fmt.Errorf("live parked gate remains after exact run-leaf removal: %w", errOr(err, ErrLinuxOwnerPIDFDNotReady))
		}
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("recheck exact parked-gate pidfd during anchor reap: %w", err)
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("recheck parked-gate creation identity during anchor reap: %w", errOr(err, ErrLinuxOwnerIdentityChanged))
	}
	return nil
}

func (run *linuxOwnerAnchorRun) verifyAuthorizedGateExec(process authority.LinuxCgroupProcessIdentity, verifyMembership bool) error {
	if run == nil || run.grant.Validate() != nil || process.Validate() != nil || run.gateIdentity == nil ||
		!run.gateReleaseAuthorized || !run.gateIdentity.Process.Equal(process) || !run.gateUserNamespace.valid() ||
		run.verifyPinnedGateUserNamespace() != nil {
		return errors.New("Linux native process lacks exact pre-registered gate and one-use release authorization")
	}
	tracked, exists := run.children[process.PID]
	if !exists || !tracked.identity.Equal(process) || !tracked.creator.Equal(run.helper) ||
		(tracked.role != "gate" && tracked.role != "native") || tracked.pidfd == nil ||
		tracked.classification.ValidateFor(run.grant, run.helper, process) != nil ||
		tracked.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
		return errors.New("Linux native process differs from its exact retained A-gate pidfd")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("verify same-PID native pidfd: %w", err)
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("verify same-PID native creation identity: %w", errOr(err, ErrLinuxOwnerIdentityChanged))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "native", run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(process.PID, run.gateUserNamespace, run.owner); err != nil {
		return fmt.Errorf("verify same-PID native user namespace and B-only maps: %w", err)
	}
	if err := verifyLinuxOwnerAnchorNativePrivilegeState(process.PID); err != nil {
		return err
	}
	if verifyMembership && !run.leafRemovedObserved {
		if err := verifyLinuxOwnerRunChildMembership(process.PID, "native", run.grant, run.leaf); err != nil {
			return err
		}
	}
	if run.leafRemovedObserved {
		ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
		if err != nil || !ready {
			return fmt.Errorf("live native process remains after exact run-leaf removal: %w", errOr(err, ErrLinuxOwnerPIDFDNotReady))
		}
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("recheck same-PID native pidfd: %w", err)
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("recheck same-PID native creation identity: %w", errOr(err, ErrLinuxOwnerIdentityChanged))
	}
	return nil
}

func (run *linuxOwnerAnchorRun) verifyPinnedGateUserNamespace() error {
	if run == nil || run.gateUserNamespaceFD == nil || run.gateImageFD == nil ||
		!run.gateReadyAuthenticated || !run.gateUserNamespace.valid() {
		return errors.New("Linux owner run lacks its authenticated GateReady, image, or user-namespace pin")
	}
	identity, err := linuxOwnerAnchorUserNamespaceFDIdentity(run.gateUserNamespaceFD)
	if err != nil || identity != run.gateUserNamespace {
		return fmt.Errorf("verify retained gate user-namespace FD: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeDiffers))
	}
	var imageStat unix.Stat_t
	if err := unix.Fstat(int(run.gateImageFD.Fd()), &imageStat); err != nil ||
		imageStat.Mode&unix.S_IFMT != unix.S_IFREG || imageStat.Uid != 0 || imageStat.Gid != 0 ||
		imageStat.Mode&0022 != 0 || imageStat.Mode&0111 == 0 || imageStat.Nlink != 1 {
		return fmt.Errorf("verify retained static gate image FD: %w", errOr(err, ErrLinuxOwnerGateImageIdentityDiffers))
	}
	return nil
}

func verifyLinuxOwnerAnchorGateNamespace(pid int, expected LinuxUserNamespaceIdentity, owner linuxgate.OwnerConfig) error {
	if pid <= 0 || !expected.valid() || owner.Validate() != nil {
		return errors.New("Linux owner anchor gate namespace identity is incomplete")
	}
	actual, err := readLinuxUserNamespaceIdentity(pid)
	if err != nil || actual != expected {
		return fmt.Errorf("Linux owner anchor gate user namespace differs from its pre-release pin: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeDiffers))
	}
	uidMap, err := readLinuxOwnerAnchorProcText(pid, "uid_map", 4096)
	if err != nil {
		return fmt.Errorf("read exact gate uid_map: %w", err)
	}
	gidMap, err := readLinuxOwnerAnchorProcText(pid, "gid_map", 4096)
	if err != nil {
		return fmt.Errorf("read exact gate gid_map: %w", err)
	}
	setgroups, err := readLinuxOwnerAnchorProcText(pid, "setgroups", 256)
	if err != nil {
		return fmt.Errorf("read exact gate setgroups policy: %w", err)
	}
	if canonicalLinuxOwnerAnchorMap(uidMap) != fmt.Sprintf("1 %d 1", owner.BUID) ||
		canonicalLinuxOwnerAnchorMap(gidMap) != fmt.Sprintf("1 %d 1", owner.BGID) || strings.TrimSpace(setgroups) != "deny" {
		return errors.New("Linux owner anchor gate user namespace is not the exact fixed-B-only map with setgroups denied")
	}
	rechecked, err := readLinuxUserNamespaceIdentity(pid)
	if err != nil || rechecked != expected {
		return fmt.Errorf("recheck Linux owner anchor gate user namespace identity: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeChanged))
	}
	return nil
}

func verifyLinuxOwnerAnchorGateImage(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	owner linuxgate.OwnerConfig,
) error {
	if process.Validate() != nil || pidfd < 0 || owner.Validate() != nil {
		return errors.New("trusted Linux gate image check is incomplete")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("verify exact gate pidfd before image readback: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("verify exact gate process before image readback: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	for _, directory := range []string{"/usr", "/usr/libexec", "/usr/libexec/symmetry"} {
		fd, err := unix.Open(directory, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("open fixed trusted gate image directory %q: %w", directory, err)
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(fd, &stat)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0022 != 0 {
			return errors.Join(fmt.Errorf("fixed gate image directory %q is not root-owned and non-writable", directory), statErr, closeErr)
		}
	}
	imageFD, err := unix.Open(linuxOwnerAnchorGateImagePath, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open fixed installed Linux gate image: %w", err)
	}
	defer unix.Close(imageFD)
	var imageStat unix.Stat_t
	if err := unix.Fstat(imageFD, &imageStat); err != nil {
		return fmt.Errorf("stat fixed installed Linux gate image: %w", err)
	}
	if imageStat.Mode&unix.S_IFMT != unix.S_IFREG || imageStat.Uid != 0 || imageStat.Gid != 0 ||
		imageStat.Mode&0022 != 0 || imageStat.Mode&0111 == 0 || imageStat.Nlink != 1 {
		return errors.New("fixed Linux gate image is not singly-linked, root-owned, executable, and immutable to A/B")
	}
	readFD, err := unix.Open(linuxOwnerAnchorGateImagePath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open fixed Linux gate image for static-ELF verification: %w", err)
	}
	readFile := os.NewFile(uintptr(readFD), "symmetry-owner-anchor-gate-image-read")
	if readFile == nil {
		_ = unix.Close(readFD)
		return errors.New("wrap fixed Linux gate image read FD")
	}
	defer readFile.Close()
	var readStat unix.Stat_t
	if err := unix.Fstat(readFD, &readStat); err != nil || readStat.Dev != imageStat.Dev || readStat.Ino != imageStat.Ino {
		return fmt.Errorf("recheck fixed Linux gate image inode for ELF verification: %w", errOr(err, errors.New("inode changed")))
	}
	image, err := elf.NewFile(readFile)
	if err != nil {
		return fmt.Errorf("parse fixed Linux gate ELF image: %w", err)
	}
	for _, program := range image.Progs {
		if program.Type == elf.PT_INTERP {
			_ = image.Close()
			return errors.New("fixed Linux gate ELF image is dynamically linked")
		}
	}
	staticAMD64 := image.Class == elf.ELFCLASS64 && image.Data == elf.ELFDATA2LSB && image.Machine == elf.EM_X86_64
	_ = image.Close()
	if !staticAMD64 {
		return errors.New("fixed Linux gate ELF image is not static Linux/amd64")
	}
	var processStat unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/exe", process.PID), &processStat); err != nil {
		return fmt.Errorf("read exact gate executable inode: %w", err)
	}
	if processStat.Mode&unix.S_IFMT != unix.S_IFREG || processStat.Dev != imageStat.Dev || processStat.Ino != imageStat.Ino {
		return errors.New("exact gate process is not executing the pinned static A gate image")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("recheck exact gate pidfd after image readback: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("recheck exact gate process after image readback: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	return nil
}

func pinLinuxOwnerAnchorGateImage(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	owner linuxgate.OwnerConfig,
) (*os.File, error) {
	if err := verifyLinuxOwnerAnchorGateImage(process, pidfd, owner); err != nil {
		return nil, err
	}
	fd, err := unix.Open(linuxOwnerAnchorGateImagePath, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("retain fixed installed Linux gate image: %w", err)
	}
	image := os.NewFile(uintptr(fd), "symmetry-owner-anchor-pinned-gate-image")
	if image == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap retained static gate image FD")
	}
	if err := verifyLinuxOwnerAnchorGateImageFD(image, process, pidfd); err != nil {
		_ = image.Close()
		return nil, err
	}
	return image, nil
}

func verifyLinuxOwnerAnchorGateImageFD(
	image *os.File,
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
) error {
	if image == nil || process.Validate() != nil || pidfd < 0 {
		return errors.New("pinned gate image/process identity is incomplete")
	}
	var imageStat unix.Stat_t
	if err := unix.Fstat(int(image.Fd()), &imageStat); err != nil {
		return fmt.Errorf("stat retained fixed gate image: %w", err)
	}
	if imageStat.Mode&unix.S_IFMT != unix.S_IFREG || imageStat.Uid != 0 || imageStat.Gid != 0 ||
		imageStat.Mode&0022 != 0 || imageStat.Mode&0111 == 0 || imageStat.Nlink != 1 {
		return errors.New("retained Linux gate image lost its root-owned non-writable identity")
	}
	var processStat unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/exe", process.PID), &processStat); err != nil {
		return fmt.Errorf("read exact gate executable against retained image pin: %w", err)
	}
	if processStat.Mode&unix.S_IFMT != unix.S_IFREG || processStat.Dev != imageStat.Dev || processStat.Ino != imageStat.Ino {
		return errors.New("exact gate process executable differs from the retained root-owned static image")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("recheck exact gate pidfd against retained image pin: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("recheck exact gate process against retained image pin: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	return nil
}

func openLinuxOwnerAnchorUserNamespaceFD(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
) (*os.File, LinuxUserNamespaceIdentity, error) {
	if process.Validate() != nil || pidfd < -1 || pidfd < 0 && process.PID != os.Getpid() {
		return nil, LinuxUserNamespaceIdentity{}, errors.New("pin exact Linux process user namespace requires a valid process identity and pidfd")
	}
	if pidfd >= 0 {
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
			return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("verify process pidfd before user-namespace pin: %w", err)
		}
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("verify process identity before user-namespace pin: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/%d/ns/user", process.PID), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("open exact process user namespace: %w", err)
	}
	namespaceFile := os.NewFile(uintptr(fd), "symmetry-owner-anchor-user-namespace")
	if namespaceFile == nil {
		_ = unix.Close(fd)
		return nil, LinuxUserNamespaceIdentity{}, errors.New("wrap exact process user namespace FD")
	}
	namespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(namespaceFile)
	if err != nil {
		_ = namespaceFile.Close()
		return nil, LinuxUserNamespaceIdentity{}, err
	}
	current, err := readLinuxUserNamespaceIdentity(process.PID)
	if err != nil || current != namespace {
		_ = namespaceFile.Close()
		return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("compare pinned nsfs inode with exact process namespace: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeDiffers))
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		_ = namespaceFile.Close()
		return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("recheck process identity after user-namespace pin: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	if pidfd >= 0 {
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
			_ = namespaceFile.Close()
			return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("recheck process pidfd after user-namespace pin: %w", err)
		}
	}
	runtime.KeepAlive(namespaceFile)
	return namespaceFile, namespace, nil
}

func linuxOwnerAnchorUserNamespaceFDIdentity(namespace *os.File) (LinuxUserNamespaceIdentity, error) {
	if namespace == nil {
		return LinuxUserNamespaceIdentity{}, errors.New("Linux user-namespace FD is unavailable")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(namespace.Fd()), &stat); err != nil {
		return LinuxUserNamespaceIdentity{}, fmt.Errorf("stat pinned Linux user-namespace FD: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return LinuxUserNamespaceIdentity{}, errors.New("pinned Linux user-namespace FD is not an nsfs inode")
	}
	namespaceType, err := unix.IoctlRetInt(int(namespace.Fd()), unix.NS_GET_NSTYPE)
	if err != nil || namespaceType != unix.CLONE_NEWUSER {
		return LinuxUserNamespaceIdentity{}, fmt.Errorf("verify pinned nsfs object is a user namespace: %w", errOr(err, errors.New("namespace type differs")))
	}
	identity := LinuxUserNamespaceIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}
	if !identity.valid() {
		return LinuxUserNamespaceIdentity{}, errors.New("pinned Linux user-namespace identity is invalid")
	}
	return identity, nil
}

func linuxOwnerAnchorUserNamespaceParentIdentity(namespace *os.File) (LinuxUserNamespaceIdentity, error) {
	if _, err := linuxOwnerAnchorUserNamespaceFDIdentity(namespace); err != nil {
		return LinuxUserNamespaceIdentity{}, err
	}
	parentFD, err := unix.IoctlRetInt(int(namespace.Fd()), unix.NS_GET_PARENT)
	if err != nil {
		return LinuxUserNamespaceIdentity{}, fmt.Errorf("read parent user namespace with NS_GET_PARENT: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), "symmetry-owner-anchor-parent-user-namespace")
	if parent == nil {
		_ = unix.Close(parentFD)
		return LinuxUserNamespaceIdentity{}, errors.New("wrap parent Linux user-namespace FD")
	}
	defer parent.Close()
	return linuxOwnerAnchorUserNamespaceFDIdentity(parent)
}

// linuxOwnerAnchorUserNamespaceRun returns the exact active RunKey whose pinned gate namespace is an
// ancestor of process; an inaccessible parent, changed PID or unstable namespace is never current-run origin.
func linuxOwnerAnchorUserNamespaceRun(
	ctx context.Context,
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	pins []linuxOwnerAnchorNamespacePin,
	anchorNamespaceFD *os.File,
) (LinuxOwnerAnchorRunKey, bool, error) {
	if ctx == nil || process.Validate() != nil || pidfd < 0 || anchorNamespaceFD == nil {
		return LinuxOwnerAnchorRunKey{}, false, errors.New("Linux run user-namespace ancestry check is incomplete")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("verify process pidfd before user-namespace ancestry check: %w", err)
	}
	anchorNamespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(anchorNamespaceFD)
	if err != nil {
		return LinuxOwnerAnchorRunKey{}, false, err
	}
	for index, pin := range pins {
		if !validLinuxOwnerAnchorRunID(pin.runKey.RunID) || pin.runKey.Generation <= 0 || pin.fd == nil || !pin.namespace.valid() {
			return LinuxOwnerAnchorRunKey{}, false, errors.New("active RunKey gate user-namespace pin is incomplete")
		}
		identity, err := linuxOwnerAnchorUserNamespaceFDIdentity(pin.fd)
		if err != nil || identity != pin.namespace || identity == anchorNamespace {
			return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("verify active RunKey gate user-namespace pin: %w", errOr(err, errors.New("identity is invalid or aliases the Anchor namespace")))
		}
		for previous := 0; previous < index; previous++ {
			if pins[previous].runKey == pin.runKey || pins[previous].namespace == pin.namespace {
				return LinuxOwnerAnchorRunKey{}, false, errors.New("active RunKey gate user-namespace pins are not distinct")
			}
		}
	}
	processNamespaceFD, processNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(process, pidfd)
	if err != nil {
		return LinuxOwnerAnchorRunKey{}, false, err
	}
	defer processNamespaceFD.Close()
	current := processNamespaceFD
	currentOwned := false
	defer func() {
		if currentOwned && current != nil {
			_ = current.Close()
		}
	}()
	var match LinuxOwnerAnchorRunKey
	found := false
	for depth := 0; depth <= linuxOwnerAnchorMaxUserNamespaceDepth; depth++ {
		if err := ctx.Err(); err != nil {
			return LinuxOwnerAnchorRunKey{}, false, err
		}
		currentIdentity, err := linuxOwnerAnchorUserNamespaceFDIdentity(current)
		if err != nil {
			return LinuxOwnerAnchorRunKey{}, false, err
		}
		if currentIdentity == anchorNamespace {
			break
		}
		for _, pin := range pins {
			if currentIdentity == pin.namespace {
				match = pin.runKey
				found = true
				break
			}
		}
		if found {
			break
		}
		if depth == linuxOwnerAnchorMaxUserNamespaceDepth {
			return LinuxOwnerAnchorRunKey{}, false, errors.New("user-namespace ancestry exceeds the kernel's bounded nesting depth")
		}
		parentFD, err := unix.IoctlRetInt(int(current.Fd()), unix.NS_GET_PARENT)
		if err != nil {
			return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("walk bounded NS_GET_PARENT chain: %w", err)
		}
		parent := os.NewFile(uintptr(parentFD), "symmetry-owner-anchor-user-namespace-parent")
		if parent == nil {
			_ = unix.Close(parentFD)
			return LinuxOwnerAnchorRunKey{}, false, errors.New("wrap parent user-namespace ancestry FD")
		}
		parentIdentity, err := linuxOwnerAnchorUserNamespaceFDIdentity(parent)
		if err != nil || parentIdentity == currentIdentity {
			_ = parent.Close()
			return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("verify parent user-namespace ancestry FD: %w", errOr(err, errors.New("namespace ancestry cycle")))
		}
		if currentOwned {
			_ = current.Close()
		}
		current = parent
		currentOwned = true
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("recheck process identity after user-namespace ancestry: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	currentProcessNamespace, err := readLinuxUserNamespaceIdentity(process.PID)
	if err != nil || currentProcessNamespace != processNamespace {
		return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("recheck process user namespace after ancestry walk: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeChanged))
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("recheck process pidfd after user-namespace ancestry: %w", err)
	}
	return match, found, nil
}

func (anchor *linuxOwnerAnchor) activeGateNamespacePins() ([]linuxOwnerAnchorNamespacePin, error) {
	if anchor == nil || anchor.userNamespaceFD == nil || !anchor.userNamespace.valid() {
		return nil, errors.New("Anchor host user namespace pin is unavailable")
	}
	identity, err := linuxOwnerAnchorUserNamespaceFDIdentity(anchor.userNamespaceFD)
	if err != nil || identity != anchor.userNamespace {
		return nil, fmt.Errorf("verify exact Anchor user namespace pin: %w", errOr(err, ErrLinuxOwnerDeviceOrInodeDiffers))
	}
	pins := make([]linuxOwnerAnchorNamespacePin, 0, len(anchor.runs))
	for key, run := range anchor.runs {
		if run == nil || run.retired {
			continue
		}
		if run.grant.Validate() != nil || run.grant.RunKey != key {
			return nil, errors.New("Anchor has an invalid active RunKey while building namespace origin pins")
		}
		if run.gateIdentity == nil {
			if run.gateUserNamespaceFD != nil {
				return nil, errors.New("Anchor run has a user-namespace FD without an exact gate identity")
			}
			continue
		}
		if err := run.verifyPinnedGateUserNamespace(); err != nil {
			return nil, err
		}
		parentNamespace, err := linuxOwnerAnchorUserNamespaceParentIdentity(run.gateUserNamespaceFD)
		if err != nil || parentNamespace != anchor.userNamespace {
			return nil, fmt.Errorf("verify active gate user namespace parent: %w", errOr(err, errors.New("parent namespace differs from Anchor")))
		}
		pins = append(pins, linuxOwnerAnchorNamespacePin{runKey: key, namespace: run.gateUserNamespace, fd: run.gateUserNamespaceFD})
	}
	sort.Slice(pins, func(left, right int) bool {
		if pins[left].runKey.RunID == pins[right].runKey.RunID {
			return pins[left].runKey.Generation < pins[right].runKey.Generation
		}
		return pins[left].runKey.RunID < pins[right].runKey.RunID
	})
	return pins, nil
}

func (anchor *linuxOwnerAnchor) classifyUnregisteredPreReleaseGate(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	cgroupPath string,
) (linuxOwnerAnchorRunChildClassification, bool, error) {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil ||
		run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) ||
		run.retired || run.gateIdentity != nil || run.gateUserNamespaceFD != nil || run.gateImageFD != nil ||
		run.gateReadyAuthenticated || run.gateReleaseAuthorized || run.stopRequested || run.stopLeafAttempted || run.stopFence != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, err
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify possible pre-release gate pidfd: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify possible pre-release gate identity: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != anchor.identity.PID {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify possible pre-release gate Anchor parent: %w", errOr(err, ErrLinuxOwnerParentPIDDiffers))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "gate", run.owner); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	launcherCount := 0
	for _, child := range run.children {
		if child.role != "launcher" {
			// A physical-only gate is accepted only in the narrow fork gap after the run's sole
			// registered root launcher and before any gate or native identity was installed.
			return linuxOwnerAnchorRunChildClassification{}, false, nil
		}
		if child.identity.Validate() != nil || child.creator.Validate() != nil || !child.creator.Equal(run.helper) ||
			child.pidfd == nil || child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
			child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
			return linuxOwnerAnchorRunChildClassification{}, false, errors.New("pre-release gate source lacks the exact registered root launcher")
		}
		// RegisterProcess already bound this retained PIDFD to the exact launcher start identity.
		// The helper may waitid-reap it first, so fdinfo stops exposing the numeric PID: never reopen it.
		ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
		if err != nil || !ready {
			return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify root launcher exited before unregistered gate containment: %w", errOr(err, ErrLinuxOwnerLauncherPIDFDNotReady))
		}
		launcherCount++
	}
	if launcherCount != 1 {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	gateNamespaceFD, gateNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(process, pidfd)
	if err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("pin possible pre-release gate user namespace: %w", err)
	}
	defer gateNamespaceFD.Close()
	if gateNamespace == anchor.userNamespace {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	parentNamespace, err := linuxOwnerAnchorUserNamespaceParentIdentity(gateNamespaceFD)
	if err != nil || parentNamespace != anchor.userNamespace {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify possible pre-release gate namespace parent: %w", errOr(err, errors.New("namespace is not an Anchor child")))
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, err
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("recheck possible pre-release gate identity: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	actualCgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil || actualCgroupPath != cgroupPath {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("recheck possible pre-release gate leaf membership: %w", errOr(err, ErrLinuxOwnerCgroupPathChanged))
	}
	classification := anchor.makeLinuxOwnerAnchorChildClassification(
		run, process, LinuxOwnerAnchorRunKey{}, "pre_release_gate", linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly,
	)
	if err := classification.ValidateFor(run.grant, run.helper, process); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, err
	}
	// AuthorizeGateRelease rejects until RegisterGate pins GateReady, so an exact helper exit with
	// neither state proves RELEASE was unreachable; keep that proof's image inode pinned.
	if err := run.trackPreReleaseGatePhysicalOnlyChild(process, pidfd, classification); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("retain exact pre-release gate origin proof: %w", err)
	}
	return classification, true, nil
}
