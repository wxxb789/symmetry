//go:build linux && amd64

package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	case <-time.After(5 * time.Second):
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

func TestLinuxOwnerHelperV2ResponseRejectsActionOutsideStatus(t *testing.T) {
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: filepath.Join(t.TempDir(), "helper.sock"), Net: "unixpacket"})
	if err != nil {
		t.Fatalf("listen for helper response test: %v", err)
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
		t.Fatalf("connect to helper response test: %v", err)
	}
	defer client.Close()
	var server *net.UnixConn
	select {
	case server = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("accept helper response test: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("helper response test connection was not accepted")
	}
	defer server.Close()
	if err := server.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("bound helper response write: %v", err)
	}
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("bound helper response read: %v", err)
	}
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
