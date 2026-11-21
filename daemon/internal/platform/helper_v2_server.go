//go:build linux && amd64

package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func linuxOwnerHelperV2Supervisor(grant LinuxOwnerAnchorGrant, helper, launcher authority.LinuxCgroupProcessIdentity, gate authority.LinuxCgroupGateIdentity, launcherExit authority.LinuxCgroupExitProof) authority.Supervisor {
	launcherExitCopy := cloneLinuxOwnerAnchorExitProof(&launcherExit)
	return authority.Supervisor{
		Version:   authority.SupervisorVersionV2,
		TargetPID: gate.Process.PID, TargetIdentity: gate.Process.Identity,
		SupervisorPID: helper.PID, SupervisorIdentity: helper.Identity,
		AnchorPID: grant.Anchor.PID, AnchorIdentity: grant.Anchor.Identity,
		LauncherPID: launcher.PID, LauncherIdentity: launcher.Identity,
		TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity:  &grant.Leaf, LauncherExit: launcherExitCopy,
	}
}

// RunLinuxOwnerHelperV2 is the fixed hidden dispatch target. The only startup
// inputs are the anchor-authenticated bootstrap packet and inherited pinned
// parent/leaf/owner-pipe descriptors; no RunJournal or Store is opened here.
func RunLinuxOwnerHelperV2(args []string) error {
	if len(args) != 0 {
		return errors.New("Linux owner helper v2 accepts no command-line arguments")
	}
	bootstrapFile := os.NewFile(linuxOwnerHelperV2BootstrapFD, "linux-owner-helper-bootstrap")
	leafFile := os.NewFile(linuxOwnerHelperV2LeafFD, "linux-owner-helper-leaf-pin")
	parentFile := os.NewFile(linuxOwnerHelperV2ParentFD, "linux-owner-helper-parent-pin")
	ownerPipe := os.NewFile(linuxOwnerHelperV2OwnerPipeFD, "linux-owner-helper-owner-pipe")
	if bootstrapFile == nil || leafFile == nil || parentFile == nil || ownerPipe == nil {
		closeLinuxOwnerHelperFiles(bootstrapFile, leafFile, parentFile, ownerPipe)
		return errors.New("Linux owner helper v2 inherited descriptor set is incomplete")
	}
	defer closeLinuxOwnerHelperFiles(bootstrapFile, leafFile, parentFile)
	for _, file := range []*os.File{leafFile, parentFile, ownerPipe} {
		if _, err := unix.FcntlInt(file.Fd(), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
			_ = ownerPipe.Close()
			return fmt.Errorf("mark Linux owner helper inherited descriptor close-on-exec: %w", err)
		}
	}
	if err := verifyLinuxOwnerAnchorOwnerPipeReadFD(int(ownerPipe.Fd())); err != nil {
		_ = ownerPipe.Close()
		return err
	}
	bootstrap, err := readLinuxOwnerHelperV2Bootstrap(bootstrapFile)
	if err != nil {
		_ = ownerPipe.Close()
		return err
	}
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("read fixed Linux owner configuration in helper: %w", err)
	}
	if err := owner.Validate(); err != nil || uint32(os.Getuid()) != owner.AUID || uint32(os.Geteuid()) != owner.AUID ||
		uint32(os.Getgid()) != owner.AGID || uint32(os.Getegid()) != owner.AGID ||
		bootstrap.Grant.OwnerUID != owner.AUID || bootstrap.Grant.LauncherUID != 0 {
		_ = ownerPipe.Close()
		return errors.Join(errors.New("Linux owner helper v2 is not running under the fixed A identity"), err)
	}
	if bootstrap.Version != linuxOwnerHelperV2Version || bootstrap.Grant.Validate() != nil ||
		!canonicalAbsoluteLinuxPath(bootstrap.AnchorSocket) || bootstrap.OwnerProcess.Validate() != nil ||
		!linuxOwnerHelperV2WitnessBootstrapValid(bootstrap) {
		_ = ownerPipe.Close()
		return errors.New("Linux owner helper v2 bootstrap is invalid")
	}
	if err := ensureLinuxOwnerAnchorSubreaper(); err != nil {
		_ = ownerPipe.Close()
		return err
	}
	helperID, err := ProcessIdentity(os.Getpid())
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("capture Linux owner helper identity: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), helperID, owner.AUID, owner.AGID, bootstrap.Grant.Anchor.PID, bootstrap.Grant.Parent.Path); err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("verify exact Linux owner helper parent and manager membership: %w", err)
	}
	anchorID, err := ProcessIdentity(bootstrap.Grant.Anchor.PID)
	if err != nil || anchorID != bootstrap.Grant.Anchor.Identity {
		_ = ownerPipe.Close()
		return fmt.Errorf("verify exact Linux owner anchor identity from helper: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	parent, err := AdoptLinuxCgroupV2Parent(int(parentFile.Fd()), bootstrap.Grant.Parent)
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("adopt the continuously pinned Linux cgroup parent: %w", err)
	}
	defer parent.Close()
	leaf, err := parent.AdoptLeaf(bootstrap.Grant.Leaf.LeafName, int(leafFile.Fd()), bootstrap.Grant.Leaf)
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("adopt the exact reserved Linux cgroup leaf: %w", err)
	}
	defer leaf.Close()
	if err := verifyLinuxOwnerAnchorLeaf(leaf, bootstrap.Grant.Leaf, owner.AUID); err != nil {
		_ = ownerPipe.Close()
		return err
	}
	anchorClient, err := NewLinuxOwnerAnchorClient(bootstrap.AnchorSocket)
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("connect the helper to its authenticated owner anchor: %w", err)
	}
	listener, socketIdentity, err := listenLinuxOwnerAnchor(bootstrap.Grant.RecoveryEndpoint, owner)
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("listen on the exact helper recovery endpoint: %w", err)
	}
	defer unlinkLinuxOwnerAnchorSocket(bootstrap.Grant.RecoveryEndpoint, socketIdentity, owner.AUID)
	helper := &linuxOwnerHelperV2{
		owner: owner, grant: bootstrap.Grant, helper: authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: helperID},
		initialOwner: bootstrap.OwnerProcess, anchorClient: anchorClient, parent: parent, leaf: leaf,
		listener: listener, ownerPipe: ownerPipe, activeConns: make(map[*net.UnixConn]struct{}),
		witnessDropResponse:       bootstrap.WitnessDropResponse,
		witnessHelperAction:       bootstrap.WitnessHelperAction,
		witnessHoldForReleaseFile: bootstrap.WitnessHoldForReleaseFile,
		witnessRunKey:             LinuxOwnerAnchorRunKey{RunID: bootstrap.WitnessRunID, Generation: bootstrap.WitnessGeneration},
		lease:                     newLinuxOwnerHelperV2Lease(),
	}
	ready := linuxOwnerHelperV2BootstrapReady{
		Version: linuxOwnerHelperV2Version, Status: "ready", Helper: helper.helper,
		Endpoint: bootstrap.Grant.RecoveryEndpoint,
	}
	if err := writeLinuxOwnerHelperV2BootstrapReady(bootstrapFile, ready); err != nil {
		return fmt.Errorf("acknowledge fixed Linux owner helper startup: %w", err)
	}
	return helper.serve()
}

type linuxOwnerHelperV2Lease struct {
	mu         sync.Mutex
	sequence   uint64
	generation uint64
	duration   time.Duration
	deadline   time.Time
	timer      *time.Timer
	stopped    bool
	wake       chan struct{}
}

func (lease *linuxOwnerHelperV2Lease) renew(duration time.Duration, sequence uint64) (bool, error) {
	if lease == nil || duration <= 0 || sequence == 0 {
		return false, errors.New("Linux owner helper lease is incomplete")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.stopped || !lease.deadline.IsZero() && !time.Now().Before(lease.deadline) {
		lease.latchLocked(true)
		return false, errors.New("Linux owner helper lease is expired")
	}
	if sequence == lease.sequence {
		if !lease.deadline.IsZero() && duration == lease.duration {
			return true, nil
		}
		return false, errors.New("Linux owner helper lease sequence replay differs")
	}
	if sequence < lease.sequence || lease.sequence == 0 && sequence != 1 || lease.sequence > 0 && sequence != lease.sequence+1 {
		return false, errors.New("Linux owner helper lease sequence is not the exact next value")
	}
	if lease.timer != nil {
		lease.timer.Stop()
	}
	lease.sequence = sequence
	lease.generation++
	generation := lease.generation
	lease.duration = duration
	lease.deadline = time.Now().Add(duration)
	lease.timer = time.AfterFunc(duration, func() {
		lease.mu.Lock()
		if lease.sequence == sequence && lease.generation == generation &&
			!lease.stopped && !time.Now().Before(lease.deadline) {
			lease.latchLocked(true)
		}
		lease.mu.Unlock()
	})
	return false, nil
}

// latchLocked freezes the lease. notify is true only for deadline expiry; an
// explicit stop must not be reported to the owner-loss watcher, because the
// stop owner still needs the accepted stop response on its live connection.
func (lease *linuxOwnerHelperV2Lease) latchLocked(notify bool) {
	if lease == nil || lease.stopped {
		return
	}
	lease.stopped = true
	lease.deadline = time.Time{}
	if lease.timer != nil {
		lease.timer.Stop()
		lease.timer = nil
	}
	if notify {
		select {
		case lease.wake <- struct{}{}:
		default:
		}
	}
}

func (lease *linuxOwnerHelperV2Lease) stop() {
	if lease == nil {
		return
	}
	lease.mu.Lock()
	lease.latchLocked(false)
	lease.mu.Unlock()
}

func (lease *linuxOwnerHelperV2Lease) isStopped() bool {
	if lease == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.stopped
}

func (lease *linuxOwnerHelperV2Lease) snapshot() (uint64, bool) {
	if lease == nil {
		return 0, false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.deadline.IsZero() {
		return lease.sequence, false
	}
	if lease.stopped || !time.Now().Before(lease.deadline) {
		lease.latchLocked(true)
		return lease.sequence, false
	}
	return lease.sequence, true
}

// peekStatus reports lease liveness without latching expiry or waking the
// owner-loss watcher. Status is an observation; effectful paths own expiry.
func (lease *linuxOwnerHelperV2Lease) peekStatus() (uint64, bool) {
	if lease == nil {
		return 0, false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.sequence, !lease.deadline.IsZero() && !lease.stopped && time.Now().Before(lease.deadline)
}

func (lease *linuxOwnerHelperV2Lease) releaseGuard() (uint64, time.Time, bool) {
	if lease == nil {
		return 0, time.Time{}, false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.deadline.IsZero() {
		return lease.sequence, time.Time{}, false
	}
	if lease.stopped || !time.Now().Before(lease.deadline) {
		lease.latchLocked(true)
		return lease.sequence, time.Time{}, false
	}
	return lease.sequence, lease.deadline, true
}

type linuxOwnerHelperV2 struct {
	owner                     linuxgate.OwnerConfig
	grant                     LinuxOwnerAnchorGrant
	helper                    authority.LinuxCgroupProcessIdentity
	initialOwner              authority.LinuxCgroupProcessIdentity
	anchorClient              *LinuxOwnerAnchorClient
	parent                    *LinuxCgroupV2Parent
	leaf                      *LinuxCgroupV2Leaf
	listener                  *net.UnixListener
	ownerPipe                 *os.File
	lease                     *linuxOwnerHelperV2Lease
	witnessDropResponse       string
	witnessHelperAction       string
	witnessHoldForReleaseFile bool
	witnessRunKey             LinuxOwnerAnchorRunKey

	stateMu                    sync.Mutex
	activeConns                map[*net.UnixConn]struct{}
	activeOwner                authority.LinuxCgroupProcessIdentity
	recoveryOwner              authority.LinuxCgroupProcessIdentity
	ownerLost                  bool
	finalized                  bool
	finalizing                 bool
	anchorTakeoverRequested    bool
	finalization               *linuxOwnerHelperV2Finalization
	prepareStarted             bool
	prepareDone                chan struct{}
	prepareCancel              context.CancelFunc
	prepareResponse            *linuxOwnerHelperV2Response
	launcher                   *authority.LinuxCgroupProcessIdentity
	launcherExit               *authority.LinuxCgroupExitProof
	gateOutcome                *authority.LinuxCgroupGateProductionResult
	gate                       *authority.LinuxCgroupGateIdentity
	gateUserNamespace          *LinuxUserNamespaceIdentity
	prepared                   *linuxgate.PreparedGate
	untrustedProcess           *authority.LinuxCgroupProcessIdentity
	helperBoundAck             bool
	commitAcknowledged         bool
	releaseAttempted           bool
	releaseInFlight            bool
	releaseDone                chan struct{}
	releaseCancel              context.CancelFunc
	released                   bool
	stopRequested              bool
	stopAttempted              bool
	stopInFlight               bool
	stopDone                   chan struct{}
	descendantReaped           map[int]authority.LinuxCgroupProcessIdentity
	stopEvidence               *LinuxOwnerHelperV2StopEvidence
	stopErrorCode              string
	witnessDropUsed            bool
	witnessActionObserved      bool
	witnessAction              string
	witnessExitRequested       bool
	witnessExitCause           error
	receiptAcknowledged        bool
	acknowledgedReceipt        *authority.StopReceipt
	acknowledgedPreGateReceipt *authority.LinuxCgroupPreGateStopReceipt
	waitMu                     sync.Mutex
	waitStarted                bool
	waitDone                   chan struct{}
	waitResult                 *LinuxOwnerHelperV2Exit
	waitError                  error
	untrustedWaitMu            sync.Mutex
	untrustedWaitAttempted     bool
	untrustedWaitResult        *LinuxOwnerHelperV2Exit
	untrustedWaitError         error
}

// linuxOwnerHelperV2Finalization serializes pin retirement for one exact
// durable proof. The record remains as the in-memory idempotency tombstone
// after success; it is not a journal and cannot mint State authority.
type linuxOwnerHelperV2Finalization struct {
	kind         string
	releaseProof *authority.SupervisorHandoffReleaseProof
	preGateProof *authority.LinuxCgroupPreGateReleaseProof
	abortProof   *authority.LinuxCgroupReservationAbortProof
	done         chan struct{}
	err          error
	completed    bool
}

func (finalization *linuxOwnerHelperV2Finalization) matches(request linuxOwnerHelperV2Request) bool {
	if finalization == nil {
		return false
	}
	switch finalization.kind {
	case "normal":
		return request.ReleaseProof != nil && request.PreGateReleaseProof == nil && request.AbortProof == nil &&
			finalization.releaseProof != nil && finalization.releaseProof.Equal(*request.ReleaseProof)
	case "pre-gate":
		return request.ReleaseProof == nil && request.PreGateReleaseProof != nil && request.AbortProof == nil &&
			finalization.preGateProof != nil && finalization.preGateProof.Equal(*request.PreGateReleaseProof)
	case "abort":
		return request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof != nil &&
			finalization.abortProof != nil && finalization.abortProof.Equal(*request.AbortProof)
	default:
		return false
	}
}

func (helper *linuxOwnerHelperV2) serve() (result error) {
	if helper == nil || helper.listener == nil || helper.leaf == nil || helper.parent == nil || helper.ownerPipe == nil {
		return errors.New("Linux owner helper lifecycle is incomplete")
	}
	watchContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchersDone := make(chan struct{}, 2)
	var sessions sync.WaitGroup
	go func() {
		defer func() { watchersDone <- struct{}{} }()
		helper.watchOwnerPipe(watchContext)
	}()
	go func() {
		defer func() { watchersDone <- struct{}{} }()
		helper.watchLease(watchContext)
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	signalDone := make(chan struct{})
	go func() {
		defer close(signalDone)
		select {
		case <-watchContext.Done():
		case <-signals:
			if err := helper.markOwnerLost(); err != nil {
				helper.requestAnchorTakeover()
			}
		}
	}()
	defer func() {
		_ = helper.listener.Close()
		// Keep owner/lease watchers alive while active sessions quiesce. A
		// failed session drain must hand control to the anchor, not strand a
		// live helper outside its pidfd-death observer.
		sessionsDone := make(chan struct{})
		go func() {
			sessions.Wait()
			close(sessionsDone)
		}()
		drainTimer := time.NewTimer(linuxOwnerHelperV2StopTimeout)
		select {
		case <-sessionsDone:
			if !drainTimer.Stop() {
				select {
				case <-drainTimer.C:
				default:
				}
			}
		case <-drainTimer.C:
			result = errors.Join(result, errors.New("Linux owner helper control sessions did not quiesce; yielding to anchor containment"))
			helper.requestAnchorTakeover()
			return
		}
		cancel()
		<-watchersDone
		<-watchersDone
		<-signalDone
		if helper.ownerPipe != nil {
			_ = helper.ownerPipe.Close()
		}
		helper.lease.stop()
		if helper.prepared != nil {
			_ = helper.prepared.Close()
		}
		if helper.leaf != nil {
			_ = helper.leaf.Close()
		}
		if helper.parent != nil {
			_ = helper.parent.Close()
		}
	}()
	for {
		connection, err := helper.listener.AcceptUnix()
		if err != nil {
			if helper.isFinalized() {
				return nil
			}
			if helper.isWitnessExitRequested() {
				return helper.witnessExitError()
			}
			acceptErr := fmt.Errorf("accept Linux owner helper control connection: %w", sanitizeLinuxOwnerAnchorSocketError(err))
			ownerErr := helper.markOwnerLost()
			if ownerErr != nil {
				helper.requestAnchorTakeover()
			}
			return errors.Join(acceptErr, ownerErr)
		}
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			_ = helper.serveOwnerConnection(connection)
			_ = connection.Close()
			if helper.isFinalized() {
				_ = helper.listener.Close()
			}
		}()
	}
}

func (helper *linuxOwnerHelperV2) serveOwnerConnection(connection *net.UnixConn) error {
	if err := connection.SetDeadline(time.Now().Add(linuxOwnerAnchorRequestTimeout)); err != nil {
		return err
	}
	peer, err := linuxOwnerHelperPeerFromConn(connection, helper.owner)
	anchorPeer := peer.PID == helper.grant.Anchor.PID && peer.Identity == helper.grant.Anchor.Identity
	if err != nil || linuxOwnerCgroupPathsOverlap(peer.CgroupPath, helper.grant.Parent.Path) && !anchorPeer {
		return errors.Join(errors.New("Linux owner helper rejected an untrusted control peer"), err)
	}
	request, receivedFDs, err := readLinuxOwnerHelperV2Request(connection)
	closeLinuxOwnerAnchorFDs(receivedFDs)
	if err != nil {
		return err
	}
	if request.Operation != "hello" || !helper.requestMatchesPeer(request, peer) {
		return errors.New("Linux owner helper hello differs from exact SO_PEERCRED or RunKey grant")
	}
	helper.stateMu.Lock()
	if helper.finalized {
		helper.stateMu.Unlock()
		return errors.New("Linux owner helper already completed durable clear")
	}
	recoveryOnly := helper.ownerLost
	if !recoveryOnly && !anchorPeer && !peerProcessEqual(peer, helper.initialOwner) {
		helper.stateMu.Unlock()
		return errors.New("Linux owner helper initial control peer differs from the authenticated StartHelper caller")
	}
	peerIdentity := authority.LinuxCgroupProcessIdentity{PID: peer.PID, Identity: peer.Identity}
	if recoveryOnly && !anchorPeer {
		if helper.recoveryOwner.Validate() != nil {
			helper.recoveryOwner = peerIdentity
		} else if !helper.recoveryOwner.Equal(peerIdentity) {
			helper.stateMu.Unlock()
			return errors.New("Linux owner helper recovery peer differs from its first authenticated attacher")
		}
	} else if !anchorPeer && helper.activeOwner.Validate() != nil {
		helper.activeOwner = peerIdentity
	} else if !anchorPeer && !helper.activeOwner.Equal(peerIdentity) {
		helper.stateMu.Unlock()
		return errors.New("Linux owner helper control peer changed within the initial owner phase")
	}
	helper.activeConns[connection] = struct{}{}
	helper.stateMu.Unlock()
	defer func() {
		helper.stateMu.Lock()
		delete(helper.activeConns, connection)
		helper.stateMu.Unlock()
	}()
	helloStatus := "ready"
	if recoveryOnly {
		helloStatus = "recovery_only"
	}
	if err := helper.writeResponse(connection, "hello", helloStatus, nil, nil, nil, nil, nil, 0, nil, ""); err != nil {
		return err
	}
	for {
		if err := connection.SetDeadline(time.Now().Add(linuxOwnerHelperV2Timeout)); err != nil {
			return err
		}
		request, rights, err := readLinuxOwnerHelperV2Request(connection)
		if err != nil {
			return err
		}
		if !helper.requestMatchesPeer(request, peer) {
			closeLinuxOwnerAnchorFDs(rights)
			return errors.New("Linux owner helper request differs from exact SO_PEERCRED or RunKey grant")
		}
		if anchorPeer && request.Operation != "status" && request.Operation != "stop" && request.Operation != "release-run" {
			closeLinuxOwnerAnchorFDs(rights)
			return errors.New("Linux owner anchor recovery session may only inspect, stop, or finalize its exact helper")
		}
		finished, handleErr := helper.handleRequest(connection, request, rights)
		closeLinuxOwnerAnchorFDs(rights)
		if handleErr != nil {
			return handleErr
		}
		if finished {
			return nil
		}
	}
}

func (helper *linuxOwnerHelperV2) requestMatchesPeer(request linuxOwnerHelperV2Request, peer linuxOwnerAnchorPeer) bool {
	return request.Version == linuxOwnerHelperV2Version && request.Grant.Equal(helper.grant) &&
		request.ClientPID == peer.PID && request.ClientIdentity == peer.Identity
}

func (helper *linuxOwnerHelperV2) handleRequest(connection *net.UnixConn, request linuxOwnerHelperV2Request, rights []int) (bool, error) {
	helper.stateMu.Lock()
	ownerLost := helper.ownerLost
	finalized := helper.finalized
	helper.stateMu.Unlock()
	if finalized {
		if request.Operation == "release-run" {
			return helper.handleFinalRelease(connection, request)
		}
		return false, helper.writeResponse(connection, request.Operation, "rejected", nil, nil, nil, nil, nil, 0, nil, "already_finalized")
	}
	helper.stateMu.Lock()
	finalizing := helper.finalizing
	helper.stateMu.Unlock()
	if finalizing && request.Operation != "status" && request.Operation != "release-run" {
		return false, helper.writeResponse(connection, request.Operation, "recovery_only", nil, nil, nil, nil, nil, 0, nil, "final_release_pending")
	}
	if ownerLost && request.Operation != "status" && request.Operation != "stop" && request.Operation != "fence-before-abort" && request.Operation != "ack-stop-receipt" && request.Operation != "release-run" {
		return false, helper.writeResponse(connection, request.Operation, "recovery_only", nil, nil, nil, nil, nil, 0, nil, "owner_lost")
	}
	switch request.Operation {
	case "status":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper status request carried unexpected descriptors")
		}
		return false, helper.handleStatus(connection)
	case "prepare":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper prepare request carried unexpected descriptors")
		}
		return false, helper.handlePrepare(connection)
	case "helper-bound":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper binding ACK carried unexpected descriptors")
		}
		return false, helper.handleHelperBound(connection, request)
	case "launcher-ack":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper launcher ACK carried unexpected descriptors")
		}
		return false, helper.handleLauncherAck(connection, request)
	case "commit":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper commit request carried unexpected descriptors")
		}
		return false, helper.handleCommit(connection, request)
	case "renew-lease":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper lease request carried unexpected descriptors")
		}
		return false, helper.handleRenewLease(connection, request)
	case "release":
		return false, helper.handleRelease(connection, request, rights)
	case "wait":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper wait request carried unexpected descriptors")
		}
		return false, helper.handleWait(connection)
	case "stop":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper stop request carried unexpected descriptors")
		}
		return false, helper.handleStop(connection)
	case "fence-before-abort":
		if len(rights) != 0 {
			return false, errors.New("Linux pre-abort fence request carried unexpected descriptors")
		}
		err := helper.handleFenceBeforeAbort(connection)
		return helper.isFinalized(), err
	case "ack-stop-receipt":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper receipt ACK carried unexpected descriptors")
		}
		return false, helper.handleReceiptAck(connection, request)
	case "release-run":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper final release carried unexpected descriptors")
		}
		return helper.handleFinalRelease(connection, request)
	default:
		return false, helper.writeResponse(connection, request.Operation, "rejected", nil, nil, nil, nil, nil, 0, nil, "unsupported_operation")
	}
}

func (helper *linuxOwnerHelperV2) handleStatus(connection *net.UnixConn) error {
	helper.stateMu.Lock()
	launcher := cloneLinuxOwnerProcessIdentity(helper.launcher)
	gate := cloneLinuxOwnerGateIdentity(helper.gate)
	userNamespace := helper.gateUserNamespace
	launcherExit := cloneLinuxOwnerAnchorExitProof(helper.launcherExit)
	leaseSequence, leaseLive := helper.lease.peekStatus()
	ownerLost := helper.ownerLost
	prepared := helper.prepareResponse != nil
	evidence := helper.stopEvidence
	commitAcknowledged := helper.commitAcknowledged
	releaseAttempted := helper.releaseAttempted
	releaseInFlight := helper.releaseInFlight
	released := helper.released
	stopRequested := helper.stopRequested
	stopAttempted := helper.stopAttempted
	witnessDropObserved := helper.witnessDropUsed
	witnessDropOperation := ""
	if witnessDropObserved {
		witnessDropOperation = helper.witnessDropResponse
	}
	witnessActionObserved := helper.witnessActionObserved
	witnessAction := helper.witnessAction
	if witnessActionObserved && (witnessAction != linuxOwnerHelperV2WitnessCrashAfterRelease ||
		helper.witnessRunKey != helper.grant.RunKey || !released || !releaseAttempted) ||
		!witnessActionObserved && witnessAction != "" {
		helper.stateMu.Unlock()
		return errors.New("Linux owner helper witness action is not bound to a successful exact RELEASE")
	}
	if !witnessActionObserved {
		witnessAction = ""
	}
	var errorCode string
	if evidence == nil && helper.stopAttempted {
		errorCode = helper.stopErrorCode
	}
	var gatePIDFD int = -1
	if gate != nil && helper.prepared != nil {
		if retained := helper.prepared.PIDFD(); retained != nil {
			gatePIDFD, _ = unix.FcntlInt(retained.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		}
	}
	helper.stateMu.Unlock()
	status := "active"
	if ownerLost {
		status = "recovery_only"
	}
	if evidence != nil {
		status = "stopped"
	}
	if !prepared && !ownerLost {
		status = "ready"
	}
	response := helper.baseResponse("status", status)
	response.Launcher = launcher
	response.Gate = gate
	response.LauncherExit = launcherExit
	response.UserNamespace = userNamespace
	response.StopEvidence = evidence
	response.ErrorCode = errorCode
	response.OwnerLost = ownerLost
	response.WitnessDropObserved = witnessDropObserved
	response.WitnessDropOperation = witnessDropOperation
	response.WitnessActionObserved = witnessActionObserved
	response.WitnessAction = witnessAction
	response.CommitAcknowledged = commitAcknowledged
	response.ReleaseAttempted = releaseAttempted
	response.Released = released
	response.LeaseSequence = leaseSequence
	response.StatusSnapshot = &linuxOwnerHelperV2StatusSnapshot{
		LeaseLive: leaseLive, LeaseSequence: leaseSequence,
		StopRequested: stopRequested, StopAttempted: stopAttempted, ReleaseInFlight: releaseInFlight,
	}
	helper.waitMu.Lock()
	if helper.waitDone != nil {
		select {
		case <-helper.waitDone:
			response.ExitResult = helper.waitResult
			if helper.waitError != nil {
				response.ErrorCode = "gate_wait_unproven"
			}
		default:
		}
	}
	helper.waitMu.Unlock()
	if gatePIDFD >= 0 {
		defer unix.Close(gatePIDFD)
		return writeLinuxOwnerHelperV2Response(connection, response, gatePIDFD)
	}
	return writeLinuxOwnerHelperV2Response(connection, response)
}

func (helper *linuxOwnerHelperV2) handlePrepare(connection *net.UnixConn) error {
	helper.stateMu.Lock()
	if !helper.helperBoundAck || helper.ownerLost || helper.stopRequested || helper.stopAttempted {
		helper.stateMu.Unlock()
		code := "helper_bind_unacknowledged"
		if helper.ownerLost || helper.stopRequested || helper.stopAttempted {
			code = "owner_lost"
		}
		return helper.writeResponse(connection, "prepare-failed", "rejected", nil, nil, nil, nil, nil, 0, nil, code)
	}
	if helper.prepareResponse != nil {
		response := *helper.prepareResponse
		helper.stateMu.Unlock()
		return helper.sendPrepareResponse(connection, response)
	}
	if helper.prepareStarted {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "prepare-failed", "unresolved", nil, nil, nil, nil, nil, 0, nil, "prepare_in_progress")
	}
	helper.prepareStarted = true
	helper.prepareDone = make(chan struct{})
	prepareContext, cancel := context.WithCancel(context.Background())
	helper.prepareCancel = cancel
	done := helper.prepareDone
	helper.stateMu.Unlock()
	defer func() {
		cancel()
		helper.stateMu.Lock()
		if helper.prepareDone == done {
			close(done)
		}
		helper.stateMu.Unlock()
	}()
	if err := helper.leaf.VerifyKillControl(); err != nil {
		return helper.cachePrepareFailure(connection, "leaf_kill_control_unavailable", nil)
	}

	birth, err := helper.leaf.BirthFD()
	if err != nil {
		return helper.cachePrepareFailure(connection, "leaf_unavailable", nil)
	}
	defer birth.Close()
	gateAuth := linuxgate.GateProductionAuth{LaunchToken: helper.grant.LaunchToken, Secret: helper.grant.Secret}
	var launcherIdentity authority.LinuxCgroupProcessIdentity
	onLauncherStart := func(ctx context.Context, launcher linuxgate.LauncherIdentity, pidfd *os.File) error {
		process := authority.LinuxCgroupProcessIdentity{PID: launcher.PID, Identity: launcher.Identity}
		if process.Validate() != nil || pidfd == nil {
			return errors.New("Linux root launcher event has no exact identity or pidfd")
		}
		if err := helper.anchorClient.RegisterProcess(ctx, helper.grant, helper.helper, helper.helper, process, "launcher", int(pidfd.Fd())); err != nil {
			return fmt.Errorf("register exact root launcher with owner anchor: %w", err)
		}
		launcherIdentity = process
		helper.stateMu.Lock()
		helper.launcher = cloneLinuxOwnerProcessIdentity(&process)
		helper.stateMu.Unlock()
		if err := helper.writeResponse(connection, "launcher-start", "awaiting_durable_grant", &process, nil, nil, nil, nil, 0, nil, ""); err != nil {
			return err
		}
		request, rights, err := readLinuxOwnerHelperV2Request(connection)
		if err != nil {
			closeLinuxOwnerAnchorFDs(rights)
			return err
		}
		if len(rights) != 0 {
			closeLinuxOwnerAnchorFDs(rights)
			return errors.New("Linux owner launcher ACK carried unexpected descriptors")
		}
		helper.stateMu.Lock()
		activeOwner := helper.activeOwner
		helper.stateMu.Unlock()
		if request.Version != linuxOwnerHelperV2Version || !request.Grant.Equal(helper.grant) ||
			request.ClientPID != activeOwner.PID || request.ClientIdentity != activeOwner.Identity ||
			request.Operation != "launcher-ack" || request.Launcher == nil || !request.Launcher.Equal(process) || !request.Accepted {
			return errors.New("daemon did not return an exact durable BindLauncher+ArmGrant ACK")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
	preparedGate, prepareErr := linuxgate.Prepare(prepareContext, int(birth.Fd()), gateAuth, onLauncherStart)
	var gateIdentity authority.LinuxCgroupGateIdentity
	var processIdentity *authority.LinuxCgroupProcessIdentity
	var userNamespace *LinuxUserNamespaceIdentity
	var launcherExit *authority.LinuxCgroupExitProof
	var gatePIDFD int = -1
	if preparedGate != nil {
		identity := preparedGate.Identity()
		process, identityErr := linuxOwnerHelperGateProcessIdentity(identity)
		gateFile := preparedGate.PIDFD()
		if identityErr == nil && gateFile != nil {
			processIdentity = &process
			gatePIDFD = int(gateFile.Fd())
			if prepareErr == nil {
				namespace, namespaceErr := CaptureLinuxUserNamespaceIdentity(process.PID, process.Identity, preparedGate.PIDFD())
				if namespaceErr != nil || namespace.Device != identity.UserNSDevice || namespace.Inode != identity.UserNSInode {
					prepareErr = errors.Join(prepareErr, fmt.Errorf("capture exact parked-gate user namespace: %w", errOr(namespaceErr, errors.New("user namespace differs from launcher result"))))
				} else {
					userNamespace = &namespace
				}
			}
			if prepareErr == nil {
				gateIdentity = authority.LinuxCgroupGateIdentity{
					Process: process, HostUID: helper.owner.AUID, ObservedAt: time.Now().UTC(),
					BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: helper.grant.Leaf,
					BornInLeaf: true, TrustedImage: true,
				}
				if err := gateIdentity.Validate(); err != nil {
					prepareErr = fmt.Errorf("validate exact A gate identity: %w", err)
				}
			}
			registerContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorRequestTimeout)
			var registerErr error
			if gateIdentity.Validate() == nil && gateIdentity.Process.Equal(process) && userNamespace != nil {
				registerErr = helper.anchorClient.RegisterGate(registerContext, helper.grant, helper.helper, gateIdentity, *userNamespace, gatePIDFD)
			} else {
				// A failed Prepare may retain an exact A child for emergency stop,
				// but must never establish gate-release transition authority.
				if credentialErr := verifyLinuxOwnerRunChildCredentials(process.PID, "descendant", helper.owner); credentialErr != nil {
					registerErr = fmt.Errorf("failed-Prepare child credentials are unresolved: %w", credentialErr)
				} else if membershipErr := verifyLinuxOwnerRunChildMembership(process.PID, "descendant", helper.grant, helper.leaf); membershipErr != nil {
					registerErr = fmt.Errorf("failed-Prepare child membership is unresolved: %w", membershipErr)
				} else {
					registerErr = helper.anchorClient.RegisterProcess(registerContext, helper.grant, helper.helper, helper.helper, process, "descendant", gatePIDFD)
				}
			}
			cancel()
			if registerErr != nil {
				prepareErr = errors.Join(prepareErr, fmt.Errorf("register exact parked gate with owner anchor: %w", registerErr))
			}
		} else {
			if identityErr == nil {
				identityErr = errors.New("prepared gate omitted its retained pidfd")
			}
			prepareErr = errors.Join(prepareErr, identityErr)
		}
	}
	var observedLauncherExit authority.LinuxCgroupExitProof
	launcherExitProven := false
	if preparedGate != nil {
		observedLauncherExit, launcherExitProven = preparedGate.LauncherExitProof()
	}
	if !launcherExitProven {
		var neverProduced *linuxgate.GateNeverProducedError
		if errors.As(prepareErr, &neverProduced) {
			observedLauncherExit, launcherExitProven = neverProduced.LauncherExitProof()
		}
	}
	if !launcherExitProven {
		var launcherOutcome *linuxgate.LauncherOutcomeError
		if errors.As(prepareErr, &launcherOutcome) {
			observedLauncherExit, launcherExitProven = launcherOutcome.ExitProof()
		}
	}
	if launcherExitProven {
		if launcherIdentity.Validate() == nil && observedLauncherExit.ValidForOwnedDescendant(launcherIdentity, helper.helper, helper.grant.Anchor, nil) {
			launcherExit = &observedLauncherExit
		} else {
			prepareErr = errors.Join(prepareErr, errors.New("protocol launcher exit proof differs from the exact helper-owned launcher"))
		}
	} else if launcherIdentity.Validate() == nil && (preparedGate != nil || isGateNeverProduced(prepareErr)) {
		prepareErr = errors.Join(prepareErr, errors.New("protocol returned no exact launcher exit/reap proof"))
	}
	var gateOutcome *authority.LinuxCgroupGateProductionResult
	var neverProduced *linuxgate.GateNeverProducedError
	if errors.As(prepareErr, &neverProduced) {
		result := neverProduced.Result()
		gateOutcome = &result
	}
	helper.stateMu.Lock()
	if launcherIdentity.Validate() == nil {
		helper.launcher = cloneLinuxOwnerProcessIdentity(&launcherIdentity)
	}
	if launcherExit != nil {
		helper.launcherExit = cloneLinuxOwnerAnchorExitProof(launcherExit)
	}
	if gateOutcome != nil {
		helper.gateOutcome = gateOutcome
	}
	helper.stateMu.Unlock()
	if preparedGate == nil || prepareErr != nil {
		if preparedGate != nil {
			helper.stateMu.Lock()
			helper.prepared = preparedGate
			if gateOutcome != nil {
				helper.gateOutcome = gateOutcome
			}
			if gateIdentity.Validate() == nil {
				helper.gate = cloneLinuxOwnerGateIdentity(&gateIdentity)
				helper.gateUserNamespace = userNamespace
			}
			if launcherExit != nil {
				helper.launcherExit = cloneLinuxOwnerAnchorExitProof(launcherExit)
			}
			helper.stateMu.Unlock()
		}
		result := linuxOwnerHelperV2Response{
			Version: linuxOwnerHelperV2Version, Operation: "prepare-failed", Status: "unresolved",
			RunKey: helper.grant.RunKey, Fence: helper.grant.Fence, LaunchToken: helper.grant.LaunchToken,
			Helper: helper.helper, Launcher: cloneLinuxOwnerProcessIdentity(&launcherIdentity),
			Gate: cloneLinuxOwnerGateIdentityIfValid(gateIdentity), LauncherExit: cloneLinuxOwnerAnchorExitProof(launcherExit),
			GateProcess: processIdentity, UserNamespace: userNamespace, GateOutcome: gateOutcome,
			ErrorCode: linuxOwnerHelperV2ErrorCode(prepareErr),
		}
		helper.stateMu.Lock()
		helper.prepareResponse = &result
		helper.stateMu.Unlock()
		return helper.sendPrepareResponse(connection, result)
	}
	if processIdentity == nil || launcherExit == nil || userNamespace == nil {
		helper.stateMu.Lock()
		helper.prepared = preparedGate
		helper.stateMu.Unlock()
		return helper.cachePrepareFailure(connection, "producer_identity_unproven", nil)
	}
	helper.stateMu.Lock()
	helper.prepared = preparedGate
	helper.gate = cloneLinuxOwnerGateIdentity(&gateIdentity)
	helper.gateUserNamespace = userNamespace
	helper.launcher = cloneLinuxOwnerProcessIdentity(&launcherIdentity)
	helper.launcherExit = cloneLinuxOwnerAnchorExitProof(launcherExit)
	result := linuxOwnerHelperV2Response{
		Version: linuxOwnerHelperV2Version, Operation: "gate-ready", Status: "ready",
		RunKey: helper.grant.RunKey, Fence: helper.grant.Fence, LaunchToken: helper.grant.LaunchToken,
		Helper: helper.helper, Launcher: cloneLinuxOwnerProcessIdentity(&launcherIdentity),
		Gate: cloneLinuxOwnerGateIdentity(&gateIdentity), LauncherExit: cloneLinuxOwnerAnchorExitProof(launcherExit),
		UserNamespace: userNamespace,
	}
	helper.prepareResponse = &result
	helper.stateMu.Unlock()
	return helper.sendPrepareResponse(connection, result)
}

func (helper *linuxOwnerHelperV2) handleHelperBound(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	helper.stateMu.Lock()
	if request.Helper == nil || !request.Helper.Equal(helper.helper) || helper.ownerLost || helper.stopRequested || helper.stopAttempted || helper.prepareStarted {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "helper-bound", "rejected", nil, nil, nil, nil, nil, 0, nil, "exact_helper_bind_ack_required")
	}
	if helper.helperBoundAck {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "helper-bound", "already_bound", nil, nil, nil, nil, nil, 0, nil, "")
	}
	helper.helperBoundAck = true
	helper.stateMu.Unlock()
	return helper.writeResponse(connection, "helper-bound", "bound", nil, nil, nil, nil, nil, 0, nil, "")
}

func (helper *linuxOwnerHelperV2) handleLauncherAck(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	// launcher-ack is consumed synchronously by the protocol.Prepare hook.
	return helper.writeResponse(connection, request.Operation, "rejected", nil, nil, nil, nil, nil, 0, nil, "unexpected_ack")
}

func (helper *linuxOwnerHelperV2) handleCommit(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	helper.stateMu.Lock()
	valid := !helper.ownerLost && !helper.stopRequested && !helper.stopAttempted && helper.prepareResponse != nil &&
		helper.prepared != nil && helper.gate != nil && helper.launcher != nil && helper.launcherExit != nil &&
		request.Gate != nil && request.Gate.Equal(*helper.gate) && request.Launcher != nil && request.Launcher.Equal(*helper.launcher) &&
		request.LauncherExit != nil && request.LauncherExit.Equal(*helper.launcherExit)
	if valid && helper.commitAcknowledged {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "commit", "already_committed", nil, nil, nil, nil, nil, 0, nil, "")
	}
	if valid {
		helper.commitAcknowledged = true
	}
	helper.stateMu.Unlock()
	if !valid {
		return helper.writeResponse(connection, "commit", "rejected", nil, nil, nil, nil, nil, 0, nil, "durable_handoff_ack_mismatch")
	}
	return helper.writeResponse(connection, "commit", "committed", nil, nil, nil, nil, nil, 0, nil, "")
}

func (helper *linuxOwnerHelperV2) handleRenewLease(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	helper.stateMu.Lock()
	allowed := helper.commitAcknowledged && !helper.ownerLost && !helper.stopRequested && !helper.stopAttempted &&
		(request.Sequence == 1 || helper.released)
	if !allowed || request.LeaseNanos <= 0 {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "renew-lease", "rejected", nil, nil, nil, nil, nil, 0, nil, "commit_or_lease_state_invalid")
	}
	replay, err := helper.lease.renew(time.Duration(request.LeaseNanos), request.Sequence)
	if err != nil {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "renew-lease", "rejected", nil, nil, nil, nil, nil, 0, nil, "lease_sequence_or_deadline_invalid")
	}
	sequence, _ := helper.lease.snapshot()
	helper.stateMu.Unlock()
	status := "acknowledged"
	if replay {
		status = "already_acknowledged"
	}
	return helper.writeResponse(connection, "renew-lease", status, nil, nil, nil, nil, nil, sequence, nil, "")
}

func (helper *linuxOwnerHelperV2) handleRelease(connection *net.UnixConn, request linuxOwnerHelperV2Request, rights []int) error {
	if len(rights) != 3 || request.Command == nil || (request.Command.Clone == nil && request.Command.Reuse == nil) || request.Helper != nil ||
		request.Launcher != nil || request.Gate != nil || request.LauncherExit != nil || request.Sequence != 0 ||
		request.LeaseNanos != 0 || request.Accepted || request.CheckoutReceipt != nil || request.Receipt != nil ||
		request.PreGateReceipt != nil || request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil {
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "stdio_or_command_invalid")
	}
	files := [3]*os.File{}
	fileNames := [3]string{"symmetry-owner-stdin-read", "symmetry-owner-stdout-write", "symmetry-owner-stderr-write"}
	for index, fd := range rights {
		duplicate, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			closeLinuxOwnerHelperFiles(files[:]...)
			return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "stdio_duplicate_failed")
		}
		files[index] = os.NewFile(uintptr(duplicate), fileNames[index])
		if files[index] == nil {
			_ = unix.Close(duplicate)
			closeLinuxOwnerHelperFiles(files[:]...)
			return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "stdio_wrap_failed")
		}
	}
	defer closeLinuxOwnerHelperFiles(files[:]...)
	command := linuxgate.NativeCommand{
		Path: request.Command.Path, Args: append([]string(nil), request.Command.Args...),
		Env: append([]string(nil), request.Command.Env...), Dir: request.Command.Dir,
		Stdin: files[0], Stdout: files[1], Stderr: files[2],
	}
	var cloneGrant *authority.LinuxCloneGrant
	if request.Command.Clone != nil {
		copy := request.Command.Clone.Clone()
		cloneGrant = &copy
		command.Clone = cloneGrant
	}
	if request.Command.Reuse != nil {
		copy := request.Command.Reuse.Clone()
		command.Reuse = &copy
	}
	if !linuxOwnerHelperV2CloneGrantMatchesRun(command, helper.grant, command.Dir) {
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "clone_grant_mismatch")
	}
	if err := command.Validate(); err != nil {
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "native_command_invalid")
	}
	if command.Reuse != nil {
		if err := verifyLinuxCloneReuseTarget(*command.Reuse, helper.owner); err != nil {
			return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "reuse_target_mismatch")
		}
	}
	helper.stateMu.Lock()
	sequence, leaseDeadline, leaseLive := helper.lease.releaseGuard()
	if helper.ownerLost {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, sequence, nil, "owner_lost")
	}
	if !helper.commitAcknowledged {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, sequence, nil, "handoff_not_committed")
	}
	if !leaseLive {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, sequence, nil, "initial_lease_not_acknowledged")
	}
	if helper.stopRequested || helper.stopAttempted || helper.releaseAttempted || helper.prepared == nil || helper.gate == nil {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, sequence, nil, "release_closed_or_used")
	}
	helper.releaseAttempted = true
	helper.releaseInFlight = true
	helper.releaseDone = make(chan struct{})
	releaseDone := helper.releaseDone
	releaseContext, cancelRelease := context.WithTimeout(context.Background(), linuxOwnerHelperV2Timeout)
	helper.releaseCancel = cancelRelease
	prepared := helper.prepared
	gateProcess := helper.gate.Process
	gateIdentity := cloneLinuxOwnerGateIdentity(helper.gate)
	var gateUserNamespace *LinuxUserNamespaceIdentity
	if helper.gateUserNamespace != nil {
		copy := *helper.gateUserNamespace
		gateUserNamespace = &copy
	}
	ownerPipe := helper.ownerPipe
	helper.stateMu.Unlock()
	var onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error
	if cloneGrant != nil {
		onCheckoutReady = func(callbackContext context.Context, receipt authority.LinuxCloneCheckoutReceipt) error {
			if !linuxOwnerHelperV2CheckoutReceiptMatches(receipt, *cloneGrant) {
				return errors.New("Linux gate checkout receipt differs from the exact clone grant")
			}
			return helper.relayCheckoutReady(callbackContext, connection, request, *cloneGrant, receipt)
		}
	}
	var err error
	if gateIdentity == nil || gateUserNamespace == nil {
		err = errors.New("Linux owner helper lacks the exact registered gate/user namespace for release authorization")
	} else {
		err = helper.anchorClient.AuthorizeGateRelease(releaseContext, helper.grant, helper.helper, *gateIdentity, *gateUserNamespace)
	}
	if err == nil {
		sequence, leaseDeadline, leaseLive = helper.lease.releaseGuard()
		if !leaseLive {
			err = errors.New("Linux owner helper lease expired before gate RELEASE")
		}
	}
	if err == nil {
		err = prepared.Release(releaseContext, int(helper.leaf.leafFD), command, linuxgate.ReleaseGuard{
			OwnerPipe: ownerPipe, LeaseDeadline: leaseDeadline,
		}, onCheckoutReady)
	}
	helper.stateMu.Lock()
	helper.releaseCancel = nil
	witnessAction := ""
	if err == nil {
		helper.released = true
		if helper.witnessHelperAction != "" {
			if helper.witnessHelperAction != linuxOwnerHelperV2WitnessCrashAfterRelease ||
				helper.witnessRunKey != helper.grant.RunKey || !helper.witnessHoldForReleaseFile {
				err = errors.New("Linux owner helper post-RELEASE witness action differs from its exact RunKey contract")
			} else {
				witnessAction = helper.witnessHelperAction
				helper.witnessAction = witnessAction
				helper.witnessActionObserved = true
			}
		}
	}
	helper.stateMu.Unlock()
	cancelRelease()
	if witnessAction != "" {
		if err := helper.waitForWitnessSelectorUnlink(); err != nil {
			return helper.exitAtWitnessAction(fmt.Errorf("wait for root to unlink the exact post-RELEASE selector: %w", err))
		}
		return helper.exitAtWitnessAction(nil)
	}
	if err == nil {
		registerContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorRequestTimeout)
		registerErr := helper.anchorClient.RegisterProcess(registerContext, helper.grant, helper.helper, helper.helper, gateProcess, "native", int(prepared.PIDFD().Fd()))
		cancel()
		if registerErr != nil {
			err = fmt.Errorf("register same-PID B exec with owner anchor: %w", registerErr)
		} else if _, waitErr := helper.ensureGateWait(); waitErr != nil {
			err = fmt.Errorf("start exact helper-owned gate waiter: %w", waitErr)
		}
	}
	helper.stateMu.Lock()
	helper.releaseInFlight = false
	close(releaseDone)
	sequence, leaseDeadline, leaseLive = helper.lease.releaseGuard()
	ownerLost := helper.ownerLost
	stopRequested := helper.stopRequested || helper.stopAttempted
	helper.stateMu.Unlock()
	if err != nil || ownerLost || stopRequested || !leaseLive {
		ctx, cancel := context.WithTimeout(context.Background(), linuxOwnerHelperV2StopTimeout)
		stopEvidence, stopErr := helper.stopOwned(ctx)
		cancel()
		code := "release_outcome_unknown"
		if ownerLost {
			code = "owner_lost_during_release"
		} else if !leaseLive {
			code = "lease_expired_during_release"
		}
		var evidence *LinuxOwnerHelperV2StopEvidence
		if stopErr == nil {
			copy := stopEvidence
			evidence = &copy
		}
		responseErr := helper.writeResponse(connection, "release", "unresolved", nil, nil, nil, nil, nil, sequence, evidence, code)
		if stopErr != nil {
			helper.requestAnchorTakeover()
		}
		return responseErr
	}
	return helper.writeResponse(connection, "release", "released", nil, nil, nil, nil, nil, sequence, nil, "")
}
