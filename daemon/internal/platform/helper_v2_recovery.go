//go:build linux && amd64

package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func quarantineLinuxOwnerAnchorRun(grant LinuxOwnerAnchorGrant, owner linuxgate.OwnerConfig, helper authority.LinuxCgroupProcessIdentity, pidfd *os.File, leaf *LinuxCgroupV2Leaf) *linuxOwnerAnchorRun {
	leafCopy, err := duplicateLinuxOwnerAnchorLeaf(leaf)
	if err != nil {
		return nil
	}
	var pidfdCopy *os.File
	if pidfd != nil {
		fd, duplicateErr := unix.FcntlInt(pidfd.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if duplicateErr != nil {
			_ = leafCopy.Close()
			return nil
		}
		pidfdCopy = os.NewFile(uintptr(fd), "symmetry-owner-quarantined-helper-pidfd")
		if pidfdCopy == nil {
			_ = unix.Close(fd)
			_ = leafCopy.Close()
			return nil
		}
	}
	return &linuxOwnerAnchorRun{
		grant: grant, owner: owner, helper: helper, helperPIDFD: pidfdCopy, leaf: leafCopy,
		children: make(map[int]linuxOwnerAnchorTrackedProcess), reaped: make(map[int]LinuxOwnerAnchorReapEvidence),
	}
}

func (helper *linuxOwnerHelperV2) beginFinalization(request linuxOwnerHelperV2Request) (*linuxOwnerHelperV2Finalization, bool, bool, error) {
	if helper == nil {
		return nil, false, false, errors.New("Linux owner helper finalizer is unavailable")
	}
	if request.Operation != "release-run" || request.Helper != nil || request.Launcher != nil || request.Gate != nil ||
		request.LauncherExit != nil || request.Sequence != 0 || request.LeaseNanos != 0 || request.Accepted ||
		request.Command != nil || request.CheckoutReceipt != nil || request.Receipt != nil || request.PreGateReceipt != nil {
		return nil, false, false, errors.New("Linux owner helper finalization request carries unexpected fields")
	}
	var record linuxOwnerHelperV2Finalization
	switch {
	case request.AbortProof != nil && request.ReleaseProof == nil && request.PreGateReleaseProof == nil:
		if request.AbortProof.Validate() != nil {
			return nil, false, false, errors.New("Linux owner helper abort finalization proof is invalid")
		}
		record.kind = "abort"
		proof := request.AbortProof.Clone()
		record.abortProof = &proof
	case request.PreGateReleaseProof != nil && request.ReleaseProof == nil && request.AbortProof == nil:
		if request.PreGateReleaseProof.Validate() != nil {
			return nil, false, false, errors.New("Linux owner helper pre-gate finalization proof is invalid")
		}
		record.kind = "pre-gate"
		proof := request.PreGateReleaseProof.Clone()
		record.preGateProof = &proof
	case request.ReleaseProof != nil && request.PreGateReleaseProof == nil && request.AbortProof == nil:
		if request.ReleaseProof.Validate() != nil {
			return nil, false, false, errors.New("Linux owner helper normal finalization proof is invalid")
		}
		record.kind = "normal"
		proof := request.ReleaseProof.Clone()
		record.releaseProof = &proof
	default:
		return nil, false, false, errors.New("Linux owner helper finalization requires exactly one durable proof")
	}
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	if helper.finalized {
		if helper.finalization != nil && helper.finalization.matches(request) && helper.finalization.err == nil {
			return helper.finalization, false, true, nil
		}
		return nil, false, false, errors.New("Linux owner helper was finalized with a different proof")
	}
	if helper.finalizing {
		if helper.finalization == nil || !helper.finalization.matches(request) {
			return nil, false, false, errors.New("Linux owner helper is finalizing a different proof")
		}
		return helper.finalization, false, false, nil
	}
	record.done = make(chan struct{})
	helper.finalization = &record
	helper.finalizing = true
	return helper.finalization, true, false, nil
}

func (helper *linuxOwnerHelperV2) finishFinalization(record *linuxOwnerHelperV2Finalization, err error, retryable bool) {
	if helper == nil || record == nil {
		return
	}
	helper.stateMu.Lock()
	if helper.finalization != record || record.completed {
		helper.stateMu.Unlock()
		return
	}
	record.err = err
	record.completed = true
	takeover := false
	if err == nil {
		helper.finalized = true
		helper.finalizing = false
	} else if retryable {
		helper.finalization = nil
		helper.finalizing = false
		takeover = helper.ownerLost
	} else {
		takeover = helper.ownerLost
	}
	close(record.done)
	helper.stateMu.Unlock()
	if takeover {
		helper.requestAnchorTakeover()
	}
}

func waitLinuxOwnerHelperV2Finalization(record *linuxOwnerHelperV2Finalization) error {
	if record == nil || record.done == nil {
		return errors.New("Linux owner helper finalization wait is incomplete")
	}
	timer := time.NewTimer(linuxOwnerHelperV2Timeout)
	defer timer.Stop()
	select {
	case <-record.done:
		return record.err
	case <-timer.C:
		return errors.New("Linux owner helper finalization remained in progress")
	}
}

func (helper *linuxOwnerHelperV2) watchOwnerPipe(ctx context.Context) {
	fd := int(helper.ownerPipe.Fd())
	for ctx.Err() == nil {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		_, err := unix.Poll(poll, int(linuxOwnerHelperV2PollInterval/time.Millisecond))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			if stopErr := helper.markOwnerLost(); stopErr != nil {
				helper.requestAnchorTakeover()
			}
			return
		}
		if poll[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) == 0 {
			continue
		}
		var byteValue [1]byte
		n, readErr := unix.Read(fd, byteValue[:])
		if readErr == nil && n == 0 || readErr != nil && !errors.Is(readErr, unix.EINTR) && !errors.Is(readErr, unix.EAGAIN) {
			if stopErr := helper.markOwnerLost(); stopErr != nil {
				helper.requestAnchorTakeover()
			}
			return
		}
		if n != 0 {
			if stopErr := helper.markOwnerLost(); stopErr != nil {
				helper.requestAnchorTakeover()
			}
			return
		}
	}
}

func (helper *linuxOwnerHelperV2) watchLease(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-helper.lease.wake:
			if stopErr := helper.markOwnerLost(); stopErr != nil {
				helper.requestAnchorTakeover()
			}
		}
	}
}

func (helper *linuxOwnerHelperV2) markOwnerLost() error {
	if helper == nil {
		return nil
	}
	helper.stateMu.Lock()
	if helper.finalized || helper.ownerLost {
		helper.stateMu.Unlock()
		return nil
	}
	helper.ownerLost = true
	if helper.finalizing {
		takeover := helper.finalization != nil && helper.finalization.completed && helper.finalization.err != nil
		connections := make([]*net.UnixConn, 0, len(helper.activeConns))
		for connection := range helper.activeConns {
			connections = append(connections, connection)
		}
		helper.stateMu.Unlock()
		helper.lease.stop()
		for _, connection := range connections {
			_ = connection.Close()
		}
		if takeover {
			helper.requestAnchorTakeover()
		}
		return nil
	}
	if helper.prepareCancel != nil {
		helper.prepareCancel()
	}
	if helper.releaseCancel != nil {
		helper.releaseCancel()
	}
	if helper.prepared != nil && !helper.released {
		_ = helper.prepared.Abort()
	}
	connections := make([]*net.UnixConn, 0, len(helper.activeConns))
	for connection := range helper.activeConns {
		connections = append(connections, connection)
	}
	helper.stateMu.Unlock()
	helper.lease.stop()
	for _, connection := range connections {
		_ = connection.Close()
	}
	stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerHelperV2StopTimeout)
	_, err := helper.stopOwned(stopContext)
	cancel()
	return err
}

func (helper *linuxOwnerHelperV2) requestAnchorTakeover() {
	if helper == nil {
		return
	}
	helper.stateMu.Lock()
	if helper.anchorTakeoverRequested {
		helper.stateMu.Unlock()
		return
	}
	helper.anchorTakeoverRequested = true
	connections := make([]*net.UnixConn, 0, len(helper.activeConns))
	for connection := range helper.activeConns {
		connections = append(connections, connection)
	}
	listener := helper.listener
	helper.stateMu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	for _, connection := range connections {
		_ = connection.Close()
	}
	// An unresolved helper cannot keep detached B alive by staying resident;
	// its anchor owns the independent pidfd watcher and pinned-leaf fallback.
	if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
		os.Exit(1)
	}
}

func (helper *linuxOwnerHelperV2) isFinalized() bool {
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	return helper.finalized
}

func (helper *linuxOwnerHelperV2) isWitnessExitRequested() bool {
	if helper == nil {
		return false
	}
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	return helper.witnessExitRequested
}

func (helper *linuxOwnerHelperV2) witnessExitError() error {
	if helper == nil {
		return errLinuxOwnerHelperV2WitnessExit
	}
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	return errors.Join(errLinuxOwnerHelperV2WitnessExit, helper.witnessExitCause)
}

func verifyLinuxCloneReuseTarget(grant authority.LinuxCloneReuseGrant, owner linuxgate.OwnerConfig) error {
	if err := grant.Validate(); err != nil {
		return err
	}
	target := grant.SourceCheckout.Target
	if !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return errors.New("Linux clone reuse target is not canonical")
	}
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("stat Linux clone reuse target: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Linux clone reuse target is not a real directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Dev) != grant.SourceCheckout.TargetDevice ||
		uint64(stat.Ino) != grant.SourceCheckout.TargetInode ||
		stat.Uid != uint32(owner.BUID) || stat.Gid != uint32(owner.BGID) {
		return errors.New("Linux clone reuse target identity differs from its receipt or B owner")
	}
	return nil
}

func (helper *linuxOwnerHelperV2) waitForWitnessSelectorUnlink() error {
	if helper == nil {
		return errors.New("Linux owner helper witness selector wait is incomplete")
	}
	helper.stateMu.Lock()
	valid := helper.witnessActionObserved && helper.witnessAction == linuxOwnerHelperV2WitnessCrashAfterRelease &&
		helper.witnessRunKey == helper.grant.RunKey && helper.witnessHoldForReleaseFile && helper.released
	helper.stateMu.Unlock()
	if !valid {
		return errors.New("Linux owner helper witness selector wait lacks the exact successful RELEASE action")
	}
	directoryFD, err := openLinuxOwnerHelperWitnessSelectorDirectory()
	if err != nil {
		return fmt.Errorf("open pinned root witness selector directory: %w", err)
	}
	defer unix.Close(directoryFD)
	timer := time.NewTimer(linuxOwnerHelperV2Timeout)
	defer timer.Stop()
	ticker := time.NewTicker(linuxOwnerHelperV2PollInterval)
	defer ticker.Stop()
	selectorName := filepath.Base(linuxOwnerAnchorWitnessConfigPath)
	for {
		var selectorStat unix.Stat_t
		err := unix.Fstatat(directoryFD, selectorName, &selectorStat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect pinned root witness selector without following links: %w", err)
		}
		helper.stateMu.Lock()
		ownerLost := helper.ownerLost
		helper.stateMu.Unlock()
		if ownerLost {
			return ErrLinuxOwnerHelperV2OwnerLost
		}
		if _, _, leaseLive := helper.lease.releaseGuard(); !leaseLive {
			return ErrLinuxOwnerHelperV2LeaseUnarmed
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return errors.New("root did not unlink the selected Linux owner witness selector before the bounded wait expired")
		}
	}
}

func openLinuxOwnerHelperWitnessSelectorDirectory() (int, error) {
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open filesystem root: %w", err)
	}
	defer unix.Close(rootFD)
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil || rootStat.Uid != 0 || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR || rootStat.Mode&0022 != 0 {
		return -1, fmt.Errorf("verify filesystem root ownership and mode: %w", errOr(err, errors.New("root is not root-owned and non-writable by group/world")))
	}
	etcFD, err := unix.Openat2(rootFD, "etc", &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if err != nil {
		return -1, fmt.Errorf("open root-controlled /etc directory: %w", err)
	}
	defer unix.Close(etcFD)
	var etcStat unix.Stat_t
	if err := unix.Fstat(etcFD, &etcStat); err != nil || etcStat.Uid != 0 || etcStat.Mode&unix.S_IFMT != unix.S_IFDIR || etcStat.Mode&0022 != 0 {
		return -1, fmt.Errorf("verify /etc ownership and mode: %w", errOr(err, errors.New("/etc is not root-owned and non-writable by group/world")))
	}
	directoryFD, err := unix.Openat2(etcFD, "symmetry", &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if err != nil {
		return -1, fmt.Errorf("open root-controlled /etc/symmetry directory: %w", err)
	}
	var directoryStat unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryStat); err != nil || directoryStat.Uid != 0 ||
		directoryStat.Mode&unix.S_IFMT != unix.S_IFDIR || directoryStat.Mode&0022 != 0 {
		_ = unix.Close(directoryFD)
		return -1, fmt.Errorf("verify /etc/symmetry ownership and mode: %w", errOr(err, errors.New("directory is not root-owned and non-writable by group/world")))
	}
	return directoryFD, nil
}

func (helper *linuxOwnerHelperV2) exitAtWitnessAction(cause error) error {
	if helper == nil {
		return errors.Join(errLinuxOwnerHelperV2WitnessExit, cause)
	}
	helper.stateMu.Lock()
	valid := helper.witnessActionObserved && helper.witnessAction == linuxOwnerHelperV2WitnessCrashAfterRelease &&
		helper.witnessRunKey == helper.grant.RunKey && helper.released
	if valid {
		helper.witnessExitRequested = true
		helper.witnessExitCause = cause
	}
	helper.stateMu.Unlock()
	if !valid {
		return errors.Join(cause, errors.New("Linux owner helper witness exit lacks the exact observed action"))
	}
	if helper.listener == nil {
		helper.stateMu.Lock()
		helper.witnessExitCause = errors.Join(helper.witnessExitCause, errors.New("Linux owner helper witness exit has no control listener"))
		helper.stateMu.Unlock()
		return helper.witnessExitError()
	}
	if err := helper.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		helper.stateMu.Lock()
		helper.witnessExitCause = errors.Join(helper.witnessExitCause, fmt.Errorf("close helper listener at witness exit: %w", err))
		helper.stateMu.Unlock()
		helper.requestAnchorTakeover()
		return helper.witnessExitError()
	}
	return helper.witnessExitError()
}

func (helper *linuxOwnerHelperV2) relayCheckoutReady(
	ctx context.Context,
	connection *net.UnixConn,
	initial linuxOwnerHelperV2Request,
	cloneGrant authority.LinuxCloneGrant,
	receipt authority.LinuxCloneCheckoutReceipt,
) error {
	if helper == nil || ctx == nil || connection == nil || !linuxOwnerHelperV2CheckoutReceiptMatches(receipt, cloneGrant) {
		return errors.New("Linux owner helper checkout-ready relay is incomplete")
	}
	deadline := time.Now().Add(linuxOwnerHelperV2Timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set checkout-ready callback deadline: %w", err)
	}
	response := helper.baseResponse("checkout-ready", "ready")
	copyReceipt := receipt.Clone()
	response.CheckoutReceipt = &copyReceipt
	helper.stateMu.Lock()
	response.OwnerLost = helper.ownerLost
	response.CommitAcknowledged = helper.commitAcknowledged
	response.ReleaseAttempted = helper.releaseAttempted
	response.Released = helper.released
	helper.stateMu.Unlock()
	if err := writeLinuxOwnerHelperV2Response(connection, response); err != nil {
		return fmt.Errorf("send exact B checkout receipt to durable owner: %w", err)
	}
	request, rights, err := readLinuxOwnerHelperV2Request(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return fmt.Errorf("read checkout-ready acknowledgement: %w", err)
	}
	peer, err := linuxOwnerHelperPeerFromConn(connection, helper.owner)
	if err != nil || peer.PID != initial.ClientPID || peer.Identity != initial.ClientIdentity ||
		!helper.requestMatchesPeer(request, peer) || request.Operation != "checkout-ready-ack" || len(rights) != 0 ||
		!request.Accepted || request.CheckoutReceipt == nil || !request.CheckoutReceipt.Equal(receipt) ||
		request.Helper != nil || request.Launcher != nil || request.Gate != nil || request.LauncherExit != nil ||
		request.Sequence != 0 || request.LeaseNanos != 0 || request.Command != nil || request.Receipt != nil ||
		request.PreGateReceipt != nil || request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil {
		return errors.Join(errors.New("checkout-ready acknowledgement is absent, rejected, or mismatched"), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	helper.stateMu.Lock()
	ownerLost := helper.ownerLost || helper.stopRequested || helper.stopAttempted
	_, _, leaseLive := helper.lease.releaseGuard()
	helper.stateMu.Unlock()
	if ownerLost || !leaseLive {
		return errors.New("owner loss or lease expiry fenced the clone checkout acknowledgement")
	}
	return nil
}

func (anchor *linuxOwnerAnchor) handleRetireHelper(connection *net.UnixConn, request linuxOwnerAnchorRequest, rights []int) {
	if len(rights) != 0 || request.Helper == nil || request.Creator != nil || request.Process != nil || request.Role != "" ||
		(request.ReleaseProof == nil) == (request.PreGateReleaseProof == nil) {
		return
	}
	run, exists := anchor.runs[request.Grant.RunKey]
	if !exists || run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
		anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
		return
	}
	if run.retired {
		if request.ReleaseProof != nil && run.retiredRelease != nil && run.retiredRelease.Equal(*request.ReleaseProof) ||
			request.PreGateReleaseProof != nil && run.retiredPreGateRelease != nil && run.retiredPreGateRelease.Equal(*request.PreGateReleaseProof) {
			anchor.writeResponse(connection, request, "already_retired", &run.helper, nil)
		} else {
			anchor.writeResponse(connection, request, "retirement_conflict", &run.helper, nil)
		}
		return
	}
	if request.PreGateReleaseProof != nil {
		proof := request.PreGateReleaseProof
		launcher := linuxOwnerAnchorTrackedIdentity(run.children, "launcher")
		if proof.Validate() != nil || proof.Receipt.LaunchToken != run.grant.LaunchToken ||
			!proof.Receipt.CgroupIdentity.Equal(run.grant.Leaf) || !proof.Receipt.Anchor.Equal(run.grant.Anchor) ||
			!proof.Receipt.Helper.Equal(run.helper) || launcher.Validate() != nil ||
			!proof.Receipt.Launcher.Equal(launcher) || !proof.Receipt.Proof.LauncherExit.Process.Equal(launcher) ||
			!proof.Receipt.Proof.LauncherExit.ValidForOwnedDescendant(launcher, run.helper, run.grant.Anchor, nil) {
			anchor.writeResponse(connection, request, "release_proof_mismatch", nil, nil)
			return
		}
	} else {
		launcher := linuxOwnerAnchorTrackedIdentity(run.children, "launcher")
		target := linuxOwnerAnchorTrackedIdentity(run.children, "native")
		if target.Validate() != nil {
			target = linuxOwnerAnchorTrackedIdentity(run.children, "gate")
		}
		if request.ReleaseProof.LauncherExit == nil || !linuxOwnerHelperV2ReleaseProofMatches(*request.ReleaseProof, run.grant, run.helper,
			target, launcher, *request.ReleaseProof.LauncherExit) {
			anchor.writeResponse(connection, request, "release_proof_mismatch", nil, nil)
			return
		}
	}
	present, leafErr := linuxOwnerAnchorPinnedLeafPresent(run)
	if leafErr != nil || present {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	run.leafRemovedObserved = true
	run.leafEmptyObserved = true
	retireContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	defer cancel()
	if run.helperExit == nil {
		if err := run.waitHelper(retireContext); err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", &run.helper, nil)
			return
		}
	}
	if err := anchor.verifyRetiredRunPhysicalState(retireContext, run); err != nil {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	if err := run.close(); err != nil {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	run.retired = true
	if request.ReleaseProof != nil {
		proof := request.ReleaseProof.Clone()
		run.retiredRelease = &proof
	} else {
		proof := request.PreGateReleaseProof.Clone()
		run.retiredPreGateRelease = &proof
	}
	anchor.writeResponse(connection, request, "retired", nil, nil)
}

func (anchor *linuxOwnerAnchor) handleRetireClearedRun(connection *net.UnixConn, request linuxOwnerAnchorRequest) {
	run, exists := anchor.runs[request.RunKey]
	if !exists || run == nil || run.grant.Fence != request.Fence || !run.helper.Equal(*request.Helper) {
		return
	}
	request.Grant = run.grant
	if run.retired {
		exact := request.ReleaseProof != nil && run.retiredRelease != nil && run.retiredRelease.Equal(*request.ReleaseProof) ||
			request.PreGateReleaseProof != nil && run.retiredPreGateRelease != nil && run.retiredPreGateRelease.Equal(*request.PreGateReleaseProof)
		if exact {
			anchor.writeResponse(connection, request, "already_retired", &run.helper, nil)
		} else {
			anchor.writeResponse(connection, request, "retirement_conflict", &run.helper, nil)
		}
		return
	}
	if request.ReleaseProof != nil {
		launcher := linuxOwnerAnchorTrackedIdentity(run.children, "launcher")
		target := linuxOwnerAnchorTrackedIdentity(run.children, "native")
		if target.Validate() != nil {
			target = linuxOwnerAnchorTrackedIdentity(run.children, "gate")
		}
		if request.ReleaseProof.LauncherExit == nil || !linuxOwnerHelperV2ReleaseProofMatches(*request.ReleaseProof,
			run.grant, run.helper, target, launcher, *request.ReleaseProof.LauncherExit) {
			anchor.writeResponse(connection, request, "release_proof_mismatch", &run.helper, nil)
			return
		}
	} else {
		proof := request.PreGateReleaseProof
		launcher := linuxOwnerAnchorTrackedIdentity(run.children, "launcher")
		if proof == nil || proof.Validate() != nil || proof.Receipt.LaunchToken != run.grant.LaunchToken ||
			!proof.Receipt.CgroupIdentity.Equal(run.grant.Leaf) || !proof.Receipt.Anchor.Equal(run.grant.Anchor) ||
			!proof.Receipt.Helper.Equal(run.helper) || launcher.Validate() != nil ||
			!proof.Receipt.Launcher.Equal(launcher) || !proof.Receipt.Proof.LauncherExit.Process.Equal(launcher) ||
			!proof.Receipt.Proof.LauncherExit.ValidForOwnedDescendant(launcher, run.helper, run.grant.Anchor, nil) {
			anchor.writeResponse(connection, request, "release_proof_mismatch", &run.helper, nil)
			return
		}
	}
	present, leafErr := linuxOwnerAnchorPinnedLeafPresent(run)
	if leafErr != nil || present {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	run.leafRemovedObserved = true
	run.leafEmptyObserved = true
	retireContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	defer cancel()
	if run.helperExit == nil {
		if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), run.helper); err == nil {
			anchorClient := &LinuxOwnerAnchorClient{socketPath: anchor.socketPath, owner: anchor.owner}
			control, dialErr := dialLinuxOwnerHelperV2(retireContext, anchorClient, run.grant, run.helper)
			if dialErr != nil {
				anchor.writeResponse(connection, request, "helper_release_unresolved", &run.helper, nil)
				return
			}
			if request.ReleaseProof != nil {
				dialErr = control.ReleaseAfterClear(retireContext, *request.ReleaseProof)
			} else {
				dialErr = control.ReleaseAfterPreGateClear(retireContext, *request.PreGateReleaseProof)
			}
			_ = control.Close()
			if dialErr != nil {
				anchor.writeResponse(connection, request, "helper_release_unresolved", &run.helper, nil)
				return
			}
		}
		if err := run.waitHelper(retireContext); err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", &run.helper, nil)
			return
		}
	}
	if err := anchor.verifyRetiredRunPhysicalState(retireContext, run); err != nil {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	if err := run.close(); err != nil {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	run.retired = true
	if request.ReleaseProof != nil {
		proof := request.ReleaseProof.Clone()
		run.retiredRelease = &proof
	} else {
		proof := request.PreGateReleaseProof.Clone()
		run.retiredPreGateRelease = &proof
	}
	anchor.writeResponse(connection, request, "retired", &run.helper, nil)
}

func (anchor *linuxOwnerAnchor) handleRetireAbortedHelper(connection *net.UnixConn, request linuxOwnerAnchorRequest) {
	run, exists := anchor.runs[request.RunKey]
	if !exists || run == nil || run.grant.Fence != request.Fence || !run.helper.Equal(*request.Helper) {
		return
	}
	request.Grant = run.grant
	proof := request.AbortProof
	if run.retired {
		if proof != nil && run.retiredAbort != nil && run.retiredAbort.Equal(*proof) {
			anchor.writeResponse(connection, request, "already_retired", &run.helper, nil)
		} else {
			anchor.writeResponse(connection, request, "retirement_conflict", &run.helper, nil)
		}
		return
	}
	helperIdentity := run.helper
	helperPointer := &helperIdentity
	if proof == nil || proof.Validate() != nil || proof.LaunchToken != run.grant.LaunchToken ||
		!proof.Parent.Equal(run.grant.Parent) || proof.LeafName != run.grant.Leaf.LeafName ||
		!proof.CreateStarted || proof.CreateStarted != run.grant.CreateStarted || proof.LeafIdentity == nil ||
		!proof.LeafIdentity.Equal(run.grant.Leaf) ||
		(proof.LeafDisposition != authority.LinuxCgroupAbortLeafRemovedEmpty &&
			proof.LeafDisposition != authority.LinuxCgroupAbortLeafAbsentAfterRmdir) || proof.OwnerUID != run.grant.OwnerUID ||
		!proof.Anchor.Equal(run.grant.Anchor) || !proof.ValidFor(run.grant.LaunchToken, run.grant.Parent,
		run.grant.Leaf.LeafName, run.grant.CreateStarted, run.grant.OwnerUID, run.grant.Anchor, proof.Mirror, helperPointer, nil) ||
		proof.Launcher != nil || proof.Helper == nil || !proof.Helper.Equal(run.helper) || proof.HelperExit == nil ||
		!proof.HelperExit.ValidFor(run.helper, run.grant.Anchor) {
		anchor.writeResponse(connection, request, "abort_proof_mismatch", &run.helper, nil)
		return
	}
	for _, child := range run.children {
		if child.role == "launcher" || child.role == "gate" || child.role == "native" {
			anchor.writeResponse(connection, request, "abort_has_producer", &run.helper, nil)
			return
		}
	}
	waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	defer cancel()
	if run.helperExit == nil {
		if err := run.waitHelper(waitContext); err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", &run.helper, nil)
			return
		}
	}
	if !run.helperExit.Equal(*proof.HelperExit) {
		anchor.writeResponse(connection, request, "helper_exit_mismatch", &run.helper, nil)
		return
	}
	if err := anchor.verifyRetiredRunPhysicalState(waitContext, run); err != nil {
		anchor.writeResponse(connection, request, "abort_unproven", &run.helper, nil)
		return
	}
	if err := run.close(); err != nil {
		anchor.writeResponse(connection, request, "abort_unproven", &run.helper, nil)
		return
	}
	run.retired = true
	proofCopy := proof.Clone()
	run.retiredAbort = &proofCopy
	anchor.writeResponse(connection, request, "retired", &run.helper, nil)
}

func (anchor *linuxOwnerAnchor) verifyRetiredRunPhysicalState(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil || run.leaf == nil ||
		!run.helperExit.ValidFor(run.helper, anchor.identity) {
		return errors.New("anchor retirement lacks exact helper exit/reap and pinned leaf authority")
	}
	if run.stopEvidence != nil {
		if err := verifyLinuxOwnerAnchorStopEvidenceNamespacePin(*run.stopEvidence, run.gateUserNamespaceFD); err != nil {
			return fmt.Errorf("verify cached AnchorStop namespace pin before retirement: %w", err)
		}
	}
	present, err := linuxOwnerAnchorPinnedLeafPresent(run)
	if err != nil {
		return fmt.Errorf("verify exact pinned leaf before retirement: %w", err)
	}
	if present {
		return errors.New("exact pinned run leaf remains present despite the release or abort proof")
	}
	run.leafRemovedObserved = true
	run.leafEmptyObserved = true
	if _, err := anchor.reapRunChildren(ctx, run); err != nil {
		return fmt.Errorf("reap every exact anchor-adopted run child before retirement: %w", err)
	}
	if len(run.physicalOnlyChildren) != 0 {
		return errors.New("physical-only pre-RegisterGate gate remains outside RunReapEvidence; run cannot be retired or cleared")
	}
	return nil
}

func (anchor *linuxOwnerAnchor) reapRunChildren(ctx context.Context, run *linuxOwnerAnchorRun) ([]LinuxOwnerAnchorReapEvidence, error) {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil || run.leaf == nil ||
		!run.helperExit.ValidFor(run.helper, anchor.identity) {
		return nil, errors.New("anchor run-wide reap is not fenced by exact helper death and leaf pins")
	}
	wasRemoved := run.leafRemovedObserved
	present, err := linuxOwnerAnchorPinnedLeafPresent(run)
	if err != nil {
		return nil, fmt.Errorf("verify exact pinned leaf before anchor reap: %w", err)
	}
	if present {
		if wasRemoved {
			return nil, errors.New("exact pinned run leaf reappeared after removal was observed")
		}
		if err := run.leaf.WaitEmpty(ctx); err != nil {
			return nil, fmt.Errorf("observe exact reserved leaf empty before anchor reap: %w", err)
		}
		run.leafEmptyObserved = true
	} else {
		run.leafRemovedObserved = true
		run.leafEmptyObserved = true
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pids, err := readLinuxOwnerProcessChildren(ctx, anchor.identity)
		if err != nil {
			return nil, fmt.Errorf("read anchor's exact kernel child list: %w", err)
		}
		var foundRunChild bool
		for _, pid := range pids {
			tracked, known := run.children[pid]
			if known && (tracked.identity.PID != pid || tracked.identity.Validate() != nil || tracked.pidfd == nil) {
				return nil, errors.New("anchor run child lacks its exact retained process identity and pidfd")
			}
			if physicalOnly, exists := run.physicalOnlyChildren[pid]; exists {
				process := physicalOnly.identity
				if known || process.PID != pid || process.Validate() != nil || physicalOnly.pidfd == nil ||
					physicalOnly.classification.ValidateFor(run.grant, run.helper, process) != nil ||
					physicalOnly.classification.Origin != linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
					return nil, errors.New("anchor physical-only gate child differs from its exact dedicated PIDFD tracker")
				}
				if _, alreadyInRunEvidence := run.reaped[pid]; alreadyInRunEvidence {
					return nil, errors.New("physical-only pre-RegisterGate child entered RunReapEvidence")
				}
				// This A gate is physical-containment-only, never a Run child receipt.
				if err := anchor.reapPhysicalOnlyChild(ctx, run, process); err != nil {
					return nil, fmt.Errorf("wait/reap exact physical-only pre-RegisterGate child: %w", err)
				}
				continue
			}
			var classification linuxOwnerAnchorRunChildClassification
			classified := false
			postKillRetained := false
			postKillObservation := run.leafRemovedObserved || run.leafKillAcknowledged && run.leafEmptyObserved
			var process authority.LinuxCgroupProcessIdentity
			if known && tracked.classification.currentRunMember() {
				process = tracked.identity
				tracked, classification, err = anchor.classifyRunChildForReap(ctx, run, process, tracked, known)
				if err != nil {
					return nil, err
				}
				classified = true
				if postKillObservation {
					ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
					if err != nil {
						return nil, fmt.Errorf("poll exact retained child pidfd after run-leaf containment: %w", err)
					}
					postKillRetained = ready && classification.currentRunMember()
				}
			}
			if !postKillRetained {
				identity, err := ProcessIdentity(pid)
				if err != nil {
					return nil, fmt.Errorf("capture anchor-adopted process creation identity: %w", err)
				}
				process = authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
				if known && !tracked.identity.Equal(process) {
					return nil, errors.New("anchor adopted a reused process PID for this run")
				}
				credentials, err := readLinuxOwnerProcessCredentials(pid)
				if err != nil || credentials.Parent != anchor.identity.PID {
					return nil, fmt.Errorf("verify exact anchor-adopted child ancestry: %w", errOr(err, errors.New("PPid differs from anchor")))
				}
			}
			if !known {
				cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
				if err != nil {
					return nil, fmt.Errorf("read untracked anchor child cgroup before cross-RunKey skip: %w", err)
				}
				otherRunChild, err := anchor.skipChildOwnedByAnotherRun(ctx, run, process, cgroupPath)
				if err != nil {
					return nil, fmt.Errorf("classify unknown anchor child before run-wide reap: %w", err)
				}
				if otherRunChild {
					continue
				}
			}
			if evidence, reaped := run.reaped[pid]; reaped {
				if err := evidence.ValidateFor(run.grant, run.helper, process, evidence.ProcessRole); err != nil {
					return nil, err
				}
				return nil, errors.New("anchor child is marked reaped but still appears in the kernel child list")
			}
			if !classified {
				tracked, classification, err = anchor.classifyRunChildForReap(ctx, run, process, tracked, known)
				if err != nil {
					return nil, err
				}
			}
			if classification.Origin == linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
				return nil, errors.New("physical-only pre-RegisterGate child lacks its dedicated Anchor PIDFD tracker")
			}
			role := tracked.role
			if classification.Role != role {
				return nil, errors.New("anchor child origin role differs from its exact retained run classification")
			}
			if !postKillRetained {
				switch role {
				case "gate":
					if transitionErr := run.verifyAuthorizedGateExec(process, !run.leafRemovedObserved); transitionErr == nil {
						role = "native"
						tracked.role = role
						tracked.classification.Role = role
						run.children[pid] = tracked
					} else if gateErr := run.verifyRegisteredGateA(process, !run.leafRemovedObserved); gateErr != nil {
						return nil, errors.Join(fmt.Errorf("verify authorized gate-to-native transition: %w", transitionErr), gateErr)
					}
				case "native":
					if err := run.verifyAuthorizedGateExec(process, !run.leafRemovedObserved); err != nil {
						return nil, fmt.Errorf("verify exact authorized native role before reap: %w", err)
					}
				default:
					if err := verifyLinuxOwnerRunChildCredentials(pid, role, run.owner); err != nil {
						return nil, err
					}
					postKillNamespaceOrigin := classification.Origin == linuxOwnerAnchorRunChildOriginGateUserNSCurrent &&
						(run.leafRemovedObserved || run.leafKillAcknowledged && run.leafEmptyObserved)
					if !run.leafRemovedObserved && !postKillNamespaceOrigin {
						if err := verifyLinuxOwnerRunChildMembership(pid, role, run.grant, run.leaf); err != nil {
							return nil, err
						}
					}
				}
			}
			foundRunChild = true
			if _, err := run.reapChild(ctx, anchor.identity, process, role); err != nil {
				return nil, err
			}
		}
		if !foundRunChild {
			break
		}
	}
	result := make([]LinuxOwnerAnchorReapEvidence, 0, len(run.reaped))
	for _, evidence := range run.reaped {
		result = append(result, evidence)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Process.PID < result[right].Process.PID })
	return result, nil
}

func (anchor *linuxOwnerAnchor) classifyRunChildForReap(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	tracked linuxOwnerAnchorTrackedProcess,
	known bool,
) (linuxOwnerAnchorTrackedProcess, linuxOwnerAnchorRunChildClassification, error) {
	if anchor == nil || ctx == nil || run == nil || process.Validate() != nil {
		return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("anchor run-child classification is incomplete")
	}
	pidfd := -1
	if known {
		if !tracked.identity.Equal(process) || tracked.pidfd == nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("registered run child lacks its exact pidfd")
		}
		pidfd = int(tracked.pidfd.Fd())
	} else {
		var err error
		pidfd, err = unix.PidfdOpen(process.PID, 0)
		if err != nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, fmt.Errorf("open pidfd for unregistered anchor child classification: %w", err)
		}
		defer unix.Close(pidfd)
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, err
		}
	}
	classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
	if err != nil {
		return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, fmt.Errorf("classify exact anchor run-child origin before wait: %w", err)
	}
	if err := classification.ValidateFor(run.grant, run.helper, process); err != nil {
		return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, err
	}
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
		registered, exists := run.children[process.PID]
		if !exists || !registered.identity.Equal(process) || registered.pidfd == nil ||
			registered.role != classification.Role || !registered.classification.Equal(classification) {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("registered-current origin lacks the exact retained RunKey child")
		}
		return registered, classification, nil
	case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		if classification.Role != "descendant" {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("gate-userns current origin is not a run descendant")
		}
		if known {
			if !tracked.identity.Equal(process) || tracked.classification.ValidateFor(run.grant, run.helper, process) != nil ||
				!tracked.classification.Equal(classification) || tracked.role != classification.Role {
				return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("namespace-proven current child differs from its retained origin pin")
			}
			return tracked, classification, nil
		}
		if err := run.trackNamespaceProvenChild(process, pidfd, classification); err != nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, fmt.Errorf("retain exact namespace-proven anchor child: %w", err)
		}
		registered, exists := run.children[process.PID]
		if !exists || !registered.identity.Equal(process) || registered.pidfd == nil ||
			!registered.classification.Equal(classification) || registered.creator.Validate() == nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("namespace-proven child was not retained without a synthetic creator")
		}
		return registered, classification, nil
	case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly:
		return linuxOwnerAnchorTrackedProcess{}, classification, errors.New("physical-only pre-RegisterGate gate cannot become RunReapEvidence")
	case linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		return linuxOwnerAnchorTrackedProcess{}, classification, errors.New("foreign physical-only gate is not a current-RunKey reap candidate")
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		run.migrationObserved = true
		return linuxOwnerAnchorTrackedProcess{}, classification, errors.New("exact current-run child escaped its reserved cgroup leaf")
	default:
		return linuxOwnerAnchorTrackedProcess{}, classification, fmt.Errorf("Anchor classified child as non-member of this RunKey (%s)", classification.Origin)
	}
}
