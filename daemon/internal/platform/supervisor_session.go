//go:build linux

package platform

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

// linuxSupervisorSession carries the supervisor helper state that outlives a
// single loop iteration. The helper loop builds it, the stop path and the
// recovery endpoint keep operating on the same values, and the pre-hello
// recovery path builds a deliberately minimal value because no handshake
// state exists yet.
type linuxSupervisorSession struct {
	launch              linuxSupervisorLaunch
	listener            *net.UnixListener
	target              *LinuxPtraceProcess
	group               *processGroup
	ownerFile           *os.File
	controlFile         *os.File
	responseFile        *os.File
	statusFile          *os.File
	stdinFile           *os.File
	stdoutFile          *os.File
	stderrFile          *os.File
	ownerRecoveryMarker linuxSupervisorOwnerRecoveryAuthenticatedMarker

	bound                         bool
	committed                     bool
	resumed                       bool
	receipt                       *authority.StopReceipt
	boundSecret                   string
	ownerLostLatched              bool
	ownerLossProofBarrier         linuxSupervisorOwnerLossProofBarrier
	ownerLossProofBarrierEnabled  bool
	ownerLossProofBarrierStarted  bool
	ownerLossProofBarrierReleased bool
	ownerLossProofBarrierErr      error
	ownerLossProofResultWritten   bool
	ownerLossProofResultCategory  string
	waitDone                      chan struct{}
	waitOnce                      sync.Once
	leaseState                    *linuxSupervisorLeaseState
	watchdog                      *linuxSupervisorOwnerWatchdog

	ownerLost        <-chan struct{}
	stop             func() error
	allowAbort       bool
	scanSealSequence func() uint64
}

func newLinuxSupervisorSession(launch linuxSupervisorLaunch, listener *net.UnixListener, target *LinuxPtraceProcess, group *processGroup, ownerFile, controlFile, responseFile, statusFile, stdinFile, stdoutFile, stderrFile *os.File, ownerRecoveryMarker linuxSupervisorOwnerRecoveryAuthenticatedMarker) *linuxSupervisorSession {
	session := &linuxSupervisorSession{
		launch:              launch,
		listener:            listener,
		target:              target,
		group:               group,
		ownerFile:           ownerFile,
		controlFile:         controlFile,
		responseFile:        responseFile,
		statusFile:          statusFile,
		stdinFile:           stdinFile,
		stdoutFile:          stdoutFile,
		stderrFile:          stderrFile,
		ownerRecoveryMarker: ownerRecoveryMarker,
	}
	session.stop = session.runStop
	return session
}

// newLinuxSupervisorPreHelloRecoverySession reproduces the four pre-hello call
// sites of the recovery endpoint: the target never reached hello, so the stop
// receipt is nil, the bound secret is empty, the owner-loss channel is nil,
// allowAbort is true, the descendant scan seal sequence is nil, and the owner
// loss proof result category is empty.
func newLinuxSupervisorPreHelloRecoverySession(listener *net.UnixListener, launch linuxSupervisorLaunch, target *LinuxPtraceProcess, stop func() error, ownerRecoveryMarker linuxSupervisorOwnerRecoveryAuthenticatedMarker) *linuxSupervisorSession {
	return &linuxSupervisorSession{
		listener:            listener,
		launch:              launch,
		target:              target,
		stop:                stop,
		ownerRecoveryMarker: ownerRecoveryMarker,
		allowAbort:          true,
	}
}

func (session *linuxSupervisorSession) runHelperLoop() error {
	ownerLossProofBarrier, ownerLossProofBarrierEnabled, err := linuxSupervisorOwnerLossProofBarrierFromEnv()
	if err != nil {
		return err
	}
	session.ownerLossProofBarrier = ownerLossProofBarrier
	session.ownerLossProofBarrierEnabled = ownerLossProofBarrierEnabled
	session.leaseState = newLinuxSupervisorLeaseState()
	defer session.leaseState.stop()
	session.watchdog = newLinuxSupervisorOwnerWatchdog(session.ownerFile, session.leaseState.latchOwnerLost)
	defer session.watchdog.disarm()
	session.waitDone = make(chan struct{})
	type requestEvent struct {
		request linuxSupervisorRequest
		err     error
	}
	requestEvents := make(chan requestEvent, 1)
	go func() {
		reader := bufio.NewReader(session.controlFile)
		for {
			var request linuxSupervisorRequest
			err := readLinuxSupervisorFrame(reader, &request)
			requestEvents <- requestEvent{request: request, err: err}
			if err != nil {
				return
			}
		}
	}()
	session.ownerLost = session.watchdog.ownerLost()
	for {
		select {
		case <-session.ownerLost:
			session.ownerLost = nil
			session.ownerLostLatched = true
			return session.enterRecovery()
		case <-session.leaseState.wakeup():
			if !session.leaseState.takeExpiry() {
				continue
			}
			if err := session.stop(); err != nil {
				// Lease expiry does not prove that the owner process is dead. Keep
				// the authenticated control channel available so the live owner can
				// retry the physical stop; control EOF/owner loss will migrate to
				// the recovery endpoint below.
				continue
			}
			// The target is reaped and the receipt is durable. Continue serving
			// the original pipe so the owner can release it without a channel
			// migration race.
			continue
		case event := <-requestEvents:
			if event.err != nil {
				// Control EOF/error is itself an owner-loss boundary. The owner
				// watchdog may publish its notification later, so never return
				// the raw protocol error while target authority is still active.
				return session.enterRecovery()
			}
			request := event.request
			if !session.bound && request.Operation == linuxSupervisorOpAbort && linuxSupervisorUnboundAbortRequestMatches(request, session.launch, session.target) {
				if err := session.stop(); err != nil {
					if writeErr := writeLinuxSupervisorResponseV2(session.responseFile, request, "error", session.target, session.launch, 0, err.Error()); writeErr != nil {
						return session.enterRecovery()
					}
					continue
				}
				if err := writeLinuxSupervisorResponseV2(session.responseFile, request, "aborted", session.target, session.launch, 0, ""); err != nil {
					return session.enterRecovery()
				}
				return nil
			}
			if request.Operation == linuxSupervisorOpBind {
				if !linuxSupervisorRequestMatchesV2(request, session.launch, session.target) || request.Secret == "" || !linuxSupervisorSecretMatchesV2(request.Secret, session.launch.SecretDigest) {
					continue
				}
				status, message := "bound", ""
				if !session.bound {
					session.bound = true
					session.boundSecret = request.Secret
					session.group.startDescendantMonitor()
					if err := session.group.waitForInitialDescendantScan(time.Now().Add(containmentCloseDeadline)); err != nil {
						status, message = "error", err.Error()
					}
				}
				if err := writeLinuxSupervisorResponseV2(session.responseFile, request, status, session.target, session.launch, 0, message); err != nil {
					return session.enterRecovery()
				}
				continue
			}
			if !session.bound {
				if request.Operation == linuxSupervisorOpAbort || request.Operation == linuxSupervisorOpStop || request.Operation == linuxSupervisorOpClose {
					if !linuxSupervisorRequestMatchesV2(request, session.launch, session.target) || request.Secret == "" || !linuxSupervisorSecretMatchesV2(request.Secret, session.launch.SecretDigest) {
						continue
					}
					if err := session.stop(); err != nil {
						if writeErr := writeLinuxSupervisorResponseV2(session.responseFile, request, "error", session.target, session.launch, 0, err.Error()); writeErr != nil {
							return session.enterRecovery()
						}
						continue
					}
					if err := writeLinuxSupervisorResponseV2(session.responseFile, request, "stopped", session.target, session.launch, 0, ""); err != nil {
						return session.enterRecovery()
					}
					return nil
				}
				continue
			}
			if !linuxSupervisorRequestMatchesV2(request, session.launch, session.target) || request.Secret == "" || !linuxSupervisorSecretMatchesV2(request.Secret, session.launch.SecretDigest) {
				continue
			}
			if request.Operation == linuxSupervisorOpAbort && !session.committed && !session.resumed {
				status, message := "aborted", ""
				if err := session.stop(); err != nil {
					status, message = "error", err.Error()
				}
				if err := writeLinuxSupervisorResponseV2(session.responseFile, request, status, session.target, session.launch, 0, message); err != nil {
					return session.enterRecovery()
				}
				if status == "aborted" {
					return nil
				}
				continue
			}
			status := "ok"
			message := ""
			needsRecovery := false
			switch request.Operation {
			case linuxSupervisorOpCommit:
				session.committed = true
				status = "committed"
			case linuxSupervisorOpResume:
				if !session.committed {
					status, message = "error", ErrLinuxSupervisorNotCommitted.Error()
				} else if session.group.descendantObservationUnproven() {
					status, message = "error", ErrLinuxDescendantContainmentUnproven.Error()
				} else if session.receipt != nil {
					status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
				} else if err := session.leaseState.resume(func() bool { return channelClosedV2(session.watchdog.ownerLost()) }, session.target.Resume); err != nil {
					status, message = "error", err.Error()
				} else {
					session.resumed = true
					if err := session.group.resumeDescendantMonitoring(); err != nil {
						status, message = "error", err.Error()
						needsRecovery = true
					} else {
						session.startWait()
						status = "resumed"
					}
				}
			case linuxSupervisorOpRenew:
				replay, err := session.leaseState.renew(time.Duration(request.DeadlineMS)*time.Millisecond, request.Sequence)
				if err != nil {
					status, message = "error", err.Error()
				} else if replay {
					status = "already_renewed"
				} else {
					status = "renewed"
				}
			case linuxSupervisorOpStop, linuxSupervisorOpClose, linuxSupervisorOpAbort:
				if err := session.stop(); err != nil {
					status, message = "error", err.Error()
					needsRecovery = true
				} else {
					status = "stopped"
				}
			case linuxSupervisorOpRelease:
				if session.receipt == nil {
					status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
				} else {
					status = "released"
				}
			default:
				status, message = "error", "unknown Linux supervisor operation"
			}
			if err := writeLinuxSupervisorResponseV2(session.responseFile, request, status, session.target, session.launch, 0, message); err != nil {
				return session.enterRecovery()
			}
			if needsRecovery {
				// The error response reached the live daemon. Keep servicing the
				// original control channel so it can retry the physical proof;
				// switch to the recovery endpoint only when that channel itself is
				// lost or a response write fails.
				continue
			}
			if request.Operation == linuxSupervisorOpRelease && status == "released" {
				return nil
			}
		}
	}
}

func (session *linuxSupervisorSession) startWait() {
	session.waitOnce.Do(func() {
		go func() {
			code, waitErr := session.target.Wait()
			closeLinuxSupervisorFilesV2(session.stdinFile, session.stdoutFile, session.stderrFile)
			response := linuxSupervisorResponse{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpWait, Status: "exited", TargetPID: session.target.PID(), TargetIdentity: session.target.Identity(), ExitCode: code}
			if waitErr != nil {
				response.ExitError = waitErr.Error()
			}
			_ = writeLinuxSupervisorFrame(session.statusFile, response)
			close(session.waitDone)
		}()
	})
}

func (session *linuxSupervisorSession) runStop() error {
	deadline := containmentDeadline(context.Background())
	session.leaseState.stop()
	if session.receipt != nil {
		return nil
	}
	ownerLostStop := session.ownerLostLatched || channelClosedV2(session.watchdog.ownerLost())
	precommitAbort := !session.committed && !session.resumed
	var ownerLossScanErr error
	if ownerLostStop && (session.bound || session.resumed) && !precommitAbort {
		ownerLossScanErr = session.group.prepareDescendantMonitorForOwnerLoss(deadline)
	}
	var terminateErr error
	if !session.resumed {
		terminateErr = session.target.Terminate()
		if terminateErr != nil {
			if retryErr := session.target.Terminate(); retryErr != nil {
				return errors.Join(terminateErr, retryErr, session.group.Close())
			}
		}
	}
	// Signal the exact process group before reaping the leader. Reaping first
	// can make a same-group descendant unreachable from the leader fence.
	if err := session.group.Terminate(true); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	session.group.mutex.Lock()
	session.group.skipNextCloseSignal = true
	session.group.mutex.Unlock()
	session.startWait()
	if err := waitForLinuxSupervisorTarget(session.waitDone, deadline); err != nil {
		return err
	}
	if err := reapLinuxProcessGroupChildren(session.group.anchor, deadline); err != nil {
		return err
	}
	proofDeadline := deadline
	if ownerLostStop && !precommitAbort && session.ownerLossProofBarrierEnabled {
		if !session.ownerLossProofBarrierStarted {
			session.ownerLossProofBarrierStarted = true
			session.ownerLossProofBarrierErr = waitForLinuxSupervisorOwnerLossProofBarrier(session.ownerLossProofBarrier)
			session.ownerLossProofBarrierReleased = session.ownerLossProofBarrierErr == nil
		}
		if !session.ownerLossProofBarrierReleased {
			return errors.Join(ownerLossScanErr, session.ownerLossProofBarrierErr)
		}
		// The bounded stop deadline above covered signal, target wait, and
		// same-group reaping. The witness pause must not consume the close
		// proof's independent bounded deadline.
		proofDeadline = time.Now().Add(containmentCloseDeadline)
	}
	var closeErr error
	if precommitAbort {
		closeErr = session.group.closePreparedAbort(proofDeadline)
	} else {
		closeErr = session.group.closeUntil(proofDeadline)
	}
	if ownerLostStop && !precommitAbort && session.ownerLossProofBarrierReleased && !session.ownerLossProofResultWritten {
		session.ownerLossProofResultWritten = true
		proofErr := errors.Join(ownerLossScanErr, closeErr)
		if resultErr := writeLinuxSupervisorOwnerLossProofResult(session.ownerLossProofBarrier, proofErr); resultErr != nil {
			closeErr = errors.Join(closeErr, resultErr)
		} else {
			session.ownerLossProofResultCategory = linuxSupervisorOwnerLossProofResultCategory(proofErr)
		}
	}
	if closeErr != nil {
		return errors.Join(ownerLossScanErr, closeErr)
	}
	if ownerLossScanErr != nil {
		return ownerLossScanErr
	}
	if precommitAbort {
		// The target remained exec-stopped because commit never crossed the
		// helper. Abort is proved by exact helper/target/group absence, not by
		// inventing a scan receipt for an execution that never became runnable.
		return nil
	}
	var scanErr error
	if ownerLostStop {
		scanErr = session.group.assertDescendantScanSealedLocally()
	} else {
		scanErr = session.group.assertDescendantScanSealed()
	}
	if scanErr != nil {
		return scanErr
	}
	receiptValue := authority.StopReceipt{Version: authority.SupervisorVersion, Status: "stopped", OwnerKind: linuxSupervisorOwnerKind, OwnerContext: session.launch.OwnerContext, TargetPID: session.target.PID(), TargetIdentity: session.target.Identity(), PipeToken: session.launch.PipeToken, JobID: session.launch.JobID, SupervisorPID: os.Getpid(), SupervisorIdentity: mustLinuxSupervisorIdentity(), ActiveProcesses: 0}
	session.receipt = &receiptValue
	return nil
}

func (session *linuxSupervisorSession) enterRecovery() error {
	// Keep the authenticated endpoint alive whether stop succeeds now or
	// still needs a retry through recovery.
	session.ownerLostLatched = session.ownerLostLatched || channelClosedV2(session.watchdog.ownerLost())
	_ = session.stop()
	session.ownerLost = session.watchdog.ownerLost()
	session.allowAbort = !session.committed && !session.resumed
	session.scanSealSequence = session.group.descendantScanSealSequence
	return session.runRecoveryLoop()
}

func (session *linuxSupervisorSession) runRecoveryLoop() error {
	ownerLossHandled := false
	ownerRecoveryMarkerPublished := false
	for {
		if !ownerLossHandled && channelClosedV2(session.ownerLost) {
			ownerLossHandled = true
			if session.stop != nil {
				_ = session.stop()
			}
		}
		if session.ownerLost != nil {
			_ = session.listener.SetDeadline(time.Now().Add(100 * time.Millisecond))
		}
		connection, err := session.listener.AcceptUnix()
		if err != nil {
			err = sanitizeLinuxSupervisorEndpointError(err)
			if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
				_ = session.listener.SetDeadline(time.Time{})
				continue
			}
			return err
		}
		_ = session.listener.SetDeadline(time.Time{})
		if err := connection.SetDeadline(time.Now().Add(linuxSupervisorTimeout)); err != nil {
			_ = connection.Close()
			continue
		}
		reader := bufio.NewReader(connection)
		var request linuxSupervisorRequest
		if err := readLinuxSupervisorFrame(reader, &request); err != nil {
			_ = connection.Close()
			continue
		}
		// A structurally exact but unsupported operation must not fall through
		// to the default stopped response.
		recoveryOperationSupported := request.Operation == linuxSupervisorOpAbort || request.Operation == linuxSupervisorOpStop || request.Operation == linuxSupervisorOpClose || request.Operation == linuxSupervisorOpRelease
		unboundAbort := recoveryOperationSupported && session.boundSecret == "" && request.Operation == linuxSupervisorOpAbort && request.SupervisorPID == 0 && request.SupervisorIdentity == ""
		valid := false
		if unboundAbort {
			valid = linuxSupervisorUnboundAbortRequestMatches(request, session.launch, session.target)
		} else if recoveryOperationSupported {
			valid = linuxSupervisorRequestMatchesV2(request, session.launch, session.target) && request.Secret != ""
			if session.boundSecret != "" {
				valid = valid && request.Secret == session.boundSecret
			} else {
				valid = valid && linuxSupervisorSecretMatchesV2(request.Secret, session.launch.SecretDigest)
			}
		}
		if valid && session.ownerRecoveryMarker.path != "" && !ownerRecoveryMarkerPublished {
			if err := writeLinuxSupervisorOwnerRecoveryAuthenticatedMarker(session.ownerRecoveryMarker, request.Operation, session.target.PID(), os.Getpid(), session.ownerLossProofResultCategory); err != nil {
				_ = connection.Close()
				return err
			}
			ownerRecoveryMarkerPublished = true
		}
		status, message := "stopped", ""
		if !valid {
			status, message = "error", "Linux supervisor recovery identity mismatch"
		} else if unboundAbort {
			if session.receipt == nil {
				if session.stop == nil {
					status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
				} else if stopErr := session.stop(); stopErr != nil {
					status, message = "error", stopErr.Error()
				} else {
					status = "aborted"
				}
			} else {
				status = "aborted"
			}
		} else if request.Operation == linuxSupervisorOpAbort && session.allowAbort && (session.receipt == nil) {
			if session.stop == nil {
				status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
			} else if stopErr := session.stop(); stopErr != nil {
				status, message = "error", stopErr.Error()
			} else {
				status = "aborted"
			}
		} else if request.Operation == linuxSupervisorOpAbort {
			status, message = "error", "Linux supervisor abort is allowed only before commit"
		} else if (session.receipt == nil) && (request.Operation == linuxSupervisorOpStop || request.Operation == linuxSupervisorOpClose) {
			if session.stop == nil {
				status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
			} else if stopErr := session.stop(); stopErr != nil {
				status, message = "error", stopErr.Error()
			} else {
				status = "stopped"
			}
		} else if session.receipt == nil {
			status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
		} else if request.Operation == linuxSupervisorOpRelease {
			status = "released"
		}
		identity := mustLinuxSupervisorIdentity()
		sealSequence := uint64(0)
		if status == "stopped" && session.scanSealSequence != nil {
			sealSequence = session.scanSealSequence()
		}
		response := linuxSupervisorRecoveryResponse{
			linuxSupervisorResponse: linuxSupervisorResponse{
				Version: linuxSupervisorProtocolVersion, Operation: request.Operation, Status: status,
				LaunchToken: session.launch.LaunchToken, Sequence: request.Sequence,
				TargetPID: session.target.PID(), TargetIdentity: session.target.Identity(), OwnerKind: linuxSupervisorOwnerKind,
				OwnerContext: session.launch.OwnerContext, SupervisorPID: os.Getpid(), SupervisorIdentity: identity,
				Token: session.launch.PipeToken, JobID: session.launch.JobID, ActiveProcesses: 0, Error: message,
			},
			ScanSealSequence: sealSequence,
		}
		if err := writeLinuxSupervisorFrame(connection, response); err != nil {
			_ = connection.Close()
			continue
		}
		_ = connection.Close()
		if status == "aborted" {
			return nil
		}
		if status == "released" {
			return nil
		}
	}
}
