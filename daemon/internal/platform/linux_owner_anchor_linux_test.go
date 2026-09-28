//go:build linux && amd64

package platform

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	daemonprotocol "github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const linuxOwnerAnchorTestRole = "SYMMETRY_LINUX_OWNER_ANCHOR_TEST_ROLE"

type linuxOwnerAnchorSubprocessEvent struct {
	Stage       string                               `json:"stage"`
	Anchor      authority.LinuxCgroupProcessIdentity `json:"anchor,omitempty"`
	Helper      authority.LinuxCgroupProcessIdentity `json:"helper,omitempty"`
	Process     authority.LinuxCgroupProcessIdentity `json:"process,omitempty"`
	ParentPID   int                                  `json:"parent_pid,omitempty"`
	HelperExit  *authority.LinuxCgroupExitProof      `json:"helper_exit,omitempty"`
	ProcessExit *authority.LinuxCgroupExitProof      `json:"process_exit,omitempty"`
	Error       string                               `json:"error,omitempty"`
}

// This test entry is also used as an isolated helper, target and daemon
// process. The environment role is set only by the focused subprocess tests.
func TestLinuxOwnerAnchorProcessHelper(t *testing.T) {
	role := os.Getenv(linuxOwnerAnchorTestRole)
	if role == "" {
		return
	}
	var err error
	switch role {
	case "anchor":
		err = runLinuxOwnerAnchorTestProcess()
	case "helper":
		err = runLinuxOwnerAnchorTestHelper()
	case "target":
		select {}
	case "daemon":
		err = runLinuxOwnerAnchorTestDaemon()
	default:
		err = fmt.Errorf("unknown owner-anchor test process role %q", role)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func TestLinuxOwnerAnchorPIDFDReadyTracksExactChildExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	process := startLinuxOwnerAnchorTestProcess(t, ctx, "target", false)
	identity := authority.LinuxCgroupProcessIdentity{
		PID: process.command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, process.command.Process.Pid),
	}
	pidfd := openLinuxOwnerAnchorTestPIDFD(t, identity)
	pidfdFile := os.NewFile(uintptr(pidfd), "linux-owner-anchor-helper-death-test-pidfd")
	if pidfdFile == nil {
		_ = unix.Close(pidfd)
		t.Fatal("wrap exact helper pidfd")
	}
	defer pidfdFile.Close()
	if ready, err := linuxOwnerAnchorPIDFDReady(pidfdFile); err != nil || ready {
		t.Fatalf("live helper pidfd readiness = %t, %v; want not ready", ready, err)
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatalf("kill exact helper by pidfd: %v", err)
	}
	if err := process.command.Wait(); err == nil {
		t.Fatal("killed helper subprocess unexpectedly exited successfully")
	}
	process.waited = true
	if ready, err := linuxOwnerAnchorPIDFDReady(pidfdFile); err != nil || !ready {
		t.Fatalf("exited helper pidfd readiness = %t, %v; want ready", ready, err)
	}
}

func TestLinuxOwnerHelperV2CloneCheckoutBindsExactRunGrant(t *testing.T) {
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: "run-1", Generation: 3, BindingKey: "primary",
		ResourceID: "00000000-0000-4000-8000-000000000001", BaseCommit: strings.Repeat("a", 40),
		BaseTreeDigest: "sha256:" + strings.Repeat("b", 64), Nonce: strings.Repeat("c", authority.LinuxCloneNonceBytes*2),
		SeedPath: "/srv/symmetry/seeds/run-1.bundle", Target: "/srv/symmetry/work/run-1",
	}
	clone := authority.LinuxCloneGrant{
		Intent: intent,
		Seed: authority.LinuxCloneSeedReceipt{
			Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
			Digest: "sha256:" + strings.Repeat("d", 64), Size: 128, Device: 7, Inode: 11,
		},
	}
	if err := clone.Validate(); err != nil {
		t.Fatalf("test clone grant is invalid: %v", err)
	}
	grant := LinuxOwnerAnchorGrant{RunKey: LinuxOwnerAnchorRunKey{RunID: intent.RunID, Generation: intent.Generation}}
	if !linuxOwnerHelperV2CloneGrantMatchesRun(&clone, grant, intent.Target) {
		t.Fatal("exact clone grant was not bound to its RunKey and B target")
	}
	receipt := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target, Nonce: intent.Nonce,
		SeedDigest: clone.Seed.Digest, BaseCommit: intent.BaseCommit, TargetDevice: 9, TargetInode: 13,
	}
	if !linuxOwnerHelperV2CheckoutReceiptMatches(receipt, clone) {
		t.Fatal("exact gate checkout receipt did not match the durable clone grant")
	}
	mutations := []func(*authority.LinuxCloneCheckoutReceipt){
		func(value *authority.LinuxCloneCheckoutReceipt) { value.RunID = "run-2" },
		func(value *authority.LinuxCloneCheckoutReceipt) { value.Generation++ },
		func(value *authority.LinuxCloneCheckoutReceipt) { value.BindingKey = "secondary" },
		func(value *authority.LinuxCloneCheckoutReceipt) {
			value.ResourceID = "00000000-0000-4000-8000-000000000002"
		},
		func(value *authority.LinuxCloneCheckoutReceipt) { value.Target = "/srv/symmetry/work/other" },
		func(value *authority.LinuxCloneCheckoutReceipt) {
			value.Nonce = strings.Repeat("e", authority.LinuxCloneNonceBytes*2)
		},
		func(value *authority.LinuxCloneCheckoutReceipt) {
			value.SeedDigest = "sha256:" + strings.Repeat("f", 64)
		},
		func(value *authority.LinuxCloneCheckoutReceipt) { value.BaseCommit = strings.Repeat("9", 40) },
	}
	for index, mutate := range mutations {
		changed := receipt
		mutate(&changed)
		if err := changed.Validate(); err != nil {
			t.Fatalf("mutated receipt %d is structurally invalid: %v", index, err)
		}
		if linuxOwnerHelperV2CheckoutReceiptMatches(changed, clone) {
			t.Fatalf("checkout receipt mutation %d matched a different clone grant", index)
		}
	}
	if linuxOwnerHelperV2CloneGrantMatchesRun(&clone, grant, "/srv/symmetry/work/other") {
		t.Fatal("clone grant was accepted for a different B working directory")
	}
}

func TestLinuxOwnerHelperFinalizationIsSingleFlightAndExactReplay(t *testing.T) {
	parent := authority.LinuxCgroupParentIdentity{
		BootID: "00000000-0000-4000-8000-000000000001", MountID: 7, Device: 8, Inode: 9,
		Path: "/sys/fs/cgroup/user.slice/symmetry/manager",
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: 100, Identity: "anchor:100:created"}
	mirror := authority.LinuxCgroupProcessIdentity{PID: 101, Identity: "mirror:101:created"}
	proof := authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: strings.Repeat("a", authority.TokenBytes*2),
		Parent: parent, LeafName: strings.Repeat("b", 32), CreateStarted: false,
		LeafDisposition: authority.LinuxCgroupAbortLeafNeverCreated, OwnerUID: 1000,
		Anchor: anchor, Mirror: mirror, Witness: anchor, CreatorFenced: true, NoProducerCanAct: true,
		ParentUnchanged: true, LeafAbsent: true, LeafNeverAppeared: false,
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("test abort proof is invalid: %v", err)
	}
	request := linuxOwnerHelperV2Request{Operation: "release-run", AbortProof: &proof}
	helper := &linuxOwnerHelperV2{}
	start := make(chan struct{})
	type result struct {
		record  *linuxOwnerHelperV2Finalization
		owner   bool
		already bool
		err     error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			<-start
			record, owner, already, err := helper.beginFinalization(request)
			results <- result{record: record, owner: owner, already: already, err: err}
		}()
	}
	close(start)
	workers.Wait()
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.record == nil || first.record != second.record ||
		first.owner == second.owner || first.already || second.already {
		t.Fatalf("concurrent finalizer claims = %#v and %#v; want one owner and one waiter on the same exact record", first, second)
	}
	helper.finishFinalization(first.record, nil, false)
	if err := waitLinuxOwnerHelperV2Finalization(first.record); err != nil {
		t.Fatalf("wait for serialized finalizer = %v", err)
	}
	replay, owner, already, err := helper.beginFinalization(request)
	if err != nil || owner || !already || replay != first.record {
		t.Fatalf("exact finalizer replay = (%p, owner=%t, already=%t, err=%v); want same already-finalized record", replay, owner, already, err)
	}
	changed := proof.Clone()
	changed.LaunchToken = strings.Repeat("c", authority.TokenBytes*2)
	conflicting := request
	conflicting.AbortProof = &changed
	if _, _, _, err := helper.beginFinalization(conflicting); err == nil {
		t.Fatal("finalizer accepted a different durable abort proof after exact retirement")
	}
}

func TestLinuxOwnerAnchorSubreaperAdoptsAndReapsAfterHelperDeath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	anchor := startLinuxOwnerAnchorTestProcess(t, ctx, "anchor", true)
	ready := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if ready.Stage != "ready" || ready.Anchor.PID != anchor.command.Process.Pid || ready.Helper.Validate() != nil || ready.Process.Validate() != nil {
		t.Fatalf("owner-anchor ready event = %#v", ready)
	}
	anchor.targetID = ready.Process
	anchor.targetFD = openLinuxOwnerAnchorTestPIDFD(t, ready.Process)
	anchor.helperFD = openLinuxOwnerAnchorTestPIDFD(t, ready.Helper)

	// The test daemon is a separate process with no parent/control relationship
	// to the anchor. Its death must not be mistaken for anchor or unit stop proof.
	daemon := startLinuxOwnerAnchorTestProcess(t, ctx, "daemon", true)
	daemonReady := readLinuxOwnerAnchorTestEvent(t, daemon.output)
	if daemonReady.Stage != "daemon-ready" {
		t.Fatalf("daemon process ready event = %#v", daemonReady)
	}
	daemonFD := openLinuxOwnerAnchorTestPIDFD(t, daemonReady.Anchor)
	defer unix.Close(daemonFD)
	if err := unix.PidfdSendSignal(daemonFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatalf("kill exact daemon subprocess via pidfd: %v", err)
	}
	if err := daemon.command.Wait(); err == nil {
		t.Fatal("killed daemon subprocess unexpectedly exited successfully")
	}
	daemon.waited = true
	if got, err := ProcessIdentity(ready.Anchor.PID); err != nil || got != ready.Anchor.Identity {
		t.Fatalf("owner anchor identity after unrelated daemon death = %q, %v; want %q", got, err, ready.Anchor.Identity)
	}
	if err := unix.PidfdSendSignal(anchor.anchorFD, 0, nil, 0); err != nil {
		t.Fatalf("owner anchor did not survive unrelated daemon process death: %v", err)
	}

	if _, err := io.WriteString(anchor.input, "kill-helper\n"); err != nil {
		t.Fatalf("request helper loss witness: %v", err)
	}
	adopted := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if adopted.Stage != "adopted" || adopted.ParentPID != ready.Anchor.PID || adopted.HelperExit == nil ||
		!adopted.HelperExit.ValidFor(ready.Helper, ready.Anchor) {
		t.Fatalf("subreaper adoption event = %#v", adopted)
	}
	if got, err := ProcessIdentity(ready.Process.PID); err != nil || got != ready.Process.Identity {
		t.Fatalf("adopted descendant identity = %q, %v; want %q", got, err, ready.Process.Identity)
	}

	if _, err := io.WriteString(anchor.input, "exit-target\n"); err != nil {
		t.Fatalf("request adopted descendant exit: %v", err)
	}
	reaped := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if reaped.Stage != "reaped" || reaped.ProcessExit == nil || !reaped.ProcessExit.ValidFor(ready.Process, ready.Anchor) {
		t.Fatalf("adopted descendant wait/reap event = %#v", reaped)
	}
	if err := anchor.command.Wait(); err != nil {
		t.Fatalf("anchor subprocess exited after exact reaping: %v", err)
	}
	anchor.waited = true
}

func TestLinuxOwnerAnchorWaitRejectsECHILDAndStalePIDIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	helper := startLinuxOwnerAnchorTestProcess(t, ctx, "helper", true)
	ready := readLinuxOwnerAnchorTestEvent(t, helper.output)
	if ready.Stage != "child-ready" || ready.Process.Validate() != nil {
		t.Fatalf("helper child event = %#v", ready)
	}
	helper.targetID = ready.Process
	targetFD := openLinuxOwnerAnchorTestPIDFD(t, ready.Process)
	targetFile := os.NewFile(uintptr(targetFD), "linux-owner-anchor-echild-target")
	if targetFile == nil {
		t.Fatal("wrap exact target pidfd")
	}
	helper.targetFile = targetFile

	stale := ready.Process
	stale.Identity += ":reused"
	if err := linuxOwnerAnchorWaitPIDFD(ctx, targetFile, stale); err == nil {
		t.Fatal("wait accepted a stale creation identity for a live numeric PID")
	}
	if err := linuxOwnerAnchorWaitPIDFD(ctx, targetFile, ready.Process); !errors.Is(err, unix.ECHILD) {
		t.Fatalf("wait on a non-child pidfd = %v, want ECHILD and no proof", err)
	}
	if _, err := io.WriteString(helper.input, "exit-target\n"); err != nil {
		t.Fatalf("tell helper to stop and reap its child: %v", err)
	}
	if err := helper.command.Wait(); err != nil {
		t.Fatalf("helper exited after reaping its child: %v", err)
	}
	helper.waited = true
}

func TestLinuxOwnerAnchorPeerCredentialsAndManagerIdentityAreExact(t *testing.T) {
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	if uid == 0 || gid == 0 || uint32(os.Geteuid()) != uid || uint32(os.Getegid()) != gid {
		t.Skip("exact unprivileged A UID/GID checks require an unprivileged test process")
	}
	processIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	owner := protocol.OwnerConfig{AUID: uid, AGID: gid, BUID: uid + 1, BGID: gid + 1}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), processIdentity, uid, gid, os.Getppid(), cgroupPath); err != nil {
		t.Fatalf("verify current exact owner process: %v", err)
	}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), processIdentity, uid, gid, os.Getppid()+1, cgroupPath); err == nil {
		t.Fatal("owner process verification accepted the wrong parent PID")
	}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), processIdentity, uid, gid, os.Getppid(), cgroupPath+"/other"); err == nil {
		t.Fatal("owner process verification accepted a different manager cgroup")
	}

	endpoint := filepath.Join(t.TempDir(), "owner.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: endpoint, Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: endpoint, Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	expected := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: processIdentity}
	if err := verifyLinuxOwnerAnchorPeer(server, owner, expected); err != nil {
		t.Fatalf("authenticate real SO_PEERCRED peer: %v", err)
	}
	if err := verifyLinuxOwnerAnchorPeer(server, owner, authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 1, Identity: processIdentity}); err == nil {
		t.Fatal("SO_PEERCRED verification accepted a different peer PID")
	}
	if err := verifyLinuxOwnerAnchorPeer(server, owner, authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: processIdentity + ":reused"}); err == nil {
		t.Fatal("SO_PEERCRED verification accepted a different process creation identity")
	}
	if err := verifyLinuxOwnerAnchorPeer(server, protocol.OwnerConfig{AUID: uid + 1, AGID: gid, BUID: uid, BGID: gid + 1}, expected); err == nil {
		t.Fatal("SO_PEERCRED verification accepted a different A UID")
	}
	if err := verifyLinuxOwnerAnchorPeer(server, protocol.OwnerConfig{AUID: uid, AGID: gid + 1, BUID: uid + 1, BGID: gid}, expected); err == nil {
		t.Fatal("SO_PEERCRED verification accepted a different A GID")
	}

	if linuxOwnerCgroupPathsOverlap("/system.slice/symmetry-daemon.service", "/system.slice/symmetry-owner.service/manager") {
		t.Fatal("sibling systemd service cgroups were classified as one ownership subtree")
	}
	if !linuxOwnerCgroupPathsOverlap("/system.slice/symmetry-owner.service", "/system.slice/symmetry-owner.service/manager") {
		t.Fatal("daemon peer in the owner unit ancestor was not rejected as a shared survival boundary")
	}
}

// This checks wire-shape binding only; the exit proofs here are synthetic and
// are not treated as process-stop evidence. The subprocess witness above
// exercises actual waitid/reap, while provisioned-host work must verify leaf
// membership before the anchor creates run-bound evidence.
func TestLinuxOwnerAnchorReapEvidenceRejectsRunAndRoleMismatches(t *testing.T) {
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := testLinuxOwnerAnchorGrant(authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	changedGrant := grant
	changedGrant.Secret = strings.Repeat("f", authority.SecretBytes*2)
	if grant.Equal(changedGrant) {
		t.Fatal("grant equality ignored a changed per-run secret")
	}
	for _, mutate := range []struct {
		name string
		edit func(*LinuxOwnerAnchorGrant)
	}{
		{"uncommitted leaf", func(value *LinuxOwnerAnchorGrant) { value.CreateStarted = false }},
		{"missing secret", func(value *LinuxOwnerAnchorGrant) { value.Secret = "" }},
		{"root owner UID", func(value *LinuxOwnerAnchorGrant) { value.OwnerUID = 0 }},
		{"non-root launcher UID", func(value *LinuxOwnerAnchorGrant) { value.LauncherUID = value.OwnerUID }},
		{"fence generation", func(value *LinuxOwnerAnchorGrant) { value.Fence.Generation++ }},
		{"parent mismatch", func(value *LinuxOwnerAnchorGrant) { value.Parent.Inode++ }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			changed := grant
			mutate.edit(&changed)
			if err := changed.Validate(); err == nil {
				t.Fatal("invalid durable owner grant was accepted")
			}
		})
	}
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationStart,
		RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken, Anchor: grant.Anchor,
		Parent: grant.Parent, OwnerUID: grant.OwnerUID, ManagerPath: grant.Parent.Path,
	}
	if !response.matches(grant, linuxOwnerAnchorOperationStart) {
		t.Fatal("exact grant response did not match its request")
	}
	response.RunKey.Generation++
	if response.matches(grant, linuxOwnerAnchorOperationStart) {
		t.Fatal("response from a different generation matched the durable grant")
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 10, Identity: "linux:v2:helper-create-identity"}
	process := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 11, Identity: "linux:v2:gate-create-identity"}
	helperExit := authority.LinuxCgroupExitProof{Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: grant.Anchor, Waited: true, Reaped: true}
	processExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: process, Witness: grant.Anchor, Waited: true, Reaped: true,
		HelperExit: &authority.LinuxCgroupExitProof{Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: grant.Anchor, Waited: true, Reaped: true},
	}
	evidence, err := makeLinuxOwnerAnchorReapEvidence(grant, helper, process, "gate", helperExit, processExit)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.ValidateFor(grant, helper, process, "gate"); err != nil {
		t.Fatalf("validate exact run-bound gate wait evidence: %v", err)
	}
	for _, mutate := range []struct {
		name string
		edit func(*LinuxOwnerAnchorReapEvidence)
	}{
		{"run key", func(value *LinuxOwnerAnchorReapEvidence) { value.RunKey.RunID += "-other" }},
		{"generation", func(value *LinuxOwnerAnchorReapEvidence) { value.RunKey.Generation++ }},
		{"fence", func(value *LinuxOwnerAnchorReapEvidence) { value.Fence.LeaseToken += "-other" }},
		{"launch token", func(value *LinuxOwnerAnchorReapEvidence) {
			value.LaunchToken = strings.Repeat("f", authority.TokenBytes*2)
		}},
		{"leaf", func(value *LinuxOwnerAnchorReapEvidence) { value.Leaf.LeafInode++ }},
		{"helper", func(value *LinuxOwnerAnchorReapEvidence) { value.Helper.Identity += "-reused" }},
		{"process", func(value *LinuxOwnerAnchorReapEvidence) { value.Process.Identity += "-reused" }},
		{"role", func(value *LinuxOwnerAnchorReapEvidence) { value.ProcessRole = "descendant" }},
		{"helper ECHILD-shaped proof", func(value *LinuxOwnerAnchorReapEvidence) { value.HelperExit.Reaped = false }},
		{"child ECHILD-shaped proof", func(value *LinuxOwnerAnchorReapEvidence) { value.ProcessExit.Waited = false }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			changed := evidence
			mutate.edit(&changed)
			if err := changed.ValidateFor(grant, helper, process, "gate"); err == nil {
				t.Fatal("mismatched run-bound evidence was accepted")
			}
		})
	}
}

func TestLinuxOwnerAnchorArgumentsRequireExplicitOperatorPaths(t *testing.T) {
	valid, err := parseLinuxOwnerAnchorArgs([]string{
		"-cgroup-mount", "/sys/fs/cgroup",
		"-delegated-path", "system.slice/symmetry-owner.service/manager",
		"-socket", "/run/symmetry-owner/owner.sock",
	})
	if err != nil {
		t.Fatalf("parse explicit owner-anchor operator flags: %v", err)
	}
	if valid.cgroupMount != "/sys/fs/cgroup" || valid.delegatedPath != "system.slice/symmetry-owner.service/manager" ||
		valid.socketPath != "/run/symmetry-owner/owner.sock" {
		t.Fatalf("parsed operator flags = %#v", valid)
	}
	for _, args := range [][]string{
		{"-cgroup-mount", "/sys/fs/cgroup", "-delegated-path", "../escape", "-socket", "/run/symmetry-owner/owner.sock"},
		{"-cgroup-mount", "/sys/fs/cgroup", "-delegated-path", "system.slice/owner/manager", "-socket", "relative.sock"},
		{"-cgroup-mount", "/sys/fs/cgroup", "-delegated-path", "system.slice/owner/manager", "-socket", "/run/symmetry-owner/owner.sock", "unexpected"},
	} {
		if _, err := parseLinuxOwnerAnchorArgs(args); err == nil {
			t.Fatalf("accepted unsafe or extra operator arguments: %v", args)
		}
	}
}

func startLinuxOwnerAnchorTestProcess(t *testing.T, ctx context.Context, role string, withInput bool) *linuxOwnerAnchorTestProcess {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLinuxOwnerAnchorProcessHelper$")
	command.Env = linuxOwnerAnchorTestEnvironment(role)
	command.Stderr = io.Discard
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	process := &linuxOwnerAnchorTestProcess{command: command, output: bufio.NewReader(output), anchorFD: -1, helperFD: -1, targetFD: -1}
	if withInput {
		process.input, err = command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		process.stop(t)
	})
	if role == "anchor" {
		process.anchorFD = openLinuxOwnerAnchorTestPIDFD(t, authority.LinuxCgroupProcessIdentity{PID: command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, command.Process.Pid)})
	}
	return process
}

type linuxOwnerAnchorTestProcess struct {
	command    *exec.Cmd
	input      io.WriteCloser
	output     *bufio.Reader
	anchorFD   int
	helperFD   int
	targetFD   int
	targetFile *os.File
	targetID   authority.LinuxCgroupProcessIdentity
	waited     bool
}

func (process *linuxOwnerAnchorTestProcess) stop(t *testing.T) {
	t.Helper()
	if process.input != nil {
		_ = process.input.Close()
	}
	for _, fd := range []int{process.helperFD, process.targetFD, process.anchorFD} {
		if fd >= 0 {
			_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
			_ = unix.Close(fd)
		}
	}
	if process.targetFile != nil {
		_ = unix.PidfdSendSignal(int(process.targetFile.Fd()), unix.SIGKILL, nil, 0)
		if process.targetID.Validate() == nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = linuxOwnerAnchorWaitPIDFD(cleanup, process.targetFile, process.targetID)
			cancel()
		}
		_ = process.targetFile.Close()
		process.targetFile = nil
	}
	if process.command != nil && process.command.Process != nil && !process.waited {
		_ = process.command.Process.Kill()
		_ = process.command.Wait()
		process.waited = true
	}
}

func readLinuxOwnerAnchorTestEvent(t *testing.T, reader *bufio.Reader) linuxOwnerAnchorSubprocessEvent {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read owner-anchor subprocess event: %v", err)
	}
	var event linuxOwnerAnchorSubprocessEvent
	if err := json.Unmarshal(line, &event); err != nil {
		t.Fatalf("decode owner-anchor subprocess event %q: %v", line, err)
	}
	if event.Error != "" {
		t.Fatalf("owner-anchor subprocess reported: %s", event.Error)
	}
	return event
}

func openLinuxOwnerAnchorTestPIDFD(t *testing.T, expected authority.LinuxCgroupProcessIdentity) int {
	t.Helper()
	if expected.Validate() != nil {
		t.Fatalf("test process identity is invalid: %#v", expected)
	}
	pidfd, err := unix.PidfdOpen(expected.PID, 0)
	if err != nil {
		t.Fatalf("open exact process pidfd for %d: %v", expected.PID, err)
	}
	if err := verifyLinuxOwnerAnchorPIDFD(pidfd, expected); err != nil {
		_ = unix.Close(pidfd)
		t.Fatalf("verify exact process pidfd: %v", err)
	}
	return pidfd
}

func mustLinuxOwnerAnchorTestIdentity(t *testing.T, pid int) string {
	t.Helper()
	identity, err := ProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func linuxOwnerAnchorTestEnvironment(role string) []string {
	result := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, linuxOwnerAnchorTestRole+"=") {
			result = append(result, entry)
		}
	}
	return append(result, linuxOwnerAnchorTestRole+"="+role)
}

func runLinuxOwnerAnchorTestDaemon() error {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "daemon-ready", Anchor: authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity},
	}); err != nil {
		return err
	}
	_, err = bufio.NewReader(os.Stdin).ReadString('\n')
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func runLinuxOwnerAnchorTestHelper() error {
	command := exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerAnchorProcessHelper$")
	command.Env = linuxOwnerAnchorTestEnvironment("target")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start test target: %w", err)
	}
	identity, err := ProcessIdentity(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		return err
	}
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		return err
	}
	defer unix.Close(pidfd)
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "child-ready", Process: authority.LinuxCgroupProcessIdentity{PID: command.Process.Pid, Identity: identity},
	}); err != nil {
		_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
		_, _ = command.Process.Wait()
		return err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if errors.Is(err, io.EOF) {
		select {}
	}
	if err != nil || strings.TrimSpace(line) != "exit-target" {
		return fmt.Errorf("unexpected helper test control %q: %w", line, errOr(err, errors.New("unknown command")))
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGTERM, nil, 0); err != nil {
		return fmt.Errorf("stop test target via exact pidfd: %w", err)
	}
	if _, err := command.Process.Wait(); err != nil {
		return fmt.Errorf("wait/reap helper-owned target: %w", err)
	}
	return nil
}

func runLinuxOwnerAnchorTestProcess() error {
	if err := ensureLinuxOwnerAnchorSubreaper(); err != nil {
		return err
	}
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		return err
	}
	var helperPIDFD = -1
	helper := exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerAnchorProcessHelper$")
	helper.Env = linuxOwnerAnchorTestEnvironment("helper")
	helper.Stderr = io.Discard
	helper.SysProcAttr = &syscall.SysProcAttr{PidFD: &helperPIDFD}
	helperOutput, err := helper.StdoutPipe()
	if err != nil {
		return err
	}
	if err := helper.Start(); err != nil {
		return fmt.Errorf("start test helper: %w", err)
	}
	helperIdentity, err := ProcessIdentity(helper.Process.Pid)
	if err != nil || helperPIDFD < 0 {
		if helperPIDFD >= 0 {
			_ = unix.PidfdSendSignal(helperPIDFD, unix.SIGKILL, nil, 0)
			_ = unix.Close(helperPIDFD)
		} else {
			_ = helper.Process.Kill()
		}
		_, _ = helper.Process.Wait()
		return fmt.Errorf("capture test helper identity and pidfd: %w", errOr(err, errors.New("pidfd unavailable")))
	}
	helperFile := os.NewFile(uintptr(helperPIDFD), "linux-owner-anchor-test-helper-pidfd")
	if helperFile == nil {
		_ = unix.Close(helperPIDFD)
		return errors.New("wrap test helper pidfd")
	}
	var targetFile *os.File
	var targetIdentity authority.LinuxCgroupProcessIdentity
	helperWaited := false
	targetWaited := false
	defer func() {
		if !helperWaited {
			_ = unix.PidfdSendSignal(helperPIDFD, unix.SIGKILL, nil, 0)
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = linuxOwnerAnchorWaitPIDFD(cleanup, helperFile, authority.LinuxCgroupProcessIdentity{PID: helper.Process.Pid, Identity: helperIdentity})
			cancel()
			_ = helper.Process.Release()
		}
		if targetFile != nil {
			if !targetWaited {
				_ = unix.PidfdSendSignal(int(targetFile.Fd()), unix.SIGKILL, nil, 0)
				if targetIdentity.Validate() == nil {
					cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					_ = linuxOwnerAnchorWaitPIDFD(cleanup, targetFile, targetIdentity)
					cancel()
				}
			}
			_ = targetFile.Close()
		}
		_ = helperFile.Close()
		_ = helperOutput.Close()
	}()
	helperEventLine, err := bufio.NewReader(helperOutput).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read test helper child identity: %w", err)
	}
	var helperEvent linuxOwnerAnchorSubprocessEvent
	if err := json.Unmarshal(helperEventLine, &helperEvent); err != nil || helperEvent.Stage != "child-ready" || helperEvent.Process.Validate() != nil {
		return fmt.Errorf("decode test helper child identity: %w", errOr(err, errors.New("invalid child event")))
	}
	targetIdentity = helperEvent.Process
	targetPIDFD, err := unix.PidfdOpen(targetIdentity.PID, 0)
	if err != nil {
		return fmt.Errorf("open target pidfd: %w", err)
	}
	targetFile = os.NewFile(uintptr(targetPIDFD), "linux-owner-anchor-test-target-pidfd")
	if targetFile == nil {
		_ = unix.Close(targetPIDFD)
		return errors.New("wrap target pidfd")
	}
	ownerUID, ownerGID := uint32(os.Getuid()), uint32(os.Getgid())
	if err := verifyLinuxOwnerAProcess(helper.Process.Pid, helperIdentity, ownerUID, ownerGID, os.Getpid(), managerPath); err != nil && os.Geteuid() != 0 {
		return fmt.Errorf("verify actual helper peer credentials and parent: %w", err)
	}
	anchorProcess := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "ready", Anchor: anchorProcess, Helper: authority.LinuxCgroupProcessIdentity{PID: helper.Process.Pid, Identity: helperIdentity}, Process: targetIdentity,
	}); err != nil {
		return err
	}
	control := bufio.NewReader(os.Stdin)
	line, err := control.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "kill-helper" {
		return fmt.Errorf("unexpected anchor test command %q: %w", line, errOr(err, errors.New("expected kill-helper")))
	}
	if err := unix.PidfdSendSignal(helperPIDFD, unix.SIGKILL, nil, 0); err != nil {
		return fmt.Errorf("kill exact helper via pidfd: %w", err)
	}
	helperProcess := authority.LinuxCgroupProcessIdentity{PID: helper.Process.Pid, Identity: helperIdentity}
	if err := linuxOwnerAnchorWaitPIDFD(context.Background(), helperFile, helperProcess); err != nil {
		return fmt.Errorf("wait/reap exact helper: %w", err)
	}
	helperWaited = true
	if err := helper.Process.Release(); err != nil {
		return fmt.Errorf("release Go helper handle after pidfd wait: %w", err)
	}
	credentials, err := readLinuxOwnerProcessCredentials(targetIdentity.PID)
	if err != nil || credentials.Parent != os.Getpid() {
		return fmt.Errorf("verify subreaper adopted exact target: %w", errOr(err, errors.New("target parent is not the owner anchor")))
	}
	if identity, err := ProcessIdentity(targetIdentity.PID); err != nil || identity != targetIdentity.Identity {
		return fmt.Errorf("verify adopted target creation identity: %w", errOr(err, errors.New("identity differs")))
	}
	helperExit := authority.LinuxCgroupExitProof{Version: authority.LinuxCgroupIdentityVersion, Process: helperProcess, Witness: anchorProcess, Waited: true, Reaped: true}
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "adopted", Anchor: anchorProcess, Helper: helperProcess, Process: targetIdentity,
		ParentPID: credentials.Parent, HelperExit: &helperExit,
	}); err != nil {
		return err
	}
	line, err = control.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "exit-target" {
		return fmt.Errorf("unexpected anchor test command %q: %w", line, errOr(err, errors.New("expected exit-target")))
	}
	if err := unix.PidfdSendSignal(int(targetFile.Fd()), unix.SIGTERM, nil, 0); err != nil {
		return fmt.Errorf("stop adopted target via exact pidfd: %w", err)
	}
	if err := linuxOwnerAnchorWaitPIDFD(context.Background(), targetFile, targetIdentity); err != nil {
		return fmt.Errorf("wait/reap exact adopted target: %w", err)
	}
	targetWaited = true
	processExit := authority.LinuxCgroupExitProof{Version: authority.LinuxCgroupIdentityVersion, Process: targetIdentity, Witness: anchorProcess, Waited: true, Reaped: true}
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "reaped", Anchor: anchorProcess, Helper: helperProcess, Process: targetIdentity,
		HelperExit: &helperExit, ProcessExit: &processExit,
	}); err != nil {
		return err
	}
	return nil
}

func testLinuxOwnerAnchorGrant(anchor authority.LinuxCgroupProcessIdentity, managerPath string) (LinuxOwnerAnchorGrant, error) {
	bootID := readLinuxCgroupBootID()
	if bootID == "" || anchor.Validate() != nil || !canonicalCgroupHierarchyPath(managerPath) {
		return LinuxOwnerAnchorGrant{}, errors.New("owner-anchor test grant identity is unavailable")
	}
	parent := authority.LinuxCgroupParentIdentity{BootID: bootID, MountID: 1, Device: 1, Inode: 2, Path: managerPath}
	leaf := authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: parent,
		LeafName: strings.Repeat("a", authority.TokenBytes*2), LeafDevice: 1, LeafInode: 3,
	}
	return LinuxOwnerAnchorGrant{
		ReservationVersion: linuxOwnerAnchorReservationVersion,
		RunKey:             LinuxOwnerAnchorRunKey{RunID: "owner-anchor-run-test", Generation: 1},
		Fence:              daemonprotocol.Fence{RuntimeID: "runtime-test", RuntimeEpoch: 1, Generation: 1, ClaimID: "claim-test", LeaseToken: "lease-test"},
		LaunchToken:        strings.Repeat("b", authority.TokenBytes*2),
		Secret:             strings.Repeat("c", authority.SecretBytes*2),
		PipeToken:          strings.Repeat("d", authority.TokenBytes*2),
		JobID:              strings.Repeat("e", authority.TokenBytes*2),
		OwnerUID:           1000, LauncherUID: 0, RecoveryEndpoint: "/run/symmetry-owner/run.sock", CreateStarted: true,
		Anchor: anchor, Parent: parent, Leaf: leaf,
	}, nil
}
