//go:build linux && amd64

package platform

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

type anchorConnection struct {
	anchor      *linuxOwnerAnchor
	connection  *net.UnixConn
	peer        linuxOwnerAnchorPeer
	request     linuxOwnerAnchorRequest
	row         linuxOwnerAnchorOperationShape
	receivedFDs []int
	helperPeer  bool
}

func (session *anchorConnection) handleIdentity() (detached bool) {
	if linuxOwnerCgroupPathsOverlap(session.peer.CgroupPath, session.anchor.managerPath) {
		return false
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), session.anchor.managerPath); err != nil {
		return false
	}
	if err := session.anchor.parent.verifyPinnedParent(); err != nil {
		return false
	}
	session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "ready"))
	return false
}

func (session *anchorConnection) handleRetireRequest() (detached bool) {
	if linuxOwnerCgroupPathsOverlap(session.peer.CgroupPath, session.anchor.managerPath) {
		return false
	}
	if err := session.connection.SetDeadline(time.Now().Add(session.row.timeoutClass.duration())); err != nil {
		return false
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), session.anchor.managerPath); err != nil {
		return false
	}
	if err := session.anchor.parent.verifyPinnedParent(); err != nil {
		return false
	}
	if session.request.Operation == linuxOwnerAnchorOperationRetireCleared {
		session.anchor.handleRetireClearedRun(session.connection, session.request)
	} else {
		session.anchor.handleRetireAbortedHelper(session.connection, session.request)
	}
	return false
}

func (session *anchorConnection) handleCloneCleanup() (detached bool) {
	return session.anchor.serveCloneCleanup(session.connection, session.peer, session.request, session.receivedFDs)
}

func (session *anchorConnection) authorizeRunRequest() bool {
	if session.request.Grant.Validate() != nil || session.request.Grant.OwnerUID != session.anchor.owner.AUID || session.request.Grant.LauncherUID != 0 ||
		!session.request.Grant.Anchor.Equal(session.anchor.identity) ||
		!session.request.Grant.Parent.Equal(session.anchor.parent.Identity()) {
		return false
	}
	if session.row.runHelperPeer && session.request.Helper != nil {
		run := session.anchor.runs[session.request.Grant.RunKey]
		session.helperPeer = run != nil && run.grant.Equal(session.request.Grant) && run.helper.Equal(*session.request.Helper) &&
			session.peer.PID == run.helper.PID && session.peer.Identity == run.helper.Identity
	}
	if linuxOwnerCgroupPathsOverlap(session.peer.CgroupPath, session.anchor.managerPath) && !session.helperPeer {
		return false
	}
	if err := session.connection.SetDeadline(time.Now().Add(session.row.timeoutClass.duration())); err != nil {
		return false
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), session.anchor.managerPath); err != nil {
		return false
	}
	if err := session.anchor.parent.verifyPinnedParent(); err != nil {
		return false
	}
	return true
}

func (session *anchorConnection) handleRunOperation() bool {
	if !session.authorizeRunRequest() {
		return false
	}
	switch session.request.Operation {
	case linuxOwnerAnchorOperationStart:
		return session.handleStart()
	case linuxOwnerAnchorOperationAttach:
		return session.handleAttach()
	case linuxOwnerAnchorOperationLookup:
		return session.handleLookup()
	case linuxOwnerAnchorOperationRegisterProcess:
		return session.handleRegisterProcess()
	case linuxOwnerAnchorOperationClassifyChild:
		return session.handleClassifyChild()
	case linuxOwnerAnchorOperationRegisterGate:
		return session.handleRegisterGate()
	case linuxOwnerAnchorOperationAuthorizeGate:
		return session.handleAuthorizeGate()
	case linuxOwnerAnchorOperationStopLeaf:
		return session.handleStopLeaf()
	case linuxOwnerAnchorOperationWaitReap:
		return session.handleWaitReap()
	case linuxOwnerAnchorOperationReapRun:
		return session.handleReapRun()
	case linuxOwnerAnchorOperationWaitHelper:
		return session.handleWaitHelper()
	case linuxOwnerAnchorOperationRetire:
		session.anchor.handleRetireHelper(session.connection, session.request)
	}
	return false
}

func (session *anchorConnection) handleStart() (detached bool) {
	if err := verifyLinuxOwnerAnchorLeafFD(session.receivedFDs[0], session.request.Grant.Leaf, session.anchor.owner.AUID); err != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "request_rejected"))
		return false
	}
	if err := verifyLinuxOwnerAnchorOwnerPipeReadFD(session.receivedFDs[1]); err != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "request_rejected"))
		return false
	}
	if existing, exists := session.anchor.runs[session.request.Grant.RunKey]; exists {
		if existing == nil || !existing.grant.Equal(session.request.Grant) {
			session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unresolved"))
			return false
		}
		response := session.anchor.baseResponse(session.request, "already_started")
		response.Helper = &existing.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	var witnessForRun *linuxOwnerAnchorWitnessConfig
	if session.anchor.witness != nil && session.anchor.witness.RunID == session.request.Grant.RunKey.RunID &&
		session.anchor.witness.Generation == session.request.Grant.RunKey.Generation {
		copy := *session.anchor.witness
		witnessForRun = &copy
	}
	ownerProcess := authority.LinuxCgroupProcessIdentity{PID: session.peer.PID, Identity: session.peer.Identity}
	beforeFork := func() error { return session.anchor.recordHelperForkMayStart(session.request.Grant.RunKey) }
	run, err := startLinuxOwnerAnchorHelperV2(
		session.anchor, session.request.Grant, ownerProcess, session.receivedFDs[0], session.receivedFDs[1], witnessForRun, beforeFork,
	)
	if run != nil {
		session.anchor.runs[session.request.Grant.RunKey] = run
	}
	if err != nil || run == nil {
		status := "request_rejected"
		if _, forkMayHaveOccurred := session.anchor.runs[session.request.Grant.RunKey]; forkMayHaveOccurred {
			status = "start_unresolved"
		}
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, status))
		return false
	}
	helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		response := session.anchor.baseResponse(session.request, "start_unresolved")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	leafFD, err := run.leaf.DuplicateFD()
	if err != nil {
		_ = unix.Close(helperFD)
		response := session.anchor.baseResponse(session.request, "start_unresolved")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	parentFD, err := session.anchor.parent.DuplicateFD()
	if err != nil {
		_ = unix.Close(helperFD)
		_ = leafFD.Close()
		response := session.anchor.baseResponse(session.request, "start_unresolved")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	response := session.anchor.baseResponse(session.request, "started")
	response.Helper = &run.helper
	session.anchor.writeResponse(session.connection, response, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
	_ = unix.Close(helperFD)
	_ = leafFD.Close()
	_ = parentFD.Close()
	return false
}

func (session *anchorConnection) handleAttach() (detached bool) {
	run, exists := session.anchor.runs[session.request.Grant.RunKey]
	if !exists || run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	if run.retired {
		response := session.anchor.baseResponse(session.request, "retired")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), run.helper); err != nil {
		response := session.anchor.baseResponse(session.request, "helper_lost")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		response := session.anchor.baseResponse(session.request, "attach_unresolved")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	leafFD, err := run.leaf.DuplicateFD()
	if err != nil {
		_ = unix.Close(helperFD)
		response := session.anchor.baseResponse(session.request, "attach_unresolved")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	parentFD, err := session.anchor.parent.DuplicateFD()
	if err != nil {
		_ = unix.Close(helperFD)
		_ = leafFD.Close()
		response := session.anchor.baseResponse(session.request, "attach_unresolved")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	response := session.anchor.baseResponse(session.request, "attached")
	response.Helper = &run.helper
	session.anchor.writeResponse(session.connection, response, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
	_ = unix.Close(helperFD)
	_ = leafFD.Close()
	_ = parentFD.Close()
	return false
}

func (session *anchorConnection) handleLookup() (detached bool) {
	run, exists := session.anchor.runs[session.request.Grant.RunKey]
	if !exists || run == nil || !run.grant.Equal(session.request.Grant) || run.helper.Validate() != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	if run.retired {
		response := session.anchor.baseResponse(session.request, "retired")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	helperExited := run.helperExit != nil
	if !helperExited {
		if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), run.helper); err != nil {
			waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
			waitErr := run.waitHelper(waitContext)
			cancel()
			if waitErr != nil {
				response := session.anchor.baseResponse(session.request, "wait_unproven")
				response.Helper = &run.helper
				session.anchor.writeResponse(session.connection, response)
				return false
			}
			helperExited = true
		}
	}
	leafFD, err := run.leaf.DuplicateFD()
	if err != nil {
		response := session.anchor.baseResponse(session.request, "lookup_unresolved")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	parentFD, err := session.anchor.parent.DuplicateFD()
	if err != nil {
		_ = leafFD.Close()
		response := session.anchor.baseResponse(session.request, "lookup_unresolved")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	if helperExited {
		response := session.anchor.baseResponse(session.request, "helper_exited")
		response.Helper = &run.helper
		response.HelperExit = run.helperExit
		session.anchor.writeResponse(session.connection, response, int(leafFD.Fd()), int(parentFD.Fd()))
	} else {
		helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			_ = leafFD.Close()
			_ = parentFD.Close()
			response := session.anchor.baseResponse(session.request, "lookup_unresolved")
			response.Helper = &run.helper
			session.anchor.writeResponse(session.connection, response)
			return false
		}
		response := session.anchor.baseResponse(session.request, "attached")
		response.Helper = &run.helper
		session.anchor.writeResponse(session.connection, response, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
		_ = unix.Close(helperFD)
	}
	_ = leafFD.Close()
	_ = parentFD.Close()
	return false
}

func (session *anchorConnection) handleRegisterProcess() (detached bool) {
	run := session.anchor.runs[session.request.Grant.RunKey]
	if run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	if err := run.registerChild(*session.request.Process, *session.request.Creator, session.request.Role, session.receivedFDs[0]); err != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "request_rejected"))
		return false
	}
	session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "registered"))
	return false
}

func (session *anchorConnection) handleClassifyChild() (detached bool) {
	if !session.helperPeer {
		return false
	}
	run := session.anchor.runs[session.request.Grant.RunKey]
	if run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(session.receivedFDs[0], *session.request.Process); err != nil {
		unproven := linuxOwnerAnchorUnknownChildClassification(run, *session.request.Process)
		response := session.anchor.baseResponse(session.request, "origin_unproven")
		response.Helper = session.request.Helper
		response.ChildClassification = &unproven
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	credentials, err := readLinuxOwnerProcessCredentials(session.request.Process.PID)
	if err != nil || credentials.Parent != run.helper.PID {
		run.migrationObserved = true
		unproven := linuxOwnerAnchorUnknownChildClassification(run, *session.request.Process)
		response := session.anchor.baseResponse(session.request, "origin_unproven")
		response.Helper = session.request.Helper
		response.ChildClassification = &unproven
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	classifyContext, cancel := context.WithTimeout(context.Background(), session.row.timeoutClass.duration())
	classification, err := session.anchor.classifyRunChildOrigin(classifyContext, run, *session.request.Process, session.receivedFDs[0])
	cancel()
	credentials, parentErr := readLinuxOwnerProcessCredentials(session.request.Process.PID)
	if err != nil || parentErr != nil || credentials.Parent != run.helper.PID {
		run.migrationObserved = true
		unproven := linuxOwnerAnchorUnknownChildClassification(run, *session.request.Process)
		response := session.anchor.baseResponse(session.request, "origin_unproven")
		response.Helper = session.request.Helper
		response.ChildClassification = &unproven
		session.anchor.writeResponse(session.connection, response)
		return false
	}
	response := session.anchor.baseResponse(session.request, "classified")
	response.Helper = session.request.Helper
	response.ChildClassification = &classification
	session.anchor.writeResponse(session.connection, response)
	return false
}

func (session *anchorConnection) handleRegisterGate() (detached bool) {
	run := session.anchor.runs[session.request.Grant.RunKey]
	if run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) || !session.helperPeer {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	for key, other := range session.anchor.runs {
		if key == session.request.Grant.RunKey || other == nil || other.retired || other.gateUserNamespaceFD == nil {
			continue
		}
		otherNamespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(other.gateUserNamespaceFD)
		if err != nil || otherNamespace == *session.request.UserNamespace {
			session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "request_rejected"))
			return false
		}
	}
	if err := run.registerGate(*session.request.Gate, *session.request.UserNamespace, session.receivedFDs[0], session.anchor.userNamespaceFD); err != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "request_rejected"))
		return false
	}
	session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "registered"))
	return false
}

func (session *anchorConnection) handleAuthorizeGate() (detached bool) {
	run := session.anchor.runs[session.request.Grant.RunKey]
	if run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) || !session.helperPeer {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	if err := run.authorizeGateRelease(*session.request.Gate, *session.request.UserNamespace); err != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "request_rejected"))
		return false
	}
	session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "release_authorized"))
	return false
}

func (session *anchorConnection) handleStopLeaf() (detached bool) {
	run := session.anchor.runs[session.request.Grant.RunKey]
	if run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) || !session.helperPeer {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	stopEvidence, err := session.anchor.stopRunLeaf(stopContext, run, session.request.StopFence)
	cancel()
	if err != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "stop_unproven"))
		return false
	}
	response := session.anchor.baseResponse(session.request, "stopped")
	response.StopEvidence = &stopEvidence
	session.anchor.writeResponse(session.connection, response)
	return false
}

func (session *anchorConnection) handleWaitReap() (detached bool) {
	run := session.anchor.runs[session.request.Grant.RunKey]
	if run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	tracked, registered := run.children[session.request.Process.PID]
	if !registered || !tracked.identity.Equal(*session.request.Process) || tracked.role != session.request.Role {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "request_rejected"))
		return false
	}
	waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	if run.helperExit == nil {
		if err := run.waitHelper(waitContext); err != nil {
			cancel()
			session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "wait_unproven"))
			return false
		}
	}
	if err := session.anchor.ensureExitedHelperContained(waitContext, run); err != nil {
		cancel()
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "stop_unproven"))
		return false
	}
	evidence, err := run.reapChild(waitContext, session.anchor.identity, *session.request.Process, session.request.Role)
	cancel()
	if err != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "wait_unproven"))
		return false
	}
	response := session.anchor.baseResponse(session.request, "reaped")
	response.Evidence = &evidence
	session.anchor.writeResponse(session.connection, response)
	return false
}

func (session *anchorConnection) handleReapRun() (detached bool) {
	run, exists := session.anchor.runs[session.request.Grant.RunKey]
	if !exists || run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	if run.helperExit == nil {
		if err := run.waitHelper(waitContext); err != nil {
			cancel()
			session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "wait_unproven"))
			return false
		}
	}
	if err := session.anchor.ensureExitedHelperContained(waitContext, run); err != nil {
		cancel()
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "stop_unproven"))
		return false
	}
	evidence, err := session.anchor.reapRunChildren(waitContext, run)
	if err != nil {
		cancel()
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "wait_unproven"))
		return false
	}
	stopEvidence, err := session.anchor.finalizeRunStopEvidence(waitContext, run, evidence)
	cancel()
	if err != nil {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "stop_unproven"))
		return false
	}
	filtered := make([]LinuxOwnerAnchorReapEvidence, 0, linuxOwnerAnchorReapPageSize)
	for _, item := range evidence {
		if item.Process.PID > session.request.AfterPID && len(filtered) < linuxOwnerAnchorReapPageSize {
			filtered = append(filtered, item)
		}
	}
	more := false
	nextPID := 0
	if len(filtered) > 0 {
		nextPID = filtered[len(filtered)-1].Process.PID
		for _, item := range evidence {
			if item.Process.PID > nextPID {
				more = true
				break
			}
		}
	}
	response := session.anchor.baseResponse(session.request, "reaped")
	response.ReapEvidence = filtered
	response.StopEvidence = &stopEvidence
	response.NextPID = nextPID
	response.More = more
	session.anchor.writeResponse(session.connection, response)
	return false
}

func (session *anchorConnection) handleWaitHelper() (detached bool) {
	run, exists := session.anchor.runs[session.request.Grant.RunKey]
	if !exists || run == nil || !run.grant.Equal(session.request.Grant) || !run.helper.Equal(*session.request.Helper) {
		session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "run_unavailable"))
		return false
	}
	waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	if run.helperExit == nil {
		if err := run.waitHelper(waitContext); err != nil {
			cancel()
			session.anchor.writeResponse(session.connection, session.anchor.baseResponse(session.request, "wait_unproven"))
			return false
		}
	}
	cancel()
	response := session.anchor.baseResponse(session.request, "reaped")
	response.Helper = session.request.Helper
	response.HelperExit = run.helperExit
	session.anchor.writeResponse(session.connection, response)
	return false
}
