//go:build linux && amd64

package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func TestLinuxOwnerHelperV2RunTreeOrderCoversNestedLeafMembers(t *testing.T) {
	snapshot := func(pid, parent int) linuxOwnerHelperV2RunProcessSnapshot {
		return linuxOwnerHelperV2RunProcessSnapshot{
			process:     authority.LinuxCgroupProcessIdentity{PID: pid, Identity: strconv.Itoa(pid)},
			credentials: linuxOwnerProcessCredentials{Parent: parent},
		}
	}
	processes := map[int]linuxOwnerHelperV2RunProcessSnapshot{
		20: snapshot(20, 10), // Direct helper child.
		21: snapshot(21, 20), // Nested descendant.
		22: snapshot(22, 21), // Further nested descendant.
	}
	order, err := linuxOwnerHelperV2RunTreeOrder(10, processes)
	if err != nil {
		t.Fatalf("validate nested helper-owned run tree: %v", err)
	}
	if len(order) != 3 || order[0] != 20 || order[1] != 21 || order[2] != 22 {
		t.Fatalf("recursive run tree order = %v, want [20 21 22]", order)
	}

	processes[30] = snapshot(30, 99)
	if _, err := linuxOwnerHelperV2RunTreeOrder(10, processes); err == nil {
		t.Fatal("accepted a leaf member whose parent is outside the exact helper-owned run tree")
	}
}

func TestLinuxOwnerAnchorChildPIDReaderBoundsBytesAndCount(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "process count",
			content: strings.Repeat("123 ", linuxOwnerHelperV2MaxMembershipProcesses+1),
			want:    "bounded process count",
		},
		{
			name:    "byte count",
			content: strings.Repeat("1", linuxOwnerAnchorMaxChildrenBytes+1),
			want:    "bounded byte count",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "children")
			if err := os.WriteFile(path, []byte(test.content), 0600); err != nil {
				t.Fatalf("write bounded child-list fixture: %v", err)
			}
			if len(test.content) > linuxOwnerAnchorMaxChildrenBytes && test.want != "bounded byte count" {
				t.Fatal("process-count fixture accidentally exceeds the byte limit")
			}
			if _, err := readLinuxOwnerChildPIDs(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("bounded child-list read error = %v, want %q rejection", err, test.want)
			}
		})
	}
}

func TestLinuxOwnerAnchorRunReaperRejectsForeignChildBeforeAdoptingIt(t *testing.T) {
	// The process starts in the current run's leaf and migrates into the other
	// run's leaf without either RunKey registering its identity.
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		t.Skipf("requires the installed unprivileged owner config: %v", err)
	}
	if err := owner.Validate(); err != nil {
		t.Skipf("requires a valid installed unprivileged owner config: %v", err)
	}
	if os.Geteuid() == 0 || uint32(os.Geteuid()) != owner.AUID || uint32(os.Getegid()) != owner.AGID {
		t.Skip("requires the test process to run as the installed A UID/GID")
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil || !strings.HasPrefix(managerPath, "/") {
		t.Skipf("requires a visible cgroup2 manager subgroup: %v", err)
	}
	delegatedPath := strings.TrimPrefix(managerPath, "/")
	if delegatedPath == "" {
		t.Skip("the owner test process is not in a delegated cgroup subgroup")
	}
	parent, err := OpenLinuxCgroupV2Parent("/sys/fs/cgroup", delegatedPath)
	if err != nil {
		t.Skipf("requires an A-writable systemd-delegated cgroup2 subgroup: %v", err)
	}
	t.Cleanup(func() {
		if err := parent.Close(); err != nil {
			t.Errorf("close test cgroup parent: %v", err)
		}
	})
	var randomCurrentLeaf, randomOtherLeaf [authority.TokenBytes]byte
	if _, err := rand.Read(randomCurrentLeaf[:]); err != nil {
		t.Fatalf("create unique current run leaf name: %v", err)
	}
	if _, err := rand.Read(randomOtherLeaf[:]); err != nil {
		t.Fatalf("create unique other run leaf name: %v", err)
	}
	leafName := hex.EncodeToString(randomCurrentLeaf[:])
	otherLeafName := hex.EncodeToString(randomOtherLeaf[:])
	leaf, err := parent.CreateLeaf(leafName)
	if err != nil {
		t.Skipf("cannot create a scratch current-run leaf under the delegated parent: %v", err)
	}
	t.Cleanup(func() {
		if err := leaf.Close(); err != nil {
			t.Errorf("close scratch run leaf: %v", err)
		}
		if err := unix.Unlinkat(parent.fd, leafName, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
			t.Errorf("remove empty scratch run leaf: %v", err)
		}
	})
	otherLeaf, err := parent.CreateLeaf(otherLeafName)
	if err != nil {
		t.Skipf("cannot create a scratch other-run leaf under the delegated parent: %v", err)
	}
	t.Cleanup(func() {
		if err := otherLeaf.Close(); err != nil {
			t.Errorf("close other-run scratch leaf: %v", err)
		}
		if err := unix.Unlinkat(parent.fd, otherLeafName, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
			t.Errorf("remove empty other-run scratch leaf: %v", err)
		}
	})
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchorProcess := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	grant, err := testLinuxOwnerAnchorGrant(anchorProcess, parent.Identity().Path)
	if err != nil {
		t.Fatal(err)
	}
	grant.RunKey.RunID = "owner-anchor-reap-current"
	grant.Parent = parent.Identity()
	grant.Leaf = leaf.Identity()
	grant.OwnerUID = parent.OwnerUID()
	if err := grant.Validate(); err != nil {
		t.Fatalf("validate exact scratch run grant: %v", err)
	}
	leafPath := filepath.Join(grant.Parent.Path, grant.Leaf.LeafName)
	otherGrant, err := testLinuxOwnerAnchorGrant(anchorProcess, parent.Identity().Path)
	if err != nil {
		t.Fatal(err)
	}
	otherGrant.RunKey.RunID = "owner-anchor-reap-other"
	otherGrant.Parent = parent.Identity()
	otherGrant.Leaf = otherLeaf.Identity()
	otherGrant.OwnerUID = parent.OwnerUID()
	if err := otherGrant.Validate(); err != nil {
		t.Fatalf("validate exact other scratch run grant: %v", err)
	}
	otherLeafPath := filepath.Join(otherGrant.Parent.Path, otherGrant.Leaf.LeafName)
	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
	defer cancel()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	foreign, foreignParentTID, releaseForeignParentThread := startLinuxOwnerAnchorTestProcessOnWorkerThread(t, ctx)
	defer close(releaseForeignParentThread)
	if foreignParentTID == os.Getpid() {
		t.Skip("child fixture was not spawned by a nonleader anchor thread")
	}
	foreignIdentity := authority.LinuxCgroupProcessIdentity{
		PID: foreign.command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, foreign.command.Process.Pid),
	}
	threadChildrenPath := filepath.Join("/proc", strconv.Itoa(os.Getpid()), "task", strconv.Itoa(foreignParentTID), "children")
	threadChildren, err := readLinuxOwnerChildPIDs(threadChildrenPath)
	if err != nil {
		t.Fatalf("read child from its nonleader parent thread: %v", err)
	}
	childOnCreatorThread := false
	for _, pid := range threadChildren {
		childOnCreatorThread = childOnCreatorThread || pid == foreignIdentity.PID
	}
	if !childOnCreatorThread {
		t.Fatalf("nonleader TID %d child list %v omits exact spawned child %d", foreignParentTID, threadChildren, foreignIdentity.PID)
	}
	foreignPIDFD := openLinuxOwnerAnchorTestPIDFD(t, foreignIdentity)
	foreign.targetFD = foreignPIDFD
	foreign.targetID = foreignIdentity
	waitFD, err := unix.FcntlInt(uintptr(foreignPIDFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("duplicate unregistered foreign child pidfd for cleanup: %v", err)
	}
	waitPIDFD := os.NewFile(uintptr(waitFD), "linux-owner-anchor-foreign-child-wait-pidfd")
	if waitPIDFD == nil {
		_ = unix.Close(waitFD)
		t.Fatal("wrap unregistered foreign child pidfd")
	}
	t.Cleanup(func() { _ = waitPIDFD.Close() })
	procsFD, err := openLinuxCgroupControlAt(leaf.leafFD, "cgroup.procs", unix.O_WRONLY)
	if err != nil {
		t.Fatalf("open exact scratch leaf cgroup.procs: %v", err)
	}
	pidBytes := []byte(strconv.Itoa(foreignIdentity.PID))
	written, writeErr := unix.Write(procsFD, pidBytes)
	closeErr := unix.Close(procsFD)
	if writeErr != nil || closeErr != nil || written != len(pidBytes) {
		t.Fatalf("move exact foreign child into run leaf: written=%d write=%v close=%v", written, writeErr, closeErr)
	}
	beforeKillPath, err := readLinuxOwnerProcessCgroupPath(foreignIdentity.PID)
	if err != nil || !linuxOwnerCgroupPathWithin(beforeKillPath, leafPath) {
		t.Fatalf("foreign child cgroup before physical stop = %q, %v; want exact run leaf %q", beforeKillPath, err, leafPath)
	}
	otherProcsFD, err := openLinuxCgroupControlAt(otherLeaf.leafFD, "cgroup.procs", unix.O_WRONLY)
	if err != nil {
		t.Fatalf("open other-run leaf cgroup.procs: %v", err)
	}
	written, writeErr = unix.Write(otherProcsFD, pidBytes)
	closeErr = unix.Close(otherProcsFD)
	if writeErr != nil || closeErr != nil || written != len(pidBytes) {
		t.Fatalf("migrate unregistered child to the other run leaf: written=%d write=%v close=%v", written, writeErr, closeErr)
	}
	if migratedPath, err := readLinuxOwnerProcessCgroupPath(foreignIdentity.PID); err != nil ||
		!linuxOwnerCgroupPathWithin(migratedPath, otherLeafPath) {
		t.Fatalf("foreign child cgroup after cross-run migration = %q, %v; want other leaf %q", migratedPath, err, otherLeafPath)
	}
	if err := otherLeaf.Kill(ctx); err != nil {
		t.Fatalf("physically stop the migrated child in the other run leaf: %v", err)
	}
	if err := otherLeaf.WaitEmpty(ctx); err != nil {
		t.Fatalf("wait for other run leaf empty after child stop: %v", err)
	}
	afterKillIdentity, err := ProcessIdentity(foreignIdentity.PID)
	if err != nil || afterKillIdentity != foreignIdentity.Identity {
		t.Skipf("kernel did not retain the exact killed child identity for reaper observation: %v", err)
	}
	afterKillPath, err := readLinuxOwnerProcessCgroupPath(foreignIdentity.PID)
	if err != nil || !linuxOwnerCgroupPathWithin(afterKillPath, otherLeafPath) {
		t.Skipf("kernel did not retain empty-leaf zombie membership for reaper observation: path=%q err=%v", afterKillPath, err)
	}
	anchorChildren, err := readLinuxOwnerProcessChildren(ctx, anchorProcess)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errLinuxOwnerProcessChildrenChanged) {
			t.Skipf("anchor task/child set changed during the bounded ownership census: %v", err)
		}
		t.Fatalf("read all-thread anchor child census: %v", err)
	}
	foreignSeen := false
	for _, pid := range anchorChildren {
		foreignSeen = foreignSeen || pid == foreignIdentity.PID
	}
	if !foreignSeen {
		t.Fatalf("all-thread anchor child census %v omitted child %d created by TID %d", anchorChildren, foreignIdentity.PID, foreignParentTID)
	}
	if len(anchorChildren) != 1 {
		t.Skipf("unrelated anchor children prevent isolated reaper test: %v", anchorChildren)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: foreignIdentity.PID + 100, Identity: "linux:v2:reaper-test-helper"}
	helperExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: anchorProcess, Waited: true, Reaped: true,
	}
	currentRun := &linuxOwnerAnchorRun{
		grant: grant, helper: helper, helperExit: &helperExit, owner: owner, leaf: leaf,
		children: make(map[int]linuxOwnerAnchorTrackedProcess), reaped: make(map[int]LinuxOwnerAnchorReapEvidence),
	}
	otherHelper := authority.LinuxCgroupProcessIdentity{PID: foreignIdentity.PID + 200, Identity: "linux:v2:foreign-run-helper"}
	otherHelperExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: otherHelper, Witness: anchorProcess, Waited: true, Reaped: true,
	}
	otherRun := &linuxOwnerAnchorRun{
		grant: otherGrant, helper: otherHelper, helperExit: &otherHelperExit,
		owner: owner, leaf: otherLeaf, children: make(map[int]linuxOwnerAnchorTrackedProcess),
		reaped: make(map[int]LinuxOwnerAnchorReapEvidence),
	}
	anchor := &linuxOwnerAnchor{
		owner: owner, identity: anchorProcess,
		runs: map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun{
			grant.RunKey: currentRun, otherGrant.RunKey: otherRun,
		},
	}
	if _, err := anchor.reapRunChildren(ctx, currentRun); err == nil {
		t.Fatal("current-run reaper accepted or skipped an unregistered migrated child")
	} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, errLinuxOwnerProcessChildrenChanged) {
		t.Skipf("anchor task/child set changed during the bounded ownership census: %v", err)
	}
	if _, registered := currentRun.children[foreignIdentity.PID]; registered {
		t.Fatal("current-run reaper registered an unregistered migrated child")
	}
	if _, reaped := currentRun.reaped[foreignIdentity.PID]; reaped {
		t.Fatal("current-run reaper consumed an unregistered migrated child's wait status")
	}
	if _, err := anchor.reapRunChildren(ctx, otherRun); err == nil {
		t.Fatal("other-run reaper accepted an unregistered in-leaf child")
	} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, errLinuxOwnerProcessChildrenChanged) {
		t.Skipf("anchor task/child set changed during the bounded ownership census: %v", err)
	}
	if _, registered := otherRun.children[foreignIdentity.PID]; registered {
		t.Fatal("other-run reaper registered a child with no exact creator evidence")
	}
	if _, reaped := otherRun.reaped[foreignIdentity.PID]; reaped {
		t.Fatal("other-run reaper consumed an unregistered child wait status")
	}
	var status unix.Siginfo
	if err := unix.Waitid(unix.P_PIDFD, waitFD, &status, unix.WEXITED|unix.WNOWAIT, nil); err != nil || status.Signo != int32(unix.SIGCHLD) {
		t.Fatalf("foreign child wait status after current-run rejection = %#v, %v; want still waitable", status, err)
	}
	if err := linuxOwnerAnchorWaitPIDFD(ctx, waitPIDFD, foreignIdentity); err != nil {
		t.Fatalf("reap unregistered foreign child through its exact wait pidfd: %v", err)
	}
	foreign.waited = true
}

func TestLinuxOwnerHelperV2UntrustedStopCannotBecomeReceiptEvidence(t *testing.T) {
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 40, Identity: "linux:v2:helper-untrusted-stop-test"}
	launcher := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 41, Identity: "linux:v2:launcher-untrusted-stop-test"}
	child := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 42, Identity: "linux:v2:untrusted-child-stop-test"}
	launcherExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher, Witness: helper, Waited: true, Reaped: true,
	}
	fence := LinuxOwnerAnchorProducerFence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Helper: helper, StopLatched: true, LeaseStopped: true,
		PrepareAttempted: true, PrepareQuiesced: true, ReleaseQuiesced: true, LeafMembershipVerified: true,
		Launcher: &launcher, LauncherExit: &launcherExit, UntrustedProcess: &child,
	}
	anchorStop := LinuxOwnerAnchorStopEvidence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf,
		Anchor: anchor, Helper: helper, ProducerFence: &fence, Launcher: &launcher, UntrustedProcess: &child,
		PinnedLeafFD: true, KillAttempted: true, KillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true,
	}
	if err := anchorStop.ValidateFor(grant, helper, anchor, nil, false); err != nil {
		t.Fatalf("validate untrusted-child StopLeaf evidence shape: %v", err)
	}
	preGate := LinuxOwnerHelperV2PreGateStopEvidence{
		Version: linuxOwnerHelperV2Version, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf, Anchor: anchor, Helper: helper,
		AnchorStop: &anchorStop, PrepareAttempted: true, Launcher: &launcher, LauncherExit: &launcherExit,
		NoProducerCanAct: true, PinnedLeafFD: true, CgroupKillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, DescendantsReaped: true,
		DescendantDigest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
	if err := preGate.ValidateFor(grant, helper); err == nil {
		t.Fatal("accepted an untrusted failed-Prepare child as authenticated no-gate receipt evidence")
	}
	evidence := LinuxOwnerHelperV2StopEvidence{
		Version: linuxOwnerHelperV2Version, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf, Anchor: anchor, Helper: helper,
		PreGate: &preGate,
	}
	if err := evidence.ValidateFor(grant, helper, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}); err == nil {
		t.Fatal("accepted untrusted failed-Prepare stop evidence as a durable helper stop receipt")
	}
	prepareFailure := &LinuxOwnerHelperV2PrepareError{Code: "operation_failed", GateProcess: &child}
	if errors.Is(prepareFailure, linuxgate.ErrGateNeverProduced) {
		t.Fatal("treated an untrusted produced child as authenticated gate-never-produced abort authority")
	}
	gate := authority.LinuxCgroupGateIdentity{
		Process: child, HostUID: grant.OwnerUID, ObservedAt: time.Now().UTC(),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: grant.Leaf,
		BornInLeaf: true, TrustedImage: true,
	}
	gateExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: child, Witness: helper, Waited: true, Reaped: true,
	}
	normalProof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
		Issuer: helper, Reaper: helper, PinnedLeafFD: true, CgroupKillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: launcherExit, GateExit: gateExit,
	}
	normalEvidence := LinuxOwnerHelperV2StopEvidence{
		Version: linuxOwnerHelperV2Version, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf, Anchor: anchor, Helper: helper,
		AnchorStop: &anchorStop, Launcher: launcher, Gate: gate, Proof: normalProof,
		DescendantDigest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
	if err := normalEvidence.ValidateFor(grant, helper, launcher, gate); err == nil {
		t.Fatal("accepted an untrusted failed-Prepare child as a normal gate StopReceipt")
	}
	trustedFence := fence.Clone()
	trustedFence.UntrustedProcess = nil
	trustedFence.Gate = &gate
	userNamespace := &LinuxUserNamespaceIdentity{Device: 7, Inode: 9}
	trustedFence.UserNamespace = userNamespace
	trustedAnchorStop := anchorStop.Clone()
	trustedAnchorStop.ProducerFence = &trustedFence
	trustedAnchorStop.UntrustedProcess = nil
	trustedAnchorStop.Gate = &gate
	trustedAnchorStop.UserNamespace = userNamespace
	trustedEvidence := normalEvidence
	trustedEvidence.AnchorStop = &trustedAnchorStop
	if err := trustedEvidence.ValidateFor(grant, helper, launcher, gate); err != nil {
		t.Fatalf("valid exact gate-bound helper stop evidence was rejected: %v", err)
	}
}

// A real killed child must be reaped by its pidfd before the helper can expose
// the exact wait proof consumed by failed-Prepare cleanup.
func TestLinuxOwnerHelperV2ExactWaitReapsPhysicallyStoppedChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
	defer cancel()
	child := startLinuxOwnerAnchorTestProcess(t, ctx, "target", false)
	process := authority.LinuxCgroupProcessIdentity{
		PID: child.command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, child.command.Process.Pid),
	}
	pidfd := openLinuxOwnerAnchorTestPIDFD(t, process)
	defer unix.Close(pidfd)
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatalf("stop exact child by its pinned pidfd: %v", err)
	}
	var status unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PIDFD, pidfd, &status, unix.WEXITED, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			t.Fatalf("wait/reap exact stopped child by pidfd: %v", err)
		}
		break
	}
	child.waited = true
	helperIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: helperIdentity}
	exit, err := linuxOwnerHelperV2ExitResult(status, process, helper)
	if err != nil || exit == nil || exit.Exited || exit.Signal != int(unix.SIGKILL) ||
		!exit.Proof.ValidForOwnedDescendant(process, helper, authority.LinuxCgroupProcessIdentity{PID: helper.PID + 1, Identity: "linux:v2:anchor-test"}, nil) {
		t.Fatalf("exact stopped-child wait result = %#v, %v; want the child PIDFD's SIGKILL/reap proof", exit, err)
	}
	if current, err := ProcessIdentity(process.PID); err == nil && current == process.Identity {
		t.Fatalf("exact stopped child %d/%q remains running after waitid reap", process.PID, process.Identity)
	}
}

func startLinuxOwnerAnchorTestProcessOnWorkerThread(
	t *testing.T,
	ctx context.Context,
) (*linuxOwnerAnchorTestProcess, int, chan struct{}) {
	t.Helper()
	type result struct {
		command *exec.Cmd
		tid     int
		err     error
	}
	started := make(chan result, 1)
	release := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tid := unix.Gettid()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLinuxOwnerAnchorProcessHelper$")
		command.Env = linuxOwnerAnchorTestEnvironment("target")
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if err := command.Start(); err != nil {
			started <- result{tid: tid, err: err}
			return
		}
		started <- result{command: command, tid: tid}
		<-release
	}()
	startedResult := <-started
	if startedResult.err != nil {
		t.Fatalf("start target from nonleader anchor thread: %v", startedResult.err)
	}
	process := &linuxOwnerAnchorTestProcess{
		command: startedResult.command, anchorFD: -1, helperFD: -1, targetFD: -1,
	}
	t.Cleanup(func() { process.stop(t) })
	return process, startedResult.tid, release
}

func TestStartLinuxOwnerAnchorHelperV2RejectsBeforeForkWithoutReservation(t *testing.T) {
	beforeForkCalled := false
	run, err := startLinuxOwnerAnchorHelperV2(
		&linuxOwnerAnchor{}, LinuxOwnerAnchorGrant{}, authority.LinuxCgroupProcessIdentity{}, -1, -1, nil,
		func() error {
			beforeForkCalled = true
			return nil
		},
	)
	if err == nil || run != nil {
		t.Fatalf("invalid pre-fork launch returned run=%#v, error=%v; want no run and rejection", run, err)
	}
	if beforeForkCalled {
		t.Fatal("beforeFork callback ran after pre-fork input validation rejected the launch")
	}
}

func TestLinuxOwnerHelperV2StatusDoesNotLatchExpiredLease(t *testing.T) {
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: filepath.Join(t.TempDir(), "status.sock"), Net: "unixpacket"})
	if err != nil {
		t.Fatalf("listen for helper Status test: %v", err)
	}
	defer listener.Close()
	listener.SetUnlinkOnClose(true)
	accepted := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- connection
	}()
	client, err := net.DialUnix("unixpacket", nil, listener.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatalf("connect to helper Status test: %v", err)
	}
	defer client.Close()
	var server *net.UnixConn
	select {
	case server = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("accept helper Status test: %v", err)
	case <-time.After(testHangGuard):
		t.Fatal("helper Status test connection was not accepted")
	}
	defer server.Close()
	if err := server.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("bound helper Status write: %v", err)
	}
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("bound helper Status read: %v", err)
	}
	deadline := time.Now().Add(-time.Second)
	wake := make(chan struct{}, 1)
	helper := &linuxOwnerHelperV2{
		helper: authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: "status-test-helper"},
		lease:  &linuxOwnerHelperV2Lease{sequence: 9, generation: 1, duration: time.Second, deadline: deadline, wake: wake},
	}
	if err := helper.handleStatus(server); err != nil {
		t.Fatalf("serve helper Status: %v", err)
	}
	response, rights, err := readLinuxOwnerHelperV2Response(client)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		t.Fatalf("read helper Status: %v", err)
	}
	if response.StatusSnapshot == nil || response.LeaseSequence != 9 ||
		response.StatusSnapshot.LeaseSequence != 9 || response.StatusSnapshot.LeaseLive ||
		response.StatusSnapshot.StopRequested || response.StatusSnapshot.StopAttempted || response.StatusSnapshot.ReleaseInFlight {
		t.Fatalf("expired lease Status snapshot = %#v, response sequence %d; want sequence 9, not live, and unchanged stop flags", response.StatusSnapshot, response.LeaseSequence)
	}
	helper.lease.mu.Lock()
	stopped, observedDeadline := helper.lease.stopped, helper.lease.deadline
	helper.lease.mu.Unlock()
	if stopped || !observedDeadline.Equal(deadline) {
		t.Fatalf("read-only Status mutated expired lease: stopped=%t deadline=%s, want stopped=false and %s", stopped, observedDeadline, deadline)
	}
	select {
	case <-wake:
		t.Fatal("read-only Status woke the owner-loss watcher for an expired lease")
	default:
	}
}

func TestLinuxOwnerHelperV2LeaseStopDoesNotWakeOwnerLoss(t *testing.T) {
	lease := newLinuxOwnerHelperV2Lease()
	if _, err := lease.renew(time.Hour, 1); err != nil {
		t.Fatalf("arm helper lease before stop: %v", err)
	}
	lease.stop()
	if !lease.isStopped() {
		t.Fatal("explicit helper stop did not latch the lease")
	}
	select {
	case <-lease.wake:
		t.Fatal("explicit helper stop woke the owner-loss watcher")
	default:
	}
}

func TestLinuxOwnerHelperV2LeaseExpiryWakesOwnerLoss(t *testing.T) {
	lease := newLinuxOwnerHelperV2Lease()
	lease.mu.Lock()
	lease.sequence, lease.deadline, lease.duration = 1, time.Now().Add(-time.Second), time.Hour
	lease.mu.Unlock()
	if _, live := lease.snapshot(); live {
		t.Fatal("expired helper lease snapshot remained live")
	}
	select {
	case <-lease.wake:
	default:
		t.Fatal("expired helper lease did not wake the owner-loss watcher")
	}
}

func TestLinuxOwnerHelperV2UnarmedLeaseObservationDoesNotLatchOrWake(t *testing.T) {
	lease := newLinuxOwnerHelperV2Lease()
	if _, live := lease.snapshot(); live {
		t.Fatal("unarmed helper lease snapshot reported live")
	}
	if _, _, live := lease.releaseGuard(); live {
		t.Fatal("unarmed helper lease release guard reported live")
	}
	if lease.isStopped() {
		t.Fatal("observing an unarmed lease latched it as stopped")
	}
	select {
	case <-lease.wake:
		t.Fatal("observing an unarmed lease woke the owner-loss watcher")
	default:
	}
	if _, err := lease.renew(time.Hour, 1); err != nil {
		t.Fatalf("lease could not be armed after observation: %v", err)
	}
}

func dialLinuxOwnerHelperV2TestExchange(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: filepath.Join(t.TempDir(), "helper.sock"), Net: "unixpacket"})
	if err != nil {
		t.Fatalf("listen for helper response test: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	listener.SetUnlinkOnClose(true)
	accepted := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- connection
	}()
	client, err := net.DialUnix("unixpacket", nil, listener.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatalf("connect to helper response test: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	var server *net.UnixConn
	select {
	case server = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("accept helper response test: %v", err)
	case <-time.After(testHangGuard):
		t.Fatal("helper response test connection was not accepted")
	}
	t.Cleanup(func() { server.Close() })
	for _, connection := range []*net.UnixConn{server, client} {
		if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatalf("bound helper response exchange: %v", err)
		}
	}
	return client, server
}

func TestLinuxOwnerHelperV2ResponseRejectsActionOutsideStatus(t *testing.T) {
	client, server := dialLinuxOwnerHelperV2TestExchange(t)
	response := linuxOwnerHelperV2Response{
		Operation: "release", WitnessActionObserved: true,
		WitnessAction: linuxOwnerHelperV2WitnessCrashAfterRelease,
	}
	if err := writeLinuxOwnerHelperV2Response(server, response); err != nil {
		t.Fatalf("write helper response with misplaced action: %v", err)
	}
	_, rights, err := readLinuxOwnerHelperV2Response(client)
	closeLinuxOwnerAnchorFDs(rights)
	if err == nil || !strings.Contains(err.Error(), "exposed a selected action outside authenticated Status") {
		t.Fatalf("helper action metadata decode error = %v, want the non-Status action-field rejection", err)
	}
}

func TestLinuxOwnerHelperV2ReleaseUnresolvedErrorCarriesStopEvidence(t *testing.T) {
	evidence := LinuxOwnerHelperV2StopEvidence{}
	response := linuxOwnerHelperV2Response{
		Operation: "release", Status: "unresolved", ErrorCode: "release_outcome_unknown",
		StopEvidence: &evidence,
	}
	err := linuxOwnerHelperV2ResponseError(response)
	var unresolved *LinuxOwnerHelperV2ReleaseUnresolvedError
	if !errors.As(err, &unresolved) || !errors.Is(err, ErrLinuxOwnerHelperV2ReleaseUnresolved) ||
		unresolved.Code != "release_outcome_unknown" || unresolved.Evidence == nil {
		t.Fatalf("unresolved release error lost stop evidence: %v", err)
	}
}

func TestLinuxOwnerHelperV2ReleaseErrorDetailClassifiesSubSteps(t *testing.T) {
	tests := []struct {
		name                                    string
		err                                     error
		ownerLost, leaseLive, stopReq           bool
		wantStage, wantArm, wantWait, wantGuest string
		wantErrno                               int
		wantReason                              string
	}{
		{
			name: "gate authorization refuses without the exact gate identity",
			err: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepGateAuthorize,
				linuxgate.ReleaseArmGateMissingIdentity, 0, errors.New("anchor refused the grant")),
			leaseLive: true, wantStage: "gate_authorize", wantArm: "missing_gate_identity",
		},
		{
			name: "clone bootstrap attempt carries its wait class",
			err: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepCloneBootstrap, linuxgate.ReleaseArmBootstrapAttempt, 0,
				fmt.Errorf("receive same-PID B bootstrap attempt: %w", context.DeadlineExceeded)),
			leaseLive: true, wantStage: "clone_bootstrap", wantArm: "bootstrap_attempt", wantWait: "deadline",
		},
		{
			name: "clone checkout receipt names the guest bootstrap failure",
			err: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepCloneCheckout, linuxgate.ReleaseArmCheckoutReceipt, 0,
				fmt.Errorf("receive B checkout-ready receipt: %w",
					&linuxgate.CloneBootstrapFailureError{Stage: linuxgate.CloneFailureExec, Errno: 1})),
			leaseLive: true, wantStage: "clone_checkout", wantArm: "checkout_receipt", wantWait: "other",
			wantGuest: "exec", wantErrno: 1,
		},
		{
			name: "native exec errno",
			err: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeErrno, 1,
				errors.New("native exec failed after RELEASE: errno=1")),
			leaseLive: true, wantStage: "native_exec", wantArm: "exec_errno", wantErrno: 1,
		},
		{
			name: "native exec without an attempt",
			err: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeNoAttempt, 0,
				errors.New("gate closed exec-status FD without an exec attempt")),
			leaseLive: true, wantStage: "native_exec", wantArm: "no_exec_attempt",
		},
		{
			name: "native exec image mismatch",
			err: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeImage, 0,
				errors.New("same-PID executable readback differs")),
			leaseLive: true, wantStage: "native_exec", wantArm: "image_mismatch",
		},
		{
			name: "clone exec proof reads an EOF as a closed peer",
			err: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeCloneProof, 0,
				fmt.Errorf("receive same-PID native exec proof: %w", io.EOF)),
			leaseLive: true, wantStage: "native_exec", wantArm: "clone_exec_proof", wantWait: "peer_closed",
		},
		{
			name: "register",
			err: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepRegister, linuxgate.ReleaseArmRegisterProcess, 0,
				errors.New("register same-PID B exec with owner anchor")),
			leaseLive: true, wantStage: "register", wantArm: "native_register",
		},
		{
			name: "joined abort keeps the sub-step",
			err: errors.Join(linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeErrno, 1,
				errors.New("native exec failed after RELEASE: errno=1")), errors.New("abort the pinned leaf")),
			leaseLive: true, wantStage: "native_exec", wantArm: "exec_errno", wantErrno: 1,
		},
		{
			// The wait class comes from the arm's own read; an abort sibling's
			// ENOTCONN must not reclassify a deadline.
			name: "abort errno does not reclassify the arm wait error",
			err: errors.Join(linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec,
				linuxgate.ReleaseArmNativeAwaitResult, 0, context.DeadlineExceeded), unix.ENOTCONN),
			leaseLive: true, wantStage: "native_exec", wantArm: "await_result", wantWait: "deadline",
		},
		{
			name: "wrapped sub-step survives the release guard chain",
			err: fmt.Errorf("%w: await guarded native exec result: %w", linuxgate.ErrOutcomeUnknown,
				linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeErrno, 1,
					errors.New("native exec failed after RELEASE: errno=1"))),
			leaseLive: true, wantStage: "native_exec", wantArm: "exec_errno", wantErrno: 1,
		},
		{
			name: "untyped release failure", err: errors.New("Linux owner helper lease expired before gate RELEASE"),
			wantStage: "none",
		},
		{
			name: "owner loss without an error", leaseLive: false, ownerLost: true, wantStage: "none", wantReason: "owner_lost",
		},
		{
			name: "lease expiry without an error", leaseLive: false, wantStage: "none", wantReason: "lease_expired",
		},
		{
			name: "stop request without an error", leaseLive: true, stopReq: true, wantStage: "none", wantReason: "stop_requested",
		},
		{
			name: "live lease and no error carries no reason", leaseLive: true, wantStage: "none",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail := linuxOwnerHelperV2ReleaseErrorDetailFor(test.err, test.ownerLost, test.leaseLive, test.stopReq)
			if detail.Stage != test.wantStage || detail.Arm != test.wantArm ||
				detail.WaitClass != test.wantWait || detail.GuestStage != test.wantGuest ||
				detail.Errno != test.wantErrno || detail.Reason != test.wantReason {
				t.Fatalf("release detail = %+v, want stage=%q arm=%q wait_class=%q guest_stage=%q errno=%d reason=%q",
					*detail, test.wantStage, test.wantArm, test.wantWait, test.wantGuest, test.wantErrno, test.wantReason)
			}
			if test.wantStage == "clone_checkout" && detail.Stage == linuxgate.ReleaseSubStepNativeExec {
				t.Fatal("checkout-receipt failure was mislabeled as native_exec")
			}
			if test.wantArm == "" && (detail.Arm != "" || detail.WaitClass != "" || detail.GuestStage != "") {
				t.Fatalf("armless failure carried arm detail: %+v", *detail)
			}
			if err := detail.Validate(); err != nil {
				t.Fatalf("classified release detail is not closed-vocabulary: %v", err)
			}
		})
	}
}

func TestLinuxOwnerHelperV2ReleaseErrorDetailNamesEveryArm(t *testing.T) {
	// Expected renderings are literals, never derived from the constants: a
	// swapped arm or sub-step constant value reddens here (mutant M7).
	arms := []struct {
		subStep string
		arm     string
		want    string
	}{
		{linuxgate.ReleaseSubStepGateAuthorize, linuxgate.ReleaseArmGateMissingIdentity, "stage=gate_authorize arm=missing_gate_identity"},
		{linuxgate.ReleaseSubStepGateAuthorize, linuxgate.ReleaseArmGateAuthorize, "stage=gate_authorize arm=anchor_authorize"},
		{linuxgate.ReleaseSubStepGateAuthorize, linuxgate.ReleaseArmGateIdentity, "stage=gate_authorize arm=gate_identity"},
		{linuxgate.ReleaseSubStepCloneBootstrap, linuxgate.ReleaseArmBootstrapInstalled, "stage=clone_bootstrap arm=installed_bootstrap"},
		{linuxgate.ReleaseSubStepCloneBootstrap, linuxgate.ReleaseArmBootstrapAttempt, "stage=clone_bootstrap arm=bootstrap_attempt wait_class=other"},
		{linuxgate.ReleaseSubStepCloneBootstrap, linuxgate.ReleaseArmBootstrapImage, "stage=clone_bootstrap arm=bootstrap_image"},
		{linuxgate.ReleaseSubStepCloneBootstrap, linuxgate.ReleaseArmBootstrapProcess, "stage=clone_bootstrap arm=bootstrap_process"},
		{linuxgate.ReleaseSubStepCloneBootstrap, linuxgate.ReleaseArmLeafMembership, "stage=clone_bootstrap arm=leaf_membership"},
		{linuxgate.ReleaseSubStepCloneCheckout, linuxgate.ReleaseArmCheckoutReceipt, "stage=clone_checkout arm=checkout_receipt wait_class=other"},
		{linuxgate.ReleaseSubStepCloneCheckout, linuxgate.ReleaseArmCheckoutReceiptMismatch, "stage=clone_checkout arm=checkout_receipt_mismatch"},
		{linuxgate.ReleaseSubStepCloneCheckout, linuxgate.ReleaseArmCheckoutCallback, "stage=clone_checkout arm=checkout_callback"},
		{linuxgate.ReleaseSubStepCloneCheckout, linuxgate.ReleaseArmCheckoutAckEncode, "stage=clone_checkout arm=checkout_ack_encode"},
		{linuxgate.ReleaseSubStepCloneCheckout, linuxgate.ReleaseArmCheckoutAckSend, "stage=clone_checkout arm=checkout_ack_send"},
		{linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeAwaitResult, "stage=native_exec arm=await_result wait_class=other"},
		{linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeErrno, "stage=native_exec arm=exec_errno"},
		{linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeNoAttempt, "stage=native_exec arm=no_exec_attempt"},
		{linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeImage, "stage=native_exec arm=image_mismatch"},
		{linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeCloneProof, "stage=native_exec arm=clone_exec_proof wait_class=other"},
		{linuxgate.ReleaseSubStepRegister, linuxgate.ReleaseArmRegisterProcess, "stage=register arm=native_register"},
		{linuxgate.ReleaseSubStepRegister, linuxgate.ReleaseArmRegisterWaiter, "stage=register arm=gate_waiter"},
	}
	rendered := make(map[string]string, len(arms))
	for _, arm := range arms {
		shape, found := linuxgate.LookupReleaseArmShape(arm.arm)
		if !found || shape.SubStep != arm.subStep {
			t.Fatalf("arm %q is not bound to sub-step %q in the closed vocabulary", arm.arm, arm.subStep)
		}
		failure := linuxgate.NewReleaseSubStepError(arm.subStep, arm.arm, 0, errors.New("bounded probe failure"))
		detail := linuxOwnerHelperV2ReleaseErrorDetailFor(failure, false, true, false)
		if detail.Stage != arm.subStep || detail.Arm != arm.arm {
			t.Fatalf("arm %q classified as stage=%q arm=%q", arm.arm, detail.Stage, detail.Arm)
		}
		if err := detail.Validate(); err != nil {
			t.Fatalf("arm %q is not closed-vocabulary: %v", arm.arm, err)
		}
		if got := detail.String(); got != arm.want {
			t.Fatalf("arm %q rendered %q, want the literal %q", arm.arm, got, arm.want)
		}
		if shape.WaitClass != strings.Contains(arm.want, " wait_class=") {
			t.Fatalf("arm %q literal %q disagrees with the closed wait-class shape", arm.arm, arm.want)
		}
		if other, duplicate := rendered[arm.want]; duplicate {
			t.Fatalf("arms %q and %q render the same bounded detail %q", other, arm.arm, arm.want)
		}
		rendered[arm.want] = arm.arm
	}
}

func TestLinuxOwnerHelperV2ReleaseWaitErrorClassClosesTheVocabulary(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "no failure", err: nil, want: ""},
		{name: "context deadline", err: context.DeadlineExceeded, want: "deadline"},
		{name: "os deadline", err: os.ErrDeadlineExceeded, want: "deadline"},
		{name: "wrapped deadline", err: fmt.Errorf("wait for close-on-exec result: %w", context.DeadlineExceeded), want: "deadline"},
		{name: "eof", err: io.EOF, want: "peer_closed"},
		{name: "wrapped eof", err: fmt.Errorf("receive same-PID native exec proof: %w", io.EOF), want: "peer_closed"},
		{name: "unexpected eof", err: io.ErrUnexpectedEOF, want: "peer_closed"},
		{name: "reset peer", err: unix.ECONNRESET, want: "peer_closed"},
		{name: "unconnected peer", err: unix.ENOTCONN, want: "peer_closed"},
		{name: "broken peer pipe", err: unix.EPIPE, want: "peer_closed"},
		{name: "canceled context", err: context.Canceled, want: "other"},
		{name: "filtered channel text", err: errors.New("exec-status channel closed or carried ancillary data before a frame"), want: "other"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := linuxgate.ReleaseWaitErrorClass(test.err); got != test.want {
				t.Fatalf("wait class = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLinuxOwnerHelperV2ReleaseGuestFailureDetailClosesTheVocabulary(t *testing.T) {
	stages := []struct {
		stage linuxgate.CloneBootstrapFailureStage
		want  string
	}{
		{linuxgate.CloneFailureRequest, "request"},
		{linuxgate.CloneFailureSeed, "seed"},
		{linuxgate.CloneFailureTarget, "target"},
		{linuxgate.CloneFailureGit, "git"},
		{linuxgate.CloneFailureCheckout, "checkout"},
		{linuxgate.CloneFailureNativeImage, "native_image"},
		{linuxgate.CloneFailureExec, "exec"},
	}
	for _, stage := range stages {
		failure := linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeCloneProof, 0,
			fmt.Errorf("receive same-PID native exec proof: %w",
				&linuxgate.CloneBootstrapFailureError{Stage: stage.stage, Errno: 13}))
		detail := linuxOwnerHelperV2ReleaseErrorDetailFor(failure, false, true, false)
		if detail.GuestStage != stage.want || detail.Errno != 13 || detail.WaitClass != "other" {
			t.Fatalf("guest stage %v = %+v, want guest_stage=%q errno=13 wait_class=other", stage.stage, *detail, stage.want)
		}
		if err := detail.Validate(); err != nil {
			t.Fatalf("guest stage %q is not closed-vocabulary: %v", stage.want, err)
		}
		for _, fragment := range []string{"guest_stage=" + stage.want, "errno=13"} {
			if !strings.Contains(detail.String(), fragment) {
				t.Fatalf("guest stage %q rendered %q without %q", stage.want, detail.String(), fragment)
			}
		}
	}
	guest := &linuxgate.CloneBootstrapFailureError{Stage: linuxgate.CloneFailureExec, Errno: 13}
	tests := []struct {
		name    string
		failure *linuxgate.ReleaseSubStepError
	}{
		{
			name: "unknown guest stage stays unclassified",
			failure: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeCloneProof, 0,
				&linuxgate.CloneBootstrapFailureError{Stage: linuxgate.CloneBootstrapFailureStage(99), Errno: 13}),
		},
		{
			name: "guest failure on an arm that cannot read it stays unclassified",
			failure: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepCloneBootstrap,
				linuxgate.ReleaseArmBootstrapImage, 0, guest),
		},
		{
			name: "the errno arm keeps its own errno",
			failure: linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec,
				linuxgate.ReleaseArmNativeErrno, 2, guest),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail := linuxOwnerHelperV2ReleaseErrorDetailFor(test.failure, false, true, false)
			if detail.GuestStage != "" {
				t.Fatalf("unclassified guest failure carried guest_stage=%q", detail.GuestStage)
			}
			if err := detail.Validate(); err != nil {
				t.Fatalf("unclassified guest failure is not closed-vocabulary: %v", err)
			}
		})
	}
}

func TestLinuxOwnerHelperV2ReleaseErrorDetailRedactsFreeFormText(t *testing.T) {
	raw := "/home/some-profile-user/secret\npid=4242 exec --lease-token=abcd1234"
	detail := linuxOwnerHelperV2ReleaseErrorDetailFor(
		linuxgate.NewReleaseSubStepError(linuxgate.ReleaseSubStepNativeExec, linuxgate.ReleaseArmNativeErrno, 1,
			errors.New(raw)),
		false, true, false)
	if rendered := detail.String(); rendered != "stage=native_exec arm=exec_errno errno=1" {
		t.Fatalf("release detail rendered %q, want the closed sub-step, arm and errno only", rendered)
	}
	rendered := detail.String()
	for _, fragment := range []string{"some-profile-user", "/home", "4242", "lease-token", "secret"} {
		if strings.Contains(rendered, fragment) {
			t.Fatalf("release detail leaked %q: %s", fragment, rendered)
		}
	}
}

func TestLinuxOwnerHelperV2ReleaseErrorDetailValidatesClosedVocabulary(t *testing.T) {
	tests := []struct {
		name    string
		detail  *LinuxOwnerHelperV2ReleaseErrorDetail
		wantErr bool
	}{
		{name: "nil detail", detail: nil},
		{
			name:   "sub-step and errno",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "exec_errno", Errno: 1},
		},
		{
			name:   "wait arm with its class",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "await_result", WaitClass: "deadline"},
		},
		{
			name: "clone proof with its guest stage",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{
				Stage: "native_exec", Arm: "clone_exec_proof", WaitClass: "other", GuestStage: "exec", Errno: 1,
			},
		},
		{name: "reason only", detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "none", Reason: "stop_requested"}},
		{name: "free-form stage", detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "/tmp/native exec"}, wantErr: true},
		{
			name:    "negative errno",
			detail:  &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "exec_errno", Errno: -1},
			wantErr: true,
		},
		{name: "free-form reason", detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "none", Reason: "owner lost"}, wantErr: true},
		{
			name:   "reason contradicts a sub-step",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Errno: 1, Reason: "owner_lost"}, wantErr: true,
		},
		{
			name:    "free-form arm",
			detail:  &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "await /home/user/secret"},
			wantErr: true,
		},
		{
			name:    "arm bound to another sub-step",
			detail:  &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "checkout_receipt", WaitClass: "other"},
			wantErr: true,
		},
		{
			name:    "arm without a sub-step",
			detail:  &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "none", Arm: "exec_errno"},
			wantErr: true,
		},
		{
			name:    "wait arm without its class",
			detail:  &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "clone_exec_proof"},
			wantErr: true,
		},
		{
			name:    "wait class on an arm that never waits",
			detail:  &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "image_mismatch", WaitClass: "other"},
			wantErr: true,
		},
		{
			name: "free-form wait class",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{
				Stage: "native_exec", Arm: "await_result", WaitClass: "soon",
			},
			wantErr: true,
		},
		{
			name: "guest stage on an arm that cannot read one",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{
				Stage: "native_exec", Arm: "exec_errno", GuestStage: "exec",
			},
			wantErr: true,
		},
		{
			name: "free-form guest stage",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{
				Stage: "native_exec", Arm: "clone_exec_proof", WaitClass: "other", GuestStage: "exec /home/user/secret",
			},
			wantErr: true,
		},
		{
			name:    "errno without a sub-step",
			detail:  &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "none", Errno: 1},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.detail.Validate()
			if test.wantErr && err == nil {
				t.Fatalf("detail %+v was accepted", *test.detail)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("detail %+v was rejected: %v", *test.detail, err)
			}
		})
	}
}

func TestLinuxOwnerHelperV2ReleaseUnresolvedErrorCarriesTheBoundedDetail(t *testing.T) {
	tests := []struct {
		name    string
		failure *LinuxOwnerHelperV2ReleaseUnresolvedError
		want    string
	}{
		{
			name: "code and detail",
			failure: &LinuxOwnerHelperV2ReleaseUnresolvedError{
				Code:   "release_outcome_unknown",
				Detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "clone_exec_proof", WaitClass: "other", GuestStage: "exec", Errno: 1},
			},
			want: "Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=native_exec arm=clone_exec_proof wait_class=other guest_stage=exec errno=1)",
		},
		{
			name:    "code only stays byte-identical",
			failure: &LinuxOwnerHelperV2ReleaseUnresolvedError{Code: "release_outcome_unknown"},
			want:    "Linux owner helper release outcome is unresolved (release_outcome_unknown)",
		},
		{
			name: "detail only",
			failure: &LinuxOwnerHelperV2ReleaseUnresolvedError{
				Detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "none", Reason: "stop_requested"},
			},
			want: "Linux owner helper release outcome is unresolved (stage=none reason=stop_requested)",
		},
		{
			name:    "empty failure",
			failure: &LinuxOwnerHelperV2ReleaseUnresolvedError{},
			want:    "Linux owner helper release outcome is unresolved",
		},
		{
			name:    "nil failure",
			failure: nil,
			want:    "Linux owner helper release outcome is unresolved",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.failure.Error(); got != test.want {
				t.Fatalf("release unresolved error = %q, want %q", got, test.want)
			}
			if !errors.Is(test.failure, ErrLinuxOwnerHelperV2ReleaseUnresolved) {
				t.Fatal("bounded release detail broke the sentinel chain")
			}
		})
	}
}

func TestLinuxOwnerHelperV2ResponseErrorCarriesTheReleaseDetail(t *testing.T) {
	detail := &LinuxOwnerHelperV2ReleaseErrorDetail{
		Stage: "clone_checkout", Arm: "checkout_receipt", WaitClass: "deadline",
	}
	err := linuxOwnerHelperV2ResponseError(linuxOwnerHelperV2Response{
		Operation: "release", Status: "unresolved", ErrorCode: "release_outcome_unknown",
		ReleaseErrorDetail: detail,
	})
	var unresolved *LinuxOwnerHelperV2ReleaseUnresolvedError
	if !errors.As(err, &unresolved) || unresolved.Evidence != nil || unresolved.Detail != detail {
		t.Fatalf("unresolved release without stop evidence lost its bounded detail: %v", err)
	}
	if !strings.Contains(err.Error(), "stage=clone_checkout arm=checkout_receipt wait_class=deadline") {
		t.Fatalf("release detail is absent from the error chain: %v", err)
	}
}

func TestLinuxOwnerHelperV2ResponseRejectsAFreeFormReleaseDetail(t *testing.T) {
	tests := []struct {
		name   string
		detail *LinuxOwnerHelperV2ReleaseErrorDetail
		want   string
	}{
		{
			name:   "free-form sub-step",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "exec /home/user/secret failed"},
			want:   "unknown sub-step",
		},
		{
			name:   "free-form arm",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "await /home/user/secret"},
			want:   "unknown arm",
		},
		{
			name:   "arm bound to another sub-step",
			detail: &LinuxOwnerHelperV2ReleaseErrorDetail{Stage: "native_exec", Arm: "checkout_receipt", WaitClass: "other"},
			want:   "does not belong to sub-step",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, server := dialLinuxOwnerHelperV2TestExchange(t)
			response := linuxOwnerHelperV2Response{
				Operation: "release", Status: "unresolved", ErrorCode: "release_outcome_unknown",
				ReleaseErrorDetail: test.detail,
			}
			if err := writeLinuxOwnerHelperV2Response(server, response); err != nil {
				t.Fatalf("write helper response with an invalid detail: %v", err)
			}
			if _, rights, err := readLinuxOwnerHelperV2Response(client); err == nil {
				closeLinuxOwnerAnchorFDs(rights)
				t.Fatal("invalid release detail was accepted from the wire")
			} else if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("release detail rejection = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLinuxOwnerHelperV2ReleaseDetailSurvivesTheWire(t *testing.T) {
	client, server := dialLinuxOwnerHelperV2TestExchange(t)
	response := linuxOwnerHelperV2Response{
		Operation: "release", Status: "unresolved", ErrorCode: "release_outcome_unknown",
		ReleaseErrorDetail: &LinuxOwnerHelperV2ReleaseErrorDetail{
			Stage: "native_exec", Arm: "clone_exec_proof", WaitClass: "other", GuestStage: "exec", Errno: 1,
		},
	}
	if err := writeLinuxOwnerHelperV2Response(server, response); err != nil {
		t.Fatalf("write helper response with a bounded detail: %v", err)
	}
	decoded, rights, err := readLinuxOwnerHelperV2Response(client)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		t.Fatalf("read helper response with a bounded detail: %v", err)
	}
	if got := decoded.ReleaseErrorDetail.String(); got != "stage=native_exec arm=clone_exec_proof wait_class=other guest_stage=exec errno=1" {
		t.Fatalf("wire release detail = %q, want the bounded detail unchanged", got)
	}
}

func readLinuxOwnerChildPIDs(path string) ([]int, error) {
	pids, _, err := readLinuxOwnerChildPIDsBounded(path, linuxOwnerAnchorMaxChildrenBytes, linuxOwnerHelperV2MaxMembershipProcesses)
	return pids, err
}
