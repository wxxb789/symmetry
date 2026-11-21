//go:build linux && amd64

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

// LinuxOwnerAnchorCleanupVerifier is the read-only State seam used immediately
// before the anchor delegates an exact clone cleanup to the root worker. The
// implementation is supplied by cmd/symmetry-daemon to avoid a platform→State
// import cycle; Close releases its pinned StateDir/runs descriptors.
type LinuxOwnerAnchorCleanupVerifier interface {
	Verify(runID string, generation int64, grant authority.LinuxCloneGrant, intent authority.LinuxCloneCleanupIntent, prior *authority.LinuxCloneTargetRemovalProof, targetFDPresent bool) error
	Close() error
}

// LinuxOwnerAnchorCleanupVerifierFactory opens the State-owned read-only
// verifier from the root-installed StateDir. RunLinuxOwnerAnchor requires the
// factory; v2 config pins its reader before READY, while strict legacy v1 config
// has no StateDir and therefore keeps clone cleanup disabled.
type LinuxOwnerAnchorCleanupVerifierFactory func(stateDir string) (LinuxOwnerAnchorCleanupVerifier, error)

type linuxOwnerAnchorCloneCleanupWorker func(
	ctx context.Context,
	rootFD, targetFD int,
	grant authority.LinuxCloneGrant,
	intent authority.LinuxCloneCleanupIntent,
	prior *authority.LinuxCloneTargetRemovalProof,
) (authority.LinuxCloneTargetRemovalProof, error)

// LinuxOwnerAnchorIdentity is an authenticated description of the live owner
// service. Parent is identity data only; Discover does not return a parent FD
// or re-establish pin authority after the caller loses its own live pin.
type LinuxOwnerAnchorIdentity struct {
	Process     authority.LinuxCgroupProcessIdentity
	Parent      authority.LinuxCgroupParentIdentity
	OwnerUID    uint32
	ManagerPath string
}

func linuxOwnerAnchorPIDFDReady(pidfd *os.File) (bool, error) {
	if pidfd == nil {
		return false, errors.New("Linux owner anchor helper pidfd is unavailable")
	}
	poll := [1]unix.PollFd{{Fd: int32(pidfd.Fd()), Events: unix.POLLIN}}
	for {
		poll[0].Revents = 0
		_, err := unix.Poll(poll[:], 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("poll exact Linux owner helper pidfd: %w", err)
		}
		if poll[0].Revents&unix.POLLNVAL != 0 {
			return false, errors.New("exact Linux owner helper pidfd became invalid")
		}
		return poll[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
	}
}

func (anchor *linuxOwnerAnchor) verifyNoForeignRunProcessInLeaf(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || !run.grant.Anchor.Equal(anchor.identity) {
		return errors.New("foreign-run leaf membership check lacks exact anchor authority")
	}
	processes, err := readLinuxOwnerAnchorRunLeafProcessIDs(ctx, run.leaf, run.grant, run.owner.AUID)
	if err != nil {
		return fmt.Errorf("read bounded recursive pinned-leaf process set before cgroup.kill: %w", err)
	}
	pids := make([]int, 0, len(processes))
	for pid := range processes {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := ProcessIdentity(pid)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("capture exact pinned-leaf process identity before cgroup.kill: %w", err)
		}
		process := authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
		pidfd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("open exact pinned-leaf process pidfd before cgroup.kill: %w", err)
		}
		classification, classifyErr := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
		if classifyErr != nil {
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return fmt.Errorf("classify exact pinned-leaf process before cgroup.kill: %w", classifyErr)
		}
		switch classification.Origin {
		case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
			if classification.Role == "launcher" {
				_ = unix.Close(pidfd)
				run.migrationObserved = true
				return errors.New("registered root launcher is inside the exact run leaf before cgroup.kill")
			}
		case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
			beforePath, err := readLinuxOwnerProcessCgroupPath(pid)
			if err != nil {
				_ = unix.Close(pidfd)
				run.migrationObserved = true
				return fmt.Errorf("recheck namespace-proven current-run process cgroup during leaf census: %w", err)
			}
			if !linuxOwnerCgroupPathWithin(beforePath, leafPath) {
				_ = unix.Close(pidfd)
				run.migrationObserved = true
				continue
			}
			if _, exists := run.children[pid]; !exists {
				if err := run.trackNamespaceProvenChild(process, pidfd, classification); err != nil {
					_ = unix.Close(pidfd)
					run.migrationObserved = true
					return fmt.Errorf("retain exact namespace-proven leaf process before cgroup.kill: %w", err)
				}
			}
		case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly:
			if err := run.trackPreReleaseGatePhysicalOnlyChild(process, pidfd, classification); err != nil {
				_ = unix.Close(pidfd)
				run.migrationObserved = true
				return fmt.Errorf("retain pre-RegisterGate physical-only gate before cgroup.kill: %w", err)
			}
		case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign,
			linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return errors.New("foreign RunKey process is inside this exact run leaf before cgroup.kill")
		case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			continue
		default:
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return errors.New("unattributable process is inside the exact run leaf before cgroup.kill")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return fmt.Errorf("recheck pinned-leaf process pidfd before cgroup.kill: %w", err)
		}
		if actual, err := ProcessIdentity(pid); err != nil || actual != process.Identity {
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return fmt.Errorf("recheck pinned-leaf process identity before cgroup.kill: %w", errOr(err, errors.New("creation identity differs")))
		}
		cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
		_ = unix.Close(pidfd)
		if err != nil || !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return fmt.Errorf("recheck pinned-leaf process membership before cgroup.kill: %w", errOr(err, errors.New("process left the exact run leaf")))
		}
	}
	return nil
}

func (anchor *linuxOwnerAnchor) stopRunLeaf(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	fence *LinuxOwnerAnchorProducerFence,
) (LinuxOwnerAnchorStopEvidence, error) {
	if anchor == nil || ctx == nil || run == nil || run.retired || run.leaf == nil || run.helperPIDFD == nil || run.grant.Validate() != nil {
		return LinuxOwnerAnchorStopEvidence{}, ErrLinuxCgroupV2Unresolved
	}
	if err := ctx.Err(); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, err
	}
	preWriteRetry := run.stopLeafAttempted && !run.leafKillAttempted && !run.leafKillAcknowledged && !run.leafKillCallStarted
	if fence != nil && (!run.stopLeafAttempted || preWriteRetry) {
		if err := anchor.verifyStopProducerFence(ctx, run, *fence); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, err
		}
	} else if fence == nil && (run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity)) {
		return LinuxOwnerAnchorStopEvidence{}, errors.New("anchor takeover StopLeaf lacks exact helper exit/reap evidence")
	}
	if run.stopLeafAttempted {
		cached, err := run.cachedStopLeaf(fence)
		if err != nil {
			if run.leafKillAttempted && !run.leafKillAcknowledged {
				if emptyErr := run.leaf.WaitEmpty(ctx); emptyErr == nil {
					run.leafEmptyObserved = true
				}
			}
			return LinuxOwnerAnchorStopEvidence{}, err
		}
		if cached != nil {
			if err := anchor.remeasureStopLeaf(ctx, run); err != nil {
				return LinuxOwnerAnchorStopEvidence{}, err
			}
			return *cached, nil
		}
		if preWriteRetry {
			if fence == nil {
				if err := anchor.fenceRunLaunchers(ctx, run); err != nil {
					return LinuxOwnerAnchorStopEvidence{}, err
				}
			}
			if err := anchor.verifyNoForeignRunProcessInLeaf(ctx, run); err != nil {
				return LinuxOwnerAnchorStopEvidence{}, err
			}
			if err := anchor.establishRunNoMigrationProof(ctx, run, fence); err != nil {
				return LinuxOwnerAnchorStopEvidence{}, err
			}
		}
	} else {
		if fence == nil {
			if err := anchor.verifyNoUnknownRunProducer(ctx, run); err != nil {
				return LinuxOwnerAnchorStopEvidence{}, err
			}
		}
		if err := anchor.verifyNoForeignRunProcessInLeaf(ctx, run); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, err
		}
		if err := anchor.establishRunNoMigrationProof(ctx, run, fence); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, err
		}
		if err := ctx.Err(); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, err
		}
		run.stopLeafAttempted = true
		run.stopRequested = true
		if fence != nil {
			copy := fence.Clone()
			run.stopFence = &copy
		}
	}
	if !run.leafKillAcknowledged {
		if run.leafKillAttempted {
			return LinuxOwnerAnchorStopEvidence{}, errors.New("previous cgroup.kill write result is unknown; no retry is permitted")
		}
		if err := run.leaf.VerifyKillControl(); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("verify exact cgroup.kill control before one-use StopLeaf: %w", err)
		}
		if err := run.writeCgroupKillOnce(ctx, run.leaf.Kill); err != nil {
			if run.leafKillAttempted {
				if emptyErr := run.leaf.WaitEmpty(ctx); emptyErr == nil {
					run.leafEmptyObserved = true
				}
			}
			return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("anchor cgroup.kill attempt is not positively acknowledged and cannot be replayed: %w", err)
		}
	}
	if err := run.leaf.WaitEmpty(ctx); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("wait for exact recursive leaf empty after anchor cgroup.kill: %w", err)
	}
	run.leafEmptyObserved = true
	if _, err := linuxOwnerAnchorPinnedLeafPresent(run); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("remeasure exact pinned leaf after anchor cgroup.kill: %w", err)
	}
	evidence := makeLinuxOwnerAnchorStopEvidence(run, anchor.identity)
	if err := evidence.ValidateFor(run.grant, run.helper, anchor.identity, nil, false, run.gateUserNamespaceFD); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, err
	}
	copy := evidence.Clone()
	run.stopEvidence = &copy
	return evidence, nil
}

func waitLinuxOwnerAnchorPIDFDReady(ctx context.Context, pidfd *os.File) error {
	if ctx == nil || pidfd == nil {
		return errors.New("wait for exact process pidfd requires a live context and pidfd")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ready, err := linuxOwnerAnchorPIDFDReady(pidfd)
		if err != nil || ready {
			return err
		}
		timer := time.NewTimer(linuxOwnerAnchorWaitPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func verifyLinuxOwnerAProcess(pid int, expectedIdentity string, uid, gid uint32, expectedParent int, expectedCgroup string) error {
	if pid <= 0 || expectedIdentity == "" || uid == 0 || gid == 0 {
		return errors.New("Linux owner A process identity or credentials are incomplete")
	}
	actualIdentity, err := ProcessIdentity(pid)
	if err != nil || actualIdentity != expectedIdentity {
		return fmt.Errorf("Linux owner A process creation identity mismatch: %w", errOr(err, errors.New("identity differs")))
	}
	credentials, err := readLinuxOwnerProcessCredentials(pid)
	if err != nil {
		return err
	}
	if credentials.UIDs != [4]uint32{uid, uid, uid, uid} || credentials.GIDs != [4]uint32{gid, gid, gid, gid} {
		return errors.New("Linux owner A process does not retain the configured real/effective/saved/filesystem UID/GID")
	}
	if expectedParent > 0 && credentials.Parent != expectedParent {
		return errors.New("Linux owner A process ancestry does not match the exact expected parent")
	}
	if expectedCgroup != "" {
		cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
		if err != nil {
			return err
		}
		if cgroupPath != expectedCgroup {
			return errors.New("Linux owner A process is not in the exact configured cgroup subgroup")
		}
	}
	actualIdentity, err = ProcessIdentity(pid)
	if err != nil || actualIdentity != expectedIdentity {
		return fmt.Errorf("Linux owner A process identity changed during credential check: %w", errOr(err, errors.New("creation identity differs")))
	}
	return nil
}

type linuxOwnerProcessCredentials struct {
	UIDs   [4]uint32
	GIDs   [4]uint32
	Parent int
}

func readLinuxOwnerProcessCredentials(pid int) (linuxOwnerProcessCredentials, error) {
	if pid <= 0 {
		return linuxOwnerProcessCredentials{}, errors.New("Linux owner process PID must be positive")
	}
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return linuxOwnerProcessCredentials{}, fmt.Errorf("read Linux owner process %d status: %w", pid, err)
	}
	var result linuxOwnerProcessCredentials
	var gotUIDs, gotGIDs, gotParent bool
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch key {
		case "Uid":
			if gotUIDs {
				return linuxOwnerProcessCredentials{}, errors.New("Linux owner process status duplicates Uid")
			}
			values, err := parseLinuxOwnerIDs(value, 4)
			if err != nil {
				return linuxOwnerProcessCredentials{}, fmt.Errorf("parse Linux owner process Uid: %w", err)
			}
			copy(result.UIDs[:], values)
			gotUIDs = true
		case "Gid":
			if gotGIDs {
				return linuxOwnerProcessCredentials{}, errors.New("Linux owner process status duplicates Gid")
			}
			values, err := parseLinuxOwnerIDs(value, 4)
			if err != nil {
				return linuxOwnerProcessCredentials{}, fmt.Errorf("parse Linux owner process Gid: %w", err)
			}
			copy(result.GIDs[:], values)
			gotGIDs = true
		case "PPid":
			if gotParent {
				return linuxOwnerProcessCredentials{}, errors.New("Linux owner process status duplicates PPid")
			}
			parent, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || parent < 0 {
				return linuxOwnerProcessCredentials{}, errors.New("Linux owner process PPid is invalid")
			}
			result.Parent = parent
			gotParent = true
		}
	}
	if !gotUIDs || !gotGIDs || !gotParent {
		return linuxOwnerProcessCredentials{}, errors.New("Linux owner process status lacks Uid/Gid/PPid")
	}
	return result, nil
}

func parseLinuxOwnerIDs(value string, count int) ([]uint32, error) {
	fields := strings.Fields(value)
	if len(fields) != count {
		return nil, errors.New("credential field has wrong number of IDs")
	}
	result := make([]uint32, count)
	for index, field := range fields {
		parsed, err := strconv.ParseUint(field, 10, 32)
		if err != nil || strconv.FormatUint(parsed, 10) != field {
			return nil, errors.New("credential ID is not canonical unsigned decimal")
		}
		result[index] = uint32(parsed)
	}
	return result, nil
}

func readLinuxOwnerProcessCgroupPath(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("Linux cgroup membership PID must be positive")
	}
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", fmt.Errorf("read Linux owner process %d cgroup membership: %w", pid, err)
	}
	var result string
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 || fields[0] == "" {
			return "", errors.New("Linux process cgroup membership contains a malformed line")
		}
		if fields[0] == "0" && fields[1] == "" {
			if result != "" || !canonicalCgroupHierarchyPath(fields[2]) {
				return "", errors.New("Linux unified cgroup membership is missing, duplicated, or noncanonical")
			}
			result = fields[2]
		}
	}
	if result == "" {
		return "", errors.New("Linux process has no unified cgroup-v2 membership")
	}
	return result, nil
}

func verifyLinuxOwnerAnchorManagerMembership(pid int, managerPath string) error {
	if managerPath == "" || managerPath == "/" {
		return errors.New("Linux owner anchor requires a non-root manager subgroup")
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
	if err != nil {
		return err
	}
	if cgroupPath != managerPath {
		return fmt.Errorf("Linux owner anchor process cgroup %q differs from pinned manager subgroup", cgroupPath)
	}
	return nil
}

func linuxOwnerCgroupPathsOverlap(left, right string) bool {
	return left == right || linuxOwnerCgroupPathWithin(left, right) || linuxOwnerCgroupPathWithin(right, left)
}

func linuxOwnerCgroupPathWithin(candidate, parent string) bool {
	if parent == "/" {
		return strings.HasPrefix(candidate, "/")
	}
	return candidate == parent || strings.HasPrefix(candidate, parent+"/")
}

func ensureLinuxOwnerAnchorSubreaper() error {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("enable independent Linux owner-anchor child subreaper: %w", err)
	}
	var enabled int32
	_, _, errno := unix.Syscall6(unix.SYS_PRCTL, uintptr(unix.PR_GET_CHILD_SUBREAPER), uintptr(unsafe.Pointer(&enabled)), 0, 0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("read back Linux owner-anchor subreaper state: %w", errno)
	}
	if enabled != 1 {
		return errors.New("Linux owner-anchor process did not retain PR_SET_CHILD_SUBREAPER")
	}
	return nil
}

func verifyLinuxOwnerAnchorLeafFD(fd int, expected authority.LinuxCgroupIdentity, ownerUID uint32) error {
	if fd < 0 || expected.Validate() != nil {
		return errors.New("Linux owner anchor leaf FD or identity is invalid")
	}
	if err := requireLinuxCgroup2FD(fd); err != nil {
		return err
	}
	stat, err := statLinuxCgroupFD(fd)
	if err != nil {
		return err
	}
	if ownerUID == 0 || stat.mount != expected.Parent.MountID || stat.device != expected.LeafDevice || stat.inode != expected.LeafInode || stat.uid != ownerUID {
		return errors.New("Linux owner anchor leaf FD does not match the exact A-owned reserved identity")
	}
	return nil
}

func verifyLinuxOwnerAnchorPIDFD(fd int, expected authority.LinuxCgroupProcessIdentity) error {
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(fd, expected); err != nil {
		return err
	}
	if err := unix.PidfdSendSignal(fd, 0, nil, 0); err != nil {
		return fmt.Errorf("probe exact Linux owner anchor pidfd: %w", err)
	}
	return nil
}

// The root-owned launcher deliberately runs in a PID namespace that A cannot
// inspect. Its exact pidfd plus the authenticated READY identity bind the
// process; re-reading /proc/<pid>/ns/pid from A would reject every real
// launcher on hosts that restrict root namespace access.
func verifyLinuxOwnerAnchorChildPIDFD(fd int, expected authority.LinuxCgroupProcessIdentity, role string) error {
	if role != "launcher" {
		return verifyLinuxOwnerAnchorPIDFD(fd, expected)
	}
	if err := verifyLinuxOwnerAnchorPIDFDTarget(fd, expected); err != nil {
		return err
	}
	if err := unix.PidfdSendSignal(fd, 0, nil, 0); err != nil {
		return fmt.Errorf("probe exact Linux owner launcher pidfd: %w", err)
	}
	return nil
}

func verifyLinuxOwnerAnchorPIDFDIdentity(fd int, expected authority.LinuxCgroupProcessIdentity) error {
	if err := verifyLinuxOwnerAnchorPIDFDTarget(fd, expected); err != nil {
		return err
	}
	identity, err := ProcessIdentity(expected.PID)
	if err != nil || identity != expected.Identity {
		return fmt.Errorf("Linux owner anchor pidfd creation identity mismatch: %w", errOr(err, errors.New("identity differs")))
	}
	return nil
}

func verifyLinuxOwnerAnchorPIDFDTarget(fd int, expected authority.LinuxCgroupProcessIdentity) error {
	if fd < 0 || expected.Validate() != nil {
		return errors.New("Linux owner anchor pidfd or process identity is invalid")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		return errors.Join(errors.New("Linux owner anchor pidfd is not close-on-exec"), err)
	}
	pid, err := readLinuxOwnerPIDFDTarget(fd)
	if err != nil || pid != expected.PID {
		return fmt.Errorf("Linux owner anchor pidfd target mismatch: %w", errOr(err, errors.New("PID differs")))
	}
	return nil
}

func readLinuxOwnerPIDFDTarget(fd int) (int, error) {
	if fd < 0 {
		return 0, errors.New("Linux pidfd is invalid")
	}
	contents, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil {
		return 0, fmt.Errorf("read exact Linux pidfd fdinfo: %w", err)
	}
	var pid int
	found := false
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || key != "Pid" {
			continue
		}
		if found {
			return 0, errors.New("Linux pidfd fdinfo contains duplicate PID fields")
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return 0, errors.New("Linux pidfd fdinfo has no live positive target PID")
		}
		pid, found = parsed, true
	}
	if !found {
		return 0, errors.New("Linux pidfd fdinfo omits target PID")
	}
	return pid, nil
}

// linuxOwnerAnchorWaitPIDFD waits and reaps only a pidfd-bound child. ECHILD,
// missing procfs identity, a replaced numeric PID, or a different pidfd target
// is unresolved evidence, never a successful wait. Membership and RunKey
// binding are checked by linuxOwnerAnchorRun before this kernel wait primitive.
func linuxOwnerAnchorWaitPIDFD(ctx context.Context, pidfd *os.File, expected authority.LinuxCgroupProcessIdentity) error {
	if ctx == nil || pidfd == nil || expected.Validate() != nil {
		return errors.New("Linux owner anchor wait requires a context, exact process identity, and pidfd")
	}
	bounded, cancel := context.WithTimeout(ctx, linuxOwnerAnchorWaitTimeout)
	defer cancel()
	fd := int(pidfd.Fd())
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(fd, expected); err != nil {
		return err
	}
	for {
		if err := bounded.Err(); err != nil {
			return fmt.Errorf("wait for exact Linux owner anchor child: %w", err)
		}
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.ECHILD) {
			return fmt.Errorf("wait for exact Linux owner anchor child: %w (ECHILD is not positive reap evidence)", err)
		}
		if err != nil {
			return fmt.Errorf("waitid exact Linux owner anchor pidfd: %w", err)
		}
		if info.Signo != 0 {
			if info.Signo != int32(unix.SIGCHLD) {
				return errors.New("waitid exact Linux owner pidfd returned an unexpected signal record")
			}
			identity, identityErr := ProcessIdentity(expected.PID)
			if identityErr != nil || identity != expected.Identity {
				return fmt.Errorf("verify exact Linux owner child identity before reap: %w", errOr(identityErr, errors.New("creation identity changed")))
			}
			for {
				var reaped unix.Siginfo
				err := unix.Waitid(unix.P_PIDFD, fd, &reaped, unix.WEXITED, nil)
				if errors.Is(err, unix.EINTR) {
					continue
				}
				if err != nil {
					return fmt.Errorf("reap exact Linux owner anchor child: %w", err)
				}
				if reaped.Signo != int32(unix.SIGCHLD) {
					return errors.New("reap exact Linux owner pidfd returned no SIGCHLD wait record")
				}
				return nil
			}
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		remaining := linuxOwnerAnchorWaitPoll
		if deadline, ok := bounded.Deadline(); ok && time.Until(deadline) < remaining {
			remaining = time.Until(deadline)
		}
		if remaining <= 0 {
			return context.DeadlineExceeded
		}
		_, err = unix.Poll(poll, int(remaining/time.Millisecond)+1)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return fmt.Errorf("poll exact Linux owner anchor pidfd: %w", err)
		}
	}
}

func duplicateLinuxOwnerAnchorLeaf(leaf *LinuxCgroupV2Leaf) (*LinuxCgroupV2Leaf, error) {
	if leaf == nil {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := leaf.verifyLive(); err != nil {
		return nil, err
	}
	parentFD, err := unix.FcntlInt(uintptr(leaf.parentFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate Linux owner anchor leaf parent pin: %w", err)
	}
	leafFD, err := unix.FcntlInt(uintptr(leaf.leafFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("duplicate Linux owner anchor leaf pin: %w", err)
	}
	return &LinuxCgroupV2Leaf{parentFD: parentFD, leafFD: leafFD, identity: leaf.identity, ownerUID: leaf.ownerUID}, nil
}

// linuxOwnerAnchorPinnedLeafPresent validates both retained FDs before using
// the pinned parent to distinguish the exact original leaf from absence or a
// same-name replacement. Exact absence is the kernel's rmdir-backed empty
// observation; serialized caller booleans never supply it.
func linuxOwnerAnchorPinnedLeafPresent(run *linuxOwnerAnchorRun) (bool, error) {
	if run == nil || run.leaf == nil || run.grant.Validate() != nil ||
		run.leaf.OwnerUID() != run.grant.OwnerUID || !run.leaf.Identity().Equal(run.grant.Leaf) {
		return false, errors.New("Linux owner anchor has no exact pinned run leaf")
	}
	leaf := run.leaf
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if leaf.parentFD < 0 || leaf.leafFD < 0 || leaf.ownerUID != run.grant.OwnerUID || !leaf.identity.Equal(run.grant.Leaf) {
		return false, ErrLinuxCgroupV2Unresolved
	}
	if err := requireLinuxCgroup2FD(leaf.parentFD); err != nil {
		return false, fmt.Errorf("verify pinned run parent filesystem: %w", err)
	}
	if err := requireLinuxCgroup2FD(leaf.leafFD); err != nil {
		return false, fmt.Errorf("verify pinned run leaf filesystem: %w", err)
	}
	parentStat, err := statLinuxCgroupFD(leaf.parentFD)
	if err != nil || parentStat.mount != run.grant.Parent.MountID || parentStat.device != run.grant.Parent.Device ||
		parentStat.inode != run.grant.Parent.Inode || parentStat.uid != run.grant.OwnerUID {
		return false, fmt.Errorf("pinned Linux owner parent identity changed: %w", errOr(err, ErrLinuxCgroupV2Unresolved))
	}
	leafStat, err := statLinuxCgroupFD(leaf.leafFD)
	if err != nil || leafStat.mount != run.grant.Leaf.Parent.MountID || leafStat.device != run.grant.Leaf.LeafDevice ||
		leafStat.inode != run.grant.Leaf.LeafInode || leafStat.uid != run.grant.OwnerUID ||
		leafStat.mode&unix.S_IFMT != unix.S_IFDIR {
		return false, fmt.Errorf("pinned Linux owner leaf identity changed: %w", errOr(err, ErrLinuxCgroupV2Unresolved))
	}
	var named unix.Stat_t
	err = unix.Fstatat(leaf.parentFD, run.grant.Leaf.LeafName, &named, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		// A cgroup2 directory cannot be removed while populated. If the old
		// directory FD still exposes cgroup.events, reject any contradiction;
		// path absence itself remains the exact pinned-parent removal result.
		if populated, readErr := readLinuxCgroupDirectoryPopulated(leaf.leafFD,
			run.grant.Parent.MountID, run.grant.Parent.Device, run.grant.OwnerUID); readErr == nil && populated {
			return false, errors.New("exact pinned run leaf is absent but still reports populated")
		}
		run.leafRemovedObserved = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read exact pinned run leaf name under its parent: %w", err)
	}
	if uint64(named.Dev) != leafStat.device || named.Ino != leafStat.inode || named.Uid != run.grant.OwnerUID ||
		named.Mode&unix.S_IFMT != unix.S_IFDIR {
		return false, errors.New("same-name Linux owner leaf differs from the exact retained cgroup pin")
	}
	if err := requireLinuxCgroupNameAt(leaf.parentFD, run.grant.Leaf.LeafName, leafStat); err != nil {
		return false, err
	}
	return true, nil
}

type linuxOwnerAnchorNamespacePin struct {
	runKey    LinuxOwnerAnchorRunKey
	namespace LinuxUserNamespaceIdentity
	fd        *os.File
}

func readLinuxOwnerAnchorProcText(pid int, name string, maxBytes int64) (string, error) {
	if pid <= 1 || maxBytes <= 0 || (name != "uid_map" && name != "gid_map" && name != "setgroups" && name != "status") {
		return "", errors.New("Linux owner anchor proc read requires a fixed filename, positive PID, and byte bound")
	}
	file, err := os.Open(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return "", fmt.Errorf("open exact Linux owner process %s: %w", name, err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read exact Linux owner process %s: %w", name, err)
	}
	if int64(len(contents)) > maxBytes {
		return "", fmt.Errorf("exact Linux owner process %s exceeds its bound", name)
	}
	return string(contents), nil
}

func verifyLinuxOwnerAnchorNativePrivilegeState(pid int) error {
	contents, err := readLinuxOwnerAnchorProcText(pid, "status", 1<<20)
	if err != nil {
		return err
	}
	var groupsSeen, noNewPrivsSeen bool
	var capabilitySeen [5]bool
	var noNewPrivs uint64
	var capabilities [5]uint64
	capabilityFields := [...]string{"CapEff", "CapPrm", "CapInh", "CapBnd", "CapAmb"}
	for _, line := range strings.Split(contents, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch key {
		case "Groups":
			if groupsSeen || len(strings.Fields(value)) != 0 {
				return errors.New("Linux native process has supplementary groups or duplicate Groups status")
			}
			groupsSeen = true
		case "NoNewPrivs":
			if noNewPrivsSeen {
				return errors.New("Linux native process status duplicates NoNewPrivs")
			}
			parsed, parseErr := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
			if parseErr != nil {
				return errors.New("Linux native process NoNewPrivs status is malformed")
			}
			noNewPrivs, noNewPrivsSeen = parsed, true
		default:
			for index, name := range capabilityFields {
				if key != name {
					continue
				}
				if capabilitySeen[index] {
					return fmt.Errorf("Linux native process status duplicates %s", name)
				}
				parsed, parseErr := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
				if parseErr != nil {
					return fmt.Errorf("Linux native process %s status is malformed", name)
				}
				capabilities[index], capabilitySeen[index] = parsed, true
			}
		}
	}
	if !groupsSeen || !noNewPrivsSeen || noNewPrivs != 1 {
		return errors.New("Linux native process status lacks empty Groups or NoNewPrivs=1")
	}
	for index, name := range capabilityFields {
		if !capabilitySeen[index] || capabilities[index] != 0 {
			return fmt.Errorf("Linux native process %s is missing or nonzero", name)
		}
	}
	return nil
}

func verifyLinuxOwnerAnchorLeaf(leaf *LinuxCgroupV2Leaf, expected authority.LinuxCgroupIdentity, ownerUID uint32) error {
	if leaf == nil || leaf.OwnerUID() != ownerUID || !leaf.Identity().Equal(expected) {
		return errors.New("Linux owner anchor leaf pin differs from the exact run grant")
	}
	return leaf.verifyLive()
}

func readLinuxOwnerAnchorRunLeafProcessIDs(
	ctx context.Context,
	leaf *LinuxCgroupV2Leaf,
	grant LinuxOwnerAnchorGrant,
	ownerUID uint32,
) (map[int]struct{}, error) {
	if ctx == nil || leaf == nil || grant.Validate() != nil || ownerUID == 0 ||
		ownerUID != grant.OwnerUID || !leaf.Identity().Equal(grant.Leaf) {
		return nil, errors.New("anchor recursive leaf census is not bound to its exact run grant")
	}
	leafPin, err := leaf.DuplicateFD()
	if err != nil {
		return nil, fmt.Errorf("duplicate exact leaf pin for anchor membership census: %w", err)
	}
	defer leafPin.Close()
	rootFD := int(leafPin.Fd())
	parent := grant.Parent
	leafIdentity := grant.Leaf
	seenDirectories := make(map[[2]uint64]struct{})
	processes := make(map[int]struct{})
	directoryCount := 1
	var totalProcsBytes int64
	var scan func(int, int) error
	scan = func(directoryFD, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > linuxOwnerHelperV2MaxMembershipDepth {
			return errors.New("anchor run-leaf census exceeds its bounded nested-cgroup depth")
		}
		stat, err := statLinuxCgroupFD(directoryFD)
		if err != nil || stat.mount != parent.MountID || stat.device != parent.Device || stat.uid != ownerUID ||
			stat.mode&unix.S_IFMT != unix.S_IFDIR || stat.mode&0300 != 0300 {
			return fmt.Errorf("verify exact anchor run-leaf subtree directory: %w", errOr(err, errors.New("mount, device, owner, or mode differs")))
		}
		if directoryFD == rootFD && (stat.device != leafIdentity.LeafDevice || stat.inode != leafIdentity.LeafInode) {
			return errors.New("anchor run-leaf census root differs from the exact reserved inode")
		}
		directoryKey := [2]uint64{stat.device, stat.inode}
		if _, duplicate := seenDirectories[directoryKey]; duplicate {
			return errors.New("anchor run-leaf census encountered a duplicate cgroup directory identity")
		}
		seenDirectories[directoryKey] = struct{}{}
		procsFD, err := openLinuxCgroupControlAt(directoryFD, "cgroup.procs", unix.O_RDONLY)
		if err != nil {
			return fmt.Errorf("open pinned anchor run-leaf cgroup.procs: %w", err)
		}
		if err := verifyLinuxCgroupControlFileOnMount(procsFD, parent.MountID, parent.Device); err != nil {
			_ = unix.Close(procsFD)
			return err
		}
		procsFile := os.NewFile(uintptr(procsFD), "symmetry-owner-anchor-leaf-cgroup-procs")
		if procsFile == nil {
			_ = unix.Close(procsFD)
			return errors.New("wrap pinned anchor run-leaf cgroup.procs")
		}
		remainingBytes := int64(linuxOwnerHelperV2MaxCgroupProcsBytes) - totalProcsBytes
		if remainingBytes < 0 {
			_ = procsFile.Close()
			return errors.New("anchor run-leaf census exhausted its aggregate cgroup.procs byte bound")
		}
		contents, readErr := io.ReadAll(io.LimitReader(procsFile, remainingBytes+1))
		closeErr := procsFile.Close()
		if readErr != nil || closeErr != nil || int64(len(contents)) > remainingBytes {
			return errors.Join(errors.New("read bounded pinned anchor run-leaf cgroup.procs"), readErr, closeErr)
		}
		totalProcsBytes += int64(len(contents))
		for _, field := range strings.Fields(string(contents)) {
			pid, parseErr := strconv.Atoi(field)
			if parseErr != nil || pid <= 1 || strconv.Itoa(pid) != field {
				return errors.New("anchor run-leaf cgroup.procs contains a noncanonical PID")
			}
			if _, duplicate := processes[pid]; duplicate {
				return errors.New("anchor run-leaf cgroup.procs repeats a PID across nested cgroups")
			}
			processes[pid] = struct{}{}
			if len(processes) > linuxOwnerHelperV2MaxMembershipProcesses {
				return errors.New("anchor run-leaf census exceeds its bounded process count")
			}
		}
		names, err := readLinuxCgroupDirectoryNames(directoryFD)
		if err != nil {
			return fmt.Errorf("enumerate nested anchor run-leaf cgroups: %w", err)
		}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			if name == "." || name == ".." {
				continue
			}
			var childStat unix.Stat_t
			if err := unix.Fstatat(directoryFD, name, &childStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return fmt.Errorf("stat nested anchor run-leaf cgroup: %w", err)
			}
			if childStat.Mode&unix.S_IFMT != unix.S_IFDIR {
				continue
			}
			directoryCount++
			if directoryCount > linuxOwnerHelperV2MaxMembershipCgroups {
				return errors.New("anchor run-leaf census exceeds its bounded nested-cgroup count")
			}
			childFD, err := unix.Openat2(directoryFD, name, &unix.OpenHow{
				Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
				Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
			})
			if err != nil {
				return fmt.Errorf("open nested anchor run-leaf cgroup without following links: %w", err)
			}
			childIdentity, identityErr := statLinuxCgroupFD(childFD)
			if identityErr != nil || childIdentity.mount != parent.MountID || childIdentity.device != parent.Device ||
				childIdentity.inode != childStat.Ino || childIdentity.uid != ownerUID ||
				childIdentity.mode&unix.S_IFMT != unix.S_IFDIR || childIdentity.mode&0300 != 0300 {
				_ = unix.Close(childFD)
				return fmt.Errorf("verify nested anchor run-leaf directory pin: %w", errOr(identityErr, errors.New("directory identity changed")))
			}
			scanErr := scan(childFD, depth+1)
			closeErr := unix.Close(childFD)
			if scanErr != nil || closeErr != nil {
				return errors.Join(scanErr, closeErr)
			}
		}
		return nil
	}
	if err := scan(rootFD, 0); err != nil {
		return nil, err
	}
	if err := leaf.verifyLive(); err != nil {
		return nil, fmt.Errorf("revalidate exact leaf after anchor membership census: %w", err)
	}
	return processes, nil
}
