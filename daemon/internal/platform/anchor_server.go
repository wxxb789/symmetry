//go:build linux && amd64

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func linuxOwnerAnchorClassifyNamespaceMembership(
	targetRun, originRun LinuxOwnerAnchorRunKey,
	cgroupPath, targetLeaf string,
) linuxOwnerAnchorRunChildOrigin {
	if !validLinuxOwnerAnchorRunID(targetRun.RunID) || targetRun.Generation <= 0 ||
		!validLinuxOwnerAnchorRunID(originRun.RunID) || originRun.Generation <= 0 || cgroupPath == "" || targetLeaf == "" {
		return linuxOwnerAnchorRunChildOriginUnknown
	}
	if originRun != targetRun {
		return linuxOwnerAnchorRunChildOriginOtherUserNSForeign
	}
	if linuxOwnerCgroupPathWithin(cgroupPath, targetLeaf) {
		return linuxOwnerAnchorRunChildOriginGateUserNSCurrent
	}
	return linuxOwnerAnchorRunChildOriginCurrentRunEscaped
}

func cloneLinuxOwnerAnchorExitProof(value *authority.LinuxCgroupExitProof) *authority.LinuxCgroupExitProof {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.HelperExit = nil
	return &cloned
}

type linuxOwnerAnchorOptions struct {
	cgroupMount   string
	delegatedPath string
	socketPath    string
}

// RunLinuxOwnerAnchor is the hidden long-lived owner service entrypoint. It
// verifies the fixed A/B installation, pins the operator-selected cgroup2
// manager subgroup, installs and reads back its own subreaper flag, then
// serves one bounded authenticated request at a time. Shutdown closes only
// local descriptors; it never manufactures a RunJournal receipt.
func RunLinuxOwnerAnchor(args []string, openVerifier LinuxOwnerAnchorCleanupVerifierFactory) (result error) {
	if openVerifier == nil {
		return errors.New("Linux owner anchor requires a State cleanup verifier factory")
	}
	options, err := parseLinuxOwnerAnchorArgs(args)
	if err != nil {
		return err
	}
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		return fmt.Errorf("read fixed Linux owner configuration: %w", err)
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	if uint32(os.Getuid()) != owner.AUID || uint32(os.Geteuid()) != owner.AUID ||
		uint32(os.Getgid()) != owner.AGID || uint32(os.Getegid()) != owner.AGID {
		return errors.New("Linux owner anchor must run with the configured host UID/GID A")
	}
	cleanupVerifier, err := openLinuxOwnerAnchorCleanupVerifierForConfig(owner, openVerifier)
	if err != nil {
		return fmt.Errorf("prepare read-only StateDir clone-cleanup verifier before READY: %w", err)
	}
	if cleanupVerifier != nil {
		defer func() {
			if closeErr := cleanupVerifier.Close(); closeErr != nil {
				result = errors.Join(result, fmt.Errorf("close read-only StateDir clone-cleanup verifier: %w", closeErr))
			}
		}()
	} // strict v1 owner config has no StateDir; retain legacy launch/recovery but never allow clone cleanup.
	witness, err := readLinuxOwnerAnchorWitnessConfig(owner)
	if err != nil {
		return fmt.Errorf("validate root-owned witness selector before READY: %w", err)
	}
	parent, err := OpenLinuxCgroupV2Parent(options.cgroupMount, options.delegatedPath)
	if err != nil {
		return fmt.Errorf("open operator-delegated Linux cgroup2 manager subgroup: %w", err)
	}
	defer parent.Close()
	if parent.OwnerUID() != owner.AUID || parent.Identity().Path == "/" {
		return errors.New("Linux owner anchor delegated parent is not an A-owned manager subgroup")
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), parent.Identity().Path); err != nil {
		return fmt.Errorf("verify Linux owner anchor manager subgroup: %w", err)
	}
	if err := ensureLinuxOwnerAnchorSubreaper(); err != nil {
		return err
	}
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return fmt.Errorf("capture Linux owner anchor process identity: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), anchorIdentity, owner.AUID, owner.AGID, 0, parent.Identity().Path); err != nil {
		return fmt.Errorf("verify Linux owner anchor A credentials and manager membership: %w", err)
	}
	anchorProcess := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	anchorUserNamespaceFD, anchorUserNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(anchorProcess, -1)
	if err != nil {
		return fmt.Errorf("pin Linux owner anchor user namespace before READY: %w", err)
	}
	defer anchorUserNamespaceFD.Close()
	listener, socketIdentity, err := listenLinuxOwnerAnchor(options.socketPath, owner)
	if err != nil {
		return err
	}
	defer unlinkLinuxOwnerAnchorSocket(options.socketPath, socketIdentity, owner.AUID)
	anchor := &linuxOwnerAnchor{
		owner: owner, parent: parent, listener: listener,
		identity:        authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity},
		userNamespaceFD: anchorUserNamespaceFD, userNamespace: anchorUserNamespace,
		managerPath: parent.Identity().Path, socketPath: options.socketPath,
		runs: make(map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun), cleanupVerifier: cleanupVerifier, witness: witness,
		cloneCleanupWorker: linuxgate.RemoveBCloneAfterReceipt,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := notifyLinuxOwnerAnchorReady(); err != nil {
		return err
	}
	return anchor.serve(ctx)
}

func openLinuxOwnerAnchorCleanupVerifierForConfig(
	owner linuxgate.OwnerConfig,
	factory LinuxOwnerAnchorCleanupVerifierFactory,
) (LinuxOwnerAnchorCleanupVerifier, error) {
	if factory == nil {
		return nil, errors.New("Linux owner anchor requires a State cleanup verifier factory")
	}
	if owner.StateDir == "" {
		// Strict v1 owner configs remain usable for launch/recovery, but v2 clone
		// cleanup stays closed because no fixed StateDir is present.
		return nil, nil
	}
	if !validLinuxOwnerAnchorStateDir(owner.StateDir) {
		return nil, errors.New("Linux owner anchor requires canonical absolute StateDir from the root-installed owner configuration")
	}
	verifier, err := factory(owner.StateDir)
	if err != nil {
		return nil, err
	}
	if verifier == nil {
		return nil, errors.New("Linux owner anchor StateDir clone-cleanup verifier factory returned nil")
	}
	return verifier, nil
}

func parseLinuxOwnerAnchorArgs(args []string) (linuxOwnerAnchorOptions, error) {
	flags := flag.NewFlagSet("linux-owner-anchor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options linuxOwnerAnchorOptions
	flags.StringVar(&options.cgroupMount, "cgroup-mount", "", "operator-provisioned cgroup2 mount point")
	flags.StringVar(&options.delegatedPath, "delegated-path", "", "canonical relative manager subgroup beneath the cgroup2 mount")
	flags.StringVar(&options.socketPath, "socket", "", "stable A-private Unix seqpacket socket")
	if err := flags.Parse(args); err != nil {
		return linuxOwnerAnchorOptions{}, err
	}
	if flags.NArg() != 0 || !canonicalAbsoluteLinuxPath(options.cgroupMount) ||
		!canonicalDelegatedCgroupPath(options.delegatedPath) || options.delegatedPath == "" {
		return linuxOwnerAnchorOptions{}, errors.New("Linux owner anchor requires only -cgroup-mount, -delegated-path, and -socket flags")
	}
	if err := validateLinuxOwnerAnchorSocketPath(options.socketPath); err != nil {
		return linuxOwnerAnchorOptions{}, err
	}
	return options, nil
}

func validLinuxOwnerAnchorStateDir(value string) bool {
	if value == "" || len(value) > 4095 || !path.IsAbs(value) || path.Clean(value) != value || value == "/" {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x20 || value[index] == 0x7f {
			return false
		}
	}
	for _, component := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

type linuxOwnerAnchor struct {
	owner               linuxgate.OwnerConfig
	parent              *LinuxCgroupV2Parent
	listener            *net.UnixListener
	identity            authority.LinuxCgroupProcessIdentity
	userNamespaceFD     *os.File
	userNamespace       LinuxUserNamespaceIdentity
	managerPath         string
	socketPath          string
	stopping            bool
	shutdownDeadline    time.Time
	shutdownForced      bool
	shutdownError       error
	runs                map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun
	cleanupVerifier     LinuxOwnerAnchorCleanupVerifier
	cloneCleanupWorker  linuxOwnerAnchorCloneCleanupWorker
	witness             *linuxOwnerAnchorWitnessConfig
	cloneCleanupWorkers atomic.Int32
}

func (anchor *linuxOwnerAnchor) serve(ctx context.Context) (result error) {
	if anchor == nil {
		return errors.New("Linux owner anchor service is incomplete")
	}
	defer func() {
		anchor.stopping = true
		if anchor.listener != nil {
			_ = anchor.listener.Close()
		}
		stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		for key, run := range anchor.runs {
			if run != nil && run.retired {
				if err := run.close(); err != nil {
					result = errors.Join(result, fmt.Errorf("close retired Linux owner run %s/%d: %w", key.RunID, key.Generation, err))
				}
				delete(anchor.runs, key)
				continue
			}
			if run == nil {
				result = errors.Join(result, fmt.Errorf("unresolved Linux owner RunKey %s/%d has no retained run pins", key.RunID, key.Generation))
				continue
			}
			stopErr := anchor.forceStopRunForShutdown(stopContext, run)
			if stopErr != nil {
				result = errors.Join(result, fmt.Errorf("final exact stop for unresolved Linux owner RunKey %s/%d: %w", key.RunID, key.Generation, stopErr))
			}
			result = errors.Join(result, fmt.Errorf("Linux owner RunKey %s/%d remains unresolved; no durable receipt or clear was created", key.RunID, key.Generation))
		}
		cancel()
		if err := anchor.waitForCloneCleanupWorkers(); err != nil {
			result = errors.Join(result, fmt.Errorf("wait for bounded root clone cleanup worker before verifier close: %w", err))
		}
	}()
	if ctx == nil || anchor.listener == nil || anchor.parent == nil {
		return errors.New("Linux owner anchor service is incomplete")
	}
	for {
		if err := anchor.superviseExitedHelpers(); err != nil {
			anchor.stopping = true
			return fmt.Errorf("autonomous Linux owner helper-death containment failed: %w", err)
		}
		if ctx.Err() != nil {
			if !anchor.stopping {
				anchor.stopping = true
				anchor.shutdownDeadline = time.Now().Add(linuxOwnerAnchorWaitTimeout)
			}
			if anchor.allRunsRetired() {
				return nil
			}
			if !time.Now().Before(anchor.shutdownDeadline) {
				if anchor.shutdownForced {
					return errors.Join(anchor.shutdownError,
						errors.New("Linux owner anchor shutdown retained unresolved RunKeys after bounded stop/reap; no durable receipt or clear was created"))
				}
				stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
				for key, run := range anchor.runs {
					if run != nil && !run.retired {
						if err := anchor.forceStopRunForShutdown(stopContext, run); err != nil {
							anchor.shutdownError = errors.Join(anchor.shutdownError,
								fmt.Errorf("stop Linux owner RunKey %s/%d: %w", key.RunID, key.Generation, err))
						}
					}
				}
				cancel()
				anchor.shutdownForced = true
				anchor.shutdownDeadline = time.Now().Add(linuxOwnerAnchorWaitTimeout)
			}
		}
		if err := anchor.listener.SetDeadline(time.Now().Add(linuxOwnerAnchorWaitPoll)); err != nil {
			anchor.stopping = true
			stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
			stopErr := anchor.forceStopUnretiredRuns(stopContext)
			cancel()
			return errors.Join(fmt.Errorf("set Linux owner anchor accept deadline: %w", err), stopErr)
		}
		connection, err := anchor.listener.AcceptUnix()
		if err != nil {
			if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
				continue
			}
			anchor.stopping = true
			stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
			stopErr := anchor.forceStopUnretiredRuns(stopContext)
			cancel()
			return errors.Join(fmt.Errorf("accept Linux owner anchor connection: %w", sanitizeLinuxOwnerAnchorSocketError(err)), stopErr)
		}
		anchor.serveConnection(connection)
	}
}

func (anchor *linuxOwnerAnchor) waitForCloneCleanupWorkers() error {
	if anchor == nil {
		return nil
	}
	deadline := time.Now().Add(linuxOwnerAnchorCleanupTimeout)
	timedOut := false
	for anchor.cloneCleanupWorkers.Load() != 0 {
		if !timedOut && !time.Now().Before(deadline) {
			timedOut = true
		}
		// Keep the State reader pinned until every worker exits. Its own
		// context and the owner unit's bounded stop timeout bound this wait.
		timer := time.NewTimer(linuxOwnerAnchorWaitPoll)
		<-timer.C
	}
	if timedOut {
		return context.DeadlineExceeded
	}
	return nil
}

func (anchor *linuxOwnerAnchor) allRunsRetired() bool {
	for _, run := range anchor.runs {
		if run == nil || !run.retired {
			return false
		}
	}
	return true
}

func (anchor *linuxOwnerAnchor) forceStopUnretiredRuns(ctx context.Context) error {
	if anchor == nil || ctx == nil {
		return ErrLinuxCgroupV2Unresolved
	}
	var result error
	for key, run := range anchor.runs {
		if run != nil && run.retired {
			continue
		}
		if run == nil {
			result = errors.Join(result, fmt.Errorf("unresolved Linux owner RunKey %s/%d has no retained run pins", key.RunID, key.Generation))
			continue
		}
		if err := anchor.forceStopRunForShutdown(ctx, run); err != nil {
			result = errors.Join(result, fmt.Errorf("stop unresolved Linux owner RunKey %s/%d: %w", key.RunID, key.Generation, err))
		}
	}
	return result
}

// superviseExitedHelpers is polled by the anchor's accept loop, independent
// of daemon requests. A helper death fences the exact run leaf and the anchor
// reaps its adopted children, but this observer never creates a journal
// receipt or releases the pinned RunKey.
func (anchor *linuxOwnerAnchor) superviseExitedHelpers() error {
	if anchor == nil {
		return errors.New("Linux owner anchor is nil")
	}
	now := time.Now()
	for _, run := range anchor.runs {
		if run == nil || run.retired || run.helperPIDFD == nil || run.helperDeathContainmentComplete ||
			run.supervisionTerminal || run.helperDeathContainmentAttempted && run.supervisionError == nil ||
			run.supervisionError != nil && now.Before(run.supervisionRetryAfter) {
			continue
		}
		var observationErr error
		if run.helperExit == nil {
			ready, err := linuxOwnerAnchorPIDFDReady(run.helperPIDFD)
			if err != nil {
				observationErr = fmt.Errorf("poll exact helper pidfd: %w", err)
			} else if !ready {
				continue
			} else {
				waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
				err = run.waitHelper(waitContext)
				cancel()
				if err != nil {
					observationErr = fmt.Errorf("observe exact Linux owner helper death: %w", err)
				}
			}
		}
		stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		var err error
		if run.helperExit != nil {
			err = anchor.ensureExitedHelperContained(stopContext, run)
		} else {
			run.helperDeathContainmentAttempted = true
			err = anchor.forceStopRunForShutdown(stopContext, run)
		}
		cancel()
		if err != nil {
			run.supervisionError = errors.Join(observationErr, fmt.Errorf("contain run after exact helper death: %w", err))
			retryWithoutNewKill := !run.leafKillAttempted && !run.leafKillCallStarted && !run.leafKillAcknowledged ||
				run.leafKillAcknowledged && !run.leafEmptyObserved || run.stopEvidence != nil
			if retryWithoutNewKill {
				run.helperDeathContainmentAttempted = false
				run.supervisionRetryAfter = time.Now().Add(linuxOwnerAnchorSupervisionRetry)
			} else {
				// A positive kill/empty observation without a complete run proof is
				// permanently unresolved in this in-memory Anchor lifetime. Never
				// retry the write or fail the owner unit and sweep sibling RunKeys.
				run.helperDeathContainmentAttempted = true
				run.supervisionTerminal = true
			}
			continue
		}
		run.helperDeathContainmentComplete = true
		run.helperDeathContainmentAttempted = true
		run.supervisionError = nil
		run.supervisionRetryAfter = time.Time{}
		run.supervisionTerminal = false
	}
	return nil
}

func (anchor *linuxOwnerAnchor) ensureExitedHelperContained(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil ||
		!run.helperExit.ValidFor(run.helper, anchor.identity) {
		return errors.New("Linux owner anchor containment requires the exact helper exit/reap proof")
	}
	if run.helperDeathContainmentComplete {
		return nil
	}
	run.helperDeathContainmentAttempted = true
	if err := anchor.forceStopRunForShutdown(ctx, run); err != nil {
		run.supervisionError = err
		return err
	}
	run.helperDeathContainmentComplete = true
	run.helperDeathContainmentAttempted = true
	run.supervisionTerminal = false
	run.supervisionRetryAfter = time.Time{}
	run.supervisionError = nil
	return nil
}

func (anchor *linuxOwnerAnchor) verifyNormalStopChildMembership(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	cgroupPath string,
) (bool, error) {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || process.Validate() != nil || cgroupPath == "" {
		return false, errors.New("normal stop child membership check is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	if process.Equal(run.helper) {
		if cgroupPath != anchor.managerPath {
			return false, errors.New("exact helper is outside the pinned manager subgroup during StopLeaf")
		}
		return false, nil
	}
	var pidfd int
	if tracked, exists := run.children[process.PID]; exists {
		if !tracked.identity.Equal(process) || tracked.pidfd == nil {
			return false, errors.New("anchor child differs from its exact current-run process pin")
		}
		pidfd = int(tracked.pidfd.Fd())
	} else {
		opened, err := unix.PidfdOpen(process.PID, 0)
		if err != nil {
			run.migrationObserved = true
			return false, fmt.Errorf("open exact anchor child pidfd before normal StopLeaf: %w", err)
		}
		defer unix.Close(opened)
		pidfd = opened
	}
	classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
	if err != nil {
		run.migrationObserved = true
		return false, fmt.Errorf("classify exact anchor child before normal StopLeaf: %w", err)
	}
	actualCgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil || actualCgroupPath != cgroupPath {
		run.migrationObserved = true
		return false, fmt.Errorf("recheck anchor child cgroup before normal StopLeaf: %w", errOr(err, errors.New("cgroup path changed")))
	}
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
		if classification.Role == "launcher" {
			if cgroupPath != run.grant.Parent.Path {
				return false, errors.New("registered launcher left the exact manager subgroup before StopLeaf")
			}
		} else if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			return false, errors.New("registered current-run child left the exact run leaf before StopLeaf")
		}
		return false, nil
	case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			return false, errors.New("namespace-proven current-run child is outside the exact leaf before StopLeaf")
		}
		if _, exists := run.children[process.PID]; !exists {
			if err := run.trackNamespaceProvenChild(process, pidfd, classification); err != nil {
				return false, fmt.Errorf("retain exact namespace-proven anchor child before StopLeaf: %w", err)
			}
		}
		return false, nil
	case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign,
		linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		if linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return false, errors.New("different RunKey child is inside this exact run leaf before StopLeaf")
		}
		return true, nil
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		run.migrationObserved = true
		return false, nil
	default:
		run.migrationObserved = true
		return false, errors.New("anchor observed an unclassified process before normal StopLeaf")
	}
}

func (anchor *linuxOwnerAnchor) verifyNoUnknownRunProducer(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) {
		return errors.New("Linux owner anchor takeover requires exact helper exit/reap evidence")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pids, err := readLinuxOwnerProcessChildren(ctx, anchor.identity)
	if err != nil {
		return fmt.Errorf("read all-thread anchor child census before takeover StopLeaf: %w", err)
	}
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := ProcessIdentity(pid)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("capture anchor child identity before takeover StopLeaf: %w", err)
		}
		process := authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
		credentials, err := readLinuxOwnerProcessCredentials(pid)
		if err != nil || credentials.Parent != anchor.identity.PID {
			run.migrationObserved = true
			return fmt.Errorf("verify anchor child ancestry before takeover StopLeaf: %w", errOr(err, errors.New("parent PID differs")))
		}
		if err := anchor.verifyTakeoverChildOrigin(ctx, run, process); err != nil {
			return err
		}
	}
	return nil
}

func (anchor *linuxOwnerAnchor) verifyTakeoverChildOrigin(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
) error {
	tracked, registered := run.children[process.PID]
	var pidfd int
	if registered {
		if !tracked.identity.Equal(process) || tracked.pidfd == nil {
			return errors.New("anchor child differs from its exact run registration during takeover")
		}
		pidfd = int(tracked.pidfd.Fd())
	} else {
		opened, err := unix.PidfdOpen(process.PID, 0)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("open exact unregistered anchor-child pidfd during takeover: %w", err)
		}
		pidfd = opened
		defer unix.Close(pidfd)
	}
	classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
	if err != nil {
		run.migrationObserved = true
		return fmt.Errorf("classify anchor child before takeover StopLeaf: %w", err)
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil {
		run.migrationObserved = true
		return fmt.Errorf("recheck anchor child cgroup during takeover: %w", err)
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
		if classification.Role == "launcher" {
			ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
			if err != nil || !ready {
				return fmt.Errorf("registered launcher remains live at anchor takeover: %w", errOr(err, errors.New("pidfd is not ready")))
			}
			return nil
		}
		if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return nil
		}
		return nil
	case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return nil
		}
		if !registered {
			if err := run.trackNamespaceProvenChild(process, pidfd, classification); err != nil {
				return fmt.Errorf("retain namespace-proven takeover child: %w", err)
			}
		}
		return nil
	case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly:
		if err := run.trackPreReleaseGatePhysicalOnlyChild(process, pidfd, classification); err != nil {
			return fmt.Errorf("retain pre-RegisterGate physical-only gate: %w", err)
		}
		return nil
	case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign,
		linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		if linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return errors.New("foreign RunKey process is inside this exact run leaf before takeover StopLeaf")
		}
		return nil
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		run.migrationObserved = true
		return nil
	default:
		run.migrationObserved = true
		return errors.New("anchor observed an unclassified child before takeover StopLeaf")
	}
}

func (anchor *linuxOwnerAnchor) skipChildOwnedByAnotherRun(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	cgroupPath string,
) (bool, error) {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || process.Validate() != nil || cgroupPath == "" {
		return false, errors.New("classify anchor child ownership requires an exact run and process identity")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var pidfd int
	if tracked, exists := run.children[process.PID]; exists {
		if !tracked.identity.Equal(process) || tracked.pidfd == nil {
			return false, errors.New("anchor child differs from its exact current-run registration")
		}
		pidfd = int(tracked.pidfd.Fd())
	} else {
		opened, err := unix.PidfdOpen(process.PID, 0)
		if err != nil {
			return false, fmt.Errorf("open exact anchor child pidfd for cross-run classification: %w", err)
		}
		defer unix.Close(opened)
		pidfd = opened
	}
	classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
	if err != nil {
		run.migrationObserved = true
		return false, fmt.Errorf("classify anchor child RunKey origin: %w", err)
	}
	actualCgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil || actualCgroupPath != cgroupPath {
		run.migrationObserved = true
		return false, fmt.Errorf("recheck anchor child cgroup for cross-run classification: %w", errOr(err, errors.New("cgroup path changed")))
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign:
		if linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return false, errors.New("anchor observed a different RunKey's child in this exact run leaf")
		}
		return true, nil
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent, linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		return false, nil
	case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly:
		return false, nil
	case linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		if linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return false, errors.New("another RunKey's physical-only gate is inside this exact run leaf")
		}
		return true, nil
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		run.migrationObserved = true
		return false, errors.New("anchor observed a current-run process outside its exact run leaf")
	default:
		run.migrationObserved = true
		return false, errors.New("anchor observed an unclassified child during cross-run ownership check")
	}
}

func (anchor *linuxOwnerAnchor) establishRunNoMigrationProof(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	fence *LinuxOwnerAnchorProducerFence,
) error {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || !run.grant.Anchor.Equal(anchor.identity) {
		return errors.New("NoMigrationObserved requires the exact retained Anchor and RunKey")
	}
	if fence != nil && fence.ValidateFor(run.grant, run.helper) != nil {
		return errors.New("NoMigrationObserved premise differs from the exact producer fence")
	}
	run.noMigrationProven = false
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(run.physicalOnlyChildren) != 0 {
		// The gate can be physically contained under its exact launcher-birth
		// premise, but without a registered GateIdentity it cannot participate
		// in a RunKey-complete StopReceipt.
		return nil
	}
	if run.migrationObserved {
		return nil
	}
	if run.gateIdentity == nil {
		if fence == nil || fence.Launcher == nil || fence.LauncherExit == nil ||
			fence.Gate != nil || fence.UserNamespace != nil || run.gateUserNamespaceFD != nil || run.gateImageFD != nil {
			// A helper-death takeover before RegisterGate has no authenticated
			// no-gate result. It remains physically containable only when the
			// earlier producer scans show no unknown process; it cannot issue a
			// run-bound stop receipt.
			return nil
		}
		if !fence.LauncherExit.ValidForOwnedDescendant(*fence.Launcher, run.helper, anchor.identity, nil) {
			return errors.New("pre-gate no-target proof lacks exact helper-owned launcher reap evidence")
		}
		if fence.GateOutcome != nil {
			if fence.UntrustedProcess != nil ||
				linuxgate.VerifyGateNeverProducedAuthenticator(run.grant.Secret, *fence.GateOutcome) != nil ||
				!fence.GateOutcome.Launcher.Equal(*fence.Launcher) {
				return errors.New("pre-gate no-gate migration premise differs from its exact authenticated launcher outcome")
			}
			launcherSeen := false
			for _, child := range run.children {
				if child.role != "launcher" || !child.identity.Equal(*fence.Launcher) ||
					!child.creator.Equal(run.helper) || child.pidfd == nil ||
					child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
					child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
					return errors.New("pre-gate no-target proof contains an unbound root launcher")
				}
				if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(child.pidfd.Fd()), child.identity); err != nil {
					return fmt.Errorf("verify pre-gate launcher pidfd: %w", err)
				}
				ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
				if err != nil || !ready {
					return fmt.Errorf("verify pre-gate launcher exit: %w", errOr(err, errors.New("pidfd is not ready")))
				}
				launcherSeen = true
			}
			if !launcherSeen {
				return errors.New("pre-gate no-target proof lacks its retained exact root launcher")
			}
			run.noMigrationProven = true
			return nil
		}
		if fence.UntrustedProcess == nil || run.gateReleaseAuthorized || fence.ReleaseAttempted || fence.Released {
			return nil
		}
		untrusted, exists := run.children[fence.UntrustedProcess.PID]
		if !exists || !untrusted.identity.Equal(*fence.UntrustedProcess) || untrusted.role != "descendant" ||
			!untrusted.creator.Equal(run.helper) || untrusted.pidfd == nil ||
			untrusted.classification.ValidateFor(run.grant, run.helper, untrusted.identity) != nil ||
			untrusted.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
			return errors.New("pre-gate failed-Prepare migration premise lacks its exact retained A child")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(untrusted.pidfd.Fd()), untrusted.identity); err != nil {
			return fmt.Errorf("verify failed-Prepare child pidfd for no-migration proof: %w", err)
		}
		if err := verifyLinuxOwnerRunChildCredentials(untrusted.identity.PID, "gate", run.owner); err != nil {
			return fmt.Errorf("verify failed-Prepare child remains host A: %w", err)
		}
		if !run.migrationObserved {
			if err := verifyLinuxOwnerRunChildMembership(untrusted.identity.PID, "descendant", run.grant, run.leaf); err != nil {
				return err
			}
		}
		launcherSeen, untrustedSeen := false, false
		for _, child := range run.children {
			switch child.role {
			case "launcher":
				if !child.identity.Equal(*fence.Launcher) || child.pidfd == nil ||
					child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
					child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
					return errors.New("pre-gate failed-Prepare proof contains an unbound root launcher")
				}
				launcherSeen = true
			case "descendant":
				if !child.identity.Equal(*fence.UntrustedProcess) || !child.creator.Equal(run.helper) ||
					child.pidfd == nil || child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
					child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
					return errors.New("pre-gate failed-Prepare proof contains an unbound helper descendant")
				}
				untrustedSeen = true
			default:
				return errors.New("pre-gate failed-Prepare proof contains a gate or native process")
			}
		}
		if !launcherSeen || !untrustedSeen {
			return errors.New("pre-gate failed-Prepare proof lacks its exact producer set")
		}
		run.noMigrationProven = true
		return nil
	}
	if run.gateIdentity.Validate() != nil || !run.gateIdentity.TrustedImage || !run.gateIdentity.BornInLeaf ||
		!run.gateIdentity.Cgroup.Equal(run.grant.Leaf) || !run.gateReadyAuthenticated || run.gateImageFD == nil {
		return errors.New("whole-leaf kill lacks the exact authenticated GateReady/image proof")
	}
	if err := run.verifyPinnedGateUserNamespace(); err != nil {
		return err
	}
	parentNamespace, err := linuxOwnerAnchorUserNamespaceParentIdentity(run.gateUserNamespaceFD)
	if err != nil || parentNamespace != anchor.userNamespace {
		return fmt.Errorf("verify exact gate user-namespace parent before cgroup.kill: %w", errOr(err, errors.New("parent namespace differs")))
	}
	var gateImageStat unix.Stat_t
	if err := unix.Fstat(int(run.gateImageFD.Fd()), &gateImageStat); err != nil ||
		gateImageStat.Mode&unix.S_IFMT != unix.S_IFREG || gateImageStat.Uid != 0 || gateImageStat.Gid != 0 ||
		gateImageStat.Mode&0022 != 0 || gateImageStat.Mode&0111 == 0 || gateImageStat.Nlink != 1 {
		return fmt.Errorf("verify exact pinned gate image before cgroup.kill: %w", errOr(err, errors.New("root-owned image identity differs")))
	}
	if fence != nil {
		if fence.Gate == nil || !fence.Gate.Equal(*run.gateIdentity) || fence.UserNamespace == nil ||
			*fence.UserNamespace != run.gateUserNamespace || fence.ValidateFor(run.grant, run.helper) != nil {
			return errors.New("normal StopLeaf migration premise differs from the exact helper GateReady fence")
		}
	}
	// The pinned trusted static gate passed its inherited-FD census before
	// GateReady, then enforced the fixed four-rights RELEASE allowlist, B-only
	// maps, no-new-privileges, and zero host capabilities before exec. The
	// Anchor rechecks B credentials/namespace and every present leaf member
	// above. No kernel snapshot can exclude an arbitrary concurrent UID-A
	// cgroup.procs migration; this proof depends on the provisioned trusted-A
	// birth-only placement contract. Any future A migration API must serialize
	// through this Anchor and record source/destination before its effect.
	run.noMigrationProven = true
	return nil
}

func validateLinuxOwnerAnchorCreatorChain(ctx context.Context, run *linuxOwnerAnchorRun, child linuxOwnerAnchorTrackedProcess) error {
	if ctx == nil || run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || child.identity.Validate() != nil ||
		child.creator.Validate() != nil || child.pidfd == nil || !linuxOwnerAnchorValidChildRole(child.role) ||
		child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
		child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent || child.classification.Role != child.role {
		return errors.New("RunKey creator chain has incomplete process identities")
	}
	current := child
	var visited [linuxOwnerAnchorMaxCreatorChainDepth]int
	visitedCount := 0
	for step := 0; step < linuxOwnerAnchorMaxCreatorChainDepth; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		for index := 0; index < visitedCount; index++ {
			if visited[index] == current.identity.PID {
				return errors.New("RunKey child creator ancestry contains a cycle")
			}
		}
		visited[visitedCount] = current.identity.PID
		visitedCount++
		if current.creator.Equal(run.helper) {
			if run.helperExit != nil {
				if !run.helperExit.ValidFor(run.helper, run.grant.Anchor) {
					return errors.New("RunKey helper creator exit differs from its exact anchor wait")
				}
				return nil
			}
			if run.helperPIDFD == nil {
				return errors.New("live RunKey helper creator has no retained pidfd")
			}
			ready, err := linuxOwnerAnchorPIDFDReady(run.helperPIDFD)
			if err != nil || ready {
				return fmt.Errorf("verify live RunKey helper creator: %w", errOr(err, errors.New("pidfd is not live")))
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
				return fmt.Errorf("verify live RunKey helper creator pidfd: %w", err)
			}
			if err := verifyLinuxOwnerAProcess(run.helper.PID, run.helper.Identity, run.owner.AUID, run.owner.AGID,
				run.grant.Anchor.PID, run.grant.Parent.Path); err != nil {
				return fmt.Errorf("verify live RunKey helper creator ancestry: %w", err)
			}
			return nil
		}
		parent, exists := run.children[current.creator.PID]
		if !exists || !parent.identity.Equal(current.creator) || !linuxOwnerAnchorValidChildRole(parent.role) {
			return errors.New("RunKey child creator is not the exact retained parent identity")
		}
		if evidence, reaped := run.reaped[parent.identity.PID]; reaped {
			if !evidence.Process.Equal(parent.identity) || evidence.ValidateFor(run.grant, run.helper, parent.identity, parent.role) != nil {
				return errors.New("RunKey child creator has invalid exact anchor wait/reap evidence")
			}
		} else {
			if parent.pidfd == nil {
				return errors.New("unreaped RunKey child creator has no retained exact pidfd")
			}
			ready, err := linuxOwnerAnchorPIDFDReady(parent.pidfd)
			if err != nil {
				return fmt.Errorf("poll retained RunKey child creator pidfd: %w", err)
			}
			if ready {
				// The retained pidfd and stored creator identity remain the exact
				// historical edge even if the parent has exited and been reaped
				// outside the anchor's wait domain.
				flags, err := unix.FcntlInt(parent.pidfd.Fd(), unix.F_GETFD, 0)
				if err != nil || flags&unix.FD_CLOEXEC == 0 {
					return errors.Join(errors.New("exited RunKey child creator pidfd is not a retained close-on-exec descriptor"), err)
				}
			} else {
				if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(parent.pidfd.Fd()), parent.identity); err != nil {
					return fmt.Errorf("verify live RunKey child creator pidfd: %w", err)
				}
				if identity, err := ProcessIdentity(parent.identity.PID); err != nil || identity != parent.identity.Identity {
					return fmt.Errorf("verify live RunKey child creator identity: %w", errOr(err, errors.New("creation identity differs")))
				}
				if err := verifyLinuxOwnerRunChildCredentials(parent.identity.PID, parent.role, run.owner); err != nil {
					return fmt.Errorf("verify live RunKey child creator credentials: %w", err)
				}
				if !run.leafRemovedObserved {
					if err := verifyLinuxOwnerRunChildMembership(parent.identity.PID, parent.role, run.grant, run.leaf); err != nil {
						return fmt.Errorf("verify live RunKey child creator membership: %w", err)
					}
				}
			}
		}
		current = parent
	}
	return errors.New("RunKey child creator ancestry exceeds its bounded depth")
}

func (anchor *linuxOwnerAnchor) fenceRunLaunchers(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) {
		return errors.New("anchor launcher fencing requires exact helper exit/reap evidence")
	}
	for _, child := range run.children {
		if err := ctx.Err(); err != nil {
			return err
		}
		if child.role != "launcher" {
			continue
		}
		if child.pidfd == nil {
			return errors.New("registered root launcher lacks its exact pidfd during anchor takeover")
		}
		ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
		if err != nil {
			return fmt.Errorf("poll exact root launcher before takeover: %w", err)
		}
		if ready {
			continue
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(child.pidfd.Fd()), child.identity); err != nil {
			return fmt.Errorf("verify live root launcher before takeover: %w", err)
		}
		if err := unix.PidfdSendSignal(int(child.pidfd.Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("signal exact root launcher during takeover: %w", err)
		}
		if err := waitLinuxOwnerAnchorPIDFDReady(ctx, child.pidfd); err != nil {
			return fmt.Errorf("wait for exact root launcher fence during takeover: %w", err)
		}
	}
	return anchor.verifyNoUnknownRunProducer(ctx, run)
}

func (anchor *linuxOwnerAnchor) forceStopRunForShutdown(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if ctx == nil || run == nil || run.leaf == nil || run.helperPIDFD == nil || run.grant.Validate() != nil {
		return ErrLinuxCgroupV2Unresolved
	}
	if run.helperExit == nil {
		var signalErr error
		if err := unix.PidfdSendSignal(int(run.helperPIDFD.Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			signalErr = fmt.Errorf("signal exact helper during bounded anchor shutdown: %w", err)
		}
		if err := run.waitHelper(ctx); err != nil {
			return errors.Join(signalErr, fmt.Errorf("wait/reap exact helper during bounded anchor shutdown: %w", err))
		}
	}
	if err := anchor.fenceRunLaunchers(ctx, run); err != nil {
		return fmt.Errorf("fence exact run launcher before anchor-owned cgroup.kill: %w", err)
	}
	stopContext, cancel := context.WithTimeout(ctx, linuxOwnerAnchorWaitTimeout)
	_, stopErr := anchor.stopRunLeaf(stopContext, run, nil)
	cancel()
	if stopErr != nil {
		var reapErr error
		if run.leafEmptyObserved && run.helperExit != nil {
			_, reapErr = anchor.reapRunChildren(ctx, run)
		}
		return errors.Join(fmt.Errorf("anchor-owned cgroup.kill/empty evidence is unresolved: %w", stopErr), reapErr)
	}
	processes, err := anchor.reapRunChildren(ctx, run)
	if err != nil {
		return fmt.Errorf("reap exact adopted run children after anchor-owned cgroup.kill: %w", err)
	}
	finalEvidence, err := anchor.finalizeRunStopEvidence(ctx, run, processes)
	if err != nil {
		return fmt.Errorf("finalize exact anchor stop/reap evidence: %w", err)
	}
	if err := finalEvidence.ValidateFor(run.grant, run.helper, anchor.identity, processes, true, run.gateUserNamespaceFD); err != nil {
		return fmt.Errorf("validate final anchor stop/reap evidence: %w", err)
	}
	return nil
}

func (anchor *linuxOwnerAnchor) serveConnection(connection *net.UnixConn) (detached bool) {
	defer func() {
		if !detached {
			_ = connection.Close()
		}
	}()
	if err := connection.SetDeadline(time.Now().Add(linuxOwnerAnchorRequestTimeout)); err != nil {
		return
	}
	peer, err := linuxOwnerAnchorPeerFromConn(connection, anchor.owner)
	if err != nil || peer.PID == anchor.identity.PID {
		return
	}
	request, receivedFDs, err := readLinuxOwnerAnchorRequest(connection)
	if err != nil {
		closeLinuxOwnerAnchorFDs(receivedFDs)
		return
	}
	defer closeLinuxOwnerAnchorFDs(receivedFDs)
	if request.Version != linuxOwnerAnchorProtocolVersion || request.ClientPID != peer.PID || request.ClientIdentity != peer.Identity {
		return
	}
	if request.Operation != linuxOwnerAnchorOperationCloneCleanup &&
		(request.CloneGrant != nil || request.Cleanup != nil || request.TargetFDPresent) {
		return
	}
	gateOperation := request.Operation == linuxOwnerAnchorOperationRegisterGate || request.Operation == linuxOwnerAnchorOperationAuthorizeGate
	stopLeafOperation := request.Operation == linuxOwnerAnchorOperationStopLeaf
	classifyChildOperation := request.Operation == linuxOwnerAnchorOperationClassifyChild
	if !gateOperation && (request.Gate != nil || request.UserNamespace != nil) {
		return
	}
	if !stopLeafOperation && request.StopFence != nil {
		return
	}
	if anchor.stopping && (request.Operation == "identity" || request.Operation == linuxOwnerAnchorOperationStart ||
		request.Operation == "register-process" || gateOperation || stopLeafOperation || classifyChildOperation) {
		anchor.writeResponse(connection, request, "anchor_stopping", nil, nil)
		return
	}
	if request.Operation == "identity" {
		if linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) {
			return
		}
		if len(receivedFDs) != 0 || request.Grant != (LinuxOwnerAnchorGrant{}) || request.Helper != nil || request.Creator != nil || request.Process != nil || request.Role != "" ||
			request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil || request.CloneGrant != nil || request.Cleanup != nil || request.TargetFDPresent ||
			request.RunKey != (LinuxOwnerAnchorRunKey{}) || request.Fence != (protocol.Fence{}) {
			return
		}
		if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), anchor.managerPath); err != nil {
			return
		}
		if err := anchor.parent.verifyPinnedParent(); err != nil {
			return
		}
		anchor.writeResponse(connection, request, "ready", nil, nil)
		return
	}
	if request.Operation == linuxOwnerAnchorOperationRetireCleared || request.Operation == linuxOwnerAnchorOperationRetireAborted {
		validProofShape := request.ReleaseProof != nil && request.PreGateReleaseProof == nil && request.AbortProof == nil ||
			request.ReleaseProof == nil && request.PreGateReleaseProof != nil && request.AbortProof == nil ||
			request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof != nil
		if len(receivedFDs) != 0 || request.Grant != (LinuxOwnerAnchorGrant{}) ||
			request.Helper == nil || request.Helper.Validate() != nil || request.RunKey.Generation <= 0 ||
			request.Fence.Generation != request.RunKey.Generation || !validLinuxOwnerAnchorRunID(request.RunKey.RunID) ||
			!validProofShape || request.Operation == linuxOwnerAnchorOperationRetireAborted && request.AbortProof == nil ||
			request.Operation == linuxOwnerAnchorOperationRetireCleared && request.AbortProof != nil ||
			linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) {
			return
		}
		if err := connection.SetDeadline(time.Now().Add(linuxOwnerAnchorWaitTimeout)); err != nil {
			return
		}
		if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), anchor.managerPath); err != nil {
			return
		}
		if err := anchor.parent.verifyPinnedParent(); err != nil {
			return
		}
		if request.Operation == linuxOwnerAnchorOperationRetireCleared {
			anchor.handleRetireClearedRun(connection, request)
		} else {
			anchor.handleRetireAbortedHelper(connection, request)
		}
		return
	}
	if request.Operation == linuxOwnerAnchorOperationCloneCleanup {
		detached = anchor.serveCloneCleanup(connection, peer, request, receivedFDs)
		return
	}
	if request.Grant.Validate() != nil || request.Grant.OwnerUID != anchor.owner.AUID || request.Grant.LauncherUID != 0 ||
		!request.Grant.Anchor.Equal(anchor.identity) ||
		!request.Grant.Parent.Equal(anchor.parent.Identity()) {
		return
	}
	helperPeer := false
	if (request.Operation == "register-process" || gateOperation || stopLeafOperation || classifyChildOperation) && request.Helper != nil {
		run := anchor.runs[request.Grant.RunKey]
		helperPeer = run != nil && run.grant.Equal(request.Grant) && run.helper.Equal(*request.Helper) &&
			peer.PID == run.helper.PID && peer.Identity == run.helper.Identity
	}
	if linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) && !helperPeer {
		return
	}
	operationTimeout := linuxOwnerAnchorRequestTimeout
	if request.Operation == "wait-reap" || request.Operation == linuxOwnerAnchorOperationReapRun ||
		request.Operation == linuxOwnerAnchorOperationWaitHelper || request.Operation == linuxOwnerAnchorOperationLookup {
		operationTimeout = linuxOwnerAnchorWaitTimeout
	}
	if stopLeafOperation || classifyChildOperation {
		operationTimeout = linuxOwnerAnchorWaitTimeout
	}
	if err := connection.SetDeadline(time.Now().Add(operationTimeout)); err != nil {
		return
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), anchor.managerPath); err != nil {
		return
	}
	if err := anchor.parent.verifyPinnedParent(); err != nil {
		return
	}
	switch request.Operation {
	case linuxOwnerAnchorOperationStart:
		if len(receivedFDs) != 2 || request.Helper != nil || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		if anchor.stopping {
			anchor.writeResponse(connection, request, "anchor_stopping", nil, nil)
			return
		}
		if err := verifyLinuxOwnerAnchorLeafFD(receivedFDs[0], request.Grant.Leaf, anchor.owner.AUID); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		if err := verifyLinuxOwnerAnchorOwnerPipeReadFD(receivedFDs[1]); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		if existing, exists := anchor.runs[request.Grant.RunKey]; exists {
			if existing == nil || !existing.grant.Equal(request.Grant) {
				anchor.writeResponse(connection, request, "run_unresolved", nil, nil)
				return
			}
			anchor.writeResponse(connection, request, "already_started", &existing.helper, nil)
			return
		}
		var witnessForRun *linuxOwnerAnchorWitnessConfig
		if anchor.witness != nil && anchor.witness.RunID == request.Grant.RunKey.RunID &&
			anchor.witness.Generation == request.Grant.RunKey.Generation {
			copy := *anchor.witness
			witnessForRun = &copy
		}
		ownerProcess := authority.LinuxCgroupProcessIdentity{PID: peer.PID, Identity: peer.Identity}
		beforeFork := func() error { return anchor.recordHelperForkMayStart(request.Grant.RunKey) }
		run, err := startLinuxOwnerAnchorHelperV2(
			anchor, request.Grant, ownerProcess, receivedFDs[0], receivedFDs[1], witnessForRun, beforeFork,
		)
		if run != nil {
			anchor.runs[request.Grant.RunKey] = run
		}
		if err != nil || run == nil {
			status := "request_rejected"
			if _, forkMayHaveOccurred := anchor.runs[request.Grant.RunKey]; forkMayHaveOccurred {
				status = "start_unresolved"
			}
			anchor.writeResponse(connection, request, status, nil, nil)
			return
		}
		helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			anchor.writeResponse(connection, request, "start_unresolved", &run.helper, nil)
			return
		}
		leafFD, err := run.leaf.DuplicateFD()
		if err != nil {
			_ = unix.Close(helperFD)
			anchor.writeResponse(connection, request, "start_unresolved", &run.helper, nil)
			return
		}
		parentFD, err := anchor.parent.DuplicateFD()
		if err != nil {
			_ = unix.Close(helperFD)
			_ = leafFD.Close()
			anchor.writeResponse(connection, request, "start_unresolved", &run.helper, nil)
			return
		}
		anchor.writeResponseWithFDs(connection, request, "started", &run.helper, nil, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
		_ = unix.Close(helperFD)
		_ = leafFD.Close()
		_ = parentFD.Close()
	case linuxOwnerAnchorOperationAttach:
		if len(receivedFDs) != 0 || request.Helper == nil || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run, exists := anchor.runs[request.Grant.RunKey]
		if !exists || run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if run.retired {
			anchor.writeResponse(connection, request, "retired", &run.helper, nil)
			return
		}
		if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), run.helper); err != nil {
			anchor.writeResponse(connection, request, "helper_lost", &run.helper, nil)
			return
		}
		helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			anchor.writeResponse(connection, request, "attach_unresolved", &run.helper, nil)
			return
		}
		leafFD, err := run.leaf.DuplicateFD()
		if err != nil {
			_ = unix.Close(helperFD)
			anchor.writeResponse(connection, request, "attach_unresolved", &run.helper, nil)
			return
		}
		parentFD, err := anchor.parent.DuplicateFD()
		if err != nil {
			_ = unix.Close(helperFD)
			_ = leafFD.Close()
			anchor.writeResponse(connection, request, "attach_unresolved", &run.helper, nil)
			return
		}
		anchor.writeResponseWithFDs(connection, request, "attached", &run.helper, nil, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
		_ = unix.Close(helperFD)
		_ = leafFD.Close()
		_ = parentFD.Close()
	case linuxOwnerAnchorOperationLookup:
		if len(receivedFDs) != 0 || request.Helper != nil || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run, exists := anchor.runs[request.Grant.RunKey]
		if !exists || run == nil || !run.grant.Equal(request.Grant) || run.helper.Validate() != nil {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if run.retired {
			anchor.writeResponse(connection, request, "retired", &run.helper, nil)
			return
		}
		helperExited := run.helperExit != nil
		if !helperExited {
			if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), run.helper); err != nil {
				waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
				waitErr := run.waitHelper(waitContext)
				cancel()
				if waitErr != nil {
					anchor.writeResponse(connection, request, "wait_unproven", &run.helper, nil)
					return
				}
				helperExited = true
			}
		}
		leafFD, err := run.leaf.DuplicateFD()
		if err != nil {
			anchor.writeResponse(connection, request, "lookup_unresolved", &run.helper, nil)
			return
		}
		parentFD, err := anchor.parent.DuplicateFD()
		if err != nil {
			_ = leafFD.Close()
			anchor.writeResponse(connection, request, "lookup_unresolved", &run.helper, nil)
			return
		}
		if helperExited {
			anchor.writeResponseWithRunPins(connection, request, "helper_exited", &run.helper, run.helperExit, int(leafFD.Fd()), int(parentFD.Fd()))
		} else {
			helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
			if err != nil {
				_ = leafFD.Close()
				_ = parentFD.Close()
				anchor.writeResponse(connection, request, "lookup_unresolved", &run.helper, nil)
				return
			}
			anchor.writeResponseWithFDs(connection, request, "attached", &run.helper, nil, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
			_ = unix.Close(helperFD)
		}
		_ = leafFD.Close()
		_ = parentFD.Close()
	case "register-process":
		if len(receivedFDs) != 1 || request.Helper == nil || request.Creator == nil || request.Process == nil ||
			request.Gate != nil || request.UserNamespace != nil ||
			(request.Role != "launcher" && request.Role != "native" && request.Role != "descendant") {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if err := run.registerChild(*request.Process, *request.Creator, request.Role, receivedFDs[0]); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		anchor.writeResponse(connection, request, "registered", nil, nil)
	case linuxOwnerAnchorOperationClassifyChild:
		if len(receivedFDs) != 1 || !validLinuxOwnerAnchorChildOriginRequest(request) || !helperPeer {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(receivedFDs[0], *request.Process); err != nil {
			anchor.writeResponseWithChildClassification(connection, request, "origin_unproven", linuxOwnerAnchorUnknownChildClassification(run, *request.Process))
			return
		}
		credentials, err := readLinuxOwnerProcessCredentials(request.Process.PID)
		if err != nil || credentials.Parent != run.helper.PID {
			run.migrationObserved = true
			anchor.writeResponseWithChildClassification(connection, request, "origin_unproven", linuxOwnerAnchorUnknownChildClassification(run, *request.Process))
			return
		}
		classifyContext, cancel := context.WithTimeout(context.Background(), operationTimeout)
		classification, err := anchor.classifyRunChildOrigin(classifyContext, run, *request.Process, receivedFDs[0])
		cancel()
		credentials, parentErr := readLinuxOwnerProcessCredentials(request.Process.PID)
		if err != nil || parentErr != nil || credentials.Parent != run.helper.PID {
			run.migrationObserved = true
			anchor.writeResponseWithChildClassification(connection, request, "origin_unproven", linuxOwnerAnchorUnknownChildClassification(run, *request.Process))
			return
		}
		anchor.writeResponseWithChildClassification(connection, request, "classified", classification)
	case linuxOwnerAnchorOperationRegisterGate:
		if len(receivedFDs) != 1 || !validLinuxOwnerAnchorGateRequest(request) || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) || !helperPeer {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		for key, other := range anchor.runs {
			if key == request.Grant.RunKey || other == nil || other.retired || other.gateUserNamespaceFD == nil {
				continue
			}
			otherNamespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(other.gateUserNamespaceFD)
			if err != nil || otherNamespace == *request.UserNamespace {
				anchor.writeResponse(connection, request, "request_rejected", nil, nil)
				return
			}
		}
		if err := run.registerGate(*request.Gate, *request.UserNamespace, receivedFDs[0], anchor.userNamespaceFD); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		anchor.writeResponse(connection, request, "registered", nil, nil)
	case linuxOwnerAnchorOperationAuthorizeGate:
		if len(receivedFDs) != 0 || !validLinuxOwnerAnchorGateRequest(request) || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) || !helperPeer {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if err := run.authorizeGateRelease(*request.Gate, *request.UserNamespace); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		anchor.writeResponse(connection, request, "release_authorized", nil, nil)
	case linuxOwnerAnchorOperationStopLeaf:
		if len(receivedFDs) != 0 || !validLinuxOwnerAnchorStopLeafRequest(request) {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) || !helperPeer {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		stopEvidence, err := anchor.stopRunLeaf(stopContext, run, request.StopFence)
		cancel()
		if err != nil {
			anchor.writeResponse(connection, request, "stop_unproven", nil, nil)
			return
		}
		anchor.writeResponseWithStopEvidence(connection, request, "stopped", stopEvidence)
	case "wait-reap":
		if len(receivedFDs) != 0 || request.Helper == nil || request.Creator != nil || request.Process == nil || !linuxOwnerAnchorValidChildRole(request.Role) {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		tracked, registered := run.children[request.Process.PID]
		if !registered || !tracked.identity.Equal(*request.Process) || tracked.role != request.Role {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		if run.helperExit == nil {
			if err := run.waitHelper(waitContext); err != nil {
				cancel()
				anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
				return
			}
		}
		if err := anchor.ensureExitedHelperContained(waitContext, run); err != nil {
			cancel()
			anchor.writeResponse(connection, request, "stop_unproven", nil, nil)
			return
		}
		evidence, err := run.reapChild(waitContext, anchor.identity, *request.Process, request.Role)
		cancel()
		if err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
			return
		}
		anchor.writeResponse(connection, request, "reaped", nil, &evidence)
	case linuxOwnerAnchorOperationReapRun:
		if len(receivedFDs) != 0 || request.Helper == nil || request.Creator != nil || request.Process != nil || request.Role != "" || request.AfterPID < 0 {
			return
		}
		run, exists := anchor.runs[request.Grant.RunKey]
		if !exists || run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		if run.helperExit == nil {
			if err := run.waitHelper(waitContext); err != nil {
				cancel()
				anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
				return
			}
		}
		if err := anchor.ensureExitedHelperContained(waitContext, run); err != nil {
			cancel()
			anchor.writeResponse(connection, request, "stop_unproven", nil, nil)
			return
		}
		evidence, err := anchor.reapRunChildren(waitContext, run)
		if err != nil {
			cancel()
			anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
			return
		}
		stopEvidence, err := anchor.finalizeRunStopEvidence(waitContext, run, evidence)
		cancel()
		if err != nil {
			anchor.writeResponse(connection, request, "stop_unproven", nil, nil)
			return
		}
		filtered := make([]LinuxOwnerAnchorReapEvidence, 0, linuxOwnerAnchorReapPageSize)
		for _, item := range evidence {
			if item.Process.PID > request.AfterPID && len(filtered) < linuxOwnerAnchorReapPageSize {
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
		anchor.writeResponseWithReapEvidence(connection, request, "reaped", filtered, nextPID, more, stopEvidence)
	case linuxOwnerAnchorOperationWaitHelper:
		if len(receivedFDs) != 0 || request.Helper == nil || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run, exists := anchor.runs[request.Grant.RunKey]
		if !exists || run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		if run.helperExit == nil {
			if err := run.waitHelper(waitContext); err != nil {
				cancel()
				anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
				return
			}
		}
		cancel()
		anchor.writeResponseWithHelperExit(connection, request, *run.helperExit)
	case linuxOwnerAnchorOperationRetire:
		anchor.handleRetireHelper(connection, request, receivedFDs)
	default:
		return
	}
	return
}

func (anchor *linuxOwnerAnchor) recordHelperForkMayStart(key LinuxOwnerAnchorRunKey) error {
	if anchor == nil || anchor.runs == nil || !validLinuxOwnerAnchorRunID(key.RunID) || key.Generation <= 0 {
		return errors.New("Linux owner anchor pre-fork tombstone identity is invalid")
	}
	if _, exists := anchor.runs[key]; exists {
		return ErrLinuxOwnerAnchorHelperAlreadyStarted
	}
	// This callback runs immediately before cmd.Start. Any later uncertain
	// outcome keeps the tombstone; pre-fork validation failures stay retryable.
	anchor.runs[key] = nil
	return nil
}

func validLinuxOwnerAnchorChildOriginRequest(request linuxOwnerAnchorRequest) bool {
	return request.Operation == linuxOwnerAnchorOperationClassifyChild && request.Grant.Validate() == nil &&
		request.Helper != nil && request.Helper.Validate() == nil && request.Process != nil && request.Process.Validate() == nil &&
		request.Gate == nil && request.UserNamespace == nil && request.StopFence == nil && request.Creator == nil && request.Role == "" &&
		request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof == nil &&
		request.RunKey == (LinuxOwnerAnchorRunKey{}) && request.Fence == (protocol.Fence{}) && request.FenceDigest == "" &&
		request.AfterPID == 0 && request.CloneGrant == nil && request.Cleanup == nil && request.PriorRemovalProof == nil && !request.TargetFDPresent
}

func validLinuxOwnerAnchorGateRequest(request linuxOwnerAnchorRequest) bool {
	return (request.Operation == linuxOwnerAnchorOperationRegisterGate || request.Operation == linuxOwnerAnchorOperationAuthorizeGate) &&
		request.Helper != nil && request.Helper.Validate() == nil && request.Gate != nil && request.Gate.Validate() == nil &&
		request.UserNamespace != nil && request.UserNamespace.valid() && request.Creator == nil && request.Process == nil && request.Role == "" &&
		request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof == nil &&
		request.RunKey == (LinuxOwnerAnchorRunKey{}) && request.Fence == (protocol.Fence{}) && request.FenceDigest == "" &&
		request.AfterPID == 0 && request.CloneGrant == nil && request.Cleanup == nil && request.PriorRemovalProof == nil && !request.TargetFDPresent
}

func validLinuxOwnerAnchorStopLeafRequest(request linuxOwnerAnchorRequest) bool {
	return request.Operation == linuxOwnerAnchorOperationStopLeaf && request.Helper != nil && request.Helper.Validate() == nil &&
		request.StopFence != nil && request.StopFence.ValidateFor(request.Grant, *request.Helper) == nil &&
		request.Gate == nil && request.UserNamespace == nil && request.Creator == nil && request.Process == nil && request.Role == "" &&
		request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof == nil &&
		request.RunKey == (LinuxOwnerAnchorRunKey{}) && request.Fence == (protocol.Fence{}) && request.FenceDigest == "" &&
		request.AfterPID == 0 && request.CloneGrant == nil && request.Cleanup == nil && request.PriorRemovalProof == nil && !request.TargetFDPresent
}

// runCloneCleanupAfterStateReadback is called only after the socket request,
// pinned descriptor, exact intent and optional prior proof pass anchor-side
// structural checks. State remains authoritative for whether Begin/receipt
// facts were actually committed; the root worker is unreachable until that
// pinned readback succeeds.
func (anchor *linuxOwnerAnchor) runCloneCleanupAfterStateReadback(
	ctx context.Context,
	request linuxOwnerAnchorRequest,
	rootFD, targetFD int,
	cloneGrant authority.LinuxCloneGrant,
	cleanup authority.LinuxCloneCleanupIntent,
	priorRemoval *authority.LinuxCloneTargetRemovalProof,
) (authority.LinuxCloneTargetRemovalProof, error) {
	if anchor == nil || ctx == nil || ctx.Err() != nil || request.Operation != linuxOwnerAnchorOperationCloneCleanup ||
		request.Cleanup == nil || request.CloneGrant == nil || request.RunKey != (LinuxOwnerAnchorRunKey{RunID: cleanup.Intent.RunID, Generation: cleanup.Intent.Generation}) ||
		request.FenceDigest != cleanup.FenceDigest || !request.Cleanup.Equal(cleanup) || !request.CloneGrant.Equal(cloneGrant) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux owner anchor clone cleanup worker is not bound to its exact request")
	}
	if rootFD < 0 || targetFD < -1 || request.TargetFDPresent != (targetFD >= 0) ||
		(request.TargetFDPresent && priorRemoval != nil) || (!request.TargetFDPresent && priorRemoval == nil) ||
		(request.PriorRemovalProof == nil) != (priorRemoval == nil) ||
		request.PriorRemovalProof != nil && !request.PriorRemovalProof.Equal(*priorRemoval) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux owner anchor clone cleanup descriptors differ from the exact prior-removal request")
	}
	if anchor.cleanupVerifier == nil {
		return authority.LinuxCloneTargetRemovalProof{}, errLinuxOwnerAnchorCleanupAuthority
	}
	if err := anchor.cleanupVerifier.Verify(
		request.RunKey.RunID, request.RunKey.Generation, cloneGrant, cleanup, priorRemoval, request.TargetFDPresent,
	); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(errLinuxOwnerAnchorCleanupAuthority, err)
	}
	if err := ctx.Err(); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("State cleanup readback outlasted its bounded anchor request: %w", err)
	}
	if anchor.cloneCleanupWorker == nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux owner anchor clone cleanup worker is unavailable")
	}
	return anchor.cloneCleanupWorker(ctx, rootFD, targetFD, cloneGrant, cleanup, priorRemoval)
}

func (anchor *linuxOwnerAnchor) serveCloneCleanup(
	connection *net.UnixConn,
	peer linuxOwnerAnchorPeer,
	request linuxOwnerAnchorRequest,
	receivedFDs []int,
) bool {
	wantFDs := 1
	if request.TargetFDPresent {
		wantFDs++
	}
	if len(receivedFDs) != wantFDs || request.Grant != (LinuxOwnerAnchorGrant{}) ||
		request.CloneGrant == nil || request.Cleanup == nil || request.Helper != nil || request.Creator != nil ||
		request.Process != nil || request.Role != "" || request.ReleaseProof != nil ||
		request.PreGateReleaseProof != nil || request.AbortProof != nil ||
		request.TargetFDPresent && request.PriorRemovalProof != nil || !request.TargetFDPresent && request.PriorRemovalProof == nil ||
		request.AfterPID != 0 ||
		request.RunKey != (LinuxOwnerAnchorRunKey{RunID: request.Cleanup.Intent.RunID, Generation: request.Cleanup.Intent.Generation}) ||
		request.Fence != (protocol.Fence{}) || request.FenceDigest != request.Cleanup.FenceDigest ||
		linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) {
		return false
	}
	expectedAnchor := authority.LinuxCgroupProcessIdentity{
		PID:      request.Cleanup.NormalReleaseProof.AnchorPID,
		Identity: request.Cleanup.NormalReleaseProof.AnchorIdentity,
	}
	if err := validateLinuxOwnerAnchorCloneCleanup(*request.CloneGrant, *request.Cleanup, anchor.owner.AUID); err != nil ||
		!expectedAnchor.Equal(anchor.identity) || request.Cleanup.NormalReleaseProof.CgroupIdentity == nil ||
		!request.Cleanup.NormalReleaseProof.CgroupIdentity.Parent.Equal(anchor.parent.Identity()) {
		anchor.writeResponse(connection, request, "request_rejected", nil, nil)
		return false
	}
	if !request.TargetFDPresent {
		if err := verifyLinuxOwnerAnchorPriorRemoval(receivedFDs[0], *request.Cleanup, request.PriorRemovalProof, anchor.owner); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return false
		}
	}
	if err := connection.SetDeadline(time.Now().Add(linuxOwnerAnchorCleanupTimeout)); err != nil {
		return false
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), anchor.managerPath); err != nil {
		anchor.writeResponse(connection, request, "request_rejected", nil, nil)
		return false
	}
	if err := anchor.parent.verifyPinnedParent(); err != nil {
		anchor.writeResponse(connection, request, "request_rejected", nil, nil)
		return false
	}
	if anchor.cloneCleanupWorkers.Add(1) > 1 {
		anchor.cloneCleanupWorkers.Add(-1)
		anchor.writeResponse(connection, request, "cleanup_busy", nil, nil)
		return false
	}
	rootCopy, err := unix.FcntlInt(uintptr(receivedFDs[0]), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		anchor.cloneCleanupWorkers.Add(-1)
		anchor.writeResponse(connection, request, "cleanup_unproven", nil, nil)
		return false
	}
	rootFile := os.NewFile(uintptr(rootCopy), "symmetry-owner-anchor-clone-root")
	if rootFile == nil {
		_ = unix.Close(rootCopy)
		anchor.cloneCleanupWorkers.Add(-1)
		anchor.writeResponse(connection, request, "cleanup_unproven", nil, nil)
		return false
	}
	targetFD := -1
	var targetFile *os.File
	if request.TargetFDPresent {
		targetCopy, duplicateErr := unix.FcntlInt(uintptr(receivedFDs[1]), unix.F_DUPFD_CLOEXEC, 0)
		if duplicateErr != nil {
			_ = rootFile.Close()
			anchor.cloneCleanupWorkers.Add(-1)
			anchor.writeResponse(connection, request, "cleanup_unproven", nil, nil)
			return false
		}
		targetFile = os.NewFile(uintptr(targetCopy), "symmetry-owner-anchor-clone-target")
		if targetFile == nil {
			_ = unix.Close(targetCopy)
			_ = rootFile.Close()
			anchor.cloneCleanupWorkers.Add(-1)
			anchor.writeResponse(connection, request, "cleanup_unproven", nil, nil)
			return false
		}
		targetFD = int(targetFile.Fd())
	}
	requestCopy := request
	cloneGrant := request.CloneGrant.Clone()
	cleanup := request.Cleanup.Clone()
	var priorRemoval *authority.LinuxCloneTargetRemovalProof
	if request.PriorRemovalProof != nil {
		priorCopy := request.PriorRemovalProof.Clone()
		priorRemoval = &priorCopy
	}
	go func() {
		defer func() {
			if targetFile != nil {
				_ = targetFile.Close()
			}
			_ = rootFile.Close()
			anchor.cloneCleanupWorkers.Add(-1)
			_ = connection.Close()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorCleanupTimeout)
		proof, err := anchor.runCloneCleanupAfterStateReadback(
			ctx, requestCopy, int(rootFile.Fd()), targetFD, cloneGrant, cleanup, priorRemoval,
		)
		cancel()
		if err != nil || proof.Validate() != nil || !proof.Intent.Equal(cleanup) ||
			priorRemoval == nil && proof.Disposition != authority.LinuxCloneTargetDispositionRemoved ||
			priorRemoval != nil && !proof.Equal(*priorRemoval) {
			status := "clone_cleanup_unproven"
			if errors.Is(err, errLinuxOwnerAnchorCleanupAuthority) {
				status = "cleanup_authority_unproven"
			}
			anchor.writeResponse(connection, requestCopy, status, nil, nil)
			return
		}
		anchor.writeResponseWithCloneRemoval(connection, requestCopy, "clone_removed", nil, nil, &proof)
	}()
	return true
}

func (anchor *linuxOwnerAnchor) writeResponse(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, evidence *LinuxOwnerAnchorReapEvidence) {
	anchor.writeResponseWithCloneRemoval(connection, request, status, helper, evidence, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithStopEvidence(
	connection *net.UnixConn,
	request linuxOwnerAnchorRequest,
	status string,
	evidence LinuxOwnerAnchorStopEvidence,
) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, StopEvidence: &evidence,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithCloneRemoval(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, evidence *LinuxOwnerAnchorReapEvidence, cloneRemoval *authority.LinuxCloneTargetRemovalProof) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: helper, Evidence: evidence, CloneRemoval: cloneRemoval,
	}
	if request.Operation == linuxOwnerAnchorOperationCloneCleanup && request.Cleanup != nil {
		response.RunKey = request.RunKey
		response.FenceDigest = request.FenceDigest
		response.LaunchToken = request.Cleanup.NormalReleaseProof.LaunchToken
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithFDs(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, evidence *LinuxOwnerAnchorReapEvidence, fds ...int) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: helper, Evidence: evidence,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame || len(fds) == 0 {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, syscall.UnixRights(fds...), nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithReapEvidence(
	connection *net.UnixConn,
	request linuxOwnerAnchorRequest,
	status string,
	evidence []LinuxOwnerAnchorReapEvidence,
	nextPID int,
	more bool,
	stopEvidence LinuxOwnerAnchorStopEvidence,
) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, ReapEvidence: evidence, StopEvidence: &stopEvidence, NextPID: nextPID, More: more,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithChildClassification(
	connection *net.UnixConn,
	request linuxOwnerAnchorRequest,
	status string,
	classification linuxOwnerAnchorRunChildClassification,
) {
	copy := classification
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: request.Helper, ChildClassification: &copy,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithHelperExit(connection *net.UnixConn, request linuxOwnerAnchorRequest, proof authority.LinuxCgroupExitProof) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: "reaped",
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: request.Helper, HelperExit: &proof,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithRunPins(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, helperExit *authority.LinuxCgroupExitProof, fds ...int) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: helper, HelperExit: helperExit,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame || len(fds) == 0 {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, syscall.UnixRights(fds...), nil)
}

func parseLinuxOwnerAnchorRights(oob []byte, flags int) ([]int, error) {
	if len(oob) == 0 {
		if flags&unix.MSG_CTRUNC != 0 {
			return nil, errors.New("Linux owner anchor ancillary data was truncated")
		}
		return nil, nil
	}
	messages, err := syscall.ParseSocketControlMessage(oob)
	var fds []int
	for _, message := range messages {
		if message.Header.Level != syscall.SOL_SOCKET || message.Header.Type != syscall.SCM_RIGHTS {
			closeLinuxOwnerAnchorFDs(fds)
			return nil, errors.New("Linux owner anchor received unsupported ancillary data")
		}
		values, err := syscall.ParseUnixRights(&message)
		if err != nil {
			closeLinuxOwnerAnchorFDs(fds)
			return nil, fmt.Errorf("parse Linux owner anchor descriptor transfer: %w", err)
		}
		fds = append(fds, values...)
	}
	if err != nil {
		closeLinuxOwnerAnchorFDs(fds)
		return nil, fmt.Errorf("parse Linux owner anchor ancillary data: %w", err)
	}
	if flags&unix.MSG_CTRUNC != 0 {
		closeLinuxOwnerAnchorFDs(fds)
		return nil, errors.New("Linux owner anchor ancillary data was truncated")
	}
	for _, fd := range fds {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
			closeLinuxOwnerAnchorFDs(fds)
			return nil, fmt.Errorf("mark received Linux owner anchor descriptor close-on-exec: %w", err)
		}
	}
	return fds, nil
}

func closeLinuxOwnerAnchorFDs(fds []int) {
	for _, fd := range fds {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	}
}
